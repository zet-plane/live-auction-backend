# Design: Agent Testing Subagent Orchestration

Date: 2026-05-31
Status: Approved

## 背景

`docs/agent-testing/` 已经通过 README、runner、module、flow、report 等文档约束 agent 的测试边界、依赖策略、证据要求和报告格式。现有流程默认由一个 agent 串行读取契约、生成计划、执行测试和沉淀报告。

当测试目标变大时，单 agent 串行执行会出现两个问题：

- 单个 module 测试可能包含计划提取、接口契约核查、真实依赖执行、证据整理和报告沉淀，单线程上下文容易变重。
- 多个测试方向或多个模块需要同时推进时，串行等待成本高，且不同目标之间本来可以独立执行。

本设计为 `docs/agent-testing/` 增加正式的 `subagent` 编排能力。它不替代现有测试契约，而是在现有渐进式读取、计划先行、依赖授权和报告沉淀规则之上，定义主 agent 与 subagent 的职责边界。

## 目标

增加一个可复盘、可控、可汇总的多 agent 测试编排协议，支持两类场景：

1. 单目标编排：例如测试一个 module 或 flow，但测试面较宽，需要多个 subagent 分工生成计划、核查契约、执行测试和整理证据。
2. 多目标并行：例如同时测试多个模块或四个测试方向，由多个 subagent 并行生成执行计划或并行执行已授权测试。

所有 subagent 行为仍必须受 `docs/agent-testing/` 的测试契约约束。主 agent 负责最终判断和最终报告，subagent 的输出不能直接等同于最终结论。

## 不覆盖范围

- 不新增真实测试业务规则。
- 不改变已有 module 或 flow 契约的含义。
- 不放宽真实依赖、线上依赖、敏感信息和测试数据清理规则。
- 不绕过并发一致性测试的计划文件和人工审核要求。
- 不要求当前阶段新增复杂的 `subagent/` 产物目录。后续如果子计划和子报告数量过多，再考虑升级目录结构。

## 文档改动

本设计采用轻量接入现有文档链路的方式：

```text
docs/agent-testing/
├── README.md
├── guides/
│   ├── runner.md
│   └── subagent.md      # 新增
└── reports/
    └── README.md
```

改动范围：

| 文件 | 变更 |
| --- | --- |
| `docs/agent-testing/README.md` | 增加 subagent 能力入口，说明单目标编排和多目标并行任务何时读取 `guides/subagent.md`。 |
| `docs/agent-testing/guides/runner.md` | 增加使用 subagent 的执行规则，说明主 agent 如何拆分任务、授权执行和汇总结论。 |
| `docs/agent-testing/guides/subagent.md` | 新增核心协议文档，定义适用场景、职责、授权、输出和冲突处理。 |
| `docs/agent-testing/reports/README.md` | 增加多 agent 报告字段，区分子 agent 证据和主 agent 复核结论。 |

## 能力定位

`subagent` 是 agent-testing 的执行编排能力，不是独立测试框架。

核心原则：

- 主 agent 是 test lead：读取入口文档，判断任务边界，拆分子任务，控制真实依赖授权，汇总最终结论。
- subagent 是 bounded executor：只处理分配给自己的目标、测试类型或证据核查任务。
- 测试边界来自契约：subagent 不得自行扩大 module、flow、guide 或计划未定义的测试范围。
- 真实依赖由计划决定：如果入口、契约或执行计划明确允许并要求真实依赖，subagent 可以在授权范围内执行；如果没有注明，必须询问主 agent 或用户。
- 并发一致性保留强审核：即使由 subagent 生成或执行，并发计划也必须按 `guides/concurrency-consistency.md` 先落文件并获得批准。

## 工作模式

### 单目标编排模式

用于一个 module 或 flow，但测试面较宽的场景。例如用户要求测试 `bid` 模块，主 agent 可以把工作拆成：

- Plan agent：读取 README、runner 和目标契约，生成测试计划草案。
- Contract agent：核查接口请求、响应、DTO 或 Apifox 对齐问题。
- Execution agent：在已批准依赖策略下执行模块测试并采集证据。
- Report agent：整理子证据，生成报告草案。

主 agent 必须复核子 agent 的输出，处理冲突，补齐失败分析，并产出唯一最终报告。

### 多目标并行模式

用于多个模块、多个流程或多个测试方向可以独立推进的场景。例如同时测试 `bid`、`order`、`payment` 和 `deposit`。

拆分规则：

- 每个 subagent 负责一个明确目标，例如一个 module、一个 flow 或一个测试方向。
- 每个 subagent 按渐进式读取规则，只读取自己目标需要的文档。
- 如果只是并行生成执行计划，subagent 不得连接真实依赖。
- 如果是并行执行测试，计划中必须已经明确依赖策略和执行许可。
- 如果多个 subagent 连接同一个真实数据库或 Redis，主 agent 必须为每个 subagent 分配唯一子批次、数据前缀或实体集合，防止并行测试数据相互污染。
- 主 agent 汇总时按目标列出读取文档、测试范围、依赖策略、执行状态、通过项、失败项、跳过项和证据。

## 授权模型

### 第一层：无需额外授权

subagent 可以执行以下工作：

- 读取 README、runner、目标 module 或 flow、必要 guide。
- 读取主 agent 或测试计划明确列出的项目上下文文件和目录，例如 `CLAUDE.md`、`docs/design/`、`docs/testing/` 或目标模块相关上下文。
- 分析代码、DTO、已有报告和测试契约。
- 生成测试计划、子报告草案、风险清单和建议新增测试。
- 运行本地隔离单元测试，但不得连接 MySQL、Redis、HTTP、WebSocket 或外部系统。

### 第二层：计划授权后可执行

当测试入口、目标契约或执行计划已经明确依赖策略，并且该测试类型允许真实依赖时，subagent 可以在分配范围内执行：

- 接口契约测试。
- 模块集成测试。
- 全流程测试。
- 状态一致性测试。

执行要求：

- 只能操作本批次或本 subagent 子批次测试数据。
- 并行执行时，每个 subagent 必须使用唯一 `batch_id`、名称前缀、幂等 key 前缀、Redis key 前缀或明确实体 ID 集合。
- 所有写入、状态验证和清理查询必须限定在该 subagent 的数据边界内，不能按模块、状态或时间范围批量影响其他 subagent 数据。
- 必须记录 batch id、请求响应、数据库或 Redis 证据、清理结果。
- 不得写入敏感地址、密码或可复用 token。
- 不得扩大到未分配的模块、流程或数据范围。

### 第三层：必须升级确认

以下情况 subagent 必须停止并交回主 agent，不能自行决定：

- 计划没有注明是否连接真实依赖。
- 计划没有注明可读取的项目上下文范围，而 subagent 判断需要读取目标契约之外的上下文。
- 契约缺少业务规则、通过标准、关键状态流转或依赖策略。
- 测试需要扩大到未分配模块或流程。
- 模块文档和流程文档冲突。
- 需要执行并发一致性测试但计划还未批准。
- 需要使用线上或线上等价依赖，但测试数据边界不清楚。
- 多个 subagent 可能连接同一真实依赖，但计划没有定义唯一子批次、前缀或实体隔离策略。
- 遇到敏感信息、清理风险或非本批次数据。

## 并发一致性特殊规则

并发一致性测试不因为使用 subagent 而降低门槛。

要求：

- 先读取 `guides/concurrency-consistency.md`。
- 先把并发计划写入 `docs/agent-testing/concurrency/`。
- 计划必须包含审核状态和执行许可。
- 用户未批准前，subagent 不得连接真实数据库或 Redis、不得创建测试数据、不得启动 runner、不得发起并发请求。
- 如果用户通过对话批准，负责执行的 agent 必须先把批准记录回写到计划文件，再执行测试。
- 执行后必须把计划文件更新为 executed 并关联报告。

## Subagent Prompt 结构

主 agent 派发 subagent 时，prompt 必须自包含，至少包括：

```text
任务目标：
分配范围：
读取入口：
必须读取：
可读取项目上下文：
禁止读取或禁止扩大范围：
测试类型：
依赖策略：
真实依赖授权：未授权 | 已按计划授权 | 遇到不明确时升级
测试数据边界：
并行数据隔离策略：
输出格式：
敏感信息规则：
需要交回主 agent 的情况：
```

对于执行型 subagent，还必须包含：

```text
计划路径或计划摘要：
batch id 或 batch id 生成规则：
子批次、前缀或实体 ID 规则：
允许的命令范围：
清理要求：
证据要求：
```

## Subagent 输出格式

每个 subagent 必须输出结构化结果，便于主 agent 汇总：

```text
子任务：
执行状态：planned | executed | blocked | failed
读取文档：
依赖策略：
测试数据：
执行步骤：
验证证据：
通过项：
失败项：
跳过项：
阻塞项：
需要主 agent 决策：
建议下一步：
产物路径：
```

如果失败，必须补充：

```text
失败场景：
复现步骤：
期望结果：
实际结果：
相关证据：
可能原因：
建议修复点：
建议新增的回归测试：
```

## 主 Agent 汇总规则

主 agent 汇总 subagent 结果时必须：

- 标明每个 subagent 的目标、状态和产物路径。
- 区分子 agent 原始结论和主 agent 复核结论。
- 对冲突结论给出处理方式，例如重新读取契约、要求补证据或标为阻塞。
- 对未执行、跳过和阻塞项说明原因与补测条件。
- 最终报告仍按 `reports/README.md` 写入 `docs/agent-testing/reports/`。

多 agent 报告建议增加：

```text
执行 agent：
- 主 agent：
- 子 agent：

子 agent 结果摘要：
主 agent 复核结论：
冲突和处理：
```

## 失败和冲突处理

失败处理规则：

- subagent 发现业务失败时，必须提供证据，不能只输出主观判断。
- subagent 遇到阻塞时，必须说明阻塞条件、已读文档和需要决策的问题。
- 主 agent 不能把缺少证据的子结论写成最终通过。
- 如果多个 subagent 结论冲突，主 agent 必须停止自动汇总为通过，先复核契约和证据。
- 如果冲突来自测试范围扩大，按 runner 规则征得用户确认后再继续。

## 验收标准

文档实现后，应满足：

- README 能把 subagent 相关任务路由到 `guides/subagent.md`。
- runner 明确主 agent 与 subagent 的执行边界。
- `guides/subagent.md` 能独立说明适用场景、两种工作模式、授权模型、prompt 模板和输出格式。
- reports 能表达多 agent 测试结果，并保留主 agent 的复核责任。
- 现有单 agent 测试流程不受影响。
- 并发一致性测试的人工审核规则不被绕过。
