# WebSocket Countdown Settlement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Redis-authoritative auction countdown, WebSocket synchronization, and atomic settlement with final `leader_user_id` / `deal_price` snapshots.

**Architecture:** Redis becomes the live source of truth for ongoing auctions. Bidding and settlement use Lua scripts against `auction:item:{item_id}:state` and `auction:ending`; MySQL is updated after Redis atomically claims the final result. WebSocket broadcasts `auction_snapshot`, `time_sync`, `auction_ended`, and `order_created` from Redis-derived state.

**Tech Stack:** Go, Redis, go-redis Lua scripts, GORM, Flamego, robfig/cron, Gorilla WebSocket, existing fake-store/fake-cache unit tests.

---

## Current Workspace Notes

- The branch is currently `main`; do not start implementation on `main`. Create or switch to `codex/ws-countdown-settlement` before code changes.
- There are existing uncommitted changes in:
  - `internal/app/item/service/bid_service.go`
  - `internal/app/item/service/bid_service_test.go`
- Those changes are a short-term fix for price-cap cleanup. The final design supersedes part of that behavior: price-cap settlement should keep a final Redis snapshot (`status=ended`) with TTL instead of deleting item state immediately.
- There are unrelated untracked files under `docs/agent-testing/`; do not edit, stage, or delete them.

## File Map

- Modify `internal/app/item/cache/cache.go`
  - Add live-state fields, end-time ZSET helpers, final snapshot TTL support, settlement Lua interface.
- Modify `internal/app/item/cache/bid.go`
  - Update bid Lua to use `status`, `deal_price`, `end_time_unix_ms`, `ended_at_unix_ms`, and `auction:ending`.
- Modify `internal/app/item/dto/events.go`
  - Add `time_sync` and `auction_snapshot` event constants/payloads; extend `auction_ended`.
- Modify `internal/app/item/dto/bid.go`
  - Add `deal_price` and `end_time_unix_ms` to bid response while keeping legacy fields compatible.
- Modify `internal/app/item/dto/item.go`
  - Add `deal_price`, `end_time_unix_ms`, `ended_at_unix_ms`, `end_reason`; map Redis state consistently.
- Modify `internal/app/item/service/service.go`
  - Initialize Redis authoritative state and ending ZSET on start; add settlement worker methods; update item/detail/list enrichment.
- Modify `internal/app/item/service/bid_service.go`
  - Use final snapshot semantics for price-cap settlement; broadcast new payloads.
- Modify `internal/app/item/init.go`
  - Register Redis settlement/time-sync jobs; demote old MySQL cron to fallback behavior.
- Modify `internal/app/ws/hub/hub.go`
  - Add room snapshot send on register and a `SendToRoom` helper used by fanout and tests.
- Modify `internal/app/ws/hub/conn.go`
  - Support server-side direct send of initial `auction_snapshot`.
- Modify tests:
  - `internal/app/item/service/service_test.go`
  - `internal/app/item/service/bid_service_test.go`
  - Add `internal/app/item/cache/cache_test.go` or extend cache tests if present.
  - `internal/app/ws/hub/hub_test.go`

## Task 1: Redis State Schema and Bid Lua

**Files:**
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/cache/bid.go`
- Test: `internal/app/item/service/service_test.go`
- Test: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Write failing fake-cache tests for canonical state fields**

Extend `TestStartItemInitializesRedisState` in `internal/app/item/service/service_test.go` so it asserts:

```go
if state.Status != "ongoing" {
	t.Fatalf("expected status ongoing, got %q", state.Status)
}
if state.DealPrice != 1000 {
	t.Fatalf("expected deal_price 1000 (start_price), got %d", state.DealPrice)
}
if state.EndTimeUnixMS != rule.EndTime.UnixMilli() {
	t.Fatalf("expected end_time_unix_ms %d, got %d", rule.EndTime.UnixMilli(), state.EndTimeUnixMS)
}
```

Add a fake-cache ending score assertion:

```go
if got := fc.ending[itemID]; got != rule.EndTime.UnixMilli() {
	t.Fatalf("expected ending score %d, got %d", rule.EndTime.UnixMilli(), got)
}
```

The fake cache will not have `Status`, `DealPrice`, `EndTimeUnixMS`, or `ending` yet, so this must fail to compile.

- [ ] **Step 2: Run the failing test**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run TestStartItemInitializesRedisState -count=1
```

Expected: compile failure for missing fields/methods.

- [ ] **Step 3: Add canonical Redis state fields and ending helpers**

In `internal/app/item/cache/cache.go`, extend `AuctionState`:

```go
type AuctionState struct {
	Status           string
	CurrentPrice     int64
	DealPrice        int64
	LeaderUserID     string
	EndTime          time.Time
	EndTimeUnixMS    int64
	EndedAtUnixMS    int64
	BidCount         int
	ParticipantCount int
	IsExtended       bool
	ExtendCount      int
	TotalExtendedSec int
	EndReason        string
}
```

Extend `Cache` with:

```go
ScheduleAuctionEnd(ctx context.Context, itemID string, endUnixMS int64) error
UnscheduleAuctionEnd(ctx context.Context, itemID string) error
```

Implement:

```go
func endingKey() string { return "auction:ending" }

func (c *RedisCache) ScheduleAuctionEnd(ctx context.Context, itemID string, endUnixMS int64) error {
	return c.client.ZAdd(ctx, endingKey(), redis.Z{Score: float64(endUnixMS), Member: itemID}).Err()
}

func (c *RedisCache) UnscheduleAuctionEnd(ctx context.Context, itemID string) error {
	return c.client.ZRem(ctx, endingKey(), itemID).Err()
}
```

Update `InitAuctionState` to write:

```go
"status", nonEmpty(state.Status, "ongoing"),
"current_price", state.CurrentPrice,
"deal_price", dealPriceOrCurrent(state),
"leader_user_id", state.LeaderUserID,
"end_time_unix", state.EndTime.Unix(),
"end_time_unix_ms", endUnixMS(state),
"ended_at_unix_ms", state.EndedAtUnixMS,
"bid_count", state.BidCount,
"participant_count", state.ParticipantCount,
"is_extended", boolToStr(state.IsExtended),
"extend_count", state.ExtendCount,
"total_extended_sec", state.TotalExtendedSec,
"end_reason", state.EndReason,
```

Update `GetAuctionState` so legacy `current_price` still works, but `DealPrice` is canonical:

```go
dealPrice := parseInt64(vals["deal_price"])
if dealPrice == 0 {
	dealPrice = parseInt64(vals["current_price"])
}
endMS := parseInt64(vals["end_time_unix_ms"])
if endMS == 0 {
	endMS = parseInt64(vals["end_time_unix"]) * 1000
}
```

- [ ] **Step 4: Update fake cache**

In `internal/app/item/service/service_test.go`, add:

```go
ending map[string]int64
```

Initialize it in `newFakeCache`.

Set canonical fields in `InitAuctionState`:

```go
if cp.Status == "" {
	cp.Status = "ongoing"
}
if cp.DealPrice == 0 {
	cp.DealPrice = cp.CurrentPrice
}
if cp.EndTimeUnixMS == 0 {
	cp.EndTimeUnixMS = cp.EndTime.UnixMilli()
}
```

Add fake methods:

```go
func (c *fakeCache) ScheduleAuctionEnd(_ context.Context, itemID string, endUnixMS int64) error {
	c.ending[itemID] = endUnixMS
	return nil
}

func (c *fakeCache) UnscheduleAuctionEnd(_ context.Context, itemID string) error {
	delete(c.ending, itemID)
	return nil
}
```

- [ ] **Step 5: Run test and fix StartItem scheduling**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run TestStartItemInitializesRedisState -count=1
```

Expected initially: fail because `StartItem` does not call `ScheduleAuctionEnd`.

Modify `internal/app/item/service/service.go` in `StartItem` after `InitAuctionState`:

```go
if err := s.cache.ScheduleAuctionEnd(ctx, item.ID, rule.EndTime.UnixMilli()); err != nil {
	_ = s.cache.DeleteAuctionState(ctx, item.ID)
	return err
}
```

Run the test again. Expected: PASS.

- [ ] **Step 6: Update bid fake and Lua semantics**

In fake `PlaceBidLua`, update canonical state:

```go
state.CurrentPrice = args.Price
state.DealPrice = args.Price
state.LeaderUserID = args.UserID
state.EndTimeUnixMS = state.EndTime.UnixMilli()
```

When extension occurs:

```go
state.EndTimeUnixMS = state.EndTime.UnixMilli()
c.ending[itemID] = state.EndTimeUnixMS
```

When capped:

```go
state.Status = "ended"
state.EndedAtUnixMS = time.Unix(args.NowUnix, 0).UnixMilli()
state.EndReason = "price_cap"
delete(c.ending, itemID)
```

In real `bid.go` Lua:

- Read `status`; reject unless empty legacy state or `ongoing`.
- Use `deal_price` in addition to `current_price`.
- Use `end_time_unix_ms` for comparison; keep `now_unix` argument for compatibility by deriving `now_ms = now_unix * 1000`.
- On extension, update both `end_time_unix` and `end_time_unix_ms`.
- On price cap, write `status=ended`, `ended_at_unix_ms`, `end_reason=price_cap`.
- Return `EndTimeUnixMS` in the Go result struct.

- [ ] **Step 7: Run item service tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit task**

Commit only files touched by this task:

```bash
rtk git add internal/app/item/cache/cache.go internal/app/item/cache/bid.go internal/app/item/service/service.go internal/app/item/service/service_test.go internal/app/item/service/bid_service_test.go
rtk git commit -m "feat: add redis authoritative auction state"
```

## Task 2: DTOs and WebSocket Event Contracts

**Files:**
- Modify: `internal/app/item/dto/events.go`
- Modify: `internal/app/item/dto/bid.go`
- Modify: `internal/app/item/dto/item.go`
- Test: `internal/app/item/service/service_test.go`
- Test: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Write failing DTO/service assertions**

Add to `TestGetItemEnrichesFromCacheWhenOngoing`:

```go
if detail.DealPrice != 5000 {
	t.Fatalf("expected deal_price 5000 from Redis, got %d", detail.DealPrice)
}
if detail.EndTimeUnixMS == 0 {
	t.Fatal("expected end_time_unix_ms from Redis")
}
```

Add to `TestPlaceBidSucceeds`:

```go
if result.DealPrice != 100 {
	t.Fatalf("expected deal_price 100, got %d", result.DealPrice)
}
if result.EndTimeUnixMS == 0 {
	t.Fatal("expected end_time_unix_ms")
}
```

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run 'Test(GetItemEnrichesFromCacheWhenOngoing|PlaceBidSucceeds)' -count=1
```

Expected: compile failure for missing DTO fields.

- [ ] **Step 2: Add event payloads**

In `events.go`, add:

```go
const (
	EventTimeSync        = "time_sync"
	EventAuctionSnapshot = "auction_snapshot"
)

type TimeSyncPayload struct {
	ItemID           string `json:"item_id"`
	ServerTimeUnixMS int64  `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64  `json:"end_time_unix_ms"`
	Status           string `json:"status"`
}

type AuctionSnapshotPayload struct {
	ItemID           string `json:"item_id"`
	Status           string `json:"status"`
	ServerTimeUnixMS int64  `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64  `json:"end_time_unix_ms"`
	EndedAtUnixMS    int64  `json:"ended_at_unix_ms,omitempty"`
	LeaderUserID     string `json:"leader_user_id"`
	DealPrice        int64  `json:"deal_price"`
	BidCount         int    `json:"bid_count"`
	ParticipantCount int    `json:"participant_count"`
	EndReason        string `json:"end_reason,omitempty"`
}
```

Extend `AuctionEndedPayload` with these fields while keeping the existing `WinnerUserID` field for API compatibility:

```go
LeaderUserID  string `json:"leader_user_id"`
DealPrice     int64  `json:"deal_price"`
EndedAtUnixMS int64  `json:"ended_at_unix_ms,omitempty"`
EndReason     string `json:"end_reason,omitempty"`
```

New code must set both `WinnerUserID` and `LeaderUserID` from the same final leader during migration.

- [ ] **Step 3: Add bid response fields**

In `bid.go`, extend `PlaceBidResult`:

```go
DealPrice     int64 `json:"deal_price"`
EndTimeUnixMS int64 `json:"end_time_unix_ms"`
```

Return both legacy and canonical fields in `PlaceBid`.

- [ ] **Step 4: Add item DTO fields**

In `item.go`, add fields to `ItemListDTO`, `ItemDetailDTO`, `MerchantItemDTO`, and `ProgressDTO`:

```go
DealPrice     int64  `json:"deal_price"`
EndTimeUnixMS int64  `json:"end_time_unix_ms"`
EndedAtUnixMS int64  `json:"ended_at_unix_ms,omitempty"`
EndReason     string `json:"end_reason,omitempty"`
```

Update `applyStateToDetail`, `applyStateToList`, and merchant progress enrichment:

```go
d.DealPrice = state.DealPrice
d.EndTimeUnixMS = state.EndTimeUnixMS
d.EndedAtUnixMS = state.EndedAtUnixMS
d.EndReason = state.EndReason
```

For legacy fallback, `DealPrice` should equal current price while ongoing.

- [ ] **Step 5: Run DTO tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run 'Test(GetItemEnrichesFromCacheWhenOngoing|PlaceBidSucceeds)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit task**

```bash
rtk git add internal/app/item/dto/events.go internal/app/item/dto/bid.go internal/app/item/dto/item.go internal/app/item/service/service.go internal/app/item/service/bid_service.go internal/app/item/service/service_test.go internal/app/item/service/bid_service_test.go
rtk git commit -m "feat: expose auction countdown snapshot fields"
```

## Task 3: Redis Settlement Worker

**Files:**
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/init.go`
- Test: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Write failing settlement tests**

Add `TestSettleDueAuctionsMarksEndedAndKeepsSnapshot`:

```go
func TestSettleDueAuctionsMarksEndedAndKeepsSnapshot(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	endTime := time.Now().Add(time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_abc", 0, 100, 0, endTime)
	fc.states[itemID].LeaderUserID = "user_winner"
	fc.states[itemID].DealPrice = 500
	fc.states[itemID].CurrentPrice = 500
	fc.ending[itemID] = time.Now().Add(-time.Second).UnixMilli()
	svc.now = func() time.Time { return time.Now() }

	svc.SettleDueAuctions(context.Background())

	item := store.items[itemID]
	if item.Status != itemmodel.ItemEnded {
		t.Fatalf("expected ended item, got %q", item.Status)
	}
	if item.WinnerID != "user_winner" || item.DealPrice != 500 {
		t.Fatalf("expected winner/deal user_winner/500, got %q/%d", item.WinnerID, item.DealPrice)
	}
	state := fc.states[itemID]
	if state == nil || state.Status != "ended" {
		t.Fatalf("expected ended redis snapshot, got %+v", state)
	}
}
```

Expected: compile failure because `SettleDueAuctions` does not exist.

- [ ] **Step 2: Add settlement cache interface**

In `cache.go`, add:

```go
type SettlementResult struct {
	ItemID        string
	LeaderUserID  string
	DealPrice     int64
	EndedAtUnixMS int64
	EndReason     string
}

ListDueAuctionEnds(ctx context.Context, nowUnixMS int64, limit int) ([]string, error)
SettleAuctionLua(ctx context.Context, itemID string, nowUnixMS int64) (*SettlementResult, bool, error)
```

Implement Redis `ListDueAuctionEnds` with `ZRangeByScore`.

Implement settlement Lua:

```lua
local state_key = KEYS[1]
local ending_key = KEYS[2]
local now_ms = tonumber(ARGV[1])
local item_id = ARGV[2]
local raw = redis.call('HGETALL', state_key)
if #raw == 0 then return {0} end
local s = {}
for i = 1, #raw, 2 do s[raw[i]] = raw[i+1] end
if s['status'] ~= 'ongoing' then return {0} end
local end_ms = tonumber(s['end_time_unix_ms'] or '0')
if end_ms == 0 then end_ms = tonumber(s['end_time_unix'] or '0') * 1000 end
if now_ms < end_ms then return {0} end
local leader = s['leader_user_id'] or ''
local deal = tonumber(s['deal_price'] or s['current_price'] or '0')
redis.call('HSET', state_key,
  'status', 'ended',
  'ended_at_unix_ms', now_ms,
  'end_reason', 'time_expired',
  'deal_price', deal)
redis.call('ZREM', ending_key, item_id)
return {1, leader, deal, now_ms, 'time_expired'}
```

- [ ] **Step 3: Implement fake settlement methods**

In fake cache:

```go
func (c *fakeCache) ListDueAuctionEnds(_ context.Context, nowUnixMS int64, limit int) ([]string, error) {
	var ids []string
	for id, score := range c.ending {
		if score <= nowUnixMS {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (c *fakeCache) SettleAuctionLua(_ context.Context, itemID string, nowUnixMS int64) (*itemcache.SettlementResult, bool, error) {
	state := c.states[itemID]
	if state == nil || state.Status != "ongoing" || nowUnixMS < state.EndTimeUnixMS {
		return nil, false, nil
	}
	state.Status = "ended"
	state.EndedAtUnixMS = nowUnixMS
	state.EndReason = "time_expired"
	delete(c.ending, itemID)
	return &itemcache.SettlementResult{
		ItemID: itemID, LeaderUserID: state.LeaderUserID, DealPrice: state.DealPrice,
		EndedAtUnixMS: nowUnixMS, EndReason: "time_expired",
	}, true, nil
}
```

- [ ] **Step 4: Implement service settlement**

In `service.go`, add:

```go
func (s *Service) SettleDueAuctions(ctx context.Context) {
	if s.cache == nil {
		s.EndExpiredAuctions(ctx)
		return
	}
	now := s.now()
	ids, err := s.cache.ListDueAuctionEnds(ctx, now.UnixMilli(), 50)
	if err != nil {
		logx.Warnw("item.SettleDueAuctions list failed", "err", err)
		return
	}
	for _, itemID := range ids {
		result, ok, err := s.cache.SettleAuctionLua(ctx, itemID, now.UnixMilli())
		if err != nil || !ok || result == nil {
			if err != nil {
				logx.Warnw("item.SettleDueAuctions settle failed", "item_id", itemID, "err", err)
			}
			continue
		}
		s.persistSettledAuction(ctx, result)
	}
}
```

Extract shared persistence/broadcast/order code from `EndExpiredAuctions` into `persistSettledAuction(ctx, result)` so Redis settlement and fallback settlement use the same MySQL/order/WS behavior.

- [ ] **Step 5: Register worker**

In `item/init.go`, replace primary one-minute live settlement with:

```go
engine.Cron.AddFunc("@every 1s", observability.WrapCron("item.settle_due_auctions", svc.SettleDueAuctions))
engine.Cron.AddFunc("@every 1m", observability.WrapCron("item.end_expired_auctions_fallback", svc.EndExpiredAuctions))
```

Fallback must not settle an item early if Redis state exists with a later `end_time_unix_ms`.

- [ ] **Step 6: Run settlement tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run 'TestSettleDueAuctions|TestEndExpiredAuctions' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit task**

```bash
rtk git add internal/app/item/cache/cache.go internal/app/item/service/service.go internal/app/item/service/service_test.go internal/app/item/init.go
rtk git commit -m "feat: settle due auctions from redis"
```

## Task 4: WebSocket Snapshot and Time Sync

**Files:**
- Modify: `internal/app/ws/hub/hub.go`
- Modify: `internal/app/ws/hub/conn.go`
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/init.go`
- Test: `internal/app/ws/hub/hub_test.go`
- Test: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Add hub direct-send support tests**

In `hub_test.go`, add a test that registers a connection and calls a new method:

```go
func TestSendToRoomDeliversEvent(t *testing.T) {
	h := NewHub(nil)
	c := newTestConn("conn_1", "user_1", "room_1")
	h.Register(c)
	evt := wsevent.Event{Type: "auction_snapshot", Payload: map[string]any{"item_id": "item_1"}}
	h.SendToRoom("room_1", evt)
	select {
	case got := <-c.send:
		if got.Type != "auction_snapshot" {
			t.Fatalf("expected auction_snapshot, got %q", got.Type)
		}
	default:
		t.Fatal("expected event delivered")
	}
}
```

Expected: compile failure for missing `SendToRoom`.

- [ ] **Step 2: Add hub room send method**

In `hub.go`, implement:

```go
func (h *Hub) SendToRoom(roomID string, event wsevent.Event) {
	h.mu.RLock()
	room := h.rooms[roomID]
	h.mu.RUnlock()
	for _, c := range room {
		h.deliver(c, event)
	}
}
```

Keep `Fanout` as the interface method and have it call `SendToRoom`.

- [ ] **Step 3: Add service time-sync broadcast**

In `service.go`, add:

```go
func (s *Service) BroadcastTimeSync(ctx context.Context) {
	if s.cache == nil || s.broadcaster == nil {
		return
	}
	nowMS := s.now().UnixMilli()
	// List active items from auction:ending with a high upper bound or add cache helper ListActiveAuctionEnds.
	// For each item, read state and fanout dto.EventTimeSync with item_id/server_time/end_time/status.
}
```

Prefer a cache helper:

```go
ListActiveAuctionEnds(ctx context.Context, limit int) ([]string, error)
```

For the first implementation, limit to 200 active items per tick.

- [ ] **Step 4: Add snapshot builder**

In `service.go`, add:

```go
func (s *Service) AuctionSnapshot(ctx context.Context, itemID string) (*dto.AuctionSnapshotPayload, bool, error)
```

It reads Redis state first. If Redis misses, fallback to MySQL detail. It returns `false` when no useful snapshot exists.

- [ ] **Step 5: Wire snapshot on WebSocket register**

Keep dependencies one-way to avoid an import cycle. Do not import `item/service` into `ws/hub`. Instead, add a snapshot provider interface to `ws/hub`:

```go
type SnapshotProvider interface {
	SnapshotForRoom(ctx context.Context, roomID string) (*wsevent.Event, bool, error)
}
```

Add `SetSnapshotProvider(provider SnapshotProvider)` to hub. On `Register`, after Redis online sync, call provider and deliver the event to the new connection.

In item load or after service construction, register a provider that reads `auction:room:{room_id}:state.current_item_id` / room current item and builds `auction_snapshot`.

- [ ] **Step 6: Register time-sync cron**

In `item/init.go`, add:

```go
engine.Cron.AddFunc("@every 1s", observability.WrapCron("item.broadcast_time_sync", svc.BroadcastTimeSync))
```

- [ ] **Step 7: Run WebSocket and item tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/... ./internal/app/item/service -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit task**

```bash
rtk git add internal/app/ws/hub/hub.go internal/app/ws/hub/conn.go internal/app/ws/hub/hub_test.go internal/app/item/service/service.go internal/app/item/service/service_test.go internal/app/item/init.go
rtk git commit -m "feat: sync auction countdown over websocket"
```

## Task 5: Price-Cap Settlement Final Snapshot

**Files:**
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_service_test.go`
- Test: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Update failing price-cap test expectation**

Change `TestPlaceBidPriceCapEndsAuction` so price-cap settlement expects Redis state to remain with ended status:

```go
state := fc.states[itemID]
if state == nil {
	t.Fatal("expected final auction state snapshot to remain in cache")
}
if state.Status != "ended" {
	t.Fatalf("expected redis state status ended, got %q", state.Status)
}
if state.LeaderUserID != "user_1" || state.DealPrice != 500 {
	t.Fatalf("expected final leader/deal user_1/500, got %q/%d", state.LeaderUserID, state.DealPrice)
}
if state.EndReason != "price_cap" {
	t.Fatalf("expected end_reason price_cap, got %q", state.EndReason)
}
```

Remove the old assertion that cache state is deleted.

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run TestPlaceBidPriceCapEndsAuction -count=1
```

Expected: fail if service still deletes Redis state.

- [ ] **Step 2: Preserve final state in price-cap path**

In `PlaceBid`, when `luaResult.IsCapped`:

- Keep `ClearRoomCurrentItem` in MySQL and Redis.
- Keep `RemoveFromRoomQueue`.
- Replace `DeleteAuctionState` with final snapshot preservation. The Lua script should already set `status=ended`; Go should not delete it.
- Call `UnscheduleAuctionEnd`.

Broadcast `auction_ended` with:

```go
Payload: dto.AuctionEndedPayload{
	ItemID: item.ID,
	WinnerUserID: current.ID,
	LeaderUserID: current.ID,
	DealPrice: input.Price,
	EndedAtUnixMS: s.now().UnixMilli(),
	EndReason: "price_cap",
}
```

- [ ] **Step 3: Run price-cap tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/service -run TestPlaceBidPriceCapEndsAuction -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit task**

```bash
rtk git add internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go
rtk git commit -m "feat: keep final snapshot for price cap settlement"
```

## Task 6: Full Verification and Spec Review

**Files:**
- Modify only if verification reveals a concrete bug.

- [ ] **Step 1: Run focused verification**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/... ./internal/app/ws/... ./internal/app/order/... -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full build/test smoke**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./... -count=1
```

Expected: PASS, or document unrelated failures with exact package and error.

- [ ] **Step 3: Verify spec coverage**

Read `docs/superpowers/specs/2026-05-31-ws-countdown-settlement-design.md` and verify each requirement has an implementation:

- Redis state has `status`, `leader_user_id`, `deal_price`, `end_time_unix_ms`, `ended_at_unix_ms`, `end_reason`.
- `auction:ending` is updated on start, extension, price cap, and time expiry.
- Reconnect snapshot exists.
- `time_sync` exists.
- Time-expired settlement is Redis-driven.
- Price-cap settlement keeps final snapshot.
- WebSocket `auction_ended` carries final `leader_user_id` and `deal_price`.

- [ ] **Step 4: Commit verification fixes**

If Step 1-3 required changes, commit the exact changed files. Use this command shape with the concrete file list from `rtk git status --short`:

```bash
rtk git add internal/app/item/cache/cache.go internal/app/item/cache/bid.go internal/app/item/service/service.go internal/app/item/service/bid_service.go internal/app/item/dto/events.go internal/app/item/dto/bid.go internal/app/item/dto/item.go internal/app/ws/hub/hub.go internal/app/ws/hub/conn.go internal/app/item/init.go
rtk git commit -m "fix: complete websocket countdown settlement"
```

If no changes are needed, do not create an empty commit.
