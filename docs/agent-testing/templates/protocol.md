# Agent Testing 通用协议

本模板定义所有 agent-testing 任务共同遵守的通用协议。它不定义具体模块业务规则，不替代模块或流程契约，也不展开并发、性能、subagent 等专项执行方法。

## 适用范围

本协议适用于：

- 本地单元测试。
- 接口契约测试。
- 模块集成测试。
- 全流程测试。
- 状态一致性测试。
- 并发一致性测试。
- 性能压测。
- WebSocket 测试。
- subagent 执行编排。
- 测试报告沉淀。

专项 guide 可以追加字段和步骤，但不能放宽本协议中的安全边界。

## 渐进式读取

默认读取顺序：

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
目标 modules/<module>.md 或 flows/<flow>.md
```

只在需要时读取专项文档：

- 环境准备、连接 DB/Redis、启动服务或创建测试数据：读 `guides/environment.md`。
- 使用 Go runner 采集结构化证据：读 `guides/go-runner.md` 和 `templates/runner.go`。
- 设计或执行并发一致性测试：读 `guides/concurrency.md`。
- 设计或执行性能压测：读 `guides/performance/README.md`、`guides/performance/types.md`、`guides/performance/online.md`、`guides/performance/runner.md` 和 `guides/performance/scenarios.md`。
- 计划批准后使用性能压测 subagent：再读 `guides/subagent.md` 和 `guides/performance/subagent.md`。
- 已批准计划进入执行阶段且需要 subagent：读 `guides/subagent.md`。
- 写入或校验报告：读 `reports/README.md`。
- 生成模块契约：读 `guides/module-generator.md` 和 `templates/module.md`。

禁止一次性读取整个 `docs/agent-testing/` 目录。

## 测试计划字段

执行测试前，agent 必须输出测试计划。通用字段：

```text
测试目标：
读取文档：
测试范围：
禁止范围：
测试类型：
测试数据：
依赖策略：
执行步骤：
验证方式：
预计输出：
```

专项 guide 可以追加字段：

- `guides/concurrency.md`：追加涉及模块、场景名称、竞争对象、并发请求、预期成功、预期失败和最终不变量。
- `guides/performance/README.md` / `guides/performance/types.md`：追加压测模型、目标阈值、压测源、监控指标、停止条件、runner 路径和业务抽样对账。

如果测试计划超出目标契约边界，必须先征得用户确认。

## 语义确认门

所有正式测试计划、专项计划、模块契约、流程契约和测试报告只能写入已经确认的业务语义、通过标准和最终不变量。

如果存在以下任何情况，agent 必须先像 brainstorm 一样向用户或主 agent 提问澄清，拿到明确答案后再生成或更新正式测试文档：

- 业务规则缺失。
- 通过标准缺失。
- 关键状态流转没有定义。
- 并发优先级没有定义。
- 允许成功数量、允许失败错误码或幂等语义不明确。
- 最终不变量无法从目标契约或用户确认中推出。
- 模块文档和流程文档冲突。

禁止在正式测试文档中写入“待确认语义”“需确认问题”“待用户确认后再定”等未决章节。未确认内容只能作为对话中的问题、草稿备注或阻塞项，不得混入已生成的正式测试文档。

## 计划执行门

以下任务必须遵循“生成计划 -> review -> 执行计划”：

- 连接真实依赖的模块测试。
- 连接真实依赖的 flow 流程测试。
- 并发一致性测试。
- 性能压测。

review 未通过或未明确批准前，agent 只能完善计划、列出阻塞项或询问用户，不得连接真实依赖、创建测试数据或发起请求。

批准可以来自用户对话、主 agent 复核，或计划文件中的明确标记。批准后，agent 必须按计划执行；如果执行中需要扩大范围、改变依赖策略或影响新的数据范围，必须回到 review。

执行完成后，报告必须记录计划来源、批准方式、执行结果和偏离项。

## 执行编排选择

测试计划批准后、执行前，agent 应根据计划规模和独立性提醒用户选择执行编排方式：

- 主 agent 串行执行。
- subagent 串行执行。
- subagent 并行执行。

执行编排方式不是业务测试计划的一部分，不应写入模块契约、流程契约或并发计划正文。选择 subagent 后，才读取 `guides/subagent.md` 并按其中规则派发执行器。

## 依赖授权

本地单元测试必须隔离数据库、Redis、HTTP 服务、WebSocket 和外部系统，使用 mock、fake、进程内数据、固定时间和固定 ID。

Agent 执行接口契约、模块集成、全流程、并发一致性、性能压测和状态一致性测试时，可以连接测试库、线上等价真实依赖或用户明确授权的真实依赖。

连接真实依赖前必须满足：

- 目标契约或测试计划明确允许真实依赖。
- 测试数据边界明确。
- 清理策略明确。
- 地址、凭据、token 不写入报告。

如果计划没有注明是否连接真实依赖，agent 只能生成计划或阻塞项，不得自行连接。

## 测试数据隔离

所有真实依赖测试必须使用可识别测试批次、名称前缀、幂等 key 前缀、Redis key 前缀或明确实体 ID 集合。

并行测试或 subagent 并行执行时，每个执行器必须有唯一子批次或互不重叠的数据范围。

禁止：

- 使用非本批次数据做破坏性操作。
- 按模块、状态或时间范围批量修改无法识别归属的数据。
- 清空数据库或 Redis。
- 复用真实用户手机号、支付信息或可复用 token。

## 证据要求

每个测试结论都必须有证据支撑。可接受证据包括：

- 测试命令和退出码。
- HTTP 请求和响应摘要。
- WebSocket 消息摘要。
- MySQL 查询结果摘要。
- Redis 查询结果摘要。
- Go 测试输出摘要。
- Go runner CASE / SUMMARY / CLEANUP 输出。
- 关键日志片段、日志文件路径或检索方式。

证据必须能对应到验证点和结果。不要只写“通过”或“失败”。

长日志只记录关键片段、路径或检索方式。

## 报告沉淀

执行测试后，应按 `docs/agent-testing/reports/README.md` 沉淀报告。

报告必须能追溯测试目标、范围、依赖策略、数据批次、执行步骤、验证证据、测试结论、风险或缺口以及清理结果。

使用 subagent 时，报告必须区分子 agent 原始结论和主 agent 复核结论。

## 清理规则

测试创建数据时，必须记录：

```text
测试批次 ID：
创建的数据：
清理方式：
清理结果：
未清理原因：
```

只能清理本次批次、前缀或明确实体 ID 集合内的数据。

禁止执行：

- `DROP DATABASE`
- `DROP TABLE`
- `TRUNCATE`
- `FLUSHALL`
- `FLUSHDB`

如果多个 subagent 并行执行，清理记录必须按 subagent 区分子批次、前缀或实体 ID 集合。

subagent cleanup 还包括关闭执行器本身：主 agent 在复核其输出、确认不再需要继续对话或补证据后，必须关闭对应 subagent，避免执行器长期挂起。

## 敏感信息规则

报告和文档中禁止写入：

- 数据库密码。
- Redis 密码。
- Apifox token。
- 生产环境地址。
- 线上数据库地址。
- 线上 Redis 地址。
- 真实用户手机号。
- 真实支付信息。
- 任何可复用认证 token。

认证信息只能写成：

```text
认证方式：使用测试环境 token，具体值已省略。
```

## 失败输出

测试失败时，输出必须可复现、可定位、可回归，至少包含失败场景、复现步骤、期望和实际结果、相关证据、可能原因、影响范围、建议修复点和建议新增的回归测试。

如果是不变量或一致性失败，还必须说明违反位置、相关数据源和最终状态。

## 必须停止并询问的情况

出现以下情况时，agent 必须停止并询问用户或主 agent：

- 目标模块或流程不明确。
- 业务规则缺失。
- 通过标准缺失。
- 关键状态流转没有定义。
- 并发优先级没有定义。
- 允许成功数量、允许失败错误码或幂等语义不明确。
- 正式计划会包含待确认语义。
- 模块文档和流程文档冲突。
- 测试依赖不明确，且会影响结论。
- 计划没有注明真实依赖授权。
- 测试数据边界不清楚。
- 清理策略可能影响非本批次数据。
- 发现文档外风险，需要扩大测试范围。

## 专项附加规则入口

专项规则只写差异：

- 并发一致性测试：见 `docs/agent-testing/guides/concurrency.md`。
- 性能压测：见 `docs/agent-testing/guides/performance/README.md`、`docs/agent-testing/guides/performance/types.md`、`docs/agent-testing/guides/performance/online.md`、`docs/agent-testing/guides/performance/runner.md`、`docs/agent-testing/guides/performance/subagent.md` 和 `docs/agent-testing/guides/performance/scenarios.md`。
- subagent 编排：见 `docs/agent-testing/guides/subagent.md`。
- Go runner 证据采集：见 `docs/agent-testing/guides/go-runner.md`。
- 环境准备：见 `docs/agent-testing/guides/environment.md`。
- 报告模板：见 `docs/agent-testing/reports/README.md`。
