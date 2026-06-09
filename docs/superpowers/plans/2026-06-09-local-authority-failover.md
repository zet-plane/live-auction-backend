# Local Authority Failover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep already-running backend pods alive during cloud Redis/MySQL reachability failures, switch auction authority to local Redis when safe, protect unsafe items, and pause irreversible side effects until durable verification completes.

**Architecture:** Add a small `internal/core/availability` runtime that reads/writes the shared control-plane state file and exposes an in-memory snapshot. Route auction Redis operations, WebSocket tickets, WebSocket bus, and presence through an active Redis authority selected from that snapshot. Use `authority_epoch` and `auction_version` as item-level sequence evidence for rebuild, MySQL buffering, bid-log idempotency, and settlement gating.

**Tech Stack:** Go, GORM, go-redis, Redis Lua, Redis Streams, Flamego, robfig/cron, k3s manifests, existing fake-store service tests.

---

## File Structure

- Create `internal/core/availability/state.go`: state schema, mode constants, validation, staleness checks, JSON parsing.
- Create `internal/core/availability/store.go`: atomic state file writer with file lock, temp file, rename, and fsync.
- Create `internal/core/availability/watcher.go`: polling watcher that keeps an atomic in-memory snapshot.
- Create `internal/core/availability/authority.go`: Redis authority selector and health/protection helpers.
- Create `internal/core/availability/state_test.go`, `store_test.go`, `watcher_test.go`, `authority_test.go`: parser, writer, snapshot, and fail-closed tests.
- Modify `config/vars.go` and `config.yaml.example`: add `availability` configuration.
- Modify `internal/core/cache/cache.go`: allow Redis client creation without startup ping and support cloud/local configs.
- Modify `internal/core/kernel/kernel.go`: carry cloud Redis, local Redis, and availability runtime.
- Modify `cmd/server/server.go`: build both Redis clients without fatal Redis ping failure, initialize availability runtime, and keep server alive when Redis is unreachable after startup.
- Modify `internal/app/base/handler/health.go`: use availability snapshot for `/readyz` and `/health`.
- Modify `internal/app/item/cache/cache.go`, `bid.go`, `bid_log_stream.go`: add authority metadata, item authority state APIs, and bid-log event fields.
- Modify `internal/app/item/model/bid_log.go`, `internal/app/item/dao/bid_log.go`: add `authority_epoch`, `auction_version`, `idempotency_key`, and idempotent batch persistence.
- Modify `internal/app/item/service/bid_service.go`, `bid_log_worker.go`, `service.go`: gate bids and settlement through availability state, enforce MySQL buffering, and verify backlog before side effects.
- Create `internal/app/item/service/availability_rebuild.go`: item-scoped local Redis rebuild from MySQL with continuity verification.
- Modify `internal/app/ws/init.go`, `internal/app/ws/handler/ticket.go`, `internal/app/ws/handler/ws.go`, `internal/app/ws/bus/broadcaster.go`, `internal/app/ws/bus/subscriber.go`, `internal/app/ws/hub/hub.go`: route ticket, bus, and presence through active Redis and report presence degraded.
- Modify `internal/core/observability/metrics.go`: add availability, rebuild, buffering, protected item, ticket, and presence metrics.
- Modify `deploy/k8s/11-app.yaml`, `deploy/k8s/02-configmaps.yaml`, and `deploy/k8s/01-secrets.example.yaml`: mount `/availability`, remove blocking Redis init behavior, configure local Redis and state path.

---

### Task 1: Availability State Parser And Snapshot

**Files:**
- Create: `internal/core/availability/state.go`
- Create: `internal/core/availability/state_test.go`

- [ ] **Step 1: Write failing parser and validation tests**

Create `internal/core/availability/state_test.go`:

```go
package availability

import (
	"testing"
	"time"
)

func TestParseStateAcceptsValidState(t *testing.T) {
	now := time.UnixMilli(1710000000000)
	raw := []byte(`{"version":1,"mode":"normal_cloud","epoch":12,"active_redis":"cloud","mysql_state":"healthy","mysql_buffering_started_at_unix_ms":0,"updated_at_unix_ms":1710000000000,"reason":"probe_ok"}`)

	state, err := ParseState(raw, ParseOptions{Now: func() time.Time { return now }, LastEpoch: 11, StaleAfter: 5 * time.Second})
	if err != nil {
		t.Fatalf("ParseState() error = %v", err)
	}
	if state.Mode != ModeNormalCloud || state.Epoch != 12 || state.ActiveRedis != RedisCloud {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestParseStateRejectsRegressingEpoch(t *testing.T) {
	raw := []byte(`{"version":1,"mode":"normal_cloud","epoch":10,"active_redis":"cloud","mysql_state":"healthy","updated_at_unix_ms":1710000000000,"reason":"probe_ok"}`)

	_, err := ParseState(raw, ParseOptions{Now: func() time.Time { return time.UnixMilli(1710000000000) }, LastEpoch: 11, StaleAfter: 5 * time.Second})
	if err == nil {
		t.Fatal("expected regressing epoch to fail")
	}
}

func TestParseStateRejectsStaleFile(t *testing.T) {
	raw := []byte(`{"version":1,"mode":"local_redis_active","epoch":12,"active_redis":"local","mysql_state":"healthy","updated_at_unix_ms":1710000000000,"reason":"probe_ok"}`)

	_, err := ParseState(raw, ParseOptions{Now: func() time.Time { return time.UnixMilli(1710000009000) }, LastEpoch: 12, StaleAfter: 5 * time.Second})
	if err == nil {
		t.Fatal("expected stale state to fail")
	}
}

func TestStateProtectsWhenBufferingWindowExpired(t *testing.T) {
	state := State{
		Version:                       1,
		Mode:                          ModeMySQLBuffering,
		Epoch:                         20,
		ActiveRedis:                   RedisCloud,
		MySQLState:                    MySQLBuffering,
		MySQLBufferingStartedAtUnixMS: 1710000000000,
		UpdatedAtUnixMS:               1710000000000,
	}

	if !state.MySQLBufferingExpired(time.UnixMilli(1710000010001), 10*time.Second) {
		t.Fatal("expected buffering window to be expired")
	}
}
```

- [ ] **Step 2: Run parser tests to verify they fail**

Run:

```bash
rtk go test ./internal/core/availability -run 'TestParseState|TestStateProtects' -count=1
```

Expected: FAIL because package `internal/core/availability` does not exist.

- [ ] **Step 3: Implement state types and validation**

Create `internal/core/availability/state.go`:

```go
package availability

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

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

	MySQLHealthy    MySQLStatus = "healthy"
	MySQLDown       MySQLStatus = "down"
	MySQLBuffering  MySQLStatus = "buffering"
	MySQLRecovering MySQLStatus = "recovering"
)

var (
	ErrInvalidState = errors.New("availability state invalid")
	ErrStaleState   = errors.New("availability state stale")
)

type State struct {
	Version                       int            `json:"version"`
	Mode                          Mode           `json:"mode"`
	Epoch                         int64          `json:"epoch"`
	ActiveRedis                   RedisAuthority `json:"active_redis"`
	MySQLState                    MySQLStatus    `json:"mysql_state"`
	MySQLBufferingStartedAtUnixMS int64          `json:"mysql_buffering_started_at_unix_ms,omitempty"`
	UpdatedAtUnixMS               int64          `json:"updated_at_unix_ms"`
	Reason                        string         `json:"reason"`
}

type ParseOptions struct {
	Now        func() time.Time
	LastEpoch  int64
	StaleAfter time.Duration
}

func ParseState(raw []byte, opts ParseOptions) (State, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, fmt.Errorf("%w: decode: %v", ErrInvalidState, err)
	}
	if err := state.Validate(opts.Now(), opts.LastEpoch, opts.StaleAfter); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s State) Validate(now time.Time, lastEpoch int64, staleAfter time.Duration) error {
	if s.Version != 1 {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidState, s.Version)
	}
	if s.Epoch < lastEpoch {
		return fmt.Errorf("%w: epoch regressed from %d to %d", ErrInvalidState, lastEpoch, s.Epoch)
	}
	if !validMode(s.Mode) || !validRedis(s.ActiveRedis) || !validMySQL(s.MySQLState) {
		return fmt.Errorf("%w: mode=%q redis=%q mysql=%q", ErrInvalidState, s.Mode, s.ActiveRedis, s.MySQLState)
	}
	if s.UpdatedAtUnixMS <= 0 {
		return fmt.Errorf("%w: updated_at_unix_ms is required", ErrInvalidState)
	}
	if staleAfter > 0 && now.Sub(time.UnixMilli(s.UpdatedAtUnixMS)) > staleAfter {
		return fmt.Errorf("%w: updated_at_unix_ms=%d", ErrStaleState, s.UpdatedAtUnixMS)
	}
	return nil
}

func (s State) ValidForWrites(now time.Time, staleAfter time.Duration) bool {
	return s.Validate(now, s.Epoch, staleAfter) == nil
}

func (s State) MySQLBufferingExpired(now time.Time, window time.Duration) bool {
	if s.Mode != ModeMySQLBuffering || s.MySQLBufferingStartedAtUnixMS <= 0 {
		return false
	}
	return now.Sub(time.UnixMilli(s.MySQLBufferingStartedAtUnixMS)) > window
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeNormalCloud, ModeLocalRedisSwitching, ModeLocalRedisActive, ModeMySQLBuffering, ModeAuctionProtected:
		return true
	default:
		return false
	}
}

func validRedis(redis RedisAuthority) bool {
	return redis == RedisCloud || redis == RedisLocal
}

func validMySQL(mysql MySQLStatus) bool {
	switch mysql {
	case MySQLHealthy, MySQLDown, MySQLBuffering, MySQLRecovering:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run parser tests**

Run:

```bash
rtk go test ./internal/core/availability -run 'TestParseState|TestStateProtects' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/core/availability/state.go internal/core/availability/state_test.go
rtk git commit -m "feat: add availability state parser"
```

---

### Task 2: Atomic Control-Plane Store And Watcher

**Files:**
- Create: `internal/core/availability/store.go`
- Create: `internal/core/availability/watcher.go`
- Create: `internal/core/availability/store_test.go`
- Create: `internal/core/availability/watcher_test.go`

- [ ] **Step 1: Write failing store and watcher tests**

Create `internal/core/availability/store_test.go`:

```go
package availability

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreWriteAndReadState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewFileStore(path)
	state := State{Version: 1, Mode: ModeNormalCloud, Epoch: 1, ActiveRedis: RedisCloud, MySQLState: MySQLHealthy, UpdatedAtUnixMS: time.Now().UnixMilli(), Reason: "test"}

	if err := store.Write(state); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	parsed, err := ParseState(raw, ParseOptions{Now: time.Now, LastEpoch: 0, StaleAfter: time.Minute})
	if err != nil {
		t.Fatalf("ParseState() error = %v", err)
	}
	if parsed.Epoch != 1 {
		t.Fatalf("epoch = %d, want 1", parsed.Epoch)
	}
}
```

Create `internal/core/availability/watcher_test.go`:

```go
package availability

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherKeepsLastValidSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(path)
	now := time.UnixMilli(1710000000000)
	initial := State{Version: 1, Mode: ModeNormalCloud, Epoch: 1, ActiveRedis: RedisCloud, MySQLState: MySQLHealthy, UpdatedAtUnixMS: now.UnixMilli(), Reason: "ok"}
	if err := store.Write(initial); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	w := NewWatcher(path, WatcherOptions{Now: func() time.Time { return now }, StaleAfter: time.Minute})
	if err := w.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	got := w.Snapshot()
	if got.State.Epoch != 1 || !got.Valid {
		t.Fatalf("snapshot = %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
rtk go test ./internal/core/availability -run 'TestFileStore|TestWatcher' -count=1
```

Expected: FAIL with undefined `NewFileStore` and `NewWatcher`.

- [ ] **Step 3: Implement atomic file store**

Create `internal/core/availability/store.go`:

```go
package availability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Write(state State) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	cleanup = false
	if dir, err := os.Open(filepath.Dir(s.path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (s *FileStore) Read(opts ParseOptions) (State, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return State{}, fmt.Errorf("read availability state: %w", err)
	}
	return ParseState(raw, opts)
}
```

- [ ] **Step 4: Implement watcher snapshot**

Create `internal/core/availability/watcher.go`:

```go
package availability

import (
	"sync/atomic"
	"time"
)

type Snapshot struct {
	State State
	Valid bool
	Error string
}

type WatcherOptions struct {
	Now        func() time.Time
	StaleAfter time.Duration
	LastEpoch  int64
}

type Watcher struct {
	path string
	opts WatcherOptions
	v    atomic.Value
}

func NewWatcher(path string, opts WatcherOptions) *Watcher {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	w := &Watcher{path: path, opts: opts}
	w.v.Store(Snapshot{Valid: false, Error: "not loaded"})
	return w
}

func (w *Watcher) Refresh() error {
	current := w.Snapshot()
	state, err := NewFileStore(w.path).Read(ParseOptions{
		Now:        w.opts.Now,
		LastEpoch:  maxInt64(w.opts.LastEpoch, current.State.Epoch),
		StaleAfter: w.opts.StaleAfter,
	})
	if err != nil {
		w.v.Store(Snapshot{Valid: false, Error: err.Error()})
		return err
	}
	w.v.Store(Snapshot{State: state, Valid: true})
	return nil
}

func (w *Watcher) Snapshot() Snapshot {
	return w.v.Load().(Snapshot)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 5: Run availability tests**

Run:

```bash
rtk go test ./internal/core/availability -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add internal/core/availability/store.go internal/core/availability/watcher.go internal/core/availability/store_test.go internal/core/availability/watcher_test.go
rtk git commit -m "feat: add availability state store"
```

---

### Task 3: Configuration, Kernel Wiring, And Redis Client Creation

**Files:**
- Modify: `config/vars.go`
- Modify: `config.yaml.example`
- Modify: `internal/core/cache/cache.go`
- Modify: `internal/core/kernel/kernel.go`
- Modify: `cmd/server/server.go`
- Test: `internal/core/cache/cache_test.go`

- [ ] **Step 1: Write failing cache configuration test**

Create `internal/core/cache/cache_test.go`:

```go
package cache

import "testing"

func TestOpenDoesNotPingWhenDisabled(t *testing.T) {
	client, err := Open(Config{Addr: "127.0.0.1:1", DisablePing: true})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if client == nil {
		t.Fatal("expected redis client")
	}
	_ = client.Close()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
rtk go test ./internal/core/cache -run TestOpenDoesNotPingWhenDisabled -count=1
```

Expected: FAIL because `Config.DisablePing` is undefined.

- [ ] **Step 3: Add availability config**

Modify `config/vars.go`:

```go
type GlobalConfig struct {
	Mode          string        `yaml:"mode"          mapstructure:"mode"`
	App           App           `yaml:"app"           mapstructure:"app"`
	HTTP          HTTP          `yaml:"http"          mapstructure:"http"`
	Database      Database      `yaml:"database"      mapstructure:"database"`
	Redis         Redis         `yaml:"redis"         mapstructure:"redis"`
	Availability  Availability  `yaml:"availability"  mapstructure:"availability"`
	Auth          Auth          `yaml:"auth"          mapstructure:"auth"`
	Security      Security      `yaml:"security"      mapstructure:"security"`
	Auction       Auction       `yaml:"auction"       mapstructure:"auction"`
	Storage       Storage       `yaml:"storage"       mapstructure:"storage"`
	Observability Observability `yaml:"observability" mapstructure:"observability"`
}

type Availability struct {
	StatePath                    string `yaml:"state_path"                       mapstructure:"state_path"`
	StaleThreshold              string `yaml:"stale_threshold"                  mapstructure:"stale_threshold"`
	RedisFailoverThreshold      string `yaml:"redis_failover_threshold"         mapstructure:"redis_failover_threshold"`
	MySQLBufferingWindow        string `yaml:"mysql_buffering_window"           mapstructure:"mysql_buffering_window"`
	LocalRedis                  Redis  `yaml:"local_redis"                      mapstructure:"local_redis"`
	RebuildBatchSize            int    `yaml:"rebuild_batch_size"               mapstructure:"rebuild_batch_size"`
	RebuildWorkerCount          int    `yaml:"rebuild_worker_count"             mapstructure:"rebuild_worker_count"`
	BidWaitWhileRebuildingMinMS int    `yaml:"bid_wait_while_rebuilding_min_ms" mapstructure:"bid_wait_while_rebuilding_min_ms"`
	BidWaitWhileRebuildingMaxMS int    `yaml:"bid_wait_while_rebuilding_max_ms" mapstructure:"bid_wait_while_rebuilding_max_ms"`
}
```

Modify `config.yaml.example` by adding:

```yaml
availability:
  state_path: /availability/state.json
  stale_threshold: 5s
  redis_failover_threshold: 3s
  mysql_buffering_window: 10s
  local_redis:
    addr: redis:6379
    password: ""
    db: 0
  rebuild_batch_size: 50
  rebuild_worker_count: 2
  bid_wait_while_rebuilding_min_ms: 100
  bid_wait_while_rebuilding_max_ms: 300
```

- [ ] **Step 4: Add optional Redis ping**

Modify `internal/core/cache/cache.go`:

```go
type Config struct {
	Addr        string
	Password    string
	DB          int
	DisablePing bool
}

func Open(cfg Config) (*redis.Client, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("redis addr is required")
	}
	client := redis.NewClient(&redis.Options{Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB})
	if err := redisotel.InstrumentTracing(client); err != nil {
		return nil, fmt.Errorf("instrument redis tracing: %w", err)
	}
	if err := redisotel.InstrumentMetrics(client); err != nil {
		return nil, fmt.Errorf("instrument redis metrics: %w", err)
	}
	if cfg.DisablePing {
		return client, nil
	}
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}
```

- [ ] **Step 5: Add Redis authority fields to kernel**

Modify `internal/core/kernel/kernel.go`:

```go
type Engine struct {
	Context      context.Context
	Cancel       context.CancelFunc
	Flame        *flamego.Flame
	DB           *gorm.DB
	Cache        *redis.Client
	CloudRedis   *redis.Client
	LocalRedis   *redis.Client
	Availability *availability.Runtime
	Config       *config.Config
	Cron         *cron.Cron
}
```

Import `github.com/zet-plane/live-auction-backend/internal/core/availability`.

- [ ] **Step 6: Update server wiring**

Modify `cmd/server/server.go` so Redis clients are created with `DisablePing: true` and startup does not fatal solely because Redis cannot be pinged:

```go
cloudRedis, err := cache.Open(cache.Config{
	Addr:        cfg.Redis.Addr,
	Password:    cfg.Redis.Password,
	DB:          cfg.Redis.DB,
	DisablePing: true,
})
if err != nil {
	logx.Fatalf("failed to create cloud redis client: %v", err)
}

localRedisCfg := cfg.Availability.LocalRedis
if localRedisCfg.Addr == "" {
	localRedisCfg = cfg.Redis
}
localRedis, err := cache.Open(cache.Config{
	Addr:        localRedisCfg.Addr,
	Password:    localRedisCfg.Password,
	DB:          localRedisCfg.DB,
	DisablePing: true,
})
if err != nil {
	logx.Fatalf("failed to create local redis client: %v", err)
}

engine, err := buildEngine(cfg, db, cloudRedis, localRedis)
```

Change `buildEngine` signature:

```go
func buildEngine(cfg *config.Config, db *gorm.DB, cloudRedis, localRedis *redis.Client) (*kernel.Engine, error)
```

Set `Cache: cloudRedis`, `CloudRedis: cloudRedis`, and `LocalRedis: localRedis` until Task 4 introduces the active selector.

- [ ] **Step 7: Run config/cache/server tests**

Run:

```bash
rtk go test ./internal/core/cache ./cmd/server -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
rtk git add config/vars.go config.yaml.example internal/core/cache/cache.go internal/core/cache/cache_test.go internal/core/kernel/kernel.go cmd/server/server.go
rtk git commit -m "feat: wire availability redis clients"
```

---

### Task 4: Active Redis Authority Selector

**Files:**
- Create: `internal/core/availability/authority.go`
- Create: `internal/core/availability/authority_test.go`
- Modify: `internal/core/kernel/kernel.go`
- Modify: `cmd/server/server.go`
- Modify: `internal/app/item/init.go`
- Modify: `internal/app/room/init.go`
- Modify: `internal/app/ws/init.go`
- Modify: `internal/app/order/init.go`

- [ ] **Step 1: Write failing selector tests**

Create `internal/core/availability/authority_test.go`:

```go
package availability

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisSelectorChoosesLocalWhenStateSaysLocal(t *testing.T) {
	cloud := redis.NewClient(&redis.Options{Addr: "cloud:6379"})
	local := redis.NewClient(&redis.Options{Addr: "local:6379"})
	selector := NewRedisSelector(cloud, local)

	got, ok := selector.Client(Snapshot{Valid: true, State: State{ActiveRedis: RedisLocal}})
	if !ok {
		t.Fatal("expected client")
	}
	if got != local {
		t.Fatal("expected local redis")
	}
}

func TestRedisSelectorFailsClosedOnInvalidSnapshot(t *testing.T) {
	selector := NewRedisSelector(redis.NewClient(&redis.Options{Addr: "cloud:6379"}), redis.NewClient(&redis.Options{Addr: "local:6379"}))

	got, ok := selector.Client(Snapshot{Valid: false, Error: "stale"})
	if ok || got != nil {
		t.Fatalf("Client() = (%v, %v), want nil false", got, ok)
	}
}
```

- [ ] **Step 2: Run selector tests to verify they fail**

Run:

```bash
rtk go test ./internal/core/availability -run TestRedisSelector -count=1
```

Expected: FAIL with undefined `NewRedisSelector`.

- [ ] **Step 3: Implement selector and runtime**

Create `internal/core/availability/authority.go`:

```go
package availability

import "github.com/redis/go-redis/v9"

type RedisSelector struct {
	cloud *redis.Client
	local *redis.Client
}

func NewRedisSelector(cloud, local *redis.Client) *RedisSelector {
	return &RedisSelector{cloud: cloud, local: local}
}

func (s *RedisSelector) Client(snapshot Snapshot) (*redis.Client, bool) {
	if !snapshot.Valid {
		return nil, false
	}
	switch snapshot.State.ActiveRedis {
	case RedisCloud:
		return s.cloud, s.cloud != nil
	case RedisLocal:
		return s.local, s.local != nil
	default:
		return nil, false
	}
}

type Runtime struct {
	Watcher  *Watcher
	Selector *RedisSelector
}

func NewRuntime(watcher *Watcher, selector *RedisSelector) *Runtime {
	return &Runtime{Watcher: watcher, Selector: selector}
}

func (r *Runtime) Snapshot() Snapshot {
	if r == nil || r.Watcher == nil {
		return Snapshot{Valid: false, Error: "availability runtime unconfigured"}
	}
	return r.Watcher.Snapshot()
}

func (r *Runtime) ActiveRedis() (*redis.Client, Snapshot, bool) {
	snapshot := r.Snapshot()
	if r == nil || r.Selector == nil {
		return nil, snapshot, false
	}
	client, ok := r.Selector.Client(snapshot)
	return client, snapshot, ok
}
```

- [ ] **Step 4: Wire runtime into server and modules**

In `cmd/server/server.go`, after creating Redis clients:

```go
statePath := cfg.Availability.StatePath
if statePath == "" {
	statePath = "/availability/state.json"
}
watcher := availability.NewWatcher(statePath, availability.WatcherOptions{
	Now:        time.Now,
	StaleAfter: cfg.AvailabilityStaleThreshold(),
})
if err := watcher.Refresh(); err != nil {
	logx.Warnw("availability state initial refresh failed", "err", err)
}
availabilityRuntime := availability.NewRuntime(watcher, availability.NewRedisSelector(cloudRedis, localRedis))
engine, err := buildEngine(cfg, db, cloudRedis, localRedis, availabilityRuntime)
```

Add duration helpers in `config`:

```go
func (c *GlobalConfig) AvailabilityStaleThreshold() time.Duration {
	return parseDuration(c.Availability.StaleThreshold, 5*time.Second)
}

func (c *GlobalConfig) MySQLBufferingWindow() time.Duration {
	return parseDuration(c.Availability.MySQLBufferingWindow, 10*time.Second)
}
```

Set `engine.Availability = availabilityRuntime`.

In module `Load` functions, keep `engine.Cache` as the cloud default in this task. Add comments only where the next task will replace direct client use:

```go
// Active Redis routing is injected in cache wrappers by Task 6 and Task 10.
```

- [ ] **Step 5: Run availability and server tests**

Run:

```bash
rtk go test ./internal/core/availability ./internal/core/cache ./cmd/server -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add internal/core/availability/authority.go internal/core/availability/authority_test.go internal/core/kernel/kernel.go cmd/server/server.go config/vars.go
rtk git commit -m "feat: add active redis selector"
```

---

### Task 5: Health Semantics And Availability Metrics

**Files:**
- Modify: `internal/app/base/handler/health.go`
- Modify: `internal/app/base/init.go`
- Modify: `internal/core/observability/metrics.go`
- Test: `internal/app/base/handler/health_test.go`

- [ ] **Step 1: Write failing health tests**

Create `internal/app/base/handler/health_test.go`:

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
)

func TestReadyzStaysReadyWhenCloudRedisDownButControlPlaneValid(t *testing.T) {
	InitAvailabilityForTest(availability.Snapshot{Valid: true, State: availability.State{
		Version: 1, Mode: availability.ModeLocalRedisActive, Epoch: 2, ActiveRedis: availability.RedisLocal,
		MySQLState: availability.MySQLHealthy, UpdatedAtUnixMS: time.Now().UnixMilli(), Reason: "cloud_redis_down",
	}})
	f := flamego.New()
	f.Get("/readyz", Readyz)

	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHealthIncludesPresenceDegraded(t *testing.T) {
	InitAvailabilityForTest(availability.Snapshot{Valid: true, State: availability.State{
		Version: 1, Mode: availability.ModeLocalRedisActive, Epoch: 2, ActiveRedis: availability.RedisLocal,
		MySQLState: availability.MySQLHealthy, UpdatedAtUnixMS: time.Now().UnixMilli(), Reason: "local_mode",
	}})
	SetPresenceStatusForTest("degraded")
	f := flamego.New()
	f.Get("/health", Health)

	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "presence") || !strings.Contains(rec.Body.String(), "degraded") {
		t.Fatalf("health body missing presence degraded: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
rtk go test ./internal/app/base/handler -run 'TestReadyz|TestHealthIncludesPresence' -count=1
```

Expected: FAIL with undefined test hooks.

- [ ] **Step 3: Inject availability into health handler**

Modify `internal/app/base/handler/health.go`:

```go
var (
	db                 *gorm.DB
	cache              *redis.Client
	availabilityRuntime interface{ Snapshot() availability.Snapshot }
	presenceStatus      = "ok"
	uploadSvc           *service.UploadService
)

func InitAvailability(rt interface{ Snapshot() availability.Snapshot }) {
	availabilityRuntime = rt
}

func InitAvailabilityForTest(snapshot availability.Snapshot) {
	availabilityRuntime = staticAvailability{snapshot: snapshot}
}

type staticAvailability struct{ snapshot availability.Snapshot }

func (s staticAvailability) Snapshot() availability.Snapshot { return s.snapshot }

func SetPresenceStatusForTest(status string) { presenceStatus = status }
```

Update `Readyz` to return `200` when availability snapshot is valid even if Redis probe is degraded:

```go
snapshot := availability.Snapshot{Valid: true}
if availabilityRuntime != nil {
	snapshot = availabilityRuntime.Snapshot()
}
if !snapshot.Valid {
	response.Success(r, http.StatusServiceUnavailable, "degraded", healthData{
		Status: "degraded",
		Components: map[string]componentStatus{
			"control_plane": {Status: "error", Error: snapshot.Error},
		},
	})
	return
}
response.OK(r, healthData{Status: "ok", Components: map[string]componentStatus{
	"control_plane": {Status: "ok"},
	"mode":          {Status: string(snapshot.State.Mode)},
}})
```

Update `Health` to include control plane, mode, epoch, active Redis, MySQL state, ticket, and presence components. Keep secret values out of response.

- [ ] **Step 4: Wire handler init**

Modify `internal/app/base/init.go` where base handler initialization occurs:

```go
handler.Init(engine.DB, engine.Cache, uploadSvc)
handler.InitAvailability(engine.Availability)
```

- [ ] **Step 5: Add metrics interface fields**

Modify `internal/core/observability/metrics.go`:

```go
type AvailabilityMetric struct {
	Mode        string
	Epoch       int64
	ActiveRedis string
	Result      string
}

func (NoopRecorder) Availability(context.Context, AvailabilityMetric) {}
```

Add the method to `Recorder` and implement a counter/gauge pair in `OTelRecorder` with safe labels `mode`, `active_redis`, and `result`.

- [ ] **Step 6: Run health and observability tests**

Run:

```bash
rtk go test ./internal/app/base/handler ./internal/core/observability -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
rtk git add internal/app/base/handler/health.go internal/app/base/handler/health_test.go internal/app/base/init.go internal/core/observability/metrics.go
rtk git commit -m "feat: expose availability health"
```

---

### Task 6: Bid Authority Metadata And Redis Lua Versioning

**Files:**
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/cache/bid.go`
- Modify: `internal/app/item/cache/bid_log_stream.go`
- Modify: `internal/app/item/cache/bid_log_stream_test.go`
- Test: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Write failing bid-log parse test for authority fields**

Modify `internal/app/item/cache/bid_log_stream_test.go`:

```go
func TestParseBidLogStreamMessageIncludesAuthorityFields(t *testing.T) {
	messages := []redis.XMessage{{
		ID: "1-0",
		Values: map[string]any{
			"bid_id": "bid_1", "item_id": "item_1", "room_id": "room_1", "user_id": "user_1",
			"price": "1200", "created_at_unix_ms": "1710000000000",
			"authority_epoch": "7", "auction_version": "3", "idempotency_key": "idem_1",
		},
	}}
	got, err := parseBidLogStreamMessages(messages, nil)
	if err != nil {
		t.Fatalf("parseBidLogStreamMessages() error = %v", err)
	}
	if got[0].Event.AuthorityEpoch != 7 || got[0].Event.AuctionVersion != 3 || got[0].Event.IdempotencyKey != "idem_1" {
		t.Fatalf("event = %+v", got[0].Event)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
rtk go test ./internal/app/item/cache -run TestParseBidLogStreamMessageIncludesAuthorityFields -count=1
```

Expected: FAIL because `BidLogEvent` lacks the authority fields.

- [ ] **Step 3: Add authority fields to cache types**

Modify `internal/app/item/cache/cache.go`:

```go
type AuctionState struct {
	AuthorityEpoch    int64
	AuthorityState    string
	AuctionVersion    int64
	Status            string
	RoomID            string
	CurrentPrice      int64
	DealPrice         int64
	LeaderUserID      string
	EndTime           time.Time
	EndTimeUnixMS     int64
	EndedAtUnixMS     int64
	BidIncrement      int64
	PriceCap          int64
	DepositAmount     int64
	BidCount          int
	ParticipantCount  int
	IsExtended        bool
	ExtendCount       int
	TotalExtendedSec  int
	ExtendTriggerSec  int
	AutoExtendSec     int
	MaxExtendCount    int
	MaxTotalExtendSec int
	EndReason         string
}

type BidLuaArgs struct {
	AuthorityEpoch    int64
	AuthorityState    string
	UserID            string
	UserName          string
	BidID             string
	RoomID            string
	Price             int64
	BidIncrement      int64
	PriceCap          int64
	ExtendTriggerSec  int
	AutoExtendSec     int
	MaxExtendCount    int
	MaxTotalExtendSec int
	NowUnix           int64
	CreatedAtUnixMS   int64
	IdempotencyKey    string
	IdempotencyTTL    int
}

type BidLogEvent struct {
	BidID           string
	ItemID          string
	RoomID          string
	UserID          string
	Price           int64
	CreatedAtUnixMS int64
	AuthorityEpoch  int64
	AuctionVersion  int64
	IdempotencyKey  string
}
```

- [ ] **Step 4: Update Lua script checks and version increment**

Modify `internal/app/item/cache/bid.go`:

```lua
local expected_epoch = tonumber(ARGV[16])
local expected_authority_state = ARGV[17]

local authority_epoch = tonumber(s['authority_epoch'] or 0)
local authority_state = s['authority_state'] or ''
if authority_epoch ~= expected_epoch then return {5,'',0,'',0,0,0,0,'','authority_epoch_mismatch',0,0} end
if authority_state ~= expected_authority_state then return {6,'',0,'',0,0,0,0,'','authority_not_ready',0,0} end

local auction_version = tonumber(s['auction_version'] or 0)
auction_version = auction_version + 1

redis.call('XADD', '{{BID_LOG_STREAM_NAME}}', '*',
  '{{BID_LOG_FIELD_BID_ID}}', bid_id,
  '{{BID_LOG_FIELD_ITEM_ID}}', item_id,
  '{{BID_LOG_FIELD_ROOM_ID}}', room_id,
  '{{BID_LOG_FIELD_USER_ID}}', user_id,
  '{{BID_LOG_FIELD_PRICE}}', price,
  '{{BID_LOG_FIELD_CREATED_AT_UNIX_MS}}', created_ms,
  '{{BID_LOG_FIELD_AUTHORITY_EPOCH}}', expected_epoch,
  '{{BID_LOG_FIELD_AUCTION_VERSION}}', auction_version,
  '{{BID_LOG_FIELD_IDEMPOTENCY_KEY}}', idem_key_raw)
```

Also write `auction_version` in the final `HSET`, and return it as the last Lua result value. For idempotent retries, return the stored `auction_version` without incrementing it.

- [ ] **Step 5: Add Go constants and parser**

Modify `internal/app/item/cache/bid_log_stream.go`:

```go
const (
	bidLogFieldAuthorityEpoch = "authority_epoch"
	bidLogFieldAuctionVersion = "auction_version"
	bidLogFieldIdempotencyKey = "idempotency_key"
)
```

Read required `authority_epoch` and `auction_version`; read optional `idempotency_key` with empty default.

- [ ] **Step 6: Update bid service Lua args**

Modify `internal/app/item/service/bid_service.go` to pass the current availability epoch and `ready` authority state:

```go
AuthorityEpoch: snapshot.State.Epoch,
AuthorityState: itemcache.AuthorityReady,
IdempotencyKey: input.IdempotencyKey,
```

Add cache constants:

```go
const (
	AuthorityRebuilding = "rebuilding"
	AuthorityReady      = "ready"
	AuthorityProtected  = "protected"
	AuthorityEnded      = "ended"
)
```

- [ ] **Step 7: Run cache and item service tests**

Run:

```bash
rtk go test ./internal/app/item/cache ./internal/app/item/service -count=1
```

Expected: PASS after fake cache and expected Lua result parsing are updated.

- [ ] **Step 8: Commit**

```bash
rtk git add internal/app/item/cache/cache.go internal/app/item/cache/bid.go internal/app/item/cache/bid_log_stream.go internal/app/item/cache/bid_log_stream_test.go internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go
rtk git commit -m "feat: add bid authority versioning"
```

---

### Task 7: Bid Log Persistence Idempotency

**Files:**
- Modify: `internal/app/item/model/bid_log.go`
- Modify: `internal/app/item/dao/bid_log.go`
- Modify: `internal/app/item/service/bid_log_worker.go`
- Modify: `internal/app/item/service/bid_log_worker_test.go`

- [ ] **Step 1: Write failing worker idempotency test**

Modify `internal/app/item/service/bid_log_worker_test.go`:

```go
func TestBidLogWorkerAcksDuplicateAlreadyPersistedLogs(t *testing.T) {
	reader := &fakeBidLogStreamReader{messages: []itemcache.BidLogStreamMessage{{
		ID: "1-0",
		Event: itemcache.BidLogEvent{
			BidID: "bid_1", ItemID: "item_1", RoomID: "room_1", UserID: "user_1",
			Price: 1000, CreatedAtUnixMS: time.Now().UnixMilli(), AuthorityEpoch: 2, AuctionVersion: 1,
		},
	}}}
	store := &fakeBidLogBatchStore{duplicateOK: true}
	worker := newBidLogWorker(store, reader, bidLogWorkerConfig{BatchSize: 1})

	if err := worker.drainOnce(context.Background()); err != nil {
		t.Fatalf("drainOnce() error = %v", err)
	}
	if len(reader.acked) != 1 || reader.acked[0] != "1-0" {
		t.Fatalf("acked = %+v", reader.acked)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
rtk go test ./internal/app/item/service -run TestBidLogWorkerAcksDuplicateAlreadyPersistedLogs -count=1
```

Expected: FAIL until the fake store and worker support idempotent duplicate success.

- [ ] **Step 3: Add bid-log columns and indexes**

Modify `internal/app/item/model/bid_log.go`:

```go
type BidLog struct {
	ID             string    `gorm:"primaryKey;size:64"`
	ItemID         string    `gorm:"index:idx_bid_logs_item_epoch_version,priority:1;index;size:64;not null"`
	RoomID         string    `gorm:"index;size:64;not null"`
	UserID         string    `gorm:"index:idx_bid_logs_item_user_idem,priority:2;index;size:64;not null"`
	Price          int64     `gorm:"not null"`
	AuthorityEpoch int64     `gorm:"index:idx_bid_logs_item_epoch_version,priority:2;not null;default:0"`
	AuctionVersion int64     `gorm:"index:idx_bid_logs_item_epoch_version,priority:3;not null;default:0"`
	IdempotencyKey string    `gorm:"index:idx_bid_logs_item_user_idem,priority:3;size:128"`
	CreatedAt      time.Time
}
```

For MySQL unique indexes, add a migration helper after AutoMigrate:

```go
func (s *GormStore) AutoMigrateBidLog() error {
	if err := s.db.AutoMigrate(&model.BidLog{}); err != nil {
		return err
	}
	if err := s.db.Exec("CREATE UNIQUE INDEX idx_bid_logs_item_epoch_version_unique ON bid_logs (item_id, authority_epoch, auction_version)").Error; err != nil && !isDuplicateIndexError(err) {
		return err
	}
	return nil
}
```

Use `errors.As` with `*mysql.MySQLError` and number `1061` for duplicate index detection.

- [ ] **Step 4: Preserve idempotent batch behavior**

Modify `internal/app/item/dao/bid_log.go` so `CreateBidLogs` remains:

```go
return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&logs).Error
```

This treats repeated `bid_id` or repeated `(item_id, authority_epoch, auction_version)` as successful no-op persistence.

- [ ] **Step 5: Populate new fields in worker**

Modify `internal/app/item/service/bid_log_worker.go`:

```go
logs = append(logs, &itemmodel.BidLog{
	ID:             event.BidID,
	ItemID:         event.ItemID,
	RoomID:         event.RoomID,
	UserID:         event.UserID,
	Price:          event.Price,
	AuthorityEpoch: event.AuthorityEpoch,
	AuctionVersion: event.AuctionVersion,
	IdempotencyKey: event.IdempotencyKey,
	CreatedAt:      time.UnixMilli(event.CreatedAtUnixMS),
})
```

- [ ] **Step 6: Run item tests**

Run:

```bash
rtk go test ./internal/app/item/... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
rtk git add internal/app/item/model/bid_log.go internal/app/item/dao/bid_log.go internal/app/item/service/bid_log_worker.go internal/app/item/service/bid_log_worker_test.go
rtk git commit -m "feat: make bid log persistence idempotent"
```

---

### Task 8: Bid Gate, MySQL Buffering, And Settlement Pause

**Files:**
- Create: `internal/app/item/service/availability_errors.go`
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/service_test.go`
- Modify: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: Write failing bid protection tests**

Modify `internal/app/item/service/bid_service_test.go`:

```go
func TestPlaceBidRejectsWhenControlPlaneInvalid(t *testing.T) {
	svc, current, itemID := newBidServiceFixture(t)
	svc.SetAvailabilitySnapshotForTest(availability.Snapshot{Valid: false, Error: "stale"})

	_, err := svc.PlaceBid(context.Background(), current, itemID, dto.PlaceBidInput{Price: 1200, UserName: "Alice", IdempotencyKey: "idem_1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAvailabilityUnavailable) {
		t.Fatalf("err = %v, want availability unavailable", err)
	}
}

func TestPlaceBidRejectsWhenMySQLBufferingWindowExpired(t *testing.T) {
	svc, current, itemID := newBidServiceFixture(t)
	svc.now = func() time.Time { return time.UnixMilli(1710000011000) }
	svc.SetAvailabilitySnapshotForTest(availability.Snapshot{Valid: true, State: availability.State{
		Version: 1, Mode: availability.ModeMySQLBuffering, Epoch: 4, ActiveRedis: availability.RedisCloud,
		MySQLState: availability.MySQLBuffering, MySQLBufferingStartedAtUnixMS: 1710000000000,
		UpdatedAtUnixMS: 1710000010000, Reason: "mysql_down",
	}})

	_, err := svc.PlaceBid(context.Background(), current, itemID, dto.PlaceBidInput{Price: 1200, UserName: "Alice", IdempotencyKey: "idem_1"})
	if err == nil {
		t.Fatal("expected buffering timeout error")
	}
}
```

- [ ] **Step 2: Write failing settlement pause test**

Modify `internal/app/item/service/service_test.go`:

```go
func TestSettleDueAuctionsPausesWhileMySQLBuffering(t *testing.T) {
	svc := newTestService(t)
	svc.SetAvailabilitySnapshotForTest(availability.Snapshot{Valid: true, State: availability.State{
		Version: 1, Mode: availability.ModeMySQLBuffering, Epoch: 5, ActiveRedis: availability.RedisCloud,
		MySQLState: availability.MySQLBuffering, UpdatedAtUnixMS: time.Now().UnixMilli(),
	}})

	svc.SettleDueAuctions(context.Background())
	if svc.cache.(*fakeCache).settleCalls != 0 {
		t.Fatalf("settle calls = %d, want 0", svc.cache.(*fakeCache).settleCalls)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestPlaceBidRejectsWhen|TestSettleDueAuctionsPauses' -count=1
```

Expected: FAIL because service has no availability gate.

- [ ] **Step 4: Add service availability error and dependency**

Create `internal/app/item/service/availability_errors.go`:

```go
package service

import (
	"net/http"

	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var ErrAvailabilityUnavailable = errorx.New(http.StatusServiceUnavailable, 50301, "auction temporarily unavailable")
```

Modify `internal/app/item/service/service.go`:

```go
type availabilityRuntime interface {
	Snapshot() availability.Snapshot
}

type Service struct {
	store       dao.Store
	policy      dto.AuctionPolicy
	cache       itemcache.Cache
	availability availabilityRuntime
	mysqlBufferingWindow time.Duration
	// existing fields remain
}

func (s *Service) SetAvailability(rt availabilityRuntime, mysqlBufferingWindow time.Duration) {
	s.availability = rt
	if mysqlBufferingWindow <= 0 {
		mysqlBufferingWindow = 10 * time.Second
	}
	s.mysqlBufferingWindow = mysqlBufferingWindow
}

func (s *Service) availabilitySnapshot() availability.Snapshot {
	if s.availability == nil {
		return availability.Snapshot{Valid: true, State: availability.State{Mode: availability.ModeNormalCloud, Epoch: 0, ActiveRedis: availability.RedisCloud, MySQLState: availability.MySQLHealthy}}
	}
	return s.availability.Snapshot()
}
```

- [ ] **Step 5: Gate PlaceBid**

At the beginning of `PlaceBid`, after basic cache nil check:

```go
snapshot := s.availabilitySnapshot()
if !snapshot.Valid {
	bidResult = "rejected"
	bidReason = "control_plane_invalid"
	return nil, ErrAvailabilityUnavailable
}
if snapshot.State.Mode == availability.ModeAuctionProtected {
	bidResult = "rejected"
	bidReason = "auction_protected"
	return nil, ErrAvailabilityUnavailable
}
if snapshot.State.MySQLBufferingExpired(s.now(), s.mysqlBufferingWindow) {
	bidResult = "rejected"
	bidReason = "mysql_buffering_timeout"
	return nil, ErrAvailabilityUnavailable
}
```

Pass `snapshot.State.Epoch` into `BidLuaArgs`.

- [ ] **Step 6: Pause settlement and side effects**

At the start of `SettleDueAuctions` and `EndExpiredAuctions`:

```go
if s.shouldPauseSettlement() {
	logx.Warnw("item.settlement paused by availability state", "mode", s.availabilitySnapshot().State.Mode)
	return
}
```

Add:

```go
func (s *Service) shouldPauseSettlement() bool {
	snapshot := s.availabilitySnapshot()
	if !snapshot.Valid {
		return true
	}
	switch snapshot.State.Mode {
	case availability.ModeMySQLBuffering, availability.ModeAuctionProtected:
		return true
	}
	return snapshot.State.MySQLState == availability.MySQLDown || snapshot.State.MySQLState == availability.MySQLBuffering
}
```

- [ ] **Step 7: Wire service availability in item init**

Modify `internal/app/item/init.go`:

```go
svc.SetAvailability(engine.Availability, engine.Config.MySQLBufferingWindow())
```

- [ ] **Step 8: Run service tests**

Run:

```bash
rtk go test ./internal/app/item/service -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
rtk git add internal/app/item/service/availability_errors.go internal/app/item/service/service.go internal/app/item/service/bid_service.go internal/app/item/service/service_test.go internal/app/item/service/bid_service_test.go internal/app/item/init.go
rtk git commit -m "feat: gate bids and settlement by availability"
```

---

### Task 9: Local Redis Rebuild With Continuity Verification

**Files:**
- Create: `internal/app/item/service/availability_rebuild.go`
- Create: `internal/app/item/service/availability_rebuild_test.go`
- Modify: `internal/app/item/dao/item.go`
- Modify: `internal/app/item/dao/bid_log.go`
- Modify: `internal/app/item/cache/cache.go`

- [ ] **Step 1: Write failing continuity verification tests**

Create `internal/app/item/service/availability_rebuild_test.go`:

```go
package service

import (
	"context"
	"testing"

	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

func TestVerifyBidLogContinuityAcceptsContinuousVersions(t *testing.T) {
	logs := []*itemmodel.BidLog{
		{ID: "bid_1", ItemID: "item_1", UserID: "u1", Price: 1000, AuthorityEpoch: 4, AuctionVersion: 1},
		{ID: "bid_2", ItemID: "item_1", UserID: "u2", Price: 1200, AuthorityEpoch: 4, AuctionVersion: 2},
	}
	result, ok := verifyBidLogContinuity(logs, 4)
	if !ok {
		t.Fatal("expected continuity")
	}
	if result.BidCount != 2 || result.LeaderUserID != "u2" || result.CurrentPrice != 1200 || result.AuctionVersion != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestVerifyBidLogContinuityRejectsGap(t *testing.T) {
	logs := []*itemmodel.BidLog{
		{ID: "bid_1", ItemID: "item_1", UserID: "u1", Price: 1000, AuthorityEpoch: 4, AuctionVersion: 1},
		{ID: "bid_3", ItemID: "item_1", UserID: "u2", Price: 1200, AuthorityEpoch: 4, AuctionVersion: 3},
	}
	_, ok := verifyBidLogContinuity(logs, 4)
	if ok {
		t.Fatal("expected continuity failure")
	}
}

func TestRebuildProtectsItemWhenContinuityFails(t *testing.T) {
	store := newFakeStore()
	cache := newFakeCache()
	worker := newAvailabilityRebuildWorker(store, cache, availabilityRebuildConfig{BatchSize: 10})

	result := worker.rebuildItem(context.Background(), "item_1", 4)
	if result != rebuildProtected {
		t.Fatalf("result = %s, want %s", result, rebuildProtected)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
rtk go test ./internal/app/item/service -run 'TestVerifyBidLogContinuity|TestRebuildProtects' -count=1
```

Expected: FAIL with undefined rebuild functions.

- [ ] **Step 3: Add DAO methods for active items and bid logs**

Modify `internal/app/item/dao/item.go` Store interface:

```go
ListActiveItemsForRebuild(limit int) ([]*model.AuctionItem, error)
ListBidLogsForItemEpoch(itemID string, authorityEpoch int64) ([]*model.BidLog, error)
```

Implement in GORM store:

```go
func (s *GormStore) ListBidLogsForItemEpoch(itemID string, authorityEpoch int64) ([]*model.BidLog, error) {
	var logs []*model.BidLog
	err := s.db.Where("item_id = ? AND authority_epoch = ?", itemID, authorityEpoch).
		Order("auction_version ASC").
		Find(&logs).Error
	return logs, err
}
```

- [ ] **Step 4: Add item authority cache APIs**

Modify `internal/app/item/cache/cache.go` interface:

```go
SetItemAuthority(ctx context.Context, itemID string, epoch int64, state string) error
GetItemAuthority(ctx context.Context, itemID string) (epoch int64, state string, ok bool, err error)
```

Implement in `RedisCache` using keys:

```go
func itemAuthorityKey(itemID string) string {
	return "auction:item:" + itemID + ":authority"
}
```

Store fields `epoch` and `state`.

- [ ] **Step 5: Implement rebuild worker and continuity verification**

Create `internal/app/item/service/availability_rebuild.go`:

```go
package service

import (
	"context"
	"sort"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

type rebuildResult string

const (
	rebuildReady     rebuildResult = "ready"
	rebuildProtected rebuildResult = "protected"
)

type continuityResult struct {
	BidCount       int
	CurrentPrice   int64
	LeaderUserID   string
	AuctionVersion int64
}

func verifyBidLogContinuity(logs []*itemmodel.BidLog, epoch int64) (continuityResult, bool) {
	sort.Slice(logs, func(i, j int) bool { return logs[i].AuctionVersion < logs[j].AuctionVersion })
	var result continuityResult
	for i, log := range logs {
		wantVersion := int64(i + 1)
		if log.AuthorityEpoch != epoch || log.AuctionVersion != wantVersion {
			return continuityResult{}, false
		}
		result.BidCount++
		result.AuctionVersion = log.AuctionVersion
		if log.Price >= result.CurrentPrice {
			result.CurrentPrice = log.Price
			result.LeaderUserID = log.UserID
		}
	}
	return result, true
}

type availabilityRebuildStore interface {
	ListBidLogsForItemEpoch(itemID string, authorityEpoch int64) ([]*itemmodel.BidLog, error)
}

type availabilityRebuildCache interface {
	SetItemAuthority(ctx context.Context, itemID string, epoch int64, state string) error
}

type availabilityRebuildConfig struct {
	BatchSize int
}

type availabilityRebuildWorker struct {
	store availabilityRebuildStore
	cache availabilityRebuildCache
	cfg   availabilityRebuildConfig
}

func newAvailabilityRebuildWorker(store availabilityRebuildStore, cache availabilityRebuildCache, cfg availabilityRebuildConfig) *availabilityRebuildWorker {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	return &availabilityRebuildWorker{store: store, cache: cache, cfg: cfg}
}

func (w *availabilityRebuildWorker) rebuildItem(ctx context.Context, itemID string, epoch int64) rebuildResult {
	logs, err := w.store.ListBidLogsForItemEpoch(itemID, epoch)
	if err != nil {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	if _, ok := verifyBidLogContinuity(logs, epoch); !ok {
		_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
		return rebuildProtected
	}
	_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityReady)
	return rebuildReady
}
```

- [ ] **Step 6: Extend rebuild to write Redis projections**

Add in `rebuildItem` after continuity succeeds:

```go
state := itemcache.AuctionState{
	AuthorityEpoch: epoch,
	AuthorityState: itemcache.AuthorityReady,
	AuctionVersion: continuity.AuctionVersion,
	CurrentPrice: continuity.CurrentPrice,
	DealPrice: continuity.CurrentPrice,
	LeaderUserID: continuity.LeaderUserID,
	BidCount: continuity.BidCount,
}
if err := w.cache.InitAuctionState(ctx, itemID, state); err != nil {
	_ = w.cache.SetItemAuthority(ctx, itemID, epoch, itemcache.AuthorityProtected)
	return rebuildProtected
}
```

- [ ] **Step 7: Run item service tests**

Run:

```bash
rtk go test ./internal/app/item/service ./internal/app/item/cache ./internal/app/item/dao -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
rtk git add internal/app/item/service/availability_rebuild.go internal/app/item/service/availability_rebuild_test.go internal/app/item/dao/item.go internal/app/item/dao/bid_log.go internal/app/item/cache/cache.go
rtk git commit -m "feat: rebuild local redis with continuity checks"
```

---

### Task 10: WebSocket Ticket, Bus, And Presence Local Mode

**Files:**
- Modify: `internal/app/ws/handler/ticket.go`
- Modify: `internal/app/ws/handler/ws.go`
- Modify: `internal/app/ws/init.go`
- Modify: `internal/app/ws/bus/broadcaster.go`
- Modify: `internal/app/ws/bus/subscriber.go`
- Modify: `internal/app/ws/hub/hub.go`
- Test: `internal/app/ws/handler/ticket_test.go`
- Test: `internal/app/ws/hub/hub_test.go`
- Test: `internal/app/ws/bus/bus_test.go`

- [ ] **Step 1: Write failing ticket authority test**

Create `internal/app/ws/handler/ticket_test.go`:

```go
package handler

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
)

type fakeTicketRedis struct {
	setKeys []string
}

func TestTicketStoreUsesActiveRedisAuthority(t *testing.T) {
	local := &fakeTicketRedis{}
	InitTicketStoreForTest(activeTicketStore{
		snapshot: availability.Snapshot{Valid: true, State: availability.State{Epoch: 8, ActiveRedis: availability.RedisLocal}},
		local:    local,
	})

	err := issueTicketForUser(context.Background(), "ticket_1", "user_1", 45*time.Second)
	if err != nil {
		t.Fatalf("issueTicketForUser() error = %v", err)
	}
	if len(local.setKeys) != 1 || local.setKeys[0] != "ws:ticket:8:ticket_1" {
		t.Fatalf("set keys = %+v", local.setKeys)
	}
}

func (_ *fakeTicketRedis) GetDel(context.Context, string) *redis.StringCmd { return redis.NewStringResult("", redis.Nil) }
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
rtk go test ./internal/app/ws/handler -run TestTicketStoreUsesActiveRedisAuthority -count=1
```

Expected: FAIL because ticket store is package-level `*redis.Client`.

- [ ] **Step 3: Introduce active ticket store**

Modify `internal/app/ws/handler/ticket.go`:

Add `net/http` to imports because `ErrTicketAuthorityUnavailable` uses `http.StatusServiceUnavailable`.

```go
type redisStringSetter interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
}

type redisStringGetDeleter interface {
	GetDel(ctx context.Context, key string) *redis.StringCmd
}

type activeRedis interface {
	ActiveRedis() (*redis.Client, availability.Snapshot, bool)
}

var ticketAuthority activeRedis

var ErrTicketAuthorityUnavailable = errorx.New(http.StatusServiceUnavailable, 50303, "websocket ticket authority temporarily unavailable")

func InitTicketAuthority(rt activeRedis) {
	ticketAuthority = rt
}

func ticketKey(epoch int64, ticket string) string {
	return fmt.Sprintf("ws:ticket:%d:%s", epoch, ticket)
}

func issueTicketForUser(ctx context.Context, ticket, userID string, ttl time.Duration) error {
	client, snapshot, ok := ticketAuthority.ActiveRedis()
	if !ok || !snapshot.Valid {
		return ErrTicketAuthorityUnavailable
	}
	return client.Set(ctx, ticketKey(snapshot.State.Epoch, ticket), userID, ttl).Err()
}
```

Update `IssueTicket` to call `issueTicketForUser`.

Modify `internal/app/ws/handler/ws.go` to validate current-epoch tickets:

```go
client, snapshot, ok := ticketAuthority.ActiveRedis()
if !ok || !snapshot.Valid {
	http.Error(w, "ticket authority unavailable", http.StatusServiceUnavailable)
	return
}
key := ticketKey(snapshot.State.Epoch, ticket)
userID, err := client.GetDel(context.Background(), key).Result()
```

- [ ] **Step 4: Route WS bus through active Redis**

Modify `internal/app/ws/bus/broadcaster.go`:

Add imports for `net/http`, `github.com/zet-plane/live-auction-backend/internal/core/availability`, and `github.com/zet-plane/live-auction-backend/pkg/errorx`.

```go
type ActiveRedisProvider interface {
	ActiveRedis() (*redis.Client, availability.Snapshot, bool)
}

var ErrEventBusUnavailable = errorx.New(http.StatusServiceUnavailable, 50302, "websocket event bus temporarily unavailable")

type ActiveRedisPublisher struct {
	provider ActiveRedisProvider
}

func NewActiveRedisPublisher(provider ActiveRedisProvider) *ActiveRedisPublisher {
	return &ActiveRedisPublisher{provider: provider}
}

func (p *ActiveRedisPublisher) Publish(ctx context.Context, channel, payload string) error {
	client, _, ok := p.provider.ActiveRedis()
	if !ok {
		return ErrEventBusUnavailable
	}
	return client.Publish(ctx, channel, payload).Err()
}
```

Modify `internal/app/ws/init.go`:

```go
Hub = bus.NewBroadcaster(bus.NewActiveRedisPublisher(engine.Availability), bus.Options{PodID: podID()})
handler.InitTicketAuthority(engine.Availability)
```

- [ ] **Step 5: Presence degraded semantics**

Modify `internal/app/ws/hub/hub.go` so presence errors set a package-visible status:

```go
var presenceStatus atomic.Value

func init() { presenceStatus.Store("ok") }

func PresenceStatus() string {
	return presenceStatus.Load().(string)
}

func markPresenceDegraded() {
	presenceStatus.Store("degraded")
}
```

Call `markPresenceDegraded()` when `JoinRoom` or `LeaveRoom` fails. Health handler reads `wshub.PresenceStatus()`.

- [ ] **Step 6: Run WS tests**

Run:

```bash
rtk go test ./internal/app/ws/... ./internal/app/base/handler -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
rtk git add internal/app/ws/handler/ticket.go internal/app/ws/handler/ws.go internal/app/ws/init.go internal/app/ws/bus/broadcaster.go internal/app/ws/bus/subscriber.go internal/app/ws/hub/hub.go internal/app/ws/handler/ticket_test.go internal/app/ws/hub/hub_test.go internal/app/ws/bus/bus_test.go internal/app/base/handler/health.go
rtk git commit -m "feat: route websocket through active redis"
```

---

### Task 11: Kubernetes Manifests And Online Test Contracts

**Files:**
- Modify: `deploy/k8s/11-app.yaml`
- Modify: `deploy/k8s/02-configmaps.yaml`
- Modify: `deploy/k8s/01-secrets.example.yaml`
- Modify: `docs/agent-testing/README.md` only if it needs a route to an existing failover guide.
- Create: `docs/agent-testing/guides/failover.md`

- [ ] **Step 1: Update deployment hostPath mount**

Modify `deploy/k8s/11-app.yaml`:

```yaml
          volumeMounts:
            - name: config
              mountPath: /config/config.yaml
              subPath: config.yaml
              readOnly: true
            - name: availability-state
              mountPath: /availability
      volumes:
        - name: config
          secret:
            secretName: app-config
        - name: availability-state
          hostPath:
            path: /var/lib/live-auction/availability
            type: DirectoryOrCreate
```

- [ ] **Step 2: Make Redis wait non-blocking**

Change the `wait-for-redis` initContainer command:

```yaml
              if redis-cli -h redis ping | grep -q PONG; then
                echo "redis reachable"
              else
                echo "redis unavailable at startup; backend will use availability control plane"
              fi
```

Keep MySQL initContainer unchanged because cold-start MySQL failure is outside first-version scope.

- [ ] **Step 3: Add config values**

Modify app config in `deploy/k8s/02-configmaps.yaml` or the secret template source used by this repo:

```yaml
availability:
  state_path: /availability/state.json
  stale_threshold: 5s
  redis_failover_threshold: 3s
  mysql_buffering_window: 10s
  local_redis:
    addr: redis:6379
    password: ""
    db: 0
  rebuild_batch_size: 50
  rebuild_worker_count: 2
  bid_wait_while_rebuilding_min_ms: 100
  bid_wait_while_rebuilding_max_ms: 300
```

- [ ] **Step 4: Write failover test guide**

Create `docs/agent-testing/guides/failover.md`:

```markdown
# Failover Test Guide

Use this guide only after reading `docs/agent-testing/README.md` and `docs/agent-testing/guides/environment.md`.

## Evidence

- Record test batch ID.
- Record created item IDs and room IDs.
- Record `/health` before fault, during fault, and after recovery.
- Record cleanup results.
- Do not record DSNs, credentials, passwords, tokens, or reusable secrets.

## Redis Authority Fault

1. Create a room and an active item for the current batch.
2. Confirm `/health` reports `mode=normal_cloud`.
3. Block backend connectivity to cloud Redis.
4. Confirm backend `/livez` remains `200`.
5. Confirm `/health` reports local Redis mode or protected items.
6. Confirm ready rebuilt items accept bids.
7. Confirm protected items reject bids without taking down the service.
8. Restore cloud Redis connectivity.
9. Confirm switchback waits for verification.

## MySQL Runtime Fault

1. Create a room and an active item for the current batch.
2. Block backend connectivity to MySQL while backend pods are already running.
3. Confirm bids are accepted only within the configured 10-second buffering window.
4. Confirm settlement, order creation, deposit refunds, and final winner confirmation are paused.
5. Restore MySQL.
6. Confirm bid-log backlog drains.
7. Confirm settlement resumes only after item verification.
```

- [ ] **Step 5: Validate manifests locally**

Run:

```bash
rtk go test ./internal/core/availability ./internal/app/item/... ./internal/app/ws/... ./internal/app/base/... -count=1
rtk git diff --check
```

Expected: tests PASS and `git diff --check` prints no whitespace errors.

- [ ] **Step 6: Commit**

```bash
rtk git add deploy/k8s/11-app.yaml deploy/k8s/02-configmaps.yaml deploy/k8s/01-secrets.example.yaml docs/agent-testing/guides/failover.md
rtk git commit -m "deploy: configure availability failover"
```

---

### Task 12: End-To-End Verification Pass

**Files:**
- No new files expected.
- Modify tests discovered failing from integration.

- [ ] **Step 1: Run focused unit suites**

Run:

```bash
rtk go test ./internal/core/availability ./internal/core/cache ./internal/app/base/... ./internal/app/item/... ./internal/app/ws/... -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full unit suite**

Run:

```bash
rtk go test ./...
```

Expected: PASS.

- [ ] **Step 3: Build backend**

Run:

```bash
rtk go build ./...
```

Expected: PASS.

- [ ] **Step 4: Self-review implementation against spec**

Open `docs/superpowers/specs/2026-06-09-local-authority-failover-design.md` and verify every requirement maps to code:

- Process remains alive after runtime Redis/MySQL reachability failure.
- Local Redis is selected only through shared control-plane state.
- Invalid or stale control plane fails closed for bids.
- `auction_version` and `authority_epoch` are persisted in Redis Stream and MySQL.
- Bid-log worker treats duplicates as idempotent success.
- Rebuild protects items when continuity cannot be proven.
- MySQL buffering uses shared timestamp and enforces 10 seconds.
- Settlement and side effects pause until backlog persistence and item verification complete.
- WebSocket ticket, new connections, market-event broadcast, and reconnect snapshots use active Redis.
- Presence degraded status is visible in health and does not block bidding.

- [ ] **Step 5: Commit verification fixes**

If the verification pass required test or code fixes:

```bash
rtk git add <changed-files>
rtk git commit -m "test: verify local authority failover"
```

If no files changed, do not create an empty commit.

---

## Execution Notes

- Use `rtk` for every shell command.
- Keep MySQL cold-start behavior unchanged in first version.
- Do not print or commit DSNs, Redis passwords, kubeconfig contents, webhook tokens, or reusable test credentials.
- Run online/fault-injection tests only through `docs/agent-testing/README.md` and the routed guide.
- Prefer item-level protection over global protection when one item cannot be proven complete.
- Treat Redis Stream pending messages as retryable unless parsing proves the message is malformed.
- Keep the shared control-plane file small; store per-item recovery state in Redis or MySQL.
