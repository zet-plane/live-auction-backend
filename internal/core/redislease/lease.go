package redislease

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Setter interface {
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
}

type RedisSetter struct {
	Client *redis.Client
}

func (s RedisSetter) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return s.Client.SetNX(ctx, key, value, ttl).Result()
}

type Store struct {
	Setter Setter
}

func (s Store) Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if s.Setter == nil {
		return false, nil
	}
	return s.Setter.SetNX(ctx, key, value, ttl)
}
