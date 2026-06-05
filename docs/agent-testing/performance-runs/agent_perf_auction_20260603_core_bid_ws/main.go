package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// PERF_WS_STREAM_MODE is a regression-run override. Product clients should
	// support millisecond sync by default and switch streams automatically.
	wsStreamModeAll           = "all"
	wsStreamModeControlMarket = "control_market"
)

type Config struct {
	BatchID              string
	Environment          string
	BaseURL              string
	PrometheusURL        string
	StopFile             string
	HumanMonitor         string
	RequestTimeout       time.Duration
	WSConnectTimeout     time.Duration
	WSConnectConcurrency int
	WSConnectMaxAttempts int
	WSStreamMode         string
	ObservabilityStep    time.Duration
	UserCount            int
	CleanupOnly          bool
	Stages               []StageConfig
}

type StageConfig struct {
	Name        string
	TargetQPS   float64
	TargetWS    int
	Concurrency int
	Duration    time.Duration
	RequestMix  string
}

type TestData struct {
	MerchantToken string
	MerchantUser  string
	RoomID        string
	ItemID        string
	UserTokens    []string
	UserIDs       []string
	HTTPClient    *http.Client
	WSConns       []*websocket.Conn
	WSUsers       map[int]bool
	WSStreamUsers map[string]map[int]bool
	WSStats       *WSStats
}

type RequestSpec struct {
	Method string
	Path   string
	Token  string
	Body   []byte
}

type RequestResult struct {
	StatusCode             int
	BusinessCode           string
	Duration               time.Duration
	Err                    string
	ExpectedBusinessReject bool
}

type StageSummary struct {
	Name                           string
	Start                          time.Time
	End                            time.Time
	TargetQPS                      float64
	TargetWS                       int
	WSStreamMode                   string
	ActualQPS                      float64
	Concurrency                    int
	Total                          int64
	Success                        int64
	HTTPFailures                   int64
	BusinessFails                  int64
	ExpectedBusinessRejects        int64
	Timeouts                       int64
	P50                            time.Duration
	P95                            time.Duration
	P99                            time.Duration
	Max                            time.Duration
	StatusCodes                    map[int]int64
	BusinessCodes                  map[string]int64
	WSConnected                    int
	ControlWSConnected             int
	MarketWSConnected              int
	WSConnectFails                 int
	WSConnectErrors                map[string]int64
	WSEventCounts                  map[string]int64
	TimeSyncCount                  int64
	ControlTimeSyncCount           int64
	TimeSyncP50                    time.Duration
	TimeSyncP95                    time.Duration
	TimeSyncP99                    time.Duration
	TimeSyncMax                    time.Duration
	ControlTimeSyncArrivalDelayP50 time.Duration
	ControlTimeSyncArrivalDelayP95 time.Duration
	ControlTimeSyncArrivalDelayP99 time.Duration
	ControlTimeSyncIntervalP50     time.Duration
	ControlTimeSyncIntervalP95     time.Duration
	ControlTimeSyncIntervalP99     time.Duration
	StopReason                     string
}

type WSStats struct {
	mu                                 sync.Mutex
	eventCounts                        map[string]int64
	timeSyncCount                      int64
	timeSyncLast                       map[int]time.Time
	timeSyncSamples                    []time.Duration
	controlTimeSyncCount               int64
	controlTimeSyncLast                map[int]time.Time
	controlTimeSyncArrivalDelaySamples []time.Duration
	controlTimeSyncIntervalSamples     []time.Duration
}

type WSStatsSnapshot struct {
	EventCountsLen                       map[string]int64
	TimeSyncCount                        int64
	TimeSyncSampleLen                    int
	ControlTimeSyncCount                 int64
	ControlTimeSyncArrivalDelaySampleLen int
	ControlTimeSyncIntervalSampleLen     int
}

type websocketConn = websocket.Conn

type wsConnector func(context.Context, Config, *TestData, int, string) (*websocketConn, error)

type wsConnectReport struct {
	Failures int
	Errors   map[string]int64
}

type prometheusQuery struct {
	Name  string
	Query string
}

type prometheusRangeResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

var seq atomic.Uint64
var bidSeq atomic.Int64

func main() {
	cfg := loadConfig()
	client := &http.Client{Timeout: cfg.RequestTimeout}
	ctx := context.Background()

	printPlan(cfg)
	printPreflight(ctx, cfg, client)

	if cfg.CleanupOnly {
		fmt.Println("\n=== CLEANUP_ONLY")
		fmt.Printf("  RESULT: %s\n", cleanupBatchOnly(ctx, cfg, client))
		return
	}

	data, err := setupData(ctx, cfg, client)
	if err != nil {
		fmt.Println("\n=== STOP_EVENT")
		fmt.Printf("  STAGE: preflight\n")
		fmt.Printf("  REASON: setup_failed err=%s\n", sanitizeErr(err))
		fmt.Println("\n=== CLEANUP")
		fmt.Printf("  RESULT: setup_failed_no_load batch_id=%s\n", cfg.BatchID)
		return
	}

	var summaries []StageSummary
	for _, stage := range cfg.Stages {
		if stopRequested(cfg.StopFile) {
			fmt.Printf("\n=== STOP_EVENT\n  STAGE: before_%s\n  REASON: stop_file_present path=%s\n", stage.Name, cfg.StopFile)
			break
		}
		wsReport := ensureWSConnections(ctx, cfg, data, stage.TargetWS)
		summary := runStage(ctx, cfg, data, stage)
		summary.WSConnected = wsConnectedCount(data)
		summary.ControlWSConnected = wsStreamConnectedCount(data, "control")
		summary.MarketWSConnected = wsStreamConnectedCount(data, "market")
		summary.WSConnectFails = wsReport.Failures
		summary.WSConnectErrors = wsReport.Errors
		if summary.StopReason == "" {
			summary.StopReason = thresholdStopReason(summary)
		}
		printStageSummary(summary)
		printPrometheusTimeline(ctx, cfg, client, summary)
		summaries = append(summaries, summary)
		if summary.StopReason != "" {
			fmt.Printf("\n=== STOP_EVENT\n  STAGE: %s\n  REASON: %s\n", stage.Name, summary.StopReason)
			break
		}
	}

	fmt.Println("\n=== RECONCILE")
	fmt.Printf("  RESULT: %s\n", reconcile(ctx, cfg, data))

	fmt.Println("\n=== CLEANUP")
	fmt.Printf("  RESULT: %s\n", cleanup(ctx, cfg, data))

	printSummary(cfg, summaries)
}

func loadConfig() Config {
	batchID := getenv("PERF_BATCH_ID", "agent_perf_auction_20260603_core_bid_ws")
	stages := defaultStages()
	requestMix := getenv("PERF_REQUEST_MIX", "core_bid_80_ranking_10_item_10")
	if customStages := customQPSStages(os.Getenv("PERF_STAGE_QPS"), requestMix); len(customStages) > 0 {
		stages = customStages
	}
	stages = applyStageOverrides(filterStages(stages, envInt("PERF_START_QPS", 0), envInt("PERF_END_QPS", 0)), requestMix, envBool("PERF_DISABLE_WS", false), csvPositiveInts(os.Getenv("PERF_STAGE_WS")))
	return Config{
		BatchID:              batchID,
		Environment:          getenv("PERF_ENVIRONMENT", "single_source_online"),
		BaseURL:              strings.TrimRight(getenv("PERF_BASE_URL", "http://127.0.0.1:8080"), "/"),
		PrometheusURL:        strings.TrimRight(os.Getenv("PERF_PROMETHEUS_URL"), "/"),
		StopFile:             getenv("PERF_STOP_FILE", "docs/agent-testing/performance-runs/"+batchID+"/STOP"),
		HumanMonitor:         getenv("PERF_HUMAN_MONITOR", "user"),
		RequestTimeout:       envDuration("PERF_REQUEST_TIMEOUT", 10*time.Second),
		WSConnectTimeout:     envDuration("PERF_WS_CONNECT_TIMEOUT", 15*time.Second),
		WSConnectConcurrency: envInt("PERF_WS_CONNECT_CONCURRENCY", 8),
		WSConnectMaxAttempts: envInt("PERF_WS_CONNECT_MAX_ATTEMPTS", 700),
		WSStreamMode:         getenv("PERF_WS_STREAM_MODE", wsStreamModeAll),
		ObservabilityStep:    envDuration("PERF_OBSERVABILITY_STEP", 30*time.Second),
		UserCount:            envInt("PERF_USER_COUNT", 320),
		CleanupOnly:          envBool("PERF_CLEANUP_ONLY", false),
		Stages:               stages,
	}
}

func defaultStages() []StageConfig {
	return []StageConfig{
		{Name: "smoke_10qps_20ws", TargetQPS: 10, TargetWS: 20, Concurrency: 10, Duration: 3 * time.Minute, RequestMix: "core_bid_80_ranking_10_item_10"},
		{Name: "step_30qps_60ws", TargetQPS: 30, TargetWS: 60, Concurrency: 30, Duration: 3 * time.Minute, RequestMix: "core_bid_80_ranking_10_item_10"},
		{Name: "step_50qps_100ws", TargetQPS: 50, TargetWS: 100, Concurrency: 50, Duration: 3 * time.Minute, RequestMix: "core_bid_80_ranking_10_item_10"},
		{Name: "step_70qps_140ws", TargetQPS: 70, TargetWS: 140, Concurrency: 70, Duration: 3 * time.Minute, RequestMix: "core_bid_80_ranking_10_item_10"},
		{Name: "step_100qps_200ws", TargetQPS: 100, TargetWS: 200, Concurrency: 100, Duration: 3 * time.Minute, RequestMix: "core_bid_80_ranking_10_item_10"},
		{Name: "step_130qps_260ws", TargetQPS: 130, TargetWS: 260, Concurrency: 130, Duration: 3 * time.Minute, RequestMix: "core_bid_80_ranking_10_item_10"},
		{Name: "step_150qps_300ws", TargetQPS: 150, TargetWS: 300, Concurrency: 150, Duration: 3 * time.Minute, RequestMix: "core_bid_80_ranking_10_item_10"},
	}
}

func customQPSStages(raw string, requestMix string) []StageConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	stages := make([]StageConfig, 0, len(parts))
	for _, part := range parts {
		qps, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || qps <= 0 {
			continue
		}
		stages = append(stages, StageConfig{
			Name:        fmt.Sprintf("custom_%dqps", qps),
			TargetQPS:   float64(qps),
			TargetWS:    qps * 2,
			Concurrency: qps,
			Duration:    3 * time.Minute,
			RequestMix:  requestMix,
		})
	}
	return stages
}

func applyStageOverrides(stages []StageConfig, requestMix string, disableWS bool, wsTargets []int) []StageConfig {
	out := make([]StageConfig, 0, len(stages))
	for _, stage := range stages {
		stage.RequestMix = requestMix
		if len(wsTargets) > len(out) {
			stage.TargetWS = wsTargets[len(out)]
		}
		if disableWS {
			stage.TargetWS = 0
		}
		out = append(out, stage)
	}
	return out
}

func csvPositiveInts(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 0 {
			continue
		}
		values = append(values, value)
	}
	return values
}

func filterStages(stages []StageConfig, startQPS int, endQPS int) []StageConfig {
	if startQPS <= 0 && endQPS <= 0 {
		return stages
	}
	filtered := make([]StageConfig, 0, len(stages))
	for _, stage := range stages {
		qps := int(stage.TargetQPS)
		if startQPS > 0 && qps < startQPS {
			continue
		}
		if endQPS > 0 && qps > endQPS {
			continue
		}
		if qps > 0 {
			filtered = append(filtered, stage)
		}
	}
	return filtered
}

func setupData(ctx context.Context, cfg Config, client *http.Client) (*TestData, error) {
	data := &TestData{HTTPClient: client, WSStats: newWSStats()}
	password := batchPassword(cfg.BatchID)

	merchantToken, merchantID, err := register(ctx, cfg, client, merchantAccount(cfg.BatchID), password)
	if err != nil {
		return nil, fmt.Errorf("register merchant: %w", err)
	}
	data.MerchantToken = merchantToken
	data.MerchantUser = merchantID
	if err := putJSON(ctx, cfg, client, "/api/v1/users/me", merchantToken, map[string]any{
		"name":     merchantDisplayName(cfg.BatchID),
		"identity": "merchant",
	}, nil); err != nil {
		return nil, fmt.Errorf("promote merchant: %w", err)
	}

	var room struct {
		ID string `json:"id"`
	}
	if err := postJSON(ctx, cfg, client, "/api/v1/merchant/room", merchantToken, map[string]any{
		"title": "agent_perf_room_" + cfg.BatchID,
	}, &room); err != nil {
		return nil, fmt.Errorf("activate room: %w", err)
	}
	data.RoomID = room.ID
	_ = postJSON(ctx, cfg, client, "/api/v1/rooms/"+url.PathEscape(data.RoomID)+"/start", merchantToken, nil, nil)

	now := time.Now().Add(-1 * time.Minute)
	end := time.Now().Add(2 * time.Hour)
	var item struct {
		ItemID string `json:"item_id"`
	}
	if err := postJSON(ctx, cfg, client, "/api/v1/items", merchantToken, map[string]any{
		"room_id":     data.RoomID,
		"title":       "agent_perf_item_" + cfg.BatchID,
		"description": "agent performance test item",
		"image_url":   "https://example.com/agent-perf.png",
		"tags":        []string{"agent", "performance"},
		"rule": map[string]any{
			"start_price":   1000,
			"bid_increment": 100,
			"start_time":    now.Format(time.RFC3339),
			"end_time":      end.Format(time.RFC3339),
		},
	}, &item); err != nil {
		return nil, fmt.Errorf("create item: %w", err)
	}
	data.ItemID = item.ItemID
	if err := postJSON(ctx, cfg, client, "/api/v1/items/"+url.PathEscape(data.ItemID)+"/publish", merchantToken, nil, nil); err != nil {
		return nil, fmt.Errorf("publish item: %w", err)
	}
	if err := postJSON(ctx, cfg, client, "/api/v1/items/"+url.PathEscape(data.ItemID)+"/start", merchantToken, nil, nil); err != nil {
		return nil, fmt.Errorf("start item: %w", err)
	}

	for i := 0; i < cfg.UserCount; i++ {
		token, userID, err := register(ctx, cfg, client, userAccount(cfg.BatchID, i), password)
		if err != nil {
			return nil, fmt.Errorf("register user %d: %w", i, err)
		}
		data.UserTokens = append(data.UserTokens, token)
		data.UserIDs = append(data.UserIDs, userID)
	}

	fmt.Println("  TEST_DATA: created batch-scoped merchant, room, item, and users")
	fmt.Printf("  TEST_DATA_COUNTS: users=%d ws_target_max=%d\n", len(data.UserTokens), maxTargetWS(cfg.Stages))
	return data, nil
}

func register(ctx context.Context, cfg Config, client *http.Client, account string, password string) (string, string, error) {
	return auth(ctx, cfg, client, "/api/v1/auth/register", account, password)
}

func login(ctx context.Context, cfg Config, client *http.Client, account string, password string) (string, string, error) {
	return auth(ctx, cfg, client, "/api/v1/auth/login", account, password)
}

func auth(ctx context.Context, cfg Config, client *http.Client, path string, account string, password string) (string, string, error) {
	var result struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	err := postJSON(ctx, cfg, client, path, "", map[string]any{
		"account":  account,
		"password": password,
	}, &result)
	if err != nil {
		return "", "", err
	}
	if result.Token == "" || result.User.ID == "" {
		return "", "", fmt.Errorf("missing token or user id")
	}
	return result.Token, result.User.ID, nil
}

func buildRequest(cfg Config, data *TestData, stage StageConfig, workerID int, n uint64) RequestSpec {
	userIdx := int(n) % len(data.UserTokens)
	if stage.RequestMix == "bid_only" {
		return RequestSpec{Method: http.MethodPost, Path: "/api/v1/items/" + url.PathEscape(data.ItemID) + "/bids", Token: data.UserTokens[userIdx]}
	}
	if stage.RequestMix == "item_only" {
		return RequestSpec{Method: http.MethodGet, Path: "/api/v1/items/" + url.PathEscape(data.ItemID)}
	}
	switch n % 10 {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		return RequestSpec{Method: http.MethodPost, Path: "/api/v1/items/" + url.PathEscape(data.ItemID) + "/bids", Token: data.UserTokens[userIdx]}
	case 8:
		return RequestSpec{Method: http.MethodGet, Path: "/api/v1/items/" + url.PathEscape(data.ItemID) + "/ranking?page=1&page_size=20"}
	}
	return RequestSpec{Method: http.MethodGet, Path: "/api/v1/items/" + url.PathEscape(data.ItemID)}
}

func ensureWSConnections(ctx context.Context, cfg Config, data *TestData, target int) wsConnectReport {
	return ensureWSConnectionsWith(ctx, cfg, data, target, connectWSForUser)
}

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
	pending := wsMissingTargets(data, target, streams)
	if len(pending) == 0 {
		return report
	}
	maxAttempts := cfg.WSConnectMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = len(pending) * 2
	}
	attempts := 0
	for len(pending) > 0 && attempts < maxAttempts {
		if stopRequested(cfg.StopFile) {
			break
		}
		remaining := maxAttempts - attempts
		attemptList := pending
		deferred := []wsConnectTarget(nil)
		if len(attemptList) > remaining {
			attemptList = pending[:remaining]
			deferred = pending[remaining:]
		}
		attempts += len(attemptList)
		batchReport := connectWSBatch(ctx, cfg, data, attemptList, connector)
		report.Failures += batchReport.Failures
		for reason, count := range batchReport.Errors {
			report.Errors[reason] += count
		}
		pending = append(batchReport.FailedTargets, deferred...)
	}
	return report
}

type wsConnectBatchReport struct {
	FailedTargets []wsConnectTarget
	Failures      int
	Errors        map[string]int64
}

type wsConnectTarget struct {
	UserIndex int
	Stream    string
}

func connectWSBatch(ctx context.Context, cfg Config, data *TestData, targets []wsConnectTarget, connector wsConnector) wsConnectBatchReport {
	if len(targets) == 0 {
		return wsConnectBatchReport{Errors: map[string]int64{}}
	}
	workers := cfg.WSConnectConcurrency
	if workers <= 0 {
		workers = 1
	}
	if workers > len(targets) {
		workers = len(targets)
	}
	jobs := make(chan wsConnectTarget)
	failed := make(chan wsConnectFailure, len(targets))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for workerID := 0; workerID < workers; workerID++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range jobs {
				if stopRequested(cfg.StopFile) {
					failed <- wsConnectFailure{Target: target, Reason: "stop_requested"}
					continue
				}
				mu.Lock()
				alreadyConnected := wsTargetConnectedLocked(data, target)
				mu.Unlock()
				if alreadyConnected {
					continue
				}
				conn, err := connector(ctx, cfg, data, target.UserIndex, target.Stream)
				if err != nil {
					failed <- wsConnectFailure{Target: target, Reason: classifyWSError(err)}
					continue
				}
				mu.Lock()
				markWSTargetConnectedLocked(data, target)
				if conn != nil {
					data.WSConns = append(data.WSConns, conn)
				}
				mu.Unlock()
				if conn != nil {
					go drainWS(data, target.UserIndex, target.Stream, conn)
				}
			}
		}()
	}

	for _, target := range targets {
		jobs <- target
	}
	close(jobs)
	wg.Wait()
	close(failed)

	var failedTargets []wsConnectTarget
	report := wsConnectBatchReport{Errors: map[string]int64{}}
	for failure := range failed {
		report.Failures++
		report.Errors[failure.Reason]++
		failedTargets = append(failedTargets, failure.Target)
	}
	sort.Slice(failedTargets, func(i, j int) bool {
		if failedTargets[i].UserIndex == failedTargets[j].UserIndex {
			return failedTargets[i].Stream < failedTargets[j].Stream
		}
		return failedTargets[i].UserIndex < failedTargets[j].UserIndex
	})
	report.FailedTargets = failedTargets
	return report
}

type wsConnectFailure struct {
	Target wsConnectTarget
	Reason string
}

func connectWSForUser(ctx context.Context, cfg Config, data *TestData, userIndex int, stream string) (*websocketConn, error) {
	ticket, err := issueTicket(ctx, cfg, data.HTTPClient, data.UserTokens[userIndex])
	if err != nil {
		return nil, fmt.Errorf("ticket: %w", err)
	}
	dialCtx := ctx
	cancel := func() {}
	if cfg.WSConnectTimeout > 0 {
		dialCtx, cancel = context.WithTimeout(ctx, cfg.WSConnectTimeout)
	}
	defer cancel()
	dialer := websocket.Dialer{HandshakeTimeout: cfg.WSConnectTimeout}
	conn, resp, err := dialer.DialContext(dialCtx, wsURL(cfg.BaseURL, data.RoomID, ticket, stream), nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial_status:%d: %w", resp.StatusCode, err)
		}
		return nil, err
	}
	return conn, nil
}

func classifyWSError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "ticket:"):
		return "ticket:" + compactErrReason(strings.TrimSpace(strings.TrimPrefix(msg, "ticket:")))
	case strings.HasPrefix(msg, "dial_status:"):
		parts := strings.SplitN(msg, ":", 3)
		if len(parts) >= 2 {
			return "dial_status:" + parts[1]
		}
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "Client.Timeout"), strings.Contains(msg, "context deadline exceeded"):
		return "dial:timeout"
	case strings.Contains(msg, "connection reset"):
		return "dial:connection_reset"
	case strings.Contains(msg, "connection refused"):
		return "dial:connection_refused"
	}
	return "dial:" + compactErrReason(msg)
}

func compactErrReason(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "unknown"
	}
	if len(msg) > 80 {
		msg = msg[:80]
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		default:
			return r
		}
	}, msg)
}

func ensureWSConnectionMaps(data *TestData) {
	if data.WSUsers == nil {
		data.WSUsers = make(map[int]bool)
	}
	if data.WSStreamUsers == nil {
		data.WSStreamUsers = make(map[string]map[int]bool)
	}
	if len(data.WSUsers) > 0 || hasWSStreamUsers(data) {
		return
	}
	for i := 0; i < len(data.WSConns) && i < len(data.UserTokens); i++ {
		data.WSUsers[i] = true
	}
}

func hasWSStreamUsers(data *TestData) bool {
	for _, users := range data.WSStreamUsers {
		if len(users) > 0 {
			return true
		}
	}
	return false
}

func wsTargetStreams(cfg Config) []string {
	switch cfg.WSStreamMode {
	case wsStreamModeControlMarket:
		return []string{"control", "market"}
	default:
		return []string{"all"}
	}
}

func wsMissingTargets(data *TestData, target int, streams []string) []wsConnectTarget {
	ensureWSConnectionMaps(data)
	if target > len(data.UserTokens) {
		target = len(data.UserTokens)
	}
	missing := make([]wsConnectTarget, 0, target*len(streams))
	for userIndex := 0; userIndex < target; userIndex++ {
		for _, stream := range streams {
			target := wsConnectTarget{UserIndex: userIndex, Stream: stream}
			if !wsTargetConnectedLocked(data, target) {
				missing = append(missing, target)
			}
		}
	}
	return missing
}

func wsConnectedCount(data *TestData) int {
	ensureWSConnectionMaps(data)
	return len(data.WSUsers) + wsStreamConnectedCount(data, "control") + wsStreamConnectedCount(data, "market")
}

func wsStreamConnectedCount(data *TestData, stream string) int {
	ensureWSConnectionMaps(data)
	return len(data.WSStreamUsers[stream])
}

func wsTargetConnectedLocked(data *TestData, target wsConnectTarget) bool {
	if target.Stream == "" || target.Stream == "all" {
		return data.WSUsers[target.UserIndex]
	}
	return data.WSStreamUsers[target.Stream][target.UserIndex]
}

func markWSTargetConnectedLocked(data *TestData, target wsConnectTarget) {
	if target.Stream == "" || target.Stream == "all" {
		data.WSUsers[target.UserIndex] = true
		return
	}
	if data.WSStreamUsers[target.Stream] == nil {
		data.WSStreamUsers[target.Stream] = make(map[int]bool)
	}
	data.WSStreamUsers[target.Stream][target.UserIndex] = true
}

func issueTicket(ctx context.Context, cfg Config, client *http.Client, token string) (string, error) {
	var result struct {
		Ticket string `json:"ticket"`
	}
	if err := postJSON(ctx, cfg, client, "/api/v1/ws-ticket", token, nil, &result); err != nil {
		return "", err
	}
	if result.Ticket == "" {
		return "", fmt.Errorf("missing ticket")
	}
	return result.Ticket, nil
}

func newWSStats() *WSStats {
	return &WSStats{
		eventCounts:         map[string]int64{},
		timeSyncLast:        map[int]time.Time{},
		controlTimeSyncLast: map[int]time.Time{},
	}
}

func drainWS(data *TestData, userIndex int, stream string, conn *websocket.Conn) {
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if data != nil && data.WSStats != nil {
			data.WSStats.recordStream(userIndex, stream, payload)
		}
	}
}

func (s *WSStats) record(userIndex int, payload []byte) {
	s.recordStream(userIndex, "all", payload)
}

func (s *WSStats) recordStream(userIndex int, stream string, payload []byte) {
	var event struct {
		Type    string `json:"type"`
		Payload struct {
			ServerTimeUnixMS int64 `json:"server_time_unix_ms"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &event); err != nil || event.Type == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventCounts[event.Type]++
	if event.Type != "time_sync" {
		return
	}
	s.timeSyncCount++
	if last, ok := s.timeSyncLast[userIndex]; ok {
		s.timeSyncSamples = append(s.timeSyncSamples, now.Sub(last))
	}
	s.timeSyncLast[userIndex] = now
	if stream != "control" {
		return
	}
	s.controlTimeSyncCount++
	if event.Payload.ServerTimeUnixMS > 0 {
		s.controlTimeSyncArrivalDelaySamples = append(s.controlTimeSyncArrivalDelaySamples, now.Sub(time.UnixMilli(event.Payload.ServerTimeUnixMS)))
	}
	if last, ok := s.controlTimeSyncLast[userIndex]; ok {
		s.controlTimeSyncIntervalSamples = append(s.controlTimeSyncIntervalSamples, now.Sub(last))
	}
	s.controlTimeSyncLast[userIndex] = now
}

func (s *WSStats) snapshot() WSStatsSnapshot {
	if s == nil {
		return WSStatsSnapshot{EventCountsLen: map[string]int64{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[string]int64, len(s.eventCounts))
	for eventType, count := range s.eventCounts {
		counts[eventType] = count
	}
	return WSStatsSnapshot{
		EventCountsLen:                       counts,
		TimeSyncCount:                        s.timeSyncCount,
		TimeSyncSampleLen:                    len(s.timeSyncSamples),
		ControlTimeSyncCount:                 s.controlTimeSyncCount,
		ControlTimeSyncArrivalDelaySampleLen: len(s.controlTimeSyncArrivalDelaySamples),
		ControlTimeSyncIntervalSampleLen:     len(s.controlTimeSyncIntervalSamples),
	}
}

func (s *WSStats) deltaSince(snapshot WSStatsSnapshot) (map[string]int64, int64, int64, []time.Duration, []time.Duration, []time.Duration) {
	if s == nil {
		return map[string]int64{}, 0, 0, nil, nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[string]int64, len(s.eventCounts))
	for eventType, count := range s.eventCounts {
		counts[eventType] = count - snapshot.EventCountsLen[eventType]
	}
	timeSyncCount := s.timeSyncCount - snapshot.TimeSyncCount
	controlTimeSyncCount := s.controlTimeSyncCount - snapshot.ControlTimeSyncCount
	if snapshot.TimeSyncSampleLen > len(s.timeSyncSamples) {
		snapshot.TimeSyncSampleLen = len(s.timeSyncSamples)
	}
	samples := append([]time.Duration(nil), s.timeSyncSamples[snapshot.TimeSyncSampleLen:]...)
	if snapshot.ControlTimeSyncArrivalDelaySampleLen > len(s.controlTimeSyncArrivalDelaySamples) {
		snapshot.ControlTimeSyncArrivalDelaySampleLen = len(s.controlTimeSyncArrivalDelaySamples)
	}
	if snapshot.ControlTimeSyncIntervalSampleLen > len(s.controlTimeSyncIntervalSamples) {
		snapshot.ControlTimeSyncIntervalSampleLen = len(s.controlTimeSyncIntervalSamples)
	}
	controlArrivalDelaySamples := append([]time.Duration(nil), s.controlTimeSyncArrivalDelaySamples[snapshot.ControlTimeSyncArrivalDelaySampleLen:]...)
	controlIntervalSamples := append([]time.Duration(nil), s.controlTimeSyncIntervalSamples[snapshot.ControlTimeSyncIntervalSampleLen:]...)
	return counts, timeSyncCount, controlTimeSyncCount, samples, controlArrivalDelaySamples, controlIntervalSamples
}

func runStage(ctx context.Context, cfg Config, data *TestData, stage StageConfig) StageSummary {
	start := time.Now()
	wsSnapshot := data.WSStats.snapshot()
	stageCtx, cancel := context.WithTimeout(ctx, stage.Duration)
	defer cancel()

	results := make(chan RequestResult, stage.Concurrency*2)
	tokens := make(chan struct{}, stage.Concurrency*2)
	var wg sync.WaitGroup
	go produceTokens(stageCtx, stage.TargetQPS, tokens)

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
					results <- doRequest(stageCtx, cfg, data, stage, id, n)
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
		TargetWS:      stage.TargetWS,
		WSStreamMode:  cfg.WSStreamMode,
		Concurrency:   stage.Concurrency,
		StatusCodes:   map[int]int64{},
		BusinessCodes: map[string]int64{},
	}
	var latencies []time.Duration
	for result := range results {
		summary.Total++
		latencies = append(latencies, result.Duration)
		if result.Duration > summary.Max {
			summary.Max = result.Duration
		}
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
		} else if result.ExpectedBusinessReject || isExpectedBusinessReject(result.BusinessCode) {
			summary.ExpectedBusinessRejects++
		} else if result.StatusCode >= 200 && result.StatusCode < 500 {
			summary.BusinessFails++
		} else {
			summary.HTTPFailures++
		}
	}
	summary.End = time.Now()
	wsEventCounts, timeSyncCount, controlTimeSyncCount, timeSyncSamples, controlArrivalDelaySamples, controlIntervalSamples := data.WSStats.deltaSince(wsSnapshot)
	summary.WSEventCounts = wsEventCounts
	summary.TimeSyncCount = timeSyncCount
	summary.ControlTimeSyncCount = controlTimeSyncCount
	elapsed := summary.End.Sub(summary.Start).Seconds()
	if elapsed > 0 {
		summary.ActualQPS = float64(summary.Total) / elapsed
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	summary.P50 = percentile(latencies, 0.50)
	summary.P95 = percentile(latencies, 0.95)
	summary.P99 = percentile(latencies, 0.99)
	sort.Slice(timeSyncSamples, func(i, j int) bool { return timeSyncSamples[i] < timeSyncSamples[j] })
	summary.TimeSyncP50 = percentile(timeSyncSamples, 0.50)
	summary.TimeSyncP95 = percentile(timeSyncSamples, 0.95)
	summary.TimeSyncP99 = percentile(timeSyncSamples, 0.99)
	sort.Slice(controlArrivalDelaySamples, func(i, j int) bool { return controlArrivalDelaySamples[i] < controlArrivalDelaySamples[j] })
	summary.ControlTimeSyncArrivalDelayP50 = percentile(controlArrivalDelaySamples, 0.50)
	summary.ControlTimeSyncArrivalDelayP95 = percentile(controlArrivalDelaySamples, 0.95)
	summary.ControlTimeSyncArrivalDelayP99 = percentile(controlArrivalDelaySamples, 0.99)
	sort.Slice(controlIntervalSamples, func(i, j int) bool { return controlIntervalSamples[i] < controlIntervalSamples[j] })
	summary.ControlTimeSyncIntervalP50 = percentile(controlIntervalSamples, 0.50)
	summary.ControlTimeSyncIntervalP95 = percentile(controlIntervalSamples, 0.95)
	summary.ControlTimeSyncIntervalP99 = percentile(controlIntervalSamples, 0.99)
	for _, sample := range timeSyncSamples {
		if sample > summary.TimeSyncMax {
			summary.TimeSyncMax = sample
		}
	}
	if stopRequested(cfg.StopFile) {
		summary.StopReason = "stop_file_present"
	}
	return summary
}

func doRequest(ctx context.Context, cfg Config, data *TestData, stage StageConfig, workerID int, n uint64) RequestResult {
	spec := buildRequest(cfg, data, stage, workerID, n)
	if spec.Method == http.MethodPost && strings.HasSuffix(spec.Path, "/bids") {
		bidSpec, err := buildBidRequest(ctx, cfg, data, stage, workerID, n)
		if err != nil {
			return RequestResult{Err: "build_bid_request: " + err.Error()}
		}
		spec = bidSpec
	}
	var body io.Reader
	if len(spec.Body) > 0 {
		body = bytes.NewReader(spec.Body)
	}
	req, err := http.NewRequestWithContext(ctx, spec.Method, cfg.BaseURL+spec.Path, body)
	if err != nil {
		return RequestResult{Err: err.Error()}
	}
	if len(spec.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if spec.Token != "" {
		req.Header.Set("Authorization", "Bearer "+spec.Token)
	}
	req.Header.Set("X-Agent-Test-Batch", cfg.BatchID)
	req.Header.Set("X-Agent-Perf-Stage", stage.Name)

	start := time.Now()
	resp, err := data.HTTPClient.Do(req)
	duration := time.Since(start)
	if err != nil {
		return RequestResult{Duration: duration, Err: err.Error()}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	code := classifyBusiness(respBody)
	return RequestResult{
		StatusCode:             resp.StatusCode,
		BusinessCode:           code,
		Duration:               duration,
		ExpectedBusinessReject: isExpectedBusinessReject(code),
	}
}

func buildBidRequest(ctx context.Context, cfg Config, data *TestData, stage StageConfig, workerID int, n uint64) (RequestSpec, error) {
	userIdx := int(n) % len(data.UserTokens)
	bidNo := bidSeq.Add(1)
	price := int64(1000 + bidNo*100)
	body, _ := json.Marshal(map[string]any{
		"price":           price,
		"idempotency_key": fmt.Sprintf("%s_%s_%d", cfg.BatchID, stage.Name, n),
	})
	return RequestSpec{Method: http.MethodPost, Path: "/api/v1/items/" + url.PathEscape(data.ItemID) + "/bids", Token: data.UserTokens[userIdx], Body: body}, nil
}

func readCurrentBidView(ctx context.Context, cfg Config, data *TestData) (int64, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/api/v1/items/"+url.PathEscape(data.ItemID), nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("X-Agent-Test-Batch", cfg.BatchID)
	resp, err := data.HTTPClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			CurrentPrice int64 `json:"current_price"`
			Rule         struct {
				BidIncrement int64 `json:"bid_increment"`
			} `json:"rule"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return 0, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Code != 0 {
		return 0, 0, fmt.Errorf("item detail status=%d code=%d msg=%s", resp.StatusCode, envelope.Code, envelope.Message)
	}
	if envelope.Data.CurrentPrice <= 0 {
		return 0, 0, fmt.Errorf("missing current_price")
	}
	return envelope.Data.CurrentPrice, envelope.Data.Rule.BidIncrement, nil
}

func reconcile(ctx context.Context, cfg Config, data *TestData) string {
	itemOK := probe(ctx, cfg, data.HTTPClient, "/api/v1/items/"+url.PathEscape(data.ItemID))
	rankingOK := probe(ctx, cfg, data.HTTPClient, "/api/v1/items/"+url.PathEscape(data.ItemID)+"/ranking?page=1&page_size=10")
	roomOK := probe(ctx, cfg, data.HTTPClient, "/api/v1/rooms/"+url.PathEscape(data.RoomID))
	wsSnapshot := data.WSStats.snapshot()
	return fmt.Sprintf("item_detail=%s ranking=%s room=%s ws_connected=%d bid_attempts=%d ws_events=%s time_sync_count=%d", itemOK, rankingOK, roomOK, wsConnectedCount(data), bidSeq.Load(), jsonLine(wsSnapshot.EventCountsLen), wsSnapshot.TimeSyncCount)
}

func cleanup(ctx context.Context, cfg Config, data *TestData) string {
	for _, c := range data.WSConns {
		if c == nil {
			continue
		}
		_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "cleanup"), time.Now().Add(time.Second))
		_ = c.Close()
	}
	parts := []string{fmt.Sprintf("closed_ws=%d", len(data.WSConns))}
	if data.ItemID != "" {
		err := postJSON(ctx, cfg, data.HTTPClient, "/api/v1/items/"+url.PathEscape(data.ItemID)+"/cancel", data.MerchantToken, nil, nil)
		parts = append(parts, "cancel_item="+okErr(err))
	}
	if data.RoomID != "" {
		err := postJSON(ctx, cfg, data.HTTPClient, "/api/v1/rooms/"+url.PathEscape(data.RoomID)+"/end", data.MerchantToken, nil, nil)
		parts = append(parts, "end_room="+okErr(err))
	}
	for _, token := range data.UserTokens {
		_ = deleteJSON(ctx, cfg, data.HTTPClient, "/api/v1/users/me", token)
	}
	if data.MerchantToken != "" {
		_ = deleteJSON(ctx, cfg, data.HTTPClient, "/api/v1/users/me", data.MerchantToken)
	}
	parts = append(parts, fmt.Sprintf("delete_users_attempted=%d", len(data.UserTokens)+1))
	return strings.Join(parts, " ")
}

type cleanupMerchantItem struct {
	ID     string `json:"id"`
	RoomID string `json:"room_id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

func cleanupBatchOnly(ctx context.Context, cfg Config, client *http.Client) string {
	password := batchPassword(cfg.BatchID)
	parts := []string{fmt.Sprintf("batch_id=%s", cfg.BatchID)}

	merchantToken, _, merchantErr := login(ctx, cfg, client, merchantAccount(cfg.BatchID), password)
	if merchantErr != nil {
		parts = append(parts, "merchant_login=err")
	} else {
		parts = append(parts, "merchant_login=ok")
		items, listErr := listBatchMerchantItems(ctx, cfg, client, merchantToken)
		cancelOK, cancelErr := 0, 0
		if listErr != nil {
			parts = append(parts, "list_items=err")
		} else {
			for _, item := range items {
				if !isBatchMerchantItem(cfg.BatchID, item) {
					continue
				}
				err := postJSON(ctx, cfg, client, "/api/v1/items/"+url.PathEscape(item.ID)+"/cancel", merchantToken, nil, nil)
				if err == nil {
					cancelOK++
				} else {
					cancelErr++
				}
			}
			parts = append(parts, fmt.Sprintf("batch_items_seen=%d cancel_ok=%d cancel_err=%d", len(items), cancelOK, cancelErr))
		}
		var room struct {
			ID string `json:"id"`
		}
		if err := getJSON(ctx, cfg, client, "/api/v1/merchant/room", merchantToken, &room); err != nil || room.ID == "" {
			parts = append(parts, "end_room=skip")
		} else {
			parts = append(parts, "end_room="+okErr(postJSON(ctx, cfg, client, "/api/v1/rooms/"+url.PathEscape(room.ID)+"/end", merchantToken, nil, nil)))
		}
	}

	loginOK, deleteOK, deleteErr := 0, 0, 0
	for i := 0; i < cfg.UserCount; i++ {
		token, _, err := login(ctx, cfg, client, userAccount(cfg.BatchID, i), password)
		if err != nil {
			continue
		}
		loginOK++
		if err := deleteJSON(ctx, cfg, client, "/api/v1/users/me", token); err != nil {
			deleteErr++
		} else {
			deleteOK++
		}
	}
	parts = append(parts, fmt.Sprintf("user_login_ok=%d user_delete_ok=%d user_delete_err=%d user_accounts_scanned=%d", loginOK, deleteOK, deleteErr, cfg.UserCount))
	if merchantToken != "" {
		parts = append(parts, "delete_merchant="+okErr(deleteJSON(ctx, cfg, client, "/api/v1/users/me", merchantToken)))
	}
	return strings.Join(parts, " ")
}

func listBatchMerchantItems(ctx context.Context, cfg Config, client *http.Client, token string) ([]cleanupMerchantItem, error) {
	var result struct {
		List []cleanupMerchantItem `json:"list"`
	}
	path := "/api/v1/merchant/items?keyword=" + url.QueryEscape(cfg.BatchID) + "&page=1&page_size=100"
	if err := getJSON(ctx, cfg, client, path, token, &result); err != nil {
		return nil, err
	}
	return result.List, nil
}

func isBatchMerchantItem(batchID string, item cleanupMerchantItem) bool {
	return strings.Contains(item.Title, batchID) && strings.HasPrefix(item.Title, "agent_perf_item_")
}

func batchPassword(batchID string) string {
	return "PerfPass_" + compactBatch(batchID)
}

func merchantAccount(batchID string) string {
	return compactBatch(batchID) + "_m"
}

func userAccount(batchID string, index int) string {
	return fmt.Sprintf("%s_u%03d", compactBatch(batchID), index)
}

func postJSON(ctx context.Context, cfg Config, client *http.Client, path string, token string, body any, out any) error {
	return doJSON(ctx, cfg, client, http.MethodPost, path, token, body, out)
}

func getJSON(ctx context.Context, cfg Config, client *http.Client, path string, token string, out any) error {
	return doJSON(ctx, cfg, client, http.MethodGet, path, token, nil, out)
}

func putJSON(ctx context.Context, cfg Config, client *http.Client, path string, token string, body any, out any) error {
	return doJSON(ctx, cfg, client, http.MethodPut, path, token, body, out)
}

func deleteJSON(ctx context.Context, cfg Config, client *http.Client, path string, token string) error {
	return doJSON(ctx, cfg, client, http.MethodDelete, path, token, nil, nil)
}

func doJSON(ctx context.Context, cfg Config, client *http.Client, method string, path string, token string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Agent-Test-Batch", cfg.BatchID)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var envelope apiResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("status=%d unparsed_response", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Code != 0 {
		return fmt.Errorf("status=%d code=%d msg=%s", resp.StatusCode, envelope.Code, envelope.Message)
	}
	if out != nil && len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return err
		}
	}
	return nil
}

func classifyBusiness(body []byte) string {
	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "unparsed"
	}
	return strconv.Itoa(parsed.Code)
}

func isBusinessSuccess(status int, code string) bool {
	return status >= 200 && status < 300 && (code == "" || code == "0")
}

func isExpectedBusinessReject(code string) bool {
	return code == "40003"
}

func thresholdStopReason(s StageSummary) string {
	if ratio(s.HTTPFailures+s.BusinessFails, s.Total) > 0.03 {
		return "error_rate_gt_3_percent"
	}
	if ratio(s.Timeouts, s.Total) > 0.03 {
		return "timeout_rate_gt_3_percent"
	}
	if s.WSStreamMode == wsStreamModeControlMarket {
		return thresholdStopReasonSplitStreams(s)
	}
	return thresholdStopReasonAllStream(s)
}

func thresholdStopReasonAllStream(s StageSummary) string {
	if s.TargetWS > 0 && s.WSConnected < int(float64(s.TargetWS)*0.95) {
		return "ws_connection_success_lt_95_percent"
	}
	if s.TargetWS > 0 && s.End.After(s.Start) {
		minExpected := int64(float64(s.TargetWS) * s.End.Sub(s.Start).Seconds() * 0.5)
		if minExpected > 0 && s.TimeSyncCount < minExpected {
			return "time_sync_missing_or_low_rate"
		}
	}
	if s.TargetWS > 0 && s.TimeSyncCount > 0 && s.TimeSyncP95 > 3*time.Second {
		return "time_sync_p95_interval_gt_3s"
	}
	return ""
}

func thresholdStopReasonSplitStreams(s StageSummary) string {
	if s.TargetWS > 0 {
		minConnected := int(float64(s.TargetWS) * 0.95)
		if s.ControlWSConnected < minConnected || s.MarketWSConnected < minConnected {
			return "ws_connection_success_lt_95_percent"
		}
	}
	if s.TargetWS > 0 && s.End.After(s.Start) {
		minExpected := int64(float64(s.TargetWS) * s.End.Sub(s.Start).Seconds() * 0.5)
		if minExpected > 0 && s.ControlTimeSyncCount < minExpected {
			return "time_sync_missing_or_low_rate"
		}
	}
	if s.TargetWS > 0 && s.ControlTimeSyncIntervalP95 > 3*time.Second {
		return "time_sync_p95_interval_gt_3s"
	}
	return ""
}

func printPlan(cfg Config) {
	fmt.Println("=== PERF_PLAN")
	fmt.Printf("  BATCH_ID: %s\n", cfg.BatchID)
	fmt.Printf("  ENVIRONMENT: %s\n", cfg.Environment)
	fmt.Printf("  BASE_URL: %s\n", redactURL(cfg.BaseURL))
	fmt.Printf("  PROMETHEUS: %s\n", present(cfg.PrometheusURL))
	fmt.Printf("  HUMAN_MONITOR: %s\n", cfg.HumanMonitor)
	fmt.Printf("  STOP_FILE: %s\n", cfg.StopFile)
	fmt.Printf("  USER_COUNT: %d\n", cfg.UserCount)
	fmt.Printf("  WS_CONNECT: concurrency=%d timeout=%s max_attempts=%d\n", cfg.WSConnectConcurrency, cfg.WSConnectTimeout, cfg.WSConnectMaxAttempts)
	fmt.Printf("  WS_STREAM_MODE: %s\n", cfg.WSStreamMode)
	fmt.Printf("  OBSERVABILITY_STEP: %s\n", cfg.ObservabilityStep)
	for _, s := range cfg.Stages {
		fmt.Printf("  STAGE_CONFIG: name=%s qps=%.2f ws=%d concurrency=%d duration=%s mix=%s\n", s.Name, s.TargetQPS, s.TargetWS, s.Concurrency, s.Duration, s.RequestMix)
	}
}

func printPreflight(ctx context.Context, cfg Config, client *http.Client) {
	fmt.Println("\n=== PREFLIGHT")
	status, err := probeStatus(ctx, cfg, client, "/health")
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
	fmt.Printf("  TARGET_WS: %d\n", s.TargetWS)
	fmt.Printf("  WS_STREAM_MODE: %s\n", s.WSStreamMode)
	fmt.Printf("  WS_CONNECTED: %d\n", s.WSConnected)
	fmt.Printf("  CONTROL_WS_CONNECTED: %d\n", s.ControlWSConnected)
	fmt.Printf("  MARKET_WS_CONNECTED: %d\n", s.MarketWSConnected)
	fmt.Printf("  WS_CONNECT_FAILS: %d\n", s.WSConnectFails)
	fmt.Printf("  WS_CONNECT_ERRORS: %s\n", jsonLine(s.WSConnectErrors))
	fmt.Printf("  WS_EVENT_COUNTS: %s\n", jsonLine(s.WSEventCounts))
	fmt.Printf("  TIME_SYNC_COUNT: %d\n", s.TimeSyncCount)
	fmt.Printf("  CONTROL_TIME_SYNC_COUNT: %d\n", s.ControlTimeSyncCount)
	fmt.Printf("  TIME_SYNC_P50: %s\n", s.TimeSyncP50)
	fmt.Printf("  TIME_SYNC_P95: %s\n", s.TimeSyncP95)
	fmt.Printf("  TIME_SYNC_P99: %s\n", s.TimeSyncP99)
	fmt.Printf("  TIME_SYNC_MAX: %s\n", s.TimeSyncMax)
	fmt.Printf("  CONTROL_TIME_SYNC_ARRIVAL_DELAY_P50: %s\n", s.ControlTimeSyncArrivalDelayP50)
	fmt.Printf("  CONTROL_TIME_SYNC_ARRIVAL_DELAY_P95: %s\n", s.ControlTimeSyncArrivalDelayP95)
	fmt.Printf("  CONTROL_TIME_SYNC_ARRIVAL_DELAY_P99: %s\n", s.ControlTimeSyncArrivalDelayP99)
	fmt.Printf("  CONTROL_TIME_SYNC_INTERVAL_P50: %s\n", s.ControlTimeSyncIntervalP50)
	fmt.Printf("  CONTROL_TIME_SYNC_INTERVAL_P95: %s\n", s.ControlTimeSyncIntervalP95)
	fmt.Printf("  CONTROL_TIME_SYNC_INTERVAL_P99: %s\n", s.ControlTimeSyncIntervalP99)
	fmt.Printf("  CONCURRENCY: %d\n", s.Concurrency)
	fmt.Printf("  TOTAL: %d\n", s.Total)
	fmt.Printf("  SUCCESS: %d\n", s.Success)
	fmt.Printf("  HTTP_FAILURES: %d\n", s.HTTPFailures)
	fmt.Printf("  BUSINESS_FAILS: %d\n", s.BusinessFails)
	fmt.Printf("  EXPECTED_BUSINESS_REJECTS: %d\n", s.ExpectedBusinessRejects)
	fmt.Printf("  TIMEOUTS: %d\n", s.Timeouts)
	fmt.Printf("  ERROR_RATE: %.4f\n", ratio(s.HTTPFailures+s.BusinessFails, s.Total))
	fmt.Printf("  TIMEOUT_RATE: %.4f\n", ratio(s.Timeouts, s.Total))
	fmt.Printf("  BUSINESS_FAILURE_RATE: %.4f\n", ratio(s.BusinessFails, s.Total))
	fmt.Printf("  EXPECTED_BUSINESS_REJECT_RATE: %.4f\n", ratio(s.ExpectedBusinessRejects, s.Total))
	fmt.Printf("  CLIENT_E2E_P50: %s\n", s.P50)
	fmt.Printf("  CLIENT_E2E_P95: %s\n", s.P95)
	fmt.Printf("  CLIENT_E2E_P99: %s\n", s.P99)
	fmt.Printf("  CLIENT_E2E_MAX: %s\n", s.Max)
	fmt.Printf("  STATUS_CODES: %s\n", jsonLine(s.StatusCodes))
	fmt.Printf("  BUSINESS_CODES: %s\n", jsonLine(s.BusinessCodes))
	if s.StopReason != "" {
		fmt.Printf("  STOP_REASON: %s\n", s.StopReason)
	}
}

func printPrometheusTimeline(ctx context.Context, cfg Config, client *http.Client, s StageSummary) {
	fmt.Printf("\n=== OBSERVABILITY: %s\n", s.Name)
	if cfg.PrometheusURL == "" {
		fmt.Println("  PROMETHEUS: not_configured")
		return
	}
	step := cfg.ObservabilityStep
	if step <= 0 {
		step = 30 * time.Second
	}
	for _, query := range defaultPrometheusQueries() {
		line, err := queryPrometheusRangeSummary(ctx, client, cfg.PrometheusURL, query.Query, s.Start, s.End, step)
		if err != nil {
			fmt.Printf("  PROM_QUERY: name=%s status=error err=%s\n", query.Name, sanitizeErr(err))
			continue
		}
		fmt.Printf("  PROM_QUERY: name=%s %s\n", query.Name, line)
	}
}

func defaultPrometheusQueries() []prometheusQuery {
	return []prometheusQuery{
		{
			Name:  "server_http_p95",
			Query: `histogram_quantile(0.95, sum(rate(http_server_request_duration_bucket[1m])) by (le))`,
		},
		{
			Name:  "server_http_p99",
			Query: `histogram_quantile(0.99, sum(rate(http_server_request_duration_bucket[1m])) by (le))`,
		},
		{
			Name:  "http_rps",
			Query: `sum(rate(http_server_request_count_total[1m]))`,
		},
		{
			Name:  "auction_bid_rps",
			Query: `sum(rate(auction_bid_count_total[1m]))`,
		},
		{
			Name:  "bid_broadcast_rps",
			Query: `sum(rate(auction_bid_broadcast_count_total[1m])) by (action, result)`,
		},
		{
			Name:  "bid_broadcast_flush_p95",
			Query: `histogram_quantile(0.95, sum(rate(auction_bid_broadcast_duration_bucket{action="flush"}[1m])) by (le))`,
		},
		{
			Name:  "bid_broadcast_bids_p95",
			Query: `histogram_quantile(0.95, sum(rate(auction_bid_broadcast_bids_bucket{action="flush"}[1m])) by (le))`,
		},
		{
			Name:  "bid_broadcast_pending_p95",
			Query: `histogram_quantile(0.95, sum(rate(auction_bid_broadcast_pending_bucket[1m])) by (le))`,
		},
		{
			Name:  "lua_result_rps",
			Query: `sum(rate(auction_place_bid_lua_result_count_total[1m]))`,
		},
		{
			Name:  "db_operation_rps",
			Query: `sum(rate(db_client_operation_count_total[1m]))`,
		},
		{
			Name:  "ws_active",
			Query: `sum(ws_connection_active)`,
		},
		{
			Name:  "ws_delivery_rps",
			Query: `sum(rate(ws_delivery_count_total[1m])) by (event_type, result)`,
		},
		{
			Name:  "ws_delivery_by_event_lane",
			Query: `sum(rate(ws_delivery_count_total[1m])) by (event_type, result, reason)`,
		},
		{
			Name:  "ws_write_rps",
			Query: `sum(rate(ws_write_count_total[1m])) by (event_type, result)`,
		},
		{
			Name:  "ws_write_p95",
			Query: `histogram_quantile(0.95, sum(rate(ws_write_duration_bucket[1m])) by (le, event_type, result))`,
		},
		{
			Name:  "ws_send_queue_depth_p95",
			Query: `histogram_quantile(0.95, sum(rate(ws_send_queue_depth_bucket[1m])) by (le, event_type, result))`,
		},
		{
			Name:  "ws_time_sync_overwrite_rps",
			Query: `sum(rate(ws_time_sync_count_total{action="overwrite"}[1m]))`,
		},
		{
			Name:  "ws_time_sync_write_lag_p95",
			Query: `histogram_quantile(0.95, sum(rate(ws_time_sync_write_lag_duration_bucket{action="write"}[1m])) by (le))`,
		},
		{
			Name:  "backend_restarts",
			Query: `sum(kube_pod_container_status_restarts_total{namespace="live-auction",pod=~"live-auction-backend.*"})`,
		},
	}
}

func queryPrometheusRangeSummary(ctx context.Context, client *http.Client, baseURL, query string, start, end time.Time, step time.Duration) (string, error) {
	reqURL, err := prometheusRangeURL(baseURL, query, start, end, step)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("prometheus status=%d", resp.StatusCode)
	}
	var parsed prometheusRangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Status != "success" {
		if parsed.Error != "" {
			return "", fmt.Errorf("prometheus query failed: %s", parsed.Error)
		}
		return "", fmt.Errorf("prometheus query failed")
	}
	return summarizePrometheusRange(parsed), nil
}

func prometheusRangeURL(baseURL, query string, start, end time.Time, step time.Duration) (*url.URL, error) {
	if step <= 0 {
		step = 30 * time.Second
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api/v1/query_range")
	if err != nil {
		return nil, err
	}
	values := parsed.Query()
	values.Set("query", query)
	values.Set("start", strconv.FormatFloat(float64(start.Unix()), 'f', 0, 64))
	values.Set("end", strconv.FormatFloat(float64(end.Unix()), 'f', 0, 64))
	values.Set("step", strconv.FormatFloat(step.Seconds(), 'f', 0, 64))
	parsed.RawQuery = values.Encode()
	return parsed, nil
}

func summarizePrometheusRange(resp prometheusRangeResponse) string {
	series := len(resp.Data.Result)
	samples := 0
	last := math.NaN()
	maxValue := math.NaN()
	for _, result := range resp.Data.Result {
		for _, sample := range result.Values {
			if len(sample) < 2 {
				continue
			}
			value, ok := prometheusSampleValue(sample[1])
			if !ok {
				continue
			}
			samples++
			last = value
			if math.IsNaN(maxValue) || value > maxValue {
				maxValue = value
			}
		}
	}
	return fmt.Sprintf("status=ok series=%d samples=%d last=%s max=%s", series, samples, formatPromValue(last), formatPromValue(maxValue))
}

func prometheusSampleValue(raw any) (float64, bool) {
	switch v := raw.(type) {
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		return parsed, err == nil
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func formatPromValue(value float64) string {
	if math.IsNaN(value) {
		return "n/a"
	}
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func printSummary(cfg Config, summaries []StageSummary) {
	var total, success, httpFailures, businessFails, expectedBusinessRejects, timeouts int64
	for _, s := range summaries {
		total += s.Total
		success += s.Success
		httpFailures += s.HTTPFailures
		businessFails += s.BusinessFails
		expectedBusinessRejects += s.ExpectedBusinessRejects
		timeouts += s.Timeouts
	}
	fmt.Println("\n=== SUMMARY")
	fmt.Printf("  BATCH_ID: %s\n", cfg.BatchID)
	fmt.Printf("  ENVIRONMENT: %s\n", cfg.Environment)
	fmt.Printf("  WS_STREAM_MODE: %s\n", cfg.WSStreamMode)
	fmt.Printf("  STAGES_RUN: %d\n", len(summaries))
	fmt.Printf("  TOTAL: %d\n", total)
	fmt.Printf("  SUCCESS: %d\n", success)
	fmt.Printf("  HTTP_FAILURES: %d\n", httpFailures)
	fmt.Printf("  BUSINESS_FAILS: %d\n", businessFails)
	fmt.Printf("  EXPECTED_BUSINESS_REJECTS: %d\n", expectedBusinessRejects)
	fmt.Printf("  TIMEOUTS: %d\n", timeouts)
	fmt.Printf("  ERROR_RATE: %.4f\n", ratio(httpFailures+businessFails, total))
	fmt.Printf("  TIMEOUT_RATE: %.4f\n", ratio(timeouts, total))
	fmt.Printf("  BUSINESS_FAILURE_RATE: %.4f\n", ratio(businessFails, total))
	fmt.Printf("  EXPECTED_BUSINESS_REJECT_RATE: %.4f\n", ratio(expectedBusinessRejects, total))
	fmt.Printf("  RUNNER_CODE_RETAINED: true\n")
}

func probe(ctx context.Context, cfg Config, client *http.Client, path string) string {
	status, err := probeStatus(ctx, cfg, client, path)
	if err != "" {
		return fmt.Sprintf("fail status=%d err=%s", status, err)
	}
	return fmt.Sprintf("ok status=%d", status)
}

func probeStatus(ctx context.Context, cfg Config, client *http.Client, path string) (int, string) {
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

func probeURL(ctx context.Context, client *http.Client, rawURL string) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
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

func ratio(n, d int64) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func jsonLine(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func getenv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if parsed, err := time.ParseDuration(val); err == nil {
			return parsed
		}
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	val := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch val {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func stopRequested(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "configured"
	}
	if parsed.Host == "" {
		return "configured"
	}
	return parsed.Scheme + "://<redacted-host>"
}

func present(value string) string {
	if value == "" {
		return "not_configured"
	}
	return "configured"
}

func wsURL(baseURL, roomID, ticket string, stream string) string {
	parsed, _ := url.Parse(baseURL)
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	default:
		parsed.Scheme = "ws"
	}
	parsed.Path = "/ws/v1/rooms/" + roomID
	parsed.RawPath = "/ws/v1/rooms/" + url.PathEscape(roomID)
	query := url.Values{}
	query.Set("ticket", ticket)
	if stream != "" && stream != "all" {
		query.Set("stream", stream)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func compactBatch(batchID string) string {
	replacer := strings.NewReplacer("agent_", "a_", "perf_", "p_", "auction_", "auc_", "_20260602155512", "_155512")
	value := replacer.Replace(batchID)
	if len(value) > 40 {
		value = value[len(value)-40:]
	}
	return value
}

func merchantDisplayName(batchID string) string {
	name := "agent perf merchant " + compactBatch(batchID)
	if len(name) > 64 {
		return name[:64]
	}
	return name
}

func maxTargetWS(stages []StageConfig) int {
	max := 0
	for _, stage := range stages {
		if stage.TargetWS > max {
			max = stage.TargetWS
		}
	}
	return max
}

func okErr(err error) string {
	if err == nil {
		return "ok"
	}
	return "err"
}

func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return strings.ReplaceAll(msg, "\n", " ")
}
