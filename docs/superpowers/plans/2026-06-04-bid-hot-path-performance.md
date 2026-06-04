# Bid Hot Path Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move active bidding off the MySQL hot path by using Redis hot auction state and Redis Stream backed asynchronous bid-log persistence.

**Architecture:** Redis remains the atomic real-time source for ongoing auctions. `PlaceBid` reads hot item/rule fields from Redis, executes Lua, appends successful bid logs to a Redis Stream, and returns before MySQL insert. A module-owned worker consumes stream entries in batches and persists `bid_logs` idempotently.

**Tech Stack:** Go, GORM, Redis Lua, Redis Streams, existing `dao.Store`/`cache.Cache`/service fake-test patterns, OpenTelemetry metrics.

---

## Scope And File Map

Create or modify these files only:

- Modify `internal/app/item/cache/cache.go`: extend `AuctionState`, add hot config and bid-log stream event types/methods to the cache interface.
- Modify `internal/app/item/cache/bid.go`: update Lua argument sourcing if needed and add Redis Stream append helper in a focused section or helper file.
- Create `internal/app/item/cache/bid_log_stream.go`: Redis Stream append and consumer helper functions.
- Modify `internal/app/item/dao/item.go`: add `CreateBidLogs`.
- Modify `internal/app/item/dao/bid_log.go`: implement idempotent batch insert.
- Modify `internal/app/item/service/service.go`: initialize hot fields in `StartItem`; hold worker lifecycle fields if needed.
- Modify `internal/app/item/service/bid_service.go`: use hot state, stream append, and remove synchronous `CreateBidLog` from the HTTP hot path.
- Create `internal/app/item/service/bid_log_worker.go`: worker loop, batch flush, retry, and shutdown behavior.
- Modify `internal/app/item/init.go`: start the worker from module load using `engine.Context`.
- Modify `internal/core/observability/metrics.go`: add metrics for hot state lookup, stream append, and worker persistence.
- Modify tests in `internal/app/item/service/bid_service_test.go` and `internal/app/item/service/service_test.go`.
- Add focused tests in `internal/app/item/service/bid_log_worker_test.go`.
- Add or modify DAO tests only if a local fake DB pattern already exists; otherwise keep DAO batch insert covered by service worker fakes and a later approved integration run.

Do not modify production manifests or deploy anything in this plan.

## Implementation Prerequisite: Worktree Isolation

Before Task 1, use `superpowers:using-git-worktrees`.

- [ ] Detect whether the current checkout is already a linked worktree.
- [ ] If it is not already isolated, create a feature worktree on a `codex/` branch before editing code.
- [ ] Run the local baseline test command in the worktree before the first code change:

```bash
rtk go test ./internal/app/item/... ./internal/core/observability/...
```

Do not mix this implementation with the current checkout's unrelated dirty files.

## Task 1: Extend Redis Auction State With Hot Bid Fields

**Files:**
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/service/service.go`
- Test: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Write the failing service test for hot fields on start**

Add this test near the existing `StartItem` tests in `internal/app/item/service/service_test.go`:

```go
func TestStartItemInitializesHotBidFields(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	item := &itemmodel.AuctionItem{
		ID:         "item_hot",
		MerchantID: "merchant_1",
		RoomID:     "room_1",
		Status:     itemmodel.ItemPublished,
		RuleID:     "rule_hot",
	}
	rule := &itemmodel.AuctionRule{
		ID:           "rule_hot",
		ItemID:       "item_hot",
		StartPrice:   1000,
		BidIncrement: 100,
		PriceCap:     5000,
		StartTime:    now.Add(-time.Minute),
		EndTime:      now.Add(10 * time.Minute),
	}
	store.items[item.ID] = item
	store.rules[rule.ID] = rule
	svc := NewService(store, itemdto.AuctionPolicy{
		ExtendTriggerSec:  30,
		AutoExtendSec:     20,
		MaxExtendCount:    3,
		MaxTotalExtendSec: 60,
	}, cache, nil, nil, nil)
	svc.now = func() time.Time { return now }

	err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, item.ID)
	if err != nil {
		t.Fatalf("StartItem returned error: %v", err)
	}

	state := cache.states[item.ID]
	if state.RoomID != "room_1" {
		t.Fatalf("expected hot state room_id room_1, got %q", state.RoomID)
	}
	if state.BidIncrement != 100 || state.PriceCap != 5000 {
		t.Fatalf("expected rule hot fields, got increment=%d cap=%d", state.BidIncrement, state.PriceCap)
	}
	if state.ExtendTriggerSec != 30 || state.AutoExtendSec != 20 || state.MaxExtendCount != 3 || state.MaxTotalExtendSec != 60 {
		t.Fatalf("expected policy hot fields, got %+v", state)
	}
}
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run:

```bash
rtk go test ./internal/app/item/service -run TestStartItemInitializesHotBidFields -count=1
```

Expected: FAIL because `AuctionState` has no `RoomID`, `BidIncrement`, `PriceCap`, or policy hot fields yet.

- [ ] **Step 3: Extend `AuctionState`**

Add these fields to `internal/app/item/cache/cache.go` inside `AuctionState`:

```go
RoomID             string
BidIncrement       int64
PriceCap           int64
DepositAmount      int64
ExtendTriggerSec   int
AutoExtendSec      int
MaxExtendCount     int
MaxTotalExtendSec  int
```

Update `RedisCache.InitAuctionState` to write them:

```go
"room_id", state.RoomID,
"bid_increment", state.BidIncrement,
"price_cap", state.PriceCap,
"deposit_amount", state.DepositAmount,
"extend_trigger_sec", state.ExtendTriggerSec,
"auto_extend_sec", state.AutoExtendSec,
"max_extend_count", state.MaxExtendCount,
"max_total_extend_sec", state.MaxTotalExtendSec,
```

Update `RedisCache.GetAuctionState` to parse them:

```go
RoomID:            vals["room_id"],
BidIncrement:      parseInt64(vals["bid_increment"]),
PriceCap:          parseInt64(vals["price_cap"]),
DepositAmount:     parseInt64(vals["deposit_amount"]),
ExtendTriggerSec:  parseInt(vals["extend_trigger_sec"]),
AutoExtendSec:     parseInt(vals["auto_extend_sec"]),
MaxExtendCount:    parseInt(vals["max_extend_count"]),
MaxTotalExtendSec: parseInt(vals["max_total_extend_sec"]),
```

- [ ] **Step 4: Write hot fields from `StartItem`**

In `internal/app/item/service/service.go`, change the `state := itemcache.AuctionState{...}` block in `StartItem` to:

```go
state := itemcache.AuctionState{
	Status:            string(model.ItemOngoing),
	RoomID:            item.RoomID,
	CurrentPrice:      rule.StartPrice,
	DealPrice:         rule.StartPrice,
	EndTime:           rule.EndTime,
	EndTimeUnixMS:     rule.EndTime.UnixMilli(),
	BidIncrement:      rule.BidIncrement,
	PriceCap:          rule.PriceCap,
	DepositAmount:     rule.DepositAmount,
	ExtendTriggerSec:  s.policy.ExtendTriggerSec,
	AutoExtendSec:     s.policy.AutoExtendSec,
	MaxExtendCount:    s.policy.MaxExtendCount,
	MaxTotalExtendSec: s.policy.MaxTotalExtendSec,
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestStartItemInitializesHotBidFields|TestStartItem' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add internal/app/item/cache/cache.go internal/app/item/service/service.go internal/app/item/service/service_test.go
rtk git commit -m "feat(item): store bid hot fields in auction state"
```

## Task 2: Add Hot State Lookup And Rebuild Path

**Files:**
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/service/bid_service.go`
- Test: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Add failing test for hot state hit avoiding store lookup**

In `internal/app/item/service/bid_service_test.go`, add:

```go
func TestPlaceBidUsesHotStateWithoutStoreLookup(t *testing.T) {
	store := newFakeStore()
	cache := newFakeBidCache()
	cache.hotState = &itemcache.AuctionState{
		Status:            string(itemmodel.ItemOngoing),
		RoomID:            "room_1",
		BidIncrement:      100,
		PriceCap:          0,
		DepositAmount:     0,
		ExtendTriggerSec:  30,
		AutoExtendSec:     20,
		MaxExtendCount:    3,
		MaxTotalExtendSec: 60,
		EndTimeUnixMS:     time.Now().Add(time.Minute).UnixMilli(),
	}
	cache.luaResult = &itemcache.BidLuaResult{
		Code:         0,
		BidID:        "bid_hot",
		CurrentPrice: 1100,
		LeaderUserID: "user_1",
		EndTimeUnixMS: cache.hotState.EndTimeUnixMS,
		Status:       "ongoing",
	}
	svc := NewService(store, itemdto.DefaultAuctionPolicy(), cache, nil, nil, nil)

	result, err := svc.PlaceBid(context.Background(), &usermodel.User{ID: "user_1", Name: "User 1"}, "item_1", itemdto.PlaceBidInput{
		Price:          1100,
		IdempotencyKey: "idem_hot",
		UserName:       "User 1",
	})
	if err != nil {
		t.Fatalf("PlaceBid returned error: %v", err)
	}
	if result.BidID != "bid_hot" {
		t.Fatalf("expected bid_hot, got %q", result.BidID)
	}
	if store.findItemWithRuleCalls != 0 {
		t.Fatalf("expected no store lookup on hot state hit, got %d", store.findItemWithRuleCalls)
	}
}
```

Extend the fake store with a counter:

```go
findItemWithRuleCalls int
```

Increment it at the top of fake `FindItemWithRule`:

```go
s.findItemWithRuleCalls++
```

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
rtk go test ./internal/app/item/service -run TestPlaceBidUsesHotStateWithoutStoreLookup -count=1
```

Expected: FAIL because `PlaceBid` still calls `FindItemWithRule` first.

- [ ] **Step 3: Add hot config type and cache method**

In `internal/app/item/cache/cache.go`, add:

```go
type AuctionHotConfig struct {
	ItemID            string
	RoomID            string
	Status            string
	BidIncrement      int64
	PriceCap          int64
	DepositAmount     int64
	ExtendTriggerSec  int
	AutoExtendSec     int
	MaxExtendCount    int
	MaxTotalExtendSec int
	EndTimeUnixMS     int64
}
```

Add to `Cache`:

```go
GetAuctionHotConfig(ctx context.Context, itemID string) (*AuctionHotConfig, bool, error)
```

Implement in `RedisCache`:

```go
func (c *RedisCache) GetAuctionHotConfig(ctx context.Context, itemID string) (*AuctionHotConfig, bool, error) {
	state, ok, err := c.GetAuctionState(ctx, itemID)
	if err != nil || !ok {
		return nil, ok, err
	}
	if state.RoomID == "" || state.BidIncrement <= 0 {
		return nil, false, nil
	}
	return &AuctionHotConfig{
		ItemID:            itemID,
		RoomID:            state.RoomID,
		Status:            state.Status,
		BidIncrement:      state.BidIncrement,
		PriceCap:          state.PriceCap,
		DepositAmount:     state.DepositAmount,
		ExtendTriggerSec:  state.ExtendTriggerSec,
		AutoExtendSec:     state.AutoExtendSec,
		MaxExtendCount:    state.MaxExtendCount,
		MaxTotalExtendSec: state.MaxTotalExtendSec,
		EndTimeUnixMS:     state.EndTimeUnixMS,
	}, true, nil
}
```

Update service test fakes to implement this method by returning their stored state.

- [ ] **Step 4: Add helper to resolve hot config in service**

In `internal/app/item/service/bid_service.go`, add a helper near `PlaceBid`:

```go
func (s *Service) bidHotConfig(ctx context.Context, itemID string) (*itemcache.AuctionHotConfig, error) {
	if s.cache == nil {
		return nil, errorx.ErrInternal
	}
	if hot, ok, err := s.cache.GetAuctionHotConfig(ctx, itemID); err != nil {
		return nil, err
	} else if ok && hot.Status == string(model.ItemOngoing) {
		return hot, nil
	}
	item, rule, err := s.store.FindItemWithRule(itemID)
	if err != nil {
		return nil, err
	}
	if item.Status != model.ItemOngoing {
		return nil, errorx.ErrInvalidRequest
	}
	state := itemcache.AuctionState{
		Status:            string(model.ItemOngoing),
		RoomID:            item.RoomID,
		CurrentPrice:      rule.StartPrice,
		DealPrice:         rule.StartPrice,
		EndTime:           rule.EndTime,
		EndTimeUnixMS:     rule.EndTime.UnixMilli(),
		BidIncrement:      rule.BidIncrement,
		PriceCap:          rule.PriceCap,
		DepositAmount:     rule.DepositAmount,
		ExtendTriggerSec:  s.policy.ExtendTriggerSec,
		AutoExtendSec:     s.policy.AutoExtendSec,
		MaxExtendCount:    s.policy.MaxExtendCount,
		MaxTotalExtendSec: s.policy.MaxTotalExtendSec,
	}
	if err := s.cache.InitAuctionState(ctx, item.ID, state); err != nil {
		return nil, err
	}
	return &itemcache.AuctionHotConfig{
		ItemID:            item.ID,
		RoomID:            item.RoomID,
		Status:            string(item.Status),
		BidIncrement:      rule.BidIncrement,
		PriceCap:          rule.PriceCap,
		DepositAmount:     rule.DepositAmount,
		ExtendTriggerSec:  s.policy.ExtendTriggerSec,
		AutoExtendSec:     s.policy.AutoExtendSec,
		MaxExtendCount:    s.policy.MaxExtendCount,
		MaxTotalExtendSec: s.policy.MaxTotalExtendSec,
		EndTimeUnixMS:     rule.EndTime.UnixMilli(),
	}, nil
}
```

- [ ] **Step 5: Use hot config in `PlaceBid`**

Replace the initial item/rule lookup section in `PlaceBid` with:

```go
hot, err := s.bidHotConfig(ctx, itemID)
if err != nil {
	bidResult = "error"
	if err == errorx.ErrInvalidRequest {
		bidResult = "rejected"
		bidReason = "item_not_ongoing"
	}
	return nil, err
}
```

Use `hot.DepositAmount`, `hot.BidIncrement`, `hot.PriceCap`, and policy fields from `hot` in the later Lua args and deposit check. Use `hot.RoomID` where `item.RoomID` was used in broadcast and bid log construction.

- [ ] **Step 6: Run tests**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestPlaceBidUsesHotStateWithoutStoreLookup|TestPlaceBid' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
rtk git add internal/app/item/cache/cache.go internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go
rtk git commit -m "feat(item): use redis hot state for bidding"
```

## Task 3: Ensure Rejected Bids Do Not Touch MySQL

**Files:**
- Modify: `internal/app/item/service/bid_service_test.go`
- Modify: `internal/app/item/service/bid_service.go`

- [ ] **Step 1: Add low-price rejection test**

Add:

```go
func TestPlaceBidLowPriceRejectionDoesNotTouchStore(t *testing.T) {
	store := newFakeStore()
	cache := newFakeBidCache()
	cache.hotState = &itemcache.AuctionState{
		Status:            string(itemmodel.ItemOngoing),
		RoomID:            "room_1",
		BidIncrement:      100,
		EndTimeUnixMS:     time.Now().Add(time.Minute).UnixMilli(),
		ExtendTriggerSec:  30,
		AutoExtendSec:     20,
		MaxExtendCount:    3,
		MaxTotalExtendSec: 60,
	}
	cache.luaResult = &itemcache.BidLuaResult{Code: 3}
	svc := NewService(store, itemdto.DefaultAuctionPolicy(), cache, nil, nil, nil)

	_, err := svc.PlaceBid(context.Background(), &usermodel.User{ID: "user_1", Name: "User 1"}, "item_1", itemdto.PlaceBidInput{
		Price:          1000,
		IdempotencyKey: "idem_low",
		UserName:       "User 1",
	})
	if err == nil {
		t.Fatal("expected low price error")
	}
	if store.findItemWithRuleCalls != 0 {
		t.Fatalf("expected no store lookup, got %d", store.findItemWithRuleCalls)
	}
	if len(store.bidLogs) != 0 {
		t.Fatalf("expected no bid logs, got %d", len(store.bidLogs))
	}
}
```

- [ ] **Step 2: Run and verify failure if store is still touched**

Run:

```bash
rtk go test ./internal/app/item/service -run TestPlaceBidLowPriceRejectionDoesNotTouchStore -count=1
```

Expected before Task 2 completion: FAIL. Expected after Task 2 completion: PASS.

- [ ] **Step 3: Fix any remaining store access before Lua result handling**

If the test fails, move any remaining item/rule dependent code behind `hot` or Lua success. The request should only call MySQL on hot state miss/rebuild.

Use this pattern in `PlaceBid`:

```go
roomID := hot.RoomID
bidIncrement := hot.BidIncrement
priceCap := hot.PriceCap
depositAmount := hot.DepositAmount
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestPlaceBidLowPriceRejectionDoesNotTouchStore|TestPlaceBidUsesHotStateWithoutStoreLookup' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go
rtk git commit -m "test(item): lock rejected bids out of mysql hot path"
```

## Task 4: Add Redis Stream Append For Successful Bid Logs

**Files:**
- Create: `internal/app/item/cache/bid_log_stream.go`
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/service/bid_service.go`
- Test: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Add failing tests for stream append semantics**

Add:

```go
func TestPlaceBidSuccessfulBidAppendsBidLogEvent(t *testing.T) {
	store := newFakeStore()
	cache := newFakeBidCache()
	cache.hotState = hotOngoingState()
	cache.luaResult = &itemcache.BidLuaResult{
		Code:         0,
		BidID:        "bid_stream",
		CurrentPrice: 1200,
		LeaderUserID: "user_1",
		EndTimeUnixMS: cache.hotState.EndTimeUnixMS,
		Status:       "ongoing",
	}
	svc := NewService(store, itemdto.DefaultAuctionPolicy(), cache, nil, nil, nil)

	_, err := svc.PlaceBid(context.Background(), &usermodel.User{ID: "user_1", Name: "User 1"}, "item_1", itemdto.PlaceBidInput{
		Price:          1200,
		IdempotencyKey: "idem_stream",
		UserName:       "User 1",
	})
	if err != nil {
		t.Fatalf("PlaceBid returned error: %v", err)
	}
	if len(cache.bidLogEvents) != 1 {
		t.Fatalf("expected one stream event, got %d", len(cache.bidLogEvents))
	}
	event := cache.bidLogEvents[0]
	if event.BidID != "bid_stream" || event.ItemID != "item_1" || event.RoomID != "room_1" || event.UserID != "user_1" || event.Price != 1200 {
		t.Fatalf("unexpected stream event: %+v", event)
	}
}

func TestPlaceBidIdempotentResultDoesNotAppendBidLogEvent(t *testing.T) {
	store := newFakeStore()
	cache := newFakeBidCache()
	cache.hotState = hotOngoingState()
	cache.luaResult = &itemcache.BidLuaResult{
		Code:         1,
		BidID:        "bid_existing",
		CurrentPrice: 1200,
		LeaderUserID: "user_1",
		EndTimeUnixMS: cache.hotState.EndTimeUnixMS,
		Status:       "ongoing",
	}
	svc := NewService(store, itemdto.DefaultAuctionPolicy(), cache, nil, nil, nil)

	_, err := svc.PlaceBid(context.Background(), &usermodel.User{ID: "user_1", Name: "User 1"}, "item_1", itemdto.PlaceBidInput{
		Price:          1200,
		IdempotencyKey: "idem_existing",
		UserName:       "User 1",
	})
	if err != nil {
		t.Fatalf("PlaceBid returned error: %v", err)
	}
	if len(cache.bidLogEvents) != 0 {
		t.Fatalf("expected no new stream events, got %d", len(cache.bidLogEvents))
	}
}
```

Add helper in the test file:

```go
func hotOngoingState() *itemcache.AuctionState {
	return &itemcache.AuctionState{
		Status:            string(itemmodel.ItemOngoing),
		RoomID:            "room_1",
		BidIncrement:      100,
		PriceCap:          0,
		DepositAmount:     0,
		ExtendTriggerSec:  30,
		AutoExtendSec:     20,
		MaxExtendCount:    3,
		MaxTotalExtendSec: 60,
		EndTimeUnixMS:     time.Now().Add(time.Minute).UnixMilli(),
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestPlaceBidSuccessfulBidAppendsBidLogEvent|TestPlaceBidIdempotentResultDoesNotAppendBidLogEvent' -count=1
```

Expected: FAIL because stream append type and cache method do not exist.

- [ ] **Step 3: Add stream event type and cache interface method**

In `internal/app/item/cache/cache.go`, add:

```go
type BidLogEvent struct {
	BidID           string
	ItemID          string
	RoomID          string
	UserID          string
	Price           int64
	CreatedAtUnixMS int64
}

type BidLogStreamMessage struct {
	ID    string
	Event BidLogEvent
}
```

Add to `Cache`:

```go
AppendBidLogEvent(ctx context.Context, event BidLogEvent) error
```

- [ ] **Step 4: Implement Redis Stream append**

Create `internal/app/item/cache/bid_log_stream.go`:

```go
package cache

import (
	"context"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const BidLogStreamName = "auction:bid_log:stream"
const BidLogDeadStreamName = "auction:bid_log:dead"
const BidLogConsumerGroup = "bid-log-writers"

func (c *RedisCache) AppendBidLogEvent(ctx context.Context, event BidLogEvent) error {
	return c.client.XAdd(ctx, &redis.XAddArgs{
		Stream: BidLogStreamName,
		Values: map[string]any{
			"bid_id":             event.BidID,
			"item_id":            event.ItemID,
			"room_id":            event.RoomID,
			"user_id":            event.UserID,
			"price":              strconv.FormatInt(event.Price, 10),
			"created_at_unix_ms": strconv.FormatInt(event.CreatedAtUnixMS, 10),
		},
	}).Err()
}
```

- [ ] **Step 5: Append event from `PlaceBid`**

After Lua success code `0` and before broadcasting, add:

```go
if err := s.cache.AppendBidLogEvent(ctx, itemcache.BidLogEvent{
	BidID:           luaResult.BidID,
	ItemID:          itemID,
	RoomID:          roomID,
	UserID:          current.ID,
	Price:           input.Price,
	CreatedAtUnixMS: now.UnixMilli(),
}); err != nil {
	bidResult = "error"
	bidReason = "bid_log_stream_error"
	return nil, err
}
```

Do not append for Lua code `1`.

- [ ] **Step 6: Update fakes and run tests**

In fake cache:

```go
bidLogEvents []itemcache.BidLogEvent
appendBidLogErr error

func (c *fakeBidCache) AppendBidLogEvent(_ context.Context, event itemcache.BidLogEvent) error {
	if c.appendBidLogErr != nil {
		return c.appendBidLogErr
	}
	c.bidLogEvents = append(c.bidLogEvents, event)
	return nil
}
```

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestPlaceBidSuccessfulBidAppendsBidLogEvent|TestPlaceBidIdempotentResultDoesNotAppendBidLogEvent|TestPlaceBid' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
rtk git add internal/app/item/cache/cache.go internal/app/item/cache/bid_log_stream.go internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go
rtk git commit -m "feat(item): append successful bids to redis stream"
```

## Task 5: Add Batch Bid Log DAO Method

**Files:**
- Modify: `internal/app/item/dao/item.go`
- Modify: `internal/app/item/dao/bid_log.go`
- Modify: service fakes in `internal/app/item/service/*_test.go`

- [ ] **Step 1: Extend DAO interface**

In `internal/app/item/dao/item.go`, add:

```go
CreateBidLogs(logs []*model.BidLog) error
```

- [ ] **Step 2: Implement idempotent batch insert**

In `internal/app/item/dao/bid_log.go`, replace imports with:

```go
import (
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	"gorm.io/gorm/clause"
)
```

Add:

```go
func (s *GormStore) CreateBidLogs(logs []*model.BidLog) error {
	if len(logs) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&logs).Error
}
```

- [ ] **Step 3: Update fake stores**

Every fake store implementing `dao.Store` must add:

```go
func (s *fakeStore) CreateBidLogs(logs []*itemmodel.BidLog) error {
	for _, log := range logs {
		if log == nil {
			continue
		}
		if _, exists := s.bidLogs[log.ID]; exists {
			continue
		}
		cp := *log
		s.bidLogs[cp.ID] = &cp
	}
	return nil
}
```

If a fake uses a slice instead of a map, adapt with duplicate-ID detection:

```go
for _, existing := range s.bidLogs {
	if existing.ID == log.ID {
		seen = true
		break
	}
}
```

- [ ] **Step 4: Run compile tests**

Run:

```bash
rtk go test ./internal/app/item/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/app/item/dao/item.go internal/app/item/dao/bid_log.go internal/app/item/service
rtk git commit -m "feat(item): add idempotent batch bid log insert"
```

## Task 6: Implement Bid Log Worker

**Files:**
- Create: `internal/app/item/service/bid_log_worker.go`
- Create: `internal/app/item/service/bid_log_worker_test.go`
- Modify: `internal/app/item/init.go`
- Modify: `internal/app/item/service/service.go`

- [ ] **Step 1: Write worker unit tests**

Create `internal/app/item/service/bid_log_worker_test.go` with:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

type fakeBidLogReader struct {
	events []itemcache.BidLogEvent
	acked  []string
	err    error
}

func (r *fakeBidLogReader) Read(ctx context.Context, count int) ([]itemcache.BidLogStreamMessage, error) {
	if r.err != nil {
		return nil, r.err
	}
	if len(r.events) == 0 {
		return nil, nil
	}
	if count > len(r.events) {
		count = len(r.events)
	}
	out := make([]itemcache.BidLogStreamMessage, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, itemcache.BidLogStreamMessage{
			ID:    "stream_" + r.events[i].BidID,
			Event: r.events[i],
		})
	}
	r.events = r.events[count:]
	return out, nil
}

func (r *fakeBidLogReader) Ack(ctx context.Context, ids []string) error {
	r.acked = append(r.acked, ids...)
	return nil
}

type fakeBidLogBatchStore struct {
	logs []*itemmodel.BidLog
	err  error
}

func (s *fakeBidLogBatchStore) CreateBidLogs(logs []*itemmodel.BidLog) error {
	if s.err != nil {
		return s.err
	}
	s.logs = append(s.logs, logs...)
	return nil
}

func TestBidLogWorkerPersistsAndAcksBatch(t *testing.T) {
	reader := &fakeBidLogReader{events: []itemcache.BidLogEvent{
		{BidID: "bid_1", ItemID: "item_1", RoomID: "room_1", UserID: "user_1", Price: 1100, CreatedAtUnixMS: time.Unix(100, 0).UnixMilli()},
	}}
	store := &fakeBidLogBatchStore{}
	worker := newBidLogWorker(reader, store, bidLogWorkerConfig{BatchSize: 10})

	if err := worker.drainOnce(context.Background()); err != nil {
		t.Fatalf("drainOnce returned error: %v", err)
	}
	if len(store.logs) != 1 || store.logs[0].ID != "bid_1" {
		t.Fatalf("expected one persisted bid log, got %+v", store.logs)
	}
	if len(reader.acked) != 1 || reader.acked[0] != "stream_bid_1" {
		t.Fatalf("expected stream ack, got %+v", reader.acked)
	}
}

func TestBidLogWorkerDoesNotAckWhenPersistFails(t *testing.T) {
	reader := &fakeBidLogReader{events: []itemcache.BidLogEvent{
		{BidID: "bid_1", ItemID: "item_1", RoomID: "room_1", UserID: "user_1", Price: 1100, CreatedAtUnixMS: time.Now().UnixMilli()},
	}}
	store := &fakeBidLogBatchStore{err: errors.New("db down")}
	worker := newBidLogWorker(reader, store, bidLogWorkerConfig{BatchSize: 10})

	if err := worker.drainOnce(context.Background()); err == nil {
		t.Fatal("expected persist error")
	}
	if len(reader.acked) != 0 {
		t.Fatalf("expected no ack on persist failure, got %+v", reader.acked)
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run:

```bash
rtk go test ./internal/app/item/service -run TestBidLogWorker -count=1
```

Expected: FAIL because worker types do not exist.

- [ ] **Step 3: Implement worker core**

Create `internal/app/item/service/bid_log_worker.go`:

```go
package service

import (
	"context"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

type bidLogStreamReader interface {
	Read(ctx context.Context, count int) ([]itemcache.BidLogStreamMessage, error)
	Ack(ctx context.Context, ids []string) error
}

type bidLogBatchStore interface {
	CreateBidLogs(logs []*itemmodel.BidLog) error
}

type bidLogWorkerConfig struct {
	BatchSize     int
	PollInterval  time.Duration
}

type bidLogWorker struct {
	reader bidLogStreamReader
	store  bidLogBatchStore
	cfg    bidLogWorkerConfig
}

func newBidLogWorker(reader bidLogStreamReader, store bidLogBatchStore, cfg bidLogWorkerConfig) *bidLogWorker {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 100 * time.Millisecond
	}
	return &bidLogWorker{reader: reader, store: store, cfg: cfg}
}

func (w *bidLogWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.drainOnce(ctx); err != nil {
				logx.Warnw("item.bid_log_worker drain failed", "err", err)
			}
		}
	}
}

func (w *bidLogWorker) drainOnce(ctx context.Context) error {
	msgs, err := w.reader.Read(ctx, w.cfg.BatchSize)
	if err != nil || len(msgs) == 0 {
		return err
	}
	logs := make([]*itemmodel.BidLog, 0, len(msgs))
	ids := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		logs = append(logs, &itemmodel.BidLog{
			ID:        msg.Event.BidID,
			ItemID:    msg.Event.ItemID,
			RoomID:    msg.Event.RoomID,
			UserID:    msg.Event.UserID,
			Price:     msg.Event.Price,
			CreatedAt: time.UnixMilli(msg.Event.CreatedAtUnixMS),
		})
		ids = append(ids, msg.ID)
	}
	if err := w.store.CreateBidLogs(logs); err != nil {
		return err
	}
	return w.reader.Ack(ctx, ids)
}
```

- [ ] **Step 4: Implement Redis stream reader**

Add a concrete reader in `internal/app/item/cache/bid_log_stream.go` or a new focused file. The reader must ensure the consumer group exists and call `XREADGROUP`. Keep the message type in the `cache` package so `cache` never imports `service`. Use this shape:

```go
type BidLogStreamReader struct {
	client   *redis.Client
	consumer string
}
```

Methods:

```go
func NewBidLogStreamReader(client *redis.Client, consumer string) *BidLogStreamReader
func (r *BidLogStreamReader) EnsureGroup(ctx context.Context) error
func (r *BidLogStreamReader) Read(ctx context.Context, count int) ([]BidLogStreamMessage, error)
func (r *BidLogStreamReader) Ack(ctx context.Context, ids []string) error
```

Parse Redis stream fields into `BidLogStreamMessage{ID: redisMessage.ID, Event: BidLogEvent{...}}`. Treat `redis.Nil` or an empty `XREADGROUP` result as no messages and return `nil, nil`. For `EnsureGroup`, accept Redis `BUSYGROUP` as success.

- [ ] **Step 5: Wire worker lifecycle**

In `internal/app/item/service/service.go`, add fields:

```go
bidLogWorkerCancel context.CancelFunc
```

Add methods:

```go
func (s *Service) StartBidLogWorker(ctx context.Context, reader bidLogStreamReader) {
	workerCtx, cancel := context.WithCancel(ctx)
	s.bidLogWorkerCancel = cancel
	worker := newBidLogWorker(reader, s.store, bidLogWorkerConfig{BatchSize: 200, PollInterval: 100 * time.Millisecond})
	go worker.Run(workerCtx)
}

func (s *Service) StopBidLogWorker() {
	if s.bidLogWorkerCancel != nil {
		s.bidLogWorkerCancel()
	}
}
```

In `internal/app/item/init.go`, after service creation and route registration:

```go
if engine.Cache != nil {
	reader := cache.NewBidLogStreamReader(engine.Cache, "backend-1")
	if err := reader.EnsureGroup(engine.Context); err != nil {
		return err
	}
	svc.StartBidLogWorker(engine.Context, reader)
}
```

In `Item.Stop`, call the service stop hook if service package exposes it, or store service in package-level handler state consistent with existing module style. Keep shutdown idempotent.

- [ ] **Step 6: Run tests**

Run:

```bash
rtk go test ./internal/app/item/service ./internal/app/item/cache ./internal/app/item/dao
```

Expected: PASS or no test files for cache/dao.

- [ ] **Step 7: Commit**

```bash
rtk git add internal/app/item/cache internal/app/item/service internal/app/item/init.go
rtk git commit -m "feat(item): persist bid logs from redis stream"
```

## Task 7: Remove Synchronous BidLog Insert From HTTP Path

**Files:**
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Add test that success does not call `CreateBidLog`**

Add:

```go
func TestPlaceBidSuccessfulBidDoesNotSynchronouslyCreateBidLog(t *testing.T) {
	store := newFakeStore()
	cache := newFakeBidCache()
	cache.hotState = hotOngoingState()
	cache.luaResult = &itemcache.BidLuaResult{
		Code:         0,
		BidID:        "bid_async",
		CurrentPrice: 1300,
		LeaderUserID: "user_1",
		EndTimeUnixMS: cache.hotState.EndTimeUnixMS,
		Status:       "ongoing",
	}
	svc := NewService(store, itemdto.DefaultAuctionPolicy(), cache, nil, nil, nil)

	_, err := svc.PlaceBid(context.Background(), &usermodel.User{ID: "user_1", Name: "User 1"}, "item_1", itemdto.PlaceBidInput{
		Price:          1300,
		IdempotencyKey: "idem_async",
		UserName:       "User 1",
	})
	if err != nil {
		t.Fatalf("PlaceBid returned error: %v", err)
	}
	if len(store.bidLogs) != 0 {
		t.Fatalf("expected no synchronous bid logs, got %d", len(store.bidLogs))
	}
	if len(cache.bidLogEvents) != 1 {
		t.Fatalf("expected stream event handoff, got %d", len(cache.bidLogEvents))
	}
}
```

- [ ] **Step 2: Run and verify failure**

Run:

```bash
rtk go test ./internal/app/item/service -run TestPlaceBidSuccessfulBidDoesNotSynchronouslyCreateBidLog -count=1
```

Expected: FAIL while synchronous `CreateBidLog` remains.

- [ ] **Step 3: Remove synchronous insert**

Delete this block from `PlaceBid`:

```go
bidLog := &model.BidLog{
	ID:     luaResult.BidID,
	ItemID: item.ID,
	RoomID: item.RoomID,
	UserID: current.ID,
	Price:  input.Price,
}
if err := s.store.CreateBidLog(bidLog); err != nil {
	bidResult = "error"
	bidReason = "db_error"
	return nil, err
}
```

The stream append from Task 4 is the durable handoff.

- [ ] **Step 4: Run focused and broad item tests**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestPlaceBid|TestBidLogWorker' -count=1
rtk go test ./internal/app/item/...
```

Expected: PASS. Update old assertions that expected immediate `store.bidLogs` to instead assert stream events, unless the test explicitly covers worker persistence.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/app/item/service
rtk git commit -m "feat(item): remove synchronous bid log insert from bid path"
```

## Task 8: Add Metrics For Hot State And Async Persistence

**Files:**
- Modify: `internal/core/observability/metrics.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_log_worker.go`
- Test: `internal/core/observability/metrics_test.go`

- [ ] **Step 1: Add recorder methods and metric structs**

In `internal/core/observability/metrics.go`, add to `Recorder`:

```go
BidHotState(context.Context, BidHotStateMetric)
BidLogStream(context.Context, BidLogStreamMetric)
BidLogWorker(context.Context, BidLogWorkerMetric)
```

Add structs:

```go
type BidHotStateMetric struct {
	Result   string
	Duration time.Duration
}

type BidLogStreamMetric struct {
	Result   string
	Duration time.Duration
}

type BidLogWorkerMetric struct {
	Result    string
	BatchSize int64
	Duration  time.Duration
}
```

Add no-op methods to `NoopRecorder`.

- [ ] **Step 2: Register OTel instruments**

Add fields to `OTelRecorder`:

```go
bidHotStateCount      metric.Int64Counter
bidHotStateDuration   metric.Float64Histogram
bidLogStreamCount     metric.Int64Counter
bidLogStreamDuration  metric.Float64Histogram
bidLogWorkerCount     metric.Int64Counter
bidLogWorkerBatchSize metric.Int64Histogram
bidLogWorkerDuration  metric.Float64Histogram
```

Create them in `NewRecorder` with names:

```text
auction.hot_state.lookup.count
auction.hot_state.lookup.duration
auction.bid_log.stream.append.count
auction.bid_log.stream.append.duration
auction.bid_log.worker.batch.count
auction.bid_log.worker.batch.size
auction.bid_log.worker.persist.duration
```

Implement recorder methods with `result` attribute.

- [ ] **Step 3: Instrument service hot state lookup**

Wrap `bidHotConfig` with:

```go
start := time.Now()
result := "hit"
defer func() {
	observability.DefaultRecorder().BidHotState(ctx, observability.BidHotStateMetric{
		Result:   result,
		Duration: time.Since(start),
	})
}()
```

Set `result` to `miss`, `rebuilt`, or `error` on each branch.

- [ ] **Step 4: Instrument stream append and worker persist**

Around `AppendBidLogEvent` call:

```go
start := time.Now()
streamResult := "success"
if err := s.cache.AppendBidLogEvent(ctx, event); err != nil {
	streamResult = "error"
	observability.DefaultRecorder().BidLogStream(ctx, observability.BidLogStreamMetric{Result: streamResult, Duration: time.Since(start)})
	return nil, err
}
observability.DefaultRecorder().BidLogStream(ctx, observability.BidLogStreamMetric{Result: streamResult, Duration: time.Since(start)})
```

In worker `drainOnce`, record batch result and duration around `CreateBidLogs`.

- [ ] **Step 5: Run tests**

Run:

```bash
rtk go test ./internal/core/observability ./internal/app/item/service -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add internal/core/observability/metrics.go internal/core/observability/metrics_test.go internal/app/item/service
rtk git commit -m "feat(observability): add bid hot path metrics"
```

## Task 9: Local Verification And Regression Gate

**Files:**
- Modify: `docs/agent-testing/reports/20260604-141143-auction-http-bid-performance.md` only if adding a follow-up note.
- No code changes unless tests reveal a defect.

- [ ] **Step 1: Run local unit suite**

Run:

```bash
rtk go test ./internal/app/item/... ./internal/core/observability/...
```

Expected: PASS.

- [ ] **Step 2: Run broader build/test check**

Run:

```bash
rtk go test ./...
```

Expected: PASS. If unrelated existing tests fail, record the package and failure text before deciding whether it blocks this feature.

- [ ] **Step 3: Prepare local performance command**

Use the existing runner after starting an approved local backend. Command shape:

```bash
rtk env \
  GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_perf_auction_local_bid_hot_path \
  PERF_ENVIRONMENT=local_smoke \
  PERF_BASE_URL=http://127.0.0.1:8080 \
  PERF_STAGE_QPS=150,300,500 \
  PERF_DISABLE_WS=true \
  PERF_REQUEST_MIX=bid_only \
  PERF_REQUEST_TIMEOUT=5s \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

Expected:

```text
150 QPS: completed
300 QPS: completed, server P95 below 500ms if Prometheus is configured
500 QPS: compare against 2026-06-04 baseline
CLEANUP: cancel_item=ok end_room=ok
```

- [ ] **Step 4: Add follow-up report if local probe runs**

If a local or online approved probe runs, create a new report under:

```text
docs/agent-testing/reports/YYYYMMDD-HHMMSS-auction-bid-hot-path-regression.md
```

Include:

```text
baseline: 20260604-141143 auction-http-bid-performance
hot_state_hit_rate:
stream_append_error_count:
worker_lag:
worker_batch_persist_duration:
server_p95_p99:
db_operation_rps:
cleanup_result:
```

- [ ] **Step 5: Commit test/report updates**

```bash
rtk git add docs/agent-testing/reports
rtk git commit -m "test(perf): record bid hot path regression evidence"
```

Only run this commit if a new report was created.

## Self-Review Checklist

- Spec coverage:
  - Redis hot state: Tasks 1-3.
  - Async bid log stream: Task 4.
  - Batch worker: Tasks 5-6.
  - Removal of sync MySQL insert: Task 7.
  - Metrics and alerts foundation: Task 8.
  - Regression verification: Task 9.
- No new infrastructure beyond Redis and existing Go service.
- No online load test occurs without a separate explicit approval.
- Each task has a focused test command and commit boundary.
