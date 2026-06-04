# 测试报告：auction-http-bid-performance

## 基本信息

- 测试目标：在禁用 WebSocket 的情况下，使用 `bid_only` 请求模型探测核心出价 HTTP 写链路、Redis Lua 和同步 `bid_logs` 落库在单压测源下的瓶颈拐点。
- 测试类型：性能压测，`single_source_online`。
- 测试时间：2026-06-04 14:11:43 +0800 至 14:23:37 +0800，清理和资源回落确认持续到压测停止后。
- 执行 agent：Codex 主 agent。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/README.md`、`guides/performance/types.md`、`guides/performance/online.md`、`guides/performance/runner.md`、`guides/environment.md`、`modules/bid.md`、`modules/ws.md`、`modules/item.md`、`modules/room.md`、`reports/README.md`、`skills/live-auction-online-ops/SKILL.md`。

## 测试环境

- 服务地址：线上入口，完整地址已省略。
- 配置来源：线上运行配置，敏感内容已省略。
- MySQL：线上 MySQL，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- Apifox：不适用，本次不是接口契约测试。
- WebSocket：禁用，`PERF_DISABLE_WS=true`。
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:d78d1a66`。
- 观测：Prometheus 临时 SSH tunnel，只读查询；测试结束后已关闭。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 真实线上依赖，服务间接写入 `bid_logs` | 验证同步落库在高出价写入下的压力 |
| Redis | 真实线上依赖，服务间接执行出价 Lua 和 ranking 更新 | 验证原子出价状态更新和竞争拒绝 |
| WebSocket | 禁用 | 隔离 HTTP/Redis/MySQL 写链路，不混入 WS fanout 放大 |
| 外部服务 | 未调用第三方支付/短信/物流 | 保持测试范围在竞拍核心链路 |

## 测试数据

- 测试批次 ID：`agent_perf_auction_20260604_bottleneck_probe_http`
- 创建数据：1 个测试商家、1 个测试房间、1 个测试拍品、320 个测试用户。
- 复用数据：无业务数据复用；仅复用线上服务和依赖。

## 执行步骤

1. 本地验证 runner 和相关单元测试：`go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws ./internal/app/ws/hub ./internal/app/item/service`。
2. 只读 preflight：线上健康检查、后端镜像、Pod/Service/Deployment、`kubectl top`、backend restart。
3. 建立 Prometheus 临时 SSH tunnel，仅用于只读指标查询。
4. 运行 runner：`PERF_REQUEST_MIX=bid_only`，`PERF_DISABLE_WS=true`，阶段为 150/200/300/500 QPS，每档计划 3 分钟。
5. 用户在 500 QPS 阶段收到 Prometheus `HighP95Latency` 告警，响应时间 P95 849.6ms 超过 500ms，发出人工停止信号。
6. 写入 STOP 文件，runner 在 500 QPS 阶段提前停止，进入 reconcile 和 cleanup。
7. 关闭 Prometheus 临时 tunnel，确认线上资源回落和 backend restart 仍为 0。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| runner 本地逻辑 | Go test 通过，使用 `/tmp/live-auction-go-cache` | 通过 |
| preflight | 健康检查 200；后端镜像 `d78d1a66`；核心 Pod Running；backend restart 0；backend 约 3m CPU / 73Mi | 通过 |
| 150 QPS | 实际 149.91 QPS；server P95 max 163.6ms，P99 max 232.7ms；DB ops max 574.2/s；timeout rate 0.16% | 通过 |
| 200 QPS | 实际 193.90 QPS；server P95 max 427.7ms，P99 max 737.2ms；DB ops max 732.8/s；timeout rate 0.46% | 有压力抬升 |
| 300 QPS | 实际 298.93 QPS；server P95 max 451.7ms，P99 max 902.2ms；DB ops max 1078.9/s；timeout rate 0.20% | 通过但接近延迟阈值 |
| 500 QPS | 实际 485.90 QPS；server P95 max 849.6ms，P99 max 1.479s；DB ops max 1617.8/s；STOP 来源为人工停止 | 停止 |
| 告警 | Prometheus `HighP95Latency`，开始时间 2026-06-04T06:22:22.231Z，P95 849.6ms 超过 500ms | 与 runner 500 QPS 阶段一致 |
| 高压资源样本 | backend 约 537m CPU / 72Mi，MySQL 417m / 669Mi，Redis 110m / 98Mi | 压力显著抬升 |
| 回落资源样本 | backend 约 7m CPU / 75Mi，MySQL 21m / 669Mi，Redis 8m / 99Mi | 回落正常 |
| 后端稳定性 | backend Pod Running，restart 0 | 通过 |
| 清理 | `closed_ws=0 cancel_item=ok end_room=ok delete_users_attempted=321` | 完成，用户删除为 attempted 级证据 |

## 通过项

- 150 QPS 和 300 QPS 阶段实际 QPS 接近目标，系统错误率低于 1%。
- 500 QPS 阶段没有 backend restart、panic 或 OOM 证据。
- 禁用 WS 后，旧的 WS fanout / time_sync 问题不参与本轮结果，能更清楚观察 HTTP 写链路。
- 停止后资源回落正常，测试数据 cleanup 完成到 runner 现有证据粒度。

## 失败项

- 500 QPS 阶段触发人工停止：Prometheus `HighP95Latency` 告警显示 P95 849.6ms 超过 500ms。
- 500 QPS 阶段服务端 P99 max 达到 1.479s，说明单源 bid-only 写链路在 500 QPS 附近进入明显延迟拐点。

## 跳过项

- WebSocket 150 QPS / 300 WS 探针未执行：HTTP bid-only 已在 500 QPS 触发人工停止，本轮按停止门收敛，不继续扩大压测范围。
- peak hold / soak 未执行：本轮目标是瓶颈探针，不形成正式容量上限。
- MySQL/Redis 内部慢查询和命令级详情未直接查询：本轮证据来自 runner、Prometheus、kubectl 和资源样本。

## Apifox 对齐偏差

不适用，本次不是接口契约测试。

## 风险和建议

- 当前可定位的 HTTP 写链路拐点在 300 QPS 到 500 QPS 之间；500 QPS 时 server P95 849.6ms，P99 1.479s，DB ops 约 1618/s。
- `PlaceBid` 仍同步写 `bid_logs`，高 QPS 下 DB ops 随成功出价和查询链路明显抬升；下一步应重点拆分 Redis Lua、MySQL `CreateBidLog`、用户/商品查询、HTTP middleware 各段耗时。
- 建议优先做代码侧分段指标或 trace：`FindItemWithRule`、`PlaceBidLua`、`CreateBidLog`、broadcast enqueue、handler total。
- 若业务允许，后续可以评估 `bid_logs` 异步落库或批量写入，但必须先明确 Redis 成功而 MySQL 延迟/失败时的业务一致性语义。

## 建议沉淀的回归测试

- 保留 bid-only 300 QPS 短探针作为 HTTP 写链路回归门：server P95 < 500ms、timeout < 1%、backend restart 0。
- 增加 500 QPS guarded probe 的只读观测版本，用于验证优化前后 P95/P99 和 DB ops 是否下降。
- 增加服务端分段指标测试，确认首个耗时段是 MySQL 写入、前置查询、Redis Lua 还是入口层。

## 已知缺口

- 单压测源结果只能用于瓶颈定位，不能作为正式线上容量上限。
- runner 用户删除只记录 attempted，未逐个统计删除成功/失败。
- 没有执行 WS 高连接数探针，因此本报告只覆盖禁用 WS 的 HTTP bid-only 写链路。

## 测试数据清理结果

- 线上依赖使用情况：已使用，地址和凭据已省略。
- 测试数据范围：仅 `agent_perf_auction_20260604_bottleneck_probe_http` 批次。
- 清理方式：runner 调用业务接口取消拍品、下播房间、删除测试用户；本轮没有创建 WS 连接。
- 清理结果：`closed_ws=0 cancel_item=ok end_room=ok delete_users_attempted=321`。
- 未清理原因：用户删除缺少逐个成功计数；如需强确认，后续应补充 runner 删除成功/失败统计或只读批次查询。
