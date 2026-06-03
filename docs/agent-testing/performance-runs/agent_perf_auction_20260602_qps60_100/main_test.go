package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
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

func TestBuildBidRequestReadsCurrentPriceBeforeIncrement(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/items/item_test" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		raw, _ := json.Marshal(apiResponse{
			Code:    0,
			Message: "ok",
			Data: mustRawMessage(t, map[string]any{
				"current_price": float64(1500),
				"rule": map[string]any{
					"bid_increment": float64(100),
				},
			}),
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Header:     make(http.Header),
		}, nil
	})}

	cfg := Config{BatchID: "batch_test", BaseURL: "http://example.test"}
	data := &TestData{
		ItemID:     "item_test",
		UserTokens: []string{"token_1"},
		HTTPClient: client,
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
	if body.Price != 1600 {
		t.Fatalf("expected bid price 1600, got %d", body.Price)
	}
}

func TestLoadConfigFiltersStagesFromStartQPS(t *testing.T) {
	t.Setenv("PERF_START_QPS", "80")

	cfg := loadConfig()

	if len(cfg.Stages) != 3 {
		t.Fatalf("expected 3 stages from 80 QPS onward, got %d", len(cfg.Stages))
	}
	if cfg.Stages[0].TargetQPS != 80 {
		t.Fatalf("expected first stage to start at 80 QPS, got %.2f", cfg.Stages[0].TargetQPS)
	}
}

func TestLoadConfigFiltersStagesThroughEndQPS(t *testing.T) {
	t.Setenv("PERF_END_QPS", "60")

	cfg := loadConfig()

	if len(cfg.Stages) != 2 {
		t.Fatalf("expected smoke and 60 QPS stages, got %d", len(cfg.Stages))
	}
	if cfg.Stages[0].TargetQPS != 10 || cfg.Stages[1].TargetQPS != 60 {
		t.Fatalf("expected stages 10 and 60 QPS, got %.0f and %.0f", cfg.Stages[0].TargetQPS, cfg.Stages[1].TargetQPS)
	}
}

func TestLoadConfigUsesApprovedQPSStages(t *testing.T) {
	cfg := loadConfig()

	want := []float64{10, 60, 70, 80, 90, 100}
	if len(cfg.Stages) != len(want) {
		t.Fatalf("expected %d stages, got %d", len(want), len(cfg.Stages))
	}
	for i, qps := range want {
		if cfg.Stages[i].TargetQPS != qps {
			t.Fatalf("stage %d expected qps %.0f, got %.0f", i, qps, cfg.Stages[i].TargetQPS)
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

	if maxTargetWS(cfg.Stages) != cfg.UserCount {
		t.Fatalf("expected peak WebSocket target to match user count; got ws=%d users=%d", maxTargetWS(cfg.Stages), cfg.UserCount)
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

	report := ensureWSConnectionsWith(context.Background(), cfg, data, 8, func(ctx context.Context, cfg Config, data *TestData, userIndex int) (*websocketConn, error) {
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

	report := ensureWSConnectionsWith(context.Background(), cfg, data, 8, func(ctx context.Context, cfg Config, data *TestData, userIndex int) (*websocketConn, error) {
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

func TestPrintStageSummaryLabelsClientE2ELatency(t *testing.T) {
	output := captureStdout(t, func() {
		printStageSummary(StageSummary{
			Name:  "stage_test",
			Start: time.Unix(100, 0).UTC(),
			End:   time.Unix(160, 0).UTC(),
			P50:   100 * time.Millisecond,
			P95:   200 * time.Millisecond,
			P99:   300 * time.Millisecond,
			Max:   400 * time.Millisecond,
		})
	})

	for _, label := range []string{
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
		"auction_place_bid_lua_result_count_total",
		"db_client_operation_count_total",
		"ws_connection_active",
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
