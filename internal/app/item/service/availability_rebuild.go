package service

import (
	"context"
	"sort"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

type rebuildResult string

const (
	rebuildReady     rebuildResult = "ready"
	rebuildProtected rebuildResult = "protected"
)

type continuityResult struct {
	BidCount         int
	ParticipantCount int
	CurrentPrice     int64
	LeaderUserID     string
	AuctionVersion   int64
	Ranking          []itemdto.BidderPrice
}

func verifyBidLogContinuity(logs []*itemmodel.BidLog, epoch int64) (continuityResult, bool) {
	if len(logs) == 0 {
		return continuityResult{}, false
	}
	sort.Slice(logs, func(i, j int) bool {
		if hasCreatedAt(logs[i]) && hasCreatedAt(logs[j]) && !logs[i].CreatedAt.Equal(logs[j].CreatedAt) {
			return logs[i].CreatedAt.Before(logs[j].CreatedAt)
		}
		if logs[i].AuthorityEpoch != logs[j].AuthorityEpoch {
			return logs[i].AuthorityEpoch < logs[j].AuthorityEpoch
		}
		return logs[i].AuctionVersion < logs[j].AuctionVersion
	})
	var result continuityResult
	bestByUser := make(map[string]int64)
	for _, log := range logs {
		result.BidCount++
		if log.AuctionVersion > result.AuctionVersion {
			result.AuctionVersion = log.AuctionVersion
		}
		if log.Price >= result.CurrentPrice {
			result.CurrentPrice = log.Price
			result.LeaderUserID = log.UserID
		}
		if log.UserID != "" && log.Price > bestByUser[log.UserID] {
			bestByUser[log.UserID] = log.Price
		}
	}
	result.ParticipantCount = len(bestByUser)
	result.Ranking = make([]itemdto.BidderPrice, 0, len(bestByUser))
	for userID, price := range bestByUser {
		result.Ranking = append(result.Ranking, itemdto.BidderPrice{UserID: userID, Price: price})
	}
	sort.Slice(result.Ranking, func(i, j int) bool { return result.Ranking[i].Price > result.Ranking[j].Price })
	return result, true
}

func hasCreatedAt(log *itemmodel.BidLog) bool {
	return log != nil && !log.CreatedAt.IsZero() && !log.CreatedAt.Equal(time.Time{})
}

type availabilityRebuildStore interface {
	FindItemWithRule(itemID string) (*itemmodel.AuctionItem, *itemmodel.AuctionRule, error)
	ListActiveItemsForRebuild(limit int) ([]*itemmodel.AuctionItem, error)
	ListBidLogsForItemEpoch(itemID string, authorityEpoch int64) ([]*itemmodel.BidLog, error)
}

type availabilityRebuildCache interface {
	InitAuctionState(ctx context.Context, itemID string, state itemcache.AuctionState) error
	SetItemAuthority(ctx context.Context, itemID string, epoch int64, state string) error
	SetItemDetail(ctx context.Context, itemID string, detail itemcache.ItemDetailCache) error
	SetRanking(ctx context.Context, itemID string, entries []itemdto.BidderPrice) error
	ScheduleAuctionEnd(ctx context.Context, itemID string, endUnixMS int64) error
	SetRoomCurrentItem(ctx context.Context, roomID, itemID string) error
}

type availabilityRebuildConfig struct {
	BatchSize int
	Policy    itemdto.AuctionPolicy
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
	if cfg.Policy.ExtendTriggerSec <= 0 ||
		cfg.Policy.AutoExtendSec <= 0 ||
		cfg.Policy.MaxExtendCount <= 0 ||
		cfg.Policy.MaxTotalExtendSec <= 0 {
		cfg.Policy = itemdto.DefaultAuctionPolicy()
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
	item, rule, err := w.store.FindItemWithRule(itemID)
	if err != nil || item == nil || rule == nil || item.Status != itemmodel.ItemOngoing {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	logs, err := w.store.ListBidLogsForItemEpoch(itemID, epoch)
	if err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if len(logs) == 0 {
		return w.rebuildNoBidItem(ctx, item, rule, epoch)
	}
	continuity, ok := verifyBidLogContinuity(logs, epoch)
	if !ok {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	state := itemcache.AuctionState{
		AuthorityEpoch:    epoch,
		AuthorityState:    itemcache.AuthorityReady,
		AuctionVersion:    continuity.AuctionVersion,
		Status:            string(item.Status),
		RoomID:            item.RoomID,
		CurrentPrice:      continuity.CurrentPrice,
		DealPrice:         continuity.CurrentPrice,
		LeaderUserID:      continuity.LeaderUserID,
		EndTime:           rule.EndTime,
		EndTimeUnixMS:     rule.EndTime.UnixMilli(),
		BidIncrement:      rule.BidIncrement,
		PriceCap:          rule.PriceCap,
		DepositAmount:     rule.DepositAmount,
		ExtendTriggerSec:  w.cfg.Policy.ExtendTriggerSec,
		AutoExtendSec:     w.cfg.Policy.AutoExtendSec,
		MaxExtendCount:    w.cfg.Policy.MaxExtendCount,
		MaxTotalExtendSec: w.cfg.Policy.MaxTotalExtendSec,
		BidCount:          continuity.BidCount,
		ParticipantCount:  continuity.ParticipantCount,
	}
	if err := w.cache.InitAuctionState(ctx, itemID, state); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if err := w.cache.SetItemDetail(ctx, itemID, itemcache.ItemDetailCache{Item: item, Rule: rule}); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if err := w.cache.SetRanking(ctx, itemID, continuity.Ranking); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if err := w.cache.ScheduleAuctionEnd(ctx, itemID, rule.EndTime.UnixMilli()); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if err := w.cache.SetRoomCurrentItem(ctx, item.RoomID, itemID); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityReady)
	return rebuildReady
}

func (w *availabilityRebuildWorker) rebuildNoBidItem(ctx context.Context, item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule, epoch int64) rebuildResult {
	itemID := item.ID
	state := itemcache.AuctionState{
		AuthorityEpoch:    epoch,
		AuthorityState:    itemcache.AuthorityReady,
		AuctionVersion:    0,
		Status:            string(item.Status),
		RoomID:            item.RoomID,
		CurrentPrice:      rule.StartPrice,
		DealPrice:         rule.StartPrice,
		EndTime:           rule.EndTime,
		EndTimeUnixMS:     rule.EndTime.UnixMilli(),
		BidIncrement:      rule.BidIncrement,
		PriceCap:          rule.PriceCap,
		DepositAmount:     rule.DepositAmount,
		ExtendTriggerSec:  w.cfg.Policy.ExtendTriggerSec,
		AutoExtendSec:     w.cfg.Policy.AutoExtendSec,
		MaxExtendCount:    w.cfg.Policy.MaxExtendCount,
		MaxTotalExtendSec: w.cfg.Policy.MaxTotalExtendSec,
		BidCount:          0,
	}
	if err := w.cache.InitAuctionState(ctx, itemID, state); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if err := w.cache.SetItemDetail(ctx, itemID, itemcache.ItemDetailCache{Item: item, Rule: rule}); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if err := w.cache.SetRanking(ctx, itemID, nil); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if err := w.cache.ScheduleAuctionEnd(ctx, itemID, rule.EndTime.UnixMilli()); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if err := w.cache.SetRoomCurrentItem(ctx, item.RoomID, itemID); err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityReady)
	return rebuildReady
}
