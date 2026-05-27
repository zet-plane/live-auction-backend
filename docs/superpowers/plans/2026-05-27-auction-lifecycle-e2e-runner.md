# Auction Lifecycle E2E Runner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and run a structured Go runner that verifies the auction lifecycle E2E flow across HTTP, MySQL, Redis, WebSocket, room queue, countdown, deposits, multi-round bidding, and settlement.

**Architecture:** The runner is an ephemeral Go program under `/tmp/agent-runner-<batch>` derived from `docs/agent-testing/templates/runner.go`. It drives the public HTTP/WebSocket API, records structured evidence from MySQL and Redis, and writes a Markdown report under `docs/agent-testing/reports/` after execution. No production data or reusable credentials are written to repository files.

**Tech Stack:** Go 1.23, `net/http`, `database/sql`, `github.com/go-sql-driver/mysql`, `github.com/redis/go-redis/v9`, `github.com/gorilla/websocket`, Markdown reports.

---

## File Structure

Runtime files:

- Create: `/tmp/agent-runner-auction-e2e/main.go`  
  The executable E2E runner. It copies the repo template, then adds WebSocket helpers, shared state, scenario cases, SQL/Redis assertions, and cleanup.
- Create: `/tmp/agent-runner-auction-e2e/go.mod`  
  Minimal module for runner dependencies.
- Create: `/tmp/agent-runner-auction-e2e/runner.out`  
  Captured stdout from the runner, used as report evidence.

Repository files:

- Create: `docs/agent-testing/reports/YYYYMMDD-HHMMSS-auction-lifecycle-flow.md`  
  Final test report generated after the runner finishes.
- Read only: `docs/agent-testing/README.md`
- Read only: `docs/agent-testing/guides/runner.md`
- Read only: `docs/agent-testing/guides/go-runner.md`
- Read only: `docs/agent-testing/guides/environment.md`
- Read only: `docs/agent-testing/flows/auction-lifecycle.md`
- Read only: `docs/agent-testing/modules/room.md`
- Read only: `docs/agent-testing/modules/item.md`
- Read only: `docs/agent-testing/modules/deposit.md`
- Read only: `docs/agent-testing/modules/bid.md`
- Read only: `docs/agent-testing/modules/ws.md`
- Read only: `docs/agent-testing/modules/user.md`
- Read only: `docs/agent-testing/reports/README.md`

---

### Task 1: Preflight Contract And Environment Check

**Files:**
- Read: `docs/agent-testing/README.md`
- Read: `docs/agent-testing/guides/runner.md`
- Read: `docs/agent-testing/guides/environment.md`
- Read: `docs/agent-testing/flows/auction-lifecycle.md`
- Read: `docs/agent-testing/modules/user.md`
- Read: `docs/agent-testing/modules/room.md`
- Read: `docs/agent-testing/modules/item.md`
- Read: `docs/agent-testing/modules/deposit.md`
- Read: `docs/agent-testing/modules/bid.md`
- Read: `docs/agent-testing/modules/ws.md`

- [ ] **Step 1: Re-read the required contracts**

Run:

```bash
rtk sed -n '1,220p' docs/agent-testing/README.md
rtk sed -n '1,260p' docs/agent-testing/guides/runner.md
rtk sed -n '1,260p' docs/agent-testing/flows/auction-lifecycle.md
```

Expected: the flow requires room, item, deposit, bid, and ws module contracts; no `auction-session` module is required for execution.

- [ ] **Step 2: Check compile readiness**

Run:

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go test ./...
```

Expected: all packages compile. If this fails, stop E2E execution and report the failing package and error. Do not run real dependency tests while compile readiness is broken.

- [ ] **Step 3: Confirm the local service health**

Run after the backend is started:

```bash
rtk curl -s http://127.0.0.1:8080/api/v1/health
```

Expected: HTTP 200 with a health response. If the service is not running, start it in a separate terminal using the project config agreed for E2E and keep its logs available for the final report.

- [ ] **Step 4: Confirm database and Redis configuration**

Before writing the runner, set the DSN only in the shell environment:

```bash
export TEST_DSN='<redacted-local-or-test-dsn>'
```

Run:

```bash
rtk redis-cli -h 127.0.0.1 -p 6379 PING
```

Expected: `PONG`. Do not paste the real `TEST_DSN` value into a report or commit.

---

### Task 2: Create The Runner Workspace

**Files:**
- Create: `/tmp/agent-runner-auction-e2e/go.mod`
- Create: `/tmp/agent-runner-auction-e2e/main.go`

- [ ] **Step 1: Create the runner directory**

Run:

```bash
rtk mkdir -p /tmp/agent-runner-auction-e2e
```

Expected: directory exists.

- [ ] **Step 2: Copy the runner template**

Run:

```bash
rtk cp docs/agent-testing/templates/runner.go /tmp/agent-runner-auction-e2e/main.go
```

Expected: `/tmp/agent-runner-auction-e2e/main.go` exists and contains the template helper functions.

- [ ] **Step 3: Create `go.mod`**

Write `/tmp/agent-runner-auction-e2e/go.mod` with:

```go
module agent-runner

go 1.23

require (
	github.com/go-sql-driver/mysql v1.8.1
	github.com/gorilla/websocket v1.5.3
	github.com/redis/go-redis/v9 v9.17.2
)
```

Run:

```bash
cd /tmp/agent-runner-auction-e2e && rtk env GOCACHE=/tmp/live-auction-go-cache go mod tidy
```

Expected: dependencies resolve and `go.sum` is created.

---

### Task 3: Add Runner State, WebSocket Helpers, And Scenario List

**Files:**
- Modify: `/tmp/agent-runner-auction-e2e/main.go`

- [ ] **Step 1: Add WebSocket import**

Add this import to the existing import block:

```go
	"github.com/gorilla/websocket"
```

Expected: `go test` is not used for this runner, but `go run main.go` later compiles with WebSocket support.

- [ ] **Step 2: Replace the config constants**

Use this CONFIG block:

```go
const (
	baseURL   = "http://127.0.0.1:8080"
	batchID   = "agent_e2e_20260527160000"
	redisAddr = "127.0.0.1:6379"
)
```

If execution happens at a later time, update only the timestamp in `batchID` before the first run. Keep the `agent_e2e_` prefix.

- [ ] **Step 3: Add shared state**

Add this package-level state below the `Result` type:

```go
type actor struct {
	Account string
	UserID  string
	Token   string
	WS      *wsRecorder
}

var (
	password      = "agentPwd123"
	merchant      actor
	userA         actor
	userB         actor
	userC         actor
	roomID        string
	item1ID       string
	item2ID       string
	item1RuleID   string
	item2RuleID   string
	item1EndUnix  int64
	successBidCnt int
)
```

- [ ] **Step 4: Add WebSocket recorder helpers**

Add this code below the shared state:

```go
type wsEvent struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

type wsRecorder struct {
	name   string
	conn   *websocket.Conn
	events []wsEvent
	mu     sync.Mutex
}

func connectWS(name, roomID, token string) (*wsRecorder, string) {
	status, body := httpDo("POST", "/api/v1/ws-ticket", nil, token)
	ticket := mustStr(body, "data", "ticket")
	if status != 200 || ticket == "" {
		return &wsRecorder{name: name}, fmt.Sprintf("ticket status=%d body=%s", status, jsonStr(body))
	}
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/ws/v1/rooms/" + roomID + "?ticket=" + ticket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	rec := &wsRecorder{name: name, conn: conn}
	if err != nil {
		return rec, "dial err=" + err.Error()
	}
	go rec.readLoop()
	return rec, "connected ticket=<redacted>"
}

func (r *wsRecorder) readLoop() {
	for {
		var ev wsEvent
		if err := r.conn.ReadJSON(&ev); err != nil {
			return
		}
		r.mu.Lock()
		r.events = append(r.events, ev)
		r.mu.Unlock()
	}
}

func (r *wsRecorder) close() {
	if r != nil && r.conn != nil {
		_ = r.conn.Close()
	}
}

func (r *wsRecorder) countEvent(eventType string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, ev := range r.events {
		if ev.Type == eventType {
			n++
		}
	}
	return n
}

func wsCounts(eventType string) string {
	return fmt.Sprintf("A=%d B=%d C=%d",
		userA.WS.countEvent(eventType),
		userB.WS.countEvent(eventType),
		userC.WS.countEvent(eventType),
	)
}
```

- [ ] **Step 5: Replace `scenarios()`**

Use this scenario list:

```go
func scenarios() []func() Result {
	return []func() Result{
		caseRegisterActors,
		casePromoteMerchant,
		caseActivateAndStartRoom,
		caseCreateTwoItems,
		casePublishTwoItemsAndVerifyQueue,
		caseConnectWebSockets,
		caseStartItemAndVerifyCountdown,
		caseUserAMissingDepositRejected,
		caseUserAPayDeposit,
		caseMultiRoundBidding,
		caseRankingAfterBids,
		caseSettlementByCron,
		caseLateBidRejected,
		casePassiveUserCNoBusinessRows,
	}
}
```

---

### Task 4: Implement Actor, Room, Item, Queue, And Countdown Cases

**Files:**
- Modify: `/tmp/agent-runner-auction-e2e/main.go`

- [ ] **Step 1: Add `caseRegisterActors`**

Add:

```go
func registerActor(label string) actor {
	acct := batchID + "_" + label
	_, body := httpDo("POST", "/api/v1/auth/register", map[string]any{
		"account": acct, "password": password,
	}, "")
	return actor{
		Account: acct,
		UserID:  mustStr(body, "data", "user", "id"),
		Token:   mustStr(body, "data", "token"),
	}
}

func caseRegisterActors() Result {
	merchant = registerActor("merchant")
	userA = registerActor("user_a")
	userB = registerActor("user_b")
	userC = registerActor("user_c")
	rows := dbRows("SELECT account, identity, deleted_at FROM users WHERE account LIKE ?", batchID+"%")
	pass := merchant.Token != "" && userA.Token != "" && userB.Token != "" && userC.Token != "" && len(rows) == 4
	return Result{
		Name:     "register actors",
		Pass:     pass,
		Reason:   fmt.Sprintf("tokens merchant=%t A=%t B=%t C=%t rows=%d", merchant.Token != "", userA.Token != "", userB.Token != "", userC.Token != "", len(rows)),
		Request:  "POST /api/v1/auth/register x4 {account:" + batchID + "_*}",
		Response: "tokens redacted",
		DB:       fmt.Sprintf("users LIKE %s%% -> %v", batchID, rows),
		Redis:    "N/A",
	}
}
```

- [ ] **Step 2: Add `casePromoteMerchant`**

Add:

```go
func casePromoteMerchant() Result {
	status, body := httpDo("PUT", "/api/v1/users/me", map[string]any{
		"name": batchID + "_merchant",
		"identity": "merchant",
	}, merchant.Token)
	rows := dbRows("SELECT id, identity FROM users WHERE account = ?", merchant.Account)
	pass := status == 200 && safeGet(rows, 0, "identity") == "merchant"
	return Result{
		Name:     "promote merchant",
		Pass:     pass,
		Reason:   fmt.Sprintf("status=%d identity=%s", status, safeGet(rows, 0, "identity")),
		Request:  "PUT /api/v1/users/me {identity:merchant}",
		Response: fmt.Sprintf("%d %s", status, jsonStr(body)),
		DB:       fmt.Sprintf("SELECT identity WHERE account=%s -> %v", merchant.Account, rows),
		Redis:    "N/A",
	}
}
```

- [ ] **Step 3: Add `caseActivateAndStartRoom`**

Add:

```go
func caseActivateAndStartRoom() Result {
	status, body := httpDo("POST", "/api/v1/merchant/room", map[string]any{
		"title": batchID + "_room",
	}, merchant.Token)
	roomID = mustStr(body, "data", "id")
	startStatus, startBody := httpDo("POST", "/api/v1/rooms/"+roomID+"/start", nil, merchant.Token)
	rows := dbRows("SELECT id, status, merchant_id FROM live_rooms WHERE id = ? AND deleted_at IS NULL", roomID)
	state := redisHGetAll("auction:room:" + roomID + ":state")
	pass := status == 200 && startStatus == 200 && roomID != "" && safeGet(rows, 0, "status") == "live" && state["status"] == "live"
	return Result{
		Name:     "activate and start room",
		Pass:     pass,
		Reason:   fmt.Sprintf("room=%s status=%d start=%d db=%s redis=%s", roomID, status, startStatus, safeGet(rows, 0, "status"), state["status"]),
		Request:  "POST /api/v1/merchant/room; POST /api/v1/rooms/{room_id}/start",
		Response: fmt.Sprintf("activate=%d room_id=%s start=%d body=%s", status, roomID, startStatus, jsonStr(startBody)),
		DB:       fmt.Sprintf("live_rooms id=%s -> %v", roomID, rows),
		Redis:    fmt.Sprintf("auction:room:%s:state -> %v", roomID, state),
	}
}
```

- [ ] **Step 4: Add item creation helpers and `caseCreateTwoItems`**

Add:

```go
func createItem(suffix string) (string, string, int) {
	end := time.Now().Add(2 * time.Minute).UTC()
	status, body := httpDo("POST", "/api/v1/items", map[string]any{
		"room_id": roomID,
		"title": batchID + "_" + suffix,
		"description": "auction lifecycle e2e",
		"image_url": "https://example.com/e2e.png",
		"tags": []string{"e2e"},
		"rule": map[string]any{
			"start_price": 1000,
			"bid_increment": 100,
			"deposit_amount": 5000,
			"start_time": time.Now().UTC().Format(time.RFC3339),
			"end_time": end.Format(time.RFC3339),
		},
	}, merchant.Token)
	return mustStr(body, "data", "item_id"), mustStr(body, "data", "rule_id"), status
}

func caseCreateTwoItems() Result {
	var status1, status2 int
	item1ID, item1RuleID, status1 = createItem("item_1")
	item2ID, item2RuleID, status2 = createItem("item_2")
	rows := dbRows("SELECT id, room_id, status, rule_id FROM auction_items WHERE id IN (?, ?) ORDER BY id", item1ID, item2ID)
	rules := dbRows("SELECT id, item_id, start_price, bid_increment, deposit_amount, UNIX_TIMESTAMP(end_time) AS end_unix FROM auction_rules WHERE id IN (?, ?) ORDER BY id", item1RuleID, item2RuleID)
	item1EndUnix = mustInt64(safeGet(rules, 0, "end_unix"))
	pass := status1 == 200 && status2 == 200 && len(rows) == 2 && len(rules) == 2 && safeGet(rules, 0, "deposit_amount") == "5000"
	return Result{
		Name: "create two items",
		Pass: pass,
		Reason: fmt.Sprintf("item1=%s item2=%s status1=%d status2=%d rows=%d rules=%d", item1ID, item2ID, status1, status2, len(rows), len(rules)),
		Request: "POST /api/v1/items x2 {room_id, rule.deposit_amount:5000}",
		Response: fmt.Sprintf("item1=%s rule1=%s item2=%s rule2=%s", item1ID, item1RuleID, item2ID, item2RuleID),
		DB: fmt.Sprintf("items -> %v rules -> %v", rows, rules),
		Redis: "N/A",
	}
}
```

Add this helper near `safeGet` helpers:

```go
func mustInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
```

Also add `strconv` to the import block.

- [ ] **Step 5: Add `casePublishTwoItemsAndVerifyQueue`**

Add:

```go
func casePublishTwoItemsAndVerifyQueue() Result {
	status1, _ := httpDo("POST", "/api/v1/items/"+item1ID+"/publish", nil, merchant.Token)
	status2, _ := httpDo("POST", "/api/v1/items/"+item2ID+"/publish", nil, merchant.Token)
	queue := redisZMembers("auction:room:" + roomID + ":item_queue")
	roomStatus, roomBody := httpDo("GET", "/api/v1/rooms/"+roomID, nil, "")
	rows := dbRows("SELECT id, status FROM auction_items WHERE id IN (?, ?) ORDER BY id", item1ID, item2ID)
	has1 := contains(queue, item1ID)
	has2 := contains(queue, item2ID)
	pass := status1 == 200 && status2 == 200 && has1 && has2 && roomStatus == 200 && len(rows) == 2
	return Result{
		Name: "publish two items and verify queue",
		Pass: pass,
		Reason: fmt.Sprintf("publish=%d/%d queue_has_item1=%t queue_has_item2=%t room_status=%d", status1, status2, has1, has2, roomStatus),
		Request: "POST /api/v1/items/{id}/publish x2; GET /api/v1/rooms/{room_id}",
		Response: fmt.Sprintf("room=%d item_queue=%s", roomStatus, jsonStr(roomBody)),
		DB: fmt.Sprintf("auction_items -> %v", rows),
		Redis: fmt.Sprintf("auction:room:%s:item_queue -> %v", roomID, queue),
	}
}

func contains(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}
```

---

### Task 5: Implement WebSocket, Countdown, Deposit, Bidding, And Ranking Cases

**Files:**
- Modify: `/tmp/agent-runner-auction-e2e/main.go`

- [ ] **Step 1: Add `caseConnectWebSockets`**

Add:

```go
func caseConnectWebSockets() Result {
	var ra, rb, rc string
	userA.WS, ra = connectWS("A", roomID, userA.Token)
	userB.WS, rb = connectWS("B", roomID, userB.Token)
	userC.WS, rc = connectWS("C", roomID, userC.Token)
	time.Sleep(300 * time.Millisecond)
	pass := userA.WS.conn != nil && userB.WS.conn != nil && userC.WS.conn != nil
	return Result{
		Name: "connect A B C websocket",
		Pass: pass,
		Reason: fmt.Sprintf("A=%s B=%s C=%s", ra, rb, rc),
		Request: "POST /api/v1/ws-ticket x3; GET /ws/v1/rooms/{room_id}?ticket=<redacted> x3",
		Response: "tickets redacted",
		DB: "N/A",
		Redis: "ws:ticket consumed by successful GETDEL during handshake",
	}
}
```

- [ ] **Step 2: Add `caseStartItemAndVerifyCountdown`**

Add:

```go
func caseStartItemAndVerifyCountdown() Result {
	status, body := httpDo("POST", "/api/v1/items/"+item1ID+"/start", nil, merchant.Token)
	time.Sleep(500 * time.Millisecond)
	state := redisHGetAll("auction:item:" + item1ID + ":state")
	itemStatus, itemBody := httpDo("GET", "/api/v1/items/"+item1ID, nil, "")
	remaining := mustFloat64(itemBody, "data", "remaining_ms")
	endUnix := mustInt64(state["end_time_unix"])
	time.Sleep(1200 * time.Millisecond)
	_, itemBody2 := httpDo("GET", "/api/v1/items/"+item1ID, nil, "")
	remaining2 := mustFloat64(itemBody2, "data", "remaining_ms")
	pass := status == 200 && itemStatus == 200 && state["current_price"] == "1000" && endUnix > 0 && remaining > remaining2 && userA.WS.countEvent("auction_started") >= 1 && userB.WS.countEvent("auction_started") >= 1 && userC.WS.countEvent("auction_started") >= 1
	return Result{
		Name: "start item and verify countdown",
		Pass: pass,
		Reason: fmt.Sprintf("status=%d state_price=%s end_unix=%d remaining=%.0f remaining2=%.0f ws_started=%s", status, state["current_price"], endUnix, remaining, remaining2, wsCounts("auction_started")),
		Request: "POST /api/v1/items/{item_1_id}/start; GET /api/v1/items/{item_1_id} x2",
		Response: fmt.Sprintf("start=%d body=%s detail=%d", status, jsonStr(body), itemStatus),
		DB: fmt.Sprintf("auction_items id=%s status checked through HTTP and Redis", item1ID),
		Redis: fmt.Sprintf("auction:item:%s:state -> %v", item1ID, state),
	}
}
```

Add helper:

```go
func mustFloat64(m map[string]any, keys ...string) float64 {
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			switch v := cur[k].(type) {
			case float64:
				return v
			case int64:
				return float64(v)
			case json.Number:
				f, _ := v.Float64()
				return f
			default:
				return 0
			}
		}
		next, _ := cur[k].(map[string]any)
		if next == nil {
			return 0
		}
		cur = next
	}
	return 0
}
```

- [ ] **Step 3: Add `caseUserAMissingDepositRejected`**

Add:

```go
func caseUserAMissingDepositRejected() Result {
	before := redisHGetAll("auction:item:" + item1ID + ":state")
	bidLogsBefore := dbRows("SELECT COUNT(*) AS cnt FROM bid_logs WHERE item_id = ?", item1ID)
	status, body := httpDo("POST", "/api/v1/items/"+item1ID+"/bids", map[string]any{
		"price": 1100,
		"idempotency_key": batchID + "_user_a_1100_missing_deposit",
	}, userA.Token)
	after := redisHGetAll("auction:item:" + item1ID + ":state")
	bidLogsAfter := dbRows("SELECT COUNT(*) AS cnt FROM bid_logs WHERE item_id = ?", item1ID)
	pass := status == 400 && strings.Contains(jsonStr(body), "deposit required") && before["current_price"] == after["current_price"] && safeGet(bidLogsBefore, 0, "cnt") == safeGet(bidLogsAfter, 0, "cnt") && userA.WS.countEvent("bid_success") == 0 && userB.WS.countEvent("bid_success") == 0 && userC.WS.countEvent("bid_success") == 0
	return Result{
		Name: "A missing deposit rejected",
		Pass: pass,
		Reason: fmt.Sprintf("status=%d msg=%s bid_logs_before=%s after=%s ws_bid_success=%s", status, mustStr(body, "message"), safeGet(bidLogsBefore, 0, "cnt"), safeGet(bidLogsAfter, 0, "cnt"), wsCounts("bid_success")),
		Request: "POST /api/v1/items/{item_1_id}/bids {A price:1100 missing deposit}",
		Response: fmt.Sprintf("%d %s", status, jsonStr(body)),
		DB: fmt.Sprintf("bid_logs before=%v after=%v", bidLogsBefore, bidLogsAfter),
		Redis: fmt.Sprintf("before=%v after=%v", before, after),
	}
}
```

- [ ] **Step 4: Add deposit and bid helpers**

Add:

```go
func payDeposit(user actor, label string) (int, map[string]any, []map[string]string) {
	status, body := httpDo("POST", "/api/v1/items/"+item1ID+"/deposit/pay", nil, user.Token)
	rows := dbRows("SELECT item_id, user_id, amount, status FROM deposits WHERE item_id = ? AND user_id = ?", item1ID, user.UserID)
	return status, body, rows
}

func placeBid(user actor, label string, price int64, key string) (int, map[string]any, map[string]string, []map[string]string) {
	status, body := httpDo("POST", "/api/v1/items/"+item1ID+"/bids", map[string]any{
		"price": price,
		"idempotency_key": key,
	}, user.Token)
	time.Sleep(300 * time.Millisecond)
	state := redisHGetAll("auction:item:" + item1ID + ":state")
	rows := dbRows("SELECT user_id, price FROM bid_logs WHERE item_id = ? ORDER BY created_at, id", item1ID)
	return status, body, state, rows
}
```

- [ ] **Step 5: Add `caseUserAPayDeposit`**

Add:

```go
func caseUserAPayDeposit() Result {
	status, body, rows := payDeposit(userA, "A")
	pass := status == 200 && safeGet(rows, 0, "amount") == "5000" && safeGet(rows, 0, "status") == "paid"
	return Result{
		Name: "A pay deposit",
		Pass: pass,
		Reason: fmt.Sprintf("status=%d amount=%s deposit_status=%s", status, safeGet(rows, 0, "amount"), safeGet(rows, 0, "status")),
		Request: "POST /api/v1/items/{item_1_id}/deposit/pay as A",
		Response: fmt.Sprintf("%d %s", status, jsonStr(body)),
		DB: fmt.Sprintf("deposits A -> %v", rows),
		Redis: "N/A",
	}
}
```

- [ ] **Step 6: Add `caseMultiRoundBidding`**

Add:

```go
func caseMultiRoundBidding() Result {
	failStatus, failBody, failState, failLogs := placeBid(userB, "B", 1200, batchID+"_user_b_1200_missing_deposit")
	if failStatus == 200 {
		return Result{Name: "multi round bidding", Pass: false, Reason: "B bid succeeded without deposit", Request: "POST bid B missing deposit", Response: jsonStr(failBody), DB: fmt.Sprintf("%v", failLogs), Redis: fmt.Sprintf("%v", failState)}
	}
	depStatus, depBody, depRows := payDeposit(userB, "B")
	rounds := []struct {
		user actor
		name string
		price int64
		key string
		wantLeader string
	}{
		{userA, "A", 1100, batchID+"_user_a_1100", userA.UserID},
		{userB, "B", 1200, batchID+"_user_b_1200", userB.UserID},
		{userA, "A", 1300, batchID+"_user_a_1300", userA.UserID},
		{userB, "B", 1400, batchID+"_user_b_1400", userB.UserID},
		{userA, "A", 1500, batchID+"_user_a_1500", userA.UserID},
		{userB, "B", 1600, batchID+"_user_b_1600", userB.UserID},
	}
	pass := depStatus == 200 && safeGet(depRows, 0, "status") == "paid"
	reasons := []string{fmt.Sprintf("B missing deposit status=%d", failStatus), fmt.Sprintf("B deposit status=%d body=%s", depStatus, jsonStr(depBody))}
	var lastState map[string]string
	var lastRows []map[string]string
	for _, r := range rounds {
		status, body, state, rows := placeBid(r.user, r.name, r.price, r.key)
		lastState = state
		lastRows = rows
		ok := status == 200 && state["current_price"] == fmt.Sprintf("%d", r.price) && state["leader_user_id"] == r.wantLeader
		if !ok {
			pass = false
		}
		if ok {
			successBidCnt++
		}
		reasons = append(reasons, fmt.Sprintf("%s:%d status=%d leader=%s body=%s", r.name, r.price, status, state["leader_user_id"], jsonStr(body)))
	}
	pass = pass && successBidCnt == 6 && lastState["current_price"] == "1600" && lastState["leader_user_id"] == userB.UserID
	return Result{
		Name: "multi round bidding",
		Pass: pass,
		Reason: strings.Join(reasons, " | ") + " ws_bid_success=" + wsCounts("bid_success") + " ws_outbid=" + wsCounts("user_outbid"),
		Request: "B missing deposit bid; B deposit; A/B sequential bids 1100..1600",
		Response: fmt.Sprintf("success_bid_count=%d", successBidCnt),
		DB: fmt.Sprintf("bid_logs -> %v deposits_B -> %v", lastRows, depRows),
		Redis: fmt.Sprintf("auction:item:%s:state -> %v ranking -> %v", item1ID, lastState, redisZMembers("auction:item:"+item1ID+":ranking")),
	}
}
```

- [ ] **Step 7: Add `caseRankingAfterBids`**

Add:

```go
func caseRankingAfterBids() Result {
	status, body := httpDo("GET", "/api/v1/items/"+item1ID+"/ranking", nil, "")
	ranking := redisZMembers("auction:item:" + item1ID + ":ranking")
	rows := dbRows("SELECT user_id, MAX(price) AS max_price FROM bid_logs WHERE item_id = ? GROUP BY user_id ORDER BY max_price DESC", item1ID)
	pass := status == 200 && len(rows) >= 2 && safeGet(rows, 0, "user_id") == userB.UserID && safeGet(rows, 0, "max_price") == "1600" && safeGet(rows, 1, "user_id") == userA.UserID && safeGet(rows, 1, "max_price") == "1500" && !contains(ranking, userC.UserID)
	return Result{
		Name: "ranking after bids",
		Pass: pass,
		Reason: fmt.Sprintf("status=%d first=%s/%s second=%s/%s C_in_redis=%t", status, safeGet(rows, 0, "user_id"), safeGet(rows, 0, "max_price"), safeGet(rows, 1, "user_id"), safeGet(rows, 1, "max_price"), contains(ranking, userC.UserID)),
		Request: "GET /api/v1/items/{item_1_id}/ranking",
		Response: fmt.Sprintf("%d %s", status, jsonStr(body)),
		DB: fmt.Sprintf("bid_logs aggregate -> %v", rows),
		Redis: fmt.Sprintf("ranking -> %v", ranking),
	}
}
```

---

### Task 6: Implement Settlement, Late Bid, Passive C, And Cleanup

**Files:**
- Modify: `/tmp/agent-runner-auction-e2e/main.go`

- [ ] **Step 1: Add `caseSettlementByCron`**

Add:

```go
func caseSettlementByCron() Result {
	deadline := time.Now().Add(90 * time.Second)
	var rows []map[string]string
	for time.Now().Before(deadline) {
		rows = dbRows("SELECT id, status, winner_id, deal_price FROM auction_items WHERE id = ?", item1ID)
		if safeGet(rows, 0, "status") == "ended" {
			break
		}
		time.Sleep(5 * time.Second)
	}
	state := redisHGetAll("auction:item:" + item1ID + ":state")
	pass := safeGet(rows, 0, "status") == "ended" && safeGet(rows, 0, "winner_id") == userB.UserID && safeGet(rows, 0, "deal_price") == "1600" && userA.WS.countEvent("auction_ended") >= 1 && userB.WS.countEvent("auction_ended") >= 1 && userC.WS.countEvent("auction_ended") >= 1
	return Result{
		Name: "settlement by cron",
		Pass: pass,
		Reason: fmt.Sprintf("status=%s winner=%s deal=%s ws_ended=%s", safeGet(rows, 0, "status"), safeGet(rows, 0, "winner_id"), safeGet(rows, 0, "deal_price"), wsCounts("auction_ended")),
		Request: "wait up to 90s for item cron EndExpiredAuctions",
		Response: "N/A",
		DB: fmt.Sprintf("auction_items -> %v", rows),
		Redis: fmt.Sprintf("auction:item:%s:state -> %v", item1ID, state),
	}
}
```

Expected: the service cron runs every minute and ends the expired auction. If the item is not ended after 90 seconds, report a failure with DB/Redis evidence and service logs.

- [ ] **Step 2: Add `caseLateBidRejected`**

Add:

```go
func caseLateBidRejected() Result {
	beforeRows := dbRows("SELECT winner_id, deal_price FROM auction_items WHERE id = ?", item1ID)
	status, body := httpDo("POST", "/api/v1/items/"+item1ID+"/bids", map[string]any{
		"price": 1700,
		"idempotency_key": batchID + "_user_a_1700_after_end",
	}, userA.Token)
	afterRows := dbRows("SELECT winner_id, deal_price FROM auction_items WHERE id = ?", item1ID)
	pass := status == 400 && safeGet(afterRows, 0, "winner_id") == userB.UserID && safeGet(afterRows, 0, "deal_price") == "1600" && userA.WS.countEvent("bid_success") == userB.WS.countEvent("bid_success")
	return Result{
		Name: "late bid rejected",
		Pass: pass,
		Reason: fmt.Sprintf("status=%d winner=%s deal=%s", status, safeGet(afterRows, 0, "winner_id"), safeGet(afterRows, 0, "deal_price")),
		Request: "POST /api/v1/items/{item_1_id}/bids {A price:1700 after ended}",
		Response: fmt.Sprintf("%d %s", status, jsonStr(body)),
		DB: fmt.Sprintf("before=%v after=%v", beforeRows, afterRows),
		Redis: fmt.Sprintf("ranking -> %v", redisZMembers("auction:item:"+item1ID+":ranking")),
	}
}
```

- [ ] **Step 3: Add `casePassiveUserCNoBusinessRows`**

Add:

```go
func casePassiveUserCNoBusinessRows() Result {
	deposits := dbRows("SELECT COUNT(*) AS cnt FROM deposits WHERE item_id = ? AND user_id = ?", item1ID, userC.UserID)
	bids := dbRows("SELECT COUNT(*) AS cnt FROM bid_logs WHERE item_id = ? AND user_id = ?", item1ID, userC.UserID)
	ranking := redisZMembers("auction:item:" + item1ID + ":ranking")
	pass := safeGet(deposits, 0, "cnt") == "0" && safeGet(bids, 0, "cnt") == "0" && !contains(ranking, userC.UserID) && userC.WS.countEvent("bid_success") >= 1 && userC.WS.countEvent("user_outbid") == 0
	return Result{
		Name: "passive C has no business rows",
		Pass: pass,
		Reason: fmt.Sprintf("deposit_cnt=%s bid_cnt=%s C_in_ranking=%t C_bid_success=%d C_outbid=%d", safeGet(deposits, 0, "cnt"), safeGet(bids, 0, "cnt"), contains(ranking, userC.UserID), userC.WS.countEvent("bid_success"), userC.WS.countEvent("user_outbid")),
		Request: "DB/Redis/WS verification for passive user C",
		Response: "N/A",
		DB: fmt.Sprintf("deposits=%v bid_logs=%v", deposits, bids),
		Redis: fmt.Sprintf("ranking -> %v", ranking),
	}
}
```

- [ ] **Step 4: Replace `cleanup()`**

Use this cleanup:

```go
func cleanup() {
	fmt.Println("\n=== CLEANUP")
	if userA.WS != nil { userA.WS.close() }
	if userB.WS != nil { userB.WS.close() }
	if userC.WS != nil { userC.WS.close() }
	if db != nil {
		for _, q := range []struct {
			name string
			sql  string
			args []any
		}{
			{"delete deposits", "DELETE FROM deposits WHERE item_id IN (?, ?)", []any{item1ID, item2ID}},
			{"delete bid_logs", "DELETE FROM bid_logs WHERE item_id IN (?, ?)", []any{item1ID, item2ID}},
			{"soft-delete items", "UPDATE auction_items SET deleted_at=NOW() WHERE id IN (?, ?) AND deleted_at IS NULL", []any{item1ID, item2ID}},
			{"soft-delete room", "UPDATE live_rooms SET deleted_at=NOW() WHERE id = ? AND deleted_at IS NULL", []any{roomID}},
			{"soft-delete users", "UPDATE users SET deleted_at=NOW() WHERE account LIKE ? AND deleted_at IS NULL", []any{batchID + "%"}},
		} {
			res, err := db.ExecContext(ctx, q.sql, q.args...)
			n := int64(0)
			if res != nil {
				n, _ = res.RowsAffected()
			}
			fmt.Printf("  %s: rows=%d err=%v\n", q.name, n, err)
		}
	}
	if rdb != nil {
		keys := []string{
			"auction:item:" + item1ID + ":state",
			"auction:item:" + item1ID + ":ranking",
			"auction:item:" + item1ID + ":bidder_names",
			"auction:item:" + item2ID + ":state",
			"auction:item:" + item2ID + ":ranking",
			"auction:item:" + item2ID + ":bidder_names",
			"auction:room:" + roomID + ":state",
		}
		for _, k := range keys {
			err := rdb.Del(ctx, k).Err()
			fmt.Printf("  redis DEL %s err=%v\n", k, err)
		}
		for _, itemID := range []string{item1ID, item2ID} {
			err := rdb.ZRem(ctx, "auction:room:"+roomID+":item_queue", itemID).Err()
			fmt.Printf("  redis ZREM room queue item=%s err=%v\n", itemID, err)
		}
	}
}
```

Expected: cleanup prints row counts and Redis cleanup actions. It never uses `TRUNCATE`, `DROP`, `FLUSHDB`, or `FLUSHALL`.

---

### Task 7: Compile And Run The Runner

**Files:**
- Read: `/tmp/agent-runner-auction-e2e/main.go`
- Create: `/tmp/agent-runner-auction-e2e/runner.out`

- [ ] **Step 1: Format the runner**

Run:

```bash
cd /tmp/agent-runner-auction-e2e && rtk gofmt -w main.go
```

Expected: no formatting errors.

- [ ] **Step 2: Compile without running**

Run:

```bash
cd /tmp/agent-runner-auction-e2e && rtk env GOCACHE=/tmp/live-auction-go-cache go test
```

Expected: `? agent-runner [no test files]`. Compilation errors must be fixed before execution.

- [ ] **Step 3: Run the E2E runner**

Run:

```bash
cd /tmp/agent-runner-auction-e2e && rtk env GOCACHE=/tmp/live-auction-go-cache TEST_DSN="$TEST_DSN" go run main.go | tee runner.out
```

Expected:

```text
=== CASE: register actors
...
=== SUMMARY
  PASS: 14  FAIL: 0
  BATCH_ID: agent_e2e_<timestamp>

=== CLEANUP
...
```

If any case fails, preserve `runner.out`, service logs, and the batch ID. Do not rerun with the same batch ID after cleanup; use a new timestamped batch ID.

---

### Task 8: Write The Test Report

**Files:**
- Create: `docs/agent-testing/reports/YYYYMMDD-HHMMSS-auction-lifecycle-flow.md`

- [ ] **Step 1: Create the report from the required template**

Use the naming format:

```text
docs/agent-testing/reports/YYYYMMDD-HHMMSS-auction-lifecycle-flow.md
```

Write the report with these sections:

```markdown
# 测试报告：auction-lifecycle

## 基本信息

- 测试目标：竞拍基础流程 E2E 一致性测试
- 测试类型：flow / state-consistency / websocket
- 测试时间：<实际执行时间>
- 执行 agent：Codex
- 读取文档：
  - docs/agent-testing/README.md
  - docs/agent-testing/guides/runner.md
  - docs/agent-testing/guides/go-runner.md
  - docs/agent-testing/guides/environment.md
  - docs/agent-testing/flows/auction-lifecycle.md
  - docs/agent-testing/modules/user.md
  - docs/agent-testing/modules/room.md
  - docs/agent-testing/modules/item.md
  - docs/agent-testing/modules/deposit.md
  - docs/agent-testing/modules/bid.md
  - docs/agent-testing/modules/ws.md
  - docs/agent-testing/reports/README.md

## 测试环境

- 服务地址：http://127.0.0.1:8080
- MySQL：测试数据库，地址和凭据已省略
- Redis：测试 Redis，地址和凭据已省略
- WebSocket：真实连接，ticket 值已省略

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | 真实本地服务 | 验证真实路由和 handler |
| MySQL | 测试库，仅本批次数据 | 验证持久化状态 |
| Redis | 测试 Redis，仅本批次 key/member | 验证缓存和实时状态 |
| WebSocket | A/B/C 真实连接 | 验证房间广播和用户单播 |
| 第三方支付 | 不调用 | 保证金当前为站内状态写入 |

## 测试数据

- 测试批次 ID：<batchID>
- 创建数据：商家 S、用户 A/B/C、房间 Room、拍品 P1/P2、保证金 A/B、BidLog 多条
- 复用数据：无

## 执行步骤

粘贴 runner 的 CASE 名称列表和摘要。

## 验证证据

粘贴 `/tmp/agent-runner-auction-e2e/runner.out` 中每个 CASE 的关键证据摘要。

## 通过项

- 根据 runner SUMMARY 填写。

## 失败项

- 无，或按失败 case 填写。

## 跳过项

- 并发测试：本次基础 E2E 不覆盖，后续作为专项执行。

## Apifox 对齐偏差

- 本次不是接口契约专项；未执行 Apifox 对齐。

## 风险和建议

- 记录 settlement cron 等待是否稳定。
- 记录 order_created 是赢家单播还是房间广播的实际表现。

## 建议沉淀的回归测试

- 将 A/B 多轮出价、保证金门槛、C 旁观 WebSocket 边界沉淀为定期 E2E。

## 已知缺口

- 并发出价未覆盖。
- Redis/MySQL 故障注入未覆盖。

## 测试数据清理结果

粘贴 runner CLEANUP 块。
```

- [ ] **Step 2: Stage and commit the report**

Run:

```bash
rtk git add docs/agent-testing/reports/YYYYMMDD-HHMMSS-auction-lifecycle-flow.md
rtk git commit -m "test(agent-testing): record auction lifecycle e2e report"
```

Expected: a commit containing only the report file.

---

## Self-Review

Spec coverage:

- Multi-item listing is covered by Task 4 step 5.
- Countdown is covered by Task 5 step 2.
- Deposit gate is covered by Task 5 steps 3 to 6.
- A/B multi-round bidding is covered by Task 5 step 6.
- Passive C WebSocket behavior is covered by Task 5 step 1 and Task 6 step 3.
- Settlement and late bid rejection are covered by Task 6 steps 1 and 2.
- Cleanup and reporting are covered by Task 6 step 4 and Task 8.

Implementation constraints:

- The runner operates only on `batchID` data and recorded IDs.
- The plan does not require modifying production source code.
- Secrets are only read from environment variables and are not written to committed files.
- Destructive DB/Redis operations are excluded.
