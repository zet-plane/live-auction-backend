# Agent Testing

本目录定义 AI agent 如何测试 `live-auction-backend`。本文件只作为第一层地图；测试协议、证据、清理和报告规则按入口表继续读取。

## 快速入口

所有 agent-testing 任务都先读本文件，再按任务读取最少必要文档。

| 任务 | 下一步读取 |
| --- | --- |
| 所有测试执行任务 | `templates/protocol.md` -> `guides/runner.md` -> 目标 `modules/<module>.md` 或 `flows/<flow>.md` |
| Redis/MySQL 故障切换验证 | `templates/protocol.md` -> `guides/environment.md` -> `guides/failover.md` |
| 并发一致性测试 | `templates/protocol.md` -> `guides/runner.md` -> `guides/concurrency.md` -> `guides/go-runner.md` -> 目标契约 |
| 性能压测 | `templates/protocol.md` -> `guides/runner.md` -> `guides/performance/README.md` -> `guides/performance/types.md` -> `guides/performance/online.md` -> `guides/performance/runner.md` -> `guides/environment.md` -> 目标契约 |
| 已批准计划的 subagent 执行编排 | `templates/protocol.md` -> `guides/runner.md` -> `guides/subagent.md` -> 已批准计划 -> 目标契约 |
| 环境准备、连接 DB/Redis、启动服务、创建测试数据 | `templates/protocol.md` -> `guides/environment.md` |
| 使用 Go runner 采集结构化证据 | `templates/protocol.md` -> `guides/go-runner.md` |
| 生成或补充模块测试文档 | `guides/module-generator.md` -> `templates/module.md` |
| 写入或补充测试报告 | `templates/protocol.md` -> `reports/README.md` |

## 目录结构

```text
docs/agent-testing/
├── README.md
├── guides/
│   ├── runner.md
│   ├── subagent.md
│   ├── environment.md
│   ├── concurrency.md
│   ├── performance/
│   │   ├── README.md
│   │   ├── types.md
│   │   ├── online.md
│   │   ├── runner.md
│   │   ├── subagent.md
│   │   └── performance-runner.go
│   ├── go-runner.md
│   └── module-generator.md
├── templates/
│   ├── protocol.md
│   ├── module.md
│   └── runner.go
├── modules/
│   ├── bid.md
│   ├── deposit.md
│   ├── item.md
│   ├── order.md
│   ├── payment.md
│   ├── room.md
│   ├── user.md
│   └── ws.md
├── flows/
│   └── auction-lifecycle.md
├── failover/
│   └── *-plan.md
├── concurrency/
│   └── *-plan.md
├── performance-runs/
│   └── <batch_id>/
└── reports/
    ├── README.md
    └── *.md
```

## 渐进式读取规则

- 不要一次性读取整个目录；先读 `README.md`，再按入口表读取 `templates/protocol.md`、任务指南、runner 和目标契约。
- 专项指南只在任务需要时读取，例如并发、性能类型、线上压测闭环、Performance runner、性能 subagent、环境、Go runner 或通用 subagent 编排。
- 流程文档要求关联模块时，只读取流程明确点名的模块文档。
- 除 `templates/protocol.md` 作为通用协议默认读取外，其他模板只在生成模块文档、编写 Go runner 或写报告时读取。

## 测试执行协议

测试计划、就绪检查、证据、失败输出和清理要求见 `templates/protocol.md`；环境阻塞按 `guides/environment.md` 处理。

## 全局硬规则

- 不要一次性读取整个目录。
- 如果目标文档缺少关键业务规则、通过标准、并发语义或最终不变量，agent 必须先询问用户，不允许自行猜测。
- 正式测试计划不得包含“待确认语义”或未决问题；语义必须先在对话中问清楚，再写入计划。
- Agent 直连真实或线上等价依赖时，只能操作本次测试创建的数据或带明确测试前缀/批次 ID 的数据。
- 执行并发一致性测试前，agent 必须先输出完整并发场景设计，并等待用户确认后才能连接真实依赖或发起并发请求。
- 报告不得包含线上地址、凭据、密码、可复用 token 或其他敏感信息。
- Subagent 的输出只是中间产物，最终通过、失败和风险结论必须由主 agent 复核后写入报告。
- 多个 subagent 连接同一真实依赖时，必须使用互不重叠的批次 ID、名称前缀、幂等 key、Redis key 或实体 ID 集合，禁止相互污染测试数据。
- Subagent 是已批准计划的执行器，不是测试计划内容；使用完成、阻塞或失败后，主 agent 必须关闭 subagent 并记录 cleanup。

## 文档类型

- `guides/`：测试执行、环境准备、Go runner、Performance runner、模块文档生成指南。
- `templates/`：通用协议模板、模块测试契约模板和 Go runner 模板。
- `modules/`：单模块测试契约。
- `flows/`：跨模块流程测试契约。
- `failover/`：Redis/MySQL 可用性、降级和故障恢复测试计划。
- `concurrency/`：并发一致性测试计划草案、审核记录和执行许可记录。
- `performance-runs/`：性能压测 runner 代码、脱敏复跑说明和脱敏证据摘要。
- `reports/`：测试报告和报告写作规则。

## Agent 输出要求

测试计划、证据、失败输出、清理和敏感信息规则见 `templates/protocol.md`；写入或补充报告时按 `reports/README.md` 执行。
