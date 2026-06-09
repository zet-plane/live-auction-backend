// Agent testing performance runner template.
//
// Copy this file into a committed run directory such as:
//   docs/agent-testing/performance-runs/<batch_id>/main.go
//
// Runtime secrets and online addresses must be passed through environment
// variables. Do not write tokens, DSNs, passwords, or production URLs into this
// file or into reports.
//
// Minimal run:
//   PERF_BASE_URL=http://127.0.0.1:8080 PERF_BATCH_ID=agent_bid_load_20260601120000 go run main.go

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── CONFIG (agent fills) ─────────────────────────────────────────────────────

type Config struct {
	BatchID        string
	Environment    string
	BaseURL        string
	AuthToken      string
	PrometheusURL  string
	StopFile       string
	HumanMonitor   string
	EvidencePath   string
	RequestTimeout time.Duration
	Stages         []StageConfig
}

type StageConfig struct {
	Name        string
	TargetQPS   float64
	Concurrency int
	Duration    time.Duration
	Ramp        time.Duration
	RequestMix  string
}

func loadConfig() Config {
	cfg := Config{
		BatchID:        getenv("PERF_BATCH_ID", "agent_perf_load_YYYYMMDDHHMMSS"),
		Environment:    getenv("PERF_ENVIRONMENT", "local_smoke"),
		BaseURL:        strings.TrimRight(getenv("PERF_BASE_URL", "http://127.0.0.1:8080"), "/"),
		AuthToken:      os.Getenv("PERF_AUTH_TOKEN"),
		PrometheusURL:  strings.TrimRight(os.Getenv("PERF_PROMETHEUS_URL"), "/"),
		StopFile:       getenv("PERF_STOP_FILE", "STOP"),
		HumanMonitor:   getenv("PERF_HUMAN_MONITOR", "unspecified"),
		EvidencePath:   os.Getenv("PERF_EVIDENCE_PATH"),
		RequestTimeout: envDuration("PERF_REQUEST_TIMEOUT", 10*time.Second),
		Stages: []StageConfig{
			{Name: "smoke", TargetQPS: 1, Concurrency: 1, Duration: 30 * time.Second, RequestMix: "health"},
			// Agent fills approved stages before running real load:
			// {Name: "step_10qps", TargetQPS: 10, Concurrency: 5, Duration: 5 * time.Minute, RequestMix: "bid"},
			// {Name: "peak_hold", TargetQPS: 50, Concurrency: 20, Duration: 10 * time.Minute, RequestMix: "bid"},
		},
	}
	return cfg
}

// ── PERFORMANCE TYPES ───────────────────────────────────────────────────────

type RequestSpec struct {
	Endpoint string
	Method   string
	Path     string
	Body     []byte
}

type RequestResult struct {
	Endpoint     string
	StatusCode   int
	BusinessCode string
	Duration     time.Duration
	BodyBytes    int
	Err          string
}

type EndpointSummary struct {
	Total         int64
	Success       int64
	HTTPFailures  int64
	BusinessFails int64
	Timeouts      int64
	P50           time.Duration
	P95           time.Duration
	P99           time.Duration
	Max           time.Duration
	StatusCodes   map[int]int64
	BusinessCodes map[string]int64
	latencies     []time.Duration
}

type StageSummary struct {
	Name          string
	Start         time.Time
	End           time.Time
	TargetQPS     float64
	ActualQPS     float64
	Concurrency   int
	Total         int64
	Success       int64
	HTTPFailures  int64
	BusinessFails int64
	Timeouts      int64
	P50           time.Duration
	P95           time.Duration
	P99           time.Duration
	Max           time.Duration
	StatusCodes   map[int]int64
	BusinessCodes map[string]int64
	EndpointStats map[string]*EndpointSummary
	StopReason    string
}

// ── WORKLOAD (agent fills) ──────────────────────────────────────────────────

func buildRequest(stage StageConfig, workerID int, seq uint64) RequestSpec {
	// Default workload is intentionally safe. Replace with the approved target
	// request after the performance plan is approved.
	return RequestSpec{Endpoint: "health", Method: http.MethodGet, Path: "/health"}
}

func classifyBusiness(body []byte) string {
	// Agent can parse the service response and return stable business codes,
	// for example: OK, BID_TOO_LOW, AUCTION_ENDED, DUPLICATE.
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "unparsed"
	}
	for _, key := range []string{"code", "error_code", "business_code"} {
		if val, ok := parsed[key].(string); ok && val != "" {
			return val
		}
	}
	return "ok"
}

func isBusinessSuccess(status int, code string) bool {
	if status < 200 || status >= 300 {
		return false
	}
	return code == "" || code == "ok" || code == "OK" || code == "0"
}

func reconcile(ctx context.Context, cfg Config, summaries []StageSummary) string {
	// Agent fills approved HTTP/MySQL/Redis/WebSocket checks here.
	// Keep sensitive values out of the returned string.
	_ = ctx
	_ = cfg
	_ = summaries
	return "not_configured"
}

func cleanup(ctx context.Context, cfg Config) string {
	// Clean online data, not this runner code. Only clean data that belongs to
	// cfg.BatchID or explicit IDs created by this run.
	_ = ctx
	return fmt.Sprintf("runner_code_retained=true batch_id=%s cleanup_not_configured", cfg.BatchID)
}

// ── RUNNER CORE (do not modify unless the template itself changes) ──────────

var seq atomic.Uint64

func main() {
	cfg := loadConfig()
	client := &http.Client{Timeout: cfg.RequestTimeout}
	ctx := context.Background()

	printPlan(cfg)
	printPreflight(ctx, cfg, client)

	var summaries []StageSummary
	for _, stage := range cfg.Stages {
		if stopRequested(cfg.StopFile) {
			fmt.Printf("\n=== STOP_EVENT\n  STAGE: before_%s\n  REASON: stop_file_present path=%s\n", stage.Name, cfg.StopFile)
			break
		}
		summary := runStage(ctx, cfg, client, stage)
		printStageSummary(summary)
		summaries = append(summaries, summary)
		if summary.StopReason != "" {
			fmt.Printf("\n=== STOP_EVENT\n  STAGE: %s\n  REASON: %s\n", stage.Name, summary.StopReason)
			break
		}
	}

	fmt.Println("\n=== RECONCILE")
	fmt.Printf("  RESULT: %s\n", reconcile(ctx, cfg, summaries))

	fmt.Println("\n=== CLEANUP")
	fmt.Printf("  RESULT: %s\n", cleanup(ctx, cfg))

	printSummary(cfg, summaries)
}

func runStage(ctx context.Context, cfg Config, client *http.Client, stage StageConfig) StageSummary {
	if stage.Concurrency <= 0 {
		stage.Concurrency = 1
	}
	if stage.Duration <= 0 {
		stage.Duration = 30 * time.Second
	}
	start := time.Now()
	stageCtx, cancel := context.WithTimeout(ctx, stage.Duration)
	defer cancel()

	results := make(chan RequestResult, stage.Concurrency*2)
	tokens := make(chan struct{}, stage.Concurrency*2)
	var wg sync.WaitGroup

	if stage.TargetQPS > 0 {
		go produceTokens(stageCtx, stage.TargetQPS, tokens)
	} else {
		go produceOpenLoop(stageCtx, tokens)
	}

	for workerID := 0; workerID < stage.Concurrency; workerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stageCtx.Done():
					return
				case _, ok := <-tokens:
					if !ok {
						return
					}
					if stopRequested(cfg.StopFile) {
						cancel()
						return
					}
					n := seq.Add(1)
					results <- doRequest(stageCtx, cfg, client, stage, id, n)
				}
			}
		}(workerID)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	summary := StageSummary{
		Name:          stage.Name,
		Start:         start,
		TargetQPS:     stage.TargetQPS,
		Concurrency:   stage.Concurrency,
		StatusCodes:   map[int]int64{},
		BusinessCodes: map[string]int64{},
		EndpointStats: map[string]*EndpointSummary{},
	}
	var latencies []time.Duration
	for result := range results {
		summary.Total++
		latencies = append(latencies, result.Duration)
		if result.Duration > summary.Max {
			summary.Max = result.Duration
		}
		recordEndpointResult(&summary, result)
		if result.Err != "" {
			if strings.Contains(result.Err, "timeout") || strings.Contains(result.Err, "deadline") {
				summary.Timeouts++
			}
			summary.HTTPFailures++
			continue
		}
		summary.StatusCodes[result.StatusCode]++
		summary.BusinessCodes[result.BusinessCode]++
		if isBusinessSuccess(result.StatusCode, result.BusinessCode) {
			summary.Success++
		} else if result.StatusCode >= 200 && result.StatusCode < 500 {
			summary.BusinessFails++
		} else {
			summary.HTTPFailures++
		}
	}
	summary.End = time.Now()
	elapsed := summary.End.Sub(summary.Start).Seconds()
	if elapsed > 0 {
		summary.ActualQPS = float64(summary.Total) / elapsed
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	summary.P50 = percentile(latencies, 0.50)
	summary.P95 = percentile(latencies, 0.95)
	summary.P99 = percentile(latencies, 0.99)
	finalizeEndpointSummaries(summary.EndpointStats)
	if stopRequested(cfg.StopFile) {
		summary.StopReason = "stop_file_present"
	}
	return summary
}

func recordEndpointResult(summary *StageSummary, result RequestResult) {
	endpoint := result.Endpoint
	if endpoint == "" {
		endpoint = "unknown"
	}
	stats := summary.EndpointStats[endpoint]
	if stats == nil {
		stats = &EndpointSummary{
			StatusCodes:   map[int]int64{},
			BusinessCodes: map[string]int64{},
		}
		summary.EndpointStats[endpoint] = stats
	}
	stats.Total++
	stats.latencies = append(stats.latencies, result.Duration)
	if result.Duration > stats.Max {
		stats.Max = result.Duration
	}
	if result.Err != "" {
		if strings.Contains(result.Err, "timeout") || strings.Contains(result.Err, "deadline") {
			stats.Timeouts++
		}
		stats.HTTPFailures++
		return
	}
	stats.StatusCodes[result.StatusCode]++
	stats.BusinessCodes[result.BusinessCode]++
	if isBusinessSuccess(result.StatusCode, result.BusinessCode) {
		stats.Success++
	} else if result.StatusCode >= 200 && result.StatusCode < 500 {
		stats.BusinessFails++
	} else {
		stats.HTTPFailures++
	}
}

func finalizeEndpointSummaries(all map[string]*EndpointSummary) {
	for _, stats := range all {
		sort.Slice(stats.latencies, func(i, j int) bool { return stats.latencies[i] < stats.latencies[j] })
		stats.P50 = percentile(stats.latencies, 0.50)
		stats.P95 = percentile(stats.latencies, 0.95)
		stats.P99 = percentile(stats.latencies, 0.99)
	}
}

func sortedEndpointNames(all map[string]*EndpointSummary) []string {
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func doRequest(ctx context.Context, cfg Config, client *http.Client, stage StageConfig, workerID int, n uint64) RequestResult {
	spec := buildRequest(stage, workerID, n)
	var body io.Reader
	if len(spec.Body) > 0 {
		body = bytes.NewReader(spec.Body)
	}
	req, err := http.NewRequestWithContext(ctx, spec.Method, cfg.BaseURL+spec.Path, body)
	if err != nil {
		return RequestResult{Endpoint: spec.Endpoint, Err: err.Error()}
	}
	if len(spec.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	}
	req.Header.Set("X-Agent-Test-Batch", cfg.BatchID)
	req.Header.Set("X-Agent-Perf-Stage", stage.Name)

	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)
	if err != nil {
		return RequestResult{Endpoint: spec.Endpoint, Duration: duration, Err: err.Error()}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return RequestResult{
		Endpoint:     spec.Endpoint,
		StatusCode:   resp.StatusCode,
		BusinessCode: classifyBusiness(respBody),
		Duration:     duration,
		BodyBytes:    len(respBody),
	}
}

func produceTokens(ctx context.Context, qps float64, out chan<- struct{}) {
	defer close(out)
	interval := time.Duration(float64(time.Second) / qps)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case out <- struct{}{}:
			default:
			}
		}
	}
}

func produceOpenLoop(ctx context.Context, out chan<- struct{}) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case out <- struct{}{}:
		}
	}
}

func printPlan(cfg Config) {
	fmt.Println("=== PERF_PLAN")
	fmt.Printf("  BATCH_ID: %s\n", cfg.BatchID)
	fmt.Printf("  ENVIRONMENT: %s\n", cfg.Environment)
	fmt.Printf("  BASE_URL: %s\n", redactURL(cfg.BaseURL))
	fmt.Printf("  PROMETHEUS: %s\n", present(cfg.PrometheusURL))
	fmt.Printf("  HUMAN_MONITOR: %s\n", cfg.HumanMonitor)
	fmt.Printf("  STOP_FILE: %s\n", cfg.StopFile)
	fmt.Printf("  STAGES: %d\n", len(cfg.Stages))
	for _, s := range cfg.Stages {
		fmt.Printf("  STAGE_CONFIG: name=%s qps=%.2f concurrency=%d duration=%s mix=%s\n", s.Name, s.TargetQPS, s.Concurrency, s.Duration, s.RequestMix)
	}
}

func printPreflight(ctx context.Context, cfg Config, client *http.Client) {
	fmt.Println("\n=== PREFLIGHT")
	status, err := probe(ctx, cfg, client, "/health")
	if err != "" {
		fmt.Printf("  HEALTH: FAIL status=%d err=%s\n", status, err)
	} else {
		fmt.Printf("  HEALTH: OK status=%d\n", status)
	}
	if cfg.PrometheusURL == "" {
		fmt.Println("  PROMETHEUS: not_configured")
	} else {
		promStatus, promErr := probeURL(ctx, client, cfg.PrometheusURL+"/-/ready")
		if promErr != "" {
			fmt.Printf("  PROMETHEUS: FAIL status=%d err=%s\n", promStatus, promErr)
		} else {
			fmt.Printf("  PROMETHEUS: OK status=%d\n", promStatus)
		}
	}
	fmt.Printf("  STOP_FILE_PRESENT: %t\n", stopRequested(cfg.StopFile))
}

func printStageSummary(s StageSummary) {
	fmt.Printf("\n=== STAGE: %s\n", s.Name)
	fmt.Printf("  WINDOW: %s -> %s\n", s.Start.Format(time.RFC3339), s.End.Format(time.RFC3339))
	fmt.Printf("  TARGET_QPS: %.2f\n", s.TargetQPS)
	fmt.Printf("  ACTUAL_QPS: %.2f\n", s.ActualQPS)
	fmt.Printf("  CONCURRENCY: %d\n", s.Concurrency)
	fmt.Printf("  TOTAL: %d\n", s.Total)
	fmt.Printf("  SUCCESS: %d\n", s.Success)
	fmt.Printf("  HTTP_FAILURES: %d\n", s.HTTPFailures)
	fmt.Printf("  BUSINESS_FAILS: %d\n", s.BusinessFails)
	fmt.Printf("  TIMEOUTS: %d\n", s.Timeouts)
	fmt.Printf("  ERROR_RATE: %.4f\n", ratio(s.HTTPFailures+s.BusinessFails, s.Total))
	fmt.Printf("  TIMEOUT_RATE: %.4f\n", ratio(s.Timeouts, s.Total))
	fmt.Printf("  BUSINESS_FAILURE_RATE: %.4f\n", ratio(s.BusinessFails, s.Total))
	fmt.Printf("  CLIENT_E2E_P50: %s\n", s.P50)
	fmt.Printf("  CLIENT_E2E_P95: %s\n", s.P95)
	fmt.Printf("  CLIENT_E2E_P99: %s\n", s.P99)
	fmt.Printf("  CLIENT_E2E_MAX: %s\n", s.Max)
	fmt.Println("  CLIENT_E2E_BY_ENDPOINT:")
	for _, name := range sortedEndpointNames(s.EndpointStats) {
		stats := s.EndpointStats[name]
		fmt.Printf("    ENDPOINT: %s\n", name)
		fmt.Printf("      TOTAL: %d\n", stats.Total)
		fmt.Printf("      SUCCESS: %d\n", stats.Success)
		fmt.Printf("      HTTP_FAILURES: %d\n", stats.HTTPFailures)
		fmt.Printf("      BUSINESS_FAILS: %d\n", stats.BusinessFails)
		fmt.Printf("      TIMEOUTS: %d\n", stats.Timeouts)
		fmt.Printf("      ERROR_RATE: %.4f\n", ratio(stats.HTTPFailures+stats.BusinessFails, stats.Total))
		fmt.Printf("      TIMEOUT_RATE: %.4f\n", ratio(stats.Timeouts, stats.Total))
		fmt.Printf("      P50: %s\n", stats.P50)
		fmt.Printf("      P95: %s\n", stats.P95)
		fmt.Printf("      P99: %s\n", stats.P99)
		fmt.Printf("      MAX: %s\n", stats.Max)
		fmt.Printf("      STATUS_CODES: %s\n", jsonLine(stats.StatusCodes))
		fmt.Printf("      BUSINESS_CODES: %s\n", jsonLine(stats.BusinessCodes))
	}
	fmt.Printf("  STATUS_CODES: %s\n", jsonLine(s.StatusCodes))
	fmt.Printf("  BUSINESS_CODES: %s\n", jsonLine(s.BusinessCodes))
	if s.StopReason != "" {
		fmt.Printf("  STOP_REASON: %s\n", s.StopReason)
	}
}

func printSummary(cfg Config, summaries []StageSummary) {
	var total, success, httpFailures, businessFails, timeouts int64
	for _, s := range summaries {
		total += s.Total
		success += s.Success
		httpFailures += s.HTTPFailures
		businessFails += s.BusinessFails
		timeouts += s.Timeouts
	}
	fmt.Println("\n=== SUMMARY")
	fmt.Printf("  BATCH_ID: %s\n", cfg.BatchID)
	fmt.Printf("  ENVIRONMENT: %s\n", cfg.Environment)
	fmt.Printf("  STAGES_RUN: %d\n", len(summaries))
	fmt.Printf("  TOTAL: %d\n", total)
	fmt.Printf("  SUCCESS: %d\n", success)
	fmt.Printf("  HTTP_FAILURES: %d\n", httpFailures)
	fmt.Printf("  BUSINESS_FAILS: %d\n", businessFails)
	fmt.Printf("  TIMEOUTS: %d\n", timeouts)
	fmt.Printf("  ERROR_RATE: %.4f\n", ratio(httpFailures+businessFails, total))
	fmt.Printf("  TIMEOUT_RATE: %.4f\n", ratio(timeouts, total))
	fmt.Printf("  BUSINESS_FAILURE_RATE: %.4f\n", ratio(businessFails, total))
	fmt.Printf("  RUNNER_CODE_RETAINED: true\n")
}

func probe(ctx context.Context, cfg Config, client *http.Client, path string) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+path, nil)
	if err != nil {
		return 0, err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	return resp.StatusCode, ""
}

func probeURL(ctx context.Context, client *http.Client, url string) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	return resp.StatusCode, ""
}

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		return values[0]
	}
	if p >= 1 {
		return values[len(values)-1]
	}
	idx := int(math.Ceil(float64(len(values))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}

func stopRequested(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func getenv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	d, err := time.ParseDuration(val)
	if err == nil {
		return d
	}
	if seconds, err := strconv.Atoi(val); err == nil {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func jsonLine(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func ratio(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func present(v string) string {
	if v == "" {
		return "not_configured"
	}
	return "configured"
}

func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "127.0.0.1") || strings.Contains(raw, "localhost") {
		return raw
	}
	return "<redacted-online-url>"
}
