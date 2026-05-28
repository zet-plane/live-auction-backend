# 并发测试指南

本指南定义 agent 如何设计、执行和报告并发测试。它只规定并发测试方法，不定义业务规则；具体业务规则、允许状态、错误码和通过标准必须来自目标 `modules/<module>.md` 或 `flows/<flow>.md`。

## 读取顺序

执行并发测试时按以下顺序读取文档：

```text
docs/agent-testing/README.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/concurrency.md
docs/agent-testing/guides/go-runner.md
docs/agent-testing/modules/<module>.md 或 docs/agent-testing/flows/<flow>.md
```

如果需要准备真实依赖、启动服务或创建测试数据，再读取：

```text
docs/agent-testing/guides/environment.md
```

如果测试完成后要写报告，再读取：

```text
docs/agent-testing/reports/README.md
```

## 适用范围

并发测试用于验证真实竞争条件下的状态唯一性、原子性、幂等性和跨存储一致性。

并发测试结果必须由数据支撑。agent 不能只写“通过”“失败”或主观判断，必须提供可复核的数据证据，包括每个并发请求的输入和响应、最终 Redis 状态、最终 MySQL 查询结果、必要的 HTTP 查询结果，以及这些数据之间的对账结论。

适合并发测试：

- 多请求竞争同一个业务状态，例如同一拍品同时出价。
- 多请求竞争同一个幂等 key。
- 多请求竞争状态流转，例如同一房间同时开播或下播。
- 读请求与写请求并发，例如排行榜查询与出价同时发生。
- 终态动作与普通动作竞争，例如一口价成交与普通出价同时发生。

不适合作为并发测试：

- 只验证字段格式、鉴权或普通错误码的接口契约测试。
- 已能用单元测试证明的纯函数逻辑。
- mock 掉 DB、Redis、锁、Lua 脚本或事务后的“并发”测试。

## 前置条件

执行并发测试前必须确认：

- `go test ./...` 无编译错误。
- 目标模块单元测试通过。
- 目标模块文档明确列出并发目标和通过标准。
- 真实 MySQL 或线上等价数据库可用。
- 真实 Redis 或线上等价 Redis 可用，如果目标模块依赖 Redis。
- 服务地址、数据库和 Redis 只用于本批次测试数据。
- 测试数据具备 `batch_id`、名称前缀或已记录的实体 ID。

如果模块文档缺少并发语义，必须先问用户，不能自行补齐。例如：重复开播是严格只有一个请求成功，还是允许幂等成功。

## 依赖策略

并发测试默认使用真实依赖：

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | 真实服务或线上等价测试服务 | 验证真实 handler、中间件和序列化边界 |
| MySQL | 真实测试库或线上等价库，仅操作本批次数据 | 验证真实事务、唯一约束和持久化结果 |
| Redis | 真实测试 Redis 或线上等价 Redis，仅操作本批次 key | 验证 Lua、原子更新、缓存状态和排名 |
| WebSocket | 需要验证实时消息时使用真实连接 | 验证真实推送链路 |
| 第三方服务 | mock 或跳过 | 不属于竞拍核心并发状态 |

禁止：

- 为了让测试容易通过而 mock Redis、DB、锁、Lua 脚本或被测状态写入。
- 清空数据库、`TRUNCATE`、`FLUSHDB`、`FLUSHALL`。
- 使用没有批次前缀的数据做破坏性操作。
- 在报告中写入数据库地址、密码、Redis 密码或可复用 token。

## 设计并发场景

每个并发场景必须包含 6 个部分：

```text
场景名称：
竞争对象：
并发请求：
预期成功：
预期失败：
最终不变量：
```

示例：

```text
场景名称：多用户不同价格同时出价
竞争对象：auction:item:{item_id}:state.current_price
并发请求：10 个用户同时 POST /api/v1/items/{item_id}/bids
预期成功：合法递增价格可成功；最高价格最终获胜
预期失败：低于并发后当前价的请求返回可解释业务错误
最终不变量：Redis state、Redis ranking、MySQL bid_logs、HTTP ranking 第一名一致
```

## 推荐场景矩阵

### 出价模块

| 优先级 | 场景 | 核心验证 |
| --- | --- | --- |
| P0 | 多用户不同价格同时出价 | 最高合法价格获胜；Redis state、ranking、MySQL BidLog 一致 |
| P0 | 多用户相同价格同时出价 | 至多一个请求改变当前价；失败请求错误可解释 |
| P0 | 同一用户同一幂等 key 重复提交 | 只产生一个 bid_id、一条 BidLog，bid_count 只增加一次 |
| P0 | 一口价和普通出价同时发生 | 成交结果唯一，成交后后续出价无效 |
| P1 | 同一用户不同幂等 key 快速递增出价 | 价格单调上升，ranking 保留该用户最高价 |
| P1 | 排行榜查询与并发出价同时发生 | 排行榜可以是某一时刻快照，但不能乱序或重复 rank |

### 房间模块

| 优先级 | 场景 | 核心验证 |
| --- | --- | --- |
| P0 | 同一商家并发激活房间 | 商家最终只有 1 条房间记录 |
| P0 | 同一房间并发开播 | 最终状态为 live，DB、Redis、查询接口一致 |
| P0 | 同一房间并发下播 | 最终状态为 idle，DB、Redis、查询接口一致 |
| P1 | 不同商家并发操作各自房间 | 互不影响，状态和归属隔离成立 |

## 执行步骤

### 1. 准备批次和数据

每次并发测试创建独立批次：

```text
batch_id = agent_<target>_concurrency_<YYYYMMDDHHMMSS>
```

准备数据必须满足：

- 用户、商家、房间、拍品等实体可追踪到当前 `batch_id`。
- 拍品或房间进入并发场景需要的初始状态。
- Redis key 已按业务路径初始化，或通过真实业务接口触发初始化。
- 所有认证 token 只在 runner 内使用，不写入报告。

### 2. 记录并发前状态

发起并发请求前必须记录基线：

- 目标 HTTP 查询响应。
- MySQL 目标行和关联行。
- Redis 目标 key，例如 state、ranking、idempotency key。
- 业务配置，例如加价幅度、一口价、自动延时策略。

### 3. 同步启动请求

Go runner 中应使用同步起跑，避免 goroutine 创建顺序被误认为并发窗口。

推荐结构：

```go
type ConcurrentAttempt struct {
    Index    int
    UserID   string
    Price    int64
    Key      string
    StartAt  time.Time
    EndAt    time.Time
    Status   int
    Code     string
    BidID    string
    Body     string
}

func runConcurrentAttempts(attempts []ConcurrentAttempt, fn func(ConcurrentAttempt) ConcurrentAttempt) []ConcurrentAttempt {
    start := make(chan struct{})
    var wg sync.WaitGroup
    out := make([]ConcurrentAttempt, len(attempts))
    wg.Add(len(attempts))
    for i, attempt := range attempts {
        go func(i int, attempt ConcurrentAttempt) {
            defer wg.Done()
            <-start
            attempt.StartAt = time.Now()
            out[i] = fn(attempt)
            out[i].EndAt = time.Now()
        }(i, attempt)
    }
    close(start)
    wg.Wait()
    return out
}
```

如果使用 `guides/go-runner.md` 的 `runConcurrent` helper，也必须在场景证据中记录每个请求的开始/结束时间，证明请求实际重叠。

### 4. 收集每个请求结果

每个并发请求必须记录：

- 请求编号。
- 用户或商家 ID。
- 请求路径。
- 请求体关键字段，例如 `price`、`idempotency_key`。
- 开始时间、结束时间、耗时。
- HTTP 状态。
- 业务错误码或成功 ID，例如 `bid_id`。
- 响应中的关键状态，例如 `current_price`、`leader_user_id`、`status`。

不要只记录汇总结果；每个请求都必须有独立证据。

### 5. 验证最终状态

并发完成后必须查询最终状态，不能只根据 HTTP 响应推断。

出价模块至少验证：

- Redis `auction:item:{item_id}:state`。
- Redis `auction:item:{item_id}:ranking`。
- Redis `auction:item:{item_id}:bidder_names`，如果本场景涉及昵称。
- Redis `auction:item:{item_id}:idempotency:{key}`，如果本场景涉及幂等。
- MySQL `bid_logs`。
- MySQL `auction_items`，如果触发一口价或结束状态。
- `GET /api/v1/items/{item_id}/ranking` 响应。

房间模块至少验证：

- MySQL `live_rooms`。
- Redis `auction:room:{room_id}:state`。
- `GET /api/v1/merchant/room` 或 `GET /api/v1/rooms/{room_id}` 响应。

### 6. 对账和判定

并发测试必须把“请求结果”和“最终状态”对账。

通用判定：

- 成功数、失败数和总请求数对得上。
- 失败请求有明确业务错误码或可解释系统错误。
- 最终状态只对应一个合法结果。
- DB、Redis、HTTP 查询结果一致，或差异已按模块文档允许的降级语义解释。
- 没有重复成功、重复 rank、重复成交、价格回退、状态回退或脏写。

出价模块额外判定：

- `current_price` 等于最终有效最高价。
- `leader_user_id` 等于最终有效最高价用户。
- ranking 第一名等于 `leader_user_id`。
- `bid_count` 等于非幂等成功出价次数。
- MySQL `bid_logs` 数量等于非幂等成功出价次数。
- 同一幂等 key 只映射一个 `bid_id`。

房间模块额外判定：

- 同一商家并发激活后只有 1 条有效房间。
- 状态动作结束后 DB 状态、Redis 状态和查询接口一致。
- 不同商家的并发操作不会改写对方房间。

## 并发隔离证明

每个并发测试报告必须包含：

```text
并发隔离证明：
- 并发请求总数：
- 请求开始时间范围：
- 请求结束时间范围：
- 最大请求耗时：
- 实际重叠窗口：
- 验证最终状态使用的 SQL：
- 验证最终状态使用的 Redis 命令：
- 并发冲突次数或失败请求数量：
- 最终状态唯一性证据：
```

实际重叠窗口可按以下方式描述：

```text
first_start = 所有请求最早开始时间
last_start = 所有请求最晚开始时间
first_end = 所有请求最早结束时间
overlap = first_end - last_start
```

如果 `overlap <= 0`，说明请求没有形成有效重叠。本次结果只能作为快速连续请求证据，不能作为有效并发证据。

## Runner 输出要求

并发场景的 runner 输出必须包含两层结果：

- 每个请求一条 `CASE`，记录独立请求证据。
- 每个场景一条 summary `CASE`，记录最终状态对账结果。

每条结论都必须能追溯到数据。请求级 `CASE` 必须包含请求关键字段和响应关键字段；summary `CASE` 必须包含 Redis、MySQL 或 HTTP 查询摘要。没有数据支撑的结论只能记为“未验证”，不能记为通过。

单请求 `CASE` 示例：

```text
=== CASE: concurrent_bid_03
  REQUEST:  POST /api/v1/items/item_x/bids {user:user_3 price:150 key:agent_x_03}
  RESPONSE: 200 bid_id=bid_x current_price=150 leader_user_id=user_3 start=12:00:00.101 end=12:00:00.130
  DB:       checked in scenario summary
  REDIS:    checked in scenario summary
  RESULT:   PASS - request result recorded
```

场景 summary `CASE` 示例：

```text
=== CASE: concurrent_bid_summary
  REQUEST:  10 concurrent POST /api/v1/items/{item_id}/bids
  RESPONSE: success=4 fail=6 highest_success_price=190
  DB:       bid_logs count=4 max_price=190 users=[...]
  REDIS:    state.current_price=190 leader_user_id=user_9 ranking_top=user_9:190
  RESULT:   PASS - final state is unique and consistent
```

## 报告要求

并发测试报告除 `reports/README.md` 的通用字段外，还必须包含：

```text
并发设计：
请求矩阵：
并发隔离证明：
最终状态对账：
```

报告中的“通过项”和“失败项”必须引用请求矩阵、最终状态对账或 runner 输出中的具体数据。只描述现象、没有数据来源的条目不能作为测试结论。

`请求矩阵` 建议使用表格：

| 编号 | 用户/商家 | 请求关键字段 | 开始时间 | 结束时间 | HTTP | 业务码 / ID | 结果解释 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | user_x | price=110 key=k0 |  |  |  |  |  |

`最终状态对账` 建议使用表格：

| 数据源 | 查询 | 关键结果 | 是否一致 |
| --- | --- | --- | --- |
| HTTP | GET ranking | top=user_x price=190 | 是 |
| Redis | HGETALL state | leader=user_x current_price=190 | 是 |
| Redis | ZREVRANGE ranking WITHSCORES | user_x=190 | 是 |
| MySQL | SELECT max(price), count(*) FROM bid_logs | max=190 count=4 | 是 |

## 失败分析

如果并发测试失败，必须按失败类型归类：

| 失败类型 | 判定方式 | 常见原因 |
| --- | --- | --- |
| 价格回退 | 最终价格低于已成功响应中的更高价格 | Redis 更新非原子、状态覆盖 |
| 重复成功 | 相同幂等 key 产生多个 bid_id 或多条 BidLog | 幂等 key 未原子写入 |
| 排名不一致 | state leader 与 ranking 第一名不同 | ranking 更新和 state 更新不在同一原子路径 |
| 日志不一致 | Redis 成功但 MySQL BidLog 缺失或重复 | 持久化失败、重试语义不清 |
| 成交冲突 | 多个请求都生成成交结果 | 结束状态缺少原子保护 |
| 状态回退 | ended 又被改回 ongoing，live 又被改回 idle 外的状态 | 状态流转缺少条件更新 |

失败报告必须包含：

```text
失败场景：
请求矩阵：
并发隔离证明：
期望最终状态：
实际最终状态：
不一致数据源：
复现步骤：
可能原因：
建议修复点：
建议新增的回归测试：
```

## 沉淀为回归测试

并发测试稳定后，优先沉淀以下回归：

- P0 场景的 Go runner 脚本或可复用用例。
- 幂等、状态唯一性和最终一致性的服务层单元测试。
- Redis Lua 原子出价的集成测试。
- DB 唯一约束或条件更新的 DAO 集成测试。
- CI 中可运行的轻量并发 smoke test。

并发测试不能只沉淀一次性聊天结论；必须在报告中记录 runner 输出、最终对账和清理结果，便于之后复盘和自动化。
