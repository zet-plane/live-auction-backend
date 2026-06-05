# WebSocket Connection Stability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce online WebSocket connection tail latency and `dial:EOF` under hot-room split-stream load by adding jittered upgrade simulation, entry-path checks, and lightweight WebSocket observability before changing client behavior.

**Architecture:** Phase 1 validates the strategy in the existing Go performance runner: keep current immediate split mode as the failure baseline, add jittered and priority-jittered upgrade modes, and compare connect P95/P99, EOF, and control arrival. Backend business logic stays unchanged; backend work is limited to low-risk metrics if the runner proves the connection path is the bottleneck.

**Tech Stack:** Go, Gorilla WebSocket, existing agent performance runner, OpenTelemetry metrics, k3s/ingress read-only online checks, Markdown reports.

---

## Source Design

Design spec:

```text
docs/superpowers/specs/2026-06-05-ws-connection-stability-design.md
```

Primary evidence:

```text
docs/agent-testing/reports/20260605-191220-auction-sync-enhanced-diagnosis.md
```

The diagnosis this plan acts on:

- Server-side `ws_time_sync_write_lag_p95` is about 4.7ms and not the bottleneck.
- Runner read-loop processing is microsecond-level and not the bottleneck.
- 600 physical WS connections show `WS_CONNECT_P95/P99` around 5.3s and `dial:EOF`.
- The first implementation step must therefore validate connection smoothing in the runner before changing backend core behavior.

## File Structure

- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
  - Add WS upgrade config fields.
  - Add immediate/jittered/priority-jittered target planning.
  - Add split-upgrade sequencing for runner-side connection smoothing.
  - Add summary labels for upgrade mode and jittered connection settings.
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`
  - Add TDD coverage for config parsing, jitter validation, target planning, upgrade ordering, and output labels.
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/README.md`
  - Document new env vars and recommended online regression matrix.
- Create: `docs/agent-testing/reports/20260605-auction-ws-connection-stability.md`
  - Written after the approved online rerun, not during implementation.
- Optional modify after runner validation: `internal/core/observability/metrics.go`
  - Add WS upgrade/close-code metrics only if the runner shows connection smoothing helps but more server-side visibility is still needed.
- Optional modify after runner validation: `internal/app/ws/hub/hub.go`, `internal/app/ws/hub/conn.go`, `internal/app/ws/hub/hub_test.go`
  - Record stream-aware connection accept/close metrics.

## Task 1: Add Runner Upgrade Mode Config

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`

- [ ] **Step 1: Write failing config parsing tests**

Add these tests to `main_test.go` near the existing config/env tests:

```go
func TestLoadConfigParsesWSUpgradeModeAndJitterConfig(t *testing.T) {
	t.Setenv("PERF_WS_UPGRADE_MODE", "priority_jittered")
	t.Setenv("PERF_WS_CONTROL_JITTER_MIN", "100ms")
	t.Setenv("PERF_WS_CONTROL_JITTER_MAX", "1500ms")
	t.Setenv("PERF_WS_MARKET_JITTER_MIN", "500ms")
	t.Setenv("PERF_WS_MARKET_JITTER_MAX", "3s")
	t.Setenv("PERF_WS_UPGRADE_BATCH_SIZE", "20")
	t.Setenv("PERF_WS_UPGRADE_BATCH_INTERVAL", "1s")

	cfg := loadConfig()

	if cfg.WSUpgradeMode != wsUpgradeModePriorityJittered {
		t.Fatalf("WSUpgradeMode = %q, want %q", cfg.WSUpgradeMode, wsUpgradeModePriorityJittered)
	}
	if cfg.WSControlJitterMin != 100*time.Millisecond || cfg.WSControlJitterMax != 1500*time.Millisecond {
		t.Fatalf("control jitter = %s..%s", cfg.WSControlJitterMin, cfg.WSControlJitterMax)
	}
	if cfg.WSMarketJitterMin != 500*time.Millisecond || cfg.WSMarketJitterMax != 3*time.Second {
		t.Fatalf("market jitter = %s..%s", cfg.WSMarketJitterMin, cfg.WSMarketJitterMax)
	}
	if cfg.WSUpgradeBatchSize != 20 {
		t.Fatalf("WSUpgradeBatchSize = %d, want 20", cfg.WSUpgradeBatchSize)
	}
	if cfg.WSUpgradeBatchInterval != time.Second {
		t.Fatalf("WSUpgradeBatchInterval = %s, want 1s", cfg.WSUpgradeBatchInterval)
	}
}

func TestLoadConfigDefaultsToImmediateUpgradeMode(t *testing.T) {
	cfg := loadConfig()

	if cfg.WSUpgradeMode != wsUpgradeModeImmediate {
		t.Fatalf("WSUpgradeMode = %q, want %q", cfg.WSUpgradeMode, wsUpgradeModeImmediate)
	}
	if cfg.WSControlJitterMin != 0 || cfg.WSControlJitterMax != 0 {
		t.Fatalf("default control jitter = %s..%s, want zero", cfg.WSControlJitterMin, cfg.WSControlJitterMax)
	}
	if cfg.WSMarketJitterMin != 0 || cfg.WSMarketJitterMax != 0 {
		t.Fatalf("default market jitter = %s..%s, want zero", cfg.WSMarketJitterMin, cfg.WSMarketJitterMax)
	}
	if cfg.WSUpgradeBatchSize != 0 {
		t.Fatalf("default WSUpgradeBatchSize = %d, want 0", cfg.WSUpgradeBatchSize)
	}
	if cfg.WSUpgradeBatchInterval != 0 {
		t.Fatalf("default WSUpgradeBatchInterval = %s, want zero", cfg.WSUpgradeBatchInterval)
	}
}

func TestLoadConfigNormalizesBadWSUpgradeModeToImmediate(t *testing.T) {
	t.Setenv("PERF_WS_UPGRADE_MODE", "surprise")

	cfg := loadConfig()

	if cfg.WSUpgradeMode != wsUpgradeModeImmediate {
		t.Fatalf("WSUpgradeMode = %q, want %q", cfg.WSUpgradeMode, wsUpgradeModeImmediate)
	}
}

func TestNormalizeDurationRangeSwapsInvertedRange(t *testing.T) {
	min, max := normalizeDurationRange(3*time.Second, 500*time.Millisecond)

	if min != 500*time.Millisecond || max != 3*time.Second {
		t.Fatalf("range = %s..%s, want 500ms..3s", min, max)
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -run 'TestLoadConfigParsesWSUpgradeModeAndJitterConfig|TestLoadConfigDefaultsToImmediateUpgradeMode|TestLoadConfigNormalizesBadWSUpgradeModeToImmediate|TestNormalizeDurationRangeSwapsInvertedRange' -count=1
```

Expected: FAIL because `WSUpgradeMode`, jitter fields, and `normalizeDurationRange` do not exist.

- [ ] **Step 3: Add config fields and constants**

In `main.go`, extend constants:

```go
const (
	// PERF_WS_STREAM_MODE is a regression-run override. Product clients should
	// support millisecond sync by default and switch streams automatically.
	wsStreamModeAll           = "all"
	wsStreamModeControlMarket = "control_market"

	wsUpgradeModeImmediate        = "immediate"
	wsUpgradeModeJittered         = "jittered"
	wsUpgradeModePriorityJittered = "priority_jittered"
)
```

Extend `Config`:

```go
type Config struct {
	BatchID                string
	Environment            string
	BaseURL                string
	PrometheusURL          string
	StopFile               string
	HumanMonitor           string
	RequestTimeout         time.Duration
	WSConnectTimeout       time.Duration
	WSConnectConcurrency   int
	WSConnectMaxAttempts   int
	WSStreamMode           string
	WSUpgradeMode          string
	WSControlJitterMin     time.Duration
	WSControlJitterMax     time.Duration
	WSMarketJitterMin      time.Duration
	WSMarketJitterMax      time.Duration
	WSUpgradeBatchSize     int
	WSUpgradeBatchInterval time.Duration
	ObservabilityStep      time.Duration
	UserCount              int
	CleanupOnly            bool
	Stages                 []StageConfig
}
```

- [ ] **Step 4: Parse env vars in `loadConfig`**

Inside `loadConfig`, compute jitter ranges before returning `Config`:

```go
	controlJitterMin, controlJitterMax := normalizeDurationRange(
		envDuration("PERF_WS_CONTROL_JITTER_MIN", 0),
		envDuration("PERF_WS_CONTROL_JITTER_MAX", 0),
	)
	marketJitterMin, marketJitterMax := normalizeDurationRange(
		envDuration("PERF_WS_MARKET_JITTER_MIN", 0),
		envDuration("PERF_WS_MARKET_JITTER_MAX", 0),
	)
```

Then add fields in the returned `Config`:

```go
		WSStreamMode:           getenv("PERF_WS_STREAM_MODE", wsStreamModeAll),
		WSUpgradeMode:          normalizeWSUpgradeMode(getenv("PERF_WS_UPGRADE_MODE", wsUpgradeModeImmediate)),
		WSControlJitterMin:     controlJitterMin,
		WSControlJitterMax:     controlJitterMax,
		WSMarketJitterMin:      marketJitterMin,
		WSMarketJitterMax:      marketJitterMax,
		WSUpgradeBatchSize:     envInt("PERF_WS_UPGRADE_BATCH_SIZE", 0),
		WSUpgradeBatchInterval: envDuration("PERF_WS_UPGRADE_BATCH_INTERVAL", 0),
```

Add helpers near env parsing helpers:

```go
func normalizeWSUpgradeMode(raw string) string {
	switch strings.TrimSpace(raw) {
	case wsUpgradeModeJittered:
		return wsUpgradeModeJittered
	case wsUpgradeModePriorityJittered:
		return wsUpgradeModePriorityJittered
	default:
		return wsUpgradeModeImmediate
	}
}

func normalizeDurationRange(min, max time.Duration) (time.Duration, time.Duration) {
	if min < 0 {
		min = 0
	}
	if max < 0 {
		max = 0
	}
	if min > max {
		return max, min
	}
	return min, max
}
```

- [ ] **Step 5: Run tests and verify GREEN**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -run 'TestLoadConfigParsesWSUpgradeModeAndJitterConfig|TestLoadConfigDefaultsToImmediateUpgradeMode|TestLoadConfigNormalizesBadWSUpgradeModeToImmediate|TestNormalizeDurationRangeSwapsInvertedRange' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
rtk git add docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go
rtk git commit -m "test: add ws upgrade mode config"
```

## Task 2: Add Deterministic Jittered Target Planning

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`

- [ ] **Step 1: Write failing tests for target waves**

Add to `main_test.go`:

```go
func TestWSConnectWavesImmediateKeepsExistingSplitShape(t *testing.T) {
	cfg := Config{WSStreamMode: wsStreamModeControlMarket, WSUpgradeMode: wsUpgradeModeImmediate}
	data := &TestData{UserTokens: make([]string, 3)}

	waves := wsConnectWaves(cfg, data, 3)

	if len(waves) != 1 {
		t.Fatalf("expected one immediate wave, got %#v", waves)
	}
	if waves[0].Delay != 0 {
		t.Fatalf("immediate wave delay = %s, want zero", waves[0].Delay)
	}
	want := []wsConnectTarget{
		{UserIndex: 0, Stream: "control"},
		{UserIndex: 0, Stream: "market"},
		{UserIndex: 1, Stream: "control"},
		{UserIndex: 1, Stream: "market"},
		{UserIndex: 2, Stream: "control"},
		{UserIndex: 2, Stream: "market"},
	}
	if !reflect.DeepEqual(waves[0].Targets, want) {
		t.Fatalf("targets = %#v, want %#v", waves[0].Targets, want)
	}
}

func TestWSConnectWavesJitteredControlsBeforeMarkets(t *testing.T) {
	cfg := Config{
		WSStreamMode:           wsStreamModeControlMarket,
		WSUpgradeMode:          wsUpgradeModeJittered,
		WSUpgradeBatchSize:     2,
		WSUpgradeBatchInterval: 500 * time.Millisecond,
		WSControlJitterMin:     100 * time.Millisecond,
		WSControlJitterMax:     100 * time.Millisecond,
		WSMarketJitterMin:      time.Second,
		WSMarketJitterMax:      time.Second,
	}
	data := &TestData{UserTokens: make([]string, 3)}

	waves := wsConnectWaves(cfg, data, 3)

	if len(waves) != 4 {
		t.Fatalf("expected 4 waves, got %#v", waves)
	}
	if got := targetsStreams(waves[0].Targets); !reflect.DeepEqual(got, []string{"control", "control"}) {
		t.Fatalf("wave 0 streams = %#v", got)
	}
	if got := targetsStreams(waves[1].Targets); !reflect.DeepEqual(got, []string{"control"}) {
		t.Fatalf("wave 1 streams = %#v", got)
	}
	if got := targetsStreams(waves[2].Targets); !reflect.DeepEqual(got, []string{"market", "market"}) {
		t.Fatalf("wave 2 streams = %#v", got)
	}
	if got := targetsStreams(waves[3].Targets); !reflect.DeepEqual(got, []string{"market"}) {
		t.Fatalf("wave 3 streams = %#v", got)
	}
	if waves[0].Delay != 100*time.Millisecond || waves[1].Delay != 500*time.Millisecond+100*time.Millisecond {
		t.Fatalf("control delays = %s, %s", waves[0].Delay, waves[1].Delay)
	}
	if waves[2].Delay != time.Second || waves[3].Delay != 500*time.Millisecond+time.Second {
		t.Fatalf("market delays = %s, %s", waves[2].Delay, waves[3].Delay)
	}
}

func TestWSConnectWavesPriorityJitteredPrioritizesFirstTwentyPercent(t *testing.T) {
	cfg := Config{
		WSStreamMode:           wsStreamModeControlMarket,
		WSUpgradeMode:          wsUpgradeModePriorityJittered,
		WSUpgradeBatchSize:     2,
		WSUpgradeBatchInterval: time.Second,
	}
	data := &TestData{UserTokens: make([]string, 10)}

	waves := wsConnectWaves(cfg, data, 10)

	if len(waves) == 0 {
		t.Fatal("expected waves")
	}
	firstUsers := []int{waves[0].Targets[0].UserIndex, waves[0].Targets[1].UserIndex}
	if !reflect.DeepEqual(firstUsers, []int{0, 1}) {
		t.Fatalf("first priority users = %#v, want [0 1]", firstUsers)
	}
}

func targetsStreams(targets []wsConnectTarget) []string {
	streams := make([]string, 0, len(targets))
	for _, target := range targets {
		streams = append(streams, target.Stream)
	}
	return streams
}
```

Add `reflect` to the test imports.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -run 'TestWSConnectWavesImmediateKeepsExistingSplitShape|TestWSConnectWavesJitteredControlsBeforeMarkets|TestWSConnectWavesPriorityJitteredPrioritizesFirstTwentyPercent' -count=1
```

Expected: FAIL because `wsConnectWaves` and `wsConnectWave` do not exist.

- [ ] **Step 3: Add wave types and deterministic planning**

In `main.go`, add after `wsConnectTarget`:

```go
type wsConnectWave struct {
	Name    string
	Delay   time.Duration
	Targets []wsConnectTarget
}
```

Add planning helpers near `wsMissingTargets`:

```go
func wsConnectWaves(cfg Config, data *TestData, target int) []wsConnectWave {
	ensureWSConnectionMaps(data)
	streams := wsTargetStreams(cfg)
	pending := wsMissingTargets(data, minInt(target, len(data.UserTokens)), streams)
	if len(pending) == 0 {
		return nil
	}
	if cfg.WSStreamMode != wsStreamModeControlMarket || cfg.WSUpgradeMode == wsUpgradeModeImmediate {
		return []wsConnectWave{{Name: "immediate", Targets: pending}}
	}
	return splitUpgradeWaves(cfg, pending)
}

func splitUpgradeWaves(cfg Config, pending []wsConnectTarget) []wsConnectWave {
	controls := filterWSTargetsByStream(pending, "control")
	markets := filterWSTargetsByStream(pending, "market")
	if cfg.WSUpgradeMode == wsUpgradeModePriorityJittered {
		controls = priorityOrderWSTargets(controls)
		markets = priorityOrderWSTargets(markets)
	}
	batchSize := cfg.WSUpgradeBatchSize
	if batchSize <= 0 {
		batchSize = len(pending)
	}
	waves := make([]wsConnectWave, 0)
	waves = append(waves, wsBatchedWaves("control", controls, batchSize, cfg.WSUpgradeBatchInterval, cfg.WSControlJitterMin)...)
	waves = append(waves, wsBatchedWaves("market", markets, batchSize, cfg.WSUpgradeBatchInterval, cfg.WSMarketJitterMin)...)
	return waves
}

func wsBatchedWaves(name string, targets []wsConnectTarget, batchSize int, interval time.Duration, baseDelay time.Duration) []wsConnectWave {
	if len(targets) == 0 {
		return nil
	}
	if batchSize <= 0 || batchSize > len(targets) {
		batchSize = len(targets)
	}
	waves := make([]wsConnectWave, 0, (len(targets)+batchSize-1)/batchSize)
	for start := 0; start < len(targets); start += batchSize {
		end := start + batchSize
		if end > len(targets) {
			end = len(targets)
		}
		waves = append(waves, wsConnectWave{
			Name:    name,
			Delay:   baseDelay + time.Duration(len(waves))*interval,
			Targets: append([]wsConnectTarget(nil), targets[start:end]...),
		})
	}
	return waves
}

func filterWSTargetsByStream(targets []wsConnectTarget, stream string) []wsConnectTarget {
	out := make([]wsConnectTarget, 0, len(targets))
	for _, target := range targets {
		if target.Stream == stream {
			out = append(out, target)
		}
	}
	return out
}

func priorityOrderWSTargets(targets []wsConnectTarget) []wsConnectTarget {
	out := append([]wsConnectTarget(nil), targets...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UserIndex < out[j].UserIndex
	})
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

This uses deterministic `baseDelay` in tests. Random jitter is added in Task 3 before sleeping; the planning function stays deterministic.

- [ ] **Step 4: Run tests and verify GREEN**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -run 'TestWSConnectWavesImmediateKeepsExistingSplitShape|TestWSConnectWavesJitteredControlsBeforeMarkets|TestWSConnectWavesPriorityJitteredPrioritizesFirstTwentyPercent' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go
rtk git commit -m "feat: plan jittered ws connect waves"
```

## Task 3: Execute Jittered Waves In The Runner

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`

- [ ] **Step 1: Write failing tests for ordered execution**

Add to `main_test.go`:

```go
func TestEnsureWSConnectionsJitteredConnectsControlsBeforeMarkets(t *testing.T) {
	var mu sync.Mutex
	var order []wsConnectTarget
	cfg := Config{
		WSStreamMode:           wsStreamModeControlMarket,
		WSUpgradeMode:          wsUpgradeModeJittered,
		WSConnectConcurrency:   4,
		WSConnectMaxAttempts:   20,
		WSUpgradeBatchSize:     2,
		WSUpgradeBatchInterval: 0,
	}
	data := &TestData{UserTokens: make([]string, 3)}

	report := ensureWSConnectionsWith(context.Background(), cfg, data, 3, func(ctx context.Context, cfg Config, data *TestData, userIndex int, stream string) (*websocketConn, error) {
		mu.Lock()
		order = append(order, wsConnectTarget{UserIndex: userIndex, Stream: stream})
		mu.Unlock()
		return nil, nil
	})

	if report.Failures != 0 {
		t.Fatalf("expected no failures, got %d", report.Failures)
	}
	streams := targetsStreams(order)
	controlDone := false
	for _, stream := range streams {
		if stream == "market" {
			controlDone = true
		}
		if controlDone && stream == "control" {
			t.Fatalf("control appeared after market in order %#v", order)
		}
	}
	if got := wsStreamConnectedCount(data, "control"); got != 3 {
		t.Fatalf("control connected = %d, want 3", got)
	}
	if got := wsStreamConnectedCount(data, "market"); got != 3 {
		t.Fatalf("market connected = %d, want 3", got)
	}
}

func TestEnsureWSConnectionsStopsJitteredAfterRetryBudget(t *testing.T) {
	cfg := Config{
		WSStreamMode:           wsStreamModeControlMarket,
		WSUpgradeMode:          wsUpgradeModeJittered,
		WSConnectConcurrency:   2,
		WSConnectMaxAttempts:   3,
		WSUpgradeBatchSize:     1,
		WSUpgradeBatchInterval: 0,
	}
	data := &TestData{UserTokens: make([]string, 3)}
	var attempts atomic.Int64

	report := ensureWSConnectionsWith(context.Background(), cfg, data, 3, func(ctx context.Context, cfg Config, data *TestData, userIndex int, stream string) (*websocketConn, error) {
		attempts.Add(1)
		return nil, errors.New("dial failed")
	})

	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
	if report.Failures != 3 {
		t.Fatalf("failures = %d, want 3", report.Failures)
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -run 'TestEnsureWSConnectionsJitteredConnectsControlsBeforeMarkets|TestEnsureWSConnectionsStopsJitteredAfterRetryBudget' -count=1
```

Expected: FAIL because `ensureWSConnectionsWith` still flattens all pending targets and does not honor jittered waves.

- [ ] **Step 3: Route `ensureWSConnectionsWith` through waves**

Replace the body of `ensureWSConnectionsWith` with:

```go
func ensureWSConnectionsWith(ctx context.Context, cfg Config, data *TestData, target int, connector wsConnector) wsConnectReport {
	ensureWSConnectionMaps(data)
	streams := wsTargetStreams(cfg)
	if target <= 0 {
		return wsConnectReport{Errors: map[string]int64{}}
	}
	report := wsConnectReport{Errors: map[string]int64{}}
	if target > len(data.UserTokens) {
		excess := (target - len(data.UserTokens)) * len(streams)
		report.Failures += excess
		report.Errors["target_exceeds_user_count"] += int64(excess)
		target = len(data.UserTokens)
	}
	maxAttempts := cfg.WSConnectMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = target * len(streams) * 2
	}
	attempts := 0
	for attempts < maxAttempts {
		pending := wsMissingTargets(data, target, streams)
		if len(pending) == 0 || stopRequested(cfg.StopFile) {
			break
		}
		waves := wsConnectWaves(cfg, data, target)
		if len(waves) == 0 {
			break
		}
		progress := false
		for _, wave := range waves {
			if attempts >= maxAttempts || stopRequested(cfg.StopFile) {
				break
			}
			waitWSConnectWave(ctx, wave.Delay)
			remaining := maxAttempts - attempts
			attemptList := filterStillMissingWSTargets(data, wave.Targets)
			if len(attemptList) == 0 {
				continue
			}
			if len(attemptList) > remaining {
				attemptList = attemptList[:remaining]
			}
			attempts += len(attemptList)
			batchReport := connectWSBatch(ctx, cfg, data, attemptList, connector)
			report.Failures += batchReport.Failures
			report.Durations = append(report.Durations, batchReport.Durations...)
			for reason, count := range batchReport.Errors {
				report.Errors[reason] += count
			}
			if len(batchReport.FailedTargets) < len(attemptList) {
				progress = true
			}
		}
		if !progress && cfg.WSUpgradeMode != wsUpgradeModeImmediate {
			continue
		}
	}
	return report
}
```

Add helpers:

```go
func waitWSConnectWave(ctx context.Context, delay time.Duration) {
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func filterStillMissingWSTargets(data *TestData, targets []wsConnectTarget) []wsConnectTarget {
	out := make([]wsConnectTarget, 0, len(targets))
	for _, target := range targets {
		if !wsTargetConnected(data, target) {
			out = append(out, target)
		}
	}
	return out
}

func wsTargetConnected(data *TestData, target wsConnectTarget) bool {
	ensureWSConnectionMaps(data)
	if data.WSStreamUsers[target.Stream] == nil {
		return false
	}
	return data.WSStreamUsers[target.Stream][target.UserIndex]
}
```

- [ ] **Step 4: Run tests and verify GREEN**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -run 'TestEnsureWSConnectionsJitteredConnectsControlsBeforeMarkets|TestEnsureWSConnectionsStopsJitteredAfterRetryBudget|TestEnsureWSConnectionsUsesControlAndMarketStreamsInSplitMode|TestEnsureWSConnectionsStopsAtRetryBudget' -count=1
```

Expected: PASS.

- [ ] **Step 5: Run full runner package tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
rtk git add docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go
rtk git commit -m "feat: execute jittered ws upgrade waves"
```

## Task 4: Add Output Labels And Runner Documentation

**Files:**
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/README.md`

- [ ] **Step 1: Write failing summary label test**

Add to `main_test.go` near `TestPrintStageSummaryLabelsClientE2ELatency`:

```go
func TestPrintPlanIncludesWSUpgradeConfig(t *testing.T) {
	cfg := Config{
		BatchID:                "batch",
		Environment:            "single_source_online",
		BaseURL:                "https://example.test",
		WSConnectConcurrency:   8,
		WSConnectTimeout:       15 * time.Second,
		WSConnectMaxAttempts:   760,
		WSStreamMode:           wsStreamModeControlMarket,
		WSUpgradeMode:          wsUpgradeModeJittered,
		WSControlJitterMin:     100 * time.Millisecond,
		WSControlJitterMax:     1500 * time.Millisecond,
		WSMarketJitterMin:      500 * time.Millisecond,
		WSMarketJitterMax:      3 * time.Second,
		WSUpgradeBatchSize:     20,
		WSUpgradeBatchInterval: time.Second,
	}

	out := captureStdout(func() {
		printPlan(cfg)
	})

	for _, want := range []string{
		"WS_UPGRADE_MODE: jittered",
		"WS_CONTROL_JITTER: 100ms..1.5s",
		"WS_MARKET_JITTER: 500ms..3s",
		"WS_UPGRADE_BATCH: size=20 interval=1s",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -run TestPrintPlanIncludesWSUpgradeConfig -count=1
```

Expected: FAIL because `printPlan` does not print upgrade config.

- [ ] **Step 3: Print upgrade config**

In `printPlan`, after `WS_STREAM_MODE`, add:

```go
	fmt.Printf("  WS_UPGRADE_MODE: %s\n", cfg.WSUpgradeMode)
	fmt.Printf("  WS_CONTROL_JITTER: %s..%s\n", cfg.WSControlJitterMin, cfg.WSControlJitterMax)
	fmt.Printf("  WS_MARKET_JITTER: %s..%s\n", cfg.WSMarketJitterMin, cfg.WSMarketJitterMax)
	fmt.Printf("  WS_UPGRADE_BATCH: size=%d interval=%s\n", cfg.WSUpgradeBatchSize, cfg.WSUpgradeBatchInterval)
```

- [ ] **Step 4: Document env vars**

Add this section to `README.md`:

```markdown
## WebSocket Upgrade Smoothing

The runner can preserve the original immediate split-stream behavior or simulate a smoother client upgrade.

| Env | Default | Meaning |
| --- | --- | --- |
| `PERF_WS_UPGRADE_MODE` | `immediate` | `immediate`, `jittered`, or `priority_jittered` |
| `PERF_WS_CONTROL_JITTER_MIN` | `0` | Minimum delay before a control wave |
| `PERF_WS_CONTROL_JITTER_MAX` | `0` | Reserved upper bound for random jitter; deterministic tests use the minimum |
| `PERF_WS_MARKET_JITTER_MIN` | `0` | Minimum delay before a market wave |
| `PERF_WS_MARKET_JITTER_MAX` | `0` | Reserved upper bound for random jitter; deterministic tests use the minimum |
| `PERF_WS_UPGRADE_BATCH_SIZE` | `0` | Users per connection wave; `0` means one wave |
| `PERF_WS_UPGRADE_BATCH_INTERVAL` | `0` | Delay between waves of the same stream |

Recommended online comparison:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=immediate \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=jittered \
  PERF_WS_CONTROL_JITTER_MIN=100ms \
  PERF_WS_CONTROL_JITTER_MAX=1500ms \
  PERF_WS_MARKET_JITTER_MIN=500ms \
  PERF_WS_MARKET_JITTER_MAX=3s \
  PERF_WS_UPGRADE_BATCH_SIZE=20 \
  PERF_WS_UPGRADE_BATCH_INTERVAL=1s \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```
```

- [ ] **Step 5: Run tests and verify GREEN**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -run TestPrintPlanIncludesWSUpgradeConfig -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
rtk git add docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/README.md
rtk git commit -m "docs: document ws upgrade smoothing runner"
```

## Task 5: Run Local Verification Before Online Approval

**Files:**
- Modify only if tests reveal defects:
  - `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
  - `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go`

- [ ] **Step 1: Run focused runner tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader package tests touched by WS split stream**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/ws/... ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

Expected: PASS.

- [ ] **Step 3: Scan generated docs and runner files for online secrets**

Run:

```bash
rtk rg -n "115\\.|deploy@|ws://|wss://|mysql://|redis://|Authorization:|Bearer [A-Za-z0-9._-]+" docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws docs/superpowers/specs/2026-06-05-ws-connection-stability-design.md docs/superpowers/plans/2026-06-05-ws-connection-stability.md
```

Expected: no real online addresses, tokens, DSNs, or WebSocket query strings. Environment variable names such as `PERF_BASE_URL` are acceptable.

- [ ] **Step 4: Commit fixes if needed**

If Step 1 or Step 2 fails, fix only the failing runner or test code, then run:

```bash
rtk git add docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main_test.go docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/README.md
rtk git commit -m "fix: stabilize ws upgrade runner tests"
```

If Step 1 and Step 2 pass without fixes, do not create an empty commit.

## Task 6: Approved Online Regression Matrix

**Files:**
- Create: `docs/agent-testing/reports/20260605-auction-ws-connection-stability.md`
- Modify: `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/evidence-redacted.md`
- Modify: `docs/superpowers/plans/2026-06-05-ws-connection-stability.md`

This task uses online dependencies. It requires fresh explicit user approval under `docs/agent-testing` before execution.

- [ ] **Step 1: State approval scope before running**

Before any online command, write this scope in the conversation:

```text
Route: docs/agent-testing/README.md -> guides/runner.md -> guides/environment.md -> performance runner.
Dependencies: online HTTP, online WebSocket, online Prometheus/kubectl read-only checks.
Data: unique batch ids created by runner for the current test only.
Cleanup: runner cleanup must close WS, cancel item, end room, and attempt test-user deletion.
Report: write a redacted report under docs/agent-testing/reports and append evidence-redacted.md.
```

- [ ] **Step 2: Run immediate baseline**

After approval, run the current failure baseline with a unique batch id:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_ws_conn_stability_20260605_immediate \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL="$PERF_ONLINE_BASE_URL" \
  PERF_PROMETHEUS_URL="$PERF_ONLINE_PROMETHEUS_URL" \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  PERF_REQUEST_MIX=core_bid_80_ranking_10_item_10 \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=immediate \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=760 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

Expected:

- Runner completes reconcile and cleanup.
- Report captures `WS_CONNECT_P95/P99`, `dial:EOF`, control arrival P95/P99, interval P95/P99, Prometheus server write lag, and backend restarts.

- [ ] **Step 3: Run jittered comparison**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_ws_conn_stability_20260605_jittered \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL="$PERF_ONLINE_BASE_URL" \
  PERF_PROMETHEUS_URL="$PERF_ONLINE_PROMETHEUS_URL" \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  PERF_REQUEST_MIX=core_bid_80_ranking_10_item_10 \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=jittered \
  PERF_WS_CONTROL_JITTER_MIN=100ms \
  PERF_WS_CONTROL_JITTER_MAX=1500ms \
  PERF_WS_MARKET_JITTER_MIN=500ms \
  PERF_WS_MARKET_JITTER_MAX=3s \
  PERF_WS_UPGRADE_BATCH_SIZE=20 \
  PERF_WS_UPGRADE_BATCH_INTERVAL=1s \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=760 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

Expected:

- `WS_CONNECT_P95/P99` and `dial:EOF` improve versus immediate baseline.
- control arrival P95/P99 improves or stays no worse.
- `ws_time_sync_write_lag_p95` remains below 20ms.

- [ ] **Step 4: Run priority-jittered comparison**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_ws_conn_stability_20260605_priority_jittered \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL="$PERF_ONLINE_BASE_URL" \
  PERF_PROMETHEUS_URL="$PERF_ONLINE_PROMETHEUS_URL" \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  PERF_REQUEST_MIX=core_bid_80_ranking_10_item_10 \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=priority_jittered \
  PERF_WS_CONTROL_JITTER_MIN=100ms \
  PERF_WS_CONTROL_JITTER_MAX=1500ms \
  PERF_WS_MARKET_JITTER_MIN=500ms \
  PERF_WS_MARKET_JITTER_MAX=3s \
  PERF_WS_UPGRADE_BATCH_SIZE=20 \
  PERF_WS_UPGRADE_BATCH_INTERVAL=1s \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=760 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

Expected:

- At minimum, connection stability is no worse than `jittered`.
- If priority users are later modeled with real user roles, they receive control first. In this runner phase, lower user indexes are the deterministic priority stand-in.

- [ ] **Step 5: Run item-only jittered control**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache \
  PERF_BATCH_ID=agent_ws_conn_stability_20260605_item_only_jittered \
  PERF_ENVIRONMENT=single_source_online \
  PERF_BASE_URL="$PERF_ONLINE_BASE_URL" \
  PERF_PROMETHEUS_URL="$PERF_ONLINE_PROMETHEUS_URL" \
  PERF_STAGE_QPS=70 \
  PERF_STAGE_WS=300 \
  PERF_USER_COUNT=340 \
  PERF_REQUEST_MIX=item_only \
  PERF_WS_STREAM_MODE=control_market \
  PERF_WS_UPGRADE_MODE=jittered \
  PERF_WS_CONTROL_JITTER_MIN=100ms \
  PERF_WS_CONTROL_JITTER_MAX=1500ms \
  PERF_WS_MARKET_JITTER_MIN=500ms \
  PERF_WS_MARKET_JITTER_MAX=3s \
  PERF_WS_UPGRADE_BATCH_SIZE=20 \
  PERF_WS_UPGRADE_BATCH_INTERVAL=1s \
  PERF_REQUEST_TIMEOUT=15s \
  PERF_WS_CONNECT_CONCURRENCY=8 \
  PERF_WS_CONNECT_TIMEOUT=15s \
  PERF_WS_CONNECT_MAX_ATTEMPTS=760 \
  go run docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go
```

Expected:

- Establishes connection-path behavior without bid fanout.
- If item-only is still high, the leading cause remains entry/transport/runner path rather than market fanout.

- [ ] **Step 6: Collect strict online log and resource snapshot**

Run read-only checks:

```bash
rtk ssh "$PERF_ONLINE_SSH_TARGET" "kubectl logs -n live-auction deployment/live-auction-backend --since=20m | grep -iE '(^|[^[:alpha:]])(panic|fatal|oom|killed)([^[:alpha:]]|$)' | wc -l"
rtk ssh "$PERF_ONLINE_SSH_TARGET" "kubectl get pods -n live-auction -o wide"
rtk ssh "$PERF_ONLINE_SSH_TARGET" "kubectl top nodes"
rtk ssh "$PERF_ONLINE_SSH_TARGET" "kubectl top pods -n live-auction"
```

Expected:

- Strict marker count is 0.
- backend Ready/Running, restart count unchanged.
- Resource snapshot does not show severe CPU/memory pressure.

Do not paste the real online host, tokens, DSNs, or full WebSocket query strings into the report.

- [ ] **Step 7: Write redacted report**

Create:

```text
docs/agent-testing/reports/20260605-auction-ws-connection-stability.md
```

Report sections:

```markdown
# 测试报告：auction ws connection stability

## 基本信息

- 测试目标：
- 测试类型：
- 测试时间：
- 执行 agent：
- 线上地址脱敏说明：

## 矩阵结果

| Batch | Upgrade mode | Mix | QPS | Physical WS | EOF | Connect P95 | Connect P99 | Arrival P95 | Arrival P99 | Interval P95 | Interval P99 | Server write lag P95 |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |

## 日志和资源复核

## 通过项

## 失败项

## 诊断结论

## 下一步

## 测试数据清理结果
```

- [ ] **Step 8: Append redacted evidence**

Append a short evidence section to:

```text
docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/evidence-redacted.md
```

Include only:

- Batch ids.
- Aggregate metrics.
- Cleanup summary.
- Prometheus metric values.
- Strict log marker count.
- No full addresses, credentials, tickets, or full WS query strings.

- [ ] **Step 9: Update this plan with execution status**

In this file, add a dated note under this task summarizing:

- Which batches ran.
- Whether jittered improved `WS_CONNECT_P95/P99` and `dial:EOF`.
- Whether backend write lag stayed low.
- Whether cleanup completed.

- [ ] **Step 10: Commit report and evidence**

Run:

```bash
rtk git add docs/agent-testing/reports docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/evidence-redacted.md docs/superpowers/plans/2026-06-05-ws-connection-stability.md
rtk git commit -m "test: report ws connection stability matrix"
```

Execution status note, 2026-06-05:

- Ran batches:
  - `agent_ws_conn_stability_20260605_immediate`
  - `agent_ws_conn_stability_20260605_jittered`
  - `agent_ws_conn_stability_20260605_priority_jittered`
  - `agent_ws_conn_stability_20260605_item_only_jittered`
- `jittered` improved connection behavior versus `immediate`: `dial:EOF` dropped from 46 to 1, Connect P95 dropped from 5.291s to 2.719s, and Connect P99 dropped from 12.196s to 3.665s.
- `priority_jittered` improved connection behavior further: connect errors 0, Connect P95 2.594s, Connect P99 3.233s. However, bid-mix control arrival P95/P99 was anomalously high at 15.403s / 30.487s, so it needs rerun before rollout.
- `item_only_jittered` kept connection P95/P99 around 2.509s / 3.197s and control arrival P95/P99 around 572ms / 1.051s, supporting that bid fanout can amplify arrival tail while entry-path smoothing improves connection stability.
- Backend write lag stayed low: `ws_time_sync_write_lag_p95` max stayed below 5ms in all four batches; backend restarts stayed 0.
- Cleanup completed for all four batches: closed WS 600, cancel item ok, end room ok, delete users attempted 341 for each batch.
- Report written to `docs/agent-testing/reports/20260605-auction-ws-connection-stability.md`; redacted evidence appended to `docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/evidence-redacted.md`.

## Task 7: Optional Backend WebSocket Metrics After Runner Validation

**Files:**
- Modify: `internal/core/observability/metrics.go`
- Modify: `internal/core/observability/metrics_test.go`
- Modify: `internal/app/ws/hub/hub.go`
- Modify: `internal/app/ws/hub/conn.go`
- Modify: `internal/app/ws/hub/hub_test.go`

Only start this task if Task 6 shows jittered upgrade improves connection behavior but more server-side visibility is needed for rollout. If Task 6 shows no improvement, do not add metrics yet; investigate entrance/LB/network first.

- [ ] **Step 1: Write failing observability metric test**

Add to `internal/core/observability/metrics_test.go`:

```go
func TestWSConnectionLifecycleMetricsIncludeStreamAndResult(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	rec, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	rec.WSConnectionLifecycle(context.Background(), WSConnectionLifecycleMetric{
		Stream: "control",
		Result: "accepted",
		Reason: "",
	})
	rec.WSConnectionLifecycle(context.Background(), WSConnectionLifecycleMetric{
		Stream: "market",
		Result: "closed",
		Reason: "normal",
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	attrs := metricAttributes(t, rm, "ws_connection_lifecycle")
	if attrs["stream"] == "" || attrs["result"] == "" {
		t.Fatalf("expected stream and result attrs, got %#v", attrs)
	}
}
```

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/core/observability -run TestWSConnectionLifecycleMetricsIncludeStreamAndResult -count=1
```

Expected: FAIL because `WSConnectionLifecycleMetric` and `WSConnectionLifecycle` do not exist.

- [ ] **Step 3: Add metric type and recorder method**

In `internal/core/observability/metrics.go`, add:

```go
type WSConnectionLifecycleMetric struct {
	Stream string
	Result string
	Reason string
}
```

Add a counter field to `Recorder`:

```go
	wsConnectionLifecycle metric.Int64Counter
```

Create the counter in `NewRecorder` with name:

```go
"ws_connection_lifecycle"
```

Add method:

```go
func (r *Recorder) WSConnectionLifecycle(ctx context.Context, m WSConnectionLifecycleMetric) {
	if r == nil {
		return
	}
	r.wsConnectionLifecycle.Add(ctx, 1, metric.WithAttributes(
		attribute.String("stream", SafeReason(m.Stream)),
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("reason", SafeReason(m.Reason)),
	))
}
```

- [ ] **Step 4: Run observability test and verify GREEN**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/core/observability -run TestWSConnectionLifecycleMetricsIncludeStreamAndResult -count=1
```

Expected: PASS.

- [ ] **Step 5: Add hub lifecycle metric tests**

In `internal/app/ws/hub/hub_test.go`, extend the fake recorder if needed and add:

```go
func TestRegisterRecordsStreamLifecycleMetric(t *testing.T) {
	rec := &recordingMetrics{}
	h := NewHub(rec)
	conn := NewConnWithStream("conn_control", "user_1", "room_1", newFakeSocket(), h, streamControl)

	h.Register(conn)
	h.Remove(conn)

	if got := rec.countLifecycle("control", "accepted"); got != 1 {
		t.Fatalf("accepted metric count = %d, want 1", got)
	}
	if got := rec.countLifecycle("control", "closed"); got != 1 {
		t.Fatalf("closed metric count = %d, want 1", got)
	}
}
```

Use the local fake metric recorder pattern already present in `hub_test.go`.

- [ ] **Step 6: Record lifecycle metrics in hub**

In `internal/app/ws/hub/hub.go`:

- On successful `Register`, record stream/result `accepted`.
- On `Remove`, record stream/result `closed`.
- For same-stream replacement, record close reason `replaced` if the existing local recorder supports a reason; otherwise record `closed`.

Metric call shape:

```go
h.metrics.WSConnectionLifecycle(context.Background(), observability.WSConnectionLifecycleMetric{
	Stream: string(c.stream),
	Result: "accepted",
})
```

Use the context style already used by existing hub metric calls in the file.

- [ ] **Step 7: Run WS and observability tests**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/core/observability ./internal/app/ws/... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

Run:

```bash
rtk git add internal/core/observability/metrics.go internal/core/observability/metrics_test.go internal/app/ws/hub/hub.go internal/app/ws/hub/conn.go internal/app/ws/hub/hub_test.go
rtk git commit -m "feat: record ws stream lifecycle metrics"
```

## Final Verification

Before claiming implementation complete, run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/core/observability ./internal/app/ws/... ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws -count=1
```

Expected: PASS.

Then run:

```bash
rtk rg -n "115\\.|deploy@|ws://|wss://|mysql://|redis://|Authorization:|Bearer [A-Za-z0-9._-]+" docs/agent-testing/reports docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws docs/superpowers/plans/2026-06-05-ws-connection-stability.md
```

Expected: no real online addresses, tokens, DSNs, or full WebSocket query strings in newly written docs/reports.

## Execution Recommendation

Use subagent-driven development:

1. Worker 1 owns Tasks 1-4 and only edits runner files and README.
2. Main agent runs Task 5 verification.
3. Main agent requests explicit online approval before Task 6.
4. Task 7 is conditional and should not start until Task 6 proves backend metrics are needed.
