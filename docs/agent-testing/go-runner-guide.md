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
