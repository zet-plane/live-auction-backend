package service

import (
	"context"
	"testing"

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

func TestRebuildActiveItemsUsesConfiguredBatchSize(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	store.items["item_1"] = &itemmodel.AuctionItem{ID: "item_1", Status: itemmodel.ItemOngoing}
	store.items["item_2"] = &itemmodel.AuctionItem{ID: "item_2", Status: itemmodel.ItemOngoing}
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

func TestRebuildContinuityCanSeedNextEpochFromPreviousEpoch(t *testing.T) {
	logs := []*itemmodel.BidLog{
		{ID: "bid_1", ItemID: "item_1", UserID: "u1", Price: 1000, AuthorityEpoch: 30, AuctionVersion: 1},
	}
	result, ok := verifyBidLogContinuity(logs, 30)
	if !ok {
		t.Fatal("expected continuity for previous epoch")
	}
	if result.AuctionVersion != 1 || result.CurrentPrice != 1000 || result.LeaderUserID != "u1" {
		t.Fatalf("result = %+v", result)
	}
}
