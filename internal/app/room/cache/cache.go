package cache

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const (
	keyRoomState = "auction:room:%s:state"
	keyItemQueue = "auction:room:%s:item_queue"
)

type RoomState struct {
	MerchantID    string
	Status        string
	CurrentItemID string
	OnlineCount   int
}

type Cache interface {
	InitRoomState(ctx context.Context, roomID string, state RoomState) error
	GetRoomState(ctx context.Context, roomID string) (*RoomState, bool, error)
	UpdateRoomStatus(ctx context.Context, roomID, status string) error
	GetItemQueue(ctx context.Context, roomID string) ([]string, error)
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

func (c *RedisCache) InitRoomState(ctx context.Context, roomID string, state RoomState) error {
	key := fmt.Sprintf(keyRoomState, roomID)
	return c.client.HSet(ctx, key,
		"merchant_id", state.MerchantID,
		"status", state.Status,
		"current_item_id", state.CurrentItemID,
		"online_count", "0",
	).Err()
}

func (c *RedisCache) GetRoomState(ctx context.Context, roomID string) (*RoomState, bool, error) {
	key := fmt.Sprintf(keyRoomState, roomID)
	result, err := c.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, false, err
	}
	if len(result) == 0 {
		return nil, false, nil
	}
	onlineCount, _ := strconv.Atoi(result["online_count"])
	return &RoomState{
		MerchantID:    result["merchant_id"],
		Status:        result["status"],
		CurrentItemID: result["current_item_id"],
		OnlineCount:   onlineCount,
	}, true, nil
}

func (c *RedisCache) UpdateRoomStatus(ctx context.Context, roomID, status string) error {
	key := fmt.Sprintf(keyRoomState, roomID)
	return c.client.HSet(ctx, key, "status", status).Err()
}

func (c *RedisCache) GetItemQueue(ctx context.Context, roomID string) ([]string, error) {
	key := fmt.Sprintf(keyItemQueue, roomID)
	result, err := c.client.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return []string{}, err
	}
	return result, nil
}
