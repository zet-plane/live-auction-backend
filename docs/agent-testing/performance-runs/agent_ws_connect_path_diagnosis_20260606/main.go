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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const firstMessageTimeout = 3 * time.Second

type Config struct {
	BatchID            string
	BaseURL            string
	SensitiveHosts     []string
	UserCount          int
	TargetWS           int
	Stream             string
	ConnectConcurrency int
	ConnectRounds      int
	ConnectTimeout     time.Duration
	RequestTimeout     time.Duration
	WaitFirstMessage   bool
}

type TestData struct {
	MerchantToken string
	RoomID        string
	ItemID        string
	UserTokens    []string
	HTTPClient    *http.Client
	WSConns       []*websocket.Conn
	mu            sync.Mutex
}

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type AttemptResult struct {
	TicketDuration       time.Duration
	DialDuration         time.Duration
	FirstMessageDuration time.Duration
	TotalDuration        time.Duration
	Connected            bool
	FirstMessageSeen     bool
	ErrorPhase           string
	ErrorReason          string
}

type RoundSummary struct {
	Round               int
	Attempts            int
	Success             int
	FirstMessages       int
	TicketDurations     []time.Duration
	DialDurations       []time.Duration
	FirstMessageWaits   []time.Duration
	TotalPathDurations  []time.Duration
	ErrorCounts         map[string]int64
	FirstMessageTimeout int64
}

type AggregateSummary struct {
	Rounds              int
	Attempts            int
	Success             int
	FirstMessages       int
	TicketDurations     []time.Duration
	DialDurations       []time.Duration
	FirstMessageWaits   []time.Duration
	TotalPathDurations  []time.Duration
	ErrorCounts         map[string]int64
	FirstMessageTimeout int64
}

var attemptSeq atomic.Int64

func main() {
	cfg := loadConfig()
	client := &http.Client{Timeout: cfg.RequestTimeout}
	ctx := context.Background()

	printPlan(cfg)
	printPreflight(ctx, cfg, client)

	data, err := setupData(ctx, cfg, client)
	if err != nil {
		fmt.Printf("  SETUP: FAIL err=%s\n", sanitizeErr(cfg, err))
		fmt.Println("\n=== CLEANUP")
		if data != nil {
			fmt.Printf("  RESULT: setup_failed %s\n", cleanup(ctx, cfg, data))
		} else {
			fmt.Println("  RESULT: setup_failed_no_batch_data")
		}
		fmt.Println("\n=== SUMMARY")
		fmt.Printf("  STATUS: setup_failed\n")
		return
	}

	aggregate := AggregateSummary{Rounds: cfg.ConnectRounds, ErrorCounts: map[string]int64{}}
	for round := 1; round <= cfg.ConnectRounds; round++ {
		summary := runRound(ctx, cfg, data, round)
		printRound(summary)
		mergeAggregate(&aggregate, summary)
	}

	fmt.Println("\n=== CLEANUP")
	fmt.Printf("  RESULT: %s\n", cleanup(ctx, cfg, data))

	printSummary(aggregate)
}

func loadConfig() Config {
	batchID := requiredEnv("PERF_BATCH_ID")
	baseURL := strings.TrimRight(requiredEnv("PERF_BASE_URL"), "/")
	userCount := requiredEnvInt("PERF_USER_COUNT")
	targetWS := requiredEnvInt("PERF_TARGET_WS")
	return Config{
		BatchID:            batchID,
		BaseURL:            baseURL,
		SensitiveHosts:     hostRedactionValues(baseURL),
		UserCount:          userCount,
		TargetWS:           targetWS,
		Stream:             requiredEnv("PERF_STREAM"),
		ConnectConcurrency: requiredEnvInt("PERF_CONNECT_CONCURRENCY"),
		ConnectRounds:      requiredEnvInt("PERF_CONNECT_ROUNDS"),
		ConnectTimeout:     requiredEnvDuration("PERF_CONNECT_TIMEOUT"),
		RequestTimeout:     envDuration("PERF_REQUEST_TIMEOUT", 10*time.Second),
		WaitFirstMessage:   envBool("PERF_WAIT_FIRST_MESSAGE", true),
	}
}

func printPlan(cfg Config) {
	fmt.Println("=== CONNECT_PROBE_PLAN")
	fmt.Printf("  BATCH_ID: %s\n", cfg.BatchID)
	fmt.Printf("  BASE_URL: %s\n", redactURL(cfg.BaseURL))
	fmt.Printf("  USER_COUNT: %d\n", cfg.UserCount)
	fmt.Printf("  TARGET_WS: %d\n", cfg.TargetWS)
	fmt.Printf("  STREAM: %s\n", cfg.Stream)
	fmt.Printf("  CONNECT_CONCURRENCY: %d\n", cfg.ConnectConcurrency)
	fmt.Printf("  CONNECT_ROUNDS: %d\n", cfg.ConnectRounds)
	fmt.Printf("  CONNECT_TIMEOUT: %s\n", cfg.ConnectTimeout)
	fmt.Printf("  WAIT_FIRST_MESSAGE: %t\n", cfg.WaitFirstMessage)
	fmt.Printf("  FIRST_MESSAGE_WAIT: %s\n", firstMessageTimeout)
}

func printPreflight(ctx context.Context, cfg Config, client *http.Client) {
	fmt.Println("\n=== PREFLIGHT")
	status, errText := probeStatus(ctx, cfg, client, "/health")
	if errText != "" {
		fmt.Printf("  HEALTH: FAIL status=%d err=%s\n", status, errText)
		return
	}
	fmt.Printf("  HEALTH: OK status=%d\n", status)
}

func setupData(ctx context.Context, cfg Config, client *http.Client) (*TestData, error) {
	data := &TestData{HTTPClient: client}
	password := batchPassword(cfg.BatchID)

	merchantToken, _, err := register(ctx, cfg, client, merchantAccount(cfg.BatchID), password)
	if err != nil {
		return nil, fmt.Errorf("register merchant: %w", err)
	}
	data.MerchantToken = merchantToken

	if err := putJSON(ctx, cfg, client, "/api/v1/users/me", merchantToken, map[string]any{
		"name":     merchantDisplayName(cfg.BatchID),
		"identity": "merchant",
	}, nil); err != nil {
		return data, fmt.Errorf("promote merchant: %w", err)
	}

	for i := 0; i < cfg.UserCount; i++ {
		token, _, err := register(ctx, cfg, client, userAccount(cfg.BatchID, i), password)
		if err != nil {
			return data, fmt.Errorf("register user %d: %w", i, err)
		}
		data.UserTokens = append(data.UserTokens, token)
	}

	var room struct {
		ID string `json:"id"`
	}
	if err := postJSON(ctx, cfg, client, "/api/v1/merchant/room", merchantToken, map[string]any{
		"title": "agent_ws_connect_room_" + cfg.BatchID,
	}, &room); err != nil {
		return data, fmt.Errorf("create room: %w", err)
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
		"title":       "agent_ws_connect_item_" + cfg.BatchID,
		"description": "agent ws connect path diagnosis item",
		"image_url":   "https://example.com/agent-ws-connect.png",
		"tags":        []string{"agent", "ws", "diagnosis"},
		"rule": map[string]any{
			"start_price":   1000,
			"bid_increment": 100,
			"start_time":    now.Format(time.RFC3339),
			"end_time":      end.Format(time.RFC3339),
		},
	}, &item); err != nil {
		return data, fmt.Errorf("create item: %w", err)
	}
	data.ItemID = item.ItemID
	if err := postJSON(ctx, cfg, client, "/api/v1/items/"+url.PathEscape(data.ItemID)+"/publish", merchantToken, nil, nil); err != nil {
		return data, fmt.Errorf("publish item: %w", err)
	}
	if err := postJSON(ctx, cfg, client, "/api/v1/items/"+url.PathEscape(data.ItemID)+"/start", merchantToken, nil, nil); err != nil {
		return data, fmt.Errorf("start item: %w", err)
	}

	fmt.Printf("  TEST_DATA: created merchant, room, item, users=%d\n", len(data.UserTokens))
	return data, nil
}

func runRound(ctx context.Context, cfg Config, data *TestData, round int) RoundSummary {
	summary := RoundSummary{Round: round, Attempts: cfg.TargetWS, ErrorCounts: map[string]int64{}}
	results := make(chan AttemptResult, cfg.TargetWS)
	sem := make(chan struct{}, cfg.ConnectConcurrency)
	var wg sync.WaitGroup

	for i := 0; i < cfg.TargetWS; i++ {
		userIndex := i % len(data.UserTokens)
		wg.Add(1)
		go func(userIndex int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results <- attemptConnect(ctx, cfg, data, userIndex)
		}(userIndex)
	}

	wg.Wait()
	close(results)

	for result := range results {
		summary.TicketDurations = append(summary.TicketDurations, result.TicketDuration)
		if result.DialDuration > 0 {
			summary.DialDurations = append(summary.DialDurations, result.DialDuration)
		}
		if result.FirstMessageDuration > 0 {
			summary.FirstMessageWaits = append(summary.FirstMessageWaits, result.FirstMessageDuration)
		}
		summary.TotalPathDurations = append(summary.TotalPathDurations, result.TotalDuration)
		if result.Connected {
			summary.Success++
		}
		if result.FirstMessageSeen {
			summary.FirstMessages++
		}
		if result.ErrorPhase != "" {
			summary.ErrorCounts[result.ErrorPhase+":"+result.ErrorReason]++
		}
		if cfg.WaitFirstMessage && result.Connected && !result.FirstMessageSeen {
			summary.FirstMessageTimeout++
		}
	}

	sortDurations(summary.TicketDurations)
	sortDurations(summary.DialDurations)
	sortDurations(summary.FirstMessageWaits)
	sortDurations(summary.TotalPathDurations)
	return summary
}

func attemptConnect(ctx context.Context, cfg Config, data *TestData, userIndex int) AttemptResult {
	started := time.Now()
	result := AttemptResult{}

	ticketStart := time.Now()
	ticket, err := issueTicket(ctx, cfg, data.HTTPClient, data.UserTokens[userIndex])
	result.TicketDuration = time.Since(ticketStart)
	if err != nil {
		result.TotalDuration = time.Since(started)
		result.ErrorPhase = "ticket"
		result.ErrorReason = classifyErr(cfg, err)
		return result
	}

	dialCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	dialer := websocket.Dialer{HandshakeTimeout: cfg.ConnectTimeout}
	dialStart := time.Now()
	conn, resp, err := dialer.DialContext(dialCtx, wsURL(cfg.BaseURL, data.RoomID, ticket, cfg.Stream), nil)
	result.DialDuration = time.Since(dialStart)
	if err != nil {
		result.TotalDuration = time.Since(started)
		result.ErrorPhase = "dial"
		if resp != nil {
			result.ErrorReason = fmt.Sprintf("status_%d", resp.StatusCode)
		} else {
			result.ErrorReason = classifyErr(cfg, err)
		}
		return result
	}

	result.Connected = true
	data.addConn(conn)

	if cfg.WaitFirstMessage {
		msgStart := time.Now()
		_ = conn.SetReadDeadline(time.Now().Add(firstMessageTimeout))
		if _, _, err := conn.ReadMessage(); err != nil {
			result.FirstMessageDuration = time.Since(msgStart)
			result.ErrorPhase = "first_message"
			result.ErrorReason = classifyErr(cfg, err)
		} else {
			result.FirstMessageDuration = time.Since(msgStart)
			result.FirstMessageSeen = true
		}
	}

	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "probe_done"), time.Now().Add(time.Second))
	_ = conn.Close()
	result.TotalDuration = time.Since(started)
	attemptSeq.Add(1)
	return result
}

func printRound(summary RoundSummary) {
	fmt.Printf("\n=== ROUND: %d\n", summary.Round)
	fmt.Printf("  ATTEMPTS: %d\n", summary.Attempts)
	fmt.Printf("  SUCCESS: %d\n", summary.Success)
	fmt.Printf("  FIRST_MESSAGES: %d\n", summary.FirstMessages)
	fmt.Printf("  FIRST_MESSAGE_TIMEOUTS: %d\n", summary.FirstMessageTimeout)
	printDurationStats("TICKET_ISSUE", summary.TicketDurations)
	printDurationStats("WS_DIAL", summary.DialDurations)
	printDurationStats("FIRST_MESSAGE_WAIT", summary.FirstMessageWaits)
	printDurationStats("TOTAL_CONNECT_PATH", summary.TotalPathDurations)
	fmt.Printf("  ERROR_COUNTS: %s\n", jsonLine(summary.ErrorCounts))
}

func mergeAggregate(aggregate *AggregateSummary, round RoundSummary) {
	aggregate.Attempts += round.Attempts
	aggregate.Success += round.Success
	aggregate.FirstMessages += round.FirstMessages
	aggregate.FirstMessageTimeout += round.FirstMessageTimeout
	aggregate.TicketDurations = append(aggregate.TicketDurations, round.TicketDurations...)
	aggregate.DialDurations = append(aggregate.DialDurations, round.DialDurations...)
	aggregate.FirstMessageWaits = append(aggregate.FirstMessageWaits, round.FirstMessageWaits...)
	aggregate.TotalPathDurations = append(aggregate.TotalPathDurations, round.TotalPathDurations...)
	for key, count := range round.ErrorCounts {
		aggregate.ErrorCounts[key] += count
	}
	sortDurations(aggregate.TicketDurations)
	sortDurations(aggregate.DialDurations)
	sortDurations(aggregate.FirstMessageWaits)
	sortDurations(aggregate.TotalPathDurations)
}

func printSummary(summary AggregateSummary) {
	fmt.Println("\n=== SUMMARY")
	fmt.Printf("  ROUNDS: %d\n", summary.Rounds)
	fmt.Printf("  ATTEMPTS: %d\n", summary.Attempts)
	fmt.Printf("  SUCCESS: %d\n", summary.Success)
	fmt.Printf("  FIRST_MESSAGES: %d\n", summary.FirstMessages)
	fmt.Printf("  FIRST_MESSAGE_TIMEOUTS: %d\n", summary.FirstMessageTimeout)
	printDurationStats("TICKET_ISSUE", summary.TicketDurations)
	printDurationStats("WS_DIAL", summary.DialDurations)
	printDurationStats("FIRST_MESSAGE_WAIT", summary.FirstMessageWaits)
	printDurationStats("TOTAL_CONNECT_PATH", summary.TotalPathDurations)
	fmt.Printf("  ERROR_COUNTS: %s\n", jsonLine(summary.ErrorCounts))
}

func printDurationStats(name string, values []time.Duration) {
	fmt.Printf("  %s_COUNT: %d\n", name, len(values))
	fmt.Printf("  %s_P50: %s\n", name, percentile(values, 0.50))
	fmt.Printf("  %s_P95: %s\n", name, percentile(values, 0.95))
	fmt.Printf("  %s_P99: %s\n", name, percentile(values, 0.99))
	fmt.Printf("  %s_MAX: %s\n", name, maxDuration(values))
}

func cleanup(ctx context.Context, cfg Config, data *TestData) string {
	data.mu.Lock()
	conns := append([]*websocket.Conn(nil), data.WSConns...)
	data.mu.Unlock()

	closed := 0
	for _, conn := range conns {
		if conn == nil {
			continue
		}
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "cleanup"), time.Now().Add(time.Second))
		_ = conn.Close()
		closed++
	}

	parts := []string{fmt.Sprintf("closed_ws=%d", closed)}
	if data.ItemID != "" {
		parts = append(parts, "cancel_item="+okErr(postJSON(ctx, cfg, data.HTTPClient, "/api/v1/items/"+url.PathEscape(data.ItemID)+"/cancel", data.MerchantToken, nil, nil)))
	}
	if data.RoomID != "" {
		parts = append(parts, "end_room="+okErr(postJSON(ctx, cfg, data.HTTPClient, "/api/v1/rooms/"+url.PathEscape(data.RoomID)+"/end", data.MerchantToken, nil, nil)))
	}
	deletedUsers := 0
	for _, token := range data.UserTokens {
		if err := deleteJSON(ctx, cfg, data.HTTPClient, "/api/v1/users/me", token); err == nil {
			deletedUsers++
		}
	}
	parts = append(parts, fmt.Sprintf("delete_users_ok=%d delete_users_attempted=%d", deletedUsers, len(data.UserTokens)))
	if data.MerchantToken != "" {
		parts = append(parts, "delete_merchant="+okErr(deleteJSON(ctx, cfg, data.HTTPClient, "/api/v1/users/me", data.MerchantToken)))
	}
	return strings.Join(parts, " ")
}

func (data *TestData) addConn(conn *websocket.Conn) {
	data.mu.Lock()
	defer data.mu.Unlock()
	data.WSConns = append(data.WSConns, conn)
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

func probeStatus(ctx context.Context, cfg Config, client *http.Client, path string) (int, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+path, nil)
	if err != nil {
		return 0, sanitizeErr(cfg, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, sanitizeErr(cfg, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, ""
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

func batchPassword(batchID string) string {
	return "PerfPass_" + compactBatch(batchID)
}

func merchantAccount(batchID string) string {
	return compactBatch(batchID) + "_m"
}

func userAccount(batchID string, index int) string {
	return fmt.Sprintf("%s_u%03d", compactBatch(batchID), index)
}

func merchantDisplayName(batchID string) string {
	name := "agent ws merchant " + compactBatch(batchID)
	if len(name) > 64 {
		return name[:64]
	}
	return name
}

func compactBatch(batchID string) string {
	replacer := strings.NewReplacer("agent_", "a_", "perf_", "p_", "auction_", "auc_", "_20260606", "_0606")
	value := replacer.Replace(batchID)
	if len(value) > 40 {
		value = value[len(value)-40:]
	}
	return value
}

func sortDurations(values []time.Duration) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
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

func maxDuration(values []time.Duration) time.Duration {
	var max time.Duration
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
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

func requiredEnv(key string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		fmt.Printf("missing required env %s\n", key)
		os.Exit(2)
	}
	return val
}

func requiredEnvInt(key string) int {
	val := requiredEnv(key)
	parsed, err := strconv.Atoi(val)
	if err != nil || parsed <= 0 {
		fmt.Printf("invalid required env %s=%q\n", key, val)
		os.Exit(2)
	}
	return parsed
}

func requiredEnvDuration(key string) time.Duration {
	val := requiredEnv(key)
	parsed, err := time.ParseDuration(val)
	if err != nil || parsed <= 0 {
		fmt.Printf("invalid required env %s=%q\n", key, val)
		os.Exit(2)
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		parsed, err := time.ParseDuration(val)
		if err != nil || parsed <= 0 {
			fmt.Printf("invalid optional env %s=%q\n", key, val)
			os.Exit(2)
		}
		return parsed
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
	if val := os.Getenv(key); val != "" {
		parsed, err := strconv.ParseBool(val)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func okErr(err error) string {
	if err == nil {
		return "ok"
	}
	return "err"
}

func classifyErr(cfg Config, err error) string {
	if err == nil {
		return ""
	}
	msg := sanitizeErr(cfg, err)
	switch {
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "Client.Timeout"), strings.Contains(msg, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "connection reset"):
		return "connection_reset"
	case strings.Contains(msg, "connection refused"):
		return "connection_refused"
	case strings.Contains(msg, "websocket: close"):
		return "closed"
	case strings.Contains(msg, "use of closed network connection"):
		return "closed"
	}
	if len(msg) > 80 {
		msg = msg[:80]
	}
	msg = strings.TrimSpace(strings.ReplaceAll(msg, " ", "_"))
	if msg == "" {
		return "unknown"
	}
	return msg
}

var sensitiveURLPattern = regexp.MustCompile(`(?i)\b(wss?|https?)://[^\s]+`)
var sensitiveAddrPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}:\d+\b`)

func sanitizeErr(cfg Config, err error) string {
	if err == nil {
		return ""
	}
	return sanitizeText(cfg, err.Error())
}

func sanitizeText(cfg Config, msg string) string {
	msg = sensitiveURLPattern.ReplaceAllStringFunc(msg, func(raw string) string {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil || parsed.Scheme == "" {
			return "<redacted-url>"
		}
		return parsed.Scheme + "://<redacted-host>"
	})
	for _, host := range cfg.SensitiveHosts {
		msg = replaceInsensitive(msg, host, "<redacted-host>")
	}
	msg = sensitiveAddrPattern.ReplaceAllString(msg, "<redacted-addr>")
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

func hostRedactionValues(raw string) []string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	values := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, value := range []string{parsed.Host, parsed.Hostname()} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
}

func replaceInsensitive(text, needle, replacement string) string {
	if needle == "" {
		return text
	}
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(needle))
	return pattern.ReplaceAllString(text, replacement)
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "configured"
	}
	return parsed.Scheme + "://<redacted-host>"
}
