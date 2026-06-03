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

type Config struct {
	BatchID        string
	Environment    string
	BaseURL        string
	StopFile       string
	HumanMonitor   string
	RequestTimeout time.Duration
	UserCount      int
	Stages         []StageConfig
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
	Name                    string
	Start                   time.Time
	End                     time.Time
	TargetQPS               float64
	TargetWS                int
	ActualQPS               float64
	Concurrency             int
	Total                   int64
	Success                 int64
	HTTPFailures            int64
	BusinessFails           int64
	ExpectedBusinessRejects int64
	Timeouts                int64
	P50                     time.Duration
	P95                     time.Duration
	P99                     time.Duration
	Max                     time.Duration
	StatusCodes             map[int]int64
	BusinessCodes           map[string]int64
	WSConnected             int
	WSConnectFails          int
	StopReason              string
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
		wsFails := ensureWSConnections(ctx, cfg, data, stage.TargetWS)
		summary := runStage(ctx, cfg, data, stage)
		summary.WSConnected = len(data.WSConns)
		summary.WSConnectFails = wsFails
		if summary.StopReason == "" {
			summary.StopReason = thresholdStopReason(summary)
		}
		printStageSummary(summary)
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
	batchID := getenv("PERF_BATCH_ID", "agent_perf_auction_20260602155512")
	stages := filterStages([]StageConfig{
		{Name: "step_20qps_40ws", TargetQPS: 20, TargetWS: 40, Concurrency: 20, Duration: 1 * time.Minute, RequestMix: "auction_mix"},
		{Name: "step_30qps_60ws", TargetQPS: 30, TargetWS: 60, Concurrency: 30, Duration: 2 * time.Minute, RequestMix: "auction_mix"},
		{Name: "step_40qps_80ws", TargetQPS: 40, TargetWS: 80, Concurrency: 40, Duration: 3 * time.Minute, RequestMix: "auction_mix"},
		{Name: "step_60qps_120ws", TargetQPS: 60, TargetWS: 120, Concurrency: 60, Duration: 5 * time.Minute, RequestMix: "auction_mix"},
		{Name: "step_70qps_160ws", TargetQPS: 70, TargetWS: 160, Concurrency: 70, Duration: 5 * time.Minute, RequestMix: "auction_mix"},
	}, envInt("PERF_START_QPS", 0))
	return Config{
		BatchID:        batchID,
		Environment:    getenv("PERF_ENVIRONMENT", "single_source_online"),
		BaseURL:        strings.TrimRight(getenv("PERF_BASE_URL", "http://127.0.0.1:8080"), "/"),
		StopFile:       getenv("PERF_STOP_FILE", "docs/agent-testing/performance-runs/"+batchID+"/STOP"),
		HumanMonitor:   getenv("PERF_HUMAN_MONITOR", "user"),
		RequestTimeout: envDuration("PERF_REQUEST_TIMEOUT", 10*time.Second),
		UserCount:      envInt("PERF_USER_COUNT", 160),
		Stages:         stages,
	}
}

func filterStages(stages []StageConfig, startQPS int) []StageConfig {
	if startQPS <= 0 {
		return stages
	}
	filtered := make([]StageConfig, 0, len(stages))
	for _, stage := range stages {
		if int(stage.TargetQPS) >= startQPS {
			filtered = append(filtered, stage)
		}
	}
	return filtered
}

func setupData(ctx context.Context, cfg Config, client *http.Client) (*TestData, error) {
	data := &TestData{HTTPClient: client}
	password := "PerfPass_" + compactBatch(cfg.BatchID)

	merchantToken, merchantID, err := register(ctx, cfg, client, compactBatch(cfg.BatchID)+"_m", password)
	if err != nil {
		return nil, fmt.Errorf("register merchant: %w", err)
	}
	data.MerchantToken = merchantToken
	data.MerchantUser = merchantID
	if err := putJSON(ctx, cfg, client, "/api/v1/users/me", merchantToken, map[string]any{
		"name":     "agent perf merchant " + cfg.BatchID,
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
		token, userID, err := register(ctx, cfg, client, fmt.Sprintf("%s_u%03d", compactBatch(cfg.BatchID), i), password)
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
	var result struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	err := postJSON(ctx, cfg, client, "/api/v1/auth/register", "", map[string]any{
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
	switch n % 100 {
	case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19:
		return RequestSpec{Method: http.MethodGet, Path: "/api/v1/rooms/" + url.PathEscape(data.RoomID)}
	case 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44:
		return RequestSpec{Method: http.MethodGet, Path: "/api/v1/items/" + url.PathEscape(data.ItemID)}
	case 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69:
		return RequestSpec{Method: http.MethodGet, Path: "/api/v1/items/" + url.PathEscape(data.ItemID) + "/ranking?page=1&page_size=20"}
	case 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84:
		return RequestSpec{Method: http.MethodPost, Path: "/api/v1/items/" + url.PathEscape(data.ItemID) + "/bids", Token: data.UserTokens[userIdx]}
	case 85, 86, 87, 88, 89:
		return RequestSpec{Method: http.MethodPost, Path: "/api/v1/ws-ticket", Token: data.UserTokens[userIdx]}
	case 90, 91, 92, 93, 94:
		return RequestSpec{Method: http.MethodGet, Path: "/api/v1/merchant/room", Token: data.MerchantToken}
	default:
		return RequestSpec{Method: http.MethodGet, Path: "/health"}
	}
}

func ensureWSConnections(ctx context.Context, cfg Config, data *TestData, target int) int {
	failures := 0
	for len(data.WSConns) < target {
		idx := len(data.WSConns) % len(data.UserTokens)
		ticket, err := issueTicket(ctx, cfg, data.HTTPClient, data.UserTokens[idx])
		if err != nil {
			failures++
			continue
		}
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(cfg.BaseURL, data.RoomID, ticket), nil)
		if err != nil {
			failures++
			continue
		}
		data.WSConns = append(data.WSConns, conn)
		go drainWS(conn)
	}
	return failures
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

func drainWS(conn *websocket.Conn) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func runStage(ctx context.Context, cfg Config, data *TestData, stage StageConfig) StageSummary {
	start := time.Now()
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
	elapsed := summary.End.Sub(summary.Start).Seconds()
	if elapsed > 0 {
		summary.ActualQPS = float64(summary.Total) / elapsed
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	summary.P50 = percentile(latencies, 0.50)
	summary.P95 = percentile(latencies, 0.95)
	summary.P99 = percentile(latencies, 0.99)
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
	currentPrice, increment, err := readCurrentBidView(ctx, cfg, data)
	if err != nil {
		return RequestSpec{}, err
	}
	if increment <= 0 {
		increment = 100
	}
	price := currentPrice + increment
	body, _ := json.Marshal(map[string]any{
		"price":           price,
		"idempotency_key": fmt.Sprintf("%s_%s_%d", cfg.BatchID, stage.Name, n),
	})
	bidSeq.Add(1)
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
	return fmt.Sprintf("item_detail=%s ranking=%s room=%s ws_connected=%d bid_attempts=%d", itemOK, rankingOK, roomOK, len(data.WSConns), bidSeq.Load())
}

func cleanup(ctx context.Context, cfg Config, data *TestData) string {
	for _, c := range data.WSConns {
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

func postJSON(ctx context.Context, cfg Config, client *http.Client, path string, token string, body any, out any) error {
	return doJSON(ctx, cfg, client, http.MethodPost, path, token, body, out)
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
	if s.P99 > 2*time.Second {
		return "p99_gt_2s"
	}
	if s.TargetWS > 0 && s.WSConnected < int(float64(s.TargetWS)*0.95) {
		return "ws_connection_success_lt_95_percent"
	}
	return ""
}

func printPlan(cfg Config) {
	fmt.Println("=== PERF_PLAN")
	fmt.Printf("  BATCH_ID: %s\n", cfg.BatchID)
	fmt.Printf("  ENVIRONMENT: %s\n", cfg.Environment)
	fmt.Printf("  BASE_URL: %s\n", redactURL(cfg.BaseURL))
	fmt.Printf("  HUMAN_MONITOR: %s\n", cfg.HumanMonitor)
	fmt.Printf("  STOP_FILE: %s\n", cfg.StopFile)
	fmt.Printf("  USER_COUNT: %d\n", cfg.UserCount)
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
	fmt.Printf("  STOP_FILE_PRESENT: %t\n", stopRequested(cfg.StopFile))
}

func printStageSummary(s StageSummary) {
	fmt.Printf("\n=== STAGE: %s\n", s.Name)
	fmt.Printf("  WINDOW: %s -> %s\n", s.Start.Format(time.RFC3339), s.End.Format(time.RFC3339))
	fmt.Printf("  TARGET_QPS: %.2f\n", s.TargetQPS)
	fmt.Printf("  ACTUAL_QPS: %.2f\n", s.ActualQPS)
	fmt.Printf("  TARGET_WS: %d\n", s.TargetWS)
	fmt.Printf("  WS_CONNECTED: %d\n", s.WSConnected)
	fmt.Printf("  WS_CONNECT_FAILS: %d\n", s.WSConnectFails)
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
	fmt.Printf("  P50: %s\n", s.P50)
	fmt.Printf("  P95: %s\n", s.P95)
	fmt.Printf("  P99: %s\n", s.P99)
	fmt.Printf("  MAX: %s\n", s.Max)
	fmt.Printf("  STATUS_CODES: %s\n", jsonLine(s.StatusCodes))
	fmt.Printf("  BUSINESS_CODES: %s\n", jsonLine(s.BusinessCodes))
	if s.StopReason != "" {
		fmt.Printf("  STOP_REASON: %s\n", s.StopReason)
	}
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

func wsURL(baseURL, roomID, ticket string) string {
	parsed, _ := url.Parse(baseURL)
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	default:
		parsed.Scheme = "ws"
	}
	parsed.Path = "/ws/v1/rooms/" + url.PathEscape(roomID)
	parsed.RawQuery = "ticket=" + url.QueryEscape(ticket)
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
