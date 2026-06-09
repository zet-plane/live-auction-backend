package availability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRuntimeChoosesCloudWhenHealthy(t *testing.T) {
	now := time.UnixMilli(1710000000000)
	rt := NewRuntime(nil, nil, nil, Options{
		Now: func() time.Time { return now },
		Probe: Probe{
			CloudRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
			LocalRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
			MySQL:      func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
		},
	})

	rt.Refresh(context.Background())

	s := rt.Snapshot()
	if !s.Valid || s.Mode != ModeNormalCloud || s.ActiveRedis != RedisCloud {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestRuntimeFailsOverToLocalAfterThreshold(t *testing.T) {
	now := time.UnixMilli(1710000000000)
	rt := NewRuntime(nil, nil, nil, Options{
		Now:           func() time.Time { return now },
		FailoverAfter: 3 * time.Second,
		Probe: Probe{
			CloudRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: false, Error: "cloud down"} },
			LocalRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
			MySQL:      func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
		},
	})

	rt.Refresh(context.Background())
	if got := rt.Snapshot().Mode; got != ModeAuctionProtected {
		t.Fatalf("first failed probe mode = %s, want protected during threshold", got)
	}

	now = now.Add(4 * time.Second)
	rt.Refresh(context.Background())
	s := rt.Snapshot()
	if s.Mode != ModeLocalRedisSwitching || s.ActiveRedis != RedisLocal {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestRuntimeKeepsLocalStickyWhenCloudRecovers(t *testing.T) {
	now := time.UnixMilli(1710000000000)
	cloudHealthy := false
	rt := NewRuntime(nil, nil, nil, Options{
		Now:           func() time.Time { return now },
		FailoverAfter: time.Second,
		Probe: Probe{
			CloudRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: cloudHealthy} },
			LocalRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
			MySQL:      func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
		},
	})

	rt.Refresh(context.Background())
	now = now.Add(2 * time.Second)
	rt.Refresh(context.Background())
	cloudHealthy = true
	now = now.Add(2 * time.Second)
	rt.Refresh(context.Background())

	s := rt.Snapshot()
	if s.Mode != ModeLocalRedisActive || s.ActiveRedis != RedisLocal {
		t.Fatalf("snapshot = %+v, want sticky local active", s)
	}
}

func TestRuntimeBuffersMySQLWithinWindowAndProtectsAfterExpiry(t *testing.T) {
	now := time.UnixMilli(1710000000000)
	rt := NewRuntime(nil, nil, nil, Options{
		Now:                  func() time.Time { return now },
		MySQLBufferingWindow: 10 * time.Second,
		Probe: Probe{
			CloudRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
			LocalRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
			MySQL:      func(context.Context) DependencyStatus { return DependencyStatus{Healthy: false, Error: "mysql down"} },
		},
	})

	rt.Refresh(context.Background())
	if got := rt.Snapshot().Mode; got != ModeMySQLBuffering {
		t.Fatalf("mode = %s, want mysql_buffering", got)
	}

	now = now.Add(11 * time.Second)
	rt.Refresh(context.Background())
	s := rt.Snapshot()
	if s.Mode != ModeAuctionProtected {
		t.Fatalf("mode = %s, want auction_protected", s.Mode)
	}
	if s.ActiveRedis != RedisNone {
		t.Fatalf("active redis = %s, want none", s.ActiveRedis)
	}
}

func TestRuntimeActiveRedisReturnsConfiguredClient(t *testing.T) {
	local := redis.NewClient(&redis.Options{Addr: "local:6379"})
	rt := NewRuntime(nil, local, nil, Options{
		Probe: Probe{
			CloudRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: false, Error: "down"} },
			LocalRedis: func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
			MySQL:      func(context.Context) DependencyStatus { return DependencyStatus{Healthy: true} },
		},
		FailoverAfter: time.Nanosecond,
	})

	rt.Refresh(context.Background())
	client, snapshot, ok := rt.ActiveRedis()
	if !ok || snapshot.ActiveRedis != RedisLocal || client != local {
		t.Fatalf("ActiveRedis() = (%v, %+v, %v), want local", client, snapshot, ok)
	}
}

func TestRuntimeUsesProbeErrorsAsDependencyStatus(t *testing.T) {
	errDown := errors.New("network refused")
	status := statusFromError(time.Millisecond, errDown)
	if status.Healthy || status.Error != "network refused" || status.Latency != time.Millisecond {
		t.Fatalf("status = %+v", status)
	}
}
