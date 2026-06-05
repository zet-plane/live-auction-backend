# 测试报告：auction ws connection stability

## 基本信息

- 测试目标：验证 split-stream WebSocket 在 immediate、jittered、priority_jittered 升级模式下的线上建连稳定性、连接尾延迟、控制流到达尾延迟和服务端写入延迟。
- 测试类型：线上受控性能回归矩阵，`single_source_online`。
- 测试时间：2026-06-05 20:30-21:23 CST。
- 执行 agent：Codex 主 agent；本次线上矩阵未使用 subagent。
- 读取文档：`docs/agent-testing/README.md`、`docs/agent-testing/templates/protocol.md`、`docs/agent-testing/guides/runner.md`、`docs/agent-testing/guides/environment.md`、`docs/agent-testing/guides/performance/online.md`、`docs/agent-testing/guides/performance/runner.md`、`docs/superpowers/plans/2026-06-05-ws-connection-stability.md`。
- 测试环境：线上服务入口，完整地址已省略；Prometheus 通过临时 SSH + `kubectl port-forward` 本地隧道只读查询。
- 依赖策略：连接线上 HTTP、线上 WebSocket、线上 Prometheus 和 read-only kubectl；只操作本次 runner batch 创建的数据。
- 线上地址脱敏说明：报告不记录真实线上地址、SSH 目标、token、DSN、Redis 凭据、WebSocket ticket 或完整 WebSocket query string。

## 矩阵结果

| Batch | Upgrade mode | Mix | QPS | Physical WS | EOF / connect error | Connect P95 | Connect P99 | Arrival P95 | Arrival P99 | Interval P95 | Interval P99 | Server write lag P95 |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `agent_ws_conn_stability_20260605_immediate` | `immediate` | bid/ranking/item | 70 | 600 | `dial:EOF=46` | 5.291s | 12.196s | 851.406ms | 1.970s | 1.478s | 2.041s | max 4.676ms |
| `agent_ws_conn_stability_20260605_jittered` | `jittered` | bid/ranking/item | 70 | 600 | `dial:EOF=1` | 2.719s | 3.665s | 1.035s | 1.663s | 1.656s | 2.198s | max 4.813ms |
| `agent_ws_conn_stability_20260605_priority_jittered` | `priority_jittered` | bid/ranking/item | 70 | 600 | 0 | 2.594s | 3.233s | 15.403s | 30.487s | 1.784s | 2.669s | max 4.822ms |
| `agent_ws_conn_stability_20260605_item_only_jittered` | `jittered` | item only | 70 | 600 | `dial_status:502=1` | 2.509s | 3.197s | 572.207ms | 1.051s | 1.382s | 1.739s | max 4.827ms |

## 日志和资源复核

- backend image 摘要：`ghcr.io/zet-plane/live-auction-backend:916015f0`。
- backend pod：Ready/Running，restart count 0。
- 严格日志 marker 计数：`panic|fatal|oom|killed` 为 0。
- 最终节点资源快照：约 3% CPU、64% memory。
- 最终 pod 资源快照：backend 约 7m CPU / 58Mi memory；MySQL 约 9m CPU / 673Mi；Redis 约 10m CPU / 83Mi。
- Prometheus backend restarts：四组矩阵均 max 0。

## 通过项

- 四组 batch 均完成 `PERF_PLAN`、`PREFLIGHT`、`STAGE`、`OBSERVABILITY`、`RECONCILE`、`CLEANUP` 和 `SUMMARY`。
- 四组 batch 均建立 600 条 physical WS：control 300，market 300。
- `jittered` 相比 `immediate` 明显降低连接失败和连接尾延迟：`dial:EOF` 从 46 降到 1，Connect P95 从 5.291s 降到 2.719s，Connect P99 从 12.196s 降到 3.665s。
- `priority_jittered` 连接稳定性进一步改善：connect error 为 0，Connect P95 2.594s，Connect P99 3.233s。
- item-only jittered 控制组显示无 bid fanout 时连接尾延迟仍保持在约 2.5s / 3.2s，控制流到达 P95/P99 低于 bid mix。
- 服务端 `ws_time_sync_write_lag_p95` 四组均低于 5ms，未接近 20ms 风险线。
- backend restart 为 0，严格错误日志 marker 为 0。

## 失败项

- 无业务失败项。
- 观察项：`priority_jittered` bid mix 的 `CONTROL_TIME_SYNC_ARRIVAL_DELAY_P95/P99` 为 15.403s / 30.487s，明显高于 `jittered` 和 item-only 控制组；连接指标更好但控制到达尾延迟异常，需要后续复核是否与 bid fanout、采样窗口、优先批次顺序或单次线上噪声有关。
- 观察项：四组均有少量 HTTP timeout，错误率 0.25%-0.55%；本次目标聚焦 WS 建连，不作为业务失败归因。

## 诊断结论

- 当前证据支持先采用 runner-side/client-side 连接平滑策略验证：jittered split upgrade 显著降低 hot-room 600 physical WS 建连的 `dial:EOF` 和 Connect P95/P99。
- 服务端写入链路不是主要瓶颈：`ws_time_sync_write_lag_p95` 稳定在约 4.7-4.8ms，backend 无 restart，资源快照无明显 CPU/内存压力。
- item-only jittered 控制组说明，去掉 bid fanout 后控制流到达 P95/P99 更低；bid fanout 仍可能放大控制到达尾延迟，但不是 immediate 建连 `dial:EOF` 的主因。
- `priority_jittered` 的连接结果最好，但控制到达尾延迟异常，不建议仅凭本次单样本直接推广为默认策略。

## 下一步

- 建议把 `jittered` 作为下一轮客户端/runner 行为验证基线，继续对比不同 batch size 和 interval。
- `priority_jittered` 需要复跑或增加更精确的优先用户语义后再判断。
- 暂不启动 Task 7 后端 lifecycle metrics 作为功能改动；如需要 rollout 监控，可先补轻量 WS lifecycle metrics，但本次证据并未显示服务端写路径是瓶颈。

## 测试数据清理结果

| Batch | Cleanup |
| --- | --- |
| `agent_ws_conn_stability_20260605_immediate` | closed_ws=600, cancel_item=ok, end_room=ok, delete_users_attempted=341 |
| `agent_ws_conn_stability_20260605_jittered` | closed_ws=600, cancel_item=ok, end_room=ok, delete_users_attempted=341 |
| `agent_ws_conn_stability_20260605_priority_jittered` | closed_ws=600, cancel_item=ok, end_room=ok, delete_users_attempted=341 |
| `agent_ws_conn_stability_20260605_item_only_jittered` | closed_ws=600, cancel_item=ok, end_room=ok, delete_users_attempted=341 |

清理 caveat：runner 记录 delete user attempts，不逐个记录 delete success count；未发现 cleanup 命令报错。

## 跳过项和已知缺口

- 未执行 Task 7 后端 metrics，因为本次矩阵已能说明连接平滑改善建连稳定性，且服务端写入延迟和资源压力未显示为主要瓶颈。
- 未做多次重复样本；`priority_jittered` 控制到达尾延迟异常需要复跑确认。
- 未写入 Apifox 对齐偏差；本次不是接口契约测试。
