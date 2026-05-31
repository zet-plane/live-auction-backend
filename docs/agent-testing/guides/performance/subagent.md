# 性能压测 Subagent 编排

本指南定义性能压测计划批准后如何使用 subagent。它不替代 `guides/subagent.md`，只补充性能压测的角色、状态机和权限边界。

主 agent 必须是唯一 test lead。subagent 是 bounded executor，只执行已批准计划内的角色任务，不能自行扩大测试目标、压力边界、命令范围或数据范围。

## 推荐角色

| 角色 | 是否可并行 | 职责 | 禁止事项 |
| --- | --- | --- | --- |
| preflight agent | 可在发压前并行只读检查 | 检查线上数据库、Redis、Kubernetes、服务版本、监控和测试数据就绪 | 不得发压，不得修改线上配置 |
| monitor agent | 与 load agent 并行 | 持续查询 Prometheus、kubectl、日志和依赖指标，触发 STOP 信号 | 不得发压，不得清理数据 |
| load agent | 单实例串行 | 执行 smoke、step load、peak hold、soak | 不得超过计划压力边界，不得自行延长阶段 |
| recorder agent | 与所有阶段并行 | 记录时间线、命令、阶段结果、指标摘要和人工观察摘要 | 不得做最终结论，不得执行破坏性命令 |
| cleanup agent | 仅 stopping 后执行 | 停止临时连接、清理本批次数据、确认指标回落 | 不得清理非本批次数据，不得提前启动 |

## 状态机

编排状态机：

```text
planned
  -> approved
  -> preflight
  -> smoke
  -> step_load
  -> peak_hold
  -> soak_optional
  -> stopping
  -> cleanup
  -> reported
```

任意执行阶段收到 STOP 后必须进入：

```text
stopping -> cleanup -> reported
```

## 执行约束

- 主 agent 生成并持有唯一批准计划、`batch_id`、压力边界、命令范围和停止条件。
- `load agent` 是唯一发压者；不得多个发压 agent 同时操作同一线上目标。
- `monitor agent` 只读监控数据，发现破阈后向主 agent 发出 STOP。
- `recorder agent` 只记录证据，不做业务判断。
- `cleanup agent` 只能在主 agent 宣布 stopping 或 stopped 后执行。
- 最终结论只能由主 agent 写入报告；subagent 输出只是中间证据。

## 环境到编排的映射

| `PerformanceEnvironment.kind` | 编排要求 |
| --- | --- |
| `local_smoke` | 主 agent 执行即可，可不使用 subagent |
| `single_source_online` | 至少拆分 monitor 和 load 职责 |
| `staging_capacity` | 推荐 preflight、monitor、load、recorder、cleanup |
| `production_guarded` | 必须主 agent 控制，monitor 与 load 分离，人类旁路监控，cleanup 串行执行 |

## STOP 信号

STOP 信号可以来自：

- monitor agent 发现指标破阈。
- load agent 发现 runner 输出破阈。
- 人类监控者要求停止。
- runner `PERF_STOP_FILE` 被创建。
- 主 agent 发现业务对账失败或范围越界。

收到 STOP 后：

- load agent 必须停止继续加压。
- monitor agent 继续采集短窗口回落证据。
- recorder agent 记录停止来源、时间、阶段和触发指标。
- cleanup agent 等主 agent 进入 stopping 后执行。

## Subagent 输出要求

每个性能压测 subagent 除 `guides/subagent.md` 的输出字段外，还必须输出：

```text
角色：
关联 batch_id：
关联 runner 路径：
当前状态机阶段：
是否触发 STOP：
STOP 原因：
证据时间窗口：
敏感信息处理：
```

主 agent 汇总时必须区分子 agent 原始输出和主 agent 复核结论。
