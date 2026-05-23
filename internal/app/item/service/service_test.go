package service

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

type fakeStore struct {
	items     map[string]*itemmodel.AuctionItem
	rules     map[string]*itemmodel.AuctionRule
	updateErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		items: map[string]*itemmodel.AuctionItem{},
		rules: map[string]*itemmodel.AuctionRule{},
	}
}

func (s *fakeStore) AutoMigrate() error { return nil }

func (s *fakeStore) CreateItemWithRule(item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule) error {
	itemCopy := *item
	ruleCopy := *rule
	s.items[item.ID] = &itemCopy
	s.rules[rule.ID] = &ruleCopy
	return nil
}

func (s *fakeStore) FindItemWithRule(itemID string) (*itemmodel.AuctionItem, *itemmodel.AuctionRule, error) {
	item, ok := s.items[itemID]
	if !ok {
		return nil, nil, errorx.ErrNotFound
	}
	rule, ok := s.rules[item.RuleID]
	if !ok {
		return nil, nil, errorx.ErrNotFound
	}
	itemCopy := *item
	ruleCopy := *rule
	return &itemCopy, &ruleCopy, nil
}

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

func (s *fakeStore) DeleteItem(itemID string) error {
	item, ok := s.items[itemID]
	if !ok {
		return errorx.ErrNotFound
	}
	delete(s.rules, item.RuleID)
	delete(s.items, itemID)
	return nil
}

func (s *fakeStore) ListItems(query itemdto.ListItemsInput) ([]itemmodel.ItemWithRule, int64, error) {
	list := make([]itemmodel.ItemWithRule, 0, len(s.items))
	for _, item := range s.items {
		if query.MerchantID != "" && item.MerchantID != query.MerchantID {
			continue
		}
		if query.Status != "" && item.Status != query.Status {
			continue
		}
		rule := s.rules[item.RuleID]
		itemCopy := *item
		ruleCopy := *rule
		list = append(list, itemmodel.ItemWithRule{Item: &itemCopy, Rule: &ruleCopy})
	}
	return list, int64(len(list)), nil
}

func TestCreateItemRequiresMerchantAndCreatesDraftItemWithRule(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{ExtendTriggerSec: 30, AutoExtendSec: 10, MaxExtendCount: 6, MaxTotalExtendSec: 300}, nil)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	end := start.Add(10 * time.Minute)

	_, err := svc.CreateItem(&usermodel.User{ID: "user_1", Identity: usermodel.IdentityUser}, itemdto.CreateItemInput{
		Title:       "翡翠手镯",
		Description: "天然翡翠，支持鉴定",
		ImageURL:    "https://example.com/item.png",
		Tags:        []string{"jewelry", "jade"},
		Rule: itemdto.RuleInput{
			StartPrice:    0,
			BidIncrement:  100,
			PriceCap:      100000,
			DepositAmount: 5000,
			StartTime:     start,
			EndTime:       end,
		},
	})
	if !errors.Is(err, errorx.ErrUnauthorized) {
		t.Fatalf("expected unauthorized for non-merchant, got %v", err)
	}

	result, err := svc.CreateItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemdto.CreateItemInput{
		Title:       " 翡翠手镯 ",
		Description: " 天然翡翠，支持鉴定 ",
		ImageURL:    " https://example.com/item.png ",
		Tags:        []string{" jewelry ", "jade"},
		Rule: itemdto.RuleInput{
			StartPrice:    0,
			BidIncrement:  100,
			PriceCap:      100000,
			DepositAmount: 5000,
			StartTime:     start,
			EndTime:       end,
		},
	})
	if err != nil {
		t.Fatalf("CreateItem returned error: %v", err)
	}
	if result.ItemID == "" || result.RuleID == "" {
		t.Fatalf("expected ids, got item=%q rule=%q", result.ItemID, result.RuleID)
	}

	item := store.items[result.ItemID]
	rule := store.rules[result.RuleID]
	if item == nil || rule == nil {
		t.Fatal("expected item and rule to be stored")
	}
	if item.MerchantID != "merchant_1" {
		t.Fatalf("expected merchant_1, got %q", item.MerchantID)
	}
	if item.Status != itemmodel.ItemDraft {
		t.Fatalf("expected draft status, got %q", item.Status)
	}
	if item.Title != "翡翠手镯" {
		t.Fatalf("expected trimmed title, got %q", item.Title)
	}
	if len(item.Tags) != 2 || item.Tags[0] != "jewelry" {
		t.Fatalf("expected trimmed tags, got %#v", item.Tags)
	}
	if rule.ItemID != item.ID || item.RuleID != rule.ID {
		t.Fatalf("expected item/rule linkage, got item.rule_id=%q rule.item_id=%q", item.RuleID, rule.ItemID)
	}
}

func TestPublishStartAndCancelValidateOwnerAndStatus(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{}, nil)
	itemID := seedDraftItem(t, svc, "merchant_1")

	if err := svc.PublishItem(&usermodel.User{ID: "merchant_2", Identity: usermodel.IdentityMerchant}, itemID); !errors.Is(err, errorx.ErrNotFound) {
		t.Fatalf("expected not found for another merchant, got %v", err)
	}
	if err := svc.StartItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request when starting draft item, got %v", err)
	}
	if err := svc.PublishItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("PublishItem returned error: %v", err)
	}
	if store.items[itemID].Status != itemmodel.ItemPublished {
		t.Fatalf("expected published status, got %q", store.items[itemID].Status)
	}
	if err := svc.StartItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("StartItem returned error: %v", err)
	}
	if store.items[itemID].Status != itemmodel.ItemOngoing {
		t.Fatalf("expected ongoing status, got %q", store.items[itemID].Status)
	}
	if err := svc.PublishItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request when publishing ongoing item, got %v", err)
	}
	if err := svc.CancelItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("CancelItem returned error: %v", err)
	}
	if store.items[itemID].Status != itemmodel.ItemCancelled {
		t.Fatalf("expected cancelled status, got %q", store.items[itemID].Status)
	}
}

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
	cp := state
	c.states[itemID] = &cp
	return nil
}

func (c *fakeCache) GetAuctionState(_ context.Context, itemID string) (*itemcache.AuctionState, bool, error) {
	s, ok := c.states[itemID]
	if !ok {
		return nil, false, nil
	}
	cp := *s
	return &cp, true, nil
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

	store.updateErr = errors.New("mysql down")

	if err := svc.StartItem(&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err == nil {
		t.Fatal("expected error when MySQL fails")
	}
	if _, ok := fc.states[itemID]; ok {
		t.Fatal("expected Redis state to be rolled back after MySQL failure")
	}
}

func TestCancelItemRemovesFromRoomQueueAndState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}

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

func seedDraftItem(t *testing.T, svc *Service, merchantID string) string {
	t.Helper()
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	result, err := svc.CreateItem(&usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant}, itemdto.CreateItemInput{
		Title:    "item",
		ImageURL: "https://example.com/item.png",
		Rule: itemdto.RuleInput{
			StartPrice:    100,
			BidIncrement:  10,
			PriceCap:      1000,
			DepositAmount: 50,
			StartTime:     start,
			EndTime:       start.Add(10 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("CreateItem returned error: %v", err)
	}
	return result.ItemID
}
