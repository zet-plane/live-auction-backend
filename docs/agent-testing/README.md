# Agent Testing

本目录用于约束 AI agent 如何测试 live-auction-backend。

这里的文档不是普通测试笔记，而是 **测试契约**：它定义 agent 测试某个模块或流程时，允许测什么、禁止测什么、依据什么规则判断、必须输出哪些证据。

## 目录结构

```text
docs/agent-testing/
├── README.md
├── environment.md
├── agent-runner-guide.md
├── go-runner-guide.md
├── runner-template.go
├── module-generator-guide.md
├── template.md
├── modules/
│   ├── bid.md
│   ├── deposit.md
│   ├── item.md
│   ├── order.md
│   ├── payment.md
│   ├── room.md
│   └── user.md
├── flows/
│   └── auction-lifecycle.md
└── reports/
    └── README.md
```

## 测试就绪检查

Agent 执行测试前，必须先区分测试类型：

- 本地代码单元测试：不允许直接连接数据库、Redis 或外部服务，必须使用 mock、fake 或进程内构造数据。
- Agent 接口契约、模块集成、全流程、并发和状态一致性测试：允许按测试任务和目标文档要求直接连接线上数据库或线上等价依赖验证真实状态。

Agent 执行任何接口契约、集成、全流程、并发或状态一致性测试前，必须先确认以下条件全部满足：

```text
- go test ./... 无编译错误。
- 目标模块单元测试全部通过。
- 目标测试所需数据库可用；如果使用线上数据库，必须只创建或修改本次测试数据。
- 目标测试所需 Redis 可用（如果模块依赖 Redis）。
- 测试数据具备可识别前缀或测试批次 ID，便于清理和复盘。
```

只有全部满足后，才允许进入集成或全流程测试。否则必须先报告阻塞原因，等待用户确认。

## 渐进式读取规则

Agent 不应一次性读取整个 `docs/agent-testing/` 目录。必须先读本文件，再按任务类型逐层下钻，只读取完成当前任务所需的最小文档集合。

### 第一层：入口路由

所有 agent-testing 相关任务都先读取：

```text
docs/agent-testing/README.md
```

本文件只用于判断任务类型、全局硬规则和下一步该读哪个文档。

### 第二层：任务指南

按任务类型只读取相关指南：

| 用户意图 | 下一步读取 |
| --- | --- |
| 准备环境、连接数据库/Redis、启动服务、创建测试数据 | `environment.md` |
| 执行模块、流程、接口、并发或状态一致性测试 | `agent-runner-guide.md` |
| 执行接口契约、集成或状态一致性测试，需要结构化证据采集（先读 `agent-runner-guide.md`） | `go-runner-guide.md` |
| 生成或补充模块测试文档 | `module-generator-guide.md` |
| 写入或补充测试报告 | `reports/README.md` |

### 第三层：目标契约

按测试目标读取业务契约：

| 测试目标 | 下一步读取 |
| --- | --- |
| 单个模块 | `modules/<module>.md` |
| 跨模块流程 | `flows/<flow>.md` |
| 流程文档要求关联模块 | 只读取流程明确要求的模块文档 |
| 目标模块没有文档 | 转入 `module-generator-guide.md` 生成文档 |

### 第四层：按需模板

只有在对应任务发生时才读取：

- 生成模块测试文档时读取 `template.md`。
- 写测试报告时读取 `reports/README.md`。
- 操作数据库、Redis、服务或测试数据时读取 `environment.md`。
- 对齐接口规范时按执行指南读取 Apifox 相关资料。

## 全局硬规则

- 如果目标文档缺少关键业务规则，agent 必须先询问用户，不允许自行猜测。
- Agent 只能在文档定义的测试边界内行动。
- 文档中标记为“不适用”的测试类型必须跳过。
- 文档中标记为“禁止”的动作不得执行。
- 本地单元测试不得因为方便而直连数据库；需要存储状态时使用 mock/fake。
- Agent 直连线上数据库时，只能操作本次测试创建的数据或带明确测试前缀 / 测试批次 ID 的数据。
- 每个测试结论都必须有证据，例如 HTTP 响应、数据库状态、Redis 状态、WebSocket 消息、日志或测试命令输出。
- Agent 执行测试后，应按 `reports/README.md` 的规则沉淀测试报告。
- 发现文档外风险时，只能作为“建议新增测试”记录，不能直接扩大本次测试范围。

## 文档类型

### 模块测试文档

模块测试文档用于描述一个业务模块如何被测试。

示例：

- `modules/bid.md`
- `modules/deposit.md`
- `modules/item.md`
- `modules/order.md`
- `modules/payment.md`
- `modules/room.md`
- `modules/user.md`

模块文档适合覆盖：

- 单元测试。
- 接口契约测试。
- 业务场景测试。
- 异常测试。
- 边界测试。
- 并发测试。
- 状态一致性测试。
- WebSocket 测试。
- 回归测试。

### 模块测试文档生成器

`module-generator-guide.md` 用于指导 agent 根据代码和模板生成模块测试文档。

它适合处理：

- 新模块还没有 `modules/<module>.md`。
- 已有模块文档缺少测试类型。
- 代码新增接口后，需要补充测试边界。
- 测试前发现模块规则不清晰，需要先补测试契约。

### 流程测试文档

流程测试文档用于描述跨模块的端到端测试。

示例：

- `flows/auction-lifecycle.md`

流程文档适合覆盖：

- 商家创建拍品。
- 配置竞拍规则。
- 创建竞拍场次。
- 用户参与竞拍。
- 出价与排名变化。
- WebSocket 推送。
- 落锤成交。
- 成交后状态验证。

### 测试报告

`reports/README.md` 用于定义测试报告的命名、内容、证据、敏感信息和清理记录要求。

Agent 执行测试后，应将可复盘的测试结果沉淀为 Markdown 报告。

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
