package service

import (
	"context"
	"sort"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

type rebuildResult string

const (
	rebuildReady     rebuildResult = "ready"
	rebuildProtected rebuildResult = "protected"
)

type continuityResult struct {
	BidCount       int
	CurrentPrice   int64
	LeaderUserID   string
	AuctionVersion int64
}

func verifyBidLogContinuity(logs []*itemmodel.BidLog, epoch int64) (continuityResult, bool) {
	if len(logs) == 0 {
		return continuityResult{}, false
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].AuctionVersion < logs[j].AuctionVersion })
	var result continuityResult
	for i, log := range logs {
		wantVersion := int64(i + 1)
		if log.AuthorityEpoch != epoch || log.AuctionVersion != wantVersion {
			return continuityResult{}, false
		}
		result.BidCount++
		result.AuctionVersion = log.AuctionVersion
		if log.Price >= result.CurrentPrice {
			result.CurrentPrice = log.Price
			result.LeaderUserID = log.UserID
		}
	}
	return result, true
}

type availabilityRebuildStore interface {
	ListActiveItemsForRebuild(limit int) ([]*itemmodel.AuctionItem, error)
	ListBidLogsForItemEpoch(itemID string, authorityEpoch int64) ([]*itemmodel.BidLog, error)
}

type availabilityRebuildCache interface {
	InitAuctionState(ctx context.Context, itemID string, state itemcache.AuctionState) error
	SetItemAuthority(ctx context.Context, itemID string, epoch int64, state string) error
}

type availabilityRebuildConfig struct {
	BatchSize int
}

type availabilityRebuildWorker struct {
	store availabilityRebuildStore
	cache availabilityRebuildCache
	cfg   availabilityRebuildConfig
}

func newAvailabilityRebuildWorker(store availabilityRebuildStore, cache availabilityRebuildCache, cfg availabilityRebuildConfig) *availabilityRebuildWorker {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	return &availabilityRebuildWorker{store: store, cache: cache, cfg: cfg}
}

func (w *availabilityRebuildWorker) rebuildActiveItems(ctx context.Context, epoch int64) map[string]rebuildResult {
	items, err := w.store.ListActiveItemsForRebuild(w.cfg.BatchSize)
	if err != nil {
		return map[string]rebuildResult{}
	}
	results := make(map[string]rebuildResult, len(items))
	for _, item := range items {
		if item == nil || item.ID == "" {
			continue
		}
		results[item.ID] = w.rebuildItem(ctx, item.ID, epoch)
	}
	return results
}

func (w *availabilityRebuildWorker) rebuildItem(ctx context.Context, itemID string, epoch int64) rebuildResult {
	logs, err := w.store.ListBidLogsForItemEpoch(itemID, epoch)
	if err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	continuity, ok := verifyBidLogContinuity(logs, epoch)
	if !ok {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	state := itemcache.AuctionState{
		AuthorityEpoch: epoch,
		AuthorityState: itemcache.AuthorityReady,
		AuctionVersion: continuity.AuctionVersion,
		CurrentPrice:   continuity.CurrentPrice,
		DealPrice:      continuity.CurrentPrice,
		LeaderUserID:   continuity.LeaderUserID,
		BidCount:       continuity.BidCount,
	}
	if err := w.cache.InitAuctionState(ctx, itemID, state); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityReady)
	return rebuildReady
}
