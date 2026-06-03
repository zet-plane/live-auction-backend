# 测试报告：auction performance

## 基本信息

- 测试目标：基于 `performance-plan.md` 执行线上直播竞拍混合链路压测，目标阶段为 60/70/80/90/100 QPS，WebSocket 模型为 `160 users / 160 WS`
- 测试类型：性能压测，`single_source_online`
- 测试时间：2026-06-02 23:27-23:51 Asia/Shanghai
- 执行 agent：Codex 主 agent
- 主 agent：Codex
- 子 agent：monitor/preflight、recorder
- 子 agent 结果摘要：recorder 已提供报告字段和阶段表模板；monitor 在主 agent 汇总结果后由主 agent复核
- 主 agent 复核结论：本次结果为 `stopped`；60 QPS 阶段 P99 超过 2s 且实际 QPS 未达到目标
- 冲突和处理：无证据冲突；runner 顺序建连导致时间线可信度下降，已列为缺口
- Subagent cleanup：recorder、monitor 均已完成并关闭
- 并行数据隔离证明：不适用；单批次 `agent_perf_auction_20260602_qps60_100`
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/README.md`、`types.md`、`online.md`、`runner.md`、`subagent.md`、`guides/environment.md`、`modules/bid.md`、`modules/ws.md`、`modules/item.md`、`modules/room.md`、`reports/README.md`

## 测试环境

- 服务地址：线上公网 Ingress，完整地址已省略
- 配置来源：线上 k3s deployment
- MySQL：线上 MySQL，地址和凭据已省略
- Redis：线上 Redis，地址和凭据已省略
- Apifox：未执行接口规范对齐，本次为性能压测
- WebSocket：公网 WebSocket 入口，完整 query 和 ticket 已省略
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:91c9a696`

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 线上依赖，批次数据 | 验证真实房间、拍品、用户、出价日志写路径 |
| Redis | 线上依赖，批次 key | 验证真实竞拍 state、ranking、WS ticket 和在线状态 |
| WebSocket | 真实公网连接 | 验证 `160 users / 160 WS` 建连和房间扇出压力 |
| 外部服务 | 未使用 | 不涉及支付、短信、物流等第三方 |

## 测试数据

- 测试批次 ID：`agent_perf_auction_20260602_qps60_100`
- 创建数据：1 个测试商家、1 个测试房间、1 个测试拍品、160 个测试用户、160 条峰值 WebSocket 连接、1290 次出价尝试
- 复用数据：无业务数据复用

## 执行步骤

1. 执行 runner 本地单元测试，通过。
2. 线上 preflight：health HTTP 200，MySQL ok，Redis ok；后端 rollout 成功；核心 Pod Running；后端 restart count 为 0。
3. 落地 runner：`docs/agent-testing/performance-runs/agent_perf_auction_20260602_qps60_100/main.go`。
4. 在沙箱内首次执行 runner 时 DNS 不可用，未创建数据；随后按批准在沙箱外执行。
5. 执行 smoke：10 QPS / 20 WS / 1 分钟。
6. 执行 60 QPS / 160 WS / 3 分钟；触发 `p99_gt_2s` 停止条件。
7. 跳过 70/80/90/100 QPS，进入对账和 cleanup。
8. 收尾采集资源、restart 和日志摘要。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 线上入口可达 | health HTTP 200，MySQL ok，Redis ok | 通过 |
| preflight | node Ready；backend rollout success；image `91c9a696`；backend restart count 0 | 通过 |
| smoke | actual 9.98 QPS，20/20 WS，P95 865ms，P99 1.413s，系统错误率 0.50% | 通过 |
| 60 QPS | actual 45.08 QPS，160/160 WS，P95 2.600s，P99 3.660s，系统错误率 1.21% | 停止 |
| 业务对账 | item detail、ranking、room detail 均 HTTP 200；bid_attempts=1290 | 通过 |
| 资源水位 | setup 采样 backend 约 129m CPU / 40Mi；后续采样较低；post cleanup node 约 3% CPU / 46% 内存 | 未见资源打满 |
| 稳定性 | backend restart count 0；采样日志未见 panic/OOM/fatal | 未见崩溃 |
| 清理 | `closed_ws=160 cancel_item=ok end_room=ok delete_users_attempted=161` | 已执行 |

## 通过项

- smoke 阶段完成，脚本、认证、测试数据、WS 建连和基础对账可用。
- `160 users / 160 WS` 在 60 QPS 阶段建立成功，WS connect fails 为 3，但最终 connected 为 160。
- 停止后业务抽样接口仍可用，cleanup 成功。

## 失败项

- 60 QPS 阶段触发停止条件：P99 3.660s，高于 2s 阈值。
- 60 QPS 阶段实际吞吐只有 45.08 QPS，未达到目标 60 QPS。

### 失败场景

60 QPS / 160 WS 混合链路压测。

### 复现步骤

使用本批次 runner，以线上公网入口、`160 users / 160 WS` 执行 `step_60qps_160ws`。

### 期望结果

实际 QPS 接近 60，P99 不超过 2s，错误率和超时率不超过 3%。

### 实际结果

实际 QPS 45.08，P99 3.660s，Max 7.095s，触发 `p99_gt_2s` 停止。

### 相关证据

`docs/agent-testing/performance-runs/agent_perf_auction_20260602_qps60_100/evidence-redacted.md`

### 可能原因

公网单源、runner 顺序 WS 建连、客户端压测源调度能力、服务端路由延迟、同步 BidLog 写入或 Redis/DB 操作延迟都可能参与；当前采样未看到 Pod CPU/内存打满。

### 影响范围

不能证明当前线上环境稳定承载 60 QPS / 160 WS，更不能继续推导 70-100 QPS。

### 建议修复点

将 runner 的 WS 建连改为并行带超时；补 Prometheus route latency、Redis Lua duration、DB operation duration 和 WS broadcast duration 时间线。

### 建议新增的回归测试

新增性能 runner 本地测试，验证 WS 建连不会无限重试，且 stage 计时不包含超长建连等待。

## 跳过项

- 70/80/90/100 QPS：因 60 QPS 阶段触发 P99 停止条件跳过。后续补测条件是修复 runner 建连口径并确认 60 QPS 稳定。
- peak hold / soak：不在本次批准阶段内，且 60 QPS 未通过。
- 精细 Prometheus / Loki / Tempo 时间线：本次只做 `kubectl top`、日志采样和 runner 证据，瓶颈归因不足。

## Apifox 对齐偏差

- 不适用，本次不是接口契约测试。

## 风险和建议

- 本次结果应标记为 `stopped`，不是正式容量上限。
- runner 顺序建立 160 WS 导致阶段前置等待过长，压测源行为不够理想。
- `40003 price too low` 已拆为预期业务拒绝，不计入系统错误率；但它会增加日志噪声。
- 下一轮应先优化 runner，再复跑 60 QPS；不要直接上 70-100。

## 建议沉淀的回归测试

- runner：并行 WS 建连、建连超时、最大失败重试和 STOP 可中断测试。
- runner：`target_users == target_ws_connections` 配置校验。
- WebSocket：广播端到端接收延迟统计。

## 已知缺口

- 单机公网压测源，不能作为正式线上容量上限。
- 缺少 Prometheus 细粒度时间线和 traces。
- 未做并发一致性结论；竞拍最终唯一性、幂等和结算不变量不在本次范围内。

## 测试数据清理结果

- 测试批次 ID：`agent_perf_auction_20260602_qps60_100`
- 创建的数据：测试商家、测试房间、测试拍品、测试用户、出价日志、Redis auction/WS 相关 key
- 清理方式：runner 关闭 WS，调用取消拍品、下播房间、删除用户接口；runner 代码保留
- 清理结果：`closed_ws=160 cancel_item=ok end_room=ok delete_users_attempted=161`
- 未清理原因：出价日志和软删除记录可能按业务模型保留在数据库中；未执行任何跨批次物理删除
