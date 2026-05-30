# Design: Agent Testing Keep Simple

Date: 2026-05-31
Status: Draft for Review

## 背景

`docs/agent-testing/` 已经覆盖模块测试、流程测试、真实依赖、Go runner、并发一致性、性能压测、subagent 编排和报告沉淀。能力变完整以后，文档结构开始变重：

- `README.md` 同时承担目录地图、路由表、渐进读取规则、硬规则和输出格式。
- `guides/runner.md` 同时承担执行入口、测试计划、依赖策略、测试类型说明、报告沉淀和回归建议。
- 证据、真实依赖、测试数据隔离、清理、敏感信息、失败报告等通用规则在 `runner.md`、`environment.md`、`go-runner.md`、`reports/README.md`、`subagent.md`、`concurrency-consistency.md`、`performance-load.md` 和 `templates/module.md` 中重复出现。
- `concurrency-consistency.md`、`performance-load.md` 这类长文件名不利于快速路由，也让 README 的路径表显得补丁化。

本设计目标是让 agent-testing 更接近“地图 + 通用协议 + 专项能力”的结构，减少重复规则，同时保持现有测试契约和报告体系不被大幅搬迁。

## 目标

做一个 Keep Simple v1：

1. 新增一个小而深的通用协议文档 `guides/protocol.md`。
2. 让 `README.md` 更像目录地图和第一层路由，而不是规则汇总。
3. 让 `runner.md` 更像执行路由和测试类型选择，而不是所有规则的容器。
4. 收敛两个过长 guide 名称：
   - `guides/concurrency-consistency.md` -> `guides/concurrency.md`
   - `guides/performance-load.md` -> `guides/performance.md`
5. 保留现有目录层级，不引入 `recipes/` 子目录。
6. 不改变模块契约、流程契约、Go runner 模板或已有报告内容。

## 不覆盖范围

- 不重写全部 agent-testing 文档。
- 不创建 `guides/recipes/`。
- 不改 `go-runner.md` 名称，避免和 `runner.md` 混淆。
- 不改 `module-generator.md` 名称，本轮只处理执行主路径。
- 不修改 `modules/*.md` 或 `flows/*.md` 的业务规则。
- 不改变真实依赖、线上依赖、并发审批、敏感信息和数据清理的安全边界。

## 目标结构

```text
docs/agent-testing/
├── README.md
├── guides/
│   ├── protocol.md
│   ├── runner.md
│   ├── environment.md
│   ├── concurrency.md
│   ├── performance.md
│   ├── go-runner.md
│   ├── subagent.md
│   └── module-generator.md
├── templates/
│   ├── module.md
│   └── runner.go
├── modules/
├── flows/
└── reports/
```

`protocol.md` 是通用测试协议。`runner.md` 是执行路由。专项 guide 只写专项差异。

## 通用协议

新增 `docs/agent-testing/guides/protocol.md`，承载现在分散在多个文档里的通用规则。

建议章节：

```text
# Agent Testing 通用协议

## 适用范围
## 渐进式读取
## 测试计划字段
## 依赖授权
## 测试数据隔离
## 证据要求
## 报告沉淀
## 清理规则
## 敏感信息规则
## 失败输出
## 必须停止并询问的情况
## 专项附加规则入口
```

`protocol.md` 不定义具体模块业务规则，也不展开并发、性能、subagent 的专项执行方法。它只定义所有 agent-testing 任务共同遵守的接口。

### 测试计划字段

通用计划字段统一放在 `protocol.md`：

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

- `subagent.md` 追加：`是否使用 subagent`、`主 agent 职责`、`subagent 分工`、`可读取项目上下文`、`真实依赖授权`、`并行数据隔离策略`。
- `concurrency.md` 追加：场景名称、竞争对象、并发请求、预期成功、预期失败、最终不变量、审核状态、执行许可。
- `performance.md` 追加：压测模型、目标阈值、压测源、监控指标、停止条件、业务抽样对账。

### 证据 / 报告 / 清理

本轮不单独创建 `evidence.md`。先把证据、报告、清理作为 `protocol.md` 的章节，形成“一点点 3”的效果。

通用证据规则：

- 每个结论必须有命令、请求、响应、数据库、Redis、WebSocket、日志或 Go 测试输出支撑。
- 证据必须能关联到验证点和结果。
- 长日志只记录关键片段、路径或检索方式。

通用报告规则：

- 测试完成后按 `reports/README.md` 沉淀报告。
- `reports/README.md` 仍是模板源，`protocol.md` 只定义什么时候必须报告、报告不得缺哪些通用证据。

通用清理规则：

- 只能清理本次批次、前缀或明确实体 ID 集合内的数据。
- 禁止 `DROP`、`TRUNCATE`、`FLUSHALL`、`FLUSHDB` 等危险操作。
- 使用真实依赖时，必须记录数据范围和清理结果。
- 多个 subagent 共享真实依赖时，必须记录各自子批次和隔离证明。

## README 改动

`README.md` 应该变薄，承担三个职责：

1. 说明 agent-testing 是测试契约目录。
2. 给出第一层路由表。
3. 给出目录结构和渐进式读取总原则。

README 不再重复完整测试计划字段、证据字段、失败输出字段。相关内容改为指向 `guides/protocol.md`。

快速入口建议变成：

```text
所有测试执行任务 | guides/protocol.md -> guides/runner.md -> 目标契约
并发一致性测试 | guides/protocol.md -> guides/runner.md -> guides/concurrency.md -> guides/go-runner.md -> 目标契约
性能压测 | guides/protocol.md -> guides/runner.md -> guides/performance.md -> guides/environment.md -> 目标契约
subagent 编排 | guides/protocol.md -> guides/runner.md -> guides/subagent.md -> 目标契约
环境准备 | guides/protocol.md -> guides/environment.md
Go runner 证据采集 | guides/protocol.md -> guides/go-runner.md
生成模块契约 | guides/module-generator.md -> templates/module.md
写报告 | guides/protocol.md -> reports/README.md
```

README 中保留少量全局硬规则，但只保留不可错过的红线：

- 不要一次性读取整个目录。
- 目标契约缺规则时必须问用户。
- 真实依赖只能操作本次测试数据。
- 并发一致性测试必须先批准计划。
- 报告不得包含敏感信息。

## Runner 改动

`runner.md` 保留执行视角，但不再重复完整通用协议。

保留内容：

- 核心原则。
- 如何判断目标模块 / 流程。
- 如何从任务类型路由到专项 guide。
- 如何选择测试类型。
- 如何判断是否需要 `environment.md`、`go-runner.md`、`reports/README.md`。

移入或引用 `protocol.md` 的内容：

- 通用测试计划字段。
- 通用依赖授权规则。
- 通用证据规则。
- 通用报告规则。
- 通用失败输出。
- 通用清理规则。
- 通用敏感信息规则。

`runner.md` 可以保留一个短表说明测试类型：

| 测试类型 | 读取 | 重点 |
| --- | --- | --- |
| 单元测试 | 目标代码和单测 | fake/mock，禁止真实依赖 |
| 接口契约 | `protocol.md` + 目标契约 + `go-runner.md` | HTTP 请求/响应和 DTO 对齐 |
| 模块集成 | `protocol.md` + 目标契约 + `go-runner.md` | 真实 DAO/Service/DB/Redis |
| 全流程 | `protocol.md` + flow + 关联 modules | 跨模块闭环 |
| 并发一致性 | `concurrency.md` | 竞争窗口和最终一致性 |
| 性能压测 | `performance.md` | 容量、延迟、错误率、资源瓶颈 |
| WebSocket | 目标契约 + `go-runner.md` 或专项说明 | 真实连接和消息证据 |
| subagent 编排 | `subagent.md` | 分工、授权、隔离、汇总 |

## 专项 Guide 改动

### concurrency.md

由 `concurrency-consistency.md` 重命名而来。

保留：

- 计划先行和人工审核。
- 并发场景六要素。
- 同步起跑、请求窗口、最终状态对账。
- 并发隔离证明。
- 并发失败分析。

减少重复：

- 依赖授权、敏感信息、清理、报告通用字段改为引用 `protocol.md`。
- 只保留并发特有的附加要求。

### performance.md

由 `performance-load.md` 重命名而来。

保留：

- 压测适用范围。
- 环境策略。
- 推荐场景矩阵。
- 压测模型、目标阈值、监控指标、业务抽样对账。
- 性能报告附加字段。

减少重复：

- 通用依赖、清理、敏感信息、证据和报告规则引用 `protocol.md`。
- 只保留性能特有附加要求。

### subagent.md

保留：

- 单目标编排模式。
- 多目标并行模式。
- 授权模型。
- prompt 模板。
- subagent 输出格式。
- 主 agent 汇总规则。

减少重复：

- 通用证据、报告、清理和敏感信息规则引用 `protocol.md`。
- 保留 subagent 特有的数据隔离和升级确认规则。

### reports/README.md

仍是报告模板源，不改为 `guides/report.md`。

调整方向：

- 开头引用 `guides/protocol.md` 的通用证据、清理、敏感信息规则。
- 保留报告命名、报告模板和字段填写说明。
- 避免重复解释所有真实依赖规则。

## 命名规则

本轮采用“短能力名优先”：

- `concurrency-consistency.md` -> `concurrency.md`
- `performance-load.md` -> `performance.md`
- 保留 `go-runner.md`
- 保留 `module-generator.md`
- 新增 `protocol.md`

原因：

- `concurrency.md` 和 `performance.md` 足够表达能力入口。
- 具体语义在文档标题和首段解释，不需要文件名承担全部概念。
- 避免继续扩大 kebab-case 长名。

## 渐进式披露

新的读取层次：

```text
README.md
  -> protocol.md
    -> runner.md
      -> 专项 guide
        -> module / flow 契约
          -> template / report / runner.go（仅在需要时）
```

实际执行时不要求每次都读所有层：

- 如果只是写报告：`README.md -> protocol.md -> reports/README.md`
- 如果只是准备环境：`README.md -> protocol.md -> environment.md`
- 如果只是生成模块契约：`README.md -> module-generator.md -> templates/module.md`
- 如果是普通模块测试：`README.md -> protocol.md -> runner.md -> modules/<module>.md`
- 如果是并发测试：`README.md -> protocol.md -> runner.md -> concurrency.md -> go-runner.md -> target`
- 如果是 subagent：`README.md -> protocol.md -> runner.md -> subagent.md -> target`

## 实施顺序

1. 新增 `guides/protocol.md`，从现有文档中提炼通用规则。
2. 更新 README 路由和目录结构。
3. 重命名 `concurrency-consistency.md` 为 `concurrency.md`，更新引用。
4. 重命名 `performance-load.md` 为 `performance.md`，更新引用。
5. 瘦身 `runner.md`，把通用规则改为引用 `protocol.md`。
6. 更新 `subagent.md`、`concurrency.md`、`performance.md`、`reports/README.md` 中的重复规则为引用。
7. 运行引用检查，确保没有旧路径残留。

## 验收标准

- `docs/agent-testing/README.md` 更短，主要承担地图和路由职责。
- `docs/agent-testing/guides/protocol.md` 成为通用规则唯一入口。
- `docs/agent-testing/guides/runner.md` 不再重复完整证据、报告、清理和敏感信息规则。
- `concurrency-consistency.md` 和 `performance-load.md` 旧路径不再出现在 agent-testing 文档中。
- 并发一致性审批规则仍然明确。
- subagent 数据隔离和主 agent 复核规则仍然明确。
- 报告模板仍可独立指导报告写作。
- 现有模块和流程契约不需要改业务内容。
