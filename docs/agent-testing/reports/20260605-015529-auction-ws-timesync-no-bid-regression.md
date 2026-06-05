# 测试报告：auction-ws-timesync-no-bid-regression

## 基本信息

- 测试目标：用无出价广播的 300 WS + 70 QPS 对照档，判断上一轮 `time_sync` 不达标是否主要来自 `bid_success` 同流广播竞争。
- 测试类型：线上受控诊断回归，`single_source_online`。
- 测试时间：2026-06-05 01:55:29 +0800 至 01:58:29 +0800。
- 执行 agent：主 agent。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/README.md`、`guides/performance/types.md`、`guides/performance/online.md`、`guides/performance/runner.md`、`guides/environment.md`、`modules/ws.md`、`reports/README.md`。

## 测试环境

- 服务地址：线上公网 Ingress，完整地址已省略。
- 配置来源：已部署线上 backend。
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:6ed885f0`。
- MySQL：线上数据库，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- Apifox：不适用，本次不是接口契约对齐测试。
- WebSocket：线上 WebSocket 入口，完整地址和 ticket 已省略。
- 观测：Prometheus SSH tunnel + 服务器侧 `kubectl` 只读查询。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 线上真实依赖 | runner 创建并清理本批次用户、商家、房间和拍品 |
| Redis | 线上真实依赖 | WebSocket ticket、在线状态和竞拍状态依赖 Redis |
| WebSocket | 线上真实连接 | 需要验证 300 连接下 `time_sync` 客户端接收间隔 |
| 外部服务 | 未调用第三方服务 | 本次只测本系统 HTTP / WS / 观测栈 |

## 测试数据

- 测试批次 ID：`agent_perf_auction_20260605_ws_timesync_no_bid_probe`。
- 创建数据：本批次 merchant、room、item、340 个测试用户、300 条 WebSocket 连接。
- 复用数据：线上服务和观测栈；不复用真实业务用户或真实商品。

## 执行步骤

1. 只读 preflight：健康检查、Prometheus readiness、backend 镜像、Pod 资源基线。
2. 创建本批次 performance runner 资产。
3. 执行 `PERF_STAGE_QPS=70`、`PERF_STAGE_WS=300`、`PERF_REQUEST_MIX=item_only`、`PERF_USER_COUNT=340` 的 3 分钟对照档。
4. 采集 runner 阶段输出、Prometheus 摘要、业务对账和 cleanup 输出。
5. 压测后复查健康和 Pod 资源回落。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| preflight | health HTTP 200，MySQL OK，Redis OK；Prometheus ready；backend 镜像为 `6ed885f0` | 通过 |
| WS 建连 | target 300，connected 300，connect failures 0 | 通过 |
| 请求负载 | target QPS 70，actual QPS 70.00；total 12,600 | 通过 |
| 消除出价广播 | stage event counts 为 `auction_snapshot=1`、`time_sync=53,701`；Prometheus bid RPS max 0，bid broadcast RPS max 0 | 通过 |
| `time_sync` 接收间隔 | P50 999.7ms，P95 1.059s，P99 1.113s，max 1.823s | P99 达标，P95 轻微未达 |
| 服务端写出 | `ws_write_p95` max 0.976ms，`ws_time_sync_write_lag_p95` max 4.813ms，overwrite 无样本 | 通过 |
| HTTP 服务端性能 | server HTTP P95/P99 max 77.969ms / 95.594ms | 通过 |
| 业务对账 | item detail OK，ranking OK，room OK，bid attempts 0，WS connected 300 | 通过 |
| cleanup | closed WS 300，cancel item OK，end room OK，delete users attempted 341 | 通过 |
| 压测后健康 | health OK；backend 9m CPU / 37Mi，MySQL 13m / 670Mi，Redis 9m / 85Mi | 通过 |

## 对照结论

与上一轮 `core_bid_80_ranking_10_item_10` 对照：

| 指标 | 有出价广播 | 无出价广播 |
| --- | ---: | ---: |
| `bid_success` stage events | 420,764 | 0 |
| WS delivery RPS max | 2,433/s | 300/s |
| `time_sync` P50 | 988ms | 999.7ms |
| `time_sync` P95 | 1.456s | 1.059s |
| `time_sync` P99 | 1.982s | 1.113s |
| `time_sync` max | 3.550s | 1.823s |
| `ws_time_sync_write_lag_p95` | 4.528ms | 4.813ms |

主判断：`time_sync` 的主要尾延迟来自同一 WebSocket 有序流里的高频 `bid_success` 业务广播竞争，而不是 MySQL 异步、服务端 write loop 或 latest lane 代码失效。无出价广播时，P99 从 1.982s 收敛到 1.113s，已经低于 1.5s 目标；P95 仍高于 1s 约 59ms，说明还存在公网/runner 接收调度/cron 粒度带来的基线抖动。

## 通过项

- 300 WebSocket 连接全部建立成功，连接失败 0。
- 本轮没有 `bid_success`、`user_outbid` 广播，隔离变量有效。
- 服务端 `time_sync` 生成和写出健康，write lag P95 约 4.8ms。
- P99 `time_sync` 接收间隔在无出价广播时达标。
- 业务对账和 cleanup 完成，压测后健康检查正常。

## 失败项

- 严格 P95 `< 1s` 仍轻微未达：实际 P95 1.059s。

失败场景：无出价广播、300 WS、70 QPS item-only 线上公网对照档中，客户端观察到的 `time_sync` P95 仍超过 1s。

复现步骤：使用本报告记录的 runner 和环境变量复跑同一批次形态。

期望结果：`time_sync` P95 < 1s，P99 < 1.5s。

实际结果：P95 1.059s，P99 1.113s。

相关证据：runner `TIME_SYNC_P95` / `TIME_SYNC_P99` 输出；Prometheus `ws_time_sync_write_lag_p95` max 4.813ms。

可能原因：公网链路、客户端 goroutine 调度、runner 以接收/解析时间统计间隔，以及 `@every 1s` cron 本身的秒级边界抖动。

影响范围：严格 1s P95 验收仍需谨慎；但高广播场景的 P99 大幅超标已被确认主要由同流业务广播放大。

建议修复点：若业务要求端到端 P95 严格小于 1s，应考虑独立 `time_sync` 通道、降低业务广播量、或改为 payload server timestamp + 客户端容忍窗口的验收口径。

建议新增的回归测试：保留本次 `item_only` 对照档，并新增同机/内网压测源档，用于区分公网与客户端侧调度抖动。

## 跳过项

- 未执行服务器本机或 k8s Job 压测源：本次先做最小公网对照，避免扩大线上执行范围。
- 未直接查询线上 MySQL / Redis 数据内容：runner 已完成 HTTP/WS 对账和本批次 cleanup，本次不需要读取敏感连接信息。

## Apifox 对齐偏差

- 不适用。

## 风险和建议

- 若继续用单条 WS 同时承载 `bid_success`、`user_outbid`、`time_sync`，应用层 priority 不能抢占已经进入 TCP / ingress / 客户端缓冲区的旧消息。
- 对高价值竞拍倒计时，建议把 `time_sync` 拆到独立连接或轻量通道，或者降低 `bid_success` fanout 频率并改用聚合快照。
- runner 当前统计的是客户端收到并解析 `time_sync` 的间隔，不是服务端 payload 时间间隔。建议增加基于 `server_time_unix_ms` 的生成间隔和 arrival delay 指标。

## 建议沉淀的回归测试

- `300 WS + 70 QPS + item_only`：作为 time_sync 基线抖动回归。
- `300 WS + 70 QPS + core_bid_80_ranking_10_item_10`：作为同流广播压力回归。
- 内网压测源同配置复跑：用于隔离公网链路噪声。

## 已知缺口

- 本轮只能证明“去掉出价广播后尾延迟显著下降”，不能单独证明正式线上容量上限。
- 未覆盖多房间同时竞拍时的跨房间广播竞争。
- 未覆盖客户端真实浏览器渲染线程对 `time_sync` 处理的影响。

## 测试数据清理结果

- 线上依赖使用情况：已使用，地址和凭据已省略。
- 测试数据范围：仅 `agent_perf_auction_20260605_ws_timesync_no_bid_probe` 本批次创建的数据。
- 清理方式：runner cleanup 关闭 WS，取消测试拍品，结束测试房间，按本批次测试用户尝试软删除。
- 清理结果：`closed_ws=300 cancel_item=ok end_room=ok delete_users_attempted=341`。
- 未清理原因：无已知未清理项。
