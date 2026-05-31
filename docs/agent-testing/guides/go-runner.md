# Go Runner 使用指南

本指南说明 agent 何时选择 Go runner 采集测试证据、如何从模板生成可运行的 runner、以及输出格式的强制要求。

通用计划字段、依赖授权、数据隔离、证据、报告、清理、敏感信息和失败输出规则见 `docs/agent-testing/templates/protocol.md`。本文只记录 Go runner 输出格式和 helper 使用的附加规则。

## 何时使用 Go runner

使用 Go runner：

- 接口契约测试：需要验证 HTTP 请求/响应结构、状态码和字段内容。
- 模块集成测试：需要同时核查 HTTP 响应、MySQL 记录和 Redis 状态。
- 状态一致性测试：需要对比多个数据源（接口返回、数据库状态、缓存状态）。
- 并发一致性测试：需要结构化记录多 goroutine 并发请求的每个结果。

不使用 Go runner：

- 本地单元测试：直接使用 `go test`。
- 纯探索性手工调试：无需证据采集的临时验证。

## 准备步骤

### 第一步：创建工作目录并复制模板

```bash
rtk mkdir -p /tmp/agent-runner-<batch>
```

读取 `docs/agent-testing/templates/runner.go`，将内容完整复制到 `/tmp/agent-runner-<batch>/main.go`。

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

然后在 `/tmp/agent-runner-<batch>` 中拉取依赖：

```bash
rtk go mod tidy
```

如果本机默认 Go build cache 目录受沙箱限制，应显式指定临时 cache：

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache go mod tidy
```

### 第三步：填写 CONFIG

修改 `main.go` 顶部 CONFIG 区的三个常量：

- `batchID`：格式 `agent_<module>_<YYYYMMDDHHMMSS>`，例如 `agent_item_20260524120000`
- `baseURL`：本地服务地址，通常 `http://127.0.0.1:8080`
- `redisAddr`：Redis 地址，通常 `127.0.0.1:6379`

DSN 通过运行时环境变量传入，不写入文件：

```bash
rtk env 'TEST_DSN=<redacted>' go run main.go
```

命令行直接传入 `TEST_DSN` 时，必须避免 shell 展开 `?` 和 `&`。推荐使用引号包住整个环境变量值，并且不要把完整 DSN 写入报告：

```bash
rtk env GOCACHE=/tmp/live-auction-go-cache 'TEST_DSN=<redacted>' go run main.go
```

### 第四步：填写 SCENARIOS 和 CLEANUP，运行

按目标模块契约文档定义的测试场景，在 `scenarios()` 函数返回的切片中添加场景函数。
在 `cleanup()` 函数中添加清理操作（只清理带 batchID 前缀的数据）。

```bash
rtk go run main.go
```

## 场景编写规范

- 每个场景是一个 `func() Result` 函数，追加到 `scenarios()` 返回的切片。
- 场景之间需要传递中间状态时（如第一步创建的 item_id 供后续步骤使用），使用 package 级别变量。
- 普通并发场景可使用 `runConcurrent(n, fn)` helper。需要证明真实并发窗口时，按 `guides/concurrency.md` 在场景内编写带 start gate 和请求时间记录的 helper。wrapper 函数内部调用 `printResult` 打印每个并发结果，最后返回一个汇总 `Result` 供 `main()` 计入总统计：
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

```go
func caseConcurrentBid() Result {
    results := runConcurrent(5, func() Result {
        status, body := httpDo("POST", "/api/v1/bids", map[string]any{
            "item_id": createdItemID,
            "amount":  110,
        }, userToken)
        pass := status == 200 || status == 409
        return Result{
            Name:     "concurrent bid attempt",
            Pass:     pass,
            Reason:   fmt.Sprintf("status=%d", status),
            Request:  "POST /api/v1/bids {item_id, amount:110}",
            Response: fmt.Sprintf("%d %s", status, jsonStr(body)),
            DB:       "N/A",
            Redis:    "N/A",
        }
    })
    passCount := 0
    for _, r := range results {
        printResult(r) // prints individual === CASE block for each goroutine
        if r.Pass {
            passCount++
        }
    }
    return Result{
        Name:   "concurrent bid summary",
        Pass:   passCount == len(results),
        Reason: fmt.Sprintf("%d/%d goroutines passed", passCount, len(results)),
        DB:     "N/A",
        Redis:  "N/A",
    }
}
```

## Helper 函数参考

模板提供以下 helper，场景函数可直接调用（详细实现见 `templates/runner.go`）：

| 函数 | 签名 | 说明 |
| --- | --- | --- |
| `httpDo` | `(method, path string, body any, token string) (int, map[string]any)` | 发 HTTP 请求；`token=""` 时不带 Authorization 头 |
| `dbRows` | `(query string, args ...any) []map[string]string` | 执行 SELECT；`TEST_DSN` 未设置时返回 nil |
| `redisGet` | `(key string) string` | GET key；key 不存在或出错时返回 `""` |
| `redisHGetAll` | `(key string) map[string]string` | HGETALL key；出错时返回 nil |
| `redisZMembers` | `(key string) []string` | ZRANGE key 0 -1；出错时返回 nil |
| `runConcurrent` | `(n int, fn func() Result) []Result` | 并发执行 fn n 次，返回全部结果；严格并发证据场景按 `guides/concurrency.md` 记录请求时间窗口 |
| `mustStr` | `(m map[string]any, keys ...string) string` | 嵌套 map 取值；路径缺失时返回 `""` |
| `safeGet` | `(rows []map[string]string, idx int, key string) string` | 安全取行；下标越界时返回 `""` |
| `jsonStr` | `(v any) string` | 将值序列化为紧凑 JSON，用于 Result 字段打印 |
| `filterLines` | `(s, substr string) string` | 从字符串中提取含 substr 的行；适用于解析服务日志输出 |

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

并发场景中，`runConcurrent` 返回的每个 `Result` 单独打印为一条 CASE 块：

```
=== CASE: concurrent_bid_0
  REQUEST:  POST /api/v1/bids {...}
  RESPONSE: 200 bid_id=bid_xxx
  DB:       ...
  REDIS:    ...
  RESULT:   PASS — ...

=== CASE: concurrent_bid_1
  ...
  RESULT:   FAIL — ...
```

每个并发结果独立计入 SUMMARY 的 PASS/FAIL 统计。

## 清理要求

通用清理和敏感信息边界见 `templates/protocol.md`。Runner 还必须满足：

- `cleanup()` 通过 `defer` 在 `main` 开头注册，场景 panic 时也会执行。
- 只清理带 batchID 前缀的数据或本次 runner 创建的可识别 ID（item_id、room_id 等）。
- 不得清空数据库或批量修改非测试数据。
- cleanup 的每一步操作和结果必须打印，作为报告的清理记录证据。
- 清理顺序应遵守业务关联：先清理或软删除本批次主实体，再清理本批次可识别的子记录和 Redis key。不得为了清理方便删除无批次前缀的数据。
- 对使用软删除的表，优先执行带主键或批次前缀条件的软删除；若需要删除子表记录，必须限定在本次 runner 创建的 ID 集合内。
- Redis 清理只能删除本批次 key，或从共享 ZSET/SET 中移除本批次 member；禁止 `FLUSHDB`、`FLUSHALL`。

## 与测试报告的衔接

| Runner 输出 | 报告字段 |
| --- | --- |
| 每条 CASE 块 | 验证证据表格的一行 |
| SUMMARY 行 | 通过项 / 失败项 |
| CLEANUP 块 | 测试数据清理结果 |

Runner 的 stdout 可直接粘贴进报告的"验证证据"节，无需二次加工。
