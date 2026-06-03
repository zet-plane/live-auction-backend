# 直播竞拍后端性能压测计划（审核草案）

## 计划状态

```text
计划版本：v0.6
创建日期：2026-06-02
批次 ID：agent_perf_auction_plan_20260602
状态：待审核，未批准执行
执行权限：未授权连接远程、未授权创建测试数据、未授权发起压测
```

本文档只沉淀性能压测计划，供人工审核。审核通过前，不连接线上或线上等价真实依赖，不创建测试数据，不发起 HTTP / WebSocket 压测请求。

## 参考依据

- 阿里云 PTS 电商场景压测示例：通过“多个并行业务会话 + 会话内串行 API”的方式模拟真实电商流量。
- 阿里云线上压测方法论中的“5 个一样”：环境、用户规模、业务场景、业务量级、流量来源尽量贴近真实线上。
- 本项目 `docs/agent-testing/guides/performance/`：性能压测必须声明环境、压测源、阶段模型、阈值、停止条件、监控、业务抽样对账、清理和证据。

阿里云 PTS 只作为业务场景建模参考。本次计划不使用阿里云 PTS 服务发压，不依赖阿里云 PTS 报告，不把 PTS 作为压测源。

## 测试目标

验证 `live-auction-backend` 在核心竞拍读写链路高压下的性能容量、延迟、错误率、资源水位和首个瓶颈。

重点回答：

- 在目标 HTTP QPS、出价 TPS 和同房间 WebSocket 连接数下，服务是否稳定。
- 出价、商品详情、排行榜的 P50 / P95 / P99 是否满足阈值。
- Redis Lua、MySQL 同步写 `bid_logs`、WebSocket 同步广播、每秒 `time_sync` 推送、排行榜 Redis / MySQL 读取、商品当前价 Redis state 读取、应用 Pod CPU / 内存中，哪个最先成为瓶颈。
- 压力升高后系统是否平稳退化，而不是出现 5xx、超时、Pod 重启或资源持续爬升。

## 非目标

本计划不是并发一致性专项测试。

不以以下内容作为主目标：

- 最终成交唯一性专项验证。
- 同价竞争的业务裁决验证。
- 结拍瞬间出价和结算竞争的最终不变量验证。
- `docs/agent-testing/concurrency/` 下的并发一致性计划执行。

性能压测阶段只做必要的业务健康抽样对账，避免把“性能容量结论”和“并发一致性结论”混在一起。

## 读取文档

计划生成和执行应遵循以下路线：

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/performance/README.md
docs/agent-testing/guides/performance/types.md
docs/agent-testing/guides/performance/online.md
docs/agent-testing/guides/performance/runner.md
docs/agent-testing/guides/environment.md
docs/agent-testing/modules/bid.md
docs/agent-testing/modules/ws.md
docs/agent-testing/modules/item.md
docs/agent-testing/modules/room.md
```

## PerformanceEnvironment

第一轮建议从受控单压测源开始：

```text
kind：single_source_online
service_scope：live-auction-backend HTTP API + WebSocket
deploy_target：远程服务或线上等价环境
entrypoint：待审核时确认，可选公网入口或 kubectl port-forward
k8s_namespace：live-auction
app_workload：deployment/live-auction-backend
dependency_scope：MySQL、Redis、WebSocket Hub、观测栈
observability_stack：Prometheus、Grafana、Loki、Tempo、kubectl top/logs
risk_window：建议低峰窗口
rollback_contact：待审核时确认
```

如果使用 `kubectl port-forward`，结论只能标记为 `single_source_online`，不能单独作为正式线上峰值容量结论。

## LoadSource

```text
kind：local_machine
count：1
cpu：待记录
memory：待记录
network_location：本机 agent 所在网络
outbound_identity：本机 agent 出口，脱敏记录
tool：项目自有 performance runner
max_supported_qps：执行前通过空跑或 smoke 估算
known_limit：本机单压测源和本机网络可能先于服务端成为瓶颈
```

正式执行时使用 `docs/agent-testing/guides/performance/performance-runner.go` 作为模板，落地到正式批次目录并保留为可复跑证据资产。

压测发起方固定为本机 agent。本计划不使用远程压测机、不使用阿里云 PTS、不使用第三方施压平台。

## 业务场景模型

本轮按用户确认的核心接口比例，只压测出价写链路、排行榜读取和商品详情/当前价读取，同时维持目标 WebSocket 连接数以覆盖出价成功后的同步广播路径。房间、WebSocket ticket、WebSocket 握手、商家后台和健康检查不进入 HTTP QPS 请求比例；它们只可用于前置测试数据准备、连接建立、只读可达性探测或阶段后对账。

| 会话 | 用户行为 | 串行步骤 | 目标 | 建议流量占比 |
| --- | --- | --- | --- | --- |
| A. 出价用户 | 提交有效或预期竞价冲突的出价 | `POST /api/v1/items/{item_id}/bids` | 核心出价写链路，重点观察 Redis Lua、保证金检查、同步写 `bid_logs` 和出价事件生产 | 80% |
| B. 排行榜读取 | 查询当前拍品排行榜 | `GET /api/v1/items/{item_id}/ranking` | 观察 Redis ranking 读取，以及 Redis miss / 异常时 MySQL 聚合 fallback 风险 | 10% |
| C. 商品详情读取 | 查询商品详情和当前价 | `GET /api/v1/items/{item_id}` | 观察 MySQL item/rule 读取和 ongoing 状态下 Redis auction state 覆盖 | 10% |

本轮 WebSocket 模型：每个阶段先建立目标数量的同房间 WebSocket 长连接，随后在该连接数下执行 80/10/10 HTTP 压测流量。`POST /api/v1/ws-ticket` 和 `GET /ws/v1/rooms/{room_id}` 用于建连，不计入 HTTP QPS 请求比例；WebSocket 连接成功率、连接保持、出价事件接收、广播延迟和每秒 `time_sync` 推送稳定性进入本轮观察与判停。

## 被测接口与业务代码定位

性能报告必须把每条压测链路对应到业务代码路径，便于后续按瓶颈定位到模块、Service、DAO、Redis Lua 或 WebSocket Hub。

| 接口 / 协议 | 所属会话 | Router | Handler | Service / 核心逻辑 | DAO / Cache / Hub | 主要性能风险 |
| --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v1/items/{item_id}/bids` | A | `internal/app/item/router/item.go` | `internal/app/item/handler/bid.go` `PlaceBid` | `internal/app/item/service/bid_service.go` `PlaceBid` | `internal/app/item/cache/bid.go`、`internal/app/item/dao/bid_log.go`、`internal/app/deposit/service/service.go`、`internal/app/order/service/service.go` | Redis Lua 原子出价、保证金检查、同步写 `bid_logs`、一口价成单、出价事件生产 |
| `GET /api/v1/items/{item_id}/ranking` | B | `internal/app/item/router/item.go` | `internal/app/item/handler/bid.go` `GetRanking` | `internal/app/item/service/bid_service.go` `GetRanking` | `internal/app/item/cache/bid.go`、`internal/app/item/dao/bid_log.go` | Redis ranking 读取、Redis miss 后 MySQL 聚合 fallback |
| `GET /api/v1/items/{item_id}` | C | `internal/app/item/router/item.go` | `internal/app/item/handler/item.go` `GetItem` | `internal/app/item/service/service.go` `GetItem` | `internal/app/item/dao/item.go`、`internal/app/item/cache/cache.go` | MySQL 查 item/rule、ongoing 状态下 Redis state 读取 |
| `POST /api/v1/ws-ticket` | WS 建连 | `internal/app/ws/router/router.go` | `internal/app/ws/handler/ticket.go` `IssueTicket` | ticket 生成和 Redis 写入 | Redis key `ws:ticket:{ticket}` | 建连前置，Redis SET / TTL，不计入 HTTP 压测 QPS |
| `GET /ws/v1/rooms/{room_id}` | WS 建连和保持 | `internal/app/ws/router/router.go` | `internal/app/ws/handler/ws.go` `ServeWS` | WebSocket upgrade、ticket `GETDEL`、连接注册、`time_sync` 接收 | `internal/app/ws/hub/hub.go`、`internal/app/ws/hub/conn.go` | 同房间连接数、Hub 索引、慢连接剔除、出价广播扇出、每秒 `time_sync` 推送 |

## 请求占比

本轮请求占比：

| 接口 | 占比 | 说明 |
| --- | --- | --- |
| `POST /api/v1/items/{item_id}/bids` | 80% | 核心出价写链路 |
| `GET /api/v1/items/{item_id}/ranking` | 10% | 排行榜读取 |
| `GET /api/v1/items/{item_id}` | 10% | 商品详情和当前价 |

其他 HTTP 接口不进入压测请求比例。`POST /api/v1/merchant/room`、`POST /api/v1/items`、`POST /api/v1/items/{item_id}/publish`、`POST /api/v1/items/{item_id}/start` 只允许在 setup 阶段创建本批次测试数据时调用；`POST /api/v1/ws-ticket` 和 `GET /ws/v1/rooms/{room_id}` 只用于建立并维持目标 WebSocket 连接；`GET /health` 或 `/api/v1/health` 只允许在 preflight 中做只读可达性探测。

## LoadModel

本轮建议阶段：

| 阶段 | 目标 HTTP QPS | 目标 WebSocket 连接数 | 持续时间 | Ramp | 目的 |
| --- | --- | --- | --- | --- | --- |
| smoke | 10 | 20 | 3 分钟 | 0-1 分钟 | 验证脚本、认证、测试数据、WS 建连、监控和对账 |
| step_load_1 | 30 | 60 | 3 分钟 | 0-1 分钟 | 观察低压下服务端接口延迟、WS 广播和错误率 |
| step_load_2 | 50 | 100 | 3 分钟 | 0-1 分钟 | 观察 Redis Lua、MySQL 写入、排行榜读取和 WS 扇出趋势 |
| step_load_3 | 70 | 140 | 3 分钟 | 0-1 分钟 | 观察核心写链路和 WS 广播是否出现抖动 |
| step_load_4 | 100 | 200 | 3 分钟 | 0-1 分钟 | 寻找 100 QPS / 200 WS 下首个明显瓶颈 |
| step_load_5 | 130 | 260 | 3 分钟 | 0-1 分钟 | 观察接近峰值时资源、服务端延迟和 WS 保持 |
| step_load_6 | 150 | 300 | 3 分钟 | 0-1 分钟 | 观察本轮上限压力下退化表现 |

本轮默认按 `目标 WebSocket 连接数 = 目标 HTTP QPS * 2` 建模；每条 WebSocket 连接对应 1 个测试普通用户并连接到同一个测试房间。runner 准备的测试普通用户数量必须同时覆盖最大目标 WS 连接数和 80% 出价写流量，避免少量用户反复出价导致业务冲突比例失真。

不允许跳过 smoke 直接进入高 QPS 阶段。

## 探测方法

具体探测方法以 `docs/agent-testing/guides/performance/` 为准，按以下顺序执行：

1. Preflight 只读探测：本机 agent 按 `skills/live-auction-online-ops/SKILL.md` 做线上只读检查，确认被测入口、后端版本、Pod 数、资源限制、MySQL / Redis / Prometheus / 日志可观测性。
2. Runner 落地：从 `docs/agent-testing/guides/performance/performance-runner.go` 创建正式批次 `main.go`。
3. Smoke：小流量验证脚本、认证、测试数据、WebSocket 建连、监控采集和抽样对账均可用。
4. Step load：本机 agent 按 `10/20 WS -> 30/60 WS -> 50/100 WS -> 70/140 WS -> 100/200 WS -> 130/260 WS -> 150/300 WS` 逐档加压，每档结束后采集 runner 输出、线上只读监控摘要、日志摘要和业务健康抽样。
5. 本轮不执行 peak hold 或 soak；若 150 QPS 未出现瓶颈，后续另行审核更高阶梯或保持压测。
6. 报告必须明确 WebSocket 结论只覆盖本轮同房间目标连接数和出价广播扇出，不覆盖更大规模在线人数容量。
7. STOP 文件判停：runner 每次发请求前检查 `PERF_STOP_FILE`，触发后停止继续加压并进入 `RECONCILE` 和 `CLEANUP`。
8. 收尾：关闭临时连接或 port-forward，清理本批次数据，保留 runner 代码、脱敏复跑说明和脱敏证据。

Runner 输出块必须保持性能指南定义的结构：

```text
=== PERF_PLAN
=== PREFLIGHT
=== STAGE: <stage_name>
=== STOP_EVENT
=== RECONCILE
=== CLEANUP
=== SUMMARY
```

每个 `STAGE` 至少输出：

```text
TARGET_QPS:
ACTUAL_QPS:
CONCURRENCY:
TOTAL:
SUCCESS:
HTTP_FAILURES:
BUSINESS_FAILS:
TIMEOUTS:
ERROR_RATE:
TIMEOUT_RATE:
BUSINESS_FAILURE_RATE:
P50:
P95:
P99:
MAX:
STATUS_CODES:
BUSINESS_CODES:
```

## 测试数据

```text
测试批次 ID：agent_perf_auction_<YYYYMMDDHHMMSS>
测试商家前缀：agent_perf_auction_<batch>_merchant_
测试用户前缀：agent_perf_auction_<batch>_user_
测试房间前缀：agent_perf_auction_<batch>_room_
测试拍品前缀：agent_perf_auction_<batch>_item_
幂等 key 前缀：agent_perf_auction_<batch>_bid_
```

建议准备：

- 1 个测试商家。
- 1 个测试直播间。
- 2-3 个测试拍品。
- 100-500 个测试普通用户。
- 测试普通用户数必须足够支撑最大目标 QPS 下 80% 出价写流量，避免少量用户反复并发出价导致预期竞价冲突比例失真。
- 出价用户使用独立 token。
- 如果拍品规则要求保证金，出价用户应预先缴纳本批次测试保证金。

禁止：

- 使用真实业务用户。
- 使用真实业务拍品。
- 使用真实支付数据。
- 清库、清表、清 Redis DB。
- 删除或修改非本批次数据。

## Thresholds

第一版建议阈值：

| 指标 | 目标阈值 |
| --- | --- |
| HTTP 5xx 率 | `< 1%` |
| HTTP 超时率 | `< 1%` |
| 出价接口 P95 | `< 500ms` |
| 出价接口 P99 | `< 1000ms` |
| 商品详情 / 排行榜 P95 | `< 300ms` |
| 商品详情 / 排行榜 P99 | `< 800ms` |
| Redis Lua P95 | `< 50ms` |
| Redis Lua P99 | `< 150ms` |
| WebSocket 连接成功率 | `> 99%` |
| WebSocket 广播延迟 P95 | `< 500ms` |
| WebSocket `time_sync` 推送间隔 | 平均约 1 秒，P95 间隔 `< 2s`，无持续缺失 |
| Pod CPU | `< 80%` |
| Pod 内存 | 稳定，无持续爬升 |
| 服务器 CPU | `< 80%`，load5 不持续超过 CPU 核数 |
| 服务器内存 | `< 85%`，无明显 swap 或 OOM 风险 |
| 服务器网络 | 无持续带宽打满、丢包或错误包异常 |
| 服务器磁盘 | 使用率 `< 85%`，无明显 I/O wait 或磁盘写入阻塞 |
| MySQL | 无连接池耗尽、明显慢查询或锁等待激增 |
| Redis | 无 timeout、明显 latency spike 或 blocked clients |

## StopCondition

触发任一条件时停止加压并进入收尾：

| 指标 | 停止阈值 | 持续时间 | 动作 |
| --- | --- | --- | --- |
| HTTP 5xx 率 | `> 3%` | 连续 2 分钟 | stop_load |
| HTTP 超时率 | `> 3%` | 连续 2 分钟 | stop_load |
| 出价接口 P99 | `> 2000ms` | 连续 2 分钟 | stop_load |
| 商品详情 / 排行榜 P99 | `> 1500ms` | 连续 2 分钟 | hold_stage 或 stop_load |
| Redis Lua P99 | `> 300ms` | 连续 2 分钟 | stop_load |
| WebSocket 连接成功率 | `< 95%` | 当前阶段 | stop_load |
| WebSocket 广播延迟 P95 | `> 1500ms` | 连续 2 分钟 | stop_load |
| WebSocket `time_sync` P95 间隔 | `> 3s` 或持续缺失 | 连续 2 分钟 | stop_load |
| Pod restart / OOM | 任意发生 | 立即 | abort_test |
| 服务器 CPU | `> 90%` 或 load5 持续超过 CPU 核数 | 连续 3 分钟 | stop_load |
| 服务器内存 | `> 90%` 或可用内存持续过低 | 连续 3 分钟 | stop_load |
| 服务器磁盘 | 根分区或关键 PVC 对应磁盘使用率 `> 90%` | 当前阶段 | abort_test |
| 服务器磁盘 I/O | I/O wait、磁盘延迟或写入阻塞明显异常 | 当前阶段 | stop_load |
| 服务器网络 | 网络收发接近瓶颈、错误包/丢包持续异常 | 当前阶段 | stop_load |
| MySQL timeout / 连接池耗尽 | 明显出现 | 当前阶段 | abort_test |
| Redis timeout | 明显出现 | 当前阶段 | abort_test |
| 人工监控者要求停止 | 任意 | 立即 | abort_test |

## ObservabilityPlan

监控采集方为本机 agent。agent 依据 `skills/live-auction-online-ops/SKILL.md` 做线上服务只读观测，默认只执行 `kubectl get`、`kubectl describe`、`kubectl logs`、`kubectl top`、rollout status、Prometheus / Loki / Tempo 查询等读操作；不得修改 k3s 资源、重启服务、扩缩容、回滚、编辑 Secret 或输出敏感值。

每个阶段至少采集：

- Runner：目标 QPS、实际 QPS、并发、总请求、成功、HTTP 失败、业务失败、超时、P50、P95、P99、max。
- HTTP：按 route 统计请求数、状态码、延迟分位。
- 出价业务指标：`auction.bid.count`，按 `result` / `reason` 统计。
- Redis Lua：`auction.place_bid.lua.result.count`、`auction.place_bid.lua.duration`。
- DB：`db.client.operation.count`、`db.client.operation.duration`、慢查询和连接池摘要。
- Order：`order.auction_create.count`。
- K8s：Pod CPU、内存、restart、OOM、日志。
- 服务器 CPU / Load：节点 CPU 使用率、CPU mode 分解、`node_load1`、`node_load5`、`node_load15`、load5 / CPU 核数。
- 服务器内存：内存使用率、可用内存、swap 或 OOM 风险摘要。
- 服务器网络：入站/出站流量、带宽趋势、错误包、丢包或重传异常摘要。
- 服务器磁盘：根分区和关键数据盘/PVC 对应磁盘使用率、可用空间、I/O wait、读写吞吐、磁盘延迟。
- Redis：ops/sec、latency、blocked clients、used memory。
- MySQL：connections、threads、slow queries、lock wait。
- WebSocket：目标连接数、连接成功率、断连率、活跃连接数、出价消息接收延迟、丢消息或慢连接剔除摘要、每秒 `time_sync` 接收数、`time_sync` P50/P95/P99 间隔和持续缺失窗口。

服务器级指标优先通过 Prometheus / node-exporter 查询，参考 `docs/design/k8s-node-resource-observability.md` 中的 CPU、内存、网络、磁盘和 load 指标。如果 node-exporter 或 Prometheus 节点指标不可用，agent 只能使用 `kubectl top nodes`、`kubectl top pods` 和经授权的只读 SSH 系统命令做降级摘要，并在报告中标记“节点指标可信度受限”。

监控证据只记录脱敏摘要，不记录线上完整地址、token、DSN、Redis 密码、Secret 内容或可复用凭据。

## BusinessReconcilePlan

性能压测不是并发一致性专项，但每个阶段结束后必须做轻量业务健康抽样：

- 抽样查询 5-10 个测试拍品详情，确认接口可用并返回当前状态。
- 抽样查询排行榜，确认响应正常且耗时在阈值内。
- 抽样检查成功出价数和 `bid_logs` 写入量是否明显偏离。
- 抽样检查 Redis auction state key 是否存在且未异常丢失。
- 抽样检查 Redis ranking 最高价、商品详情当前价和 `bid_logs` 写入趋势是否明显矛盾。
- 抽样检查 WebSocket 客户端是否收到 `bid_success`、`auction_extended` 或其他预期出价事件，并记录端到端消息延迟摘要。
- 抽样检查 WebSocket 客户端是否持续收到 `time_sync`，记录每阶段每连接平均接收数、P95 接收间隔、最大接收间隔和缺失窗口。

该对账只用于确认压测没有明显业务写入断层，不输出并发一致性结论。

## Runner 资产

正式执行前，按性能指南创建可复跑资产：

```text
docs/agent-testing/performance-runs/<batch_id>/
├── main.go
├── README.md
└── evidence-redacted.md
```

要求：

- `main.go` 来自 `docs/agent-testing/guides/performance/performance-runner.go`。
- `README.md` 记录脱敏复跑方式，不写线上地址、token、DSN、密码或可复用凭据。
- `evidence-redacted.md` 记录脱敏阶段结果、监控摘要、抽样对账和清理结果。
- runner 通过环境变量接收 `PERF_BATCH_ID`、`PERF_ENVIRONMENT`、`PERF_BASE_URL`、`PERF_STOP_FILE`、`PERF_REQUEST_TIMEOUT` 等配置。
- 线上入口、token、Prometheus 地址等敏感值只通过环境变量传入，不写入代码和报告。

本审核草案不包含 runner 代码。审核通过并获得执行授权后，再创建正式 `<batch_id>` 目录。

## 清理策略

压测结束或中止后必须清理或记录未清理原因：

- 本批次测试用户。
- 本批次测试商家。
- 本批次测试房间。
- 本批次测试拍品和规则。
- 本批次保证金、出价日志、订单。
- 本批次 Redis key。
- 临时 WebSocket 连接。
- 临时 port-forward 进程。
- 临时 token，或确认其已过期。

禁止清理非本批次数据，禁止 `DROP DATABASE`、`DROP TABLE`、`TRUNCATE`、`FLUSHALL`、`FLUSHDB`。

## 预计报告输出

```text
压测目标：
压测环境：
压测源：
业务链路模型：
被测接口与业务代码定位：
请求比例：
阶段模型：
性能时间线：
关键事件时间线：
阶段演进分析：
热点接口 / 代码路径分析：
停止条件触发情况：
抽样对账时间线：
清理时间线：
最终结论：passed / failed / stopped / inconclusive
下一步优化建议：
```

报告核心是“基于时间线的性能结果表现”，不是单点最终结果。每个阶段按固定时间窗口记录指标演进，例如每 30 秒或 1 分钟一行。

建议时间线表：

| 时间窗口 | 阶段 | 目标 QPS | 实际 QPS | 目标 WS | 活跃 WS | 出价 TPS | 商品详情 QPS | 排行榜 QPS | P50 | P95 | P99 | HTTP 失败率 | 业务失败率 | 超时率 | Redis Lua P95/P99 | MySQL 摘要 | WebSocket 摘要 | `time_sync` 摘要 | Pod CPU/内存 | 服务器 CPU/内存 | 服务器网络 | 服务器磁盘 | 关键观察 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| T+00:00~00:30 | smoke | 10 |  | 20 |  | 8 | 1 | 1 |  |  |  |  |  |  |  |  |  |  |  |  |  |  |
| T+00:30~01:00 | smoke | 10 |  | 20 |  | 8 | 1 | 1 |  |  |  |  |  |  |  |  |  |  |  |  |  |  |
| T+03:00~04:00 | step_load_1 | 30 |  | 60 |  | 24 | 3 | 3 |  |  |  |  |  |  |  |  |  |  |  |  |  |  |

关键事件时间线单独记录：

| 时间点 | 阶段 | 事件 | 触发指标 | 影响 | 处理动作 |
| --- | --- | --- | --- | --- | --- |
| T+xx:xx | step_load_2 | P99 抬升 | 出价 P99 接近阈值 | 继续观察或 hold_stage | 记录监控摘要 |
| T+xx:xx | step_load_3 | 停止条件触发 | 服务器 CPU 或 Redis Lua 超阈 | 停止加压 | 进入 RECONCILE / CLEANUP |

阶段演进分析按阶段写：

- `smoke`：脚本、认证、测试数据、WebSocket 建连、监控和抽样对账是否可用。
- `step_load_1`：30 QPS / 60 WS 下延迟、错误率、广播和资源是否稳定。
- `step_load_2`：50 QPS / 100 WS 下 Redis Lua、MySQL 写入、排行榜读取和 WS 扇出是否出现趋势变化。
- `step_load_3`：70 QPS / 140 WS 下核心写链路和广播是否出现抖动。
- `step_load_4`：100 QPS / 200 WS 下首个明显瓶颈是否开始显现。
- `step_load_5`：130 QPS / 260 WS 下服务端延迟、错误率、WS 保持和资源水位是否接近停止条件。
- `step_load_6`：150 QPS / 300 WS 下退化表现和首个瓶颈证据。

最终结论只作为时间线分析后的汇总，必须引用具体时间窗口和证据，不能只写一个静态结果。

## 审核决策项

执行前需要人工确认：

1. 被测环境：远程线上、线上等价环境，还是本地 smoke。
2. 服务入口：公网入口、内网入口，还是 `kubectl port-forward`。
3. 压测源：本机 agent 运行项目 performance runner。
4. 最大 HTTP QPS。
5. 最大出价 TPS。
6. 最大 WebSocket 连接数。
7. 压测窗口开始和结束时间。
8. 是否低峰执行。
9. 人工旁路监控者。
10. 是否允许创建本批次测试用户、测试房间、测试拍品和测试保证金。
11. 是否允许执行清理本批次测试数据。
12. 是否授权本机 agent 在压测窗口内执行线上只读监控命令和监控查询。

## 建议第一轮执行边界

建议第一轮只做核心接口受控容量探测：

```text
10/20 WS -> 30/60 WS -> 50/100 WS -> 70/140 WS -> 100/200 WS -> 130/260 WS -> 150/300 WS
```

如果任一阶段已经出现 MySQL `bid_logs` 写入抖动、Redis Lua 延迟升高、WebSocket 广播延迟或断连率升高、商品详情 / 排行榜服务端 P99 破阈、HTTP 5xx / timeout 上升或 Pod 资源过高，应停止继续加压，先定位瓶颈。

当前后端最值得优先观察的瓶颈是：

```text
成功出价后的 Redis Lua 原子更新 + 同步 MySQL `bid_logs` 落库 + WebSocket 同房间广播
```
