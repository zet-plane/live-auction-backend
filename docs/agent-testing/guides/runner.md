# Agent 测试执行指南

通用计划字段、依赖授权、数据隔离、证据、报告、清理、敏感信息和失败输出规则见 `docs/agent-testing/templates/protocol.md`。本文只负责执行路由和测试类型选择。

本指南定义 AI agent 如何使用 `docs/agent-testing` 下的测试契约文档来执行测试。它不定义具体业务规则；具体业务规则必须来自模块文档或流程文档。

## 核心原则

1. 文档定义边界。
2. Agent 定义执行计划。
3. 测试工具和断言给出证据。
4. 缺少业务规则时，必须先问用户，不允许自行猜测。
5. 发现文档外风险时，只能提出建议，不能直接扩大测试范围。

## 执行入口

Agent 接到测试任务后，必须遵守 `README.md` 和 `templates/protocol.md` 的渐进式读取规则：先读入口和通用协议，再读本执行指南，最后按测试目标读取模块、流程或专项文档。

```text
测试单个模块：
读取 docs/agent-testing/README.md
读取 docs/agent-testing/templates/protocol.md
读取 docs/agent-testing/guides/runner.md
读取 docs/agent-testing/modules/<module>.md

测试跨模块流程：
读取 docs/agent-testing/README.md
读取 docs/agent-testing/templates/protocol.md
读取 docs/agent-testing/guides/runner.md
读取 docs/agent-testing/flows/<flow>.md
只按流程文档要求读取关联模块文档

测试并发一致性场景：
读取 docs/agent-testing/README.md
读取 docs/agent-testing/templates/protocol.md
读取 docs/agent-testing/guides/runner.md
读取 docs/agent-testing/guides/concurrency.md
读取 docs/agent-testing/guides/go-runner.md
读取 docs/agent-testing/modules/<module>.md 或 docs/agent-testing/flows/<flow>.md

测试性能压测场景：
读取 docs/agent-testing/README.md
读取 docs/agent-testing/templates/protocol.md
读取 docs/agent-testing/guides/runner.md
读取 docs/agent-testing/guides/performance.md
读取 docs/agent-testing/guides/environment.md
读取 docs/agent-testing/modules/<module>.md 或 docs/agent-testing/flows/<flow>.md

使用 subagent 编排单目标测试：
读取 docs/agent-testing/README.md
读取 docs/agent-testing/templates/protocol.md
读取 docs/agent-testing/guides/runner.md
读取 docs/agent-testing/guides/subagent.md
读取目标 docs/agent-testing/modules/<module>.md 或 docs/agent-testing/flows/<flow>.md

使用 subagent 并行测试多个目标：
读取 docs/agent-testing/README.md
读取 docs/agent-testing/templates/protocol.md
读取 docs/agent-testing/guides/runner.md
读取 docs/agent-testing/guides/subagent.md
每个 subagent 只读取自己目标需要的模块或流程契约

涉及环境准备、连接数据库/Redis、启动服务或创建测试数据：
再读取 docs/agent-testing/guides/environment.md

测试结束需要沉淀报告：
再读取 docs/agent-testing/reports/README.md
```

如果用户没有明确模块或流程，agent 必须先询问用户要测试哪个目标。

## 执行流程

### 1. 读取测试契约

Agent 必须提取目标契约中的测试目标、边界、禁止事项、业务规则、不变量、测试数据准备、需要覆盖的测试类型、状态一致性要求和通过标准。

### 2. 判断文档是否足够

如果出现以下情况，agent 必须停止并询问用户：

- 业务规则缺失。
- 通过标准缺失。
- 关键状态流转没有定义。
- 并发优先级没有定义。
- 模块文档和流程文档冲突。
- 测试依赖不明确，且会影响测试结论。

Agent 不允许通过猜测补齐这些信息。

### 3. 生成测试计划

执行前，agent 必须输出测试计划。按 `templates/protocol.md` 的通用字段输出，subagent、concurrency、performance 追加各自专项字段。

如果使用 subagent，测试计划必须写清主 agent 与每个 subagent 的分工、每个 subagent 可读取的项目上下文、真实依赖授权状态，以及并行执行时的唯一子批次、前缀或实体隔离策略。计划没有写清时，subagent 只能生成计划或提出阻塞项，不得连接真实依赖。

如果测试计划超出文档边界，必须先征得用户确认。

如果测试类型是并发一致性测试，agent 必须在执行前额外输出 `guides/concurrency.md` 要求的完整并发场景设计，并等待用户确认。未获得确认前，不得连接真实依赖、创建测试数据或发起并发请求。

### 4. 选择依赖策略

Agent 必须根据测试类型选择 mock、fake 或真实依赖，并遵守 `templates/protocol.md` 的依赖授权、数据隔离、清理和敏感信息规则。

原则：

> Mock 用来隔离不可控依赖，不是用来逃避真实业务风险。

> 执行接口契约测试、模块集成测试或状态一致性测试时，优先使用 Go runner 采集结构化证据，而不是手工构造散点 curl 命令。参见 `docs/agent-testing/guides/go-runner.md`。

本地代码单元测试禁止直接连接数据库、Redis 或外部服务。Agent 执行接口契约、模块集成、全流程、并发一致性、性能压测和状态一致性测试时，可以在授权和隔离明确后连接测试库、线上等价真实依赖或用户明确授权的真实依赖。

#### Apifox 对齐步骤

执行接口契约测试前，agent 应对比目标接口的 Request/Response 字段结构与 Apifox 当前规范是否一致。如有字段偏差，先记录为"Apifox 对齐问题"，再继续执行测试。测试报告中必须单独列出对齐偏差，不得将其混入失败项。

Apifox 对齐至少检查：

- 目标路径是否存在。
- 必填字段、枚举、金额/时间字段、分页默认值和最大值是否与代码及模块文档一致。
- 成功响应字段是否与 DTO 一致。
- 错误响应是否使用统一错误结构。

如果发现偏差，不要因为 Apifox 偏差阻塞业务测试，除非偏差会导致无法构造请求或无法判断通过标准。可继续测试时，必须在报告的“Apifox 对齐偏差”中单独列出。

### 5. 选择测试类型

| 测试类型 | 读取 | 重点 |
| --- | --- | --- |
| 单元测试 | 目标模块文档、相关代码测试文件 | 隔离数据库、Redis、HTTP 服务、WebSocket 和外部系统；使用 mock/fake 验证纯业务逻辑 |
| 接口契约 | 目标模块文档、接口实现、Apifox 规范 | 校验 HTTP 请求、响应、错误码、字段结构，并执行 Apifox 对齐 |
| 模块集成 | 目标模块文档、`guides/environment.md`、必要时 `guides/go-runner.md` | 验证模块内真实 DAO/Service/缓存协作，只 mock 不可控第三方 |
| 全流程 | 目标流程文档、流程要求的模块文档、`guides/environment.md` | 验证跨模块业务闭环、最终状态和核心不变量 |
| 并发一致性 | `guides/concurrency.md`、目标模块或流程文档、必要时 `guides/go-runner.md` | 先输出完整并发设计并等批准；验证竞态、锁、事务和最终状态唯一性 |
| 性能压测 | `guides/performance.md`、`guides/environment.md`、目标模块或流程文档 | 明确压测模型、阈值、压测源、停止条件、监控指标和业务抽样对账 |
| WebSocket | 目标模块或流程文档、`guides/environment.md` | 使用真实连接和真实业务触发消息，以查询接口或存储状态校验推送结果 |
| subagent 编排 | `guides/subagent.md`、每个目标自己的模块或流程契约 | 拆分互不重叠任务，限定读取上下文，明确授权、子批次和数据隔离 |

### 6. 执行测试

Agent 执行测试时按 `templates/protocol.md` 记录证据。不要只输出“测试通过”或“测试失败”。

如果目标文档中的某些测试类型因为业务语义未确认、环境未就绪或超出当前任务边界而未执行，agent 必须把它们列为“跳过项”，并说明原因、风险、后续补测条件和建议的回归测试类型。跳过项不能混入通过项，也不能记为失败项，除非目标文档明确要求本次必须覆盖。

### 7. 沉淀测试报告

测试结束后，agent 应读取 `docs/agent-testing/reports/README.md`，并按其中规则生成测试报告。报告必须遵守 `templates/protocol.md` 的报告、证据、清理和敏感信息规则。

### 8. 回归沉淀

发现 bug 或高风险行为后，agent 必须建议沉淀方式。

可选类型：

- 单元测试。
- 接口契约测试。
- 模块集成测试。
- 全流程测试。
- 并发一致性测试。
- 性能压测。
- 文档化回归场景。

选择标准：

```text
纯业务规则错误 -> 单元测试
HTTP 响应不符合约定 -> 接口契约测试
模块真实依赖协作错误 -> 模块集成测试
跨模块链路错误 -> 全流程测试
竞态或状态覆盖错误 -> 并发一致性测试
吞吐、延迟或资源瓶颈问题 -> 性能压测
业务规则不清晰 -> 补充测试契约文档
```

## Agent 自检清单

执行前检查：

- 是否读取了 `docs/agent-testing/README.md` 和 `docs/agent-testing/templates/protocol.md`？
- 是否读取了目标模块或流程文档？
- 是否通过了测试就绪检查？
- 是否明确了测试目标、测试边界、禁止事项和通过标准？
- 是否按测试类型读取了必要专项 guide？
- 如果是并发一致性测试，是否已按 `guides/concurrency.md` 输出完整设计并等待批准？
- 如果是性能压测，是否已读取 `guides/performance.md` 和 `guides/environment.md`？
- 如果是接口契约测试，是否执行或计划执行 Apifox 对齐？
- 是否存在必须先问用户的问题？

执行后检查：

- 是否每个结论都有证据？
- 是否说明了跳过范围？
- 是否区分了通过项、失败项和跳过项？
- 是否提出了回归沉淀建议？
- 是否按 `docs/agent-testing/reports/README.md` 沉淀报告？
- 是否发现需要补充测试契约文档的地方？
