# Millisecond Auction Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a realtime auction sync path where server-side bidding remains the millisecond-authoritative source of truth, countdown/control events are isolated from high-volume market broadcasts, and clients can recover consistent ranking state after jitter or reconnect.

**Architecture:** Phase 1 keeps the current single backend instance and Hub, but supports adaptive WebSocket stream modes. Clients default to one backward-compatible `all` connection; when the room becomes hot or the auction timing requires tighter sync, the client automatically upgrades to two physical streams, `control` and `market`, then closes `all` after the split streams are ready. `control` carries countdown, auction lifecycle, snapshot, and low-frequency personal critical events; `market` carries high-volume bid/ranking state. Clients do not open a separate `user` connection in Phase 1; `user` remains only a future logical extension if personal event volume ever requires isolation. All auction state events carry a monotonic `auction_version` derived from Redis `bid_count`, so clients can ignore stale or out-of-order messages during normal delivery, upgrade, downgrade, and reconnect. Phase 2 multi-instance fanout is not implemented here; this plan only makes stream routing and event contracts multi-instance friendly.

**Tech Stack:** Go, Flamego, Gorilla WebSocket, Redis Lua hot auction state, existing `wsevent.Broadcaster`, existing Hub high/latest/normal lanes, OpenTelemetry metrics, agent performance runner.

---

## Scope And Non-Goals

This plan implements the first production-hardening slice:

- Split WS streams by purpose while preserving the existing `/ws/v1/rooms/{room_id}` default behavior.
- Keep the default client shape to one `all` connection for ordinary rooms and low-pressure viewing.
- Support automatic adaptive upgrade to `control` + `market` when the room is hot or the auction timing requires tighter sync.
- Allow one user to hold multiple same-room connections when each connection is for a different stream.
- Add `auction_version` to all auction state/control payloads that clients use to order state.
- Extend Redis Lua bid result to return the authoritative version for successful/idempotent bids.
- Keep `bid_success` coalescing, but formalize its payload as the latest market state for that 100ms flush window.
- Extend performance runner to compare `all` versus split `control+market` stream modes.
- Write protocol docs and regression report requirements.

Phase 1 stream policy:

| Stream | Default physical connection | Events |
| --- | --- | --- |
| `all` | Yes, default | Receives every event for ordinary rooms, low-pressure fallback, and old clients |
| `control` | Only after adaptive upgrade | `auction_snapshot`, `time_sync`, `auction_started`, `auction_extended`, `auction_ended`, `auction_cancelled`, `user_outbid`, `order_created` |
| `market` | Only after adaptive upgrade | `bid_success` and future ranking/price summary events |
| `user` | No | Reserved for Phase 2+ if personal event volume grows enough to justify a separate connection |

Adaptive client policy:

- Start with one `all` connection after entering a room.
- Upgrade to split mode automatically when the client observes hot-room pressure, such as at least 5 `bid_success` or market updates per second for 3 consecutive seconds, or the auction enters the last 30 seconds.
- Upgrade sequence:
  1. Keep `all` open.
  2. Open `control` and wait for `auction_snapshot` or the next `time_sync`.
  3. Open `market`.
  4. Close `all` after both split streams are healthy.
- Downgrade sequence:
  1. Open `all`.
  2. Wait for `auction_snapshot`.
  3. Close `control` and `market`.
- During upgrade/downgrade overlap, clients must dedupe by `auction_version` and event type so duplicate `all` and split-stream messages cannot roll state backward.

This plan explicitly does not implement multi-instance Pub/Sub/NATS fanout. Multi-instance is a Phase 2 plan after single-instance stream isolation passes regression.

## File Structure

- Create: `internal/app/ws/hub/stream.go`
  - Owns stream names, stream parsing, and event-to-stream classification.
- Modify: `internal/app/ws/hub/conn.go`
  - Adds `stream` to `Conn` and exposes it for Hub routing.
- Modify: `internal/app/ws/hub/hub.go`
  - Allows same user + same room + different stream connections, and filters delivery by stream.
- Modify: `internal/app/ws/handler/ws.go`
  - Parses `?stream=control|market|all`, defaulting to `all`; `user` remains reserved and maps to `control` in Phase 1.
- Modify: `internal/app/ws/hub/hub_test.go`
  - Covers stream classification, multi-stream registration, and stream filtering.
- Modify: `internal/app/item/cache/cache.go`
  - Adds `AuctionVersion` to `BidLuaResult`.
- Modify: `internal/app/item/cache/bid.go`
  - Returns `bid_count` from Lua as the authoritative auction version.
- Modify: `internal/app/item/dto/events.go`
  - Adds `auction_version` and market coalescing metadata to event payloads.
- Modify: `internal/app/item/service/bid_service.go`
  - Fills versions on `bid_success`, `user_outbid`, `auction_extended`, and `auction_ended`.
- Modify: `internal/app/item/service/bid_broadcast.go`
  - Preserves coalescing and records how many bids were absorbed into one market update.
- Modify: `internal/app/item/service/service.go`
  - Fills versions on `auction_snapshot` and `time_sync`.
- Modify: `internal/app/item/service/*_test.go`
  - Locks version propagation and coalescing semantics.
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
  - Adds split stream mode to the existing online performance runner.
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`
  - Tests stream mode URL generation and config parsing.
- Create: `docs/realtime/auction-sync-protocol.md`
  - Documents server-authoritative timing, stream roles, event ordering, and reconnect behavior for frontend.

## Task 1: Add WebSocket Stream Classification

**Files:**
- Create: `internal/app/ws/hub/stream.go`
- Modify: `internal/app/ws/hub/hub_test.go`

- [ ] **Step 1: Write failing stream classification tests**

Add to `internal/app/ws/hub/hub_test.go`:

```go
func TestParseConnStream(t *testing.T) {
	tests := []struct {
		raw  string
		want connStream
	}{
		{"", streamAll},
		{"all", streamAll},
		{"control", streamControl},
		{"market", streamMarket},
		{"user", streamControl},
		{"bad", streamAll},
	}
	for _, tt := range tests {
		if got := parseConnStream(tt.raw); got != tt.want {
			t.Fatalf("parseConnStream(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestClassifyEventStream(t *testing.T) {
	tests := []struct {
		eventType string
		want      connStream
	}{
		{"time_sync", streamControl},
		{"auction_snapshot", streamControl},
		{"auction_started", streamControl},
		{"auction_extended", streamControl},
		{"auction_ended", streamControl},
		{"auction_cancelled", streamControl},
		{"bid_success", streamMarket},
		{"user_outbid", streamControl},
		{"order_created", streamControl},
		{"unknown_event", streamMarket},
	}
	for _, tt := range tests {
		if got := classifyEventStream(tt.eventType); got != tt.want {
			t.Fatalf("classifyEventStream(%q) = %q, want %q", tt.eventType, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run 'TestParseConnStream|TestClassifyEventStream' -count=1
```

Expected: compile failure because `connStream`, `parseConnStream`, and `classifyEventStream` do not exist.

- [ ] **Step 3: Implement stream classification**

Create `internal/app/ws/hub/stream.go`:

```go
package hub

type connStream string

const (
	streamAll     connStream = "all"
	streamControl connStream = "control"
	streamMarket  connStream = "market"
)

func parseConnStream(raw string) connStream {
	switch connStream(raw) {
	case streamControl, streamMarket, streamAll:
		return connStream(raw)
	case "user":
		return streamControl
	default:
		return streamAll
	}
}

func classifyEventStream(eventType string) connStream {
	switch eventType {
	case "time_sync", "auction_snapshot", "auction_started", "auction_extended", "auction_ended", "auction_cancelled", "user_outbid", "order_created":
		return streamControl
	default:
		return streamMarket
	}
}

func streamAccepts(conn connStream, event connStream) bool {
	return conn == streamAll || conn == event
}
```

- [ ] **Step 4: Run the tests and verify GREEN**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run 'TestParseConnStream|TestClassifyEventStream' -count=1
```

Expected: PASS.

## Task 2: Add Stream-Aware Connections Without Breaking Existing Clients

**Files:**
- Modify: `internal/app/ws/hub/conn.go`
- Modify: `internal/app/ws/handler/ws.go`
- Modify: `internal/app/ws/hub/hub.go`
- Modify: `internal/app/ws/hub/hub_test.go`

- [ ] **Step 1: Write failing test for same user multi-stream connections**

Add to `internal/app/ws/hub/hub_test.go`:

```go
func TestRegisterAllowsSameUserSameRoomDifferentStreams(t *testing.T) {
	h := NewHub(nil)
	control := NewConnWithStream("conn_control", "user_1", "room_1", newFakeSocket(), h, streamControl)
	market := NewConnWithStream("conn_market", "user_1", "room_1", newFakeSocket(), h, streamMarket)

	h.Register(control)
	h.Register(market)

	h.mu.RLock()
	defer h.mu.RUnlock()
	if got := len(h.rooms["room_1"]); got != 2 {
		t.Fatalf("room connection count = %d, want 2", got)
	}
	if got := len(h.users["user_1"]); got != 2 {
		t.Fatalf("user connection count = %d, want 2", got)
	}
}

func TestRegisterReplacesSameUserSameRoomSameStream(t *testing.T) {
	h := NewHub(nil)
	first := NewConnWithStream("conn_1", "user_1", "room_1", newFakeSocket(), h, streamMarket)
	second := NewConnWithStream("conn_2", "user_1", "room_1", newFakeSocket(), h, streamMarket)

	h.Register(first)
	h.Register(second)

	h.mu.RLock()
	defer h.mu.RUnlock()
	if got := len(h.rooms["room_1"]); got != 1 {
		t.Fatalf("room connection count = %d, want 1", got)
	}
	if _, ok := h.rooms["room_1"]["conn_2"]; !ok {
		t.Fatalf("replacement connection not registered")
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run 'TestRegisterAllowsSameUserSameRoomDifferentStreams|TestRegisterReplacesSameUserSameRoomSameStream' -count=1
```

Expected: compile failure for `NewConnWithStream` or behavior failure because current replacement ignores stream.

- [ ] **Step 3: Add stream to `Conn` constructors**

Modify `internal/app/ws/hub/conn.go`:

```go
type Conn struct {
	id     string
	userID string
	roomID string
	stream connStream
	ws     socket
	high   chan wsevent.Event
	send   chan wsevent.Event
	hub    *Hub

	timeSyncMu      sync.Mutex
	latestTimeSync  *wsevent.Event
	timeSyncUpdated time.Time
	timeSyncNotify  chan struct{}

	closeMu   sync.RWMutex
	closeOnce sync.Once
	closed    bool
}

func NewConn(id, userID, roomID string, ws socket, hub *Hub) *Conn {
	return NewConnWithStream(id, userID, roomID, ws, hub, streamAll)
}

func NewConnWithStream(id, userID, roomID string, ws socket, hub *Hub, stream connStream) *Conn {
	return &Conn{
		id:     id,
		userID: userID,
		roomID: roomID,
		stream: stream,
		ws:     ws,
		high:   make(chan wsevent.Event, highBufSize),
		send:   make(chan wsevent.Event, sendBufSize),
		hub:    hub,

		timeSyncNotify: make(chan struct{}, 1),
	}
}
```

- [ ] **Step 4: Parse `stream` in the WebSocket handler**

Modify `internal/app/ws/handler/ws.go`:

```go
stream := wshub.ParseConnStream(r.URL.Query().Get("stream"))
conn := wshub.NewConnWithStream("conn_"+snowflake.MakeUUID(), userID, roomID, wsConn, hub, stream)
```

Export the parser in `stream.go`:

```go
func ParseConnStream(raw string) connStream {
	return parseConnStream(raw)
}
```

- [ ] **Step 5: Make replacement stream-aware**

Modify the replacement condition in `internal/app/ws/hub/hub.go`:

```go
for connID, existing := range h.rooms[c.roomID] {
	if existing.userID != c.userID || existing.stream != c.stream || existing.id == c.id {
		continue
	}
	delete(h.rooms[c.roomID], connID)
	h.removeUserConnLocked(existing)
	replaced = append(replaced, existing)
}
```

- [ ] **Step 6: Run focused tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run 'TestRegisterAllowsSameUserSameRoomDifferentStreams|TestRegisterReplacesSameUserSameRoomSameStream' -count=1
```

Expected: PASS.

## Task 3: Route Events Only To Matching Streams

**Files:**
- Modify: `internal/app/ws/hub/hub.go`
- Modify: `internal/app/ws/hub/hub_test.go`

- [ ] **Step 1: Write failing stream filtering test**

Add to `internal/app/ws/hub/hub_test.go`:

```go
func TestFanoutDeliversOnlyToMatchingRoomStreams(t *testing.T) {
	h := NewHub(nil)
	controlWS := newFakeSocket()
	marketWS := newFakeSocket()
	allWS := newFakeSocket()
	control := NewConnWithStream("conn_control", "user_1", "room_1", controlWS, h, streamControl)
	market := NewConnWithStream("conn_market", "user_2", "room_1", marketWS, h, streamMarket)
	all := NewConnWithStream("conn_all", "user_3", "room_1", allWS, h, streamAll)
	h.Register(control)
	h.Register(market)
	h.Register(all)

	h.SendToRoom("room_1", wsevent.Event{Type: "time_sync"})

	if len(controlWS.writtenEvents()) != 0 || len(marketWS.writtenEvents()) != 0 || len(allWS.writtenEvents()) != 0 {
		t.Fatalf("events should be queued but not written until write loop starts")
	}
	if len(control.high) != 0 {
		t.Fatalf("time_sync must not enter high queue")
	}
	if market.latestTimeSync != nil {
		t.Fatalf("market stream should not receive control time_sync")
	}
	if all.latestTimeSync == nil {
		t.Fatalf("all stream should receive time_sync for backward compatibility")
	}
	if control.latestTimeSync == nil {
		t.Fatalf("control stream should receive time_sync")
	}
}
```

- [ ] **Step 2: Run and verify RED**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run TestFanoutDeliversOnlyToMatchingRoomStreams -count=1
```

Expected: failure because current Hub delivers every room event to every room connection.

- [ ] **Step 3: Filter in `Hub.SendToRoom` and `Hub.Unicast`**

Modify `SendToRoom` loop in `internal/app/ws/hub/hub.go`:

```go
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
```

Modify `Unicast` similarly:

```go
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
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub -run 'TestFanoutDeliversOnlyToMatchingRoomStreams|TestFanoutDeliversToRoom|TestUnicastDeliversToUser' -count=1
```

Expected: PASS. Existing `all` stream behavior keeps old tests compatible.

## Task 4: Add Auction Version To Redis Lua Result

**Files:**
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/cache/bid.go`
- Modify: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Write failing service test for bid version propagation source**

Add to `internal/app/item/service/bid_service_test.go`:

```go
func TestPlaceBidUsesLuaBidCountAsAuctionVersion(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	itemID := "item_version"
	fc.states[itemID] = &itemcache.AuctionState{
		Status:       "ongoing",
		RoomID:       "room_1",
		CurrentPrice: 1000,
		DealPrice:    1000,
		EndTimeUnix:  time.Now().Add(time.Minute).Unix(),
		EndTimeUnixMS: time.Now().Add(time.Minute).UnixMilli(),
		BidIncrement: 100,
	}
	fc.bidLuaResult = &itemcache.BidLuaResult{
		Code:           0,
		BidID:          "bid_1",
		CurrentPrice:   1100,
		LeaderUserID:   "user_1",
		EndTimeUnix:    time.Now().Add(time.Minute).Unix(),
		EndTimeUnixMS:  time.Now().Add(time.Minute).UnixMilli(),
		AuctionVersion: 7,
		Status:         "ongoing",
	}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	svc.bidBroadcastDelay = time.Millisecond

	_, err := svc.PlaceBid(context.Background(), &usermodel.User{ID: "user_1", Name: "Alice"}, itemID, itemdto.PlaceBidInput{
		Price:          1100,
		IdempotencyKey: "idem_version",
		UserName:       "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	fanouts := waitForBidFanouts(t, fb, 1)
	payload, ok := fanouts[0].event.Payload.(itemdto.BidSuccessPayload)
	if !ok {
		t.Fatalf("expected BidSuccessPayload, got %T", fanouts[0].event.Payload)
	}
	if payload.AuctionVersion != 7 {
		t.Fatalf("auction_version = %d, want 7", payload.AuctionVersion)
	}
}
```

- [ ] **Step 2: Add `AuctionVersion` to `BidLuaResult`**

Modify `internal/app/item/cache/cache.go`:

```go
type BidLuaResult struct {
	Code             int
	BidID            string
	CurrentPrice     int64
	LeaderUserID     string
	EndTimeUnix      int64
	EndTimeUnixMS    int64
	IsExtended       bool
	IsCapped         bool
	PrevLeaderUserID string
	Status           string
	AuctionVersion   int64
}
```

- [ ] **Step 3: Return bid count from Lua**

Modify all Lua `return` arrays in `internal/app/item/cache/bid.go` to include `bid_cnt` as the last value. For success:

```lua
return {0, bid_id, price, user_id, end_unix, end_ms, is_extended, is_capped, prev_leader, result_status, bid_cnt}
```

For idempotent existing result, parse existing state bid count and return it:

```lua
local bid_cnt = tonumber(m['bid_count'] or 0)
return {1, existing, deal_price, m['leader_user_id'] or '', end_unix, end_ms, 0, 0, '', status, bid_cnt}
```

For rejection paths, append `0`:

```lua
return {2,'',0,'',0,0,0,0,'','',0}
return {3,'',0,'',0,0,0,0,'','',0}
return {4,'',0,'',0,0,0,0,'','',0}
```

- [ ] **Step 4: Parse version in Go**

Modify the result parser in `internal/app/item/cache/bid.go`:

```go
if len(res) > 10 {
	result.AuctionVersion = toI64(res[10])
}
```

- [ ] **Step 5: Run focused cache and service tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/cache ./internal/app/item/service -run 'TestPlaceBidUsesLuaBidCountAsAuctionVersion|TestPlaceBid' -count=1
```

Expected: PASS.

## Task 5: Add Version Fields To Realtime Payloads

**Files:**
- Modify: `internal/app/item/dto/events.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_broadcast.go`
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/service_test.go`
- Modify: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Add payload fields**

Modify `internal/app/item/dto/events.go`:

```go
type BidSuccessPayload struct {
	ItemID           string    `json:"item_id"`
	UserID           string    `json:"user_id"`
	Price            int64     `json:"price"`
	CurrentPrice     int64     `json:"current_price"`
	LeaderUserID     string    `json:"leader_user_id"`
	EndTime          time.Time `json:"end_time"`
	ServerTimeUnixMS int64     `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64     `json:"end_time_unix_ms"`
	AuctionVersion   int64     `json:"auction_version"`
	CoalescedBids    int64     `json:"coalesced_bids,omitempty"`
}

type TimeSyncPayload struct {
	ItemID           string `json:"item_id"`
	ServerTimeUnixMS int64  `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64  `json:"end_time_unix_ms"`
	Status           string `json:"status"`
	AuctionVersion   int64  `json:"auction_version"`
}
```

Add `AuctionVersion int64` to `AuctionStartedPayload`, `AuctionExtendedPayload`, `AuctionSnapshotPayload`, `UserOutbidPayload`, and `AuctionEndedPayload`.

- [ ] **Step 2: Fill bid and user event versions**

Modify `internal/app/item/service/bid_service.go` where payloads are created:

```go
AuctionVersion: luaResult.AuctionVersion,
```

For `UserOutbidPayload`, `AuctionExtendedPayload`, and price-cap `AuctionEndedPayload`, use the same `luaResult.AuctionVersion`.

- [ ] **Step 3: Preserve coalesced bid count in fanout**

Modify `internal/app/item/service/bid_broadcast.go` before fanout:

```go
payload := pending.payload
payload.CoalescedBids = pending.bids
err := s.broadcaster.Fanout(wsevent.RoomTopic(pending.roomID), wsevent.Event{
	Type:    dto.EventBidSuccess,
	Payload: payload,
})
```

- [ ] **Step 4: Fill snapshot and time_sync versions from Redis state**

Modify `auctionSnapshotFromState` in `internal/app/item/service/service.go`:

```go
AuctionVersion: int64(state.BidCount),
```

Modify `BroadcastTimeSync` payload:

```go
AuctionVersion: int64(state.BidCount),
```

For store fallback snapshots, use `0` because MySQL snapshot is not the hot bidding source:

```go
AuctionVersion: 0,
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run 'TestAuctionSnapshot|TestBroadcastTimeSync|TestPlaceBidUsesLuaBidCountAsAuctionVersion|TestPlaceBidCoalescesBidSuccessFanout' -count=1
```

Expected: PASS.

## Task 6: Extend Performance Runner For Split Stream Regression

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`

- [ ] **Step 1: Add stream mode config**

Modify runner `Config`:

```go
WSStreamMode string
```

Parse env in config:

```go
WSStreamMode: getenv("PERF_WS_STREAM_MODE", "all"),
```

Supported values:

```text
all
control_market
```

- [ ] **Step 2: Generate stream-aware WS URLs**

Change the WS connector to accept a stream:

```go
func connectWSForUserStream(ctx context.Context, cfg Config, data *TestData, userIndex int, stream string) (*websocketConn, error) {
	ticket, err := issueWSTicket(ctx, cfg, data.UserTokens[userIndex])
	if err != nil {
		return nil, err
	}
	wsURL := strings.Replace(cfg.BaseURL, "http", "ws", 1) + "/ws/v1/rooms/" + url.PathEscape(data.RoomID) + "?ticket=" + url.QueryEscape(ticket)
	if stream != "" && stream != "all" {
		wsURL += "&stream=" + url.QueryEscape(stream)
	}
	return dialWS(ctx, cfg, wsURL)
}
```

Keep existing `all` mode behavior by calling `connectWSForUserStream(..., "all")`.

- [ ] **Step 3: In `control_market` mode, open control and market connections**

For `PERF_WS_STREAM_MODE=control_market`, each target user opens:

```text
stream=control
stream=market
```

This is the upgraded high-pressure client shape, not the default room entry shape. Do not open a third `user` connection in Phase 1. Record control stream stats separately for two `time_sync` dimensions:

- arrival delay: `client_received_at - payload.server_time_unix_ms`.
- interval: the gap between consecutive `time_sync` messages received by the same control connection.

Arrival delay requires the load source clock to be NTP-synchronized with the backend host. If clock skew cannot be confirmed, report arrival delay as diagnostic-only and use interval plus server-side `ws_time_sync_write_lag_p95` for pass/fail.

The runner summary must print:

```text
WS_STREAM_MODE:
CONTROL_WS_CONNECTED:
MARKET_WS_CONNECTED:
CONTROL_TIME_SYNC_ARRIVAL_DELAY_P50:
CONTROL_TIME_SYNC_ARRIVAL_DELAY_P95:
CONTROL_TIME_SYNC_ARRIVAL_DELAY_P99:
CONTROL_TIME_SYNC_INTERVAL_P50:
CONTROL_TIME_SYNC_INTERVAL_P95:
CONTROL_TIME_SYNC_INTERVAL_P99:
```

- [ ] **Step 4: Add runner tests for URL mode**

Add to `main_test.go` a test that asserts generated URLs include `stream=control` and `stream=market` in split mode, and omit `stream` in `all` mode.

- [ ] **Step 5: Run runner tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

Expected: PASS.

## Task 7: Write Frontend/Protocol Contract

**Files:**
- Create: `docs/realtime/auction-sync-protocol.md`

- [ ] **Step 1: Create protocol doc**

Create `docs/realtime/auction-sync-protocol.md` with these sections:

```markdown
# Auction Realtime Sync Protocol

## Authority

The server is the only authority for bid acceptance, ranking, auction extension, and auction ending. Client countdowns are display-only.

## Streams

- `all`: default room entry stream; receives every event and is enough for ordinary rooms or low-pressure viewing.
- `control`: `auction_snapshot`, `time_sync`, `auction_started`, `auction_extended`, `auction_ended`, `auction_cancelled`, `user_outbid`, `order_created`.
- `market`: `bid_success` as latest market state after server coalescing.
- `user`: reserved future logical stream; Phase 1 clients do not open it, and `?stream=user` is treated as `control`.

## Adaptive Stream Mode

Clients start with one `all` connection. Millisecond auction sync is supported by default; clients automatically upgrade to split mode when room pressure or auction timing requires it.

Upgrade trigger examples:

- at least 5 `bid_success` or market updates per second for 3 consecutive seconds.
- the auction enters the last 30 seconds.

Upgrade sequence:

1. Keep `all` open.
2. Open `control` and wait for `auction_snapshot` or the next `time_sync`.
3. Open `market`.
4. Close `all` after `control` and `market` are healthy.

Downgrade sequence:

1. Open `all`.
2. Wait for `auction_snapshot`.
3. Close `control` and `market`.

During upgrade and downgrade overlap, clients must dedupe by `auction_version` and event type.

## Client Ordering

Clients must keep the largest `auction_version` seen for each `item_id`. A message with a lower `auction_version` must not overwrite current price, leader, ranking, or end time.

## Countdown

Clients compute:

```text
server_now = Date.now() + server_offset_ms
remaining_ms = end_time_unix_ms - server_now
```

`time_sync` corrects clock drift; it does not drive rendering one tick at a time.

## Reconnect

On reconnect, the client must:

1. Fetch room detail.
2. Fetch item detail.
3. Fetch ranking.
4. Request fresh WS tickets.
5. Reconnect in the mode active before disconnect: `all` for ordinary mode, or `control` then `market` for millisecond mode.
6. Apply `auction_snapshot` only if its `auction_version` is greater than or equal to the local version.
```

- [ ] **Step 2: Self-check protocol doc**

Run:

```bash
rtk rg -n "password|secret|credential" docs/realtime/auction-sync-protocol.md
```

Expected: no output.

## Task 8: Local Verification Gate

**Files:**
- All files changed by Tasks 1-7.

- [ ] **Step 1: Run focused unit tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/hub ./internal/app/item/cache ./internal/app/item/service ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader WS and item tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/... ./internal/app/item/... -count=1
```

Expected: PASS.

- [ ] **Step 3: Inspect stream-sensitive diff**

Run:

```bash
rtk git diff -- internal/app/ws internal/app/item docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws docs/realtime/auction-sync-protocol.md
```

Check:

- Existing clients without `stream` still receive all events.
- New clients start with one `all` connection and automatically switch to split mode only when hot-room pressure or auction timing triggers it.
- Same user can hold `control` and `market` connections in the same room.
- No Phase 1 test or runner path opens a third `user` connection by default.
- `control` stream does not receive `bid_success`.
- `market` stream does not receive `time_sync`.
- `all` stream remains backward compatible.
- `auction_version` is sourced from Redis hot state / Lua bid count.
- No report, doc, or test includes credentials or full online URLs.

## Task 9: Online Regression After Deployment

**Files:**
- Existing performance runner.
- New report under `docs/agent-testing/reports/`.

This task requires separate user approval under `docs/agent-testing` before execution.

- [ ] **Step 1: Run baseline-compatible all-stream regression**

After deployment, run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_perf_auction_20260605_all_stream_regression \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL="$PERF_ONLINE_BASE_URL" \
  PERF_PROMETHEUS_URL="$PERF_ONLINE_PROMETHEUS_URL" \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  PERF_REQUEST_MIX=core_bid_80_ranking_10_item_10 \
  PERF_WS_STREAM_MODE=all \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=760 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

- [ ] **Step 2: Run split stream regression**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_perf_auction_20260605_split_stream_regression \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL="$PERF_ONLINE_BASE_URL" \
  PERF_PROMETHEUS_URL="$PERF_ONLINE_PROMETHEUS_URL" \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  PERF_REQUEST_MIX=core_bid_80_ranking_10_item_10 \
  PERF_WS_STREAM_MODE=control_market \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=760 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

- [ ] **Step 3: Acceptance criteria**

The split stream run passes only if:

```text
load source clock skew confirmed or arrival delay marked diagnostic-only
CONTROL_TIME_SYNC_ARRIVAL_DELAY_P95 < 150ms
CONTROL_TIME_SYNC_ARRIVAL_DELAY_P99 < 300ms
CONTROL_TIME_SYNC_INTERVAL_P95 < 1.05s
CONTROL_TIME_SYNC_INTERVAL_P99 < 1.20s
WS connect failures = 0
ws_delivery dropped = 0
ws_time_sync_write_lag_p95 < 20ms
backend restarts = 0
business reconcile = ok
cleanup = ok
```

The all-stream run verifies default room entry and compatibility behavior. The split-stream run verifies the upgraded hot-room / millisecond-mode behavior.

## Phase 2 Multi-Instance Follow-Up

Do not start this until Phase 1 passes online regression.

Phase 2 should get its own plan with these components:

- Redis Pub/Sub or NATS room event bus.
- `room_id -> instance` or broadcast-to-all-instance fanout strategy.
- `user_id -> instance/conn` presence routing for low-frequency personal events if they are later split out of `control`.
- Event dedupe by `{item_id, auction_version, event_type}`.
- Sticky session decision for same room control/market streams.
- Cross-instance online count reconciliation.

Phase 1 deliberately makes this easier by giving every event a stream class and monotonic auction version.

## Final Verification

Before claiming complete:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/... ./internal/app/item/... ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

Expected: PASS.

Then write a report under:

```text
docs/agent-testing/reports/20260605-auction-millisecond-sync-regression.md
```

The report must include:

- all-stream versus split-stream comparison.
- adaptive mode upgrade/downgrade behavior.
- `CONTROL_TIME_SYNC_ARRIVAL_DELAY_P95/P99`.
- `CONTROL_TIME_SYNC_INTERVAL_P95/P99`.
- WS event counts by stream.
- `auction_version` ordering evidence.
- cleanup result.
- explicit note that multi-instance fanout remains Phase 2.
