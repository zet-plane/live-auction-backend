# Agent Testing

本目录定义 AI agent 如何测试 `live-auction-backend`。这里的文档是测试契约：约束测试边界、依赖策略、证据要求和报告格式。

## 快速入口

所有 agent-testing 任务都先读本文件，再按任务读取最少必要文档。

| 任务 | 下一步读取 |
| --- | --- |
| 执行模块、流程、接口、并发或状态一致性测试 | `guides/runner.md`，再读目标 `modules/<module>.md` 或 `flows/<flow>.md` |
| 使用 subagent 编排单目标测试或多目标并行测试 | `guides/runner.md` -> `guides/subagent.md`，再按目标读取契约 |
| 并行生成多个测试计划 | `guides/runner.md` -> `guides/subagent.md`，每个目标只读自己的模块或流程契约和主 agent 明确列出的项目上下文 |
| 并行执行多个已授权测试计划 | `guides/runner.md` -> `guides/subagent.md`，再按测试类型读取 `guides/go-runner.md`、`guides/environment.md`、`guides/concurrency-consistency.md` 或 `guides/performance-load.md` |
| 设计或执行并发测试 | `guides/runner.md` -> `guides/concurrency.md` -> `guides/go-runner.md`，再读目标契约 |
| 准备环境、连接 DB/Redis、启动服务、创建测试数据 | `guides/environment.md` |
| 使用 Go runner 采集结构化证据 | `guides/runner.md` -> `guides/go-runner.md` |
| 生成或补充模块测试文档 | `guides/module-generator.md` -> `templates/module.md` |
| 写入或补充测试报告 | `reports/README.md` |

## 目录结构

```text
docs/agent-testing/
├── README.md
├── guides/
│   ├── runner.md
│   ├── subagent.md
│   ├── environment.md
│   ├── concurrency.md
│   ├── go-runner.md
│   └── module-generator.md
├── templates/
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
└── reports/
    ├── README.md
    └── *.md
```

## 渐进式读取规则

- 不要一次性读取整个目录。
- 先读 `README.md`，再读任务指南，再读目标契约。
- 使用 subagent 时，先读 `guides/runner.md`，再读 `guides/subagent.md`，然后每个 subagent 只读取自己目标需要的契约和计划列出的项目上下文。
- 并行执行测试时，如果多个 subagent 连接同一真实数据库或 Redis，计划必须先定义唯一子批次、前缀或实体隔离策略。
- 设计或执行并发测试时，先读 `guides/runner.md`，再读 `guides/concurrency.md` 和目标契约。
- 流程文档要求关联模块时，只读取流程明确点名的模块文档。
- 只有生成文档时才读 `templates/module.md`。
- 只有编写 Go runner 时才读 `templates/runner.go`。
- 只有写报告时才读 `reports/README.md`。

## 测试就绪检查

执行任何接口契约、集成、全流程、并发或状态一致性测试前，必须确认：

```text
- go test ./... 无编译错误。
- 目标模块单元测试全部通过。
- 目标测试所需数据库可用；如果使用线上数据库，只能创建或修改本次测试数据。
- 目标测试所需 Redis 可用（如果模块依赖 Redis）。
- 测试数据具备可识别前缀或测试批次 ID，便于清理和复盘。
```

如果任一条件不满足，停止真实依赖测试，按 `guides/environment.md` 输出阻塞信息。

## 全局硬规则

- 本地单元测试不得直连 MySQL、Redis、HTTP 服务、WebSocket 或外部系统，必须使用 mock/fake/进程内数据。
- Agent 直连线上或线上等价依赖时，只能操作本次测试创建的数据或带明确测试前缀/批次 ID 的数据。
- 每个测试结论都必须有证据，例如 HTTP 响应、数据库状态、Redis 状态、WebSocket 消息、日志或测试命令输出。
- 文档中标记为“不适用”的测试类型必须跳过。
- 文档中标记为“禁止”的动作不得执行。
- 如果目标文档缺少关键业务规则，agent 必须先询问用户，不允许自行猜测。
- 发现文档外风险时，只能作为“建议新增测试”记录，不能直接扩大本次测试范围。
- Subagent 的输出只是中间产物，最终通过、失败和风险结论必须由主 agent 复核后写入报告。
- 多个 subagent 连接同一真实依赖时，必须使用互不重叠的批次 ID、名称前缀、幂等 key、Redis key 或实体 ID 集合，禁止相互污染测试数据。
- 执行测试后，应按 `reports/README.md` 沉淀报告。

## 文档类型

- `guides/`：测试执行、环境准备、Go runner、模块文档生成指南。
- `templates/`：模块测试契约模板和 Go runner 模板。
- `modules/`：单模块测试契约。
- `flows/`：跨模块流程测试契约。
- `reports/`：测试报告和报告写作规则。

## Agent 输出要求

每次测试完成后，agent 至少输出：

```text
测试目标：
读取文档：
执行范围：
跳过范围：
执行步骤：
验证证据：
通过项：
失败项：
风险和建议：
建议沉淀的回归测试：
已知缺口：（本次测试因文档或实现原因未覆盖的风险，以及建议如何补充）
```

测试失败时，必须输出：

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
