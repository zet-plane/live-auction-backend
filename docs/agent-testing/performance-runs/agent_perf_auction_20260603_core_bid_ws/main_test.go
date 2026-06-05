package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPriceTooLowIsExpectedRejectAndDoesNotTriggerSystemStop(t *testing.T) {
	summary := StageSummary{
		Total:                   100,
		ExpectedBusinessRejects: 10,
		BusinessCodes:           map[string]int64{"0": 90, "40003": 10},
	}

	if reason := thresholdStopReason(summary); reason != "" {
		t.Fatalf("expected price_too_low rejects to be excluded from stop threshold, got %q", reason)
	}
}

func TestBuildBidRequestUsesLocalMonotonicPriceWithoutHiddenRead(t *testing.T) {
	bidSeq.Store(0)
	cfg := Config{BatchID: "batch_test", BaseURL: "http://example.test"}
	data := &TestData{
		ItemID:     "item_test",
		UserTokens: []string{"token_1"},
	}

	spec, err := buildBidRequest(context.Background(), cfg, data, StageConfig{Name: "stage_test"}, 0, 42)
	if err != nil {
		t.Fatalf("buildBidRequest returned error: %v", err)
	}

	var body struct {
		Price int64 `json:"price"`
	}
	if err := json.Unmarshal(spec.Body, &body); err != nil {
		t.Fatalf("unmarshal bid body: %v", err)
	}
	if body.Price != 1100 {
		t.Fatalf("expected first bid price 1100, got %d", body.Price)
	}
}

func TestLoadConfigFiltersStagesFromStartQPS(t *testing.T) {
	t.Setenv("PERF_START_QPS", "80")

	cfg := loadConfig()

	if len(cfg.Stages) != 3 {
		t.Fatalf("expected 3 stages from 100 QPS onward, got %d", len(cfg.Stages))
	}
	if cfg.Stages[0].TargetQPS != 100 {
		t.Fatalf("expected first stage to start at 100 QPS, got %.2f", cfg.Stages[0].TargetQPS)
	}
}

func TestLoadConfigFiltersStagesThroughEndQPS(t *testing.T) {
	t.Setenv("PERF_END_QPS", "60")

	cfg := loadConfig()

	if len(cfg.Stages) != 3 {
		t.Fatalf("expected 10, 30, and 50 QPS stages, got %d", len(cfg.Stages))
	}
	if cfg.Stages[0].TargetQPS != 10 || cfg.Stages[1].TargetQPS != 30 || cfg.Stages[2].TargetQPS != 50 {
		t.Fatalf("expected stages 10, 30, and 50 QPS, got %.0f, %.0f, and %.0f", cfg.Stages[0].TargetQPS, cfg.Stages[1].TargetQPS, cfg.Stages[2].TargetQPS)
	}
}

func TestLoadConfigUsesApprovedQPSStages(t *testing.T) {
	cfg := loadConfig()

	want := []float64{10, 30, 50, 70, 100, 130, 150}
	if len(cfg.Stages) != len(want) {
		t.Fatalf("expected %d stages, got %d", len(want), len(cfg.Stages))
	}
	for i, qps := range want {
		if cfg.Stages[i].TargetQPS != qps {
			t.Fatalf("stage %d expected qps %.0f, got %.0f", i, qps, cfg.Stages[i].TargetQPS)
		}
	}
}

func TestLoadConfigCanDisableWebSocketTargets(t *testing.T) {
	t.Setenv("PERF_DISABLE_WS", "true")

	cfg := loadConfig()

	for _, stage := range cfg.Stages {
		if stage.TargetWS != 0 {
			t.Fatalf("expected websocket target to be disabled for %s, got %d", stage.Name, stage.TargetWS)
		}
	}
}

func TestLoadConfigCanUseCustomQPSStages(t *testing.T) {
	t.Setenv("PERF_STAGE_QPS", "150,200,300,500")
	t.Setenv("PERF_DISABLE_WS", "true")
	t.Setenv("PERF_REQUEST_MIX", "bid_only")

	cfg := loadConfig()

	want := []float64{150, 200, 300, 500}
	if len(cfg.Stages) != len(want) {
		t.Fatalf("expected %d custom stages, got %d", len(want), len(cfg.Stages))
	}
	for i, qps := range want {
		stage := cfg.Stages[i]
		if stage.TargetQPS != qps || stage.Concurrency != int(qps) || stage.TargetWS != 0 || stage.RequestMix != "bid_only" {
			t.Fatalf("unexpected custom stage %d: %#v", i, stage)
		}
	}
}

func TestLoadConfigCanRunCleanupOnly(t *testing.T) {
	t.Setenv("PERF_CLEANUP_ONLY", "true")

	cfg := loadConfig()

	if !cfg.CleanupOnly {
		t.Fatal("expected cleanup-only mode to be enabled")
	}
}

func TestLoadConfigCanUseWebSocketStreamMode(t *testing.T) {
	t.Setenv("PERF_WS_STREAM_MODE", wsStreamModeControlMarket)

	cfg := loadConfig()

	if cfg.WSStreamMode != wsStreamModeControlMarket {
		t.Fatalf("expected websocket stream mode %s, got %q", wsStreamModeControlMarket, cfg.WSStreamMode)
	}
}

func TestLoadConfigCanOverrideCustomWebSocketTargets(t *testing.T) {
	t.Setenv("PERF_STAGE_QPS", "5,5")
	t.Setenv("PERF_STAGE_WS", "200,400")
	t.Setenv("PERF_REQUEST_MIX", "item_only")

	cfg := loadConfig()

	wantWS := []int{200, 400}
	if len(cfg.Stages) != len(wantWS) {
		t.Fatalf("expected %d custom stages, got %d", len(wantWS), len(cfg.Stages))
	}
	for i, ws := range wantWS {
		stage := cfg.Stages[i]
		if stage.TargetQPS != 5 || stage.Concurrency != 5 || stage.TargetWS != ws || stage.RequestMix != "item_only" {
			t.Fatalf("unexpected ws hold stage %d: %#v", i, stage)
		}
	}
}

func TestBatchScopedCleanupCredentialsAreDeterministic(t *testing.T) {
	batchID := "agent_perf_auction_20260604_ws_limit_probe_fanout"

	if got := merchantAccount(batchID); got != compactBatch(batchID)+"_m" {
		t.Fatalf("unexpected merchant account: %s", got)
	}
	if got := userAccount(batchID, 7); got != compactBatch(batchID)+"_u007" {
		t.Fatalf("unexpected user account: %s", got)
	}
	if got := batchPassword(batchID); got != "PerfPass_"+compactBatch(batchID) {
		t.Fatalf("unexpected password derivation: %s", got)
	}
}

func TestIsBatchMerchantItemOnlySelectsRunnerOwnedItems(t *testing.T) {
	batchID := "agent_perf_auction_20260604_ws_limit_probe_fanout"
	tests := []struct {
		name string
		item cleanupMerchantItem
		want bool
	}{
		{name: "owned item", item: cleanupMerchantItem{Title: "agent_perf_item_" + batchID}, want: true},
		{name: "same batch wrong prefix", item: cleanupMerchantItem{Title: "manual_item_" + batchID}, want: false},
		{name: "runner prefix different batch", item: cleanupMerchantItem{Title: "agent_perf_item_other_batch"}, want: false},
	}

	for _, tt := range tests {
		if got := isBatchMerchantItem(batchID, tt.item); got != tt.want {
			t.Fatalf("%s: got %t, want %t", tt.name, got, tt.want)
		}
	}
}

func TestMerchantDisplayNameFitsUserNameLimit(t *testing.T) {
	got := merchantDisplayName("agent_perf_auction_20260603_ws_prom_validation2")

	if len(got) > 64 {
		t.Fatalf("expected merchant display name to fit 64 chars, got %d: %q", len(got), got)
	}
	if got == "" {
		t.Fatal("expected non-empty merchant display name")
	}
}

func TestDefaultStagesReachOneWebSocketPerUserAtPeak(t *testing.T) {
	cfg := loadConfig()

	if maxTargetWS(cfg.Stages) != 300 {
		t.Fatalf("expected peak WebSocket target 300, got %d", maxTargetWS(cfg.Stages))
	}
	if maxTargetWS(cfg.Stages) > cfg.UserCount {
		t.Fatalf("expected user count to cover peak WebSocket target; got ws=%d users=%d", maxTargetWS(cfg.Stages), cfg.UserCount)
	}
}

func TestLoadConfigUsesWebSocketAndObservabilityDefaults(t *testing.T) {
	cfg := loadConfig()

	if cfg.WSConnectConcurrency <= 1 {
		t.Fatalf("expected parallel websocket connections by default, got concurrency=%d", cfg.WSConnectConcurrency)
	}
	if cfg.WSConnectConcurrency != 8 {
		t.Fatalf("expected default websocket connection concurrency 8 after online EOF diagnosis, got %d", cfg.WSConnectConcurrency)
	}
	if cfg.WSConnectTimeout <= 0 {
		t.Fatalf("expected websocket connect timeout to be configured")
	}
	if cfg.WSConnectMaxAttempts <= cfg.UserCount {
		t.Fatalf("expected websocket retry budget above user count, got attempts=%d users=%d", cfg.WSConnectMaxAttempts, cfg.UserCount)
	}
	if cfg.ObservabilityStep <= 0 {
		t.Fatalf("expected observability query step to be configured")
	}
}

func TestBuildRequestUsesCoreBidRankingItemMix(t *testing.T) {
	data := &TestData{
		ItemID:     "item_test",
		UserTokens: []string{"token_1"},
	}
	var bids, rankings, items, other int
	for n := uint64(0); n < 100; n++ {
		spec := buildRequest(Config{}, data, StageConfig{}, 0, n)
		switch {
		case spec.Method == http.MethodPost && strings.HasSuffix(spec.Path, "/bids"):
			bids++
		case spec.Method == http.MethodGet && strings.Contains(spec.Path, "/ranking"):
			rankings++
		case spec.Method == http.MethodGet && spec.Path == "/api/v1/items/item_test":
			items++
		default:
			other++
		}
	}
	if bids != 80 || rankings != 10 || items != 10 || other != 0 {
		t.Fatalf("expected 80/10/10 core mix and no other requests, got bids=%d rankings=%d items=%d other=%d", bids, rankings, items, other)
	}
}

func TestBuildRequestCanUseBidOnlyMix(t *testing.T) {
	data := &TestData{
		ItemID:     "item_test",
		UserTokens: []string{"token_1"},
	}
	for n := uint64(0); n < 100; n++ {
		spec := buildRequest(Config{}, data, StageConfig{RequestMix: "bid_only"}, 0, n)
		if spec.Method != http.MethodPost || !strings.HasSuffix(spec.Path, "/bids") {
			t.Fatalf("expected bid-only request at n=%d, got %#v", n, spec)
		}
	}
}

func TestBuildRequestCanUseItemOnlyMix(t *testing.T) {
	data := &TestData{
		ItemID:     "item_test",
		UserTokens: []string{"token_1"},
	}
	for n := uint64(0); n < 100; n++ {
		spec := buildRequest(Config{}, data, StageConfig{RequestMix: "item_only"}, 0, n)
		if spec.Method != http.MethodGet || strings.Contains(spec.Path, "/bids") || strings.Contains(spec.Path, "/ranking") {
			t.Fatalf("expected item-only request at n=%d, got %#v", n, spec)
		}
	}
}

func TestWSStatsRecordsTimeSyncIntervals(t *testing.T) {
	stats := newWSStats()
	stats.record(1, []byte(`{"type":"time_sync","payload":{"server_time_unix_ms":1}}`))
	time.Sleep(time.Millisecond)
	stats.record(1, []byte(`{"type":"time_sync","payload":{"server_time_unix_ms":2}}`))
	stats.record(1, []byte(`{"type":"bid_success","payload":{}}`))

	snapshot := stats.snapshot()
	if snapshot.EventCountsLen["time_sync"] != 2 || snapshot.EventCountsLen["bid_success"] != 1 {
		t.Fatalf("unexpected event counts: %#v", snapshot.EventCountsLen)
	}
	if snapshot.TimeSyncCount != 2 {
		t.Fatalf("expected two time_sync events, got %d", snapshot.TimeSyncCount)
	}
	_, count, _, samples, _, _ := stats.deltaSince(WSStatsSnapshot{})
	if count != 2 || len(samples) != 1 || samples[0] <= 0 {
		t.Fatalf("expected time_sync count and interval sample, got count=%d samples=%v", count, samples)
	}
}

func TestWSStatsRecordsControlTimeSyncDiagnostics(t *testing.T) {
	stats := newWSStats()
	serverTime := time.Now().Add(-10 * time.Millisecond).UnixMilli()
	stats.recordStream(1, "control", []byte(`{"type":"time_sync","payload":{"server_time_unix_ms":`+strconv.FormatInt(serverTime, 10)+`}}`))
	time.Sleep(time.Millisecond)
	stats.recordStream(1, "control", []byte(`{"type":"time_sync","payload":{"server_time_unix_ms":`+strconv.FormatInt(serverTime, 10)+`}}`))

	_, _, controlCount, _, arrivalDelays, intervals := stats.deltaSince(WSStatsSnapshot{})
	if controlCount != 2 {
		t.Fatalf("expected two control time_sync events, got %d", controlCount)
	}
	if len(arrivalDelays) != 2 {
		t.Fatalf("expected two control arrival delay samples, got %d", len(arrivalDelays))
	}
	if len(intervals) != 1 || intervals[0] <= 0 {
		t.Fatalf("expected one positive control interval sample, got %v", intervals)
	}
}

func TestWSURLIncludesSplitStreamQuery(t *testing.T) {
	controlURL := wsURL("https://example.test/base", "room 1", "ticket 1", "control")
	marketURL := wsURL("http://example.test", "room/2", "ticket/2", "market")

	if !strings.HasPrefix(controlURL, "wss://example.test/ws/v1/rooms/room%201?") {
		t.Fatalf("expected https base to produce wss room URL, got %s", controlURL)
	}
	if !strings.Contains(controlURL, "ticket=ticket+1") || !strings.Contains(controlURL, "stream=control") {
		t.Fatalf("expected control URL to include ticket and stream query, got %s", controlURL)
	}
	if !strings.HasPrefix(marketURL, "ws://example.test/ws/v1/rooms/room%2F2?") {
		t.Fatalf("expected http base to produce ws room URL, got %s", marketURL)
	}
	if !strings.Contains(marketURL, "ticket=ticket%2F2") || !strings.Contains(marketURL, "stream=market") {
		t.Fatalf("expected market URL to include ticket and stream query, got %s", marketURL)
	}
}

func TestWSURLOmitsStreamQueryForAllMode(t *testing.T) {
	for _, stream := range []string{"", "all"} {
		got := wsURL("http://example.test", "room", "ticket", stream)
		if strings.Contains(got, "stream=") {
			t.Fatalf("expected all-mode stream %q to omit stream query, got %s", stream, got)
		}
	}
}

func TestEnsureWSConnectionsUsesOneUserPerConnectionAndRunsInParallel(t *testing.T) {
	var inFlight atomic.Int64
	var maxInFlight atomic.Int64
	var seenMu sync.Mutex
	seen := make(map[int]bool)

	cfg := Config{
		WSConnectConcurrency: 4,
		WSConnectMaxAttempts: 8,
		WSConnectTimeout:     time.Second,
	}
	data := &TestData{
		UserTokens: make([]string, 8),
	}
	for i := range data.UserTokens {
		data.UserTokens[i] = "token"
	}

	report := ensureWSConnectionsWith(context.Background(), cfg, data, 8, func(ctx context.Context, cfg Config, data *TestData, userIndex int, stream string) (*websocketConn, error) {
		if stream != "all" {
			t.Fatalf("expected default connector stream all, got %q", stream)
		}
		current := inFlight.Add(1)
		for {
			previous := maxInFlight.Load()
			if current <= previous || maxInFlight.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)
		seenMu.Lock()
		seen[userIndex] = true
		seenMu.Unlock()
		return nil, nil
	})

	if report.Failures != 0 {
		t.Fatalf("expected no websocket connection failures, got %d", report.Failures)
	}
	if got := wsConnectedCount(data); got != 8 {
		t.Fatalf("expected 8 connected websocket users, got %d", got)
	}
	if maxInFlight.Load() <= 1 {
		t.Fatalf("expected websocket connector to run in parallel, max in-flight=%d", maxInFlight.Load())
	}
	if len(seen) != 8 {
		t.Fatalf("expected one connection attempt per user, got %d unique users", len(seen))
	}
}

func TestEnsureWSConnectionsUsesControlAndMarketStreamsInSplitMode(t *testing.T) {
	var seenMu sync.Mutex
	seen := make(map[int]map[string]int)

	cfg := Config{
		WSConnectConcurrency: 4,
		WSConnectMaxAttempts: 8,
		WSStreamMode:         wsStreamModeControlMarket,
	}
	data := &TestData{
		UserTokens: make([]string, 3),
	}

	report := ensureWSConnectionsWith(context.Background(), cfg, data, 3, func(ctx context.Context, cfg Config, data *TestData, userIndex int, stream string) (*websocketConn, error) {
		seenMu.Lock()
		if seen[userIndex] == nil {
			seen[userIndex] = make(map[string]int)
		}
		seen[userIndex][stream]++
		seenMu.Unlock()
		return nil, nil
	})

	if report.Failures != 0 {
		t.Fatalf("expected no websocket connection failures, got %d", report.Failures)
	}
	if got := wsConnectedCount(data); got != 6 {
		t.Fatalf("expected 6 physical websocket connections, got %d", got)
	}
	if got := wsStreamConnectedCount(data, "control"); got != 3 {
		t.Fatalf("expected 3 control websocket connections, got %d", got)
	}
	if got := wsStreamConnectedCount(data, "market"); got != 3 {
		t.Fatalf("expected 3 market websocket connections, got %d", got)
	}
	for userIndex := 0; userIndex < 3; userIndex++ {
		if seen[userIndex]["control"] != 1 || seen[userIndex]["market"] != 1 || seen[userIndex]["all"] != 0 {
			t.Fatalf("expected user %d to open exactly control and market streams, got %#v", userIndex, seen[userIndex])
		}
	}
}

func TestEnsureWSConnectionsAttemptsMissingSplitTargetDespiteAggregateCount(t *testing.T) {
	var attempted []wsConnectTarget
	cfg := Config{
		WSConnectConcurrency: 1,
		WSConnectMaxAttempts: 4,
		WSStreamMode:         wsStreamModeControlMarket,
	}
	data := &TestData{
		UserTokens: make([]string, 2),
		WSUsers:    map[int]bool{9: true},
		WSStreamUsers: map[string]map[int]bool{
			"control": {0: true, 1: true},
			"market":  {0: true},
		},
	}

	report := ensureWSConnectionsWith(context.Background(), cfg, data, 2, func(ctx context.Context, cfg Config, data *TestData, userIndex int, stream string) (*websocketConn, error) {
		attempted = append(attempted, wsConnectTarget{UserIndex: userIndex, Stream: stream})
		return nil, nil
	})

	if report.Failures != 0 {
		t.Fatalf("expected no websocket connection failures, got %d", report.Failures)
	}
	if len(attempted) != 1 {
		t.Fatalf("expected exactly one missing target attempt, got %#v", attempted)
	}
	if attempted[0] != (wsConnectTarget{UserIndex: 1, Stream: "market"}) {
		t.Fatalf("expected missing user 1 market stream to be attempted, got %#v", attempted[0])
	}
}

func TestEnsureWSConnectionsStopsAtRetryBudget(t *testing.T) {
	cfg := Config{
		WSConnectConcurrency: 4,
		WSConnectMaxAttempts: 5,
		WSConnectTimeout:     time.Second,
	}
	data := &TestData{
		UserTokens: make([]string, 8),
	}
	var attempts atomic.Int64

	report := ensureWSConnectionsWith(context.Background(), cfg, data, 8, func(ctx context.Context, cfg Config, data *TestData, userIndex int, stream string) (*websocketConn, error) {
		attempts.Add(1)
		return nil, errors.New("dial failed")
	})

	if attempts.Load() != 5 {
		t.Fatalf("expected retry budget to cap attempts at 5, got %d", attempts.Load())
	}
	if report.Failures != 5 {
		t.Fatalf("expected failures to match failed attempts, got %d", report.Failures)
	}
	if report.Errors["dial:dial failed"] != 5 {
		t.Fatalf("expected dial error bucket to count 5 failures, got %d", report.Errors["dial:dial failed"])
	}
	if got := wsConnectedCount(data); got != 0 {
		t.Fatalf("expected no websocket users connected, got %d", got)
	}
}

func TestClassifyWSErrorBucketsCommonFailures(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{errors.New("ticket: status=500 code=50001 msg=internal server error"), "ticket:status=500 code=50001 msg=internal server error"},
		{errors.New("dial_status:403: websocket: bad handshake"), "dial_status:403"},
		{errors.New("dial tcp: i/o timeout"), "dial:timeout"},
	}

	for _, tt := range tests {
		if got := classifyWSError(tt.err); got != tt.want {
			t.Fatalf("classifyWSError(%q) = %q, want %q", tt.err.Error(), got, tt.want)
		}
	}
}

func TestThresholdStopReasonDoesNotStopOnClientE2EP99(t *testing.T) {
	summary := StageSummary{
		Total: 100,
		P99:   3 * time.Second,
	}

	if reason := thresholdStopReason(summary); reason != "" {
		t.Fatalf("expected client-side p99 to be advisory for server-focused tests, got stop reason %q", reason)
	}
}

func TestThresholdStopReasonRequiresBothSplitStreams(t *testing.T) {
	summary := StageSummary{
		WSStreamMode:               wsStreamModeControlMarket,
		TargetWS:                   100,
		WSConnected:                190,
		ControlWSConnected:         100,
		MarketWSConnected:          90,
		ControlTimeSyncCount:       10000,
		TimeSyncCount:              10000,
		TimeSyncP95:                time.Second,
		ControlTimeSyncIntervalP95: time.Second,
	}

	if reason := thresholdStopReason(summary); reason != "ws_connection_success_lt_95_percent" {
		t.Fatalf("expected partial split stream connections to fail threshold, got %q", reason)
	}
}

func TestThresholdStopReasonPassesBalancedSplitStreams(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	summary := StageSummary{
		WSStreamMode:               wsStreamModeControlMarket,
		TargetWS:                   100,
		WSConnected:                190,
		ControlWSConnected:         95,
		MarketWSConnected:          95,
		Start:                      start,
		End:                        start.Add(10 * time.Second),
		ControlTimeSyncCount:       500,
		ControlTimeSyncIntervalP95: time.Second,
		TimeSyncCount:              0,
		TimeSyncP95:                10 * time.Second,
	}

	if reason := thresholdStopReason(summary); reason != "" {
		t.Fatalf("expected balanced split streams to pass threshold, got %q", reason)
	}
}

func TestThresholdStopReasonUsesControlTimeSyncInSplitMode(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	summary := StageSummary{
		WSStreamMode:               wsStreamModeControlMarket,
		TargetWS:                   10,
		WSConnected:                20,
		ControlWSConnected:         10,
		MarketWSConnected:          10,
		Start:                      start,
		End:                        start.Add(10 * time.Second),
		ControlTimeSyncCount:       1000,
		TimeSyncCount:              1000,
		TimeSyncP95:                time.Second,
		ControlTimeSyncIntervalP95: 4 * time.Second,
	}

	if reason := thresholdStopReason(summary); reason != "time_sync_p95_interval_gt_3s" {
		t.Fatalf("expected split mode to use control time_sync interval threshold, got %q", reason)
	}
}

func TestThresholdStopReasonUsesControlTimeSyncCountInSplitMode(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	summary := StageSummary{
		WSStreamMode:               wsStreamModeControlMarket,
		TargetWS:                   10,
		WSConnected:                20,
		ControlWSConnected:         10,
		MarketWSConnected:          10,
		Start:                      start,
		End:                        start.Add(10 * time.Second),
		TimeSyncCount:              1000,
		TimeSyncP95:                time.Second,
		ControlTimeSyncCount:       1,
		ControlTimeSyncIntervalP95: time.Second,
	}

	if reason := thresholdStopReason(summary); reason != "time_sync_missing_or_low_rate" {
		t.Fatalf("expected split mode to use control time_sync count threshold, got %q", reason)
	}
}

func TestPrintStageSummaryLabelsClientE2ELatency(t *testing.T) {
	output := captureStdout(t, func() {
		printStageSummary(StageSummary{
			Name:               "stage_test",
			Start:              time.Unix(100, 0).UTC(),
			End:                time.Unix(160, 0).UTC(),
			WSStreamMode:       wsStreamModeControlMarket,
			ControlWSConnected: 10,
			MarketWSConnected:  10,
			P50:                100 * time.Millisecond,
			P95:                200 * time.Millisecond,
			P99:                300 * time.Millisecond,
			Max:                400 * time.Millisecond,
		})
	})

	for _, label := range []string{
		"WS_STREAM_MODE:",
		"CONTROL_WS_CONNECTED:",
		"MARKET_WS_CONNECTED:",
		"CONTROL_TIME_SYNC_COUNT:",
		"CONTROL_TIME_SYNC_ARRIVAL_DELAY_P50:",
		"CONTROL_TIME_SYNC_ARRIVAL_DELAY_P95:",
		"CONTROL_TIME_SYNC_ARRIVAL_DELAY_P99:",
		"CONTROL_TIME_SYNC_INTERVAL_P50:",
		"CONTROL_TIME_SYNC_INTERVAL_P95:",
		"CONTROL_TIME_SYNC_INTERVAL_P99:",
		"CLIENT_E2E_P50:",
		"CLIENT_E2E_P95:",
		"CLIENT_E2E_P99:",
		"CLIENT_E2E_MAX:",
	} {
		if !strings.Contains(output, label) {
			t.Fatalf("expected output to include %s; got:\n%s", label, output)
		}
	}
	for _, oldLabel := range []string{"\n  P50:", "\n  P95:", "\n  P99:", "\n  MAX:"} {
		if strings.Contains(output, oldLabel) {
			t.Fatalf("expected output to avoid ambiguous latency label %q; got:\n%s", oldLabel, output)
		}
	}
}

func TestPrometheusRangeURLBuildsQueryRange(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	end := time.Unix(220, 0).UTC()

	got, err := prometheusRangeURL("http://prometheus.example/base", `sum(rate(http_server_request_count[1m]))`, start, end, 30*time.Second)
	if err != nil {
		t.Fatalf("prometheusRangeURL returned error: %v", err)
	}

	if got.Path != "/base/api/v1/query_range" {
		t.Fatalf("expected query_range path, got %s", got.Path)
	}
	if got.Query().Get("query") != `sum(rate(http_server_request_count[1m]))` {
		t.Fatalf("query parameter was not preserved: %s", got.RawQuery)
	}
	if got.Query().Get("start") == "" || got.Query().Get("end") == "" || got.Query().Get("step") != "30" {
		t.Fatalf("expected start/end/step query parameters, got %s", got.RawQuery)
	}
}

func TestDefaultPrometheusQueriesUseObservedMetricNames(t *testing.T) {
	queries := defaultPrometheusQueries()
	joined := ""
	names := map[string]bool{}
	for _, query := range queries {
		joined += "\n" + query.Name + " " + query.Query
		names[query.Name] = true
	}

	for _, metric := range []string{
		"http_server_request_duration_bucket",
		"http_server_request_count_total",
		"auction_bid_count_total",
		"auction_bid_broadcast_count_total",
		"auction_bid_broadcast_duration_bucket",
		"auction_bid_broadcast_bids_bucket",
		"auction_bid_broadcast_pending_bucket",
		"auction_place_bid_lua_result_count_total",
		"db_client_operation_count_total",
		"ws_connection_active",
		"ws_delivery_count_total",
		"ws_write_count_total",
		"ws_write_duration_bucket",
		"ws_send_queue_depth_bucket",
		"ws_time_sync_count_total",
		"ws_time_sync_write_lag_duration_bucket",
	} {
		if !strings.Contains(joined, metric) {
			t.Fatalf("expected default Prometheus queries to include %s; got:%s", metric, joined)
		}
	}
	if strings.Contains(joined, "_duration_seconds_bucket") {
		t.Fatalf("default Prometheus queries still include old _duration_seconds_bucket metric names:%s", joined)
	}
	for _, name := range []string{"server_http_p95", "server_http_p99"} {
		if !names[name] {
			t.Fatalf("expected default Prometheus queries to include %s; got:%s", name, joined)
		}
	}
	for _, oldName := range []string{"http_p95", "http_p99"} {
		if names[oldName] {
			t.Fatalf("expected default Prometheus queries to avoid ambiguous name %s; got:%s", oldName, joined)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = old
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out)
}

func mustRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw message: %v", err)
	}
	return raw
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
