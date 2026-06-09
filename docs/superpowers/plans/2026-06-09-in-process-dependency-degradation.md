# In-Process Dependency Degradation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the file-backed availability control plane with an in-process dependency degradation runtime that supports cloud Redis to local Redis failover, MySQL short-window buffering, and protective rejection after the buffering window expires.

**Architecture:** Keep the existing `Availability.ActiveRedis()` integration contract so item cache, WebSocket bus, ticket authority, presence, and cron users can continue to route through one active Redis runtime. Replace watcher/file state with local probe state, make local Redis sticky after cloud Redis failover, and use MySQL bid logs as the rebuild source while accepting rollback to the durable point.

**Tech Stack:** Go, go-redis v9, GORM, Flamego, robfig/cron, existing fake-store/fake-cache service tests.

---

## File Structure

- Modify `internal/core/availability/state.go`: redefine modes, snapshot, dependency health, and snapshot helpers for in-process probes.
- Modify `internal/core/availability/authority.go`: replace `RedisSelector`/watcher-backed runtime with probe-backed `Runtime`, `Options`, `Probe`, and `ActiveRedis()`.
- Delete or stop using `internal/core/availability/watcher.go`, `internal/core/availability/store.go`, and their tests after replacing coverage with runtime tests.
- Modify `config/vars.go` and `config/config.go`: replace control-plane config fields with probe/failover/buffering durations.
- Modify `config.yaml.example` and deployment config maps: remove `state_path`/`stale_threshold`; add probe settings.
- Modify `cmd/server/server.go`: create cloud Redis and local Redis clients, create availability runtime, run probe loop on engine context, and remove watcher setup.
- Modify `internal/core/kernel/kernel.go`: keep `CloudRedis`, `LocalRedis`, and `Availability`; `Cache` remains cloud Redis for compatibility and cron lease defaults until callers are migrated.
- Modify `internal/app/base/handler/health.go`: report runtime probe data instead of control-plane file data.
- Modify `internal/app/item/init.go`: continue using `cache.NewActiveRedisCache(engine.Availability)` and configure service with the runtime.
- Modify `internal/app/item/service/service.go` and `internal/app/item/service/bid_service.go`: update availability checks for Redis down, MySQL buffering window, and local Redis rebuild semantics.
- Modify `internal/app/ws/*` only if the runtime interface changes; the preferred plan keeps `ActiveRedis()` compatible so most WS code remains unchanged.
- Add/update tests in `internal/core/availability`, `internal/app/base/handler`, and `internal/app/item/service`.

## Task 1: Redefine Availability State And Runtime Tests

**Files:**
- Modify: `internal/core/availability/state.go`
- Modify: `internal/core/availability/authority.go`
- Replace test: `internal/core/availability/authority_test.go`
- Replace test: `internal/core/availability/state_test.go`

- [ ] **Step 1: Write failing state tests**

Replace `internal/core/availability/state_test.go` with:

```go
package availability

import (
	"testing"
	"time"
)

func TestSnapshotHelpers(t *testing.T) {
	now := time.UnixMilli(1710000000000)
	s := Snapshot{
		Valid:       true,
		Mode:        ModeLocalRedisActive,
		ActiveRedis: RedisLocal,
		CloudRedis:  DependencyStatus{Healthy: false, Error: "timeout"},
		LocalRedis:  DependencyStatus{Healthy: true, Latency: 2 * time.Millisecond},
		MySQL:       DependencyStatus{Healthy: true, Latency: time.Millisecond},
		UpdatedAt:   now,
	}

	if !s.RedisWritable() {
		t.Fatal("expected redis writable")
	}
	if s.MySQLBufferingExpired(now.Add(11*time.Second), 10*time.Second) {
		t.Fatal("local redis active without buffering start must not expire")
	}
}

func TestMySQLBufferingExpired(t *testing.T) {
	start := time.UnixMilli(1710000000000)
	s := Snapshot{
		Valid:                      true,
		Mode:                       ModeMySQLBuffering,
		ActiveRedis:                RedisCloud,
		MySQLBufferingStartedAt:    start,
		MySQLBufferingStartedUnixMS: start.UnixMilli(),
	}

	if s.MySQLBufferingExpired(start.Add(9*time.Second), 10*time.Second) {
		t.Fatal("buffering expired too early")
	}
	if !s.MySQLBufferingExpired(start.Add(11*time.Second), 10*time.Second) {
		t.Fatal("buffering should expire after window")
	}
}

func TestProtectedSnapshotIsNotWritable(t *testing.T) {
	s := Snapshot{Valid: true, Mode: ModeAuctionProtected, ActiveRedis: RedisNone}
	if s.RedisWritable() {
		t.Fatal("protected snapshot must not be writable")
	}
}
```

- [ ] **Step 2: Run state tests and verify failure**

Run:

```bash
rtk go test ./internal/core/availability -run 'TestSnapshotHelpers|TestMySQLBufferingExpired|TestProtectedSnapshotIsNotWritable'
```

Expected: FAIL because `DependencyStatus`, new `Snapshot` fields, `RedisNone`, `RedisWritable`, and the new `MySQLBufferingExpired` semantics do not exist yet.

- [ ] **Step 3: Implement new state model**

Replace `internal/core/availability/state.go` with:

```go
package availability

import "time"

type Mode string
type RedisAuthority string
type MySQLStatus string

const (
	ModeNormalCloud         Mode = "normal_cloud"
	ModeLocalRedisSwitching Mode = "local_redis_switching"
	ModeLocalRedisActive    Mode = "local_redis_active"
	ModeMySQLBuffering      Mode = "mysql_buffering"
	ModeAuctionProtected    Mode = "auction_protected"

	RedisCloud RedisAuthority = "cloud"
	RedisLocal RedisAuthority = "local"
	RedisNone  RedisAuthority = "none"

	MySQLHealthy   MySQLStatus = "healthy"
	MySQLDown      MySQLStatus = "down"
	MySQLBuffering MySQLStatus = "buffering"
)

type DependencyStatus struct {
	Healthy bool
	Latency time.Duration
	Error   string
}

type Snapshot struct {
	Valid                      bool
	Mode                       Mode
	ActiveRedis                RedisAuthority
	CloudRedis                 DependencyStatus
	LocalRedis                 DependencyStatus
	MySQL                      DependencyStatus
	MySQLState                 MySQLStatus
	MySQLBufferingStartedAt    time.Time
	MySQLBufferingStartedUnixMS int64
	UpdatedAt                  time.Time
	Reason                     string
	Error                      string
}

func (s Snapshot) RedisWritable() bool {
	return s.Valid && (s.ActiveRedis == RedisCloud || s.ActiveRedis == RedisLocal) && s.Mode != ModeAuctionProtected
}

func (s Snapshot) MySQLBufferingExpired(now time.Time, window time.Duration) bool {
	if s.Mode != ModeMySQLBuffering || s.MySQLBufferingStartedAt.IsZero() || window <= 0 {
		return false
	}
	return now.Sub(s.MySQLBufferingStartedAt) > window
}
```

- [ ] **Step 4: Write failing runtime tests**

Replace `internal/core/availability/authority_test.go` with:

```go
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
	if got := rt.Snapshot().Mode; got != ModeAuctionProtected {
		t.Fatalf("mode = %s, want auction_protected", got)
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
```

- [ ] **Step 5: Implement probe-backed runtime**

Replace `internal/core/availability/authority.go` with:

```go
package availability

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ProbeFunc func(context.Context) DependencyStatus

type Probe struct {
	CloudRedis ProbeFunc
	LocalRedis ProbeFunc
	MySQL      ProbeFunc
}

type Options struct {
	Now                  func() time.Time
	ProbeInterval        time.Duration
	FailoverAfter        time.Duration
	MySQLBufferingWindow time.Duration
	Probe                Probe
}

type Runtime struct {
	cloudRedis *redis.Client
	localRedis *redis.Client
	db         *gorm.DB
	opts       Options
	v          atomic.Value
	mu         sync.Mutex

	cloudDownSince          time.Time
	mysqlDownSince          time.Time
	localAuthorityActivated bool
}

func NewRuntime(cloudRedis, localRedis *redis.Client, db *gorm.DB, opts Options) *Runtime {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ProbeInterval <= 0 {
		opts.ProbeInterval = time.Second
	}
	if opts.FailoverAfter <= 0 {
		opts.FailoverAfter = 3 * time.Second
	}
	if opts.MySQLBufferingWindow <= 0 {
		opts.MySQLBufferingWindow = 10 * time.Second
	}
	rt := &Runtime{cloudRedis: cloudRedis, localRedis: localRedis, db: db, opts: opts}
	rt.v.Store(Snapshot{Valid: false, Mode: ModeAuctionProtected, ActiveRedis: RedisNone, Reason: "not_probed", Error: "not probed"})
	return rt
}

func (r *Runtime) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{Valid: false, Mode: ModeAuctionProtected, ActiveRedis: RedisNone, Reason: "runtime_nil", Error: "availability runtime unconfigured"}
	}
	return r.v.Load().(Snapshot)
}

func (r *Runtime) ActiveRedis() (*redis.Client, Snapshot, bool) {
	snapshot := r.Snapshot()
	if !snapshot.RedisWritable() {
		return nil, snapshot, false
	}
	switch snapshot.ActiveRedis {
	case RedisCloud:
		return r.cloudRedis, snapshot, r.cloudRedis != nil
	case RedisLocal:
		return r.localRedis, snapshot, r.localRedis != nil
	default:
		return nil, snapshot, false
	}
}

func (r *Runtime) Run(ctx context.Context) {
	r.Refresh(ctx)
	ticker := time.NewTicker(r.opts.ProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Refresh(ctx)
		}
	}
}

func (r *Runtime) Refresh(ctx context.Context) {
	if r == nil {
		return
	}
	now := r.opts.Now()
	cloud := r.probeCloud(ctx)
	local := r.probeLocal(ctx)
	mysql := r.probeMySQL(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()

	if cloud.Healthy {
		r.cloudDownSince = time.Time{}
	} else if r.cloudDownSince.IsZero() {
		r.cloudDownSince = now
	}

	if mysql.Healthy {
		r.mysqlDownSince = time.Time{}
	} else if r.mysqlDownSince.IsZero() {
		r.mysqlDownSince = now
	}

	mode := ModeAuctionProtected
	active := RedisNone
	mysqlState := MySQLHealthy
	reason := "protected"

	if !mysql.Healthy {
		mysqlState = MySQLBuffering
	}

	if r.localAuthorityActivated {
		if local.Healthy {
			mode = ModeLocalRedisActive
			active = RedisLocal
			reason = "local_sticky"
		} else {
			mode = ModeAuctionProtected
			active = RedisNone
			reason = "local_redis_down"
		}
	} else if cloud.Healthy {
		mode = ModeNormalCloud
		active = RedisCloud
		reason = "cloud_redis_ok"
	} else if local.Healthy && !r.cloudDownSince.IsZero() && now.Sub(r.cloudDownSince) >= r.opts.FailoverAfter {
		mode = ModeLocalRedisSwitching
		active = RedisLocal
		reason = "cloud_redis_failover"
		r.localAuthorityActivated = true
	} else {
		mode = ModeAuctionProtected
		active = RedisNone
		reason = "cloud_redis_failover_threshold"
	}

	var mysqlStarted time.Time
	if !mysql.Healthy {
		mysqlStarted = r.mysqlDownSince
		if !mysqlStarted.IsZero() && now.Sub(mysqlStarted) <= r.opts.MySQLBufferingWindow && active != RedisNone {
			mode = ModeMySQLBuffering
			mysqlState = MySQLBuffering
			reason = "mysql_buffering"
		} else {
			mode = ModeAuctionProtected
			reason = "mysql_buffering_expired"
		}
	}

	r.v.Store(Snapshot{
		Valid:                      true,
		Mode:                       mode,
		ActiveRedis:                active,
		CloudRedis:                 cloud,
		LocalRedis:                 local,
		MySQL:                      mysql,
		MySQLState:                 mysqlState,
		MySQLBufferingStartedAt:    mysqlStarted,
		MySQLBufferingStartedUnixMS: unixMilliOrZero(mysqlStarted),
		UpdatedAt:                  now,
		Reason:                     reason,
	})
}

func (r *Runtime) probeCloud(ctx context.Context) DependencyStatus {
	if r.opts.Probe.CloudRedis != nil {
		return r.opts.Probe.CloudRedis(ctx)
	}
	return probeRedis(ctx, r.cloudRedis)
}

func (r *Runtime) probeLocal(ctx context.Context) DependencyStatus {
	if r.opts.Probe.LocalRedis != nil {
		return r.opts.Probe.LocalRedis(ctx)
	}
	return probeRedis(ctx, r.localRedis)
}

func (r *Runtime) probeMySQL(ctx context.Context) DependencyStatus {
	if r.opts.Probe.MySQL != nil {
		return r.opts.Probe.MySQL(ctx)
	}
	return probeDB(ctx, r.db)
}

func probeRedis(ctx context.Context, client *redis.Client) DependencyStatus {
	if client == nil {
		return DependencyStatus{Healthy: false, Error: "not initialized"}
	}
	start := time.Now()
	err := client.Ping(ctx).Err()
	return statusFromError(time.Since(start), err)
}

func probeDB(ctx context.Context, db *gorm.DB) DependencyStatus {
	if db == nil {
		return DependencyStatus{Healthy: false, Error: "not initialized"}
	}
	sqlDB, err := db.DB()
	if err != nil {
		return DependencyStatus{Healthy: false, Error: err.Error()}
	}
	start := time.Now()
	err = sqlDB.PingContext(ctx)
	return statusFromError(time.Since(start), err)
}

func statusFromError(latency time.Duration, err error) DependencyStatus {
	if err != nil {
		return DependencyStatus{Healthy: false, Latency: latency, Error: err.Error()}
	}
	return DependencyStatus{Healthy: true, Latency: latency}
}

func unixMilliOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
```

- [ ] **Step 6: Run availability tests**

Run:

```bash
rtk go test ./internal/core/availability
```

Expected: FAIL only in old watcher/store tests if they still reference deleted concepts. New state/runtime tests should pass.

- [ ] **Step 7: Remove obsolete watcher/store files**

Delete:

```text
internal/core/availability/watcher.go
internal/core/availability/watcher_test.go
internal/core/availability/store.go
internal/core/availability/store_test.go
```

Use `apply_patch` delete hunks so the deletion does not require shell escalation.

- [ ] **Step 8: Run availability tests again**

Run:

```bash
rtk go test ./internal/core/availability
```

Expected: PASS.

## Task 2: Update Config And Server Wiring

**Files:**
- Modify: `config/vars.go`
- Modify: `config/config.go`
- Modify: `config.yaml.example`
- Modify: `cmd/server/server.go`

- [ ] **Step 1: Write failing config test**

Append to `config/config_test.go`:

```go
func TestAvailabilityDurationFallbacks(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.AvailabilityRedisProbeInterval(); got != time.Second {
		t.Fatalf("redis probe fallback = %v, want 1s", got)
	}
	if got := cfg.AvailabilityRedisFailoverThreshold(); got != 3*time.Second {
		t.Fatalf("redis failover fallback = %v, want 3s", got)
	}
	if got := cfg.AvailabilityMySQLProbeInterval(); got != time.Second {
		t.Fatalf("mysql probe fallback = %v, want 1s", got)
	}
	if got := cfg.MySQLBufferingWindow(); got != 10*time.Second {
		t.Fatalf("mysql buffering fallback = %v, want 10s", got)
	}
}
```

- [ ] **Step 2: Run config test and verify failure**

Run:

```bash
rtk go test ./config -run TestAvailabilityDurationFallbacks
```

Expected: FAIL because new helper methods do not exist.

- [ ] **Step 3: Update config structs**

In `config/vars.go`, replace `Availability` with:

```go
type Availability struct {
	RedisProbeInterval     string `yaml:"redis_probe_interval"      mapstructure:"redis_probe_interval"`
	RedisFailoverThreshold string `yaml:"redis_failover_threshold"  mapstructure:"redis_failover_threshold"`
	RedisRecoverThreshold  string `yaml:"redis_recover_threshold"   mapstructure:"redis_recover_threshold"`
	MySQLProbeInterval     string `yaml:"mysql_probe_interval"      mapstructure:"mysql_probe_interval"`
	MySQLBufferingWindow   string `yaml:"mysql_buffering_window"    mapstructure:"mysql_buffering_window"`
	LocalRedis             Redis  `yaml:"local_redis"               mapstructure:"local_redis"`
}
```

- [ ] **Step 4: Update config duration helpers**

In `config/config.go`, replace `AvailabilityStaleThreshold()` with:

```go
func (c *GlobalConfig) AvailabilityRedisProbeInterval() time.Duration {
	return parseDuration(c.Availability.RedisProbeInterval, time.Second)
}

func (c *GlobalConfig) AvailabilityRedisFailoverThreshold() time.Duration {
	return parseDuration(c.Availability.RedisFailoverThreshold, 3*time.Second)
}

func (c *GlobalConfig) AvailabilityRedisRecoverThreshold() time.Duration {
	return parseDuration(c.Availability.RedisRecoverThreshold, 30*time.Second)
}

func (c *GlobalConfig) AvailabilityMySQLProbeInterval() time.Duration {
	return parseDuration(c.Availability.MySQLProbeInterval, time.Second)
}
```

Keep the existing `MySQLBufferingWindow()` helper, but make sure it reads `c.Availability.MySQLBufferingWindow`.

- [ ] **Step 5: Run config tests**

Run:

```bash
rtk go test ./config
```

Expected: PASS.

- [ ] **Step 6: Update server wiring**

In `cmd/server/server.go`:

1. Keep creating `cloudRedis` from `cfg.Redis` with `DisablePing: true`.
2. Keep creating `localRedis` from `cfg.Availability.LocalRedis`, falling back to `cfg.Redis` only if no local Redis addr is configured.
3. Delete the `statePath`, `NewWatcher`, `watcher.Refresh`, `watcher.Run`, and `watcherRefreshInterval` code.
4. Create runtime with:

```go
availabilityRuntime := availability.NewRuntime(cloudRedis, localRedis, db, availability.Options{
	ProbeInterval:        cfg.AvailabilityRedisProbeInterval(),
	FailoverAfter:        cfg.AvailabilityRedisFailoverThreshold(),
	MySQLBufferingWindow: cfg.MySQLBufferingWindow(),
})
```

5. Pass `availabilityRuntime` to `buildEngine`.
6. After `engine` is built, start the probe loop with:

```go
go availabilityRuntime.Run(engine.Context)
```

7. Delete `watcherRefreshInterval`.

- [ ] **Step 7: Update config example**

In `config.yaml.example`, make the availability block exactly:

```yaml
availability:
  redis_probe_interval: 1s
  redis_failover_threshold: 3s
  redis_recover_threshold: 30s
  mysql_probe_interval: 1s
  mysql_buffering_window: 10s
  local_redis:
    addr: redis:6379
    password: ""
    db: 0
```

- [ ] **Step 8: Run server package tests/build**

Run:

```bash
rtk go test ./cmd/server ./config ./internal/core/availability
```

Expected: PASS.

## Task 3: Update Health Handler To Report Probe Runtime

**Files:**
- Modify: `internal/app/base/handler/health.go`
- Modify: `internal/app/base/handler/health_test.go`

- [ ] **Step 1: Write failing health tests**

Replace availability-specific tests in `internal/app/base/handler/health_test.go` with:

```go
func TestReadyzReportsLocalRedisActiveAsDegradedOK(t *testing.T) {
	prevAvailability := availabilityRuntime
	t.Cleanup(func() { availabilityRuntime = prevAvailability })
	InitAvailabilityForTest(availability.Snapshot{
		Valid:       true,
		Mode:        availability.ModeLocalRedisActive,
		ActiveRedis: availability.RedisLocal,
		CloudRedis:  availability.DependencyStatus{Healthy: false, Error: "cloud down"},
		LocalRedis:  availability.DependencyStatus{Healthy: true},
		MySQL:       availability.DependencyStatus{Healthy: true},
		MySQLState:  availability.MySQLHealthy,
		Reason:      "local_sticky",
		UpdatedAt:   time.Now(),
	})

	status, data := renderReadyzForTest()
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if data.Status != "degraded" {
		t.Fatalf("data status = %s, want degraded", data.Status)
	}
	if got := data.Components["active_redis"].Value; got != "local" {
		t.Fatalf("active redis = %s, want local", got)
	}
}

func TestReadyzReturns503WhenAuctionProtected(t *testing.T) {
	prevAvailability := availabilityRuntime
	t.Cleanup(func() { availabilityRuntime = prevAvailability })
	InitAvailabilityForTest(availability.Snapshot{
		Valid:       true,
		Mode:        availability.ModeAuctionProtected,
		ActiveRedis: availability.RedisNone,
		CloudRedis:  availability.DependencyStatus{Healthy: false, Error: "cloud down"},
		LocalRedis:  availability.DependencyStatus{Healthy: false, Error: "local down"},
		MySQL:       availability.DependencyStatus{Healthy: true},
		MySQLState:  availability.MySQLHealthy,
		Reason:      "redis_down",
		UpdatedAt:   time.Now(),
	})

	status, data := renderReadyzForTest()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}
	if data.Status != "degraded" {
		t.Fatalf("data status = %s, want degraded", data.Status)
	}
}
```

Add this helper in the test file so the tests can invoke `availabilityHealthData()` directly:

```go
func renderReadyzForTest() (int, healthData) {
	data, ok := availabilityHealthData()
	if !ok {
		return http.StatusServiceUnavailable, data
	}
	return http.StatusOK, data
}
```

- [ ] **Step 2: Run health tests and verify failure**

Run:

```bash
rtk go test ./internal/app/base/handler -run 'TestReadyzReportsLocalRedisActiveAsDegradedOK|TestReadyzReturns503WhenAuctionProtected'
```

Expected: FAIL because `availabilityHealthData()` still reports control-plane fields and treats valid local mode as fully ok.

- [ ] **Step 3: Update health data rendering**

In `internal/app/base/handler/health.go`, update `availabilityHealthData()` to:

```go
func availabilityHealthData() (healthData, bool) {
	snapshot := availabilityRuntime.Snapshot()
	components := make(map[string]componentStatus)
	if !snapshot.Valid {
		components["availability_mode"] = componentStatus{Status: "error", Error: snapshot.Error}
		observability.DefaultRecorder().Availability(context.Background(), observability.AvailabilityMetric{Result: "invalid"})
		return healthData{Status: "degraded", Components: components}, false
	}

	components["availability_mode"] = componentStatus{Status: "ok", Value: string(snapshot.Mode)}
	components["active_redis"] = componentStatus{Status: statusOK(snapshot.ActiveRedis != availability.RedisNone), Value: string(snapshot.ActiveRedis)}
	components["cloud_redis"] = dependencyComponent(snapshot.CloudRedis)
	components["local_redis"] = dependencyComponent(snapshot.LocalRedis)
	components["mysql"] = dependencyComponent(snapshot.MySQL)
	components["mysql_state"] = componentStatus{Status: string(snapshot.MySQLState)}
	components["presence"] = componentStatus{Status: wshub.PresenceStatus()}

	overall := "ok"
	ready := true
	switch snapshot.Mode {
	case availability.ModeLocalRedisActive, availability.ModeLocalRedisSwitching, availability.ModeMySQLBuffering:
		overall = "degraded"
	case availability.ModeAuctionProtected:
		overall = "degraded"
		ready = false
	}

	observability.DefaultRecorder().Availability(context.Background(), observability.AvailabilityMetric{
		Mode:        string(snapshot.Mode),
		ActiveRedis: string(snapshot.ActiveRedis),
		Result:      overall,
	})
	return healthData{Status: overall, Components: components}, ready
}

func dependencyComponent(status availability.DependencyStatus) componentStatus {
	if !status.Healthy {
		return componentStatus{Status: "error", Error: status.Error, Latency: status.Latency.String()}
	}
	return componentStatus{Status: "ok", Latency: status.Latency.String()}
}

func statusOK(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}
```

Remove the old `epoch` and `control_plane` component output from this function.

- [ ] **Step 4: Run health tests**

Run:

```bash
rtk go test ./internal/app/base/handler
```

Expected: PASS.

## Task 4: Update Item Service Availability Semantics

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_service_test.go`
- Modify: `internal/app/item/service/service_test.go`

- [ ] **Step 1: Write failing MySQL buffering tests**

Add to `internal/app/item/service/bid_service_test.go`:

```go
func TestPlaceBidAllowsMySQLBufferingWithinWindow(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	now := time.UnixMilli(1710000000000)
	svc.now = func() time.Time { return now }
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, now.Add(5*time.Minute))
	svc.SetAvailabilitySnapshotForTest(availability.Snapshot{
		Valid:                      true,
		Mode:                       availability.ModeMySQLBuffering,
		ActiveRedis:                availability.RedisCloud,
		MySQLState:                 availability.MySQLBuffering,
		MySQLBufferingStartedAt:    now.Add(-5 * time.Second),
		MySQLBufferingStartedUnixMS: now.Add(-5 * time.Second).UnixMilli(),
	})

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          100,
		UserName:       "Alice",
		IdempotencyKey: "buffering-ok",
	})
	if err != nil {
		t.Fatalf("PlaceBid() error = %v", err)
	}
}

func TestPlaceBidRejectsMySQLBufferingAfterWindow(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	now := time.UnixMilli(1710000000000)
	svc.now = func() time.Time { return now }
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, now.Add(5*time.Minute))
	svc.SetAvailabilitySnapshotForTest(availability.Snapshot{
		Valid:                      true,
		Mode:                       availability.ModeMySQLBuffering,
		ActiveRedis:                availability.RedisCloud,
		MySQLState:                 availability.MySQLBuffering,
		MySQLBufferingStartedAt:    now.Add(-11 * time.Second),
		MySQLBufferingStartedUnixMS: now.Add(-11 * time.Second).UnixMilli(),
	})

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          100,
		UserName:       "Alice",
		IdempotencyKey: "buffering-expired",
	})
	if !errors.Is(err, ErrAvailabilityUnavailable) {
		t.Fatalf("err = %v, want availability unavailable", err)
	}
}
```

Use the existing `seedOngoingItem`, `newFakeStore`, `newFakeCache`, `testPolicy`, and package-level `bidder` helpers already present in `bid_service_test.go`.

- [ ] **Step 2: Run buffering tests**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestPlaceBidAllowsMySQLBufferingWithinWindow|TestPlaceBidRejectsMySQLBufferingAfterWindow'
```

Expected: one or both FAIL because current service checks still use old snapshot fields and modes.

- [ ] **Step 3: Update service availability helper**

In `internal/app/item/service/service.go`, update `availabilitySnapshot()` default to use the new snapshot:

```go
func (s *Service) availabilitySnapshot() availability.Snapshot {
	if s.availability == nil {
		return availability.Snapshot{
			Valid:       true,
			Mode:        availability.ModeNormalCloud,
			ActiveRedis: availability.RedisCloud,
			MySQLState:  availability.MySQLHealthy,
		}
	}
	return s.availability.Snapshot()
}
```

Replace `shouldPauseSettlement()` with:

```go
func (s *Service) shouldPauseSettlement() bool {
	snapshot := s.availabilitySnapshot()
	if !snapshot.Valid {
		return true
	}
	if snapshot.Mode == availability.ModeAuctionProtected {
		return true
	}
	if snapshot.MySQLBufferingExpired(s.now(), s.mysqlBufferingWindow) {
		return true
	}
	return snapshot.Mode == availability.ModeMySQLBuffering
}
```

- [ ] **Step 4: Update PlaceBid gating**

In `internal/app/item/service/bid_service.go`, replace the old control-plane checks:

```go
snapshot := s.availabilitySnapshot()
if !snapshot.Valid {
	...
}
if snapshot.State.Mode == availability.ModeAuctionProtected {
	...
}
if snapshot.State.MySQLBufferingExpired(s.now(), s.mysqlBufferingWindow) {
	...
}
```

with:

```go
snapshot := s.availabilitySnapshot()
if !snapshot.Valid {
	bidResult = "rejected"
	bidReason = "availability_invalid"
	return nil, ErrAvailabilityUnavailable
}
if !snapshot.RedisWritable() {
	bidResult = "rejected"
	bidReason = "redis_unavailable"
	return nil, ErrAvailabilityUnavailable
}
if snapshot.Mode == availability.ModeAuctionProtected {
	bidResult = "rejected"
	bidReason = "auction_protected"
	return nil, ErrAvailabilityUnavailable
}
if snapshot.MySQLBufferingExpired(s.now(), s.mysqlBufferingWindow) {
	bidResult = "rejected"
	bidReason = "mysql_buffering_timeout"
	return nil, ErrAvailabilityUnavailable
}
```

When calling `PlaceBidLua`, pass an item-level epoch. For the first implementation, continue using the snapshot epoch only if still present; otherwise use `0` until Task 5 refines local rebuild:

```go
AuthorityEpoch: 0,
AuthorityState: itemcache.AuthorityReady,
```

- [ ] **Step 5: Run item service tests**

Run:

```bash
rtk go test ./internal/app/item/service
```

Expected: FAIL in tests that still assert old control-plane reason strings or old `snapshot.State.*` shape.

- [ ] **Step 6: Update tests to new snapshot shape**

For each failing test that constructs:

```go
availability.Snapshot{Valid: true, State: availability.State{...}}
```

replace it with:

```go
availability.Snapshot{
	Valid:       true,
	Mode:        availability.ModeLocalRedisActive,
	ActiveRedis: availability.RedisLocal,
	MySQLState:  availability.MySQLHealthy,
}
```

For invalid snapshots, use:

```go
availability.Snapshot{Valid: false, Mode: availability.ModeAuctionProtected, ActiveRedis: availability.RedisNone, Error: "stale"}
```

- [ ] **Step 7: Run item service tests again**

Run:

```bash
rtk go test ./internal/app/item/service
```

Expected: PASS.

## Task 5: Preserve Local Redis Rebuild With Rollback Semantics

**Files:**
- Modify: `internal/app/item/service/availability_rebuild.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/availability_rebuild_test.go`
- Modify: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Write rollback rebuild test**

Add to `internal/app/item/service/availability_rebuild_test.go`:

```go
func TestRebuildAcceptsDurableMySQLPointAsAuthority(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	store.bidLogsByEpoch["item_1:0"] = []*itemmodel.BidLog{
		{ID: "bid_1", ItemID: "item_1", UserID: "u1", Price: 1000, AuthorityEpoch: 0, AuctionVersion: 1},
		{ID: "bid_2", ItemID: "item_1", UserID: "u2", Price: 1200, AuthorityEpoch: 0, AuctionVersion: 2},
	}

	worker := newAvailabilityRebuildWorker(store, cache, availabilityRebuildConfig{BatchSize: 1})
	got := worker.rebuildItem(context.Background(), "item_1", 0)

	if got != rebuildReady {
		t.Fatalf("rebuild = %s, want ready", got)
	}
	state := cache.states["item_1"]
	if state.CurrentPrice != 1200 || state.LeaderUserID != "u2" || state.AuctionVersion != 2 {
		t.Fatalf("state = %+v", state)
	}
}
```

This test uses the existing fake store/cache helpers shared by the service tests.

- [ ] **Step 2: Run rebuild test**

Run:

```bash
rtk go test ./internal/app/item/service -run TestRebuildAcceptsDurableMySQLPointAsAuthority
```

Expected: PASS if current rebuild already accepts continuous MySQL logs; FAIL only if fake helpers need adjustment.

- [ ] **Step 3: Remove cloud switchback reconcile path**

In `internal/app/item/service/bid_service.go`, remove `reconcileCloudAuctionStateFromLocal` and the branch:

```go
if snapshot.Valid && snapshot.State.ActiveRedis == availability.RedisCloud && existing != nil && existing.AuthorityEpoch < snapshot.State.Epoch {
	...
}
```

The design explicitly avoids automatic local-to-cloud sync or merge.

- [ ] **Step 4: Update local rebuild trigger**

In `bidHotConfig`, change old checks from `snapshot.State.ActiveRedis` to:

```go
if snapshot.Valid && snapshot.ActiveRedis == availability.RedisLocal && existing == nil {
	if rebuildErr := s.rebuildLocalAuctionState(ctx, itemID, 0); rebuildErr != nil {
		result = "rejected"
		return nil, rebuildErr
	}
	...
}
```

Use epoch `0` for the first implementation unless a per-item authority epoch source already exists in Redis/MySQL.

- [ ] **Step 5: Remove Redis authorities from Service**

In `internal/app/item/service/service.go`, remove fields:

```go
cloudCache itemcache.Cache
localCache itemcache.Cache
```

Remove method:

```go
func (s *Service) SetRedisAuthorities(...)
```

In `internal/app/item/init.go`, delete the `SetRedisAuthorities` setup block.

- [ ] **Step 6: Run item and init tests**

Run:

```bash
rtk go test ./internal/app/item/...
```

Expected: PASS.

## Task 6: Update WebSocket And Active Redis Compatibility

**Files:**
- Modify if needed: `internal/app/ws/init.go`
- Modify if needed: `internal/app/ws/bus/broadcaster.go`
- Modify if needed: `internal/app/ws/bus/subscriber.go`
- Modify if needed: `internal/app/ws/handler/ticket.go`
- Tests: `internal/app/ws/bus/bus_test.go`, `internal/app/ws/handler/ticket_test.go`, `internal/app/ws/hub/hub_test.go`

- [ ] **Step 1: Run WS tests before edits**

Run:

```bash
rtk go test ./internal/app/ws/...
```

Expected: FAIL only where tests still construct old `availability.Snapshot{State: ...}`.

- [ ] **Step 2: Update WS test snapshots**

Replace old snapshot constructions with:

```go
availability.Snapshot{
	Valid:       true,
	Mode:        availability.ModeLocalRedisActive,
	ActiveRedis: availability.RedisLocal,
}
```

Invalid authority tests should use:

```go
availability.Snapshot{
	Valid:       false,
	Mode:        availability.ModeAuctionProtected,
	ActiveRedis: availability.RedisNone,
	Error:       "unavailable",
}
```

- [ ] **Step 3: Update ticket key epoch references**

If `ticket.go` still references `snapshot.State.Epoch`, replace ticket key calls with epoch `0`:

```go
return client.Set(ctx, ticketKey(0, ticket), userID, ttl).Err()
```

and:

```go
return client.GetDel(ctx, ticketKey(0, ticket)).Result()
```

Do not add local-to-cloud ticket migration.

- [ ] **Step 4: Run WS tests**

Run:

```bash
rtk go test ./internal/app/ws/...
```

Expected: PASS.

## Task 7: Deployment And Config Cleanup

**Files:**
- Modify: `deploy/k8s/02-configmaps.yaml`
- Modify: any config files that contain `availability.state_path`
- Modify: docs that directly instruct mounting `/availability/state.json`, only if they describe the current implementation path.

- [ ] **Step 1: Find control-plane config references**

Run:

```bash
rtk rg -n "state_path|stale_threshold|/availability/state.json|control_plane|local_redis_switching|redis_failover_threshold|mysql_buffering_window" config.yaml.example deploy config docs/superpowers/specs/2026-06-09-in-process-dependency-degradation-design.md
```

Expected: shows references to update. Do not modify older historical specs except the new dependency degradation spec unless they are used as active implementation instructions.

- [ ] **Step 2: Update deployment config map**

In `deploy/k8s/02-configmaps.yaml`, make the backend config `availability` block match:

```yaml
availability:
  redis_probe_interval: 1s
  redis_failover_threshold: 3s
  redis_recover_threshold: 30s
  mysql_probe_interval: 1s
  mysql_buffering_window: 10s
  local_redis:
    addr: redis:6379
    password: ""
    db: 0
```

- [ ] **Step 3: Remove state file mount references from active manifests**

If active backend deployment manifests mount `/availability/state.json` or `/availability`, remove those volume mounts and volumes. Leave unrelated observability or data volume mounts alone.

- [ ] **Step 4: Run config/deployment grep again**

Run:

```bash
rtk rg -n "state_path|stale_threshold|/availability/state.json|control_plane" config.yaml.example deploy/k8s
```

Expected: no matches in active config or deployment manifests.

## Task 8: Full Verification

**Files:**
- No new files.

- [ ] **Step 1: Run focused tests**

Run:

```bash
rtk go test ./internal/core/availability ./internal/app/base/handler ./internal/app/item/... ./internal/app/ws/... ./config ./cmd/server
```

Expected: PASS.

- [ ] **Step 2: Run full Go test suite**

Run:

```bash
rtk go test ./...
```

Expected: PASS. If tests attempt real MySQL/Redis connections, stop and inspect; local unit tests must use fakes for this change.

- [ ] **Step 3: Run build**

Run:

```bash
rtk go build ./...
```

Expected: PASS.

- [ ] **Step 4: Review changed files**

Run:

```bash
rtk git diff --stat
rtk git diff -- internal/core/availability config cmd/server internal/app/base internal/app/item internal/app/ws deploy/k8s config.yaml.example
```

Expected: diff only covers availability runtime, config/wiring, health, item gating/rebuild, WS compatibility, and active deployment config.

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add internal/core/availability config cmd/server internal/app/base internal/app/item internal/app/ws deploy/k8s config.yaml.example docs/superpowers/specs/2026-06-09-in-process-dependency-degradation-design.md docs/superpowers/plans/2026-06-09-in-process-dependency-degradation.md
rtk git commit -m "feat: add in-process dependency degradation"
```

Expected: commit succeeds.

## Self-Review Notes

- Spec coverage: Redis failover, local sticky behavior, allowed Redis rollback, MySQL buffering window, protective rejection, health reporting, config cleanup, and fake-test requirements are each covered by tasks.
- No local-to-cloud Redis sync is included; this was explicitly removed from scope.
- No shared control plane or `/availability/state.json` remains in active implementation tasks.
- TDD coverage is front-loaded for runtime, config, health, item service, rebuild, and WS compatibility.
