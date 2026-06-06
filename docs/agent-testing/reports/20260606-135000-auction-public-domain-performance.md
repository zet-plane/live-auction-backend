# 测试报告：auction public-domain performance

## 基本信息

- 测试目标：验证本机 runner 经 public HTTPS/WSS 入口访问线上竞拍链路时的 HTTP 延迟、出价路由延迟、WebSocket 建连、`bid_success` / `time_sync` 到达表现和清理安全性。
- 测试类型：性能压测，`single_source_online`。
- 测试时间：2026-06-06 13:50-15:20 CST。
- 执行 agent：Codex 主 agent。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/*`、`guides/environment.md`、`flows/auction-lifecycle.md`、`modules/bid.md`、`modules/ws.md`、`modules/item.md`、`modules/room.md`、`modules/deposit.md`、本批次 `performance-plan.md`。

## 测试环境

- 服务地址：public HTTPS/WSS 线上入口，完整地址已省略。
- 配置来源：`performance-plan.md` 和环境变量，敏感值未写入文件。
- MySQL：线上数据库，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- WebSocket：public WSS，control/market 物理分流。
- 压测源：本机单源 runner。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | public HTTPS | 验证用户侧 public path |
| WebSocket | public WSS | 验证 WSS 建连和 fanout 到达 |
| MySQL / Redis | 线上真实依赖，业务接口间接访问 | 验证真实竞拍链路 |
| Kubernetes | 只读 `kubectl top/get/logs` | 观察资源、restart 和日志安全性 |
| Prometheus | readiness 已确认，runner timeline 未配置 | 无本地 Prometheus 入口；本轮结论不写正式容量上限 |

## 测试数据

- 父批次 ID：`agent_perf_auction_20260606_public_domain_local`
- 子批次：原批次 partial setup、`run2`、`run3`、`run4`、`run5`
- 创建数据：批次前缀商家、用户、房间、拍品、保证金、出价、WS ticket。
- 复用数据：无业务数据复用；线上服务和依赖复用。

## 执行步骤

1. 读取计划和测试契约，补齐 runner 指标。
2. 执行 preflight：public health、线上只读资源、backend image、restart、日志标记、Prometheus readiness。
3. 执行 smoke：50 QPS、100 logical / 200 physical WS。
4. 执行 A 组：400 logical / 800 physical WS，150/300/500 QPS，500 档触发停止。
5. 执行 B 组：300 QPS，400/600 logical WS；400 档 WSS connect P99 触发计划停止，600 档为 STOP 后部分阶段。
6. 执行 cleanup 和最终资源、restart、日志检查。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| runner 可运行 | `go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260606_public_domain_local` 通过 | 通过 |
| public health | HTTP 200，MySQL/Redis component ok | 通过 |
| smoke 对账 | item detail/ranking/room 均 HTTP 200，WS connected `200` | 通过 |
| A/500 停止 | error rate `3.63%`，timeout `3.24%`，client P99 `20.921s` | 失败并停止 |
| B/ws_400 停止 | public WSS connect P99 `14.892s`，超过 `10s` 阈值 | 失败并停止 |
| 资源安全 | backend restart `0`，最终 backend `7m/103Mi` 附近 | 通过 |
| 日志安全 | 严格 panic/fatal/OOM/killed 标记 `0` | 通过 |
| 清理 | 各子批次均关闭 WS、取消拍品、下播房间、尝试删除账号 | 完成，有账号删除仅记录 attempted |

## 每档结果

| 阶段 | 目标 QPS | 实际 QPS | logical WS | physical WS | HTTP fail | timeout | client P95 | client P99 | bid route P99 | bid_success P99 | time_sync interval P99 | 结论 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| smoke | 50 | 47.84 | 100 | 200 | 1.03% | 1.03% | 1.558s | 2.385s | 2.385s | 1.359s | 1.863s | 通过 |
| A/150 | 150 | 128.60 | 400 | 800 | 1.29% | 1.28% | 1.915s | 2.734s | 2.734s | 1.437s | 1.899s | 通过 |
| A/300 | 300 | 209.17 | 400 | 800 | 1.91% | 1.91% | 2.708s | 3.829s | 3.828s | 1.573s | 1.943s | 黄色观察 |
| A/500 | 500 | 203.07 | 400 | 800 | 3.63% | 3.24% | 8.830s | 20.921s | 20.921s | 13.835s | 7.429s | 停止 |
| B/ws_400 | 300 | 225.79 | 400 | 800 | 1.71% | 1.71% | 2.087s | 3.964s | 3.906s | 1.394s | 1.901s | WSS P99 停止 |
| B/ws_600 partial | 300 | 248.70 | 600 | 1200 | 2.02% | 0.00% | 2.092s | 3.056s | 3.048s | 2.095s | 1.926s | STOP 后部分阶段 |

## 通过项

- Smoke 完整通过，业务对账成功。
- A/150 未触发停止条件。
- A/300 未触发硬停止，但 client P95 超过黄色观察线。
- 线上 backend 未重启，严格 panic/fatal/OOM/killed 日志标记为 0。
- 每次 runner 结束后均执行 cleanup。

## 失败项

- 失败场景：A/500 QPS 阶段。
- 复现步骤：本机 public HTTPS/WSS，400 logical / 800 physical WS，目标 500 QPS，3 分钟。
- 期望结果：HTTP failure rate、timeout rate 均不超过 3%，业务对账通过。
- 实际结果：HTTP failure rate `3.63%`，timeout rate `3.24%`，client P99 `20.921s`，触发 `error_rate_gt_3_percent`。
- 相关证据：run4 `custom_500qps` runner 输出。
- 可能原因：本机单源 public path / 客户端网络和请求尾部先成为瓶颈；服务端资源样本未显示 CPU/内存瓶颈。
- 影响范围：不能用本轮证明 public path 在 500 target QPS 下稳定。
- 建议修复点：使用远端或多源 public 压测源复跑，并补 Prometheus server-side timeline。
- 建议新增的回归测试：public path 阶梯压测自动判停包含 WSS P99 和 server-side HTTP P99。

- 失败场景：B/ws_400 public WSS connect P99 超阈值。
- 复现步骤：本机 public WSS，400 logical / 800 physical WS，固定 300 target QPS。
- 期望结果：public WSS connect P99 不超过 10s。
- 实际结果：WSS connect P99 `14.892s`，max `20.539s`。
- 相关证据：run5 第一阶段 runner 输出。
- 可能原因：本机到 public WSS 的网络路径或 TLS/WSS 建连尾延迟。
- 影响范围：B 组 800/1000 logical 未执行，C 组不执行。
- 建议修复点：从线上 host、云上压测源或多地域压测源复跑 public WSS 建连。
- 建议新增的回归测试：低并发/高并发 public WSS connect sweep。

## 跳过项

- A/800、A/1000：A/500 已触发停止条件，按计划不继续加压。
- B/ws_800、B/ws_1000：B/ws_400 已触发 WSS connect P99 停止；B/ws_600 仅因 STOP 文件生效前已开始而产生部分阶段。
- C 组：A/B 未全部健康，不满足执行条件。

## Apifox 对齐偏差

- 不适用。本轮是性能压测，不是接口契约测试。

## 风险和建议

- 本轮是 `single_source_online`，只能说明本机单源经 public path 的表现，不能作为正式线上容量上限。
- runner 未接入 Prometheus timeline，缺少 server-side HTTP P95/P99、WS write P95/P99、Redis/MySQL RPS 的完整时间序列；归因只能结合 runner 指标和 `kubectl top` 样本。
- A/500 时服务端资源样本和 restart/log 安全，但 client 和 WS 到达延迟显著劣化，优先怀疑本机 public path 或压测源瓶颈。
- 原批次账号删除后不能立即复用，后续建议 runner 自动使用子批次后缀，并记录父批次关联。

## 建议沉淀的回归测试

- 给 performance runner 增加计划级停止条件：public WSS connect P99 > 10s 时自动停止。
- 给 performance runner 增加 Prometheus port-forward 或 SSH query adapter，避免 server-side timeline 缺失。
- 给 cleanup 增加删除成功数量统计，而不是只输出 attempted。

## 已知缺口

- 未采集完整 Prometheus stage timeline。
- 未直接查询 MySQL / Redis 内部最终状态，只通过 HTTP 对账和业务 cleanup 验证。
- 未执行 C 组组合验证。

## 测试数据清理结果

| 子批次 | 清理方式 | 清理结果 | 未清理原因 |
| --- | --- | --- | --- |
| 原批次 partial | cleanup-only | 取消 1 个 item，下播房间，删除 31 用户和 merchant | 无已知 |
| run2 | runner cleanup | 关闭 240 WS，取消 item，下播房间，删除账号 attempted `121` | 删除成功数未由 runner 输出 |
| run3 | runner cleanup | 关闭 200 WS，取消 item，下播房间，删除账号 attempted `121` | 删除成功数未由 runner 输出 |
| run4 | runner cleanup | 关闭 800 WS，取消 item，下播房间，删除账号 attempted `421` | 删除成功数未由 runner 输出 |
| run5 | runner cleanup | 关闭 1200 WS，取消 item，下播房间，删除账号 attempted `1021` | 删除成功数未由 runner 输出 |
