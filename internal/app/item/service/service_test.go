package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
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
	items                 map[string]*itemmodel.AuctionItem
	rules                 map[string]*itemmodel.AuctionRule
	roomCurrentItems      map[string]string
	updateErr             error
	setRoomCurrentErr     error
	clearRoomCurrentErr   error
	bidLogs               []*itemmodel.BidLog
	findMu                sync.Mutex
	findItemCalls         map[string]int
	findItemWithRuleCalls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		items:            map[string]*itemmodel.AuctionItem{},
		rules:            map[string]*itemmodel.AuctionRule{},
		roomCurrentItems: map[string]string{},
		findItemCalls:    map[string]int{},
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
	s.findMu.Lock()
	s.findItemWithRuleCalls++
	s.findItemCalls[itemID]++
	s.findMu.Unlock()

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
	if s.setRoomCurrentErr != nil {
		return s.setRoomCurrentErr
	}
	s.roomCurrentItems[roomID] = itemID
	return nil
}

func (s *fakeStore) GetRoomCurrentItem(roomID string) (string, bool, error) {
	itemID := s.roomCurrentItems[roomID]
	if itemID == "" {
		return "", false, nil
	}
	return itemID, true, nil
}

func (s *fakeStore) ClearRoomCurrentItem(roomID, itemID string) error {
	if s.clearRoomCurrentErr != nil {
		return s.clearRoomCurrentErr
	}
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
	states             map[string]*itemcache.AuctionState
	stateTTLs          map[string]time.Duration
	ending             map[string]int64
	queues             map[string][]string
	roomCurrent        map[string]string
	ranking            map[string]map[string]int64
	bidderNames        map[string]map[string]string
	listActiveCalls    atomic.Int32
	listActiveStarted  chan struct{}
	listActiveRelease  chan struct{}
	bidLuaCode         int
	bidLuaErr          error
	bidLuaResult       *itemcache.BidLuaResult
	lastBidLuaArgs     *itemcache.BidLuaArgs
	initCalls          int
	hotFieldUpdates    int
	endBeforeHotUpdate bool
	initErr            error
	getStateErr        error
	deleteErr          error
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		states:      map[string]*itemcache.AuctionState{},
		stateTTLs:   map[string]time.Duration{},
		ending:      map[string]int64{},
		queues:      map[string][]string{},
		roomCurrent: map[string]string{},
		ranking:     map[string]map[string]int64{},
		bidderNames: map[string]map[string]string{},
	}
}

func (c *fakeCache) InitAuctionState(_ context.Context, itemID string, state itemcache.AuctionState) error {
	c.initCalls++
	if c.initErr != nil {
		return c.initErr
	}
	cp := state
	if cp.Status == "" {
		cp.Status = "ongoing"
	}
	if cp.DealPrice == 0 {
		cp.DealPrice = cp.CurrentPrice
	}
	if cp.EndTimeUnixMS == 0 {
		cp.EndTimeUnixMS = cp.EndTime.UnixMilli()
	}
	c.states[itemID] = &cp
	return nil
}

func (c *fakeCache) GetAuctionState(_ context.Context, itemID string) (*itemcache.AuctionState, bool, error) {
	if c.getStateErr != nil {
		return nil, false, c.getStateErr
	}
	s, ok := c.states[itemID]
	if !ok {
		return nil, false, nil
	}
	cp := *s
	return &cp, true, nil
}

func (c *fakeCache) GetAuctionHotConfig(ctx context.Context, itemID string) (*itemcache.AuctionHotConfig, bool, error) {
	state, ok, err := c.GetAuctionState(ctx, itemID)
	if err != nil || !ok {
		return nil, ok, err
	}
	if state.Status == "" ||
		state.RoomID == "" ||
		state.BidIncrement <= 0 ||
		state.EndTimeUnixMS <= 0 ||
		state.ExtendTriggerSec <= 0 ||
		state.AutoExtendSec <= 0 ||
		state.MaxExtendCount <= 0 ||
		state.MaxTotalExtendSec <= 0 {
		return nil, false, nil
	}
	return &itemcache.AuctionHotConfig{
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

func (c *fakeCache) UpdateAuctionHotFields(_ context.Context, itemID string, hot itemcache.AuctionHotConfig) error {
	c.hotFieldUpdates++
	state, ok := c.states[itemID]
	if !ok {
		state = &itemcache.AuctionState{}
		c.states[itemID] = state
	}
	if c.endBeforeHotUpdate {
		state.Status = "ended"
	}
	state.RoomID = hot.RoomID
	state.BidIncrement = hot.BidIncrement
	state.PriceCap = hot.PriceCap
	state.DepositAmount = hot.DepositAmount
	state.ExtendTriggerSec = hot.ExtendTriggerSec
	state.AutoExtendSec = hot.AutoExtendSec
	state.MaxExtendCount = hot.MaxExtendCount
	state.MaxTotalExtendSec = hot.MaxTotalExtendSec
	return nil
}

func (c *fakeCache) DeleteAuctionState(_ context.Context, itemID string) error {
	if c.deleteErr != nil {
		return c.deleteErr
	}
	delete(c.states, itemID)
	delete(c.stateTTLs, itemID)
	return nil
}

func (c *fakeCache) ExpireAuctionState(_ context.Context, itemID string, ttl time.Duration) error {
	if _, ok := c.states[itemID]; !ok {
		return nil
	}
	c.stateTTLs[itemID] = ttl
	return nil
}

func (c *fakeCache) ScheduleAuctionEnd(_ context.Context, itemID string, endUnixMS int64) error {
	c.ending[itemID] = endUnixMS
	return nil
}

func (c *fakeCache) UnscheduleAuctionEnd(_ context.Context, itemID string) error {
	delete(c.ending, itemID)
	return nil
}

func (c *fakeCache) ListDueAuctionEnds(_ context.Context, nowUnixMS int64, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	var ids []string
	for itemID, endUnixMS := range c.ending {
		if endUnixMS <= nowUnixMS {
			ids = append(ids, itemID)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return c.ending[ids[i]] < c.ending[ids[j]]
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (c *fakeCache) ListActiveAuctionEnds(_ context.Context, limit int) ([]string, error) {
	c.listActiveCalls.Add(1)
	if c.listActiveStarted != nil {
		select {
		case c.listActiveStarted <- struct{}{}:
		default:
		}
	}
	if c.listActiveRelease != nil {
		<-c.listActiveRelease
	}
	if limit <= 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(c.ending))
	for itemID := range c.ending {
		ids = append(ids, itemID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return c.ending[ids[i]] < c.ending[ids[j]]
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (c *fakeCache) SettleAuctionLua(_ context.Context, itemID string, nowUnixMS int64) (*itemcache.SettlementResult, bool, error) {
	state, ok := c.states[itemID]
	if !ok {
		return nil, false, nil
	}
	if state.Status != "ongoing" {
		return nil, false, nil
	}
	endUnixMS := state.EndTimeUnixMS
	if endUnixMS == 0 && !state.EndTime.IsZero() {
		endUnixMS = state.EndTime.UnixMilli()
	}
	if endUnixMS == 0 || nowUnixMS < endUnixMS {
		return nil, false, nil
	}
	dealPrice := state.DealPrice
	if dealPrice == 0 {
		dealPrice = state.CurrentPrice
	}
	state.Status = "ended"
	state.EndedAtUnixMS = nowUnixMS
	state.EndReason = "time_expired"
	state.DealPrice = dealPrice
	delete(c.ending, itemID)
	return &itemcache.SettlementResult{
		ItemID:        itemID,
		LeaderUserID:  state.LeaderUserID,
		DealPrice:     dealPrice,
		EndedAtUnixMS: nowUnixMS,
		EndReason:     "time_expired",
	}, true, nil
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

func (c *fakeCache) GetRoomCurrentItem(_ context.Context, roomID string) (string, bool, error) {
	itemID := c.roomCurrent[roomID]
	if itemID == "" {
		return "", false, nil
	}
	return itemID, true, nil
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
	argsCopy := args
	c.lastBidLuaArgs = &argsCopy
	if c.bidLuaResult != nil {
		result := *c.bidLuaResult
		return &result, nil
	}
	if c.bidLuaCode != 0 {
		result := &itemcache.BidLuaResult{Code: c.bidLuaCode}
		if c.bidLuaCode == 1 {
			if state, ok := c.states[itemID]; ok {
				result.BidID = args.BidID
				result.CurrentPrice = state.DealPrice
				if result.CurrentPrice == 0 {
					result.CurrentPrice = state.CurrentPrice
				}
				result.LeaderUserID = state.LeaderUserID
				result.EndTimeUnix = state.EndTime.Unix()
				result.EndTimeUnixMS = state.EndTimeUnixMS
				result.Status = state.Status
			}
		}
		return result, nil
	}
	state, ok := c.states[itemID]
	if !ok {
		return &itemcache.BidLuaResult{Code: 2}, nil
	}
	if state.Status != "" && state.Status != "ongoing" {
		return &itemcache.BidLuaResult{Code: 2}, nil
	}
	endUnixMS := state.EndTimeUnixMS
	if endUnixMS == 0 {
		endUnixMS = state.EndTime.UnixMilli()
	}
	if args.NowUnix*1000 >= endUnixMS {
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
	state.DealPrice = args.Price
	state.LeaderUserID = args.UserID
	state.BidCount++
	state.EndTimeUnixMS = endUnixMS

	isExtended := false
	remaining := state.EndTimeUnixMS - args.NowUnix*1000
	if remaining <= int64(args.ExtendTriggerSec)*1000 &&
		state.ExtendCount < args.MaxExtendCount &&
		state.TotalExtendedSec+args.AutoExtendSec <= args.MaxTotalExtendSec {
		state.EndTime = state.EndTime.Add(time.Duration(args.AutoExtendSec) * time.Second)
		state.EndTimeUnixMS = state.EndTime.UnixMilli()
		c.ending[itemID] = state.EndTimeUnixMS
		state.ExtendCount++
		state.TotalExtendedSec += args.AutoExtendSec
		isExtended = true
	}
	state.IsExtended = isExtended

	isCapped := args.PriceCap > 0 && args.Price >= args.PriceCap
	if isCapped {
		state.Status = "ended"
		state.EndedAtUnixMS = time.Unix(args.NowUnix, 0).UnixMilli()
		state.EndReason = "price_cap"
		delete(c.ending, itemID)
	}
	return &itemcache.BidLuaResult{
		Code:             0,
		BidID:            args.BidID,
		CurrentPrice:     args.Price,
		LeaderUserID:     args.UserID,
		EndTimeUnix:      state.EndTime.Unix(),
		EndTimeUnixMS:    state.EndTimeUnixMS,
		IsExtended:       isExtended,
		IsCapped:         isCapped,
		PrevLeaderUserID: prevLeader,
		Status:           state.Status,
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
	if state.Status != "ongoing" {
		t.Fatalf("expected status ongoing, got %q", state.Status)
	}
	if state.DealPrice != 1000 {
		t.Fatalf("expected deal_price 1000 (start_price), got %d", state.DealPrice)
	}
	rule := store.rules[store.items[itemID].RuleID]
	if state.EndTimeUnixMS != rule.EndTime.UnixMilli() {
		t.Fatalf("expected end_time_unix_ms %d, got %d", rule.EndTime.UnixMilli(), state.EndTimeUnixMS)
	}
	if got := fc.ending[itemID]; got != rule.EndTime.UnixMilli() {
		t.Fatalf("expected ending score %d, got %d", rule.EndTime.UnixMilli(), got)
	}
	if state.EndTime.IsZero() {
		t.Fatal("expected non-zero end_time")
	}
}

func TestStartItemInitializesHotBidFields(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	policy := itemdto.AuctionPolicy{
		ExtendTriggerSec:  30,
		AutoExtendSec:     20,
		MaxExtendCount:    3,
		MaxTotalExtendSec: 60,
	}
	svc := NewService(store, policy, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_1")
	rule := store.rules[store.items[itemID].RuleID]
	rule.PriceCap = 5000
	rule.DepositAmount = 800

	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err != nil {
		t.Fatalf("StartItem failed: %v", err)
	}
	state, ok := fc.states[itemID]
	if !ok {
		t.Fatal("expected auction state in cache after StartItem")
	}
	if state.RoomID != "room_1" {
		t.Fatalf("expected room_id room_1, got %q", state.RoomID)
	}
	if state.BidIncrement != 100 {
		t.Fatalf("expected bid_increment 100, got %d", state.BidIncrement)
	}
	if state.PriceCap != 5000 {
		t.Fatalf("expected price_cap 5000, got %d", state.PriceCap)
	}
	if state.DepositAmount != 800 {
		t.Fatalf("expected deposit_amount 800, got %d", state.DepositAmount)
	}
	if state.ExtendTriggerSec != 30 {
		t.Fatalf("expected extend_trigger_sec 30, got %d", state.ExtendTriggerSec)
	}
	if state.AutoExtendSec != 20 {
		t.Fatalf("expected auto_extend_sec 20, got %d", state.AutoExtendSec)
	}
	if state.MaxExtendCount != 3 {
		t.Fatalf("expected max_extend_count 3, got %d", state.MaxExtendCount)
	}
	if state.MaxTotalExtendSec != 60 {
		t.Fatalf("expected max_total_extend_sec 60, got %d", state.MaxTotalExtendSec)
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
	if _, ok := fc.ending[itemID]; ok {
		t.Fatal("expected auction end schedule to be rolled back after MySQL failure")
	}
}

func TestStartItemRollsBackRedisOnRoomCurrentFailure(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")

	store.setRoomCurrentErr = errors.New("room current failed")

	if err := svc.StartItem(context.Background(), &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}, itemID); err == nil {
		t.Fatal("expected error when setting room current item fails")
	}
	if _, ok := fc.states[itemID]; ok {
		t.Fatal("expected Redis state to be rolled back after room current failure")
	}
	if _, ok := fc.ending[itemID]; ok {
		t.Fatal("expected auction end schedule to be rolled back after room current failure")
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
	if detail.DealPrice != 5000 {
		t.Fatalf("expected deal_price 5000 from Redis, got %d", detail.DealPrice)
	}
	if detail.EndTimeUnixMS == 0 {
		t.Fatal("expected end_time_unix_ms from Redis")
	}
	if detail.LeaderUserID != "user_99" {
		t.Fatalf("expected leader_user_id user_99, got %q", detail.LeaderUserID)
	}
}

func TestAuctionSnapshotReturnsRedisOngoingState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	svc.now = func() time.Time { return now }
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	endTime := now.Add(90 * time.Second)
	fc.states[itemID] = &itemcache.AuctionState{
		Status:           "ongoing",
		CurrentPrice:     3400,
		DealPrice:        3400,
		LeaderUserID:     "user_7",
		EndTime:          endTime,
		EndTimeUnixMS:    endTime.UnixMilli(),
		BidCount:         5,
		ParticipantCount: 3,
	}

	snapshot, ok, err := svc.AuctionSnapshot(context.Background(), itemID)
	if err != nil {
		t.Fatalf("AuctionSnapshot returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected auction snapshot")
	}
	if snapshot.ItemID != itemID {
		t.Fatalf("expected item_id %q, got %q", itemID, snapshot.ItemID)
	}
	if snapshot.Status != "ongoing" {
		t.Fatalf("expected status ongoing, got %q", snapshot.Status)
	}
	if snapshot.ServerTimeUnixMS != now.UnixMilli() {
		t.Fatalf("expected server_time_unix_ms %d, got %d", now.UnixMilli(), snapshot.ServerTimeUnixMS)
	}
	if snapshot.EndTimeUnixMS != endTime.UnixMilli() {
		t.Fatalf("expected end_time_unix_ms %d, got %d", endTime.UnixMilli(), snapshot.EndTimeUnixMS)
	}
	if snapshot.LeaderUserID != "user_7" || snapshot.DealPrice != 3400 {
		t.Fatalf("expected leader/deal user_7/3400, got %q/%d", snapshot.LeaderUserID, snapshot.DealPrice)
	}
	if snapshot.BidCount != 5 || snapshot.ParticipantCount != 3 {
		t.Fatalf("expected bid/participant counts 5/3, got %d/%d", snapshot.BidCount, snapshot.ParticipantCount)
	}
}

func TestAuctionSnapshotReturnsRedisEndedState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	svc.now = func() time.Time { return now }
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	endTime := now.Add(-time.Second)
	endedAt := now.UnixMilli()
	fc.states[itemID] = &itemcache.AuctionState{
		Status:           "ended",
		CurrentPrice:     1600,
		DealPrice:        1600,
		LeaderUserID:     "user_winner",
		EndTime:          endTime,
		EndTimeUnixMS:    endTime.UnixMilli(),
		EndedAtUnixMS:    endedAt,
		BidCount:         8,
		ParticipantCount: 2,
		EndReason:        "time_expired",
	}

	snapshot, ok, err := svc.AuctionSnapshot(context.Background(), itemID)
	if err != nil {
		t.Fatalf("AuctionSnapshot returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected auction snapshot")
	}
	if snapshot.Status != "ended" {
		t.Fatalf("expected status ended, got %q", snapshot.Status)
	}
	if snapshot.EndTimeUnixMS != endTime.UnixMilli() {
		t.Fatalf("expected end_time_unix_ms %d, got %d", endTime.UnixMilli(), snapshot.EndTimeUnixMS)
	}
	if snapshot.EndedAtUnixMS != endedAt {
		t.Fatalf("expected ended_at_unix_ms %d, got %d", endedAt, snapshot.EndedAtUnixMS)
	}
	if snapshot.LeaderUserID != "user_winner" || snapshot.DealPrice != 1600 {
		t.Fatalf("expected final leader/deal user_winner/1600, got %q/%d", snapshot.LeaderUserID, snapshot.DealPrice)
	}
	if snapshot.BidCount != 8 || snapshot.ParticipantCount != 2 {
		t.Fatalf("expected bid/participant counts 8/2, got %d/%d", snapshot.BidCount, snapshot.ParticipantCount)
	}
	if snapshot.EndReason != "time_expired" {
		t.Fatalf("expected end_reason time_expired, got %q", snapshot.EndReason)
	}
}

func TestBroadcastTimeSyncFansOutOngoingActiveAuctions(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, fb)
	svc.now = func() time.Time { return now }
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	endTime := now.Add(45 * time.Second)
	fc.states[itemID] = &itemcache.AuctionState{
		Status:        "ongoing",
		CurrentPrice:  1200,
		DealPrice:     1200,
		EndTime:       endTime,
		EndTimeUnixMS: endTime.UnixMilli(),
	}
	fc.ending[itemID] = endTime.UnixMilli()

	svc.BroadcastTimeSync(context.Background())

	var fanout fakeFanout
	found := false
	for _, f := range fb.fanouts {
		if f.event.Type == itemdto.EventTimeSync {
			fanout = f
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected time_sync fanout, got %+v", fb.fanouts)
	}
	if fanout.topic != wsevent.RoomTopic("room_abc") {
		t.Fatalf("expected room topic %q, got %q", wsevent.RoomTopic("room_abc"), fanout.topic)
	}
	if fanout.event.Type != itemdto.EventTimeSync {
		t.Fatalf("expected time_sync event, got %q", fanout.event.Type)
	}
	payload, ok := fanout.event.Payload.(itemdto.TimeSyncPayload)
	if !ok {
		t.Fatalf("expected TimeSyncPayload, got %T", fanout.event.Payload)
	}
	if payload.ItemID != itemID {
		t.Fatalf("expected item_id %q, got %q", itemID, payload.ItemID)
	}
	if payload.ServerTimeUnixMS != now.UnixMilli() {
		t.Fatalf("expected server_time_unix_ms %d, got %d", now.UnixMilli(), payload.ServerTimeUnixMS)
	}
	if payload.EndTimeUnixMS != endTime.UnixMilli() {
		t.Fatalf("expected end_time_unix_ms %d, got %d", endTime.UnixMilli(), payload.EndTimeUnixMS)
	}
	if payload.Status != "ongoing" {
		t.Fatalf("expected status ongoing, got %q", payload.Status)
	}
}

func TestBroadcastTimeSyncSkipsOverlappingRun(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, fb)
	svc.now = func() time.Time { return now }
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	endTime := now.Add(45 * time.Second)
	fc.states[itemID] = &itemcache.AuctionState{
		Status:        "ongoing",
		CurrentPrice:  1200,
		DealPrice:     1200,
		EndTime:       endTime,
		EndTimeUnixMS: endTime.UnixMilli(),
	}
	fc.ending[itemID] = endTime.UnixMilli()
	fc.listActiveStarted = make(chan struct{}, 2)
	fc.listActiveRelease = make(chan struct{})

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		svc.BroadcastTimeSync(context.Background())
	}()
	<-fc.listActiveStarted

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		svc.BroadcastTimeSync(context.Background())
	}()

	select {
	case <-fc.listActiveStarted:
		t.Fatal("expected overlapping BroadcastTimeSync call to return without listing active auctions")
	case <-secondDone:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("expected overlapping BroadcastTimeSync call to return quickly")
	}

	close(fc.listActiveRelease)
	<-firstDone
	if got := fc.listActiveCalls.Load(); got != 1 {
		t.Fatalf("expected one active auction listing, got %d", got)
	}
}

func TestBroadcastTimeSyncCachesRoomIDAfterFirstLookup(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, fb)
	svc.now = func() time.Time { return now }
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	endTime := now.Add(45 * time.Second)
	fc.states[itemID] = &itemcache.AuctionState{
		Status:        "ongoing",
		CurrentPrice:  1200,
		DealPrice:     1200,
		EndTime:       endTime,
		EndTimeUnixMS: endTime.UnixMilli(),
	}
	fc.ending[itemID] = endTime.UnixMilli()
	store.findMu.Lock()
	store.findItemCalls = map[string]int{}
	store.findMu.Unlock()

	svc.BroadcastTimeSync(context.Background())
	svc.BroadcastTimeSync(context.Background())

	store.findMu.Lock()
	got := store.findItemCalls[itemID]
	store.findMu.Unlock()
	if got != 1 {
		t.Fatalf("expected room_id lookup cached after first broadcast, got %d store lookups", got)
	}
	timeSyncCount := 0
	for _, f := range fb.fanouts {
		if f.event.Type == itemdto.EventTimeSync {
			timeSyncCount++
		}
	}
	if timeSyncCount != 2 {
		t.Fatalf("expected 2 time_sync fanouts, got %d", timeSyncCount)
	}
}

func TestGetItemMapsEndedRedisSnapshotWhenMySQLOngoing(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	svc.now = func() time.Time { return now }
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	endTime := now.Add(time.Minute)
	endedAt := now.Add(-time.Second).UnixMilli()
	fc.states[itemID] = &itemcache.AuctionState{
		Status:        "ended",
		CurrentPrice:  5000,
		DealPrice:     5000,
		LeaderUserID:  "user_99",
		EndTime:       endTime,
		EndTimeUnixMS: endTime.UnixMilli(),
		EndedAtUnixMS: endedAt,
		EndReason:     "time_expired",
	}

	detail, err := svc.GetItem(context.Background(), itemID)
	if err != nil {
		t.Fatalf("GetItem failed: %v", err)
	}
	if detail.Status != itemmodel.ItemEnded {
		t.Fatalf("expected status ended from Redis, got %q", detail.Status)
	}
	if detail.RemainingMS != 0 {
		t.Fatalf("expected remaining_ms 0 for ended Redis state, got %d", detail.RemainingMS)
	}
	if detail.DealPrice != 5000 {
		t.Fatalf("expected deal_price 5000 from Redis, got %d", detail.DealPrice)
	}
	if detail.EndTimeUnixMS != endTime.UnixMilli() {
		t.Fatalf("expected end_time_unix_ms %d, got %d", endTime.UnixMilli(), detail.EndTimeUnixMS)
	}
	if detail.EndedAtUnixMS != endedAt {
		t.Fatalf("expected ended_at_unix_ms %d, got %d", endedAt, detail.EndedAtUnixMS)
	}
	if detail.EndReason != "time_expired" {
		t.Fatalf("expected end_reason time_expired, got %q", detail.EndReason)
	}
}

func TestListMerchantItemsMapsRedisSnapshotFields(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	svc.now = func() time.Time { return now }
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	endTime := now.Add(time.Minute)
	endedAt := now.Add(-time.Second).UnixMilli()
	fc.states[itemID] = &itemcache.AuctionState{
		Status:           "ended",
		CurrentPrice:     6000,
		DealPrice:        6000,
		LeaderUserID:     "user_42",
		EndTime:          endTime,
		EndTimeUnixMS:    endTime.UnixMilli(),
		EndedAtUnixMS:    endedAt,
		EndReason:        "time_expired",
		BidCount:         7,
		ParticipantCount: 3,
	}

	result, err := svc.ListMerchantItems(context.Background(), merchant, itemdto.ListItemsInput{})
	if err != nil {
		t.Fatalf("ListMerchantItems failed: %v", err)
	}
	if len(result.List) != 1 {
		t.Fatalf("expected 1 merchant item, got %d", len(result.List))
	}
	item := result.List[0]
	if item.Status != itemmodel.ItemEnded {
		t.Fatalf("expected status ended from Redis, got %q", item.Status)
	}
	if item.StatusText != "已结束" {
		t.Fatalf("expected status_text 已结束, got %q", item.StatusText)
	}
	if item.ExplainStatus != "ended" {
		t.Fatalf("expected explain_status ended, got %q", item.ExplainStatus)
	}
	if item.Actions.CanCancel {
		t.Fatal("expected can_cancel false for ended Redis state")
	}
	if !item.Actions.CanUnpublish {
		t.Fatal("expected can_unpublish true for ended Redis state")
	}
	if item.DealPrice != 6000 {
		t.Fatalf("expected top-level deal_price 6000, got %d", item.DealPrice)
	}
	if item.EndTimeUnixMS != endTime.UnixMilli() {
		t.Fatalf("expected top-level end_time_unix_ms %d, got %d", endTime.UnixMilli(), item.EndTimeUnixMS)
	}
	if item.EndedAtUnixMS != endedAt {
		t.Fatalf("expected top-level ended_at_unix_ms %d, got %d", endedAt, item.EndedAtUnixMS)
	}
	if item.EndReason != "time_expired" {
		t.Fatalf("expected top-level end_reason time_expired, got %q", item.EndReason)
	}
	if item.Result.DealPrice != 6000 {
		t.Fatalf("expected result deal_price 6000, got %d", item.Result.DealPrice)
	}
	if item.Result.WinnerUserID != "user_42" {
		t.Fatalf("expected result winner_user_id user_42, got %q", item.Result.WinnerUserID)
	}
	if item.Progress.DealPrice != 6000 {
		t.Fatalf("expected progress deal_price 6000, got %d", item.Progress.DealPrice)
	}
	if item.Progress.EndTimeUnixMS != endTime.UnixMilli() {
		t.Fatalf("expected progress end_time_unix_ms %d, got %d", endTime.UnixMilli(), item.Progress.EndTimeUnixMS)
	}
	if item.Progress.EndedAtUnixMS != endedAt {
		t.Fatalf("expected progress ended_at_unix_ms %d, got %d", endedAt, item.Progress.EndedAtUnixMS)
	}
	if item.Progress.EndReason != "time_expired" {
		t.Fatalf("expected progress end_reason time_expired, got %q", item.Progress.EndReason)
	}
	if item.Progress.RemainingMS != 0 {
		t.Fatalf("expected progress remaining_ms 0 for ended Redis state, got %d", item.Progress.RemainingMS)
	}
}

func TestListMerchantItemsKeepsResultEmptyForRedisOngoingSnapshot(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, itemdto.AuctionPolicy{}, fc, nil, nil, nil)
	svc.now = func() time.Time { return now }
	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_abc")
	_ = svc.StartItem(context.Background(), merchant, itemID)

	endTime := now.Add(time.Minute)
	fc.states[itemID] = &itemcache.AuctionState{
		Status:           "ongoing",
		CurrentPrice:     7000,
		DealPrice:        7000,
		LeaderUserID:     "user_live",
		EndTime:          endTime,
		EndTimeUnixMS:    endTime.UnixMilli(),
		BidCount:         4,
		ParticipantCount: 2,
	}

	result, err := svc.ListMerchantItems(context.Background(), merchant, itemdto.ListItemsInput{})
	if err != nil {
		t.Fatalf("ListMerchantItems failed: %v", err)
	}
	if len(result.List) != 1 {
		t.Fatalf("expected 1 merchant item, got %d", len(result.List))
	}
	item := result.List[0]
	if item.Status != itemmodel.ItemOngoing {
		t.Fatalf("expected status ongoing from Redis, got %q", item.Status)
	}
	if item.Progress.LeaderUserID != "user_live" {
		t.Fatalf("expected progress leader_user_id user_live, got %q", item.Progress.LeaderUserID)
	}
	if item.Progress.DealPrice != 7000 {
		t.Fatalf("expected progress deal_price 7000, got %d", item.Progress.DealPrice)
	}
	if item.Result.WinnerUserID != "" {
		t.Fatalf("expected empty result winner_user_id for ongoing Redis state, got %q", item.Result.WinnerUserID)
	}
	if item.Result.DealPrice != 0 {
		t.Fatalf("expected result deal_price 0 for ongoing Redis state, got %d", item.Result.DealPrice)
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
	if _, ok := fc.ending[itemID]; ok {
		t.Fatal("expected auction end schedule removed from cache")
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

	if _, ok := fc.ending[itemID]; ok {
		t.Fatal("expected auction end schedule removed after expired auction settlement")
	}

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

func TestEndExpiredAuctionsSkipsWhenRedisStateReadFails(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	now := time.Now()
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_abc", 0, 100, 0, now.Add(-time.Minute))
	fc.getStateErr = errors.New("redis read failed")
	svc.now = func() time.Time { return now }

	svc.EndExpiredAuctions(context.Background())

	item := store.items[itemID]
	if item.Status != itemmodel.ItemOngoing {
		t.Fatalf("expected ongoing item, got %q", item.Status)
	}
	for _, f := range fb.fanouts {
		if f.event.Type == itemdto.EventAuctionEnded {
			t.Fatalf("expected no auction_ended fanout, got %+v", f)
		}
	}
}

func TestSettleDueAuctionsMarksEndedAndKeepsSnapshot(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	now := time.Now()
	endTime := now.Add(time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_abc", 0, 100, 0, endTime)
	dueEnd := now.Add(-time.Second)
	fc.states[itemID].EndTime = dueEnd
	fc.states[itemID].EndTimeUnixMS = dueEnd.UnixMilli()
	fc.states[itemID].LeaderUserID = "user_winner"
	fc.states[itemID].DealPrice = 500
	fc.states[itemID].CurrentPrice = 500
	fc.ending[itemID] = dueEnd.UnixMilli()
	svc.now = func() time.Time { return now }

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
	if got := fc.stateTTLs[itemID]; got != itemcache.FinalSnapshotTTL {
		t.Fatalf("expected final snapshot TTL %s, got %s", itemcache.FinalSnapshotTTL, got)
	}
}

func TestSettleDueAuctionsBroadcastsWhenRoomCurrentClearFails(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	now := time.Now()
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_abc", 0, 100, 0, now.Add(time.Minute))
	dueEnd := now.Add(-time.Second)
	fc.states[itemID].EndTime = dueEnd
	fc.states[itemID].EndTimeUnixMS = dueEnd.UnixMilli()
	fc.states[itemID].LeaderUserID = "user_winner"
	fc.states[itemID].DealPrice = 500
	fc.states[itemID].CurrentPrice = 500
	fc.ending[itemID] = dueEnd.UnixMilli()
	store.clearRoomCurrentErr = errors.New("clear room current failed")
	svc.now = func() time.Time { return now }

	svc.SettleDueAuctions(context.Background())

	item := store.items[itemID]
	if item.Status != itemmodel.ItemEnded {
		t.Fatalf("expected ended item, got %q", item.Status)
	}
	found := false
	for _, f := range fb.fanouts {
		if f.event.Type == itemdto.EventAuctionEnded && f.topic == wsevent.RoomTopic("room_abc") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected auction_ended fanout despite room cleanup error, got %+v", fb.fanouts)
	}
}

func TestSettleDueAuctionsBroadcastsFinalAuctionEndedPayload(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_abc", 0, 100, 0, now.Add(time.Minute))
	dueEnd := now.Add(-time.Second)
	fc.states[itemID].EndTime = dueEnd
	fc.states[itemID].EndTimeUnixMS = dueEnd.UnixMilli()
	fc.states[itemID].LeaderUserID = "user_winner"
	fc.states[itemID].DealPrice = 1600
	fc.states[itemID].CurrentPrice = 1600
	fc.ending[itemID] = dueEnd.UnixMilli()
	svc.now = func() time.Time { return now }

	svc.SettleDueAuctions(context.Background())

	var payload itemdto.AuctionEndedPayload
	found := false
	for _, f := range fb.fanouts {
		if f.event.Type != itemdto.EventAuctionEnded || f.topic != wsevent.RoomTopic("room_abc") {
			continue
		}
		got, ok := f.event.Payload.(itemdto.AuctionEndedPayload)
		if !ok {
			t.Fatalf("expected AuctionEndedPayload, got %T", f.event.Payload)
		}
		payload = got
		found = true
	}
	if !found {
		t.Fatalf("expected auction_ended fanout, got %+v", fb.fanouts)
	}
	if payload.ItemID != itemID {
		t.Fatalf("expected item_id %q, got %q", itemID, payload.ItemID)
	}
	if payload.WinnerUserID != "user_winner" || payload.LeaderUserID != "user_winner" {
		t.Fatalf("expected winner/leader user_winner, got %q/%q", payload.WinnerUserID, payload.LeaderUserID)
	}
	if payload.DealPrice != 1600 {
		t.Fatalf("expected deal_price 1600, got %d", payload.DealPrice)
	}
	if payload.EndedAtUnixMS != now.UnixMilli() {
		t.Fatalf("expected ended_at_unix_ms %d, got %d", now.UnixMilli(), payload.EndedAtUnixMS)
	}
	if payload.EndReason != "time_expired" {
		t.Fatalf("expected end_reason time_expired, got %q", payload.EndReason)
	}
}

func TestSettleDueAuctionsDoesNotFinalizeTwice(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_abc", 0, 100, 0, now.Add(time.Minute))
	dueEnd := now.Add(-time.Second)
	fc.states[itemID].EndTime = dueEnd
	fc.states[itemID].EndTimeUnixMS = dueEnd.UnixMilli()
	fc.states[itemID].LeaderUserID = "user_winner"
	fc.states[itemID].DealPrice = 1600
	fc.states[itemID].CurrentPrice = 1600
	fc.ending[itemID] = dueEnd.UnixMilli()
	svc.now = func() time.Time { return now }

	svc.SettleDueAuctions(context.Background())
	svc.SettleDueAuctions(context.Background())

	endedEvents := 0
	for _, f := range fb.fanouts {
		if f.event.Type == itemdto.EventAuctionEnded {
			endedEvents++
		}
	}
	if endedEvents != 1 {
		t.Fatalf("expected one auction_ended fanout after duplicate settlement runs, got %d: %+v", endedEvents, fb.fanouts)
	}
	item := store.items[itemID]
	if item.WinnerID != "user_winner" || item.DealPrice != 1600 {
		t.Fatalf("expected final result user_winner/1600, got %q/%d", item.WinnerID, item.DealPrice)
	}
	if fc.states[itemID].Status != "ended" {
		t.Fatalf("expected redis snapshot to remain ended, got %q", fc.states[itemID].Status)
	}
}

func TestSettleDueAuctionsSkipsStaleEndingWhenStateEndTimeFuture(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	now := time.Now()
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_abc", 0, 100, 0, now.Add(time.Minute))
	fc.ending[itemID] = now.Add(-time.Second).UnixMilli()
	svc.now = func() time.Time { return now }

	svc.SettleDueAuctions(context.Background())

	item := store.items[itemID]
	if item.Status != itemmodel.ItemOngoing {
		t.Fatalf("expected ongoing item, got %q", item.Status)
	}
	state := fc.states[itemID]
	if state == nil || state.Status != "ongoing" {
		t.Fatalf("expected ongoing redis snapshot, got %+v", state)
	}
	for _, f := range fb.fanouts {
		if f.event.Type == itemdto.EventAuctionEnded {
			t.Fatalf("expected no auction_ended fanout, got %+v", f)
		}
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
