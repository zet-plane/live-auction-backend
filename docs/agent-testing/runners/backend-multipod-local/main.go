package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

type Result struct {
	Name     string
	Pass     bool
	Reason   string
	Request  string
	Response string
	DB       string
	Redis    string
}

type config struct {
	BatchID       string
	ProducerURL   string
	SubscriberURL string
	RedisAddr     string
	RepoRoot      string
	GoCache       string
	StartBackends bool
}

type apiClient struct {
	base string
	hc   *http.Client
}

type httpResult struct {
	Status int
	Body   map[string]any
	Raw    string
	Err    error
}

type wsEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type backendProcess struct {
	name string
	cmd  *exec.Cmd
	log  *os.File
	done chan struct{}
}

var (
	cfg          config
	ctx          = context.Background()
	httpClient   = &http.Client{Timeout: 10 * time.Second}
	db           *sql.DB
	rdb          *redis.Client
	producer     apiClient
	subscriber   apiClient
	started      []*backendProcess
	tmpConfigDir string

	merchantToken string
	merchantID    string
	bidderToken   string
	bidderID      string
	observerToken string
	observerID    string
	roomID        string
	itemID        string
	bidKey        string
	issuedTickets []string
	observerConn  *websocket.Conn
	bidderConn    *websocket.Conn
)

func main() {
	parseConfig()
	defer cleanup()

	if err := setupClients(); err != nil {
		printResult(Result{
			Name:     "setup clients",
			Pass:     false,
			Reason:   err.Error(),
			Request:  "open TEST_DSN and Redis client",
			Response: "setup failed",
			DB:       "TEST_DSN is required; value omitted",
			Redis:    "redis addr configured; value omitted",
		})
		fmt.Printf("\n=== SUMMARY\n  PASS: 0  FAIL: 1\n  BATCH_ID: %s\n", cfg.BatchID)
		return
	}

	results := make([]Result, 0, len(scenarios()))
	for _, fn := range scenarios() {
		r := fn()
		results = append(results, r)
		printResult(r)
	}

	pass, fail := 0, 0
	for _, r := range results {
		if r.Pass {
			pass++
		} else {
			fail++
		}
	}
	fmt.Printf("\n=== SUMMARY\n  PASS: %d  FAIL: %d\n  BATCH_ID: %s\n", pass, fail, cfg.BatchID)
}

func parseConfig() {
	defaultBatch := "agent_multipod_" + time.Now().Format("20060102150405") + "_"
	flag.StringVar(&cfg.BatchID, "batch", envOr("TEST_BATCH_ID", defaultBatch), "test batch prefix")
	flag.StringVar(&cfg.ProducerURL, "producer-url", envOr("TEST_PRODUCER_URL", "http://127.0.0.1:18080"), "producer backend base URL")
	flag.StringVar(&cfg.SubscriberURL, "subscriber-url", envOr("TEST_SUBSCRIBER_URL", "http://127.0.0.1:18081"), "subscriber backend base URL")
	flag.StringVar(&cfg.RedisAddr, "redis-addr", envOr("TEST_REDIS_ADDR", "127.0.0.1:6379"), "Redis address")
	flag.StringVar(&cfg.RepoRoot, "repo-root", envOr("TEST_REPO_ROOT", "."), "repo root for -start-backends")
	flag.StringVar(&cfg.GoCache, "gocache", envOr("GOCACHE", "/tmp/live-auction-go-cache"), "Go build cache for -start-backends")
	flag.BoolVar(&cfg.StartBackends, "start-backends", envBool("TEST_START_BACKENDS"), "start two local backend processes")
	flag.Parse()
	if !strings.HasPrefix(cfg.BatchID, "agent_multipod_") {
		cfg.BatchID = "agent_multipod_" + strings.TrimLeft(cfg.BatchID, "_")
	}
}

func setupClients() error {
	dsn := os.Getenv("TEST_DSN")
	if strings.TrimSpace(dsn) == "" {
		return errors.New("TEST_DSN is required and must not be written to reports")
	}
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	rdb = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	producer = apiClient{base: strings.TrimRight(cfg.ProducerURL, "/"), hc: httpClient}
	subscriber = apiClient{base: strings.TrimRight(cfg.SubscriberURL, "/"), hc: httpClient}
	return nil
}

func scenarios() []func() Result {
	return []func() Result{
		caseStartBackendsIfRequested,
		caseProducerReady,
		caseSubscriberReady,
		caseRegisterBatchUsers,
		casePromoteMerchant,
		caseCreateAndStartRoom,
		caseCreateAndPublishItem,
		caseConnectSubscriberWebSockets,
		caseStartItemFanout,
		casePriceCapBidFanoutAndUnicast,
		caseHTTPFinalStateOnSubscriber,
	}
}

func caseStartBackendsIfRequested() Result {
	if !cfg.StartBackends {
		return Result{
			Name:     "start local backends",
			Pass:     true,
			Reason:   "start-backends=false; using already running producer and subscriber",
			Request:  "reuse configured backend URLs",
			Response: fmt.Sprintf("producer=%s subscriber=%s", cfg.ProducerURL, cfg.SubscriberURL),
			DB:       "N/A",
			Redis:    "N/A",
		}
	}
	if err := startBackendPair(); err != nil {
		return Result{
			Name:     "start local backends",
			Pass:     false,
			Reason:   err.Error(),
			Request:  "go run . server -c <temp config> x2",
			Response: "startup failed; see temp runner logs",
			DB:       "TEST_DSN used from env; value omitted",
			Redis:    "redis address used from config; value omitted",
		}
	}
	return Result{
		Name:     "start local backends",
		Pass:     true,
		Reason:   "both local backend processes started and reached readiness",
		Request:  "go run . server -c <temp config> x2",
		Response: processSummary(),
		DB:       "TEST_DSN used from env; value omitted",
		Redis:    "Redis shared by both processes; address omitted",
	}
}

func caseProducerReady() Result {
	res := producer.do("GET", "/readyz", "", nil)
	ok := res.Err == nil && res.Status == http.StatusOK && mustStr(res.Body, "data", "status") == "ok"
	return Result{
		Name:     "producer readyz",
		Pass:     ok,
		Reason:   fmt.Sprintf("status=%d err=%v", res.Status, res.Err),
		Request:  "GET producer /readyz",
		Response: summarizeReady(res),
		DB:       "MySQL component asserted by /readyz",
		Redis:    "Redis component asserted by /readyz",
	}
}

func caseSubscriberReady() Result {
	res := subscriber.do("GET", "/readyz", "", nil)
	ok := res.Err == nil && res.Status == http.StatusOK && mustStr(res.Body, "data", "status") == "ok"
	return Result{
		Name:     "subscriber readyz",
		Pass:     ok,
		Reason:   fmt.Sprintf("status=%d err=%v", res.Status, res.Err),
		Request:  "GET subscriber /readyz",
		Response: summarizeReady(res),
		DB:       "MySQL component asserted by /readyz",
		Redis:    "Redis component asserted by /readyz",
	}
}

func caseRegisterBatchUsers() Result {
	var err error
	merchantToken, merchantID, err = registerUser(producer, cfg.BatchID+"merchant")
	if err == nil {
		bidderToken, bidderID, err = registerUser(producer, cfg.BatchID+"bidder")
	}
	if err == nil {
		observerToken, observerID, err = registerUser(producer, cfg.BatchID+"observer")
	}
	rows := dbRows("SELECT account, identity FROM users WHERE account LIKE ? ORDER BY account", cfg.BatchID+"%")
	ok := err == nil && merchantID != "" && bidderID != "" && observerID != "" && len(rows) == 3
	return Result{
		Name:     "register batch users",
		Pass:     ok,
		Reason:   fmt.Sprintf("err=%v users=%d", err, len(rows)),
		Request:  "POST producer /api/v1/auth/register x3 {account:<batch>*, password omitted}",
		Response: fmt.Sprintf("merchant=%s bidder=%s observer=%s", merchantID, bidderID, observerID),
		DB:       fmt.Sprintf("SELECT account,identity WHERE account LIKE batch -> %v", rows),
		Redis:    "N/A",
	}
}

func casePromoteMerchant() Result {
	if merchantToken == "" || bidderToken == "" {
		return failedPrereq("promote merchant", "missing registered tokens")
	}
	resMerchant := producer.do("PUT", "/api/v1/users/me", merchantToken, map[string]any{
		"identity": "merchant",
		"name":     cfg.BatchID + "merchant",
	})
	resBidder := producer.do("PUT", "/api/v1/users/me", bidderToken, map[string]any{
		"identity": "user",
		"name":     cfg.BatchID + "bidder",
	})
	rows := dbRows("SELECT account, identity, name FROM users WHERE account LIKE ? ORDER BY account", cfg.BatchID+"%")
	ok := resMerchant.ok() && resBidder.ok() && safeGet(rows, 1, "identity") == "merchant"
	return Result{
		Name:     "promote merchant",
		Pass:     ok,
		Reason:   fmt.Sprintf("merchant_status=%d bidder_status=%d", resMerchant.Status, resBidder.Status),
		Request:  "PUT producer /api/v1/users/me {identity}",
		Response: fmt.Sprintf("merchant=%s bidder=%s", resMerchant.brief(), resBidder.brief()),
		DB:       fmt.Sprintf("SELECT account,identity,name WHERE account LIKE batch -> %v", rows),
		Redis:    "N/A",
	}
}

func caseCreateAndStartRoom() Result {
	if merchantToken == "" {
		return failedPrereq("create and start room", "missing merchant token")
	}
	resCreate := producer.do("POST", "/api/v1/merchant/room", merchantToken, map[string]string{
		"title": cfg.BatchID + "room",
	})
	roomID = mustStr(resCreate.Body, "data", "id")
	resStart := httpResult{}
	if roomID != "" {
		resStart = producer.do("POST", "/api/v1/rooms/"+roomID+"/start", merchantToken, nil)
	}
	rows := dbRows("SELECT id, status, title FROM live_rooms WHERE id = ?", roomID)
	ok := resCreate.ok() && resStart.ok() && roomID != "" && safeGet(rows, 0, "status") == "live"
	return Result{
		Name:     "create and start room",
		Pass:     ok,
		Reason:   fmt.Sprintf("room_id=%s create=%d start=%d", roomID, resCreate.Status, resStart.Status),
		Request:  "POST producer /api/v1/merchant/room; POST producer /api/v1/rooms/{room_id}/start",
		Response: fmt.Sprintf("room_id=%s", roomID),
		DB:       fmt.Sprintf("SELECT id,status,title FROM live_rooms WHERE id=%s -> %v", roomID, rows),
		Redis:    fmt.Sprintf("room state before item=%v", redisHGetAll(roomStateKey(roomID))),
	}
}

func caseCreateAndPublishItem() Result {
	if merchantToken == "" || roomID == "" {
		return failedPrereq("create and publish item", "missing merchant token or room id")
	}
	start := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	end := time.Now().Add(6 * time.Minute).UTC().Format(time.RFC3339Nano)
	resCreate := producer.do("POST", "/api/v1/items", merchantToken, map[string]any{
		"room_id":     roomID,
		"title":       cfg.BatchID + "item",
		"description": "local multipod event bus test",
		"rule": map[string]any{
			"start_price":    1000,
			"bid_increment":  100,
			"price_cap":      1200,
			"deposit_amount": 0,
			"start_time":     start,
			"end_time":       end,
		},
	})
	itemID = mustStr(resCreate.Body, "data", "item_id")
	resPublish := httpResult{}
	if itemID != "" {
		resPublish = producer.do("POST", "/api/v1/items/"+itemID+"/publish", merchantToken, nil)
	}
	rows := dbRows("SELECT id, status, title FROM auction_items WHERE id = ?", itemID)
	queue := redisZMembers(roomQueueKey(roomID))
	ok := resCreate.ok() && resPublish.ok() && itemID != "" && safeGet(rows, 0, "status") == "published" && contains(queue, itemID)
	return Result{
		Name:     "create and publish item",
		Pass:     ok,
		Reason:   fmt.Sprintf("item_id=%s create=%d publish=%d", itemID, resCreate.Status, resPublish.Status),
		Request:  "POST producer /api/v1/items; POST producer /api/v1/items/{item_id}/publish",
		Response: fmt.Sprintf("item_id=%s", itemID),
		DB:       fmt.Sprintf("SELECT id,status,title FROM auction_items WHERE id=%s -> %v", itemID, rows),
		Redis:    fmt.Sprintf("%s -> %v", roomQueueKey(roomID), queue),
	}
}

func caseConnectSubscriberWebSockets() Result {
	if roomID == "" || observerToken == "" || bidderToken == "" {
		return failedPrereq("connect subscriber websockets", "missing room or user tokens")
	}
	observerTicket, err := issueTicket(subscriber, observerToken)
	if err == nil {
		observerConn, err = connectWS(subscriber.base, roomID, observerTicket)
	}
	bidderTicket := ""
	if err == nil {
		bidderTicket, err = issueTicket(subscriber, bidderToken)
	}
	if err == nil {
		bidderConn, err = connectWS(subscriber.base, roomID, bidderTicket)
	}
	time.Sleep(200 * time.Millisecond)
	online := redisSMembers(onlineUsersKey(roomID))
	ok := err == nil && observerConn != nil && bidderConn != nil && contains(online, observerID) && contains(online, bidderID)
	return Result{
		Name:     "connect subscriber websockets",
		Pass:     ok,
		Reason:   fmt.Sprintf("err=%v online_users=%d", err, len(online)),
		Request:  "POST subscriber /api/v1/ws-ticket x2; GET subscriber /ws/v1/rooms/{room_id}?ticket=<omitted>",
		Response: "two WebSocket connections established on subscriber",
		DB:       "N/A",
		Redis:    fmt.Sprintf("%s -> %v; ticket values omitted", onlineUsersKey(roomID), online),
	}
}

func caseStartItemFanout() Result {
	if merchantToken == "" || itemID == "" || observerConn == nil {
		return failedPrereq("start item fanout", "missing merchant token, item id, or observer websocket")
	}
	res := producer.do("POST", "/api/v1/items/"+itemID+"/start", merchantToken, nil)
	events, err := collectEvents(observerConn, map[string]bool{"auction_started": true}, 8*time.Second)
	rows := dbRows("SELECT id, status FROM auction_items WHERE id = ?", itemID)
	state := redisHGetAll(itemStateKey(itemID))
	ok := res.ok() && err == nil && safeGet(rows, 0, "status") == "ongoing" && state["status"] == "ongoing"
	return Result{
		Name:     "start item fanout",
		Pass:     ok,
		Reason:   fmt.Sprintf("start_status=%d collect_err=%v events=%v", res.Status, err, eventTypes(events)),
		Request:  "POST producer /api/v1/items/{item_id}/start",
		Response: fmt.Sprintf("subscriber observer events=%v", eventTypes(events)),
		DB:       fmt.Sprintf("SELECT id,status FROM auction_items WHERE id=%s -> %v", itemID, rows),
		Redis:    fmt.Sprintf("%s.status=%s", itemStateKey(itemID), state["status"]),
	}
}

func casePriceCapBidFanoutAndUnicast() Result {
	if bidderToken == "" || itemID == "" || observerConn == nil || bidderConn == nil {
		return failedPrereq("price cap bid fanout and unicast", "missing bidder token, item id, or websockets")
	}
	bidKey = cfg.BatchID + "bid_cap"
	res := producer.do("POST", "/api/v1/items/"+itemID+"/bids", bidderToken, map[string]any{
		"price":           1200,
		"idempotency_key": bidKey,
	})
	observerEvents, observerErr := collectEvents(observerConn, map[string]bool{
		"bid_success":   true,
		"auction_ended": true,
	}, 8*time.Second)
	bidderEvents, bidderErr := collectEvents(bidderConn, map[string]bool{
		"order_created": true,
	}, 8*time.Second)
	rows := dbRows("SELECT id, user_id, price, status FROM orders WHERE item_id = ?", itemID)
	state := redisHGetAll(itemStateKey(itemID))
	ok := res.ok() && observerErr == nil && bidderErr == nil && len(rows) == 1 && safeGet(rows, 0, "user_id") == bidderID && safeGet(rows, 0, "status") == "pending"
	return Result{
		Name:     "price cap bid fanout and unicast",
		Pass:     ok,
		Reason:   fmt.Sprintf("bid_status=%d observer_err=%v bidder_err=%v", res.Status, observerErr, bidderErr),
		Request:  "POST producer /api/v1/items/{item_id}/bids {price:1200,idempotency_key:<batch>}",
		Response: fmt.Sprintf("observer=%v bidder=%v", eventTypes(observerEvents), eventTypes(bidderEvents)),
		DB:       fmt.Sprintf("SELECT id,user_id,price,status FROM orders WHERE item_id=%s -> %v", itemID, rows),
		Redis:    fmt.Sprintf("%s.status=%s %s.leader_user_id=%s", itemStateKey(itemID), state["status"], itemStateKey(itemID), state["leader_user_id"]),
	}
}

func caseHTTPFinalStateOnSubscriber() Result {
	if itemID == "" {
		return failedPrereq("http final state on subscriber", "missing item id")
	}
	res := subscriber.do("GET", "/api/v1/items/"+itemID, "", nil)
	status := mustStr(res.Body, "data", "status")
	leader := mustStr(res.Body, "data", "leader_user_id")
	dealPrice := asInt64(res.Body, "data", "deal_price")
	rows := dbRows("SELECT id, status, winner_id, deal_price FROM auction_items WHERE id = ?", itemID)
	ok := res.ok() && status == "ended" && leader == bidderID && dealPrice == 1200 && safeGet(rows, 0, "status") == "ended"
	return Result{
		Name:     "http final state on subscriber",
		Pass:     ok,
		Reason:   fmt.Sprintf("status=%s leader=%s deal_price=%d", status, leader, dealPrice),
		Request:  "GET subscriber /api/v1/items/{item_id}",
		Response: fmt.Sprintf("status=%s leader_user_id=%s deal_price=%d", status, leader, dealPrice),
		DB:       fmt.Sprintf("SELECT id,status,winner_id,deal_price FROM auction_items WHERE id=%s -> %v", itemID, rows),
		Redis:    fmt.Sprintf("%s -> %v", itemStateKey(itemID), redisHGetAll(itemStateKey(itemID))),
	}
}

func startBackendPair() error {
	dsn := os.Getenv("TEST_DSN")
	tmp, err := os.MkdirTemp("", "agent-multipod-runner-*")
	if err != nil {
		return err
	}
	tmpConfigDir = tmp
	producerCfg := filepath.Join(tmp, "producer.yaml")
	subscriberCfg := filepath.Join(tmp, "subscriber.yaml")
	if err := os.WriteFile(producerCfg, []byte(localConfig("18080", dsn, cfg.RedisAddr)), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(subscriberCfg, []byte(localConfig("18081", dsn, cfg.RedisAddr)), 0o600); err != nil {
		return err
	}
	if err := startBackend("producer", producerCfg); err != nil {
		return err
	}
	if err := startBackend("subscriber", subscriberCfg); err != nil {
		return err
	}
	if err := waitReady(producer, 45*time.Second); err != nil {
		return fmt.Errorf("producer ready: %w", err)
	}
	if err := waitReady(subscriber, 45*time.Second); err != nil {
		return fmt.Errorf("subscriber ready: %w", err)
	}
	return nil
}

func startBackend(name, configPath string) error {
	logPath := filepath.Join(tmpConfigDir, name+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "run", ".", "server", "-c", configPath)
	cmd.Dir = cfg.RepoRoot
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "GOCACHE="+cfg.GoCache)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	proc := &backendProcess{name: name, cmd: cmd, log: logFile, done: make(chan struct{})}
	started = append(started, proc)
	go func() {
		_ = cmd.Wait()
		close(proc.done)
	}()
	return nil
}

func localConfig(port, dsn, redisAddr string) string {
	return fmt.Sprintf(`mode: debug
app:
  name: live-auction-backend
  version: agent-multipod-local
http:
  host: 127.0.0.1
  port: "%s"
database:
  driver: mysql
  dsn: %q
  max_idle_conns: 5
  max_open_conns: 20
  conn_max_lifetime: 30m
redis:
  addr: %s
  password: ""
  db: 0
auth:
  token_secret: live-auction-development-secret
  token_ttl: 24h
security:
  allowed_origins:
    - "*"
auction:
  extend_trigger_sec: 30
  auto_extend_sec: 10
  max_extend_count: 6
  max_total_extend_sec: 300
observability:
  enabled: false
  service_name: live-auction-backend
  service_version: agent-multipod-local
  environment: local
  otlp_endpoint: 127.0.0.1:4317
  otlp_insecure: true
  trace_sample_ratio: 0
  metrics_interval: 15s
  logs:
    format: json
    output: stdout
    include_trace_context: true
`, port, dsn, redisAddr)
}

func waitReady(c apiClient, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		res := c.do("GET", "/readyz", "", nil)
		if res.ok() && mustStr(res.Body, "data", "status") == "ok" {
			return nil
		}
		last = fmt.Errorf("status=%d err=%v", res.Status, res.Err)
		time.Sleep(500 * time.Millisecond)
	}
	return last
}

func registerUser(c apiClient, account string) (token, userID string, err error) {
	res := c.do("POST", "/api/v1/auth/register", "", map[string]string{
		"account":  account,
		"password": "agentPass123",
	})
	if !res.ok() {
		return "", "", fmt.Errorf("register %s: %s", account, res.brief())
	}
	return mustStr(res.Body, "data", "token"), mustStr(res.Body, "data", "user", "id"), nil
}

func issueTicket(c apiClient, token string) (string, error) {
	res := c.do("POST", "/api/v1/ws-ticket", token, nil)
	ticket := mustStr(res.Body, "data", "ticket")
	if !res.ok() || ticket == "" {
		return "", fmt.Errorf("issue ticket: %s", res.brief())
	}
	issuedTickets = append(issuedTickets, ticket)
	return ticket, nil
}

func connectWS(base, roomID, ticket string) (*websocket.Conn, error) {
	u, err := url.Parse(strings.Replace(base, "http://", "ws://", 1))
	if err != nil {
		return nil, err
	}
	u.Path = "/ws/v1/rooms/" + roomID
	q := u.Query()
	q.Set("ticket", ticket)
	q.Set("stream", "all")
	u.RawQuery = q.Encode()
	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial status=%d err=%w", resp.StatusCode, err)
		}
		return nil, err
	}
	return conn, nil
}

func collectEvents(conn *websocket.Conn, want map[string]bool, timeout time.Duration) ([]wsEvent, error) {
	deadline := time.Now().Add(timeout)
	seen := map[string]bool{}
	var events []wsEvent
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return events, err
		}
		var event wsEvent
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		events = append(events, event)
		if want[event.Type] {
			seen[event.Type] = true
		}
		if allSeen(want, seen) {
			return events, nil
		}
	}
	missing := make([]string, 0, len(want))
	for typ := range want {
		if !seen[typ] {
			missing = append(missing, typ)
		}
	}
	return events, fmt.Errorf("missing events: %s", strings.Join(missing, ","))
}

func (c apiClient) do(method, path, token string, payload any) httpResult {
	var reader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return httpResult{Err: err}
		}
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return httpResult{Err: err}
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return httpResult{Err: err}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return httpResult{Status: resp.StatusCode, Body: body, Raw: string(raw)}
}

func (r httpResult) ok() bool {
	return r.Err == nil && r.Status >= 200 && r.Status < 300 && asInt64Map(r.Body, "code") == 0
}

func (r httpResult) brief() string {
	if r.Err != nil {
		return r.Err.Error()
	}
	return fmt.Sprintf("status=%d code=%d message=%s", r.Status, asInt64Map(r.Body, "code"), mustStr(r.Body, "message"))
}

func cleanup() {
	fmt.Println("\n=== CLEANUP")
	if observerConn != nil {
		_ = observerConn.Close()
		fmt.Println("  close observer websocket: ok")
	}
	if bidderConn != nil {
		_ = bidderConn.Close()
		fmt.Println("  close bidder websocket: ok")
	}
	cleanupRedis()
	cleanupMySQL()
	stopBackends()
	if rdb != nil {
		_ = rdb.Close()
	}
	if db != nil {
		_ = db.Close()
	}
	if tmpConfigDir != "" {
		_ = os.RemoveAll(tmpConfigDir)
		fmt.Println("  remove temp backend config dir: ok")
	}
}

func cleanupRedis() {
	if rdb == nil {
		fmt.Println("  redis cleanup skipped: client unavailable")
		return
	}
	deleted := int64(0)
	if roomID != "" {
		keys := []string{roomStateKey(roomID), onlineUsersKey(roomID), roomQueueKey(roomID)}
		n, _ := rdb.Del(ctx, keys...).Result()
		deleted += n
	}
	if itemID != "" {
		keys := []string{
			itemStateKey(itemID),
			itemDetailKey(itemID),
			rankingKey(itemID),
			rankingRebuildLockKey(itemID),
			rankingRebuildCooldownKey(itemID),
			bidderNamesKey(itemID),
		}
		n, _ := rdb.Del(ctx, keys...).Result()
		deleted += n
		_ = rdb.ZRem(ctx, endingKey(), itemID).Err()
		if bidKey != "" {
			n, _ := rdb.Del(ctx, idempotencyKey(itemID, bidKey)).Result()
			deleted += n
		}
	}
	for _, ticket := range issuedTickets {
		n, _ := rdb.Del(ctx, "ws:ticket:"+ticket).Result()
		deleted += n
	}
	fmt.Printf("  redis cleanup known room/item/ticket keys: deleted=%d\n", deleted)
}

func cleanupMySQL() {
	if db == nil {
		fmt.Println("  mysql cleanup skipped: db unavailable")
		return
	}
	rowsAffected := int64(0)
	queries := []struct {
		label string
		sql   string
		args  []any
	}{
		{"orders", "DELETE FROM orders WHERE item_id = ? OR user_id IN (SELECT id FROM users WHERE account LIKE ?)", []any{itemID, cfg.BatchID + "%"}},
		{"deposits", "DELETE FROM deposits WHERE item_id = ? OR user_id IN (SELECT id FROM users WHERE account LIKE ?)", []any{itemID, cfg.BatchID + "%"}},
		{"bid_logs", "DELETE FROM bid_logs WHERE item_id = ? OR user_id IN (SELECT id FROM users WHERE account LIKE ?)", []any{itemID, cfg.BatchID + "%"}},
		{"auction_rules", "DELETE FROM auction_rules WHERE item_id = ?", []any{itemID}},
		{"auction_items", "DELETE FROM auction_items WHERE id = ? OR title LIKE ?", []any{itemID, cfg.BatchID + "%"}},
		{"live_rooms", "DELETE FROM live_rooms WHERE id = ? OR title LIKE ?", []any{roomID, cfg.BatchID + "%"}},
		{"users", "DELETE FROM users WHERE account LIKE ?", []any{cfg.BatchID + "%"}},
	}
	for _, q := range queries {
		res, err := db.ExecContext(ctx, q.sql, q.args...)
		if err != nil {
			fmt.Printf("  mysql cleanup %s: err=%v\n", q.label, err)
			continue
		}
		n, _ := res.RowsAffected()
		rowsAffected += n
		fmt.Printf("  mysql cleanup %s: rows=%d\n", q.label, n)
	}
	fmt.Printf("  mysql cleanup total: rows=%d\n", rowsAffected)
}

func stopBackends() {
	for _, p := range started {
		if p == nil || p.cmd == nil || p.cmd.Process == nil {
			continue
		}
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGINT)
		select {
		case <-p.done:
			fmt.Printf("  stop backend %s: graceful\n", p.name)
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			fmt.Printf("  stop backend %s: killed after timeout\n", p.name)
		}
		if p.log != nil {
			_ = p.log.Close()
		}
	}
}

func printResult(r Result) {
	status := "PASS"
	if !r.Pass {
		status = "FAIL"
	}
	fmt.Printf("\n=== CASE: %s\n", r.Name)
	fmt.Printf("  REQUEST:  %s\n", r.Request)
	fmt.Printf("  RESPONSE: %s\n", r.Response)
	fmt.Printf("  DB:       %s\n", r.DB)
	fmt.Printf("  REDIS:    %s\n", r.Redis)
	fmt.Printf("  RESULT:   %s -- %s\n", status, r.Reason)
}

func failedPrereq(name, reason string) Result {
	return Result{
		Name:     name,
		Pass:     false,
		Reason:   reason,
		Request:  "skipped due to missing prerequisite",
		Response: "N/A",
		DB:       "N/A",
		Redis:    "N/A",
	}
}

func summarizeReady(res httpResult) string {
	if res.Err != nil {
		return res.Err.Error()
	}
	components, _ := nestedMap(res.Body, "data", "components")
	return fmt.Sprintf("status=%d app_status=%s components=%v", res.Status, mustStr(res.Body, "data", "status"), components)
}

func dbRows(query string, args ...any) []map[string]string {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return []map[string]string{{"error": err.Error()}}
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]string{}
	for rows.Next() {
		vals := make([][]byte, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		_ = rows.Scan(ptrs...)
		row := make(map[string]string, len(cols))
		for i, col := range cols {
			row[col] = string(vals[i])
		}
		out = append(out, row)
	}
	return out
}

func redisHGetAll(key string) map[string]string {
	if rdb == nil || key == "" {
		return nil
	}
	val, err := rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil
	}
	return val
}

func redisZMembers(key string) []string {
	if rdb == nil || key == "" {
		return nil
	}
	val, err := rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil
	}
	return val
}

func redisSMembers(key string) []string {
	if rdb == nil || key == "" {
		return nil
	}
	val, err := rdb.SMembers(ctx, key).Result()
	if err != nil {
		return nil
	}
	return val
}

func mustStr(m map[string]any, keys ...string) string {
	cur := m
	for i, key := range keys {
		if i == len(keys)-1 {
			switch v := cur[key].(type) {
			case string:
				return v
			case fmt.Stringer:
				return v.String()
			default:
				if v != nil {
					return fmt.Sprint(v)
				}
				return ""
			}
		}
		next, ok := cur[key].(map[string]any)
		if !ok {
			return ""
		}
		cur = next
	}
	return ""
}

func nestedMap(m map[string]any, keys ...string) (map[string]any, bool) {
	cur := m
	for _, key := range keys {
		next, ok := cur[key].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func asInt64(m map[string]any, keys ...string) int64 {
	if len(keys) == 0 {
		return 0
	}
	cur := m
	for _, key := range keys[:len(keys)-1] {
		next, ok := cur[key].(map[string]any)
		if !ok {
			return 0
		}
		cur = next
	}
	return asInt64Value(cur[keys[len(keys)-1]])
}

func asInt64Map(m map[string]any, key string) int64 {
	return asInt64Value(m[key])
}

func asInt64Value(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func safeGet(rows []map[string]string, idx int, key string) string {
	if idx >= len(rows) {
		return ""
	}
	return rows[idx][key]
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func eventTypes(events []wsEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func allSeen(want, seen map[string]bool) bool {
	for typ := range want {
		if !seen[typ] {
			return false
		}
	}
	return true
}

func processSummary() string {
	parts := make([]string, 0, len(started))
	for _, p := range started {
		pid := 0
		if p != nil && p.cmd != nil && p.cmd.Process != nil {
			pid = p.cmd.Process.Pid
		}
		parts = append(parts, fmt.Sprintf("%s_pid=%d", p.name, pid))
	}
	return strings.Join(parts, " ")
}

func envOr(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func roomQueueKey(id string) string          { return "auction:room:" + id + ":item_queue" }
func roomStateKey(id string) string          { return "auction:room:" + id + ":state" }
func onlineUsersKey(id string) string        { return "auction:room:" + id + ":online_users" }
func itemStateKey(id string) string          { return "auction:item:" + id + ":state" }
func itemDetailKey(id string) string         { return "auction:item:" + id + ":detail" }
func rankingKey(id string) string            { return "auction:item:" + id + ":ranking" }
func rankingRebuildLockKey(id string) string { return "auction:item:" + id + ":ranking:rebuild_lock" }
func rankingRebuildCooldownKey(id string) string {
	return "auction:item:" + id + ":ranking:rebuild_cooldown"
}
func bidderNamesKey(id string) string { return "auction:item:" + id + ":bidder_names" }
func idempotencyKey(itemID, key string) string {
	return "auction:item:" + itemID + ":idempotency:" + key
}
func endingKey() string { return "auction:ending" }
