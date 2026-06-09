package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
)

var errActiveRedisUnavailable = errors.New("active redis unavailable")

type activeRedisProvider interface {
	ActiveRedis() (*redis.Client, availability.Snapshot, bool)
}

type ActiveRedisCache struct {
	provider activeRedisProvider
}

func NewActiveRedisCache(provider activeRedisProvider) *ActiveRedisCache {
	return &ActiveRedisCache{provider: provider}
}

func (c *ActiveRedisCache) current() (*RedisCache, error) {
	if c == nil || c.provider == nil {
		return nil, errActiveRedisUnavailable
	}
	client, _, ok := c.provider.ActiveRedis()
	if !ok || client == nil {
		return nil, errActiveRedisUnavailable
	}
	return NewRedisCache(client), nil
}

func (c *ActiveRedisCache) SetItemDetail(ctx context.Context, itemID string, detail ItemDetailCache) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.SetItemDetail(ctx, itemID, detail)
}

func (c *ActiveRedisCache) GetItemDetail(ctx context.Context, itemID string) (*ItemDetailCache, bool, error) {
	rc, err := c.current()
	if err != nil {
		return nil, false, err
	}
	return rc.GetItemDetail(ctx, itemID)
}

func (c *ActiveRedisCache) DeleteItemDetail(ctx context.Context, itemID string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.DeleteItemDetail(ctx, itemID)
}

func (c *ActiveRedisCache) InitAuctionState(ctx context.Context, itemID string, state AuctionState) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.InitAuctionState(ctx, itemID, state)
}

func (c *ActiveRedisCache) GetAuctionState(ctx context.Context, itemID string) (*AuctionState, bool, error) {
	rc, err := c.current()
	if err != nil {
		return nil, false, err
	}
	return rc.GetAuctionState(ctx, itemID)
}

func (c *ActiveRedisCache) GetAuctionHotConfig(ctx context.Context, itemID string) (*AuctionHotConfig, bool, error) {
	rc, err := c.current()
	if err != nil {
		return nil, false, err
	}
	return rc.GetAuctionHotConfig(ctx, itemID)
}

func (c *ActiveRedisCache) UpdateAuctionHotFields(ctx context.Context, itemID string, hot AuctionHotConfig) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.UpdateAuctionHotFields(ctx, itemID, hot)
}

func (c *ActiveRedisCache) DeleteAuctionState(ctx context.Context, itemID string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.DeleteAuctionState(ctx, itemID)
}

func (c *ActiveRedisCache) ExpireAuctionState(ctx context.Context, itemID string, ttl time.Duration) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.ExpireAuctionState(ctx, itemID, ttl)
}

func (c *ActiveRedisCache) ScheduleAuctionEnd(ctx context.Context, itemID string, endUnixMS int64) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.ScheduleAuctionEnd(ctx, itemID, endUnixMS)
}

func (c *ActiveRedisCache) UnscheduleAuctionEnd(ctx context.Context, itemID string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.UnscheduleAuctionEnd(ctx, itemID)
}

func (c *ActiveRedisCache) ListDueAuctionEnds(ctx context.Context, nowUnixMS int64, limit int) ([]string, error) {
	rc, err := c.current()
	if err != nil {
		return nil, err
	}
	return rc.ListDueAuctionEnds(ctx, nowUnixMS, limit)
}

func (c *ActiveRedisCache) ListActiveAuctionEnds(ctx context.Context, limit int) ([]string, error) {
	rc, err := c.current()
	if err != nil {
		return nil, err
	}
	return rc.ListActiveAuctionEnds(ctx, limit)
}

func (c *ActiveRedisCache) SettleAuctionLua(ctx context.Context, itemID string, nowUnixMS int64) (*SettlementResult, bool, error) {
	rc, err := c.current()
	if err != nil {
		return nil, false, err
	}
	return rc.SettleAuctionLua(ctx, itemID, nowUnixMS)
}

func (c *ActiveRedisCache) SetItemAuthority(ctx context.Context, itemID string, epoch int64, state string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.SetItemAuthority(ctx, itemID, epoch, state)
}

func (c *ActiveRedisCache) GetItemAuthority(ctx context.Context, itemID string) (int64, string, bool, error) {
	rc, err := c.current()
	if err != nil {
		return 0, "", false, err
	}
	return rc.GetItemAuthority(ctx, itemID)
}

func (c *ActiveRedisCache) PushToRoomQueue(ctx context.Context, roomID, itemID string, score float64) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.PushToRoomQueue(ctx, roomID, itemID, score)
}

func (c *ActiveRedisCache) RemoveFromRoomQueue(ctx context.Context, roomID, itemID string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.RemoveFromRoomQueue(ctx, roomID, itemID)
}

func (c *ActiveRedisCache) SetRoomCurrentItem(ctx context.Context, roomID, itemID string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.SetRoomCurrentItem(ctx, roomID, itemID)
}

func (c *ActiveRedisCache) GetRoomCurrentItem(ctx context.Context, roomID string) (string, bool, error) {
	rc, err := c.current()
	if err != nil {
		return "", false, err
	}
	return rc.GetRoomCurrentItem(ctx, roomID)
}

func (c *ActiveRedisCache) ClearRoomCurrentItem(ctx context.Context, roomID, itemID string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.ClearRoomCurrentItem(ctx, roomID, itemID)
}

func (c *ActiveRedisCache) PlaceBidLua(ctx context.Context, itemID string, args BidLuaArgs) (*BidLuaResult, error) {
	rc, err := c.current()
	if err != nil {
		return nil, err
	}
	return rc.PlaceBidLua(ctx, itemID, args)
}

func (c *ActiveRedisCache) AppendBidLogEvent(ctx context.Context, event BidLogEvent) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.AppendBidLogEvent(ctx, event)
}

func (c *ActiveRedisCache) GetRanking(ctx context.Context, itemID string, offset, limit int) ([]dto.BidderPrice, error) {
	rc, err := c.current()
	if err != nil {
		return nil, err
	}
	return rc.GetRanking(ctx, itemID, offset, limit)
}

func (c *ActiveRedisCache) SetRanking(ctx context.Context, itemID string, entries []dto.BidderPrice) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.SetRanking(ctx, itemID, entries)
}

func (c *ActiveRedisCache) GetUserRanking(ctx context.Context, itemID, userID string) (*dto.CurrentUserRanking, error) {
	rc, err := c.current()
	if err != nil {
		return nil, err
	}
	return rc.GetUserRanking(ctx, itemID, userID)
}

func (c *ActiveRedisCache) AcquireRankingRebuild(ctx context.Context, itemID, owner string, ttl time.Duration) (bool, error) {
	rc, err := c.current()
	if err != nil {
		return false, err
	}
	return rc.AcquireRankingRebuild(ctx, itemID, owner, ttl)
}

func (c *ActiveRedisCache) SetRankingRebuildCooldown(ctx context.Context, itemID string, ttl time.Duration) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.SetRankingRebuildCooldown(ctx, itemID, ttl)
}

func (c *ActiveRedisCache) RankingRebuildCoolingDown(ctx context.Context, itemID string) (bool, error) {
	rc, err := c.current()
	if err != nil {
		return false, err
	}
	return rc.RankingRebuildCoolingDown(ctx, itemID)
}
