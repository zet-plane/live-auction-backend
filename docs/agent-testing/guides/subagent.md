# Subagent 测试编排指南

本指南定义 agent-testing 任务中如何使用 subagent。它不定义新的业务规则，不替代模块或流程契约，只规定主 agent 与 subagent 的职责边界、授权规则、数据隔离、输出格式和汇总方式。

通用计划字段、依赖授权、数据隔离、证据、报告、清理、敏感信息和失败输出规则见 `docs/agent-testing/templates/protocol.md`。本文只记录 subagent 编排的附加规则。

## 读取入口

使用 subagent 时仍然先按 `docs/agent-testing/README.md` 的渐进式读取规则进入目标任务。

常见读取顺序：

```text
单目标多 agent 编排：
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/subagent.md
docs/agent-testing/modules/<module>.md 或 docs/agent-testing/flows/<flow>.md

多目标并行测试：
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/subagent.md
每个 subagent 只读取自己目标需要的 modules/<module>.md 或 flows/<flow>.md

并发一致性测试涉及 subagent：
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/subagent.md
docs/agent-testing/guides/concurrency.md
docs/agent-testing/guides/go-runner.md
目标模块或流程契约
```

如果计划要求读取项目上下文目录或文件，例如 `CLAUDE.md`、`docs/design/`、`docs/testing/` 或目标模块相关上下文，subagent 只能读取计划或主 agent 明确列出的范围。

## 能力定位

`subagent` 是测试执行编排能力，不是独立测试框架。

- 主 agent 是 test lead，负责读取入口、判断边界、拆分任务、控制真实依赖授权、复核证据和输出最终报告。
- subagent 是 bounded executor，只处理被分配的模块、流程、测试类型、计划草案、证据核查或报告草案。
- 测试边界仍来自 README、runner、guide、module、flow 和已批准计划。
- subagent 不能自行扩大测试范围，不能自行补齐缺失业务规则，不能把建议新增测试直接纳入本次执行范围。
- subagent 的输出是中间产物，主 agent 必须复核后才能写入最终结论。

## 适用场景

适合使用 subagent：

- 一个 module 或 flow 的测试面较宽，需要拆分计划、契约核查、执行和报告。
- 多个模块、流程或测试方向互相独立，可以并行生成计划。
- 多个模块、流程或测试方向已有明确执行许可，可以并行执行测试。
- 需要独立核查 Apifox 对齐、DTO 字段、已有报告或证据完整性。

不适合使用 subagent：

- 用户没有明确允许 subagent、委派或并行 agent 工作。
- 任务目标还不清楚，主 agent 尚未完成基本范围判断。
- 子任务之间共享同一批未隔离数据，可能相互污染。
- 下一步关键决策依赖同一个阻塞问题，主 agent 应先本地处理。
- 需要并发一致性测试但计划尚未通过人工或对话批准。

## 工作模式

### 单目标编排模式

用于一个 module 或 flow，但测试面较宽的场景。

主 agent 可以拆分为：

| 子任务 | 典型职责 |
| --- | --- |
| Plan agent | 提取契约，生成测试计划草案。 |
| Contract agent | 核查接口请求、响应、DTO 或 Apifox 对齐偏差。 |
| Execution agent | 在已授权依赖策略下执行测试并采集证据。 |
| Report agent | 整理子证据，生成报告草案。 |

主 agent 必须复核所有子输出，解决冲突，补齐失败分析，并产出唯一最终报告。

### 多目标并行模式

用于多个模块、流程或测试方向可以独立推进的场景。

拆分规则：

- 每个 subagent 负责一个明确目标，例如一个 module、一个 flow 或一个测试方向。
- 每个 subagent 只读取自己目标需要的文档和计划列出的项目上下文。
- 并行生成计划时，subagent 不得连接真实依赖。
- 并行执行测试时，计划中必须明确依赖策略、真实依赖授权和测试数据边界。
- 如果多个 subagent 连接同一个真实数据库或 Redis，主 agent 必须为每个 subagent 分配唯一子批次、数据前缀或实体集合。

## 授权模型

### 第一层：无需额外授权

subagent 可以执行：

- 读取 README、runner、目标 module 或 flow、必要 guide。
- 读取主 agent 或计划明确列出的项目上下文文件和目录。
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
- 不得违反 `templates/protocol.md` 的敏感信息和失败输出规则。
- 不得扩大到未分配的模块、流程或数据范围。

### 第三层：必须升级确认

以下情况 subagent 必须停止并交回主 agent：

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

- 先读取 `guides/concurrency.md`。
- 先把并发计划写入 `docs/agent-testing/concurrency/`。
- 计划必须包含审核状态和执行许可。
- 用户未批准前，subagent 不得连接真实数据库或 Redis、不得创建测试数据、不得启动 runner、不得发起并发请求。
- 如果用户通过对话批准，负责执行的 agent 必须先把批准记录回写到计划文件，再执行测试。
- 执行后必须把计划文件更新为 executed 并关联报告。

## Subagent Prompt 模板

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

执行型 subagent 还必须包含：

```text
计划路径或计划摘要：
batch id 或 batch id 生成规则：
子批次、前缀或实体 ID 规则：
允许的命令范围：
清理要求：
证据要求：
```

## Subagent 输出格式

每个 subagent 必须输出：

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

## 失败和冲突处理

- subagent 发现业务失败时，必须提供证据，不能只输出主观判断。
- subagent 遇到阻塞时，必须说明阻塞条件、已读文档和需要决策的问题。
- 主 agent 不能把缺少证据的子结论写成最终通过。
- 如果多个 subagent 结论冲突，主 agent 必须停止自动汇总为通过，先复核契约和证据。
- 如果冲突来自测试范围扩大，按 `guides/runner.md` 的规则征得用户确认后再继续。
