# Agent Testing Go Runner Template — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a structured Go runner template to `docs/agent-testing/` so agents executing interface contract, integration, and state-consistency tests produce reliable, reproducible verification evidence instead of ad-hoc curl output.

**Architecture:** Two new files (`go-runner-guide.md` and `runner-template.go`) are added to `docs/agent-testing/`. The guide explains when and how to use the runner; the template is a standalone Go program agents copy to `/tmp`, fill with test scenarios, and run. Two existing files (`README.md` and `agent-runner-guide.md`) receive minimal updates to route agents toward the new guide.

**Tech Stack:** Go 1.23.6, `github.com/go-sql-driver/mysql` v1.8.1, `github.com/redis/go-redis/v9` v9.17.2

---

## File Map

| Action | Path | Responsibility |
|---|---|---|
| Modify | `docs/agent-testing/README.md` | Add one routing row for `go-runner-guide.md` |
| Modify | `docs/agent-testing/agent-runner-guide.md` | Add one reference in dependency strategy section |
| Create | `docs/agent-testing/go-runner-guide.md` | When to use, setup steps, output format contract |
| Create | `docs/agent-testing/runner-template.go` | Standalone Go template agents copy to `/tmp` |

---

## Task 1: Update README.md routing table

**Files:**
- Modify: `docs/agent-testing/README.md`

- [ ] **Step 1: Add one row to the second-layer routing table**

In `docs/agent-testing/README.md`, find the 第二层 routing table (around line 62-67):

```markdown
| 用户意图 | 下一步读取 |
| --- | --- |
| 准备环境、连接数据库/Redis、启动服务、创建测试数据 | `environment.md` |
| 执行模块、流程、接口、并发或状态一致性测试 | `agent-runner-guide.md` |
| 生成或补充模块测试文档 | `module-generator-guide.md` |
| 写入或补充测试报告 | `reports/README.md` |
```

Replace with:

```markdown
| 用户意图 | 下一步读取 |
| --- | --- |
| 准备环境、连接数据库/Redis、启动服务、创建测试数据 | `environment.md` |
| 执行模块、流程、接口、并发或状态一致性测试 | `agent-runner-guide.md` |
| 执行接口契约、集成或状态一致性测试，需要结构化证据采集 | `go-runner-guide.md` |
| 生成或补充模块测试文档 | `module-generator-guide.md` |
| 写入或补充测试报告 | `reports/README.md` |
```

- [ ] **Step 2: Commit**

```bash
git add docs/agent-testing/README.md
git commit -m "docs(agent-testing): add go-runner-guide route to README"
```

---

## Task 2: Update agent-runner-guide.md

**Files:**
- Modify: `docs/agent-testing/agent-runner-guide.md`

- [ ] **Step 1: Add a reference in the dependency strategy section**

In `docs/agent-testing/agent-runner-guide.md`, find the `### 4. 选择依赖策略` section header. Add the following note immediately after the opening principle block (after the line `> Mock 用来隔离不可控依赖，不是用来逃避真实业务风险。`):

```markdown
> 执行接口契约测试、模块集成测试或状态一致性测试时，优先使用 Go runner 采集结构化证据，而不是手工构造散点 curl 命令。参见 `go-runner-guide.md`。
```

- [ ] **Step 2: Commit**

```bash
git add docs/agent-testing/agent-runner-guide.md
git commit -m "docs(agent-testing): reference go-runner-guide in dependency strategy"
```

---

## Task 3: Create go-runner-guide.md

**Files:**
- Create: `docs/agent-testing/go-runner-guide.md`

- [ ] **Step 1: Write the file**

Create `docs/agent-testing/go-runner-guide.md` with the following content:

```markdown
# Go Runner 使用指南

本指南说明 agent 何时选择 Go runner 采集测试证据、如何从模板生成可运行的 runner、以及输出格式的强制要求。

## 何时使用 Go runner

使用 Go runner：

- 接口契约测试：需要验证 HTTP 请求/响应结构、状态码和字段内容。
- 模块集成测试：需要同时核查 HTTP 响应、MySQL 记录和 Redis 状态。
- 状态一致性测试：需要对比多个数据源（接口返回、数据库状态、缓存状态）。
- 并发测试：需要结构化记录多 goroutine 并发请求的每个结果。

不使用 Go runner：

- 本地单元测试：直接使用 `go test`。
- 纯探索性手工调试：无需证据采集的临时验证。

## 准备步骤

### 第一步：创建工作目录并复制模板

```bash
mkdir -p /tmp/agent-runner-<batch>
```

读取 `docs/agent-testing/runner-template.go`，将内容完整复制到 `/tmp/agent-runner-<batch>/main.go`。

### 第二步：创建 go.mod

在 `/tmp/agent-runner-<batch>/go.mod` 写入：

```
module agent-runner

go 1.23

require (
    github.com/go-sql-driver/mysql v1.8.1
    github.com/redis/go-redis/v9 v9.17.2
)
```

然后拉取依赖：

```bash
cd /tmp/agent-runner-<batch> && go mod tidy
```

### 第三步：填写 CONFIG

修改 `main.go` 顶部 CONFIG 区的三个常量：

- `batchID`：格式 `agent_<module>_<YYYYMMDDHHMMSS>`，例如 `agent_item_20260524120000`
- `baseURL`：本地服务地址，通常 `http://127.0.0.1:8080`
- `redisAddr`：Redis 地址，通常 `127.0.0.1:6379`

DSN 通过环境变量传入，不写入文件：

```bash
export TEST_DSN="<user>:<pass>@tcp(127.0.0.1:3306)/<dbname>?parseTime=true"
```

### 第四步：填写 SCENARIOS 和 CLEANUP，运行

按目标模块契约文档定义的测试场景，在 `scenarios()` 函数返回的切片中添加场景函数。
在 `cleanup()` 函数中添加清理操作（只清理带 batchID 前缀的数据）。

```bash
cd /tmp/agent-runner-<batch> && go run main.go
```

## 场景编写规范

- 每个场景是一个 `func() Result` 函数，追加到 `scenarios()` 返回的切片。
- 场景之间需要传递中间状态时（如第一步创建的 item_id 供后续步骤使用），使用 package 级别变量。
- 并发场景使用 `runConcurrent(n, fn)` helper，它返回 `[]Result`；agent 将每个结果单独计入 pass/fail 统计。
- 函数名建议以 `case` 开头，例如 `caseCreateItem`、`casePublishItem`。

示例：

```go
var createdItemID string

func caseCreateItem() Result {
    status, body := httpDo("POST", "/api/v1/items", map[string]any{
        "title":       batchID + "_jade",
        "room_id":     "room_test_001",
        "description": "agent test item",
        "rule": map[string]any{
            "start_price":   100,
            "bid_increment": 10,
            "start_time":    "2026-06-01T10:00:00Z",
            "end_time":      "2026-06-01T11:00:00Z",
        },
    }, merchantToken)
    itemID := mustStr(body, "data", "item_id")
    createdItemID = itemID
    rows := dbRows("SELECT id, status FROM auction_items WHERE id = ? AND deleted_at IS NULL", itemID)
    pass := status == 200 && itemID != "" && len(rows) == 1 && rows[0]["status"] == "draft"
    return Result{
        Name:     "create item",
        Pass:     pass,
        Reason:   fmt.Sprintf("status=%d item_id=%s db_status=%s", status, itemID, safeGet(rows, 0, "status")),
        Request:  "POST /api/v1/items {title:" + batchID + "_jade}",
        Response: fmt.Sprintf("%d item_id=%s", status, itemID),
        DB:       fmt.Sprintf("SELECT id,status WHERE id=%s -> %v", itemID, rows),
        Redis:    "N/A",
    }
}
```

## 输出格式要求

Runner 输出格式是强制契约，agent 不得修改 `printResult` 和 `main` 中的打印逻辑。

每条场景输出：

```
=== CASE: <name>
  REQUEST:  <method> <path> <body summary>
  RESPONSE: <status> <key fields>
  DB:       <query and result summary>
  REDIS:    <key + value summary>（无 Redis 验证时填 N/A）
  RESULT:   PASS / FAIL — <reason>
```

结束输出：

```
=== SUMMARY
  PASS: n  FAIL: m
  BATCH_ID: <batchID>

=== CLEANUP
  <each cleanup action and result>
```

## 清理要求

- `cleanup()` 通过 `defer` 在 `main` 开头注册，场景 panic 时也会执行。
- 只清理带 batchID 前缀的数据或本次 runner 创建的可识别 ID（item_id、room_id 等）。
- 不得清空数据库或批量修改非测试数据。
- cleanup 的每一步操作和结果必须打印，作为报告的清理记录证据。

## 与测试报告的衔接

| Runner 输出 | 报告字段 |
| --- | --- |
| 每条 CASE 块 | 验证证据表格的一行 |
| SUMMARY 行 | 通过项 / 失败项 |
| CLEANUP 块 | 测试数据清理结果 |

Runner 的 stdout 可直接粘贴进报告的"验证证据"节，无需二次加工。
```

- [ ] **Step 2: Commit**

```bash
git add docs/agent-testing/go-runner-guide.md
git commit -m "docs(agent-testing): add go-runner-guide.md"
```

---

## Task 4: Create runner-template.go

**Files:**
- Create: `docs/agent-testing/runner-template.go`

- [ ] **Step 1: Write the file**

Create `docs/agent-testing/runner-template.go` with the following content:

```go
// Agent testing Go runner template.
// Copy this file to /tmp/agent-runner-<batch>/main.go, create go.mod (see go-runner-guide.md),
// fill CONFIG and SCENARIOS, then: go run main.go
// This file never enters the repository at runtime.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

// ── CONFIG (agent fills) ─────────────────────────────────────────────────────

const (
	baseURL   = "http://127.0.0.1:8080"
	batchID   = "agent_item_20260524120000" // replace <module> and timestamp
	redisAddr = "127.0.0.1:6379"
)

// DSN is read from TEST_DSN env var — do not write credentials into this file.
// Example: TEST_DSN="user:pass@tcp(127.0.0.1:3306)/live_auction?parseTime=true" go run main.go

// ── TYPES ────────────────────────────────────────────────────────────────────

// Result holds the structured evidence for one test scenario.
type Result struct {
	Name     string
	Pass     bool
	Reason   string
	Request  string // "METHOD /path {body summary}"
	Response string // "status key=val key=val"
	DB       string // "SELECT ... -> [{col:val}]" or "N/A"
	Redis    string // "key -> val" or "N/A"
}

// ── SCENARIOS (agent fills) ──────────────────────────────────────────────────
// Declare package-level vars here to share state between sequential scenarios.
// Example:
//
//	var (
//	    merchantToken string
//	    createdItemID string
//	)

func scenarios() []func() Result {
	return []func() Result{
		// TODO: add scenario functions following the module contract.
		// Example:
		//   caseCreateItem,
		//   casePublishItem,
	}
}

// Example scenario — delete or replace when filling in real cases:
//
// func caseCreateItem() Result {
//     status, body := httpDo("POST", "/api/v1/items", map[string]any{
//         "title":       batchID + "_jade",
//         "room_id":     "room_test_001",
//         "description": "agent test item",
//         "rule": map[string]any{
//             "start_price":   100,
//             "bid_increment": 10,
//             "start_time":    "2026-06-01T10:00:00Z",
//             "end_time":      "2026-06-01T11:00:00Z",
//         },
//     }, merchantToken)
//     itemID := mustStr(body, "data", "item_id")
//     createdItemID = itemID
//     rows := dbRows("SELECT id, status FROM auction_items WHERE id = ? AND deleted_at IS NULL", itemID)
//     pass := status == 200 && itemID != "" && len(rows) == 1 && rows[0]["status"] == "draft"
//     return Result{
//         Name:     "create item",
//         Pass:     pass,
//         Reason:   fmt.Sprintf("status=%d item_id=%s db_status=%s", status, itemID, safeGet(rows, 0, "status")),
//         Request:  "POST /api/v1/items {title:" + batchID + "_jade}",
//         Response: fmt.Sprintf("%d item_id=%s", status, itemID),
//         DB:       fmt.Sprintf("SELECT id,status WHERE id=%s -> %v", itemID, rows),
//         Redis:    "N/A",
//     }
// }

// ── CLEANUP (agent fills) ────────────────────────────────────────────────────
// Only clean up data created in this batch (rows with batchID prefix, known IDs).

func cleanup() {
	fmt.Println("\n=== CLEANUP")
	// TODO: agent adds cleanup actions here. Examples:
	//
	// Soft-delete items created in this batch:
	//   res, err := db.ExecContext(ctx, "UPDATE auction_items SET deleted_at=NOW() WHERE title LIKE ? AND deleted_at IS NULL", batchID+"%")
	//   n, _ := res.RowsAffected()
	//   fmt.Printf("  soft-delete auction_items LIKE %s%%: rows=%d err=%v\n", batchID, n, err)
	//
	// Delete Redis keys created in this batch:
	//   rdb.Del(ctx, "auction:item:"+createdItemID+":state")
	//   fmt.Printf("  del auction:item:%s:state\n", createdItemID)
	fmt.Println("  (no cleanup configured)")
}

// ── HELPERS (do not modify) ──────────────────────────────────────────────────

var (
	httpClient = &http.Client{Timeout: 10 * time.Second}
	db         *sql.DB
	rdb        *redis.Client
	ctx        = context.Background()
)

func init() {
	if dsn := os.Getenv("TEST_DSN"); dsn != "" {
		var err error
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "db open: %v\n", err)
		}
	}
	rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
}

// httpDo sends an HTTP request and returns the status code and parsed JSON body.
// Pass token="" to omit the Authorization header.
func httpDo(method, path string, body any, token string) (int, map[string]any) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL+path, r)
	if err != nil {
		return 0, map[string]any{"error": err.Error()}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, map[string]any{"error": err.Error()}
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// dbRows executes a SELECT and returns rows as []map[string]string.
// Returns nil if TEST_DSN was not set.
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
	var out []map[string]string
	for rows.Next() {
		vals := make([][]byte, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...) //nolint:errcheck
		row := make(map[string]string, len(cols))
		for i, col := range cols {
			row[col] = string(vals[i])
		}
		out = append(out, row)
	}
	return out
}

// redisGet returns the string value of a key, or "" if missing or on error.
func redisGet(key string) string {
	val, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return ""
	}
	return val
}

// redisHGetAll returns all fields of a hash key, or nil on error.
func redisHGetAll(key string) map[string]string {
	val, err := rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil
	}
	return val
}

// redisZMembers returns all members of a sorted set (score ascending), or nil on error.
func redisZMembers(key string) []string {
	vals, err := rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil
	}
	return vals
}

// runConcurrent launches n goroutines each calling fn and collects all results.
func runConcurrent(n int, fn func() Result) []Result {
	out := make([]Result, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			out[idx] = fn()
		}(i)
	}
	wg.Wait()
	return out
}

// mustStr extracts a string from a nested map path.
// mustStr(body, "data", "item_id") reads body["data"]["item_id"].
func mustStr(m map[string]any, keys ...string) string {
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			s, _ := cur[k].(string)
			return s
		}
		next, _ := cur[k].(map[string]any)
		if next == nil {
			return ""
		}
		cur = next
	}
	return ""
}

// safeGet returns rows[idx][key] or "" if idx is out of bounds.
func safeGet(rows []map[string]string, idx int, key string) string {
	if idx >= len(rows) {
		return ""
	}
	return rows[idx][key]
}

// jsonStr serialises v to compact JSON for use in Result fields.
func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// filterLines returns lines from s that contain substr, joined by newline.
func filterLines(s, substr string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// ── MAIN (do not modify) ─────────────────────────────────────────────────────

func main() {
	defer cleanup()

	fns := scenarios()
	if len(fns) == 0 {
		fmt.Println("no scenarios defined — fill the scenarios() function and try again")
		return
	}

	var results []Result
	for _, fn := range fns {
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
	fmt.Printf("\n=== SUMMARY\n  PASS: %d  FAIL: %d\n  BATCH_ID: %s\n", pass, fail, batchID)
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
	fmt.Printf("  RESULT:   %s — %s\n", status, r.Reason)
}
```

- [ ] **Step 2: Commit**

```bash
git add docs/agent-testing/runner-template.go
git commit -m "docs(agent-testing): add runner-template.go"
```

---

## Task 5: Verify the template compiles

**Files:** (none modified — verification only)

- [ ] **Step 1: Create a temp dir, copy template, create go.mod**

```bash
mkdir -p /tmp/agent-runner-verify
cp /Users/echin/echin/go/live-auction-backend/docs/agent-testing/runner-template.go /tmp/agent-runner-verify/main.go
cat > /tmp/agent-runner-verify/go.mod << 'EOF'
module agent-runner

go 1.23

require (
    github.com/go-sql-driver/mysql v1.8.1
    github.com/redis/go-redis/v9 v9.17.2
)
EOF
```

- [ ] **Step 2: Pull dependencies**

```bash
cd /tmp/agent-runner-verify && go mod tidy
```

Expected: exits 0, `go.sum` is created, no errors.

- [ ] **Step 3: Build (no run — avoids needing a real server)**

```bash
cd /tmp/agent-runner-verify && go build ./...
```

Expected: exits 0, binary produced. If there are compile errors, fix `runner-template.go` and re-run.

- [ ] **Step 4: Confirm "no scenarios" message when run**

```bash
cd /tmp/agent-runner-verify && go run main.go
```

Expected output:
```
no scenarios defined — fill the scenarios() function and try again

=== CLEANUP
  (no cleanup configured)
```

- [ ] **Step 5: Clean up temp dir and commit any fixes**

```bash
rm -rf /tmp/agent-runner-verify
```

If `runner-template.go` was fixed in Step 3, amend the previous commit:

```bash
git add docs/agent-testing/runner-template.go
git commit -m "fix(agent-testing): fix runner-template.go compile errors"
```
