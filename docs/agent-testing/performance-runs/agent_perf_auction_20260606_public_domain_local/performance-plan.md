# Public Domain 本机压测计划

## 状态

- 计划状态：待执行。
- 执行授权：未授权。新对话执行前必须重新获得用户明确批准。
- 计划批次 ID：`agent_perf_auction_20260606_public_domain_local`。
- 目标入口：public domain / HTTPS + WSS，完整地址不得写入计划、报告或证据文件。

## 交接上下文

上一轮已经完成 skip-TLS service-path 压测：

- 报告：`docs/agent-testing/reports/20260606-033413-auction-skip-tls-performance.md`
- runner 资产：`docs/agent-testing/performance-runs/agent_perf_auction_20260606_skip_tls_70qps_500ws/`
- 有效服务端侧峰值：约 `485.56 HTTP RPS`、`388.44 bid RPS`、`2000 active WS`。
- 有效远端 service-path 高点资源样本：backend 约 `707m CPU / 128Mi`，MySQL 约 `137m / 681Mi`，Redis 约 `93m / 107Mi`，节点约 `31% CPU / 64% memory`。
- backend restart count 为 `0`，严格 `panic|fatal|oom|killed` 日志标记为 `0`。
- 该轮证明了 skip-TLS service path 下千级 WS 和接近 500 QPS 的服务端处理能力，但没有证明 public domain / TLS / WSS 入口体验。

本轮要测的是：

```text
本机 runner -> public domain HTTPS/WSS -> Ingress/Traefik -> backend -> Redis/MySQL
```

本轮不是只看端到端体验，也不是只看 backend 纯服务端上限。它同时观察两类能力：

- 全链路耗时：判断用户侧体验、公网/TLS/Ingress/压测源影响。
- 接口耗时和服务端指标：判断 backend、Redis、MySQL、WS fanout 是否健康。

归因规则：

- 如果全链路劣化但出价接口 server P95/P99、WS write、Redis/MySQL 和资源健康，优先归因 public path 或本机压测源。
- 如果全链路和接口 server P95/P99 同步劣化，或 Redis/MySQL/WS queue/CPU 同步抬升，优先归因服务端链路。
- 如果本机 CPU、网络、fd 或 public WSS connect 先劣化，本轮只能说明本机 public-domain 压测源遇到瓶颈，不能证明 backend 上限。

## 必须读取文档

新对话开始执行前，按渐进式披露读取：

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/performance/README.md
docs/agent-testing/guides/performance/types.md
docs/agent-testing/guides/performance/online.md
docs/agent-testing/guides/performance/runner.md
docs/agent-testing/guides/environment.md
docs/agent-testing/flows/auction-lifecycle.md
docs/agent-testing/modules/bid.md
docs/agent-testing/modules/ws.md
docs/agent-testing/modules/item.md
docs/agent-testing/modules/room.md
docs/agent-testing/modules/deposit.md
```

如果写报告，再读取：

```text
docs/agent-testing/reports/README.md
```

项目本地技能：

- `skills/agent-testing-gate/SKILL.md`
- `skills/live-auction-online-ops/SKILL.md`

## 测试目标

验证本机压测源经 public domain / HTTPS + WSS 访问线上服务时：

1. 全链路 P95/P99、public WSS connect 和入口层表现。
2. 出价接口 server P95/P99、Redis Lua、DB ops 和 backend 资源表现。
3. 高 fanout 下 `bid_success` 和 `time_sync` 的推送耗时、服务端写入延迟、队列深度和丢弃/覆盖行为。
4. QPS 增长和 WS 增长分别对系统造成的影响，避免同时改变多个变量导致归因混乱。

## 禁止范围

- 不走 SSH service tunnel 发压。
- 不修改线上 Deployment、Ingress、Service、Secret、ConfigMap 或镜像。
- 不扩缩容、不重启、不发布、不回滚。
- 不清库、不清表、不执行 `FLUSHALL` / `FLUSHDB`。
- 不操作非本批次数据。
- 不在计划、报告或证据中写入线上地址、token、DSN、Redis 凭据、kubeconfig、proxy 凭据或完整 WebSocket ticket/query string。

## 依赖策略

| 依赖 | 使用方式 | 说明 |
| --- | --- | --- |
| HTTP | public HTTPS domain | 本机 runner 真实公网入口访问 |
| WebSocket | public WSS domain | control/market 物理隔离 |
| MySQL | 线上真实依赖 | 通过业务接口间接使用 |
| Redis | 线上真实依赖 | 通过业务接口、WS ticket、竞拍状态、排行榜、stream 间接使用 |
| Prometheus | 线上只读查询 | 可 SSH 到线上服务器查询，报告脱敏 |
| kubectl/logs | 线上只读查询 | 仅 `get/top/logs` 等只读命令 |
| 压测源 | 本机 | 必须记录本机资源是否成为瓶颈 |

## 数据边界

- batch id：`agent_perf_auction_20260606_public_domain_local`
- 商家、用户、房间、拍品、规则、保证金、出价、WS ticket 都必须带本批次可追踪前缀或实体集合。
- cleanup 只能作用于本批次数据。
- 如果 cleanup 失败，必须记录未清理项、公开状态和人工处理建议。

## Runner 准备要求

执行前必须检查当前 performance runner 是否已经支持以下指标：

- per-route client latency，至少能区分出价接口 client P95/P99。
- `bid_success` client arrival delay P50/P95/P99/max。
- `time_sync` client arrival delay P50/P95/P99/max。
- `time_sync` interval P50/P95/P99/max。
- 每阶段完整 stdout 落盘。

如果缺失，先补 runner，再执行压测。

推荐远端和本地证据保存方式：

```text
docs/agent-testing/performance-runs/agent_perf_auction_20260606_public_domain_local/
├── main.go
├── README.md
├── evidence-redacted.md
└── performance-plan.md
```

runner 运行时建议同时写本地日志，避免长输出丢失：

```text
runner stdout -> docs/agent-testing/performance-runs/agent_perf_auction_20260606_public_domain_local/runner-output-redacted.log
```

不要把 token、完整域名、ticket 或完整 query string 写入日志。

## 压测模型

HTTP mix：

```text
core_bid_80_ranking_10_item_10
```

WebSocket：

```text
PERF_WS_STREAM_MODE=control_market
physical_ws = logical_users * 2
```

### A 组：固定 WS，增加 QPS

固定：

```text
logical users = 400
physical WS = 800
```

| 阶段 | QPS | logical users | physical WS | 时长 | 变量 |
| --- | ---: | ---: | ---: | ---: | --- |
| smoke | 50 | 100 | 200 | 1-2 min | 脚本、数据、public WSS 可用性 |
| qps_150 | 150 | 400 | 800 | 3 min | QPS |
| qps_300 | 300 | 400 | 800 | 3 min | QPS |
| qps_500 | 500 | 400 | 800 | 3 min | QPS |
| qps_800 | 800 | 400 | 800 | 3 min | QPS |
| qps_1000 | 1000 | 400 | 800 | 3 min | QPS |

### B 组：固定 QPS，增加 WS

固定：

```text
QPS = 300
```

| 阶段 | QPS | logical users | physical WS | 时长 | 变量 |
| --- | ---: | ---: | ---: | ---: | --- |
| ws_400 | 300 | 400 | 800 | 3 min | WS |
| ws_600 | 300 | 600 | 1200 | 3 min | WS |
| ws_800 | 300 | 800 | 1600 | 3 min | WS |
| ws_1000 | 300 | 1000 | 2000 | 3 min | WS |

### C 组：组合验证

只在 A/B 都健康时执行。

| 阶段 | QPS | logical users | physical WS | 时长 |
| --- | ---: | ---: | ---: | ---: |
| combo_500_1000 | 500 | 1000 | 2000 | 5 min |
| combo_hold | 最高稳定组合 | 对应用户数 | 对应 WS | 10-15 min |

## 每阶段必须输出指标

### 全链路 HTTP

| 指标 | 来源 |
| --- | --- |
| target QPS | runner |
| actual QPS | runner + Prometheus |
| total requests | runner |
| success | runner |
| HTTP failures | runner |
| timeouts | runner |
| error rate | runner |
| timeout rate | runner |
| status code distribution | runner + Prometheus |
| business code distribution | runner |
| full-path client P50/P95/P99/max | runner |

### 出价接口

| 指标 | 来源 |
| --- | --- |
| bid request count | runner + Prometheus |
| bid RPS | Prometheus |
| bid success / expected reject / unexpected fail | runner |
| bid client P50/P95/P99/max | runner per-route |
| bid server P95/P99 | Prometheus route filter |
| Redis Lua result RPS | Prometheus |
| DB ops RPS | Prometheus |

### WebSocket 总体

| 指标 | 来源 |
| --- | --- |
| target logical users | runner |
| physical WS target | runner |
| WS connected | runner + Prometheus |
| WS connect failures | runner |
| WS connect P50/P95/P99/max | runner |
| WS active | Prometheus |
| WS delivery RPS | Prometheus |
| WS write P95/P99 | Prometheus |
| send queue depth P95/P99 | Prometheus |
| dropped / timeout / slow close count | runner + Prometheus/logs |

### `bid_success` 专项

| 指标 | 来源 |
| --- | --- |
| `bid_success` event count | runner |
| `bid_success` delivery RPS | Prometheus by event_type |
| `bid_success` client arrival delay P50/P95/P99/max | runner |
| `bid_success` server write P95/P99 | Prometheus by event_type |
| `bid_success` send queue depth P95/P99 | Prometheus by event_type |
| bid broadcast flush P95/P99 | Prometheus |
| bid broadcast batch size P95/P99 | Prometheus |
| bid broadcast pending P95/P99 | Prometheus |

### `time_sync` 专项

| 指标 | 来源 |
| --- | --- |
| `time_sync` event count | runner |
| `time_sync` delivery RPS | Prometheus by event_type |
| `time_sync` client arrival delay P50/P95/P99/max | runner |
| `time_sync` interval P50/P95/P99/max | runner |
| `time_sync` server write lag P95/P99 | Prometheus |
| `time_sync` overwrite/drop count | Prometheus |
| control stream `time_sync` P95/P99 | runner |

### 资源

| 指标 | 来源 |
| --- | --- |
| backend CPU / memory | kubectl top |
| backend restart count | kubectl / Prometheus |
| backend panic/fatal/OOM/killed marker | logs |
| Redis CPU / memory | kubectl top |
| MySQL CPU / memory | kubectl top |
| node CPU / memory | kubectl top |
| local pressure source CPU / memory / fd / network | local host sampling |

### 业务对账

| 指标 | 来源 |
| --- | --- |
| item detail ok/fail | runner HTTP check |
| ranking ok/fail | runner HTTP check |
| room state ok/fail | runner HTTP check |
| WS connected count matches | runner + Prometheus |
| cleanup result | runner |

## 停止条件

立即停止：

- HTTP failure rate > `3%`。
- timeout rate > `3%`。
- 5xx 明显增加。
- WS connect success < `95%`。
- public WSS connect P99 > `10s` 持续一个阶段。
- 出价接口 server P99 > `300ms` 持续。
- backend restart / panic / OOM。
- Redis/MySQL timeout 或连接池耗尽迹象。
- 业务对账失败。
- 人工要求停止。

黄色观察，不立即停止：

- 全链路 P99 > `5s`。
- 全链路 P95 > `2s`。
- `bid_success` client arrival P99 > `5s`。
- `time_sync` interval P99 > `5s`。
- 本机压测源 CPU、网络或 fd 接近瓶颈。

黄色观察升级为停止：

- 全链路 P99 > `5s` 且 timeout / 5xx / WS connect failure 同时上升。
- `bid_success` 或 `time_sync` P99 > `5s` 且 WS queue / drop / write lag 同时上升。
- 本机压测源瓶颈导致指标失真。

## 线上监控命令范围

允许的新对话执行命令类型：

```bash
rtk ssh deploy@115.191.46.148 "kubectl top pods -n live-auction"
rtk ssh deploy@115.191.46.148 "kubectl top nodes"
rtk ssh deploy@115.191.46.148 "kubectl get pod -n live-auction -l app=live-auction-backend ..."
rtk ssh deploy@115.191.46.148 "kubectl logs deployment/live-auction-backend -n live-auction --since=<window>"
rtk ssh deploy@115.191.46.148 "curl -sG ... Prometheus query ..."
```

禁止在新对话中执行任何修改线上资源的命令，除非用户单独批准。

## 报告要求

正式报告路径：

```text
docs/agent-testing/reports/<timestamp>-auction-public-domain-performance.md
```

报告必须包含：

1. 测试目标和环境。
2. 压测源说明：本机、public domain、HTTPS/WSS。
3. A 组 QPS 上探完整阶段表。
4. B 组 WS 上探完整阶段表。
5. C 组组合验证表，如果执行。
6. 每阶段全链路指标表。
7. 每阶段出价接口指标表。
8. 每阶段 `bid_success` 指标表。
9. 每阶段 `time_sync` 指标表。
10. 每阶段 WebSocket 总体指标表。
11. 每阶段资源表。
12. 业务对账和 cleanup 结果。
13. 停止条件是否触发。
14. 归因判断：public path / 本机压测源 / backend / Redis / MySQL / WS fanout。
15. 与 `20260606-033413-auction-skip-tls-performance.md` 的 service-path 对照结论。

## 新对话执行前检查清单

- 已重新读取本计划和 `docs/agent-testing/README.md` 路由文档。
- 已获得用户明确批准执行 public domain 压测。
- 已确认 public domain、Prometheus、线上只读监控可达。
- 已确认 runner 支持本计划要求的 per-route、`bid_success`、`time_sync` 指标；如果不支持，先补 runner。
- 已确认本机 fd limit、CPU、网络采样方式。
- 已确认 batch id 和 cleanup 策略。
- 已确认报告不会写入敏感信息。

## 推荐给下一位 agent 的技能

- `agent-testing-gate`：执行前必须使用，确认授权门和真实依赖边界。
- `live-auction-online-ops`：线上只读监控、Prometheus/kubectl/logs。
- `diagnose` 或 `systematic-debugging`：如果 public path 指标异常，需要先复现、分层定位，再归因。
