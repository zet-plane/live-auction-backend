package cache

import (
	"context"
	"errors"
	"strconv"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
)

var errActiveRedisUnavailable = errors.New("active redis unavailable")

type activeRedisProvider interface {
	ActiveRedis() (*redis.Client, availability.Snapshot, bool)
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

func (c *RedisCache) MarkPaidDeposit(ctx context.Context, itemID, userID string, amount int64) error {
	if c == nil || c.client == nil {
		return errActiveRedisUnavailable
	}
	return c.client.Set(ctx, paidDepositKey(itemID, userID), strconv.FormatInt(amount, 10), 0).Err()
}

func (c *RedisCache) HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error) {
	if requiredAmount <= 0 {
		return true, nil
	}
	if c == nil || c.client == nil {
		return false, errActiveRedisUnavailable
	}
	raw, err := c.client.Get(ctx, paidDepositKey(itemID, userID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	amount, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false, err
	}
	return amount >= requiredAmount, nil
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

func (c *ActiveRedisCache) MarkPaidDeposit(ctx context.Context, itemID, userID string, amount int64) error {
	rc, err := c.current()
	if err != nil {
		return err
	}
	return rc.MarkPaidDeposit(ctx, itemID, userID, amount)
}

func (c *ActiveRedisCache) HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error) {
	rc, err := c.current()
	if err != nil {
		return false, err
	}
	return rc.HasPaidDeposit(ctx, itemID, userID, requiredAmount)
}

func paidDepositKey(itemID, userID string) string {
	return "auction:deposit:" + itemID + ":" + userID + ":paid"
}
