# 性能压测指南

本指南是性能压测入口。它负责定义读取顺序、执行路线和结论门槛；类型定义、线上闭环、subagent 编排和 runner 落地分别由专项文档承载。

通用计划字段、依赖授权、数据隔离、证据、报告、清理、敏感信息和失败输出规则见 `docs/agent-testing/templates/protocol.md`。性能压测不能替代并发一致性测试；并发出价唯一性、幂等和最终状态不变量见 `docs/agent-testing/guides/concurrency.md`。

## 读取顺序

执行性能压测时按以下顺序读取文档：

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/performance/README.md
docs/agent-testing/guides/performance/types.md
docs/agent-testing/guides/performance/online.md
docs/agent-testing/guides/performance/runner.md
docs/agent-testing/guides/environment.md
docs/agent-testing/modules/<module>.md 或 docs/agent-testing/flows/<flow>.md
```

如果计划批准后使用 subagent，再读取：

```text
docs/agent-testing/guides/subagent.md
docs/agent-testing/guides/performance/subagent.md
```

如果压测完成后要写报告，再读取：

```text
docs/agent-testing/reports/README.md
```

## 文档分工

| 文档 | 负责内容 |
| --- | --- |
| `performance/README.md` | 入口、路线、全局硬规则和结论门槛 |
| `performance/types.md` | 性能压测专属类型、环境类型、证据和结论枚举 |
| `performance/online.md` | 线上受控压测闭环、port-forward 模式、授权和判停 |
| `performance/runner.md` | performance runner 代码落地、运行、STOP 文件和输出块 |
| `performance/subagent.md` | 主 agent 和 subagent 的角色拆分、状态机和权限边界 |

## 适用范围

性能压测用于回答以下问题：

- 目标链路在指定环境中能承载多少 QPS / TPS。
- 目标并发用户数下的 P50、P95、P99 延迟是否达标。
- 错误率、超时率和业务失败率是否在可接受范围内。
- CPU、内存、GC、goroutine、MySQL、Redis、网络或 WebSocket 推送链路中的首个瓶颈在哪里。
- 超过目标压力后，系统是否可控退化，而不是雪崩或产生错误业务状态。

不适合作为性能压测：

- 只验证字段格式、鉴权或普通错误码的接口契约测试。
- 只验证同一状态竞争结果是否唯一的并发一致性测试。
- 使用本地开发机单点发压，试图推导线上容量。
- 没有监控、没有压测批次、没有清理策略的线上直接打压。

## 全局硬规则

- 正式性能压测必须声明 `PerformanceTestPlan`，字段见 `performance/types.md`。
- 正式性能压测应使用 `performance/performance-runner.go` 落地可复跑代码，规则见 `performance/runner.md`。
- 线上或线上等价压测必须遵守 `performance/online.md` 的授权门、就绪门、判停门和收尾门。
- 线上或线上等价压测必须先明确被测目标并确认线上服务入口可达；入口不通时停止在环境阻塞，不得继续发压。
- 计划批准后才能创建线上测试数据、连接线上 Redis / MySQL、发起线上 HTTP / WebSocket 请求或启动压测工具。
- 压测代码保留作为证据资产；线上测试数据必须按 `batch_id` 清理或记录未清理原因。
- 使用 `kubectl port-forward` 压线上服务时，只能形成 `single_source_online` 级别结论，不得单独作为线上峰值容量结论。
- 没有监控数据、压测源信息、runner 输出、业务状态抽样对账或清理结果的结果，不能作为正式性能压测结论。

## 执行路线

1. 读取目标模块或流程契约，确认要测试的模块、流程、接口或 WebSocket 入口，以及业务规则、状态不变量和通过标准。
2. 按 `performance/online.md` 和 `environment.md` 确认线上服务入口、授权范围和只读可达性探测方式。
3. 如果线上服务入口不可达，停止在环境阻塞，不创建测试数据、不落地 runner、不启动压测。
4. 按 `performance/types.md` 生成 `PerformanceTestPlan`。
5. 按 `performance/online.md` 确认命令范围、停止条件和人工旁路监控。
6. 按 `performance/runner.md` 落地 runner 代码到 `docs/agent-testing/performance-runs/<batch_id>/`。
7. 执行 smoke，证明脚本、认证、数据、监控和对账没有误打非测试数据。
8. 按计划执行 step load、peak hold 和可选 soak。
9. 每档采集 runner 输出、Prometheus / 日志 / `kubectl` 摘要和业务对账。
10. 触发停止条件时立即进入 stopping -> cleanup -> reported。
11. 清理本批次测试数据，保留 runner 代码和脱敏证据。
12. 按 `reports/README.md` 写报告。

## 压测计划字段

性能压测计划除 `templates/protocol.md` 的通用字段外，必须追加：

```text
PerformanceEnvironment：
被测目标：
线上入口：
服务可达性探测：
LoadSource：
LoadModel：
Thresholds：
StopCondition：
ObservabilityPlan：
BusinessReconcilePlan：
SubagentExecutionPlan：
runner 代码路径：
线上窗口：
命令范围：
人工监控者：
清理策略：
批准方式：
```

## 报告字段

性能压测报告除 `reports/README.md` 的通用字段外，还必须包含：

```text
压测目标：
压测环境：
线上入口可达性：
压测源：
压测模型：
通过阈值：
压测数据批次：
runner 代码路径：
每档压测结果：
最低统计指标：
资源观测：
日志观测：
瓶颈分析：
业务状态抽样对账：
停止条件触发情况：
runner 清理结果：
结论可信度：
结论：
建议优化：
```

每档压测结果建议使用表格：

| 阶段 | 时间窗口 | 目标 QPS | 实际 QPS | 并发 | 总数 | 成功 | HTTP 失败率 | 业务失败率 | 超时率 | P50 | P95 | P99 | 状态码 | 业务码 | CPU | MySQL 摘要 | Redis 摘要 | 对账 | 结论 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| smoke |  | 10 |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |  |

最低统计指标包括：目标 QPS、实际 QPS、并发、总请求数、成功数、HTTP 失败数、业务失败数、超时数、错误率、超时率、业务失败率、P50、P95、P99、最大延迟、HTTP 状态码分布和业务码分布。P50 / P95 / P99 是必要指标，但不足以单独支撑压测结论。

## 通过标准

压测结论必须明确写出：

- 当前 `PerformanceEnvironment.kind` 下的稳定承载能力。
- 达到目标流量时是否满足延迟和错误率阈值。
- 首个瓶颈和证据。
- 超过目标压力后的退化表现。
- 是否发现业务状态错误。
- 下次压测前需要修复或补充观测的事项。

结论只能使用 `performance/types.md` 定义的枚举：`passed`、`failed`、`stopped`、`inconclusive`、`skipped`。
