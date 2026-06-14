package service

import (
	"context"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
)

type cloudFailbackRuntime interface {
	Snapshot() availability.Snapshot
	MarkCloudFailbackReady()
	Refresh(context.Context)
}

type BidLogDrainChecker interface {
	Drained(ctx context.Context) (bool, error)
}

func (s *Service) PrewarmCloudRedisForFailback(ctx context.Context, cloudCache itemcache.Cache, bidLogDrain BidLogDrainChecker, rt cloudFailbackRuntime) error {
	if s == nil || cloudCache == nil || bidLogDrain == nil || rt == nil {
		return nil
	}
	snapshot := rt.Snapshot()
	if snapshot.ActiveRedis != availability.RedisLocal ||
		snapshot.Mode != availability.ModeLocalRedisActive ||
		!snapshot.CloudRedis.Healthy ||
		!snapshot.LocalRedis.Healthy ||
		snapshot.MySQLState != availability.MySQLHealthy {
		return nil
	}
	drained, err := bidLogDrain.Drained(ctx)
	if err != nil {
		return err
	}
	if !drained {
		return nil
	}

	worker := newAvailabilityRebuildWorker(s.store, cloudCache, availabilityRebuildConfig{BatchSize: 100, Policy: s.policy})
	results := worker.rebuildActiveItems(ctx, 0)
	for _, result := range results {
		if result != rebuildReady {
			return ErrAvailabilityUnavailable
		}
	}
	drained, err = bidLogDrain.Drained(ctx)
	if err != nil {
		return err
	}
	if !drained {
		return nil
	}
	rt.MarkCloudFailbackReady()
	rt.Refresh(ctx)
	return nil
}
