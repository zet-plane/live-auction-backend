# 测试报告：auction skip-TLS performance

## 基本信息

- 测试目标：跳过 public TLS/WSS 路径，验证直播竞拍核心出价 + 排行榜 + 商品详情 + WebSocket 分流链路在 70 QPS / 500 physical WS 起步后继续上探到 1000 逻辑用户、约 500 QPS 的服务端承载表现。
- 测试类型：线上受控性能压测，`single_source_online`。
- 测试时间：2026-06-06 03:34:13 +0800 至 04:20 左右；清理和复核持续到压测后。
- 执行 agent：Codex 主 agent。
- 主 agent：Codex。
- 子 agent：未使用。
- 子 agent 结果摘要：未使用。
- 主 agent 复核结论：未使用。
- 冲突和处理：无子 agent 冲突；本地 SSH 隧道 ramp 因转发断开判为无效，不纳入容量结论。
- Subagent cleanup：未使用。
- 并行数据隔离证明：不适用。
- 读取文档：`AGENTS.md`、`skills/agent-testing-gate/SKILL.md`、`skills/live-auction-online-ops/SKILL.md`、`docs/agent-testing/README.md`、`templates/protocol.md`、`guides/runner.md`、`guides/performance/README.md`、`guides/performance/types.md`、`guides/performance/online.md`、`guides/performance/runner.md`、`guides/environment.md`、`flows/auction-lifecycle.md`、`modules/bid.md`、`modules/ws.md`、`modules/item.md`、`modules/room.md`、`modules/deposit.md`、`reports/README.md`。

## 测试环境

- 服务地址：线上服务；正式报告省略完整地址。
- 配置来源：已部署线上 backend，镜像 `ghcr.io/zet-plane/live-auction-backend:5359b9f3`。
- MySQL：线上数据库，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- Apifox：不适用，本次不是接口契约对齐测试。
- WebSocket：线上 WebSocket 服务；有效 ramp 从线上服务器内通过 service path 访问，绕过 public TLS/WSS。
- 观测：Prometheus、`kubectl top`、backend restart count、backend 日志严格 marker。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| MySQL | 线上真实依赖 | 出价、用户、商家、房间、商品和保证金链路需要真实持久化行为 |
| Redis | 线上真实依赖 | WebSocket ticket、在线状态、竞拍热状态、排行榜和 bid log stream 依赖 Redis |
| WebSocket | 真实连接 | 验证 control/market 物理隔离后高连接数与 fanout 表现 |
| Prometheus/kubectl/logs | 只读查询 | 采集资源、服务端延迟、RPS、WS 指标、重启和错误日志 |
| 外部服务 | 未调用 | 本次只测本系统 HTTP/WS/观测栈 |

## 测试数据

- 测试批次 ID：
  - `agent_perf_auction_20260606_skip_tls_70qps_500ws`
  - `agent_perf_auction_20260606_skip_tls_ramp_500qps_1000users`（本地隧道无效轮）
  - `agent_perf_auction_20260606_remote_skip_tls_ramp_500qps_1000users`
- 创建数据：批次商家、测试用户、直播间、拍品、竞拍规则、保证金、WebSocket 连接、出价请求。
- 复用数据：线上服务和观测栈；不复用真实业务用户或真实商品。

## 证据资产

| 资产 | 路径 | 用途 |
| --- | --- | --- |
| 性能运行索引 | `docs/agent-testing/performance-runs/README.md` | 汇总 2026-06-06 skip-TLS 与 public-domain 后续运行状态 |
| 本轮 runner | `docs/agent-testing/performance-runs/agent_perf_auction_20260606_skip_tls_70qps_500ws/main.go` | 复跑或审查 skip-TLS 70 QPS 与 remote service-path ramp 的 runner 实现 |
| 本轮脱敏证据摘要 | `docs/agent-testing/performance-runs/agent_perf_auction_20260606_skip_tls_70qps_500ws/evidence-redacted.md` | 保留关键指标、资源样本、失败隧道和清理摘要 |
| 本轮运行说明 | `docs/agent-testing/performance-runs/agent_perf_auction_20260606_skip_tls_70qps_500ws/README.md` | 说明路径、批次、有效结论和无效结论边界 |
| public-domain 后续计划 | `docs/agent-testing/performance-runs/agent_perf_auction_20260606_public_domain_local/performance-plan.md` | 后续经 public HTTPS/WSS 路径验证用户侧体验 |

## 执行步骤

1. 只读 preflight：确认 backend 镜像、Pod/Service/Deployment、健康检查、Prometheus ready、Pod/Node 资源基线。
2. 复制并调整 performance runner：保留所有指标输出，将 client E2E / `time_sync` 到达延迟从硬判停改为 advisory。
3. 通过本地 SSH service tunnel 执行 `70 QPS / 250 logical WS users / 500 physical WS`，完成 runner 输出、Prometheus 观测、业务对账和 cleanup。
4. 尝试通过本地 SSH service tunnel 执行 `150/300/500 QPS` ramp；该轮在第一档隧道断开，判为无效并执行 cleanup-only。
5. 交叉编译 Linux runner，上传至线上服务器 `/tmp`，从线上服务器内直连 backend service 和 Prometheus ClusterIP 执行有效 ramp。
6. 使用 Prometheus 时间线还原有效 ramp 的三个稳态窗口，并按阶段计算服务端性能指标。
7. 执行 cleanup-only 和公开状态复核，删除远端临时二进制和日志。
8. 压测后复查资源回落、backend restart、严格日志 marker。

## 验证证据

| 验证点 | 证据 | 结果 |
| --- | --- | --- |
| preflight | backend 镜像 `5359b9f3`；health HTTP 200，MySQL/Redis OK；Prometheus ready | 通过 |
| 70 QPS 基线 | runner 完成 `69.72 QPS / 500 physical WS`，业务对账 OK，cleanup OK | 通过 |
| 本地隧道 ramp | `150 QPS / 1000 physical WS` 阶段出现本地转发端口 `connection refused`，错误率约 99.96% | 无效，不计入容量 |
| 远端 service-path ramp | Prometheus 显示 HTTP RPS max `485.56/s`，WS active max `2000`，bid RPS max `388.44/s` | 达到目标量级 |
| 重启和日志 | backend restart count `0`；严格 `panic|fatal|oom|killed` marker count `0` | 通过 |
| 资源回落 | 最终 backend `7m / 86Mi`，MySQL `10m / 681Mi`，Redis `10m / 127Mi`，节点 `4% CPU / 61% memory` | 通过 |
| 清理状态 | 远端 ramp 残留公开状态复核：测试商品 `cancelled`，房间 `idle`，无当前拍品，在线数 0，队列空 | 通过，仍保留已取消测试商品记录 |

## 每阶段结果

### Runner 完整输出阶段

| 阶段 | 路径 | 目标 QPS | 实际 QPS | 逻辑 WS 用户 | 物理 WS | Total | Success | HTTP failures | Expected 400 | Timeouts | Client P95/P99 | Server P95/P99 max | WS connect P95/P99 | WS delivery max | WS write P95 max | time_sync write lag P95 max | 结论 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 70 QPS / 500 physical WS | 本地 SSH service tunnel | 70 | 69.72 | 250 | 500 | 12,550 | 10,979 | 4 | 1,567 | 4 | 116.036ms / 163.141ms | 95.888ms / 99.178ms | 244.767ms / 262.766ms | 2,050/s | 0.969ms | 4.289ms | 通过 |

### 远端 service-path ramp 稳态窗口

远端 runner 主 stdout 未完整回传，以下指标来自 Prometheus 稳态窗口。`400` 主要来自出价链路预期业务拒绝，例如价格过低；未观察到 5xx 峰值。

| 阶段 | 稳态窗口 CST | 目标 QPS | HTTP RPS avg/max | bid RPS avg/max | 逻辑 WS 用户 | 物理 WS active | status code RPS at end | Server P95 max | Server P99 max | DB ops max | WS delivery max | WS write P95 max | send queue depth P95 max | time_sync write lag P95 max | bid flush P95 | flush bids P95 | pending P95 | backend restarts |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 150 QPS / 1000 physical WS | 04:10:25-04:12:25 | 150 | 150.00 / 150.00 | 120.00 / 120.00 | 500 | 1000 | 200=149.82/s, 400=0.18/s | 1.994ms | 6.747ms | 160.04/s | 5,155.93/s | 0.956ms | 0 | 6.084ms | 242.5ms | 24.25 | 4.731 | 0 |
| 300 QPS / 1500 physical WS | 04:13:55-04:15:55 | 300 | 296.64 / 297.00 | 237.30 / 237.58 | 750 | 1500 | 200=292.60/s, 400=4.02/s | 3.786ms | 9.125ms | 307.04/s | 7,810.02/s | 0.959ms | 0 | 12.628ms | 242.5ms | 47.44 | 4.740 | 0 |
| 500 QPS / 2000 physical WS | 04:17:25-04:18:55 | 500 | 485.06 / 485.56 | 388.04 / 388.44 | 1000 | 2000 | 200=468.07/s, 400=17.11/s | 4.775ms | 17.276ms | 495.56/s | 10,471.09/s | 0.959ms | 0 | 16.563ms | 242.5ms | 48.75 | 4.744 | 0 |

### 资源样本

| 时间点 / 阶段 | backend | MySQL | Redis | Node | 说明 |
| --- | ---: | ---: | ---: | ---: | --- |
| 压测前基线 | 7m / 48Mi | 9m / 674Mi | 10m / 78Mi | 4% CPU / 62% memory | 初始 preflight |
| 70 QPS 阶段中 | 166m / 53Mi | 49m / 674Mi | 45m / 80Mi | 未记录 | 本地 service tunnel 有效轮 |
| remote ramp 高点样本 | 707m / 128Mi | 137m / 681Mi | 93m / 107Mi | 31% CPU / 64% memory | 最高连接阶段附近 |
| 压测后回落 | 7m / 86Mi | 10m / 681Mi | 10m / 127Mi | 4% CPU / 61% memory | cleanup 后 |

## 通过项

- 跳过 public TLS/WSS 后，service path 下 WebSocket 建连显著稳定：70 QPS 轮 WS connect P99 为 `262.766ms`。
- 远端 service-path ramp 达到约 `485.56 HTTP RPS`、`388.44 bid RPS` 和 `2000 active WS`。
- 三个远端稳态窗口的服务端 HTTP P99 均低于 `20ms`，最高为 `17.276ms`。
- WS write P95 在高连接下仍约 `0.959ms`，send queue depth P95 为 `0`。
- backend 无 restart，严格 panic/fatal/OOM/killed 日志标记为 0。
- 资源未打满，最高观测节点 CPU 约 31%，backend CPU 约 707m。
- 测试后资源回落，房间不再 live，测试商品为 cancelled。

## 失败项

- 本地 SSH service tunnel ramp 无效：在 `150 QPS / 1000 physical WS` 阶段本地转发端口断开，runner 记录大量 `connection refused`，该轮不作为 backend 容量证据。

失败场景：本地 agent 通过 SSH 本地转发继续上探时，转发端口中途不可用。

复现步骤：通过本地 SSH tunnel 转发 backend service 和 Prometheus 后，执行 `150/300/500 QPS` ramp。

期望结果：本地转发持续可用，runner 完成所有阶段。

实际结果：第一档出现 `connection refused`，HTTP failure ratio 约 99.96%，Prometheus 查询和业务对账也因本地端口不可用失败。

相关证据：runner 输出 `STOP_REASON: error_rate_gt_3_percent`，reconcile 中 item/ranking/room 请求均为 `connect: connection refused`。

可能原因：长时间高连接压测下本地 SSH forwarding 不适合作为稳定压测通道。

影响范围：仅影响本地隧道 ramp 证据，不影响远端 service-path ramp 结论。

建议修复点：后续高压阶段直接在远端压测源或 k8s Job 中执行 runner，并把 stdout 重定向到文件再回收。

建议新增的回归测试：保留 remote-host skip-TLS ramp 复跑脚本，避免本地转发路径参与容量判断。

## 跳过项

- 未执行 public TLS/WSS 路径高压复跑：本轮明确跳过 TLS，目标是隔离服务端路径。
- 未执行 500 QPS 以上继续上探：用户给定本轮业务目标是 1000 人、约 500 QPS；达到后停止。
- 未直接查询线上 MySQL/Redis 内部数据：报告只使用 HTTP 公开状态、runner cleanup 输出和 Prometheus/kubectl/logs 证据，避免暴露地址和凭据。

## Apifox 对齐偏差

- 不适用，本次不是接口契约测试。

## 风险和建议

- 严格 `PerformanceReport` 结论应标记为 `inconclusive`，因为远端有效 ramp 的 runner stdout 未完整回传，缺少 runner 侧 `TOTAL/SUCCESS/CLIENT_E2E/cleanup` 完整块。
- 服务端侧容量判断可以写为：在 `single_source_online`、remote-host service path、control/market 物理隔离条件下，服务端指标已支撑约 `485 HTTP RPS / 388 bid RPS / 2000 active WS`，且资源、重启和日志健康。
- 后续正式容量报告建议让 runner 在远端写完整 stdout 到 `/tmp/<batch>.log`，执行结束后再下载脱敏日志，避免 SSH 流式输出丢失。
- 如果要证明公网用户体验，需要单独恢复 public TLS/WSS 路径压测；本报告不能证明 public ingress/TLS 下的连接体验。

## 建议沉淀的回归测试

- `70 QPS / 500 physical WS / service path`：作为 skip-TLS 基线。
- `150/300/500 QPS + 1000/1500/2000 physical WS / remote-host service path`：作为高压容量回归。
- public WSS connect path 诊断：用于区分客户端公网路径、Ingress/TLS 和服务端 accept/register。

## 已知缺口

- 远端有效 ramp 主 runner stdout 未完整回传，导致 runner client-side total/success/client E2E 分位不可恢复。
- remote cleanup-only 由于原账号已不可登录，返回 `merchant_login=err user_login_ok=0`；后续通过公开状态确认测试商品 cancelled、房间 idle，但没有逐用户删除成功块。
- Prometheus 指标按稳态窗口重建，阶段边界存在 30s 采样和 1m rate 窗口误差。
- 公开商品列表会返回 cancelled 测试商品，因此报告记录为“业务安全但仍有已取消测试记录可见”。

## 测试数据清理结果

- 线上依赖使用情况：已使用，地址和凭据已省略。
- 测试数据范围：仅本次三个 batch ID 创建的数据。
- `agent_perf_auction_20260606_skip_tls_70qps_500ws`：runner cleanup 显示 closed WS `500`，cancel item OK，end room OK，delete users attempted `321`。
- `agent_perf_auction_20260606_skip_tls_ramp_500qps_1000users`：隧道恢复后 cleanup-only 显示 cancel OK、end room OK、user delete OK `1100/1100`、merchant delete OK。
- `agent_perf_auction_20260606_remote_skip_tls_ramp_500qps_1000users`：公开状态复核显示测试商品 `cancelled`、房间 `idle`、`current_item_id=""`、`online_count=0`、队列长度 `0`；远端临时 runner 二进制和日志已删除。
- 未清理原因：仍可通过公开商品列表看到 cancelled 测试商品记录；当前接口公开列表不过滤 cancelled，且商家账号已不可登录，未继续执行数据库级清理以避免越过测试数据清理边界。
