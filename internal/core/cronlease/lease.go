package cronlease

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

type Store interface {
	Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
}

type RedisStore struct {
	Client *redis.Client
}

func (s RedisStore) Acquire(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return s.Client.SetNX(ctx, key, value, ttl).Result()
}

func Wrap(name, podID string, ttl time.Duration, store Store, fn func(context.Context)) func(context.Context) {
	return func(ctx context.Context) {
		if store == nil {
			record(ctx, name, "lease_unconfigured")
			return
		}
		ok, err := store.Acquire(ctx, "cron:lease:"+name, podID, ttl)
		if err != nil {
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
