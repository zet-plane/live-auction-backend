package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type fakeStore struct {
	items            map[string]*itemmodel.AuctionItem
	rules            map[string]*itemmodel.AuctionRule
	roomCurrentItems map[string]string
	updateErr        error
	bidLogs          []*itemmodel.BidLog
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		items:            map[string]*itemmodel.AuctionItem{},
		rules:            map[string]*itemmodel.AuctionRule{},
		roomCurrentItems: map[string]string{},
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

func (s *fakeStore) SetRoomCurrentItem(roomID, itemID string) error {
	s.roomCurrentItems[roomID] = itemID
	return nil
}

func (s *fakeStore) ClearRoomCurrentItem(roomID, itemID string) error {
	if s.roomCurrentItems[roomID] == itemID {
		delete(s.roomCurrentItems, roomID)
	}
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

func (s *fakeStore) ListOngoingItemsPastEndTime(before time.Time, limit int) ([]itemmodel.ItemWithRule, error) {
	var result []itemmodel.ItemWithRule
	for _, item := range s.items {
		if item.Status != itemmodel.ItemOngoing {
			continue
		}
		rule := s.rules[item.RuleID]
		if rule == nil || !rule.EndTime.Before(before) {
			continue
		}
		itemCopy := *item
		ruleCopy := *rule
		result = append(result, itemmodel.ItemWithRule{Item: &itemCopy, Rule: &ruleCopy})
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *fakeStore) AutoMigrateBidLog() error { return nil }

func (s *fakeStore) CreateBidLog(log *itemmodel.BidLog) error {
	cp := *log
	s.bidLogs = append(s.bidLogs, &cp)
	return nil
}

func (s *fakeStore) ListBidRanking(itemID string, limit int) ([]itemdto.BidderPrice, error) {
	best := map[string]int64{}
	for _, l := range s.bidLogs {
		if l.ItemID != itemID {
			continue
		}
		if l.Price > best[l.UserID] {
			best[l.UserID] = l.Price
		}
	}
	entries := make([]itemdto.BidderPrice, 0, len(best))
	for uid, price := range best {
		entries = append(entries, itemdto.BidderPrice{UserID: uid, Price: price})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Price > entries[j].Price })
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func TestCreateItemRequiresMerchantAndCreatesDraftItemWithRule(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{ExtendTriggerSec: 30, AutoExtendSec: 10, MaxExtendCount: 6, MaxTotalExtendSec: 300}, nil, nil, nil, nil)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	end := start.Add(10 * time.Minute)

	_, err := svc.CreateItem(context.Background(), &usermodel.User{ID: "user_1", Identity: usermodel.IdentityUser}, itemdto.CreateItemInput{
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

	result, err := svc.CreateItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemdto.CreateItemInput{
		RoomID:      "room_abc",
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
	if item.RoomID != "room_abc" {
		t.Fatalf("expected room_abc, got %q", item.RoomID)
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

func TestCreateItemRejectsMissingRoomID(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{}, nil, nil, nil, nil)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)

	_, err := svc.CreateItem(context.Background(),
		&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant},
		itemdto.CreateItemInput{
			Title: "翡翠手镯",
			Rule: itemdto.RuleInput{
				StartPrice:   1000,
				BidIncrement: 100,
				StartTime:    start,
				EndTime:      start.Add(10 * time.Minute),
			},
		},
	)
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request for missing room_id, got %v", err)
	}
	if len(store.items) != 0 {
		t.Fatalf("expected no item to be stored, got %d", len(store.items))
	}
}

func TestPublishStartAndCancelValidateOwnerAndStatus(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{}, nil, nil, nil, nil)
	itemID := seedDraftItem(t, svc, "merchant_1")

	if err := svc.PublishItem(context.Background(), &usermodel.User{ID: "merchant_2", Identity: usermodel.IdentityMerchant}, itemID); !errors.Is(err, errorx.ErrNotFound) {
		t.Fatalf("expected not found for another merchant, got %v", err)
	}
	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request when starting draft item, got %v", err)
	}
	if err := svc.PublishItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("PublishItem returned error: %v", err)
	}
	if store.items[itemID].Status != itemmodel.ItemPublished {
		t.Fatalf("expected published status, got %q", store.items[itemID].Status)
	}
	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("StartItem returned error: %v", err)
	}
	if store.items[itemID].Status != itemmodel.ItemOngoing {
		t.Fatalf("expected ongoing status, got %q", store.items[itemID].Status)
	}
	if err := svc.PublishItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request when publishing ongoing item, got %v", err)
	}
	if err := svc.CancelItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("CancelItem returned error: %v", err)
	}
	if store.items[itemID].Status != itemmodel.ItemCancelled {
		t.Fatalf("expected cancelled status, got %q", store.items[itemID].Status)
	}
}

type fakeCache struct {
	states      map[string]*itemcache.AuctionState
	queues      map[string][]string
	roomCurrent map[string]string
	ranking     map[string]map[string]int64
	bidderNames map[string]map[string]string
	bidLuaCode  int
	bidLuaErr   error
	initErr     error
	deleteErr   error
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		states:      map[string]*itemcache.AuctionState{},
		queues:      map[string][]string{},
		roomCurrent: map[string]string{},
		ranking:     map[string]map[string]int64{},
		bidderNames: map[string]map[string]string{},
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

func (c *fakeCache) SetRoomCurrentItem(_ context.Context, roomID, itemID string) error {
	c.roomCurrent[roomID] = itemID
	return nil
}

func (c *fakeCache) ClearRoomCurrentItem(_ context.Context, roomID, itemID string) error {
	if c.roomCurrent[roomID] == itemID {
		delete(c.roomCurrent, roomID)
	}
	return nil
}

func (c *fakeCache) PlaceBidLua(_ context.Context, itemID string, args itemcache.BidLuaArgs) (*itemcache.BidLuaResult, error) {
	if c.bidLuaErr != nil {
		return nil, c.bidLuaErr
	}
	if c.bidLuaCode != 0 {
		return &itemcache.BidLuaResult{Code: c.bidLuaCode}, nil
	}
	state, ok := c.states[itemID]
	if !ok {
		return &itemcache.BidLuaResult{Code: 2}, nil
	}
	if args.NowUnix >= state.EndTime.Unix() {
		return &itemcache.BidLuaResult{Code: 2}, nil
	}
	if args.Price <= state.CurrentPrice {
		return &itemcache.BidLuaResult{Code: 3}, nil
	}
	if args.BidIncrement > 0 && (args.Price-state.CurrentPrice)%args.BidIncrement != 0 {
		return &itemcache.BidLuaResult{Code: 4}, nil
	}
	if c.ranking[itemID] == nil {
		c.ranking[itemID] = map[string]int64{}
	}
	if _, exists := c.ranking[itemID][args.UserID]; !exists {
		state.ParticipantCount++
	}
	if args.Price > c.ranking[itemID][args.UserID] {
		c.ranking[itemID][args.UserID] = args.Price
	}
	if c.bidderNames[itemID] == nil {
		c.bidderNames[itemID] = map[string]string{}
	}
	c.bidderNames[itemID][args.UserID] = args.UserName
	prevLeader := state.LeaderUserID
	state.CurrentPrice = args.Price
	state.LeaderUserID = args.UserID
	state.BidCount++

	isExtended := false
	remaining := state.EndTime.Unix() - args.NowUnix
	if remaining <= int64(args.ExtendTriggerSec) &&
		state.ExtendCount < args.MaxExtendCount &&
		state.TotalExtendedSec+args.AutoExtendSec <= args.MaxTotalExtendSec {
		state.EndTime = state.EndTime.Add(time.Duration(args.AutoExtendSec) * time.Second)
		state.ExtendCount++
		state.TotalExtendedSec += args.AutoExtendSec
		isExtended = true
	}
	state.IsExtended = isExtended

	isCapped := args.PriceCap > 0 && args.Price >= args.PriceCap
	return &itemcache.BidLuaResult{
		Code:             0,
		BidID:            args.BidID,
		CurrentPrice:     args.Price,
		LeaderUserID:     args.UserID,
		EndTimeUnix:      state.EndTime.Unix(),
		IsExtended:       isExtended,
		IsCapped:         isCapped,
		PrevLeaderUserID: prevLeader,
	}, nil
}

func (c *fakeCache) GetRanking(_ context.Context, itemID string, offset, limit int) ([]itemdto.BidderPrice, error) {
	m := c.ranking[itemID]
	if len(m) == 0 {
		return nil, nil
	}
	entries := make([]itemdto.BidderPrice, 0, len(m))
	for uid, price := range m {
		name := ""
		if c.bidderNames[itemID] != nil {
			name = c.bidderNames[itemID][uid]
		}
		entries = append(entries, itemdto.BidderPrice{UserID: uid, UserName: name, Price: price})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Price > entries[j].Price })
	if offset >= len(entries) {
		return nil, nil
	}
	entries = entries[offset:]
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func TestCreateItemStoresRoomID(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{}, nil, nil, nil, nil)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)

	result, err := svc.CreateItem(context.Background(),
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

func TestUpdateItemRejectsBlankRoomIDAndKeepsExistingRoomID(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{}, nil, nil, nil, nil)
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedDraftItem(t, svc, merchant.ID)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)

	err := svc.UpdateItem(context.Background(), merchant, itemID, itemdto.CreateItemInput{
		RoomID: "   ",
		Title:  "Updated Item",
		Rule: itemdto.RuleInput{
			StartPrice:   1000,
			BidIncrement: 100,
			StartTime:    start,
			EndTime:      start.Add(10 * time.Minute),
		},
	})
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request for blank room_id, got %v", err)
	}
	if store.items[itemID].RoomID != "room_abc" {
		t.Fatalf("expected existing room_id to remain room_abc, got %q", store.items[itemID].RoomID)
	}
}

func TestUpdateItemPersistsRoomID(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, itemdto.AuctionPolicy{}, nil, nil, nil, nil)
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedDraftItem(t, svc, merchant.ID)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)

	err := svc.UpdateItem(context.Background(), merchant, itemID, itemdto.CreateItemInput{
		RoomID: " room_xyz ",
		Title:  "Updated Item",
		Rule: itemdto.RuleInput{
			StartPrice:   1000,
			BidIncrement: 100,
			StartTime:    start,
			EndTime:      start.Add(10 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("UpdateItem failed: %v", err)
	}
	if store.items[itemID].RoomID != "room_xyz" {
		t.Fatalf("expected room_id room_xyz, got %q", store.items[itemID].RoomID)
	}
}

func TestPublishItemPushesToRoomQueue(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)

	result, _ := svc.CreateItem(context.Background(),
		&usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant},
		itemdto.CreateItemInput{
			RoomID: "room_abc",
			Title:  "翡翠手镯",
			Rule:   itemdto.RuleInput{BidIncrement: 100, StartTime: start, EndTime: end},
		},
	)
	if err := svc.PublishItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, result.ItemID); err != nil {
		t.Fatalf("PublishItem failed: %v", err)
	}
	if len(fc.queues["room_abc"]) == 0 || fc.queues["room_abc"][0] != result.ItemID {
		t.Fatalf("expected item in room queue, got %v", fc.queues["room_abc"])
	}
}

func TestStartItemInitializesRedisState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")

	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
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

func TestStartItemSetsRoomCurrentItem(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")

	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("StartItem failed: %v", err)
	}
	if store.roomCurrentItems["room_abc"] != itemID {
		t.Fatalf("expected MySQL room current item %q, got %q", itemID, store.roomCurrentItems["room_abc"])
	}
	if fc.roomCurrent["room_abc"] != itemID {
		t.Fatalf("expected Redis room current item %q, got %q", itemID, fc.roomCurrent["room_abc"])
	}
}

func TestStartItemFailsWhenRedisInitFails(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.initErr = errors.New("redis down")
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")

	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err == nil {
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
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")

	store.updateErr = errors.New("mysql down")

	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err == nil {
		t.Fatal("expected error when MySQL fails")
	}
	if _, ok := fc.states[itemID]; ok {
		t.Fatal("expected Redis state to be rolled back after MySQL failure")
	}
}

func TestGetItemEnrichesFromCacheWhenOngoing(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	fc.states[itemID] = &itemcache.AuctionState{
		CurrentPrice: 5000,
		LeaderUserID: "user_99",
		EndTime:      time.Now().Add(time.Minute),
		BidCount:     3,
	}

	detail, err := svc.GetItem(context.Background(), itemID)
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
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)
	delete(fc.states, itemID)

	detail, err := svc.GetItem(context.Background(), itemID)
	if err != nil {
		t.Fatalf("GetItem should not fail on cache miss, got %v", err)
	}
	if detail.CurrentPrice == 0 {
		t.Fatalf("expected MySQL start_price fallback, got %d", detail.CurrentPrice)
	}
}

func TestCancelItemRemovesFromRoomQueueAndState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}

	_ = svc.StartItem(context.Background(), merchant, itemID)

	if err := svc.CancelItem(context.Background(), merchant, itemID); err != nil {
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

func TestCancelItemClearsRoomCurrentItem(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}

	_ = svc.StartItem(context.Background(), merchant, itemID)

	if err := svc.CancelItem(context.Background(), merchant, itemID); err != nil {
		t.Fatalf("CancelItem failed: %v", err)
	}
	if got := store.roomCurrentItems["room_abc"]; got != "" {
		t.Fatalf("expected MySQL room current item cleared, got %q", got)
	}
	if got := fc.roomCurrent["room_abc"]; got != "" {
		t.Fatalf("expected Redis room current item cleared, got %q", got)
	}
}

func seedPublishedItem(t *testing.T, svc *Service, merchantID, roomID string) string {
	t.Helper()
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	result, err := svc.CreateItem(context.Background(),
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
	if err := svc.PublishItem(context.Background(), &usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant}, result.ItemID); err != nil {
		t.Fatalf("PublishItem failed: %v", err)
	}
	return result.ItemID
}

func TestEndExpiredAuctionsBroadcastsAuctionEnded(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, itemdto.AuctionPolicy{ExtendTriggerSec: 30, AutoExtendSec: 10, MaxExtendCount: 6, MaxTotalExtendSec: 300}, fc, nil, nil, fb)

	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_abc", 0, 100, 0, endTime)

	// Set a leader in the cache
	fc.states[itemID].LeaderUserID = "user_winner"
	fc.states[itemID].CurrentPrice = 500

	// Advance time past the end time
	svc.now = func() time.Time { return time.Now().Add(10 * time.Minute) }

	svc.EndExpiredAuctions(context.Background())

	found := false
	for _, f := range fb.fanouts {
		if f.event.Type == itemdto.EventAuctionEnded && f.topic == wsevent.RoomTopic("room_abc") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected auction_ended fanout to room topic, got: %v", fb.fanouts)
	}
}

func seedDraftItem(t *testing.T, svc *Service, merchantID string) string {
	t.Helper()
	start := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	result, err := svc.CreateItem(context.Background(), &usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant}, itemdto.CreateItemInput{
		RoomID:   "room_abc",
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
