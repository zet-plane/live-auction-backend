# 测试报告：auction-ws-write-probe

## 基本信息

- 测试目标：复跑核心出价链路在当前镜像下的 WebSocket 分发压力，验证 100 QPS / 200 WS 下 `time_sync`、WS 写出、send queue 和连接保持是否仍是瓶颈。
- 测试类型：性能压测，`single_source_online`。
- 测试时间：2026-06-03 22:06:20 +0800 至 22:16:06 +0800，清理和补证据持续到 23:13 后。
- 执行 agent：Codex 主 agent。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/*`、`guides/environment.md`、`modules/ws.md`、`modules/bid.md`、`reports/README.md`、`skills/live-auction-online-ops/SKILL.md`。

## 测试环境

- 服务地址：线上入口，完整地址已省略。
- 配置来源：线上运行配置，敏感内容已省略。
- MySQL：线上 MySQL，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- WebSocket：线上 WebSocket，同房间连接，ticket 值已省略。
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:d78d1a66`。
- 观测：Prometheus 临时 SSH tunnel；Tempo 当前 CrashLoop，不作为本轮必要证据。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 真实线上依赖 | 验证同步写 `bid_logs` 和真实查询压力 |
| Redis | 真实线上依赖 | 验证 Redis Lua、ranking、auction state 和 WS ticket |
| WebSocket | 真实线上连接 | 验证出价广播、单播、`time_sync` 和连接保持 |
| 外部服务 | 未调用第三方支付/短信/物流 | 保持测试范围在竞拍核心链路 |

## 测试数据

- 测试批次 ID：`agent_perf_auction_20260603_ws_write_probe`
- 创建数据：1 个测试商家、1 个测试房间、1 个测试拍品、320 个测试用户。
- 复用数据：无业务数据复用；仅复用线上服务和依赖。

## 执行步骤

1. 本地验证 runner：`rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws`。
2. 只读 preflight：线上健康检查、Prometheus readiness、后端镜像、Pod/Service/Deployment、`kubectl top`、restart count。
3. 建立 Prometheus 临时 SSH tunnel，仅用于只读查询。
4. 运行 runner：HTTP mix 为 bids 80%、ranking 10%、item detail 10%；阶段为 10 QPS / 20 WS、70 QPS / 140 WS、100 QPS / 200 WS。
5. 每档采集 runner 输出和 Prometheus 摘要。
6. 100 QPS 阶段后补采 `ws.write`、`ws.delivery`、`ws.send_queue.depth`、`ws.connection` 细项指标。
7. 执行 runner cleanup，并关闭临时观测通道。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| runner 本地逻辑 | Go test 通过，使用 `/tmp/live-auction-go-cache` | 通过 |
| preflight | 健康检查 200；Prometheus ready；后端镜像 `d78d1a66`；核心 Pod Running；backend restart 0 | 通过 |
| 10 QPS | 实际 10.00 QPS，20 WS，`time_sync` P95 1.425s，server HTTP P99 max 99.5ms | 通过 |
| 70 QPS | 实际 70.00 QPS，140 WS，`time_sync` P95 1.430s，P99 1.818s，`ws_active` last/max 141 | 通过 |
| 100 QPS | 实际 99.97 QPS，200 WS，`time_sync` count 35976，P95 1.432s，P99 1.832s，`ws_active` last/max 201 | 通过 |
| 100 QPS 服务端 | server HTTP P95 max 9.146ms，P99 max 17.498ms，bid RPS max 80.07，DB ops max 331.96 | 通过 |
| 100 QPS WS 写出 | `ws_write` P95 max 0.966ms，P99 max 1.850ms，success RPS max 1934.04，failed RPS max 0 | 通过 |
| 100 QPS WS 队列/丢弃 | `ws_send_queue_depth` P95/P99 max 0，`ws_delivery_dropped` 无有效序列，connection close RPS max 0 | 通过 |
| 100 QPS 事件拆分 | `bid_success` broadcast max 8.44/s，delivery/write max 1688.89/s；`time_sync` write 200/s；`user_outbid` write max 45.16/s | 通过 |
| 资源回落 | 后测 backend 2m CPU / 71Mi，MySQL 10m / 667Mi，Redis 8m / 79Mi，backend restart 0 | 通过 |
| 日志摘要 | 后端日志尾部未见 panic/OOM/fatal，压测中仅有预期 `40003 price too low` 竞争拒绝 | 通过 |
| 清理 | `closed_ws=200 cancel_item=ok end_room=ok delete_users_attempted=321` | 完成，用户删除为 attempted 级证据 |

## 通过项

- 当前镜像在 100 QPS / 200 WS 下没有复现旧报告的 `time_sync_missing_or_low_rate`。
- 100 QPS 阶段实际 QPS 达到 99.97，HTTP timeout rate 0.19%，低于 1%。
- `time_sync` 接收数量接近理论值 36000，P95/P99 明显低于 3s 判停线。
- `ws_connection_active` 没有从 200 级别下滑，阶段内 last/max 均为 201。
- WS 写出耗时低且无失败，send queue 未出现积压，未观察到 delivery drop。
- 后端、MySQL、Redis 资源未打满，压测后回落正常。

## 失败项

无。

## 跳过项

- 130 QPS / 260 WS、150 QPS / 300 WS 未执行：本轮目标是复核旧故障点 100 QPS / 200 WS，并补采写出指标，不做更高容量探顶。
- peak hold / soak 未执行：本轮为定点瓶颈诊断，不形成正式容量上限结论。

## Apifox 对齐偏差

不适用，本次不是接口契约测试。

## 风险和建议

- 旧报告 `ba7098c5` 的瓶颈结论仍成立：当时瓶颈主要在 WS 分发放大，而不是 HTTP handler、Redis Lua 或 MySQL。
- 当前 `d78d1a66` 已显著缓解旧故障：`bid_success` fanout 被合并到约 8.4/s，100 QPS / 200 WS 下没有写队列积压、写失败或连接下滑。
- 当前剩余风险不是 100 QPS / 200 WS 下的写循环，而是更高 WS 数、更高 QPS、多房间同时竞拍和长时间 soak 下的广播放大。
- `time_sync` 最大间隔仍偶发到 20s 级别，但 P95/P99 稳定；建议后续按连接维度记录最大间隔来源，区分单连接建连/调度抖动与系统性拥塞。

## 建议沉淀的回归测试

- 保留 100 QPS / 200 WS 的 `time_sync` P95/P99、`ws_active`、`ws_write_failed`、`ws_send_queue_depth` 指标作为 WS 性能回归门。
- 增加 150 QPS / 300 WS 或多房间同压的单独 guarded probe，用于验证合并策略的上限。
- 增加长时间 soak，观察 `time_sync` 最大间隔、连接 churn 和用户删除 cleanup 成功/失败计数。

## 已知缺口

- 用户删除只有 attempted 证据，没有逐个删除成功计数。
- 本轮使用单压测源和线上入口，只能形成 `single_source_online` 诊断结论，不作为正式容量上限。
- Tempo 当前 CrashLoop，未使用 trace 证据；本轮结论依赖 runner、Prometheus、kubectl 和日志摘要。

## 测试数据清理结果

- 测试批次 ID：`agent_perf_auction_20260603_ws_write_probe`
- 创建的数据：测试商家、测试房间、测试拍品、320 个测试用户、出价日志、Redis auction/ws key。
- 清理方式：runner 调用业务接口关闭 WS、取消拍品、下播房间、删除测试用户。
- 清理结果：`closed_ws=200 cancel_item=ok end_room=ok delete_users_attempted=321`。
- 未清理原因：用户删除缺少逐个成功计数；如需强确认，后续应补充只读批次查询或 runner 删除结果统计。
