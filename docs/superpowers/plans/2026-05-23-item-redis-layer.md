# Item Module Redis Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Redis layer to the item module so auction state is read from Redis for `ongoing` items, and `item_queue` ZSET is maintained on publish/cancel.

**Architecture:** New `cache/cache.go` package defines a `Cache` interface with a `RedisCache` impl and `fakeCache` for tests. `Service` gains a nullable `cache` field — `nil` means bypass (existing tests pass unchanged). `StartItem` is Redis-first with MySQL rollback; `PublishItem`/`CancelItem` are MySQL-first with Redis soft-fail. Read paths (`GetItem`, `ListItems`, `ListMerchantItems`) enrich `ongoing` items from Redis and degrade silently on miss.

**Tech Stack:** go-redis/v9 (`github.com/redis/go-redis/v9`), GORM, flamego, standard `context` package.

---

## File Map

| Action | File | What changes |
|---|---|---|
| Create | `internal/app/item/cache/cache.go` | `AuctionState`, `Cache` interface, `RedisCache` |
| Modify | `internal/app/item/model/item.go` | Add `RoomID` field |
| Modify | `internal/app/item/dto/item.go` | Add `RoomID` to `CreateItemInput`, `CreateItemRequest`, `ItemListDTO`; update `NewItemListDTO` |
| Modify | `internal/app/item/service/service.go` | Add `cache` field; rewrite `CreateItem`, `PublishItem`, `StartItem`, `CancelItem`, `GetItem`, `ListItems`, `ListMerchantItems`; add `applyStateToDetail`, `applyStateToList` |
| Modify | `internal/app/item/init.go` | Wire `cache.NewRedisCache(engine.Cache)` into `NewService` |
| Modify | `internal/app/item/service/service_test.go` | Add `fakeCache`; update all `NewService` calls; add 8 new tests |

---

### Task 1: Add RoomID to model and DTO

**Files:**
- Modify: `internal/app/item/model/item.go`
- Modify: `internal/app/item/dto/item.go`

- [ ] **Step 1: Add `RoomID` to `AuctionItem` model**

In `internal/app/item/model/item.go`, add the field after `MerchantID`:

```go
type AuctionItem struct {
	ID          string            `gorm:"primaryKey;size:64" json:"id"`
	MerchantID  string            `gorm:"index;size:64;not null" json:"merchant_id"`
	RoomID      string            `gorm:"index;size:64;not null" json:"room_id"`
	Title       string            `gorm:"size:128;not null" json:"title"`
	Description string            `gorm:"size:1024" json:"description"`
	ImageURL    string            `gorm:"size:512" json:"image_url"`
	Tags        []string          `gorm:"serializer:json;type:json" json:"tags"`
	Status      AuctionItemStatus `gorm:"index;size:32;not null" json:"status"`
	RuleID      string            `gorm:"index;size:64;not null" json:"rule_id"`
	WinnerID    string            `gorm:"size:64" json:"winner_id,omitempty"`
	DealPrice   int64             `json:"deal_price"`

	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
```

- [ ] **Step 2: Add `RoomID` to `CreateItemInput` and `CreateItemRequest`**

In `internal/app/item/dto/item.go`, update `CreateItemInput`:

```go
type CreateItemInput struct {
	RoomID      string
	Title       string
	Description string
	ImageURL    string
	Tags        []string
	Rule        RuleInput
}
```

Update `CreateItemRequest`:

```go
type CreateItemRequest struct {
	RoomID      string    `json:"room_id"     binding:"required,min=1,max=64"`
	Title       string    `json:"title"       binding:"required,min=1,max=128"`
	Description string    `json:"description" binding:"omitempty,max=1024"`
	ImageURL    string    `json:"image_url"   binding:"omitempty,max=512"`
	Tags        []string  `json:"tags"        binding:"omitempty,dive,min=1,max=64"`
	Rule        RuleInput `json:"rule"        binding:"required"`
}

func (r CreateItemRequest) Input() CreateItemInput {
	return CreateItemInput{
		RoomID:      r.RoomID,
		Title:       r.Title,
		Description: r.Description,
		ImageURL:    r.ImageURL,
		Tags:        r.Tags,
		Rule:        r.Rule,
	}
}
```

- [ ] **Step 3: Add `RoomID` to `ItemListDTO` and update `NewItemListDTO`**

Add the field to `ItemListDTO`:

```go
type ItemListDTO struct {
	ID               string                      `json:"id"`
	RoomID           string                      `json:"room_id"`
	Title            string                      `json:"title"`
	Description      string                      `json:"description"`
	ImageURL         string                      `json:"image_url"`
	Tags             []string                    `json:"tags"`
	Status           itemmodel.AuctionItemStatus `json:"status"`
	CurrentPrice     int64                       `json:"current_price"`
	StartPrice       int64                       `json:"start_price"`
	BidIncrement     int64                       `json:"bid_increment"`
	PriceCap         int64                       `json:"price_cap"`
	ExtendTriggerSec int                         `json:"extend_trigger_sec"`
	AutoExtendSec    int                         `json:"auto_extend_sec"`
	ParticipantCount int                         `json:"participant_count"`
	BidCount         int                         `json:"bid_count"`
	StartTime        time.Time                   `json:"start_time"`
	EndTime          time.Time                   `json:"end_time"`
	RemainingMS      int64                       `json:"remaining_ms"`
}
```

Update `NewItemListDTO` to populate `RoomID`:

```go
func NewItemListDTO(item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule, policy AuctionPolicy, now time.Time) ItemListDTO {
	return ItemListDTO{
		ID:               item.ID,
		RoomID:           item.RoomID,
		Title:            item.Title,
		Description:      item.Description,
		ImageURL:         item.ImageURL,
		Tags:             item.Tags,
		Status:           item.Status,
		CurrentPrice:     currentPrice(item, rule),
		StartPrice:       rule.StartPrice,
		BidIncrement:     rule.BidIncrement,
		PriceCap:         rule.PriceCap,
		ExtendTriggerSec: policy.ExtendTriggerSec,
		AutoExtendSec:    policy.AutoExtendSec,
		StartTime:        rule.StartTime,
		EndTime:          rule.EndTime,
		RemainingMS:      remainingMS(item.Status, rule.EndTime, now),
	}
}
```

- [ ] **Step 4: Run existing tests — must still pass**

```bash
go test ./internal/app/item/...
```

Expected: all pass (no behavior change yet).

- [ ] **Step 5: Commit**

```bash
git add internal/app/item/model/item.go internal/app/item/dto/item.go
git commit -m "feat(item): add room_id field to AuctionItem, CreateItemRequest, and ItemListDTO"
```

---

### Task 2: Create cache package

**Files:**
- Create: `internal/app/item/cache/cache.go`

- [ ] **Step 1: Create `internal/app/item/cache/cache.go`**

```go
package cache

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type AuctionState struct {
	CurrentPrice     int64
	LeaderUserID     string
	EndTime          time.Time
	BidCount         int
	ParticipantCount int
	IsExtended       bool
	ExtendCount      int
	TotalExtendedSec int
}

type Cache interface {
	InitAuctionState(ctx context.Context, itemID string, state AuctionState) error
	GetAuctionState(ctx context.Context, itemID string) (*AuctionState, bool, error)
	DeleteAuctionState(ctx context.Context, itemID string) error
	PushToRoomQueue(ctx context.Context, roomID, itemID string, score float64) error
	RemoveFromRoomQueue(ctx context.Context, roomID, itemID string) error
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

func itemStateKey(itemID string) string {
	return "auction:item:" + itemID + ":state"
}

func roomQueueKey(roomID string) string {
	return "auction:room:" + roomID + ":item_queue"
}

func (c *RedisCache) InitAuctionState(ctx context.Context, itemID string, state AuctionState) error {
	return c.client.HSet(ctx, itemStateKey(itemID),
		"current_price", state.CurrentPrice,
		"leader_user_id", state.LeaderUserID,
		"end_time_unix", state.EndTime.Unix(),
		"bid_count", state.BidCount,
		"participant_count", state.ParticipantCount,
		"is_extended", boolToStr(state.IsExtended),
		"extend_count", state.ExtendCount,
		"total_extended_sec", state.TotalExtendedSec,
	).Err()
}

func (c *RedisCache) GetAuctionState(ctx context.Context, itemID string) (*AuctionState, bool, error) {
	vals, err := c.client.HGetAll(ctx, itemStateKey(itemID)).Result()
	if err != nil {
		return nil, false, err
	}
	if len(vals) == 0 {
		return nil, false, nil
	}
	return &AuctionState{
		CurrentPrice:     parseInt64(vals["current_price"]),
		LeaderUserID:     vals["leader_user_id"],
		EndTime:          time.Unix(parseInt64(vals["end_time_unix"]), 0),
		BidCount:         parseInt(vals["bid_count"]),
		ParticipantCount: parseInt(vals["participant_count"]),
		IsExtended:       vals["is_extended"] == "1",
		ExtendCount:      parseInt(vals["extend_count"]),
		TotalExtendedSec: parseInt(vals["total_extended_sec"]),
	}, true, nil
}

func (c *RedisCache) DeleteAuctionState(ctx context.Context, itemID string) error {
	return c.client.Del(ctx, itemStateKey(itemID)).Err()
}

func (c *RedisCache) PushToRoomQueue(ctx context.Context, roomID, itemID string, score float64) error {
	return c.client.ZAdd(ctx, roomQueueKey(roomID), redis.Z{Score: score, Member: itemID}).Err()
}

func (c *RedisCache) RemoveFromRoomQueue(ctx context.Context, roomID, itemID string) error {
	return c.client.ZRem(ctx, roomQueueKey(roomID), itemID).Err()
}

func boolToStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
```

- [ ] **Step 2: Verify package compiles**

```bash
go build ./internal/app/item/cache/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/app/item/cache/cache.go
git commit -m "feat(item): add cache package with Cache interface and RedisCache"
```

---

### Task 3: Add fakeCache to tests and update NewService signature

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Add `cache` field to `Service` and update `NewService`**

In `internal/app/item/service/service.go`, update the imports and struct:

```go
import (
	"context"
	"strings"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store  dao.Store
	cache  itemcache.Cache
	policy dto.AuctionPolicy
	now    func() time.Time
}

func NewService(store dao.Store, policy dto.AuctionPolicy, cache itemcache.Cache) *Service {
	return &Service{
		store:  store,
		cache:  cache,
		policy: policy,
		now:    time.Now,
	}
}
```

- [ ] **Step 2: Update all `NewService` calls in `service_test.go`**

Every call in `service_test.go` that reads `NewService(store, policy)` must become `NewService(store, policy, nil)`. For example:

```go
svc := NewService(store, itemdto.AuctionPolicy{ExtendTriggerSec: 30, AutoExtendSec: 10, MaxExtendCount: 6, MaxTotalExtendSec: 300}, nil)
```

And:

```go
svc := NewService(store, itemdto.AuctionPolicy{}, nil)
```

Search for all occurrences: `grep -n "NewService" internal/app/item/service/service_test.go`

- [ ] **Step 3: Add `fakeCache` to `service_test.go`**

Add after the `fakeStore` definition:

```go
type fakeCache struct {
	states    map[string]*itemcache.AuctionState
	queues    map[string][]string
	initErr   error
	deleteErr error
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		states: map[string]*itemcache.AuctionState{},
		queues: map[string][]string{},
	}
}

func (c *fakeCache) InitAuctionState(_ context.Context, itemID string, state itemcache.AuctionState) error {
	if c.initErr != nil {
		return c.initErr
	}
	copy := state
	c.states[itemID] = &copy
	return nil
}

func (c *fakeCache) GetAuctionState(_ context.Context, itemID string) (*itemcache.AuctionState, bool, error) {
	s, ok := c.states[itemID]
	if !ok {
		return nil, false, nil
	}
	copy := *s
	return &copy, true, nil
}

func (c *fakeCache) DeleteAuctionState(_ context.Context, itemID string) error {
	if c.deleteErr != nil {
		return c.deleteErr
	}
	delete(c.states, itemID)
	return nil
}

func (c *fakeCache) PushToRoomQueue(_ context.Context, roomID, itemID string, _ float64) error {
	c.queues[roomID] = append(c.queues[roomID], itemID)
	return nil
}

func (c *fakeCache) RemoveFromRoomQueue(_ context.Context, roomID, itemID string) error {
	q := c.queues[roomID]
	for i, id := range q {
		if id == itemID {
			c.queues[roomID] = append(q[:i], q[i+1:]...)
			return nil
		}
	}
	return nil
}
```

Add the import for the cache package and context at the top of `service_test.go`:

```go
import (
	"context"
	"errors"
	"testing"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)
```

- [ ] **Step 4: Run tests — all existing tests must still pass**

```bash
go test ./internal/app/item/service/...
```

Expected: all pass (cache is nil, behavior unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/app/item/service/service.go internal/app/item/service/service_test.go
git commit -m "feat(item): add cache field to Service; add fakeCache to tests"
```

---

### Task 4: Update CreateItem to store RoomID

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Write failing test**

Add to `service_test.go`:

```go
func TestCreateItemStoresRoomID(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{}, nil)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)

	result, err := svc.CreateItem(
		&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant},
		itemdto.CreateItemInput{
			RoomID: "room_abc",
			Title:  "翡翠手镯",
			Rule:   itemdto.RuleInput{BidIncrement: 100, StartTime: start, EndTime: end},
		},
	)
	if err != nil {
		t.Fatalf("CreateItem failed: %v", err)
	}
	item := store.items[result.ItemID]
	if item.RoomID != "room_abc" {
		t.Fatalf("expected room_id room_abc, got %q", item.RoomID)
	}
}
```

- [ ] **Step 2: Run — must fail**

```bash
go test ./internal/app/item/service/... -run TestCreateItemStoresRoomID
```

Expected: FAIL (item.RoomID is empty).

- [ ] **Step 3: Update `CreateItem` to set `RoomID`**

In `service.go`, in `CreateItem`, update the item construction:

```go
item := &model.AuctionItem{
	ID:          itemID,
	MerchantID:  current.ID,
	RoomID:      input.RoomID,
	Title:       input.Title,
	Description: input.Description,
	ImageURL:    input.ImageURL,
	Tags:        input.Tags,
	Status:      model.ItemDraft,
	RuleID:      ruleID,
}
```

- [ ] **Step 4: Run test — must pass**

```bash
go test ./internal/app/item/service/... -run TestCreateItemStoresRoomID
```

Expected: PASS.

- [ ] **Step 5: Run all tests**

```bash
go test ./internal/app/item/...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/app/item/service/service.go internal/app/item/service/service_test.go
git commit -m "feat(item): store room_id on item creation"
```

---

### Task 5: Rewrite PublishItem with Redis soft-fail

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Write failing test**

Add to `service_test.go`:

```go
func TestPublishItemPushesToRoomQueue(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)

	result, _ := svc.CreateItem(
		&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant},
		itemdto.CreateItemInput{
			RoomID: "room_abc",
			Title:  "翡翠手镯",
			Rule:   itemdto.RuleInput{BidIncrement: 100, StartTime: start, EndTime: end},
		},
	)
	if err := svc.PublishItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, result.ItemID); err != nil {
		t.Fatalf("PublishItem failed: %v", err)
	}
	if len(fc.queues["room_abc"]) == 0 || fc.queues["room_abc"][0] != result.ItemID {
		t.Fatalf("expected item in room queue, got %v", fc.queues["room_abc"])
	}
}
```

- [ ] **Step 2: Run — must fail**

```bash
go test ./internal/app/item/service/... -run TestPublishItemPushesToRoomQueue
```

Expected: FAIL (cache not called).

- [ ] **Step 3: Rewrite `PublishItem` in `service.go`**

Replace the current one-liner:

```go
func (s *Service) PublishItem(current *usermodel.User, itemID string) error {
	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemDraft {
		return errorx.ErrInvalidRequest
	}
	item.Status = model.ItemPublished
	if err := s.store.UpdateItemWithRule(item, rule); err != nil {
		return err
	}
	if s.cache != nil {
		// soft-fail: Redis queue is a convenience; MySQL is the source of truth
		_ = s.cache.PushToRoomQueue(context.Background(), item.RoomID, item.ID, float64(s.now().Unix()))
	}
	return nil
}
```

- [ ] **Step 4: Run test — must pass**

```bash
go test ./internal/app/item/service/... -run TestPublishItemPushesToRoomQueue
```

Expected: PASS.

- [ ] **Step 5: Run all tests**

```bash
go test ./internal/app/item/...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/app/item/service/service.go internal/app/item/service/service_test.go
git commit -m "feat(item): PublishItem pushes to room item_queue (soft-fail)"
```

---

### Task 6: Rewrite StartItem with Redis-first and rollback

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Write failing tests**

Add to `service_test.go`:

```go
func seedPublishedItem(t *testing.T, svc *Service, merchantID, roomID string) string {
	t.Helper()
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	result, err := svc.CreateItem(
		&usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant},
		itemdto.CreateItemInput{
			RoomID: roomID,
			Title:  "Test Item",
			Rule:   itemdto.RuleInput{BidIncrement: 100, StartPrice: 1000, StartTime: start, EndTime: end},
		},
	)
	if err != nil {
		t.Fatalf("CreateItem failed: %v", err)
	}
	if err := svc.PublishItem(&usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant}, result.ItemID); err != nil {
		t.Fatalf("PublishItem failed: %v", err)
	}
	return result.ItemID
}

func TestStartItemInitializesRedisState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")

	if err := svc.StartItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("StartItem failed: %v", err)
	}
	state, ok := fc.states[itemID]
	if !ok {
		t.Fatal("expected auction state in cache after StartItem")
	}
	if state.CurrentPrice != 1000 {
		t.Fatalf("expected current_price 1000 (start_price), got %d", state.CurrentPrice)
	}
	if state.EndTime.IsZero() {
		t.Fatal("expected non-zero end_time")
	}
}

func TestStartItemFailsWhenRedisInitFails(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.initErr = errors.New("redis down")
	svc := NewService(store, itemdto.AuctionPolicy{}, fc)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")

	if err := svc.StartItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err == nil {
		t.Fatal("expected error when Redis init fails")
	}
	item := store.items[itemID]
	if item.Status != itemmodel.ItemPublished {
		t.Fatalf("expected MySQL status to remain published, got %q", item.Status)
	}
}

func TestStartItemRollsBackRedisOnMySQLFailure(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")

	// make MySQL update fail after Redis succeeds
	store.updateErr = errors.New("mysql down")

	if err := svc.StartItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err == nil {
		t.Fatal("expected error when MySQL fails")
	}
	if _, ok := fc.states[itemID]; ok {
		t.Fatal("expected Redis state to be rolled back after MySQL failure")
	}
}
```

For `TestStartItemRollsBackRedisOnMySQLFailure` to work, `fakeStore` needs an `updateErr` field. Update `fakeStore`:

```go
type fakeStore struct {
	items     map[string]*itemmodel.AuctionItem
	rules     map[string]*itemmodel.AuctionRule
	updateErr error
}
```

And update `UpdateItemWithRule`:

```go
func (s *fakeStore) UpdateItemWithRule(item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if _, ok := s.items[item.ID]; !ok {
		return errorx.ErrNotFound
	}
	itemCopy := *item
	ruleCopy := *rule
	s.items[item.ID] = &itemCopy
	s.rules[rule.ID] = &ruleCopy
	return nil
}
```

- [ ] **Step 2: Run tests — must fail**

```bash
go test ./internal/app/item/service/... -run "TestStartItem"
```

Expected: FAIL (cache not used in StartItem yet).

- [ ] **Step 3: Rewrite `StartItem` in `service.go`**

Replace the current one-liner:

```go
func (s *Service) StartItem(current *usermodel.User, itemID string) error {
	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemPublished {
		return errorx.ErrInvalidRequest
	}
	if s.cache != nil {
		state := itemcache.AuctionState{
			CurrentPrice: rule.StartPrice,
			EndTime:      rule.EndTime,
		}
		if err := s.cache.InitAuctionState(context.Background(), item.ID, state); err != nil {
			return err
		}
	}
	item.Status = model.ItemOngoing
	if err := s.store.UpdateItemWithRule(item, rule); err != nil {
		if s.cache != nil {
			_ = s.cache.DeleteAuctionState(context.Background(), item.ID)
		}
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run tests — must pass**

```bash
go test ./internal/app/item/service/... -run "TestStartItem"
```

Expected: PASS.

- [ ] **Step 5: Run all tests**

```bash
go test ./internal/app/item/...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/app/item/service/service.go internal/app/item/service/service_test.go
git commit -m "feat(item): StartItem initializes Redis auction state (Redis-first with rollback)"
```

---

### Task 7: Rewrite CancelItem with Redis cleanup

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Write failing test**

Add to `service_test.go`:

```go
func TestCancelItemRemovesFromRoomQueueAndState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}

	// Also start it so there's an auction state to clean up
	_ = svc.StartItem(merchant, itemID)

	if err := svc.CancelItem(merchant, itemID); err != nil {
		t.Fatalf("CancelItem failed: %v", err)
	}
	if _, ok := fc.states[itemID]; ok {
		t.Fatal("expected auction state deleted from cache")
	}
	for _, id := range fc.queues["room_abc"] {
		if id == itemID {
			t.Fatal("expected item removed from room queue")
		}
	}
}
```

- [ ] **Step 2: Run — must fail**

```bash
go test ./internal/app/item/service/... -run TestCancelItemRemovesFromRoomQueueAndState
```

Expected: FAIL (cache not cleaned up).

- [ ] **Step 3: Rewrite `CancelItem` in `service.go`**

```go
func (s *Service) CancelItem(current *usermodel.User, itemID string) error {
	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemPublished && item.Status != model.ItemOngoing {
		return errorx.ErrInvalidRequest
	}
	item.Status = model.ItemCancelled
	if err := s.store.UpdateItemWithRule(item, rule); err != nil {
		return err
	}
	if s.cache != nil {
		_ = s.cache.RemoveFromRoomQueue(context.Background(), item.RoomID, item.ID)
		_ = s.cache.DeleteAuctionState(context.Background(), item.ID)
	}
	return nil
}
```

- [ ] **Step 4: Run test — must pass**

```bash
go test ./internal/app/item/service/... -run TestCancelItemRemovesFromRoomQueueAndState
```

Expected: PASS.

- [ ] **Step 5: Run all tests**

```bash
go test ./internal/app/item/...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/app/item/service/service.go internal/app/item/service/service_test.go
git commit -m "feat(item): CancelItem cleans up Redis queue and auction state"
```

---

### Task 8: Enrich GetItem, ListItems, ListMerchantItems from Redis

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Write failing tests**

Add to `service_test.go`:

```go
func TestGetItemEnrichesFromCacheWhenOngoing(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc)
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(merchant, itemID)

	// Override Redis state to simulate bid activity
	fc.states[itemID] = &itemcache.AuctionState{
		CurrentPrice: 5000,
		LeaderUserID: "user_99",
		EndTime:      time.Now().Add(time.Minute),
		BidCount:     3,
	}

	detail, err := svc.GetItem(itemID)
	if err != nil {
		t.Fatalf("GetItem failed: %v", err)
	}
	if detail.CurrentPrice != 5000 {
		t.Fatalf("expected current_price 5000 from Redis, got %d", detail.CurrentPrice)
	}
	if detail.LeaderUserID != "user_99" {
		t.Fatalf("expected leader_user_id user_99, got %q", detail.LeaderUserID)
	}
}

func TestGetItemFallsBackToMySQLWhenCacheMiss(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc)
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(merchant, itemID)
	// Remove state to simulate cache miss
	delete(fc.states, itemID)

	detail, err := svc.GetItem(itemID)
	if err != nil {
		t.Fatalf("GetItem should not fail on cache miss, got %v", err)
	}
	if detail.CurrentPrice == 0 {
		// start_price was 1000 set in seedPublishedItem
		t.Fatalf("expected MySQL start_price fallback, got %d", detail.CurrentPrice)
	}
}
```

- [ ] **Step 2: Run — must fail**

```bash
go test ./internal/app/item/service/... -run "TestGetItem"
```

Expected: FAIL.

- [ ] **Step 3: Add `applyStateToDetail` and `applyStateToList` helpers**

Add at the bottom of `service.go`:

```go
func applyStateToDetail(d *dto.ItemDetailDTO, state *itemcache.AuctionState, now time.Time) {
	d.CurrentPrice = state.CurrentPrice
	d.LeaderUserID = state.LeaderUserID
	d.BidCount = state.BidCount
	d.ParticipantCount = state.ParticipantCount
	d.IsExtended = state.IsExtended
	remaining := state.EndTime.Sub(now).Milliseconds()
	if remaining < 0 {
		remaining = 0
	}
	d.RemainingMS = remaining
}

func applyStateToList(d *dto.ItemListDTO, state *itemcache.AuctionState, now time.Time) {
	d.CurrentPrice = state.CurrentPrice
	d.BidCount = state.BidCount
	d.ParticipantCount = state.ParticipantCount
	remaining := state.EndTime.Sub(now).Milliseconds()
	if remaining < 0 {
		remaining = 0
	}
	d.RemainingMS = remaining
}
```

- [ ] **Step 4: Update `GetItem`**

```go
func (s *Service) GetItem(itemID string) (*dto.ItemDetailDTO, error) {
	item, rule, err := s.store.FindItemWithRule(strings.TrimSpace(itemID))
	if err != nil {
		return nil, err
	}
	now := s.now()
	result := dto.NewItemDetailDTO(item, rule, s.policy, now)
	if item.Status == model.ItemOngoing && s.cache != nil {
		if state, ok, _ := s.cache.GetAuctionState(context.Background(), item.ID); ok {
			applyStateToDetail(&result, state, now)
		}
	}
	return &result, nil
}
```

- [ ] **Step 5: Update `ListItems`**

```go
func (s *Service) ListItems(query dto.ListItemsInput) (*dto.ItemListResult, error) {
	query = normalizeListInput(query)
	items, total, err := s.store.ListItems(query)
	if err != nil {
		return nil, err
	}
	list := make([]dto.ItemListDTO, 0, len(items))
	now := s.now()
	for _, iwr := range items {
		d := dto.NewItemListDTO(iwr.Item, iwr.Rule, s.policy, now)
		if iwr.Item.Status == model.ItemOngoing && s.cache != nil {
			if state, ok, _ := s.cache.GetAuctionState(context.Background(), iwr.Item.ID); ok {
				applyStateToList(&d, state, now)
			}
		}
		list = append(list, d)
	}
	return &dto.ItemListResult{
		List:     list,
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    total,
	}, nil
}
```

- [ ] **Step 6: Update `ListMerchantItems`**

```go
func (s *Service) ListMerchantItems(current *usermodel.User, query dto.ListItemsInput) (*dto.MerchantItemListResult, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	query.MerchantID = current.ID
	query = normalizeListInput(query)
	items, total, err := s.store.ListItems(query)
	if err != nil {
		return nil, err
	}
	list := make([]dto.MerchantItemDTO, 0, len(items))
	now := s.now()
	for _, iwr := range items {
		d := dto.NewMerchantItemDTO(iwr.Item, iwr.Rule, s.policy, now)
		if iwr.Item.Status == model.ItemOngoing && s.cache != nil {
			if state, ok, _ := s.cache.GetAuctionState(context.Background(), iwr.Item.ID); ok {
				d.Progress.CurrentPrice = state.CurrentPrice
				d.Progress.LeaderUserID = state.LeaderUserID
				d.Progress.BidCount = state.BidCount
				d.Progress.ParticipantCount = state.ParticipantCount
				d.Progress.IsExtended = state.IsExtended
				remaining := state.EndTime.Sub(now).Milliseconds()
				if remaining < 0 {
					remaining = 0
				}
				d.Progress.RemainingMS = remaining
			}
		}
		list = append(list, d)
	}
	return &dto.MerchantItemListResult{
		List:     list,
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    total,
	}, nil
}
```

- [ ] **Step 7: Run tests — must pass**

```bash
go test ./internal/app/item/service/... -run "TestGetItem"
```

Expected: PASS.

- [ ] **Step 8: Run all tests**

```bash
go test ./internal/app/item/...
```

Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/app/item/service/service.go internal/app/item/service/service_test.go
git commit -m "feat(item): enrich ongoing items from Redis in GetItem, ListItems, ListMerchantItems"
```

---

### Task 9: Wire cache in init.go and run full test suite

**Files:**
- Modify: `internal/app/item/init.go`

- [ ] **Step 1: Update `Load()` to wire `RedisCache`**

In `internal/app/item/init.go`:

```go
package item

import (
	"context"
	"errors"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/item/router"
	"github.com/zet-plane/live-auction-backend/internal/app/item/service"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

var ErrEmptyDatabase = errors.New("database pointer is nil")

type Item struct {
	Name string
	app.UnimplementedModule
}

func (i *Item) Info() string { return i.Name }

func (i *Item) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return ErrEmptyDatabase
	}
	store := dao.NewGormStore(engine.DB)
	return store.AutoMigrate()
}

func (i *Item) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)
	policy := dto.DefaultAuctionPolicy()
	if engine.Config.Auction.ExtendTriggerSec > 0 {
		policy.ExtendTriggerSec = engine.Config.Auction.ExtendTriggerSec
	}
	if engine.Config.Auction.AutoExtendSec > 0 {
		policy.AutoExtendSec = engine.Config.Auction.AutoExtendSec
	}
	if engine.Config.Auction.MaxExtendCount > 0 {
		policy.MaxExtendCount = engine.Config.Auction.MaxExtendCount
	}
	if engine.Config.Auction.MaxTotalExtendSec > 0 {
		policy.MaxTotalExtendSec = engine.Config.Auction.MaxTotalExtendSec
	}
	c := cache.NewRedisCache(engine.Cache)
	svc := service.NewService(store, policy, c)
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (i *Item) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
```

- [ ] **Step 2: Build entire project**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Run full test suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/app/item/init.go
git commit -m "feat(item): wire RedisCache into item module Load()"
```
