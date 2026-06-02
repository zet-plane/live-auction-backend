package observability

import (
	"context"
	"database/sql"
	"runtime"
	"time"

	"go.opentelemetry.io/otel/metric"
)

type DBStats struct {
	MaxOpen         int
	OpenConnections int
	InUse           int
	Idle            int
	WaitCount       int64
	WaitDuration    time.Duration
}

type DBStatsProvider interface {
	Stats() DBStats
}

type SQLDBStatsProvider struct {
	DB *sql.DB
}

func (p SQLDBStatsProvider) Stats() DBStats {
	if p.DB == nil {
		return DBStats{}
	}
	stats := p.DB.Stats()
	return DBStats{
		MaxOpen:         stats.MaxOpenConnections,
		OpenConnections: stats.OpenConnections,
		InUse:           stats.InUse,
		Idle:            stats.Idle,
		WaitCount:       stats.WaitCount,
		WaitDuration:    stats.WaitDuration,
	}
}

func RegisterRuntimeMetrics(provider metric.MeterProvider, db DBStatsProvider) (func() error, error) {
	if provider == nil {
		return func() error { return nil }, nil
	}
	meter := provider.Meter("github.com/zet-plane/live-auction-backend/runtime")

	goroutines, err := meter.Int64ObservableGauge("process.runtime.go.goroutines")
	if err != nil {
		return nil, err
	}
	heapAlloc, err := meter.Int64ObservableGauge("process.runtime.go.memory.heap_alloc")
	if err != nil {
		return nil, err
	}
	heapObjects, err := meter.Int64ObservableGauge("process.runtime.go.memory.heap_objects")
	if err != nil {
		return nil, err
	}
	gcCount, err := meter.Int64ObservableGauge("process.runtime.go.gc.count")
	if err != nil {
		return nil, err
	}
	gcPauseTotal, err := meter.Float64ObservableGauge("process.runtime.go.gc.pause_total")
	if err != nil {
		return nil, err
	}
	dbOpen, err := meter.Int64ObservableGauge("db.client.connections.open")
	if err != nil {
		return nil, err
	}
	dbIdle, err := meter.Int64ObservableGauge("db.client.connections.idle")
	if err != nil {
		return nil, err
	}
	dbInUse, err := meter.Int64ObservableGauge("db.client.connections.in_use")
	if err != nil {
		return nil, err
	}
	dbWaitCount, err := meter.Int64ObservableGauge("db.client.connections.wait_count")
	if err != nil {
		return nil, err
	}
	dbWaitDuration, err := meter.Float64ObservableGauge("db.client.connections.wait_duration")
	if err != nil {
		return nil, err
	}
	dbMaxOpen, err := meter.Int64ObservableGauge("db.client.connections.max_open")
	if err != nil {
		return nil, err
	}

	registration, err := meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		observer.ObserveInt64(goroutines, int64(runtime.NumGoroutine()))
		observer.ObserveInt64(heapAlloc, int64(mem.HeapAlloc))
		observer.ObserveInt64(heapObjects, int64(mem.HeapObjects))
		observer.ObserveInt64(gcCount, int64(mem.NumGC))
		observer.ObserveFloat64(gcPauseTotal, float64(mem.PauseTotalNs)/float64(time.Second))

		if db != nil {
			stats := db.Stats()
			observer.ObserveInt64(dbOpen, int64(stats.OpenConnections))
			observer.ObserveInt64(dbIdle, int64(stats.Idle))
			observer.ObserveInt64(dbInUse, int64(stats.InUse))
			observer.ObserveInt64(dbWaitCount, stats.WaitCount)
			observer.ObserveFloat64(dbWaitDuration, stats.WaitDuration.Seconds())
			observer.ObserveInt64(dbMaxOpen, int64(stats.MaxOpen))
		}
		return nil
	}, goroutines, heapAlloc, heapObjects, gcCount, gcPauseTotal, dbOpen, dbIdle, dbInUse, dbWaitCount, dbWaitDuration, dbMaxOpen)
	if err != nil {
		return nil, err
	}
	return registration.Unregister, nil
}
