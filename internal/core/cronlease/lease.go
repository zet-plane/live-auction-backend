package cronlease

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

type Store interface {
	Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
}

type RedisStore struct {
	Client *redis.Client
}

type activeRedisProvider interface {
	ActiveRedis() (*redis.Client, availability.Snapshot, bool)
}

type ActiveRedisStore struct {
	Provider activeRedisProvider
}

var ErrUnconfigured = errors.New("cron lease store unconfigured")

func NewRedisStore(client *redis.Client) Store {
	if client == nil {
		return nil
	}
	return RedisStore{Client: client}
}

func NewActiveRedisStore(provider activeRedisProvider) Store {
	if provider == nil {
		return nil
	}
	return ActiveRedisStore{Provider: provider}
}

func (s RedisStore) Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if s.Client == nil {
		return false, ErrUnconfigured
	}
	return s.Client.SetNX(ctx, key, value, ttl).Result()
}

func (s ActiveRedisStore) Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if s.Provider == nil {
		return false, ErrUnconfigured
	}
	client, _, ok := s.Provider.ActiveRedis()
	if !ok || client == nil {
		return false, ErrUnconfigured
	}
	return client.SetNX(ctx, key, value, ttl).Result()
}

func WrapCron(name, podID string, ttl time.Duration, store Store, fn func(context.Context)) func() {
	return func() {
		Wrap(name, podID, ttl, store, func(context.Context) {
			observability.WrapCron(name, fn)()
		})(context.Background())
	}
}

func Wrap(name, podID string, ttl time.Duration, store Store, fn func(context.Context)) func(context.Context) {
	return func(ctx context.Context) {
		if store == nil {
			record(ctx, name, "lease_unconfigured")
			return
		}
		ok, err := store.Acquire(ctx, "cron:lease:"+name, podID, ttl)
		if err != nil {
			if errors.Is(err, ErrUnconfigured) {
				record(ctx, name, "lease_unconfigured")
				return
			}
			logx.Warnw("cron lease acquire failed", "cron_name", name, "lease_owner", podID, "err", err)
			record(ctx, name, "lease_error")
			return
		}
		if !ok {
			record(ctx, name, "lease_skipped")
			return
		}
		record(ctx, name, "lease_acquired")
		fn(ctx)
	}
}

func record(ctx context.Context, name, result string) {
	observability.DefaultRecorder().Cron(ctx, observability.CronMetric{
		Name:   name + ".lease",
		Result: result,
	})
}
