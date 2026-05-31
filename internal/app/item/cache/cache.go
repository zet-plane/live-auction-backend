package cache

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
)

type AuctionState struct {
	Status           string
	CurrentPrice     int64
	DealPrice        int64
	LeaderUserID     string
	EndTime          time.Time
	EndTimeUnixMS    int64
	EndedAtUnixMS    int64
	BidCount         int
	ParticipantCount int
	IsExtended       bool
	ExtendCount      int
	TotalExtendedSec int
	EndReason        string
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
	EndTimeUnixMS    int64
	IsExtended       bool
	IsCapped         bool
	PrevLeaderUserID string // leader before this bid; empty if no previous leader
}

type Cache interface {
	InitAuctionState(ctx context.Context, itemID string, state AuctionState) error
	GetAuctionState(ctx context.Context, itemID string) (*AuctionState, bool, error)
	DeleteAuctionState(ctx context.Context, itemID string) error
	ScheduleAuctionEnd(ctx context.Context, itemID string, endUnixMS int64) error
	UnscheduleAuctionEnd(ctx context.Context, itemID string) error
	PushToRoomQueue(ctx context.Context, roomID, itemID string, score float64) error
	RemoveFromRoomQueue(ctx context.Context, roomID, itemID string) error
	SetRoomCurrentItem(ctx context.Context, roomID, itemID string) error
	ClearRoomCurrentItem(ctx context.Context, roomID, itemID string) error
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

func roomStateKey(roomID string) string {
	return "auction:room:" + roomID + ":state"
}

func endingKey() string {
	return "auction:ending"
}

func (c *RedisCache) InitAuctionState(ctx context.Context, itemID string, state AuctionState) error {
	status := state.Status
	if status == "" {
		status = "ongoing"
	}
	dealPrice := state.DealPrice
	if dealPrice == 0 {
		dealPrice = state.CurrentPrice
	}
	endUnixMS := state.EndTimeUnixMS
	if endUnixMS == 0 {
		endUnixMS = state.EndTime.UnixMilli()
	}
	return c.client.HSet(ctx, itemStateKey(itemID),
		"status", status,
		"current_price", state.CurrentPrice,
		"deal_price", dealPrice,
		"leader_user_id", state.LeaderUserID,
		"end_time_unix", state.EndTime.Unix(),
		"end_time_unix_ms", endUnixMS,
		"ended_at_unix_ms", state.EndedAtUnixMS,
		"bid_count", state.BidCount,
		"participant_count", state.ParticipantCount,
		"is_extended", boolToStr(state.IsExtended),
		"extend_count", state.ExtendCount,
		"total_extended_sec", state.TotalExtendedSec,
		"end_reason", state.EndReason,
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
	dealPrice := parseInt64(vals["deal_price"])
	if dealPrice == 0 {
		dealPrice = parseInt64(vals["current_price"])
	}
	endMS := parseInt64(vals["end_time_unix_ms"])
	if endMS == 0 {
		endMS = parseInt64(vals["end_time_unix"]) * 1000
	}
	return &AuctionState{
		Status:           vals["status"],
		CurrentPrice:     parseInt64(vals["current_price"]),
		DealPrice:        dealPrice,
		LeaderUserID:     vals["leader_user_id"],
		EndTime:          time.UnixMilli(endMS),
		EndTimeUnixMS:    endMS,
		EndedAtUnixMS:    parseInt64(vals["ended_at_unix_ms"]),
		BidCount:         parseInt(vals["bid_count"]),
		ParticipantCount: parseInt(vals["participant_count"]),
		IsExtended:       vals["is_extended"] == "1",
		ExtendCount:      parseInt(vals["extend_count"]),
		TotalExtendedSec: parseInt(vals["total_extended_sec"]),
		EndReason:        vals["end_reason"],
	}, true, nil
}

func (c *RedisCache) DeleteAuctionState(ctx context.Context, itemID string) error {
	return c.client.Del(ctx, itemStateKey(itemID)).Err()
}

func (c *RedisCache) ScheduleAuctionEnd(ctx context.Context, itemID string, endUnixMS int64) error {
	return c.client.ZAdd(ctx, endingKey(), redis.Z{Score: float64(endUnixMS), Member: itemID}).Err()
}

func (c *RedisCache) UnscheduleAuctionEnd(ctx context.Context, itemID string) error {
	return c.client.ZRem(ctx, endingKey(), itemID).Err()
}

func (c *RedisCache) PushToRoomQueue(ctx context.Context, roomID, itemID string, score float64) error {
	return c.client.ZAdd(ctx, roomQueueKey(roomID), redis.Z{Score: score, Member: itemID}).Err()
}

func (c *RedisCache) RemoveFromRoomQueue(ctx context.Context, roomID, itemID string) error {
	return c.client.ZRem(ctx, roomQueueKey(roomID), itemID).Err()
}

func (c *RedisCache) SetRoomCurrentItem(ctx context.Context, roomID, itemID string) error {
	return c.client.HSet(ctx, roomStateKey(roomID), "current_item_id", itemID).Err()
}

func (c *RedisCache) ClearRoomCurrentItem(ctx context.Context, roomID, itemID string) error {
	current, err := c.client.HGet(ctx, roomStateKey(roomID), "current_item_id").Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != itemID {
		return nil
	}
	return c.client.HSet(ctx, roomStateKey(roomID), "current_item_id", "").Err()
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
