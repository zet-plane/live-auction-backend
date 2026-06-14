package service

import (
	"context"
	"testing"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

func TestVerifyBidLogContinuityAcceptsContinuousVersions(t *testing.T) {
	logs := []*itemmodel.BidLog{
		{ID: "bid_1", ItemID: "item_1", UserID: "u1", Price: 1000, AuthorityEpoch: 4, AuctionVersion: 1},
		{ID: "bid_2", ItemID: "item_1", UserID: "u2", Price: 1200, AuthorityEpoch: 4, AuctionVersion: 2},
	}
	result, ok := verifyBidLogContinuity(logs, 4)
	if !ok {
		t.Fatal("expected continuity")
	}
	if result.BidCount != 2 || result.LeaderUserID != "u2" || result.CurrentPrice != 1200 || result.AuctionVersion != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestVerifyBidLogContinuityRejectsGap(t *testing.T) {
	logs := []*itemmodel.BidLog{
		{ID: "bid_1", ItemID: "item_1", UserID: "u1", Price: 1000, AuthorityEpoch: 4, AuctionVersion: 1},
		{ID: "bid_3", ItemID: "item_1", UserID: "u2", Price: 1200, AuthorityEpoch: 4, AuctionVersion: 3},
	}
	_, ok := verifyBidLogContinuity(logs, 4)
	if ok {
		t.Fatal("expected continuity failure")
	}
}

func TestRebuildProtectsItemWhenContinuityFails(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	worker := newAvailabilityRebuildWorker(store, cache, availabilityRebuildConfig{BatchSize: 10})

	result := worker.rebuildItem(context.Background(), "item_1", 4)
	if result != rebuildProtected {
		t.Fatalf("result = %s, want %s", result, rebuildProtected)
	}
}

func TestRebuildAcceptsDurableMySQLPointAsAuthority(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	seedRebuildItemRule(t, store, "item_1", "room_1")
	store.bidLogsByEpoch["item_1:0"] = []*itemmodel.BidLog{
		{ID: "bid_1", ItemID: "item_1", UserID: "u1", Price: 1000, AuthorityEpoch: 0, AuctionVersion: 1},
		{ID: "bid_2", ItemID: "item_1", UserID: "u2", Price: 1200, AuthorityEpoch: 0, AuctionVersion: 2},
	}

	worker := newAvailabilityRebuildWorker(store, cache, availabilityRebuildConfig{BatchSize: 1, Policy: testPolicy})
	got := worker.rebuildItem(context.Background(), "item_1", 0)

	if got != rebuildReady {
		t.Fatalf("rebuild = %s, want ready", got)
	}
	state := cache.states["item_1"]
	if state.CurrentPrice != 1200 || state.LeaderUserID != "u2" || state.AuctionVersion != 2 {
		t.Fatalf("state = %+v", state)
	}
	if state.RoomID != "room_1" || state.BidIncrement != 100 || state.ParticipantCount != 2 {
		t.Fatalf("state hot fields = %+v", state)
	}
	ranking, err := cache.GetRanking(context.Background(), "item_1", 0, 10)
	if err != nil {
		t.Fatalf("GetRanking failed: %v", err)
	}
	if len(ranking) != 2 || ranking[0].UserID != "u2" || ranking[0].Price != 1200 {
		t.Fatalf("ranking = %+v", ranking)
	}
}

func TestRebuildSeedsNoBidOngoingItemFromDurableItemRule(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	endTime := time.Now().Add(5 * time.Minute).Truncate(time.Millisecond)
	store.items["item_1"] = &itemmodel.AuctionItem{
		ID:     "item_1",
		RuleID: "rule_1",
		RoomID: "room_1",
		Status: itemmodel.ItemOngoing,
	}
	store.rules["rule_1"] = &itemmodel.AuctionRule{
		ID:            "rule_1",
		ItemID:        "item_1",
		StartPrice:    1000,
		BidIncrement:  100,
		PriceCap:      5000,
		DepositAmount: 200,
		EndTime:       endTime,
	}

	worker := newAvailabilityRebuildWorker(store, cache, availabilityRebuildConfig{BatchSize: 1})
	got := worker.rebuildItem(context.Background(), "item_1", 0)

	if got != rebuildReady {
		t.Fatalf("rebuild = %s, want ready", got)
	}
	state := cache.states["item_1"]
	if state == nil {
		t.Fatal("expected rebuilt state")
	}
	if state.CurrentPrice != 1000 || state.DealPrice != 1000 || state.LeaderUserID != "" || state.BidCount != 0 {
		t.Fatalf("expected no-bid price state from durable rule, got %+v", state)
	}
	if state.AuctionVersion != 0 || state.AuthorityEpoch != 0 || state.AuthorityState != itemcache.AuthorityReady {
		t.Fatalf("expected fresh ready authority state, got %+v", state)
	}
	if state.Status != string(itemmodel.ItemOngoing) || state.RoomID != "room_1" || state.BidIncrement != 100 || state.EndTimeUnixMS != endTime.UnixMilli() {
		t.Fatalf("expected hot fields from durable item/rule, got %+v", state)
	}
	if _, ok, err := cache.GetAuctionHotConfig(context.Background(), "item_1"); err != nil || !ok {
		t.Fatalf("expected rebuilt state to be usable as hot config, ok=%v err=%v state=%+v", ok, err, state)
	}
	authority := cache.authority["item_1"]
	if authority.epoch != 0 || authority.state != itemcache.AuthorityReady {
		t.Fatalf("authority = %+v, want epoch 0 ready", authority)
	}
}

func TestRebuildActiveItemsUsesConfiguredBatchSize(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	seedRebuildItemRule(t, store, "item_1", "room_1")
	seedRebuildItemRule(t, store, "item_2", "room_2")
	store.bidLogsByEpoch["item_1:4"] = []*itemmodel.BidLog{
		{ID: "bid_1", ItemID: "item_1", UserID: "u1", Price: 1000, AuthorityEpoch: 4, AuctionVersion: 1},
	}
	store.bidLogsByEpoch["item_2:4"] = []*itemmodel.BidLog{
		{ID: "bid_2", ItemID: "item_2", UserID: "u2", Price: 1200, AuthorityEpoch: 4, AuctionVersion: 1},
	}
	worker := newAvailabilityRebuildWorker(store, cache, availabilityRebuildConfig{BatchSize: 1})

	results := worker.rebuildActiveItems(context.Background(), 4)
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	for _, got := range results {
		if got != rebuildReady {
			t.Fatalf("results = %+v", results)
		}
	}
}

func seedRebuildItemRule(t *testing.T, store *fakeStore, itemID, roomID string) {
	t.Helper()
	ruleID := "rule_" + itemID
	endTime := time.Now().Add(5 * time.Minute).Truncate(time.Millisecond)
	store.items[itemID] = &itemmodel.AuctionItem{
		ID:     itemID,
		RuleID: ruleID,
		RoomID: roomID,
		Status: itemmodel.ItemOngoing,
	}
	store.rules[ruleID] = &itemmodel.AuctionRule{
		ID:           ruleID,
		ItemID:       itemID,
		StartPrice:   1000,
		BidIncrement: 100,
		EndTime:      endTime,
	}
}
