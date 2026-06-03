# 测试报告：auction-core-bid-performance

## 基本信息

- 测试目标：核心出价写链路 + 排行榜读取 + 商品详情/当前价读取，并维持同房间 WebSocket 连接观测同步广播和每秒 `time_sync`。
- 测试类型：性能压测，`single_source_online`。
- 测试时间：2026-06-03 15:26:01 +0800 至 15:46:56 +0800。
- 执行 agent：Codex 主 agent。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/*`、`guides/environment.md`、`modules/bid.md`、`modules/ws.md`、`modules/item.md`、`modules/room.md`、`reports/README.md`。

## 测试环境

- 服务地址：线上入口，完整地址已省略。
- 配置来源：线上运行配置，敏感内容已省略。
- MySQL：线上 MySQL，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- WebSocket：线上 WebSocket，同房间连接，ticket 值已省略。
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:ba7098c5`。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 真实线上依赖 | 验证同步写 `bid_logs` 和真实查询压力 |
| Redis | 真实线上依赖 | 验证 Redis Lua、ranking、auction state 和 WS ticket |
| WebSocket | 真实线上连接 | 验证出价广播和 `time_sync` 推送 |
| 外部服务 | 未调用第三方支付/短信/物流 | 保持测试范围在竞拍核心链路 |

## 测试数据

- 测试批次 ID：`agent_perf_auction_20260603_core_bid_ws`
- 创建数据：1 个测试商家、1 个测试房间、1 个测试拍品、320 个测试用户。
- 复用数据：无业务数据复用；仅复用线上服务和依赖。

## 执行步骤

1. 本地验证 runner：`rtk env GOCACHE=/tmp/live-auction-go-cache go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws`。
2. 只读 preflight：公共健康检查、后端镜像、Pod/Service/Deployment、`kubectl top`、restart count。
3. 建立 Prometheus 临时 SSH tunnel，仅用于只读查询。
4. 运行 runner：HTTP mix 为 bids 80%、ranking 10%、item detail 10%；WS 目标为 20/60/100/140/200/260/300。
5. 每档采集 runner 输出、Prometheus 摘要、kubectl top 和日志摘要。
6. 100 QPS / 200 WS 阶段触发 `time_sync_missing_or_low_rate` 判停。
7. 执行 runner cleanup，并关闭 Prometheus tunnel。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| runner 本地逻辑 | Go test 通过，耗时 0.548s | 通过 |
| preflight | 公共健康检查曾返回 200；后端镜像 `ba7098c5`；核心 Pod Running | 通过，runner 内 health 曾 10s 超时 |
| 50 QPS | 实际 49.99 QPS，100 WS，服务端 P99 max 24ms，bid RPS max 40.13，DB ops max 168.09 | 通过 |
| 70 QPS | 实际 67.01 QPS，140 WS，服务端 P99 max 41ms，`time_sync` P95 2.193s | 有 WS 抖动 |
| 100 QPS | 实际 88.34 QPS，200 WS，`time_sync` P95 4.602s，P99 13.248s，ws_active last 42 | 触发判停 |
| 资源回落 | 后测 backend 5m CPU / 52Mi，MySQL 14m / 558Mi，Redis 8m / 16Mi，restart 0 | 回落正常 |
| 清理 | `closed_ws=200 cancel_item=ok end_room=ok delete_users_attempted=321` | 完成，用户删除为 attempted 级证据 |

## 通过项

- 10、30、50 QPS 阶段完成，HTTP 错误率均低于 1%。
- 50 QPS / 100 WS 下服务端 HTTP P99 仍低，最大约 24ms。
- 后端 Pod 未重启，未观察到 OOM/panic/fatal。
- Redis Lua 和 bid RPS 可随 50、70、100 QPS 阶段增长，100 QPS 阶段 bid RPS max 80。

## 失败项

- 100 QPS / 200 WS 阶段触发 `time_sync_missing_or_low_rate`。
- 100 QPS 阶段实际 QPS 仅 88.34，HTTP timeout rate 1.44%，超过目标 `<1%`。
- 100 QPS 阶段 `time_sync` P95 4.602s、P99 13.248s，超过计划阈值。
- Prometheus `ws_active` 在 100 QPS 阶段 max 201、last 42，说明 WS 连接保持或指标侧活跃连接出现明显下滑。

## 跳过项

- 130 QPS / 260 WS、150 QPS / 300 WS 未执行：100 QPS 阶段已触发停止条件。
- peak hold / soak 未执行：本轮计划明确不执行。

## Apifox 对齐偏差

- 不适用，本次不是接口契约测试。

## 风险和建议

- 首个明确瓶颈方向是 WebSocket 推送链路，尤其是 `time_sync` 周期推送和连接保持，而不是服务端 HTTP handler 本身。
- 50 至 100 QPS 期间服务端 HTTP P99 仍较低，但客户端端到端 P99、timeout 和 `time_sync` 抖动持续上升，建议优先检查 WS Hub 写循环、连接 send buffer、慢连接剔除、`time_sync` 广播调度和反压策略。
- `time_sync` 是每秒全连接广播，和出价事件叠加后可能造成 WS 写队列拥塞；建议将 `time_sync` 与业务事件分离观测，记录 per-room fanout duration、dropped/slow connection count、send channel occupancy。
- runner cleanup 对用户删除只输出 attempted，建议增强 runner 记录删除成功/失败计数。

## 建议沉淀的回归测试

- WS Hub 压力测试：固定 200/300 同房间连接，单独测 `time_sync` P95/P99 间隔。
- 出价广播压力测试：固定 100 QPS 出价 + 200 WS，统计 `bid_success` 投递延迟和慢连接剔除。
- 服务端指标补充测试：为 WS fanout duration、send queue、dropped slow connection、time_sync tick duration 添加指标并压测验证。

## 已知缺口

- smoke 和 30 QPS 阶段 Prometheus query_range 不稳定，主要用 runner 和 kubectl 证据支撑。
- 未直接查询 MySQL/Redis 数据库内部记录，业务对账使用 HTTP 和 runner 事件摘要。
- 用户删除只有 attempted 证据，没有逐个删除成功计数。

## 测试数据清理结果

- 测试批次 ID：`agent_perf_auction_20260603_core_bid_ws`
- 创建的数据：测试商家、测试房间、测试拍品、320 个测试用户、出价日志、Redis auction/ws key。
- 清理方式：runner 调用业务接口关闭 WS、取消拍品、下播房间、删除测试用户。
- 清理结果：`closed_ws=200 cancel_item=ok end_room=ok delete_users_attempted=321`。
- 未清理原因：用户删除缺少逐个成功计数；如需强确认，后续应补充只读批次查询或 runner 删除结果统计。
