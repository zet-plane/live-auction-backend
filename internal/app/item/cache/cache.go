package cache

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
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

type BidLuaArgs struct {
	UserID            string
	UserName          string
	BidID             string
	Price             int64
	BidIncrement      int64
	PriceCap          int64
	ExtendTriggerSec  int
	AutoExtendSec     int
	MaxExtendCount    int
	MaxTotalExtendSec int
	NowUnix           int64
	IdempotencyKey    string
	IdempotencyTTL    int
}

type BidLuaResult struct {
	Code             int
	BidID            string
	CurrentPrice     int64
	LeaderUserID     string
	EndTimeUnix      int64
	IsExtended       bool
	IsCapped         bool
	PrevLeaderUserID string // leader before this bid; empty if no previous leader
}

type Cache interface {
	InitAuctionState(ctx context.Context, itemID string, state AuctionState) error
	GetAuctionState(ctx context.Context, itemID string) (*AuctionState, bool, error)
	DeleteAuctionState(ctx context.Context, itemID string) error
	PushToRoomQueue(ctx context.Context, roomID, itemID string, score float64) error
	RemoveFromRoomQueue(ctx context.Context, roomID, itemID string) error
	PlaceBidLua(ctx context.Context, itemID string, args BidLuaArgs) (*BidLuaResult, error)
	GetRanking(ctx context.Context, itemID string, offset, limit int) ([]dto.BidderPrice, error)
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
