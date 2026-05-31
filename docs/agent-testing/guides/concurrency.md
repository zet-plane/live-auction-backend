# 并发一致性测试指南

本指南定义 agent 如何设计、执行和报告并发一致性测试。它关注真实竞争条件下的状态唯一性、原子性、幂等性和跨存储一致性，不用于评估系统容量、吞吐、延迟或资源瓶颈。

通用计划字段、依赖授权、数据隔离、证据、报告、清理、敏感信息和失败输出规则见 `docs/agent-testing/templates/protocol.md`。本文只记录并发一致性测试的附加规则。

性能压测、容量评估、阶梯加压、稳定性压测和资源观测见 `docs/agent-testing/guides/performance/README.md`。

本指南只规定并发一致性测试方法，不定义业务规则；具体业务规则、允许状态、错误码和通过标准必须来自目标 `modules/<module>.md` 或 `flows/<flow>.md`。

## 计划先行

并发一致性测试必须遵守 `templates/protocol.md` 的语义确认门和计划执行门。agent 必须先确认业务语义，再生成计划，经过 review 后再执行；review 未通过或未明确批准前，不得连接真实数据库或 Redis、不得创建测试数据、不得启动 runner、不得发起并发请求。

并发一致性测试计划必须落到：

```text
docs/agent-testing/concurrency/
```

计划文件命名格式：

```text
YYYYMMDD-HHMMSS-<target>-<scenario>-plan.md
```

示例：

```text
20260531-143000-<target>-<scenario>-plan.md
```

计划文件只描述将要执行的并发一致性测试，不记录最终测试结果。测试完成后的报告仍写入 `docs/agent-testing/reports/`。

计划文件必须注明本次测试涉及的模块：

```text
涉及模块：
- 目标模块：
- 关联模块：
- 关联 flow：
```

涉及模块是本次并发一致性测试会读写、验证或依赖的模块范围。测试单个 module 时，目标模块写该 module，关联模块写被用作前置数据、跨模块状态或最终对账的数据来源。测试 flow 时，关联 flow 写目标流程，目标模块和关联模块写该流程中本次实际覆盖的模块。

计划文件还必须记录计划来源、review 结果、批准方式和执行结果。环境阻塞、用户拒绝或计划废弃时，只记录未执行原因，不得把计划写成已执行。

并发计划同样适用 `templates/protocol.md` 的语义确认门；发现未确认业务语义时，必须先提问澄清，再写入计划。

## 读取顺序

执行并发一致性测试时按以下顺序读取文档：

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
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

并发一致性测试用于验证真实竞争条件下的状态唯一性、原子性、幂等性和跨存储一致性。

并发一致性测试结果必须由数据支撑。agent 不能只写“通过”“失败”或主观判断，必须提供可复核的数据证据，包括每个并发请求的输入和响应、最终 Redis 状态、最终 MySQL 查询结果、必要的 HTTP 查询结果，以及这些数据之间的对账结论。

并发请求不是测试终点，只是制造竞争窗口的手段。测试终点是：逐项核对并发请求结果是否符合计划中的预期成功、预期失败和最终不变量，并证明最终数据状态在所有要求的数据源之间一致。没有完成预期核对和最终状态对账的并发测试，只能记为“已发起请求，未完成验证”，不能记为通过。

适合并发一致性测试的场景类型：

- 多请求竞争同一个业务状态。
- 多请求竞争同一个幂等 key。
- 多请求竞争同一个状态流转。
- 读请求与写请求并发。
- 终态动作与普通动作竞争。
- 多个存储或查询视图需要最终对账。

不适合作为并发一致性测试：

- 只验证字段格式、鉴权或普通错误码的接口契约测试。
- 已能用单元测试证明的纯函数逻辑。
- mock 掉 DB、Redis、锁、Lua 脚本或事务后的“并发”测试。

## 前置条件

执行并发一致性测试前必须确认：

- `go test ./...` 无编译错误。
- 目标模块单元测试通过。
- 目标模块文档明确列出并发目标和通过标准。
- 真实 MySQL 或线上等价数据库可用。
- 真实 Redis 或线上等价 Redis 可用，如果目标模块依赖 Redis。
- 服务地址、数据库和 Redis 只用于本批次测试数据。
- 测试数据具备 `batch_id`、名称前缀或已记录的实体 ID。

如果模块文档缺少并发语义，必须先问用户，不能自行补齐。例如：同一个状态动作被重复提交时，是严格只有一个请求成功，还是允许多个请求返回同一个幂等成功结果。

## 依赖策略

并发一致性测试默认使用真实依赖：

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | 真实服务或线上等价测试服务 | 验证真实 handler、中间件和序列化边界 |
| MySQL | 真实测试库或线上等价库，仅操作本批次数据 | 验证真实事务、唯一约束和持久化结果 |
| Redis | 真实测试 Redis 或线上等价 Redis，仅操作本批次 key | 验证 Lua、原子更新、缓存状态和派生视图 |
| WebSocket | 需要验证实时消息时使用真实连接 | 验证真实推送链路 |
| 第三方服务 | mock 或跳过 | 不属于被测核心并发状态 |

禁止：

- 为了让测试容易通过而 mock Redis、DB、锁、Lua 脚本或被测状态写入。
- 违反 `templates/protocol.md` 的清理、数据隔离和敏感信息规则。

## 设计并发场景

agent 必须先确认并发场景语义，再将并发场景设计写入 `docs/agent-testing/concurrency/` 下的计划文件，并按 `templates/protocol.md` 的计划执行门完成 review。review 未通过或未明确批准前，agent 不得连接真实数据库或 Redis、不得创建测试数据、不得启动 runner、不得发起并发请求。

每个并发场景必须包含 6 个部分：

```text
场景名称：
竞争对象：
并发请求：
预期成功：
预期失败：
最终不变量：
```

执行任何并发一致性测试前，agent 必须先把上述 6 个部分作为“并发场景设计”完整写入计划文件，并在对话中输出计划路径和摘要，等待用户明确确认后才能继续执行。用户未确认前，agent 不得连接真实数据库或 Redis、不得创建测试数据、不得启动 runner、不得发起并发请求。

如果上述 6 个部分中的任何一项只能写成“待确认”“可能”“需要用户决定”，则不得写入正式计划文件；agent 必须先提问澄清。确认后的计划应直接写明唯一采用的语义。

如果用户修改场景设计，agent 必须先更新设计并再次等待确认。只有当用户确认后的设计仍然落在目标模块或流程契约边界内时，agent 才能继续；如果设计超出文档边界，必须先按 `guides/runner.md` 的规则征得扩展范围确认。

计划一旦批准，执行范围就被锁定。执行过程中发现的新风险只能记录为“建议新增并发计划”或“跳过项”，不能直接扩大本次测试范围。

以下是格式示例，不是业务测试契约。真实场景必须从目标 `modules/<module>.md` 或 `flows/<flow>.md` 生成。

```text
涉及模块：<target-module>, <related-module-or-flow>
场景名称：多个 actor 同时更新同一资源状态
竞争对象：<resource_state_key> / <resource_db_row>
并发请求：N 个 actor 同时调用 <method> <path>
预期成功：符合目标契约的请求可以成功，成功数量和状态变更规则来自模块或流程文档
预期失败：冲突请求返回目标契约允许的业务错误或幂等结果
最终不变量：HTTP 查询、Redis 状态、MySQL 记录和派生视图之间满足目标契约定义的一致性
```

## 从契约生成场景

本指南不预置具体模块的测试场景。agent 必须从目标模块或流程契约中提取业务规则，再生成计划。

生成场景时按以下顺序提取信息：

- 竞争对象：同一业务实体、同一状态字段、同一幂等 key、同一唯一约束或同一派生视图。
- 参与 actor：目标契约定义的用户角色、系统任务或其他调用方。
- 并发动作：同类写请求、不同写请求、读写混合、终态动作和普通动作混合。
- 允许结果：成功数量、失败错误码、幂等返回、允许的中间状态。
- 最终不变量：DB、Redis、HTTP 查询、WebSocket 消息或其他证据之间必须满足的关系。
- 禁止结果：重复成功、状态回退、脏写、重复派生记录、不可解释失败、跨租户或跨主体污染。

计划中的场景矩阵建议使用：

| 优先级 | 场景类型 | 契约来源 | 核心不变量 |
| --- | --- | --- |
| P0 | 同类写请求竞争同一状态 | `<module-or-flow>.md` 中的状态规则 | 最终状态唯一且所有数据源一致 |
| P0 | 同一幂等 key 重复提交 | `<module-or-flow>.md` 中的幂等规则 | 只产生契约允许的一次副作用或同一结果 |
| P1 | 读写混合 | `<module-or-flow>.md` 中的查询一致性规则 | 读结果可解释，最终对账一致 |

如果目标契约没有定义并发优先级、允许结果或最终不变量，agent 必须停止并询问用户，不能把本指南中的示例当成业务规则。

## 执行步骤

### 1. 准备批次和数据

每次并发一致性测试创建独立批次：

```text
batch_id = agent_<target>_concurrency_<YYYYMMDDHHMMSS>
```

准备数据必须满足：

- 所有测试主体和业务实体可追踪到当前 `batch_id`。
- 目标资源进入并发场景需要的初始状态。
- Redis key 已按业务路径初始化，或通过真实业务接口触发初始化。
- 所有认证 token 只在 runner 内使用，不写入报告。

### 2. 记录并发前状态

发起并发请求前必须记录基线：

- 目标 HTTP 查询响应。
- MySQL 目标行和关联行。
- Redis 目标 key，例如 state、ranking、idempotency key。
- 会影响判定的业务配置，例如阈值、状态策略、幂等策略、时间窗口或自动流转规则。

### 3. 同步启动请求

Go runner 中应使用同步起跑，避免 goroutine 创建顺序被误认为并发窗口。

推荐结构：

```go
type ConcurrentAttempt struct {
    Index          int
    ActorID        string
    PayloadSummary string
    IdempotencyKey string
    StartAt        time.Time
    EndAt          time.Time
    HTTPStatus     int
    BusinessCode   string
    ResultID       string
    Body           string
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
- actor ID 或测试主体 ID。
- 请求路径。
- 请求体关键字段，例如目标资源、状态动作、幂等 key 或其他会影响判定的字段。
- 开始时间、结束时间、耗时。
- HTTP 状态。
- 业务错误码或成功结果 ID。
- 响应中的关键状态字段。

不要只记录汇总结果；每个请求都必须有独立证据。

### 5. 验证最终状态

并发完成后必须查询最终状态，不能只根据 HTTP 响应推断。

每个场景至少验证计划中列出的所有最终不变量。常见数据源包括：

- HTTP 查询接口返回的最终状态或派生视图。
- MySQL 目标行、关联行、唯一约束结果或聚合结果。
- Redis 状态 key、集合、排序结构、幂等 key 或锁相关 key。
- WebSocket 消息，如果目标契约要求实时推送一致性。
- 其他目标契约明确要求的证据源。

如果某个数据源在目标契约中是核心状态，但计划没有验证它，agent 必须把计划退回补充，不能执行。

### 6. 对账和判定

并发一致性测试必须把“请求结果”和“最终状态”对账。

对账是并发一致性测试的完成条件。agent 必须先对照已批准计划中的预期成功、预期失败和最终不变量，再判断是否通过；不能因为并发请求全部返回、HTTP 状态为 2xx、runner 退出码为 0，就直接判定测试通过。

通用判定：

- 成功数、失败数和总请求数对得上。
- 失败请求有明确业务错误码或可解释系统错误。
- 最终状态只对应一个合法结果。
- DB、Redis、HTTP 查询结果一致，或差异已按模块文档允许的降级语义解释。
- 没有重复成功、重复派生记录、状态回退、脏写或跨主体污染。
- 幂等场景只产生契约允许的一次副作用或同一个结果。
- 终态场景不会被后续普通动作覆盖或回退。

## 并发隔离证明

每个并发一致性测试报告必须包含：

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

单请求 `CASE` 格式示例：

```text
=== CASE: concurrent_<scenario>_03
  REQUEST:  <METHOD> <PATH> {actor:actor_3 resource:resource_x key:agent_x_03 action:<action>}
  RESPONSE: 200 result_id=result_x state=<state> start=12:00:00.101 end=12:00:00.130
  DB:       checked in scenario summary
  REDIS:    checked in scenario summary
  RESULT:   PASS - request result recorded
```

场景 summary `CASE` 格式示例：

```text
=== CASE: concurrent_<scenario>_summary
  REQUEST:  10 concurrent <METHOD> <PATH>
  RESPONSE: success=4 fail=6 result_ids=[...]
  DB:       target_rows=<summary> aggregate=<summary>
  REDIS:    state=<summary> derived_view=<summary>
  RESULT:   PASS - final state satisfies the planned invariants
```

## 报告要求

并发一致性测试报告除 `reports/README.md` 的通用字段外，还必须包含：

```text
并发设计：
请求矩阵：
并发隔离证明：
最终状态对账：
```

报告中的“通过项”和“失败项”必须引用请求矩阵、最终状态对账或 runner 输出中的具体数据。只描述现象、没有数据来源的条目不能作为测试结论。

`请求矩阵` 建议使用表格：

| 编号 | actor | 请求关键字段 | 开始时间 | 结束时间 | HTTP | 业务码 / ID | 结果解释 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | actor_x | resource=r0 key=k0 action=<action> |  |  |  |  |  |

`最终状态对账` 建议使用表格：

| 数据源 | 查询 | 关键结果 | 是否一致 |
| --- | --- | --- | --- |
| HTTP | GET <query-path> | state=<state> derived=<summary> | 是 |
| Redis | HGETALL <state-key> | state=<state> owner=<actor> | 是 |
| Redis | <READ derived-key> | derived=<summary> | 是 |
| MySQL | SELECT ... FROM <target-table> | rows=<count> aggregate=<summary> | 是 |

## 失败分析

如果并发一致性测试失败，必须按失败类型归类：

| 失败类型 | 判定方式 | 常见原因 |
| --- | --- | --- |
| 状态回退 | 最终状态早于或弱于已成功响应确认的状态 | 条件更新缺失、状态覆盖 |
| 重复成功 | 同一竞争对象产生超过契约允许数量的成功副作用 | 幂等 key、唯一约束或锁未原子生效 |
| 派生视图不一致 | 主状态和查询视图、排序视图或缓存视图不同 | 多数据源更新不在同一原子路径或缺少补偿 |
| 持久化不一致 | 缓存或响应成功，但 DB 记录缺失、重复或内容不一致 | 持久化失败、重试语义不清 |
| 终态冲突 | 多个请求都完成互斥终态动作 | 终态缺少条件更新或唯一约束 |
| 跨主体污染 | 一个 actor 的并发动作影响到另一个 actor 或租户的数据 | 隔离条件缺失、查询或 key 设计错误 |

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

并发一致性测试稳定后，优先沉淀以下回归：

- P0 场景的 Go runner 脚本或可复用用例。
- 幂等、状态唯一性和最终一致性的服务层单元测试。
- Redis Lua、锁或缓存原子路径的集成测试。
- DB 唯一约束或条件更新的 DAO 集成测试。
- CI 中可运行的轻量并发 smoke test。

并发一致性测试不能只沉淀一次性聊天结论；必须在报告中记录 runner 输出、最终对账和清理结果，便于之后复盘和自动化。
