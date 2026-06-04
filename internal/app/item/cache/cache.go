package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
)

type AuctionState struct {
	Status            string
	RoomID            string
	CurrentPrice      int64
	DealPrice         int64
	LeaderUserID      string
	EndTime           time.Time
	EndTimeUnixMS     int64
	EndedAtUnixMS     int64
	BidIncrement      int64
	PriceCap          int64
	DepositAmount     int64
	BidCount          int
	ParticipantCount  int
	IsExtended        bool
	ExtendCount       int
	TotalExtendedSec  int
	ExtendTriggerSec  int
	AutoExtendSec     int
	MaxExtendCount    int
	MaxTotalExtendSec int
	EndReason         string
}

type AuctionHotConfig struct {
	ItemID            string
	RoomID            string
	Status            string
	BidIncrement      int64
	PriceCap          int64
	DepositAmount     int64
	ExtendTriggerSec  int
	AutoExtendSec     int
	MaxExtendCount    int
	MaxTotalExtendSec int
	EndTimeUnixMS     int64
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
	Status           string
}

type SettlementResult struct {
	ItemID        string
	LeaderUserID  string
	DealPrice     int64
	EndedAtUnixMS int64
	EndReason     string
}

const FinalSnapshotTTL = 24 * time.Hour

type Cache interface {
	InitAuctionState(ctx context.Context, itemID string, state AuctionState) error
	GetAuctionState(ctx context.Context, itemID string) (*AuctionState, bool, error)
	GetAuctionHotConfig(ctx context.Context, itemID string) (*AuctionHotConfig, bool, error)
	DeleteAuctionState(ctx context.Context, itemID string) error
	ExpireAuctionState(ctx context.Context, itemID string, ttl time.Duration) error
	ScheduleAuctionEnd(ctx context.Context, itemID string, endUnixMS int64) error
	UnscheduleAuctionEnd(ctx context.Context, itemID string) error
	ListDueAuctionEnds(ctx context.Context, nowUnixMS int64, limit int) ([]string, error)
	ListActiveAuctionEnds(ctx context.Context, limit int) ([]string, error)
	SettleAuctionLua(ctx context.Context, itemID string, nowUnixMS int64) (*SettlementResult, bool, error)
	PushToRoomQueue(ctx context.Context, roomID, itemID string, score float64) error
	RemoveFromRoomQueue(ctx context.Context, roomID, itemID string) error
	SetRoomCurrentItem(ctx context.Context, roomID, itemID string) error
	GetRoomCurrentItem(ctx context.Context, roomID string) (string, bool, error)
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

const settleAuctionLuaScript = `
local state_key = KEYS[1]
local ending_key = KEYS[2]
local now_ms = tonumber(ARGV[1])
local item_id = ARGV[2]

local raw = redis.call('HGETALL', state_key)
if #raw == 0 then return {0} end

local s = {}
for i = 1, #raw, 2 do s[raw[i]] = raw[i+1] end

if (s['status'] or '') ~= 'ongoing' then return {0} end

local end_ms = tonumber(s['end_time_unix_ms'] or 0)
if end_ms == 0 then
  end_ms = tonumber(s['end_time_unix'] or 0) * 1000
end
if end_ms == 0 or end_ms > now_ms then return {0} end

local leader = s['leader_user_id'] or ''
local deal = tonumber(s['deal_price'] or s['current_price'] or 0)

redis.call('HSET', state_key,
  'status', 'ended',
  'ended_at_unix_ms', now_ms,
  'end_reason', 'time_expired',
  'deal_price', deal)
redis.call('ZREM', ending_key, item_id)

return {1, leader, deal, now_ms, 'time_expired'}
`

var settleAuctionScript = redis.NewScript(settleAuctionLuaScript)

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
		"room_id", state.RoomID,
		"current_price", state.CurrentPrice,
		"deal_price", dealPrice,
		"leader_user_id", state.LeaderUserID,
		"end_time_unix", state.EndTime.Unix(),
		"end_time_unix_ms", endUnixMS,
		"ended_at_unix_ms", state.EndedAtUnixMS,
		"bid_increment", state.BidIncrement,
		"price_cap", state.PriceCap,
		"deposit_amount", state.DepositAmount,
		"bid_count", state.BidCount,
		"participant_count", state.ParticipantCount,
		"is_extended", boolToStr(state.IsExtended),
		"extend_count", state.ExtendCount,
		"total_extended_sec", state.TotalExtendedSec,
		"extend_trigger_sec", state.ExtendTriggerSec,
		"auto_extend_sec", state.AutoExtendSec,
		"max_extend_count", state.MaxExtendCount,
		"max_total_extend_sec", state.MaxTotalExtendSec,
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
		Status:            vals["status"],
		RoomID:            vals["room_id"],
		CurrentPrice:      parseInt64(vals["current_price"]),
		DealPrice:         dealPrice,
		LeaderUserID:      vals["leader_user_id"],
		EndTime:           time.UnixMilli(endMS),
		EndTimeUnixMS:     endMS,
		EndedAtUnixMS:     parseInt64(vals["ended_at_unix_ms"]),
		BidIncrement:      parseInt64(vals["bid_increment"]),
		PriceCap:          parseInt64(vals["price_cap"]),
		DepositAmount:     parseInt64(vals["deposit_amount"]),
		BidCount:          parseInt(vals["bid_count"]),
		ParticipantCount:  parseInt(vals["participant_count"]),
		IsExtended:        vals["is_extended"] == "1",
		ExtendCount:       parseInt(vals["extend_count"]),
		TotalExtendedSec:  parseInt(vals["total_extended_sec"]),
		ExtendTriggerSec:  parseInt(vals["extend_trigger_sec"]),
		AutoExtendSec:     parseInt(vals["auto_extend_sec"]),
		MaxExtendCount:    parseInt(vals["max_extend_count"]),
		MaxTotalExtendSec: parseInt(vals["max_total_extend_sec"]),
		EndReason:         vals["end_reason"],
	}, true, nil
}

func (c *RedisCache) GetAuctionHotConfig(ctx context.Context, itemID string) (*AuctionHotConfig, bool, error) {
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
	return &AuctionHotConfig{
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

func (c *RedisCache) DeleteAuctionState(ctx context.Context, itemID string) error {
	return c.client.Del(ctx, itemStateKey(itemID)).Err()
}

func (c *RedisCache) ExpireAuctionState(ctx context.Context, itemID string, ttl time.Duration) error {
	return c.client.Expire(ctx, itemStateKey(itemID), ttl).Err()
}

func (c *RedisCache) ScheduleAuctionEnd(ctx context.Context, itemID string, endUnixMS int64) error {
	return c.client.ZAdd(ctx, endingKey(), redis.Z{Score: float64(endUnixMS), Member: itemID}).Err()
}

func (c *RedisCache) UnscheduleAuctionEnd(ctx context.Context, itemID string) error {
	return c.client.ZRem(ctx, endingKey(), itemID).Err()
}

func (c *RedisCache) ListDueAuctionEnds(ctx context.Context, nowUnixMS int64, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	return c.client.ZRangeByScore(ctx, endingKey(), &redis.ZRangeBy{
		Min:   "-inf",
		Max:   strconv.FormatInt(nowUnixMS, 10),
		Count: int64(limit),
	}).Result()
}

func (c *RedisCache) ListActiveAuctionEnds(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	return c.client.ZRange(ctx, endingKey(), 0, int64(limit-1)).Result()
}

func (c *RedisCache) SettleAuctionLua(ctx context.Context, itemID string, nowUnixMS int64) (*SettlementResult, bool, error) {
	res, err := settleAuctionScript.Run(ctx, c.client, []string{itemStateKey(itemID), endingKey()},
		strconv.FormatInt(nowUnixMS, 10),
		itemID,
	).Slice()
	if err != nil {
		return nil, false, err
	}
	if len(res) == 0 {
		return nil, false, fmt.Errorf("settlement lua result is empty")
	}
	if luaAnyToInt64(res[0]) == 0 {
		return nil, false, nil
	}
	if len(res) < 5 {
		return nil, false, fmt.Errorf("settlement lua result length unexpected: %d", len(res))
	}
	return &SettlementResult{
		ItemID:        itemID,
		LeaderUserID:  luaAnyToString(res[1]),
		DealPrice:     luaAnyToInt64(res[2]),
		EndedAtUnixMS: luaAnyToInt64(res[3]),
		EndReason:     luaAnyToString(res[4]),
	}, true, nil
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

func (c *RedisCache) GetRoomCurrentItem(ctx context.Context, roomID string) (string, bool, error) {
	itemID, err := c.client.HGet(ctx, roomStateKey(roomID), "current_item_id").Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if itemID == "" {
		return "", false, nil
	}
	return itemID, true, nil
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

func luaAnyToInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		return parseInt64(n)
	case []byte:
		return parseInt64(string(n))
	default:
		return parseInt64(fmt.Sprint(v))
	}
}

func luaAnyToString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprint(v)
	}
}
