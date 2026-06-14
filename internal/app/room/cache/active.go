package cache

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
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

func (c *ActiveRedisCache) InitRoomState(ctx context.Context, roomID string, state RoomState) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.InitRoomState(ctx, roomID, state)
}

func (c *ActiveRedisCache) GetRoomState(ctx context.Context, roomID string) (*RoomState, bool, error) {
	rc, err := c.current()
	if err != nil {
		return nil, false, err
	}
	return rc.GetRoomState(ctx, roomID)
}

func (c *ActiveRedisCache) UpdateRoomStatus(ctx context.Context, roomID, status string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.UpdateRoomStatus(ctx, roomID, status)
}

func (c *ActiveRedisCache) ClearRoomCurrentItem(ctx context.Context, roomID string) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.ClearRoomCurrentItem(ctx, roomID)
}

func (c *ActiveRedisCache) GetItemQueue(ctx context.Context, roomID string) ([]string, error) {
	rc, err := c.current()
	if err != nil {
		return []string{}, err
	}
	return rc.GetItemQueue(ctx, roomID)
}
