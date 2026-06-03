# 测试报告：auction performance

## 基本信息

- 测试目标：线上直播竞拍混合链路性能探测
- 测试类型：性能压测，`single_source_online`
- 测试时间：2026-06-02 16:19-16:28 Asia/Shanghai
- 执行 agent：Codex 主 agent
- 主 agent：Codex
- 子 agent：未使用
- 子 agent 结果摘要：未使用
- 主 agent 复核结论：未使用
- 冲突和处理：无
- Subagent cleanup：未使用
- 并行数据隔离证明：不适用
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/README.md`、`types.md`、`online.md`、`runner.md`、`guides/environment.md`、`modules/bid.md`、`modules/ws.md`、`modules/item.md`、`modules/room.md`、`reports/README.md`

## 测试环境

- 服务地址：线上公网 Ingress，完整地址已省略
- 配置来源：线上 k3s deployment
- MySQL：线上 MySQL，地址和凭据已省略
- Redis：线上 Redis，地址和凭据已省略
- Apifox：未执行接口规范对齐，本次为性能压测
- WebSocket：公网 WebSocket 入口，完整地址和 ticket 已省略
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:91c9a696`

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 线上依赖，批次数据 | 验证真实 BidLog、房间、拍品、用户写路径 |
| Redis | 线上依赖，批次 key | 验证真实竞拍 state、ranking、WS ticket |
| WebSocket | 真实公网连接 | 验证连接建立和维持 |
| 外部服务 | 未使用 | 不涉及支付、短信、物流等第三方 |

## 测试数据

- 测试批次 ID：`agent_perf_auction_20260602155512`
- 创建数据：1 个测试商家、1 个测试房间、1 个测试拍品、160 个测试用户、原始执行最高 80 条 WebSocket 连接、1800 次出价尝试
- 复用数据：无业务数据复用

## 执行步骤

1. 执行本地基线：`rtk env GOCACHE=/private/tmp/live-auction-go-test-cache go test ./...`，通过。
2. 线上 preflight：确认健康检查、Deployment rollout、Pod/Service/Ingress、资源水位和后端镜像。
3. 落地 runner：`docs/agent-testing/performance-runs/agent_perf_auction_20260602155512/main.go`。
4. 执行公网单源压测：20 QPS / 40 WS，30 QPS / 60 WS，40 QPS / 80 WS。
5. 40 QPS 档触发 runner 停止阈值，进入对账和 cleanup。
6. 收尾采集 Pod、节点、restart 和日志摘要。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| 线上入口可达 | health HTTP 200，MySQL ok，Redis ok | 通过 |
| 20 QPS | actual 19.98 QPS，40 WS，P95 570ms，P99 694ms，错误率 0.83% | 通过 |
| 30 QPS | actual 30.00 QPS，60 WS，P95 1.057s，P99 1.445s，错误率 2.31% | 通过但接近停止线 |
| 40 QPS | actual 39.99 QPS，80 WS，P95 1.361s，P99 1.886s，错误率 3.51% | 停止 |
| 业务对账 | item detail、ranking、room detail 均 HTTP 200 | 通过 |
| 资源水位 | backend 最高采样约 153m CPU / 31Mi；节点最高采样 CPU 8%、内存 45% | 未见资源瓶颈 |
| 稳定性 | backend restart count 0；HTTP 500 采样计数 0；panic/OOM/fatal 采样计数 0 | 未见崩溃 |
| 清理 | closed_ws=80，cancel_item=ok，end_room=ok，delete_users_attempted=161 | 已执行 |

## 通过项

- 20 QPS / 40 WS 和 30 QPS / 60 WS 阶段完成，未触发停止条件。
- 线上服务没有 5xx、Pod restart、panic、OOM 或明显资源水位异常。
- 停止后业务抽样接口仍可用。

## 失败项

- 40 QPS / 80 WS 阶段触发 runner 停止：aggregate error rate 3.51%。
- 主要业务失败为 `40003 price too low`，属于出价竞争下的业务拒绝；本次 runner 将其计入总错误率，因此结论为 `stopped`，不是正式容量失败。

## 跳过项

- 60 QPS / 120 WS、70 QPS / 160 WS：因 40 QPS 档触发停止条件跳过；复跑配置已改为峰值 160 users / 160 WS。
- peak hold / soak：未在本次批准边界内执行。
- Prometheus 细粒度指标和链路追踪：本次只采集 kubectl top、日志和 runner 证据；后续正式容量结论应补齐 Prometheus 时间线。

## Apifox 对齐偏差

- 不适用，本次不是接口契约测试。

## 风险和建议

- 当前 runner 的出价价格生成在并发下会产生可预期的 `price too low`，导致业务失败率被放大。下一轮建议把 `40003` 从“系统错误率”中拆出，或改成每次出价前读取当前价再生成合法价格。
- 资源水位很低但公网延迟 P95/P99 较高，下一轮建议补 Prometheus HTTP route latency、Redis Lua duration、DB operation duration、WS broadcast duration 的时间线，区分公网链路、客户端压测源和服务端处理耗时。
- `PlaceBid failed` 在采样窗口出现 168 次，需结合业务码确认是否均为预期低价拒绝。

## 建议沉淀的回归测试

- 性能 runner 增加“可接受业务拒绝码”分类，避免把预期 `40003` 直接计入系统错误停止。
- 出价压测增加动态合法出价模式，单独测 Redis Lua + BidLog 写入吞吐。
- WebSocket 专项增加广播端到端延迟统计，而不只统计连接成功数。
- 复跑时按 160 个测试用户建立 160 条 WebSocket 连接，验证一个用户一个房间连接都能收到房间广播事件。

## 已知缺口

- 本次为单机公网压测源，不能作为正式线上容量上限。
- 未读取或记录完整 Prometheus / Loki / Tempo 指标时间线。
- 未做并发一致性结论；竞拍最终唯一性、幂等和结算不变量不在本次范围内。

## 测试数据清理结果

- 测试批次 ID：`agent_perf_auction_20260602155512`
- 创建的数据：测试商家、测试房间、测试拍品、测试用户、出价日志、Redis auction/WS 相关 key
- 清理方式：runner 关闭 WS，调用取消拍品、下播房间、删除用户接口；runner 代码保留
- 清理结果：`closed_ws=80 cancel_item=ok end_room=ok delete_users_attempted=161`
- 未清理原因：出价日志和软删除记录可能按业务模型保留在数据库中；未执行任何跨批次物理删除
