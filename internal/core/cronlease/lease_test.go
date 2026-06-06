package cronlease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/core/observability"
)

type fakeStore struct {
	acquired bool
	err      error
}

func (s fakeStore) Acquire(context.Context, string, string, time.Duration) (bool, error) {
	return s.acquired, s.err
}

func TestWrapRunsFunctionWhenLeaseAcquired(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{acquired: true}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if !called {
		t.Fatal("expected wrapped function to run")
	}
}

func TestWrapSkipsFunctionWhenLeaseNotAcquired(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{acquired: false}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if called {
		t.Fatal("expected wrapped function to be skipped")
	}
}

func TestWrapSkipsFunctionWhenAcquireErrors(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, fakeStore{err: errors.New("redis down")}, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if called {
		t.Fatal("expected wrapped function to be skipped after acquire error")
	}
}

func TestWrapSkipsFunctionWhenStoreUnconfigured(t *testing.T) {
	called := false
	fn := Wrap("job_a", "pod_a", time.Second, nil, func(context.Context) {
		called = true
	})

	fn(context.Background())

	if called {
		t.Fatal("expected wrapped function to be skipped without lease store")
	}
}

func TestRedisStoreNilClientReturnsUnconfigured(t *testing.T) {
	ok, err := RedisStore{}.Acquire(context.Background(), "key", "owner", time.Second)
	if !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("err = %v, want ErrUnconfigured", err)
	}
	if ok {
		t.Fatal("expected ok=false")
	}
}

func TestWrapCronRecordsBaseMetricOnlyWhenLeaseAcquired(t *testing.T) {
	rec := &cronCaptureRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

	WrapCron("job_a", "pod_a", time.Second, fakeStore{acquired: false}, func(context.Context) {
		t.Fatal("skipped lease should not run job")
	})()

	if rec.count("job_a") != 0 {
		t.Fatalf("base cron metric recorded for skipped lease")
	}
	if rec.count("job_a.lease") != 1 {
		t.Fatalf("lease metric count = %d, want 1", rec.count("job_a.lease"))
	}

	WrapCron("job_a", "pod_a", time.Second, fakeStore{acquired: true}, func(context.Context) {})()

	if rec.count("job_a") != 1 {
		t.Fatalf("base cron metric count = %d, want 1", rec.count("job_a"))
	}
	if rec.count("job_a.lease") != 2 {
		t.Fatalf("lease metric count = %d, want 2", rec.count("job_a.lease"))
	}
}

type cronCaptureRecorder struct {
	observability.NoopRecorder
	metrics []observability.CronMetric
}

func (r *cronCaptureRecorder) Cron(_ context.Context, m observability.CronMetric) {
	r.metrics = append(r.metrics, m)
}

func (r *cronCaptureRecorder) count(name string) int {
	var n int
	for _, metric := range r.metrics {
		if metric.Name == name {
			n++
		}
	}
	return n
}
