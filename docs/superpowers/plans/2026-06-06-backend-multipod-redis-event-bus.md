# Backend Multi-Pod Redis Event Bus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `live-auction-backend` safe to run with multiple backend pods by adding a Redis-backed WebSocket event bus, cron leases, health probes, and Kubernetes multi-replica settings.

**Architecture:** Keep the backend as one deployable service. Each pod keeps its own WebSocket connections, publishes outbound room/user events to Redis Pub/Sub, subscribes to the same bus, and performs local-only delivery for connections held by that pod. Redis leases coordinate cron jobs so only one pod executes a leased job in each window.

**Tech Stack:** Go 1.23, Flamego, Gorilla WebSocket, go-redis/v9 Pub/Sub, robfig/cron, GORM, k3s manifests.

---

## Spec

Source design: `docs/superpowers/specs/2026-06-06-backend-multipod-redis-event-bus-design.md`

This plan resolves the spec's open decisions as follows:

- Pub/Sub uses two global channels: `ws:event:room` and `ws:event:user`.
- Missed-event replay is not implemented in this plan; reconnect snapshot is authoritative.
- Client ordering remains based on `auction_version`; this plan updates protocol documentation so clients do not need to infer multi-pod behavior.

## File Structure

Create:

- `internal/app/ws/bus/envelope.go` — event envelope type, channel constants, scope constants, topic/address parsing, event ID generation, auction version extraction.
- `internal/app/ws/bus/broadcaster.go` — Redis publisher adapter and `wsevent.Broadcaster` implementation that publishes envelopes.
- `internal/app/ws/bus/subscriber.go` — Redis Pub/Sub subscriber that decodes envelopes and dispatches to local hub only.
- `internal/app/ws/bus/bus_test.go` — unit tests for envelope creation, publish behavior, and local dispatch behavior.
- `internal/core/redislease/lease.go` — small Redis `SET NX PX` lease helper used by ranking rebuild coalescing.
- `internal/core/redislease/lease_test.go` — unit tests for acquired, skipped, and error lease paths.
- `internal/core/cronlease/lease.go` — Redis lease wrapper for cron functions.
- `internal/core/cronlease/lease_test.go` — unit tests for acquired, skipped, and error lease paths.
- `internal/app/base/handler/health_test.go` — tests for `/livez`, `/readyz`, and `/health` behavior.

Modify:

- `internal/app/ws/hub/hub.go` — add `SendToUser(userID, event)` and make `Unicast` delegate to it.
- `internal/app/ws/hub/hub_test.go` — add local user delivery test.
- `internal/app/ws/init.go` — wire local hub plus distributed broadcaster when Redis is available; stop subscriber on shutdown.
- `internal/app/item/init.go` — wrap item cron functions with Redis leases.
- `internal/app/item/cache/cache.go` — add ranking rebuild lock/cooldown methods to the item cache interface.
- `internal/app/item/cache/bid.go` — implement ranking rebuild lock and cooldown keys in Redis.
- `internal/app/item/service/service.go` — add ranking rebuild owner and timing fields to service state.
- `internal/app/item/service/bid_service.go` — wrap Redis ranking miss rebuild with local singleflight plus distributed Redis lease.
- `internal/app/item/service/bid_service_test.go` — add multi-service ranking rebuild coalescing tests with fake cache locks.
- `internal/app/order/init.go` — wrap order cron functions with Redis leases.
- `internal/app/base/handler/health.go` — add `/livez` and `/readyz`, keep `/health` detailed.
- `internal/app/base/router/router.go` — register `/livez`, `/readyz`, and existing `/health`.
- `internal/core/observability/metrics.go` — add optional event bus metrics using existing recorder style.
- `docs/realtime/auction-sync-protocol.md` — document multi-pod reconnect and snapshot authority.
- `deploy/k8s/11-app.yaml` — set 2 replicas, rolling update strategy, probes, and termination grace period.

Do not modify the existing unrelated dirty files unless executing a task that directly touches them. If a file is already dirty, inspect the current contents before applying the task and preserve user changes.

---

### Task 1: Add Local User Delivery Boundary To WS Hub

**Files:**
- Modify: `internal/app/ws/hub/hub.go`
- Modify: `internal/app/ws/hub/hub_test.go`

- [ ] **Step 1: Write the failing local user delivery test**

Add this test to `internal/app/ws/hub/hub_test.go` near the existing `SendToRoom` / `Unicast` tests:

```go
func TestSendToUserDeliversToLocalUserConnections(t *testing.T) {
	h := NewHub(nil)
	targetRoomConn := newTestConn("user_1", "room_1")
	otherRoomConn := newTestConn("user_1", "room_2")
	otherUserConn := newTestConn("user_2", "room_1")

	h.Register(targetRoomConn)
	h.Register(otherRoomConn)
	h.Register(otherUserConn)

	h.SendToUser("user_1", wsevent.Event{Type: "order_created"})

	if got := len(targetRoomConn.high); got != 1 {
		t.Fatalf("target room user high queue len = %d, want 1", got)
	}
	if got := len(otherRoomConn.high); got != 1 {
		t.Fatalf("other room same user high queue len = %d, want 1", got)
	}
	if got := len(otherUserConn.high); got != 0 {
		t.Fatalf("other user high queue len = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run the focused hub test and verify it fails**

Run:

```bash
rtk go test ./internal/app/ws/hub -run TestSendToUserDeliversToLocalUserConnections -count=1
```

Expected: FAIL with `h.SendToUser undefined`.

- [ ] **Step 3: Implement `SendToUser` and delegate `Unicast`**

In `internal/app/ws/hub/hub.go`, replace the current `Unicast` body with a local-user helper:

```go
func (h *Hub) Unicast(addr string, event wsevent.Event) error {
	userID := strings.TrimPrefix(addr, "user:")
	h.SendToUser(userID, event)
	return nil
}

func (h *Hub) SendToUser(userID string, event wsevent.Event) {
	start := time.Now()
	h.mu.RLock()
	conns := append([]*Conn(nil), h.users[userID]...)
	h.mu.RUnlock()

	var delivered int64
	var dropped int64
	eventStream := classifyEventStream(event.Type)
	for _, c := range conns {
		if !streamAccepts(c.stream, eventStream) {
			continue
		}
		if h.deliver(c, event) {
			delivered++
		} else {
			dropped++
		}
	}
	result := "success"
	if dropped > 0 {
		result = "dropped"
	}
	observability.DefaultRecorder().WSBroadcast(context.Background(), observability.WSBroadcastMetric{
		Mode:       "unicast",
		Result:     result,
		EventType:  event.Type,
		Recipients: delivered,
		Duration:   time.Since(start),
	})
}
```

- [ ] **Step 4: Run hub package tests**

Run:

```bash
rtk go test ./internal/app/ws/hub -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Task 1**

```bash
rtk git add internal/app/ws/hub/hub.go internal/app/ws/hub/hub_test.go
rtk git commit -m "feat(ws): expose local user delivery"
```

---

### Task 2: Add Event Bus Envelope And Publishing Broadcaster

**Files:**
- Create: `internal/app/ws/bus/envelope.go`
- Create: `internal/app/ws/bus/broadcaster.go`
- Create: `internal/app/ws/bus/bus_test.go`

- [ ] **Step 1: Write failing envelope and publish tests**

Create `internal/app/ws/bus/bus_test.go` with:

```go
package bus

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type fakePublisher struct {
	channel string
	payload string
	err     error
}

func (p *fakePublisher) Publish(_ context.Context, channel, payload string) error {
	p.channel = channel
	p.payload = payload
	return p.err
}

func TestBroadcasterPublishesRoomFanoutEnvelope(t *testing.T) {
	pub := &fakePublisher{}
	b := NewBroadcaster(pub, Options{PodID: "pod_a", NewEventID: func() string { return "evt_1" }})

	err := b.Fanout(wsevent.RoomTopic("room_1"), wsevent.Event{
		Type: "bid_success",
		Payload: map[string]any{
			"item_id": "item_1",
			"auction_version": float64(7),
		},
	})
	if err != nil {
		t.Fatalf("Fanout returned error: %v", err)
	}
	if pub.channel != ChannelRoom {
		t.Fatalf("channel = %q, want %q", pub.channel, ChannelRoom)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(pub.payload), &env); err != nil {
		t.Fatalf("payload is not envelope json: %v", err)
	}
	if env.EventID != "evt_1" || env.Scope != ScopeRoom || env.Target != "room_1" || env.Type != "bid_success" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if env.AuctionVersion != 7 {
		t.Fatalf("auction_version = %d, want 7", env.AuctionVersion)
	}
	if env.SourcePod != "pod_a" {
		t.Fatalf("source pod = %q, want pod_a", env.SourcePod)
	}
}

func TestBroadcasterPublishesUserUnicastEnvelope(t *testing.T) {
	pub := &fakePublisher{}
	b := NewBroadcaster(pub, Options{PodID: "pod_b", NewEventID: func() string { return "evt_2" }})

	err := b.Unicast(wsevent.UserAddr("user_1"), wsevent.Event{Type: "order_created"})
	if err != nil {
		t.Fatalf("Unicast returned error: %v", err)
	}
	if pub.channel != ChannelUser {
		t.Fatalf("channel = %q, want %q", pub.channel, ChannelUser)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(pub.payload), &env); err != nil {
		t.Fatalf("payload is not envelope json: %v", err)
	}
	if env.EventID != "evt_2" || env.Scope != ScopeUser || env.Target != "user_1" || env.Type != "order_created" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
rtk go test ./internal/app/ws/bus -count=1
```

Expected: FAIL because package `internal/app/ws/bus` does not exist yet.

- [ ] **Step 3: Create envelope definitions**

Create `internal/app/ws/bus/envelope.go`:

```go
package bus

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

const (
	ChannelRoom = "ws:event:room"
	ChannelUser = "ws:event:user"

	ScopeRoom = "room"
	ScopeUser = "user"
)

type Envelope struct {
	EventID         string          `json:"event_id"`
	Scope           string          `json:"scope"`
	Target          string          `json:"target"`
	Type            string          `json:"type"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	AuctionVersion  int64           `json:"auction_version,omitempty"`
	SourcePod       string          `json:"source_pod"`
	CreatedAtUnixMS int64           `json:"created_at_unix_ms"`
}

func newEventID() string {
	return "evt_" + snowflake.MakeUUID()
}

func envelopeFromEvent(scope, target string, event wsevent.Event, sourcePod string, eventID func() string, now func() time.Time) (Envelope, error) {
	raw, err := json.Marshal(event.Payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		EventID:         eventID(),
		Scope:           scope,
		Target:          target,
		Type:            event.Type,
		Payload:         raw,
		AuctionVersion:  auctionVersionFromJSON(raw),
		SourcePod:       sourcePod,
		CreatedAtUnixMS: now().UnixMilli(),
	}, nil
}

func topicTarget(topic string) (string, error) {
	target := strings.TrimPrefix(topic, "room:")
	if target == "" || target == topic {
		return "", fmt.Errorf("invalid room topic: %q", topic)
	}
	return target, nil
}

func userTarget(addr string) (string, error) {
	target := strings.TrimPrefix(addr, "user:")
	if target == "" || target == addr {
		return "", fmt.Errorf("invalid user address: %q", addr)
	}
	return target, nil
}

func auctionVersionFromJSON(raw []byte) int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return 0
	}
	switch v := fields["auction_version"].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		return 0
	}
}
```

- [ ] **Step 4: Create publishing broadcaster**

Create `internal/app/ws/bus/broadcaster.go`:

```go
package bus

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

type RedisPublisher struct {
	client *redis.Client
}

func NewRedisPublisher(client *redis.Client) *RedisPublisher {
	return &RedisPublisher{client: client}
}

func (p *RedisPublisher) Publish(ctx context.Context, channel, payload string) error {
	return p.client.Publish(ctx, channel, payload).Err()
}

type Options struct {
	PodID      string
	NewEventID func() string
	Now        func() time.Time
}

type Broadcaster struct {
	publisher Publisher
	podID     string
	newEventID func() string
	now       func() time.Time
}

func NewBroadcaster(publisher Publisher, opts Options) *Broadcaster {
	if opts.NewEventID == nil {
		opts.NewEventID = newEventID
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Broadcaster{
		publisher: publisher,
		podID:     opts.PodID,
		newEventID: opts.NewEventID,
		now:       opts.Now,
	}
}

func (b *Broadcaster) Fanout(topic string, event wsevent.Event) error {
	target, err := topicTarget(topic)
	if err != nil {
		return err
	}
	env, err := envelopeFromEvent(ScopeRoom, target, event, b.podID, b.newEventID, b.now)
	if err != nil {
		return err
	}
	return b.publish(ChannelRoom, env)
}

func (b *Broadcaster) Unicast(addr string, event wsevent.Event) error {
	target, err := userTarget(addr)
	if err != nil {
		return err
	}
	env, err := envelopeFromEvent(ScopeUser, target, event, b.podID, b.newEventID, b.now)
	if err != nil {
		return err
	}
	return b.publish(ChannelUser, env)
}

func (b *Broadcaster) publish(channel string, env Envelope) error {
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return b.publisher.Publish(context.Background(), channel, string(raw))
}
```

- [ ] **Step 5: Run bus tests**

Run:

```bash
rtk go test ./internal/app/ws/bus -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

```bash
rtk git add internal/app/ws/bus/envelope.go internal/app/ws/bus/broadcaster.go internal/app/ws/bus/bus_test.go
rtk git commit -m "feat(ws): publish events to redis bus"
```

---

### Task 3: Add Redis Subscriber And Local Dispatch

**Files:**
- Modify: `internal/app/ws/bus/bus_test.go`
- Create: `internal/app/ws/bus/subscriber.go`

- [ ] **Step 1: Add failing dispatch tests**

Append these tests to `internal/app/ws/bus/bus_test.go`:

```go
type fakeDispatcher struct {
	roomID string
	userID string
	event  wsevent.Event
}

func (d *fakeDispatcher) SendToRoom(roomID string, event wsevent.Event) {
	d.roomID = roomID
	d.event = event
}

func (d *fakeDispatcher) SendToUser(userID string, event wsevent.Event) {
	d.userID = userID
	d.event = event
}

func TestSubscriberDispatchesRoomEnvelopeLocally(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	s := NewSubscriber(dispatcher)
	raw := `{"event_id":"evt_1","scope":"room","target":"room_1","type":"bid_success","payload":{"item_id":"item_1"},"source_pod":"pod_a","created_at_unix_ms":1}`

	if err := s.DispatchPayload([]byte(raw)); err != nil {
		t.Fatalf("DispatchPayload returned error: %v", err)
	}
	if dispatcher.roomID != "room_1" {
		t.Fatalf("roomID = %q, want room_1", dispatcher.roomID)
	}
	if dispatcher.event.Type != "bid_success" {
		t.Fatalf("event type = %q, want bid_success", dispatcher.event.Type)
	}
}

func TestSubscriberDispatchesUserEnvelopeLocally(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	s := NewSubscriber(dispatcher)
	raw := `{"event_id":"evt_2","scope":"user","target":"user_1","type":"order_created","payload":{"order_id":"order_1"},"source_pod":"pod_b","created_at_unix_ms":1}`

	if err := s.DispatchPayload([]byte(raw)); err != nil {
		t.Fatalf("DispatchPayload returned error: %v", err)
	}
	if dispatcher.userID != "user_1" {
		t.Fatalf("userID = %q, want user_1", dispatcher.userID)
	}
	if dispatcher.event.Type != "order_created" {
		t.Fatalf("event type = %q, want order_created", dispatcher.event.Type)
	}
}

func TestSubscriberRejectsMalformedEnvelope(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	s := NewSubscriber(dispatcher)

	if err := s.DispatchPayload([]byte(`{"scope":"room","target":"","type":"bid_success"}`)); err == nil {
		t.Fatal("expected malformed envelope error")
	}
	if dispatcher.roomID != "" || dispatcher.userID != "" {
		t.Fatalf("malformed envelope should not dispatch: %+v", dispatcher)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
rtk go test ./internal/app/ws/bus -count=1
```

Expected: FAIL with `NewSubscriber undefined`.

- [ ] **Step 3: Implement subscriber**

Create `internal/app/ws/bus/subscriber.go`:

```go
package bus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type LocalDispatcher interface {
	SendToRoom(roomID string, event wsevent.Event)
	SendToUser(userID string, event wsevent.Event)
}

type Subscriber struct {
	dispatcher LocalDispatcher
}

func NewSubscriber(dispatcher LocalDispatcher) *Subscriber {
	return &Subscriber{dispatcher: dispatcher}
}

func (s *Subscriber) DispatchPayload(raw []byte) error {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if env.Target == "" || env.Type == "" {
		return fmt.Errorf("invalid websocket bus envelope")
	}
	event := wsevent.Event{Type: env.Type, Payload: json.RawMessage(env.Payload)}
	switch env.Scope {
	case ScopeRoom:
		s.dispatcher.SendToRoom(env.Target, event)
	case ScopeUser:
		s.dispatcher.SendToUser(env.Target, event)
	default:
		return fmt.Errorf("unknown websocket bus scope: %s", env.Scope)
	}
	return nil
}

func (s *Subscriber) Run(ctx context.Context, client *redis.Client) {
	pubsub := client.Subscribe(ctx, ChannelRoom, ChannelUser)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := s.DispatchPayload([]byte(msg.Payload)); err != nil {
				logx.Warnw("ws bus dispatch failed", "channel", msg.Channel, "err", err)
			}
		}
	}
}
```

- [ ] **Step 4: Run bus tests**

Run:

```bash
rtk go test ./internal/app/ws/bus -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Task 3**

```bash
rtk git add internal/app/ws/bus/subscriber.go internal/app/ws/bus/bus_test.go
rtk git commit -m "feat(ws): dispatch redis bus events locally"
```

---

### Task 4: Wire Distributed Broadcaster Into WS Module

**Files:**
- Modify: `internal/app/ws/init.go`
- Modify: `internal/app/item/init.go`

- [ ] **Step 1: Inspect current dirty state for touched files**

Run:

```bash
rtk git status --short internal/app/ws/init.go internal/app/item/init.go
```

Expected: either clean, or local changes that are read and preserved before editing.

- [ ] **Step 2: Modify WS module globals and lifecycle**

Update `internal/app/ws/init.go` so the WS module owns a local hub and exports a distributed broadcaster when Redis exists:

```go
package ws

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/bus"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/handler"
	wshub "github.com/zet-plane/live-auction-backend/internal/app/ws/hub"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/router"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

var Hub wsevent.Broadcaster

type WS struct {
	Name string
	cancel context.CancelFunc
	app.UnimplementedModule
}

func (w *WS) Info() string { return w.Name }

func (w *WS) Load(engine *kernel.Engine) error {
	localHub := wshub.NewHub(engine.Cache)
	Hub = localHub
	if engine.Cache != nil {
		Hub = bus.NewBroadcaster(bus.NewRedisPublisher(engine.Cache), bus.Options{PodID: podID()})
		subCtx, cancel := context.WithCancel(engine.Context)
		w.cancel = cancel
		go bus.NewSubscriber(localHub).Run(subCtx, engine.Cache)
	}
	handler.Init(localHub)
	handler.ConfigureOriginChecker(web.NewOriginPolicy(engine.Config.Mode, engine.Config.Security.AllowedOrigins))
	handler.InitTicket(engine.Cache)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (w *WS) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	if w.cancel != nil {
		w.cancel()
	}
	return nil
}

func podID() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "backend-unknown"
	}
	return "backend-" + strings.TrimSpace(name)
}
```

- [ ] **Step 3: Update item snapshot provider wiring**

`internal/app/item/init.go` currently calls `SetSnapshotProvider` on `wsapp.Hub`. Because `Hub` becomes the distributed publisher, keep snapshot registration against the local hub by adding this function to `internal/app/ws/init.go`:

```go
var localSnapshotTarget interface{ SetSnapshotProvider(wshub.SnapshotProvider) }

func SetSnapshotProvider(provider wshub.SnapshotProvider) {
	if localSnapshotTarget != nil {
		localSnapshotTarget.SetSnapshotProvider(provider)
	}
}
```

Then set it during `Load`:

```go
localHub := wshub.NewHub(engine.Cache)
localSnapshotTarget = localHub
```

Replace the snapshot provider block in `internal/app/item/init.go`:

```go
wsapp.SetSnapshotProvider(svc)
```

Remove the now-unused `wshub` import from `internal/app/item/init.go`.

- [ ] **Step 4: Run compile tests for WS and item packages**

Run:

```bash
rtk go test ./internal/app/ws/... ./internal/app/item/... -run TestDoesNotExist -count=1
```

Expected: PASS with packages compiling and no tests matching `TestDoesNotExist`.

- [ ] **Step 5: Run focused existing snapshot behavior tests**

Run:

```bash
rtk go test ./internal/app/ws/hub ./internal/app/item/service -run 'TestRegister|Test.*Snapshot|Test.*Broadcast' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 4**

```bash
rtk git add internal/app/ws/init.go internal/app/item/init.go
rtk git commit -m "feat(ws): wire distributed broadcaster"
```

---

### Task 5: Add Event Bus Metrics

**Files:**
- Modify: `internal/core/observability/metrics.go`
- Modify: `internal/core/observability/metrics_test.go`
- Modify: `internal/app/ws/bus/broadcaster.go`
- Modify: `internal/app/ws/bus/subscriber.go`

- [ ] **Step 1: Add failing recorder metric tests**

In `internal/core/observability/metrics_test.go`, add a focused test following the existing capture-recorder style:

```go
func TestNoopRecorderAcceptsWSEventBusMetric(t *testing.T) {
	var rec Recorder = NoopRecorder{}
	rec.WSEventBus(context.Background(), WSEventBusMetric{
		Action:    "publish",
		Result:    "success",
		Scope:     "room",
		EventType: "bid_success",
	})
}
```

- [ ] **Step 2: Run observability tests and verify they fail**

Run:

```bash
rtk go test ./internal/core/observability -run TestNoopRecorderAcceptsWSEventBusMetric -count=1
```

Expected: FAIL with `rec.WSEventBus undefined` or `WSEventBusMetric undefined`.

- [ ] **Step 3: Add metric type and recorder method**

In `internal/core/observability/metrics.go`, add this method to `Recorder`:

```go
WSEventBus(context.Context, WSEventBusMetric)
```

Add this metric struct near the WebSocket metric structs:

```go
type WSEventBusMetric struct {
	Action    string
	Result    string
	Scope     string
	EventType string
}
```

Add the noop implementation:

```go
func (NoopRecorder) WSEventBus(context.Context, WSEventBusMetric) {}
```

Add fields to `OTelRecorder`:

```go
wsEventBusCount metric.Int64Counter
```

In `NewRecorder`, create the counter with the same meter as the other metrics:

```go
wsEventBusCount, err := meter.Int64Counter("live_auction_ws_event_bus_total")
if err != nil {
	return nil, err
}
```

Assign it in the returned recorder:

```go
wsEventBusCount: wsEventBusCount,
```

Add the method:

```go
func (r *OTelRecorder) WSEventBus(ctx context.Context, m WSEventBusMetric) {
	r.wsEventBusCount.Add(ctx, 1, metric.WithAttributes(
		attribute.String("action", SafeReason(m.Action)),
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("scope", SafeReason(m.Scope)),
		attribute.String("event_type", SafeReason(m.EventType)),
	))
}
```

- [ ] **Step 4: Record publish and dispatch metrics**

In `internal/app/ws/bus/broadcaster.go`, import `github.com/zet-plane/live-auction-backend/internal/core/observability`.

In `publish`, record success or error:

```go
func (b *Broadcaster) publish(channel string, env Envelope) error {
	raw, err := json.Marshal(env)
	if err != nil {
		recordBus("publish", "error", env.Scope, env.Type)
		return err
	}
	err = b.publisher.Publish(context.Background(), channel, string(raw))
	if err != nil {
		recordBus("publish", "error", env.Scope, env.Type)
		return err
	}
	recordBus("publish", "success", env.Scope, env.Type)
	return nil
}

func recordBus(action, result, scope, eventType string) {
	observability.DefaultRecorder().WSEventBus(context.Background(), observability.WSEventBusMetric{
		Action:    action,
		Result:    result,
		Scope:     scope,
		EventType: eventType,
	})
}
```

In `internal/app/ws/bus/subscriber.go`, call `recordBus("dispatch", "success", env.Scope, env.Type)` after successful dispatch and `recordBus("dispatch", "error", env.Scope, env.Type)` before returning validation errors.

- [ ] **Step 5: Run observability and bus tests**

Run:

```bash
rtk go test ./internal/core/observability ./internal/app/ws/bus -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 5**

```bash
rtk git add internal/core/observability/metrics.go internal/core/observability/metrics_test.go internal/app/ws/bus/broadcaster.go internal/app/ws/bus/subscriber.go
rtk git commit -m "feat(ws): record event bus metrics"
```

---

### Task 6: Add Distributed Ranking Rebuild Coalescing

**Files:**
- Create: `internal/core/redislease/lease.go`
- Create: `internal/core/redislease/lease_test.go`
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/cache/bid.go`
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_service_test.go`
- Modify: `internal/app/item/init.go`

- [ ] **Step 1: Write failing shared Redis lease tests**

Create `internal/core/redislease/lease_test.go`:

```go
package redislease

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSetter struct {
	ok  bool
	err error
}

func (s fakeSetter) SetNX(context.Context, string, any, time.Duration) (bool, error) {
	return s.ok, s.err
}

func TestAcquireReturnsTrueWhenSetNXWins(t *testing.T) {
	store := Store{Setter: fakeSetter{ok: true}}
	ok, err := store.Acquire(context.Background(), "key", "owner", time.Second)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected acquired lease")
	}
}

func TestAcquireReturnsFalseWhenSetNXLoses(t *testing.T) {
	store := Store{Setter: fakeSetter{ok: false}}
	ok, err := store.Acquire(context.Background(), "key", "owner", time.Second)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if ok {
		t.Fatal("expected lease miss")
	}
}

func TestAcquireReturnsErrorFromStore(t *testing.T) {
	store := Store{Setter: fakeSetter{err: errors.New("redis down")}}
	ok, err := store.Acquire(context.Background(), "key", "owner", time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	if ok {
		t.Fatal("expected ok=false on error")
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
rtk go test ./internal/core/redislease -count=1
```

Expected: FAIL because package `internal/core/redislease` does not exist yet.

- [ ] **Step 3: Implement shared Redis lease helper**

Create `internal/core/redislease/lease.go`:

```go
package redislease

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Setter interface {
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
}

type RedisSetter struct {
	Client *redis.Client
}

func (s RedisSetter) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return s.Client.SetNX(ctx, key, value, ttl).Result()
}

type Store struct {
	Setter Setter
}

func (s Store) Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if s.Setter == nil {
		return false, nil
	}
	return s.Setter.SetNX(ctx, key, value, ttl)
}
```

- [ ] **Step 4: Add ranking rebuild methods to cache interface and Redis cache**

In `internal/app/item/cache/cache.go`, add these methods to `Cache`:

```go
AcquireRankingRebuild(ctx context.Context, itemID, owner string, ttl time.Duration) (bool, error)
SetRankingRebuildCooldown(ctx context.Context, itemID string, ttl time.Duration) error
RankingRebuildCoolingDown(ctx context.Context, itemID string) (bool, error)
```

In `internal/app/item/cache/bid.go`, add imports:

```go
"github.com/zet-plane/live-auction-backend/internal/core/redislease"
```

Add key helpers:

```go
func rankingRebuildLockKey(itemID string) string {
	return "auction:item:" + itemID + ":ranking:rebuild_lock"
}

func rankingRebuildCooldownKey(itemID string) string {
	return "auction:item:" + itemID + ":ranking:rebuild_cooldown"
}
```

Add methods:

```go
func (c *RedisCache) AcquireRankingRebuild(ctx context.Context, itemID, owner string, ttl time.Duration) (bool, error) {
	return redislease.Store{Setter: redislease.RedisSetter{Client: c.client}}.
		Acquire(ctx, rankingRebuildLockKey(itemID), owner, ttl)
}

func (c *RedisCache) SetRankingRebuildCooldown(ctx context.Context, itemID string, ttl time.Duration) error {
	return c.client.Set(ctx, rankingRebuildCooldownKey(itemID), "1", ttl).Err()
}

func (c *RedisCache) RankingRebuildCoolingDown(ctx context.Context, itemID string) (bool, error) {
	n, err := c.client.Exists(ctx, rankingRebuildCooldownKey(itemID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
```

- [ ] **Step 5: Add service owner and timing fields**

In `internal/app/item/service/service.go`, add fields to `Service`:

```go
rankingRebuildOwner       string
rankingRebuildLockTTL     time.Duration
rankingRebuildWait        time.Duration
rankingRebuildCooldownTTL time.Duration
```

Set defaults in `NewService`:

```go
rankingRebuildOwner:       "backend-local",
rankingRebuildLockTTL:     time.Second,
rankingRebuildWait:        100 * time.Millisecond,
rankingRebuildCooldownTTL: 2 * time.Second,
```

Add a setter near `NewService`:

```go
func (s *Service) SetRankingRebuildOwner(owner string) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return
	}
	s.rankingRebuildOwner = owner
}
```

- [ ] **Step 6: Wire pod owner from item module**

In `internal/app/item/init.go`, after `svc := service.NewService(...)`, add:

```go
svc.SetRankingRebuildOwner(bidLogConsumerName(os.Hostname))
```

- [ ] **Step 7: Add failing multi-service ranking rebuild test**

Append this test to `internal/app/item/service/bid_service_test.go`:

```go
func TestGetRankingCoalescesRedisMissRebuildAcrossServices(t *testing.T) {
	store := newFakeStore()
	store.listBidRankingDelay = 20 * time.Millisecond
	fc := newFakeCache()
	svcA := NewService(store, testPolicy, fc, nil, nil, nil)
	svcB := NewService(store, testPolicy, fc, nil, nil, nil)
	svcA.SetRankingRebuildOwner("backend-a")
	svcB.SetRankingRebuildOwner("backend-b")
	svcA.rankingRebuildWait = 40 * time.Millisecond
	svcB.rankingRebuildWait = 40 * time.Millisecond

	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svcA, "merchant_1", "room_1", 0, 100, 0, endTime)

	store.bidLogs = append(store.bidLogs,
		&itemmodel.BidLog{ID: "b1", ItemID: itemID, RoomID: "room_1", UserID: "u1", Price: 200},
		&itemmodel.BidLog{ID: "b2", ItemID: itemID, RoomID: "room_1", UserID: "u2", Price: 300},
	)
	fc.states[itemID].BidCount = 2

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = svcA.GetRanking(context.Background(), itemID, 1, 10)
	}()
	go func() {
		defer wg.Done()
		_, _ = svcB.GetRanking(context.Background(), itemID, 1, 10)
	}()
	wg.Wait()

	if store.listBidRankingCalls != 1 {
		t.Fatalf("expected one distributed ListBidRanking rebuild, got %d", store.listBidRankingCalls)
	}
	if got := fc.ranking[itemID]["u2"]; got != 300 {
		t.Fatalf("expected rebuilt ranking to be cached, got u2=%d", got)
	}
}
```

Update fake cache in `internal/app/item/service/service_test.go` with lock/cooldown fields and methods:

```go
rankingMu        sync.Mutex
rankingLocks     map[string]string
rankingCooldowns map[string]bool
```

Initialize them in `newFakeCache()`:

```go
rankingLocks:     map[string]string{},
rankingCooldowns: map[string]bool{},
```

Add methods:

```go
func (c *fakeCache) AcquireRankingRebuild(_ context.Context, itemID, owner string, _ time.Duration) (bool, error) {
	c.rankingMu.Lock()
	defer c.rankingMu.Unlock()
	if c.rankingLocks[itemID] != "" {
		return false, nil
	}
	c.rankingLocks[itemID] = owner
	return true, nil
}

func (c *fakeCache) SetRankingRebuildCooldown(_ context.Context, itemID string, _ time.Duration) error {
	c.rankingMu.Lock()
	defer c.rankingMu.Unlock()
	c.rankingCooldowns[itemID] = true
	return nil
}

func (c *fakeCache) RankingRebuildCoolingDown(_ context.Context, itemID string) (bool, error) {
	c.rankingMu.Lock()
	defer c.rankingMu.Unlock()
	return c.rankingCooldowns[itemID], nil
}
```

- [ ] **Step 8: Run the new test and verify it fails**

Run:

```bash
rtk go test ./internal/app/item/service -run TestGetRankingCoalescesRedisMissRebuildAcrossServices -count=1
```

Expected: FAIL with two `ListBidRanking` calls before the distributed lease is used.

- [ ] **Step 9: Wrap ranking rebuild with distributed lease**

In `internal/app/item/service/bid_service.go`, update `rebuildRankingOnce` so the local `singleflight` body first checks cooldown, then acquires the distributed lock:

```go
func (s *Service) rebuildRankingOnce(ctx context.Context, itemID string, limit int) ([]dto.BidderPrice, error) {
	if limit <= 0 {
		return nil, nil
	}
	value, err, _ := s.rankingRebuilds.Do("ranking:"+itemID, func() (any, error) {
		if s.cache != nil {
			coolingDown, err := s.cache.RankingRebuildCoolingDown(ctx, itemID)
			if err != nil {
				logx.Warnw("item.GetRanking check rebuild cooldown failed", "item_id", itemID, "err", err)
			}
			if coolingDown {
				return s.waitForRankingRebuild(ctx, itemID, limit), nil
			}
			acquired, err := s.cache.AcquireRankingRebuild(ctx, itemID, s.rankingRebuildOwner, s.rankingRebuildLockTTL)
			if err != nil {
				logx.Warnw("item.GetRanking acquire ranking rebuild lock failed", "item_id", itemID, "err", err)
				return s.waitForRankingRebuild(ctx, itemID, limit), nil
			}
			if !acquired {
				return s.waitForRankingRebuild(ctx, itemID, limit), nil
			}
		}

		entries, err := s.store.ListBidRanking(itemID, limit)
		if err != nil {
			if s.cache != nil {
				_ = s.cache.SetRankingRebuildCooldown(ctx, itemID, s.rankingRebuildCooldownTTL)
			}
			return nil, err
		}
		if s.cache != nil {
			if len(entries) > 0 {
				if err := s.cache.SetRanking(ctx, itemID, entries); err != nil {
					logx.Warnw("item.GetRanking set rebuilt redis ranking failed", "item_id", itemID, "err", err)
				}
			} else {
				_ = s.cache.SetRankingRebuildCooldown(ctx, itemID, s.rankingRebuildCooldownTTL)
			}
		}
		return entries, nil
	})
	if err != nil {
		return nil, err
	}
	entries, _ := value.([]dto.BidderPrice)
	return entries, nil
}

func (s *Service) waitForRankingRebuild(ctx context.Context, itemID string, limit int) []dto.BidderPrice {
	if s.cache == nil {
		return nil
	}
	timer := time.NewTimer(s.rankingRebuildWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil
	case <-timer.C:
	}
	entries, err := s.cache.GetRanking(ctx, itemID, 0, limit)
	if err != nil {
		logx.Warnw("item.GetRanking reread ranking after rebuild wait failed", "item_id", itemID, "err", err)
		return nil
	}
	return entries
}
```

- [ ] **Step 10: Run ranking tests**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestGetRanking.*Rebuild|TestGetRankingCoalesces' -count=1
```

Expected: PASS.

- [ ] **Step 11: Run item cache and service tests**

Run:

```bash
rtk go test ./internal/core/redislease ./internal/app/item/cache ./internal/app/item/service -count=1
```

Expected: PASS.

- [ ] **Step 12: Commit Task 6**

```bash
rtk git add internal/core/redislease/lease.go internal/core/redislease/lease_test.go internal/app/item/cache/cache.go internal/app/item/cache/bid.go internal/app/item/service/service.go internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go internal/app/item/init.go
rtk git commit -m "feat(item): coalesce ranking rebuilds across pods"
```

---

### Task 7: Add Redis Cron Lease Wrapper And Wrap Cron Jobs

**Files:**
- Create: `internal/core/cronlease/lease.go`
- Create: `internal/core/cronlease/lease_test.go`
- Modify: `internal/app/item/init.go`
- Modify: `internal/app/order/init.go`

- [ ] **Step 1: Write failing cron lease tests**

Create `internal/core/cronlease/lease_test.go`:

```go
package cronlease

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	acquired bool
	err      error
}

func (s fakeStore) Acquire(context.Context, string, string, time.Duration) (bool, error) {
	return s.acquired, s.err
}

func TestWrapRunsFunctionWhenLeaseAcquired(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{acquired: true}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if !called {
		t.Fatal("expected wrapped function to run")
	}
}

func TestWrapSkipsFunctionWhenLeaseNotAcquired(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{acquired: false}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if called {
		t.Fatal("expected wrapped function to be skipped")
	}
}

func TestWrapSkipsFunctionWhenAcquireErrors(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{err: errors.New("redis down")}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if called {
		t.Fatal("expected wrapped function to be skipped after acquire error")
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
rtk go test ./internal/core/cronlease -count=1
```

Expected: FAIL because package `internal/core/cronlease` does not exist yet.

- [ ] **Step 3: Implement cron lease wrapper**

Create `internal/core/cronlease/lease.go`:

```go
package cronlease

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

type Store interface {
	Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
}

type RedisStore struct {
	Client *redis.Client
}

func (s RedisStore) Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return s.Client.SetNX(ctx, key, value, ttl).Result()
}

func Wrap(name, podID string, ttl time.Duration, store Store, fn func(context.Context)) func(context.Context) {
	return func(ctx context.Context) {
		if store == nil {
			record(ctx, name, "lease_unconfigured")
			return
		}
		ok, err := store.Acquire(ctx, "cron:lease:"+name, podID, ttl)
		if err != nil {
			logx.Warnw("cron lease acquire failed", "cron_name", name, "lease_owner", podID, "err", err)
			record(ctx, name, "lease_error")
			return
		}
		if !ok {
			record(ctx, name, "lease_skipped")
			return
		}
		record(ctx, name, "lease_acquired")
		fn(ctx)
	}
}

func record(ctx context.Context, name, result string) {
	observability.DefaultRecorder().Cron(ctx, observability.CronMetric{
		Name:   name + ".lease",
		Result: result,
	})
}
```

- [ ] **Step 4: Run cron lease tests**

Run:

```bash
rtk go test ./internal/core/cronlease -count=1
```

Expected: PASS.

- [ ] **Step 5: Wrap item cron jobs**

In `internal/app/item/init.go`, import `time` and `github.com/zet-plane/live-auction-backend/internal/core/cronlease`.

Create a store and pod owner after service setup:

```go
leaseStore := cronlease.RedisStore{Client: engine.Cache}
leaseOwner := bidLogConsumerName(os.Hostname)
```

Replace cron registration:

```go
engine.Cron.AddFunc("@every 1s", observability.WrapCron("item.settle_due_auctions",
	cronlease.Wrap("item.settle_due_auctions", leaseOwner, 2*time.Second, leaseStore, svc.SettleDueAuctions)))
engine.Cron.AddFunc("@every 1s", observability.WrapCron("item.broadcast_time_sync",
	cronlease.Wrap("item.broadcast_time_sync", leaseOwner, time.Second, leaseStore, svc.BroadcastTimeSync)))
engine.Cron.AddFunc("@every 1m", observability.WrapCron("item.end_expired_auctions_fallback",
	cronlease.Wrap("item.end_expired_auctions_fallback", leaseOwner, 30*time.Second, leaseStore, svc.EndExpiredAuctions)))
```

- [ ] **Step 6: Wrap order cron jobs**

In `internal/app/order/init.go`, import `os`, `strings`, and `github.com/zet-plane/live-auction-backend/internal/core/cronlease`.

Add helper:

```go
func leaseOwner(hostname func() (string, error)) string {
	name, err := hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "backend-unknown"
	}
	return "backend-" + strings.TrimSpace(name)
}
```

Replace order cron registration:

```go
storeLease := cronlease.RedisStore{Client: engine.Cache}
owner := leaseOwner(os.Hostname)
engine.Cron.AddFunc("@every 5m", observability.WrapCron("order.scan_expired_orders",
	cronlease.Wrap("order.scan_expired_orders", owner, 2*time.Minute, storeLease, Svc.ScanExpiredOrders)))
engine.Cron.AddFunc("@every 10m", observability.WrapCron("order.scan_compensation",
	cronlease.Wrap("order.scan_compensation", owner, 2*time.Minute, storeLease, Svc.ScanCompensation)))
```

- [ ] **Step 7: Run affected package tests**

Run:

```bash
rtk go test ./internal/core/cronlease ./internal/app/item/... ./internal/app/order/... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit Task 7**

```bash
rtk git add internal/core/cronlease/lease.go internal/core/cronlease/lease_test.go internal/app/item/init.go internal/app/order/init.go
rtk git commit -m "feat(cron): coordinate jobs with redis leases"
```

---

### Task 8: Split Liveness, Readiness, And Detailed Health

**Files:**
- Modify: `internal/app/base/handler/health.go`
- Modify: `internal/app/base/router/router.go`
- Create: `internal/app/base/handler/health_test.go`

- [ ] **Step 1: Add failing health handler tests**

Create `internal/app/base/handler/health_test.go`:

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flamego/flamego"
)

func TestLivezAlwaysReturnsOK(t *testing.T) {
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Get("/livez", Livez)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadyzWithoutDBReturnsServiceUnavailable(t *testing.T) {
	Init(nil, nil)
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Get("/readyz", Readyz)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
rtk go test ./internal/app/base/handler -run 'TestLivez|TestReadyz' -count=1
```

Expected: FAIL with `Livez undefined` and `Readyz undefined`.

- [ ] **Step 3: Implement `Livez` and `Readyz`**

Add these handlers to `internal/app/base/handler/health.go`:

```go
func Livez(r flamego.Render) {
	response.OK(r, map[string]string{"status": "ok"})
}

func Readyz(r flamego.Render) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if db == nil {
		response.Success(r, 503, "degraded", healthData{
			Status: "degraded",
			Components: map[string]componentStatus{
				"mysql": {Status: "error", Error: "not initialized"},
			},
		})
		return
	}
	start := time.Now()
	sqlDB, err := db.DB()
	if err == nil {
		err = sqlDB.PingContext(ctx)
	}
	elapsed := time.Since(start)
	if err != nil {
		response.Success(r, 503, "degraded", healthData{
			Status: "degraded",
			Components: map[string]componentStatus{
				"mysql": {Status: "error", Error: err.Error()},
			},
		})
		return
	}
	response.OK(r, healthData{
		Status: "ok",
		Components: map[string]componentStatus{
			"mysql": {Status: "ok", Latency: elapsed.String()},
		},
	})
}
```

- [ ] **Step 4: Register routes**

Modify `internal/app/base/router/router.go`:

```go
func RegisterRoutes(f *flamego.Flame) {
	f.Get("/livez", handler.Livez)
	f.Get("/readyz", handler.Readyz)
	f.Get("/health", handler.Health)
}
```

- [ ] **Step 5: Run base tests**

Run:

```bash
rtk go test ./internal/app/base/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 8**

```bash
rtk git add internal/app/base/handler/health.go internal/app/base/handler/health_test.go internal/app/base/router/router.go
rtk git commit -m "feat(base): add liveness and readiness probes"
```

---

### Task 9: Update Realtime Protocol Documentation

**Files:**
- Modify: `docs/realtime/auction-sync-protocol.md`

- [ ] **Step 1: Add multi-pod reconnect contract**

Modify the `## Reconnect` section in `docs/realtime/auction-sync-protocol.md` so it reads:

```markdown
## Reconnect

Reconnect must not depend on returning to the same backend pod.

On reconnect, the client must:

1. Fetch room detail.
2. Fetch item detail.
3. Fetch ranking.
4. Request fresh WS tickets.
5. Reconnect in the mode active before disconnect: `all` for ordinary mode, or `control` then `market` for millisecond mode.
6. Wait for `auction_snapshot` from the new connection when an active room item exists.
7. Apply `auction_snapshot` as the authoritative state for the current item.
8. Resume incremental processing after the snapshot.

Clients must keep the largest `auction_version` seen for each `item_id`.

- If `auction_snapshot.auction_version` is greater than or equal to the local version, the snapshot replaces local auction state.
- If an incremental event has an older `auction_version`, it must not overwrite current price, leader, ranking, winner, or end time.
- `time_sync` only corrects clock offset and remaining time display. It must not overwrite an ended status, winner, or deal price.

If a bid HTTP request times out while the WebSocket disconnects, retry the bid with the same `idempotency_key`. The server-side Redis Lua path treats the retry as the same bid attempt.
```

- [ ] **Step 2: Review protocol wording**

Run:

```bash
rtk rg -n "same backend pod|auction_snapshot|auction_version|idempotency_key" docs/realtime/auction-sync-protocol.md
```

Expected: output includes all four terms in the reconnect section.

- [ ] **Step 3: Commit Task 9**

```bash
rtk git add docs/realtime/auction-sync-protocol.md
rtk git commit -m "docs(ws): clarify multi-pod reconnect contract"
```

---

### Task 10: Update K8s Deployment For Two Backend Replicas

**Files:**
- Modify: `deploy/k8s/11-app.yaml`

- [ ] **Step 1: Update Deployment spec**

Modify `deploy/k8s/11-app.yaml`:

```yaml
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
```

Keep the existing selector and template labels unchanged.

- [ ] **Step 2: Add pod termination and probes**

Inside the Deployment pod spec, add:

```yaml
      terminationGracePeriodSeconds: 30
```

Inside the `app` container, after `ports`, add:

```yaml
          startupProbe:
            httpGet:
              path: /livez
              port: 8080
            initialDelaySeconds: 3
            periodSeconds: 5
            failureThreshold: 12
          livenessProbe:
            httpGet:
              path: /livez
              port: 8080
            periodSeconds: 10
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            periodSeconds: 5
            failureThreshold: 3
```

- [ ] **Step 3: Validate manifest shape locally**

Run:

```bash
rtk kubectl kustomize deploy/k8s
```

Expected: command prints rendered YAML and includes `replicas: 2`, `/livez`, and `/readyz`.

If `kubectl` is unavailable locally, run:

```bash
rtk rg -n "replicas: 2|maxUnavailable: 0|maxSurge: 1|/livez|/readyz|terminationGracePeriodSeconds" deploy/k8s/11-app.yaml
```

Expected: every searched term is found.

- [ ] **Step 4: Commit Task 10**

```bash
rtk git add deploy/k8s/11-app.yaml
rtk git commit -m "deploy: run backend with two replicas"
```

---

### Task 11: Run Full Local Verification

**Files:**
- No source edits unless verification exposes a failure in files changed by Tasks 1-10.

- [ ] **Step 1: Run focused unit packages**

Run:

```bash
rtk go test ./internal/app/ws/... ./internal/core/cronlease ./internal/app/base/... -count=1
```

Expected: PASS.

- [ ] **Step 2: Run affected business packages**

Run:

```bash
rtk go test ./internal/app/item/... ./internal/app/order/... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run full test suite**

Run:

```bash
rtk go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 4: Build the backend**

Run:

```bash
rtk go build ./...
```

Expected: PASS with no compile errors.

- [ ] **Step 5: Commit any verification fixes**

Only if fixes were needed:

```bash
rtk git add <fixed-files>
rtk git commit -m "fix: stabilize multi-pod event bus verification"
```

Expected: no commit is needed if all previous tasks passed.

---

### Task 12: Agent-Guided Online Validation Plan

**Files:**
- Read first: `docs/agent-testing/README.md`
- Create report only if running online validation: `docs/agent-testing/reports/YYYYMMDD-HHMMSS-backend-multipod-event-bus.md`

- [ ] **Step 1: Enter agent testing docs through the required README**

Run:

```bash
rtk sed -n '1,220p' docs/agent-testing/README.md
```

Expected: README routes the worker to the runner and environment guides needed for online validation.

- [ ] **Step 2: Perform read-only preflight before applying any manifest**

Use the online ops skill and run only read-only checks first:

```bash
rtk ssh deploy@115.191.46.148 "kubectl get deployment live-auction-backend -n live-auction -o wide"
rtk ssh deploy@115.191.46.148 "kubectl get pods -n live-auction -l app=live-auction-backend -o wide"
rtk ssh deploy@115.191.46.148 "kubectl rollout status deployment/live-auction-backend -n live-auction --timeout=60s"
```

Expected: current deployment and pod state are captured before changes.

- [ ] **Step 3: Apply via the repository's deployment process**

Use the project's normal deployment pipeline or approved k3s apply workflow. Do not hand-edit secrets. Do not print secret values.

Expected: `live-auction-backend` reaches `2/2` ready replicas.

- [ ] **Step 4: Verify multi-pod readiness**

Run:

```bash
rtk ssh deploy@115.191.46.148 "kubectl get deployment live-auction-backend -n live-auction -o jsonpath='{.status.readyReplicas}{\"/\"}{.status.replicas}{\" ready\\n\"}'"
rtk ssh deploy@115.191.46.148 "kubectl get pods -n live-auction -l app=live-auction-backend -o wide"
```

Expected: `2/2 ready` and two backend pods.

- [ ] **Step 5: Run a batch-scoped WS and bid validation**

Follow the routed agent-testing flow for auction lifecycle or WS module. The validation must create its own room, item, users, deposit data, bids, and cleanup. Evidence must prove:

- WebSocket clients receive `auction_snapshot`.
- A bid event produced through HTTP reaches all active room clients.
- `auction_ended` reaches all active room clients.
- Winner can recover order state through API if `order_created` is missed during reconnect.

Expected: no online addresses, credentials, full tickets, passwords, or reusable tokens are written into the report.

- [ ] **Step 6: Verify cron lease behavior**

Use Prometheus or logs without exposing secrets. Confirm that lease metrics or logs show one acquired executor per job window and skipped leases for non-owner pods.

Expected: no evidence of every pod executing the same leased cron job in the same cadence window.

- [ ] **Step 7: Write validation report**

Create a report under `docs/agent-testing/reports/` with:

- Deployment image or commit tag.
- Replica count evidence.
- Test batch identifiers.
- WS event receipt summary.
- Reconnect snapshot summary.
- Cron lease summary.
- Cleanup summary.
- Redaction statement.

- [ ] **Step 8: Commit validation report**

```bash
rtk git add docs/agent-testing/reports/<report-file>.md
rtk git commit -m "test: validate backend multi-pod event bus"
```

---

## Self-Review

Spec coverage:

- Multi-replica backend: Tasks 4 and 10.
- Cross-pod room fanout and user unicast: Tasks 1-4.
- Event bus observability: Task 5.
- Distributed ranking rebuild coalescing: Task 6.
- Reconnect snapshot authority: Tasks 4 and 9, with online validation in Task 12.
- Cron duplication prevention: Task 7.
- Health probes: Task 8.
- K8s rollout settings: Task 10.
- Testing and online evidence: Tasks 11 and 12.

Placeholder scan:

- This plan uses concrete file paths, function names, commands, and expected results.
- No step depends on unspecified implementation details outside the files named in that task.

Type consistency:

- `bus.LocalDispatcher` requires `SendToRoom` and `SendToUser`.
- `hub.Hub` already has `SendToRoom`; Task 1 adds `SendToUser`.
- `bus.Broadcaster` implements `wsevent.Broadcaster`.
- `cronlease.Wrap` returns `func(context.Context)`, matching the function shape expected before `observability.WrapCron`.
