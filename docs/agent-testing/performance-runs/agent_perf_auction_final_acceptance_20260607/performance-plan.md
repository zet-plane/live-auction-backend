# Auction Final Acceptance Performance Plan

## 状态

- 计划状态：待批准执行。
- 执行授权：未授权。执行前必须由用户在对话中明确批准压测窗口、最大压力边界、命令范围、数据批次、清理权限和人工监控者。
- 计划批次 ID：`agent_perf_auction_final_acceptance_20260607`
- 目标结论：验收当前后端在多实例、Redis 热路径 / 缓存预热、WebSocket control / market 分层后的最终承载能力和退化边界。
- 结论边界：`staging_capacity` 才能形成后端架构容量结论；`single_source_online` 只形成公网体验和入口路径风险结论。

## 历史观测摘要

已参考的历史计划和报告：

- `docs/agent-testing/performance-runs/agent_perf_auction_arch_capacity_20260606170223/performance-plan.md`
- `docs/agent-testing/reports/20260606-033413-auction-skip-tls-performance.md`
- `docs/agent-testing/reports/20260606-135000-auction-public-domain-performance.md`
- `docs/agent-testing/reports/20260607-015515-backend-multipod-local-flow.md`
- `docs/agent-testing/reports/20260604-141143-auction-http-bid-performance.md`
- `docs/agent-testing/reports/20260604-211734-auction-bid-async-probe.md`
- `docs/agent-testing/reports/20260605-auction-ws-connection-stability.md`
- `docs/agent-testing/reports/20260605-012433-auction-ws-message-lane-priority.md`
- `docs/agent-testing/reports/20260606-ws-connect-path-diagnosis.md`

已有事实：

1. 本地双实例已经验证 Redis event bus 可把 producer 实例上的 `auction_started`、`bid_success`、`auction_ended` 和 `order_created` 投递到 subscriber 实例上的 WebSocket 客户端，且最终 HTTP 状态可恢复。
2. 当前 k8s manifest 已配置 backend `replicas: 3`、rolling update、`/livez`、`/readyz` 和 pod identity 环境变量。
3. bid-only 同步 MySQL 版本在 500 QPS 附近出现服务端延迟拐点；异步 bid log stream 后，500 QPS 下服务端 P99 已明显降低，瓶颈不再是同步 `bid_logs` 落库。
4. skip-TLS service path 历史高点曾达到约 `485 HTTP RPS / 388 bid RPS / 2000 active WS`，backend restart 为 0，严格 `panic|fatal|oom|killed` 日志标记为 0。
5. 本机 public HTTPS/WSS 压测在 500 QPS 和 public WSS 建连尾延迟上先触发停止；该结论不能证明 backend 上限。
6. WS control / market 分流、high / latest / normal lane 生效；服务端 WS write 和 `time_sync` write lag 处于毫秒级。公网端到端 `time_sync` 到达尾延迟仍会受 public path、压测源和客户端读循环影响。

本计划补足的验收缺口：

- 真实 k8s 3 副本下的 service-path 容量。
- 跨 pod fanout / unicast 在高压下的持续正确性。
- Redis hot state、ranking rebuild 合并、bid log stream、worker lag 和 cleanup 的性能证据。
- WS 分层在千级至万级物理连接下的控制面和行情面隔离效果。
- public HTTPS/WSS 体验复测，但不把它写成后端容量上限。

## 必须读取文档

执行前按 progressive disclosure 读取：

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

如执行后写报告，再读取：

```text
docs/agent-testing/reports/README.md
```

项目本地技能：

```text
skills/agent-testing-gate/SKILL.md
skills/live-auction-online-ops/SKILL.md
```

## 测试目标

1. 验证 3 副本 backend 在 service path / in-cluster / same-VPC 压测源下的稳定 HTTP QPS、bid TPS 和 active WS 数。
2. 验证高压下跨 pod room fanout、user unicast、WS event bus publish / dispatch 和最终 HTTP 状态恢复正确。
3. 验证 Redis hot state 命中、ranking rebuild 合并、bid log stream pending / lag 和 worker batch 写入不会成为新的雪崩点。
4. 验证 control / market WS 分层：control 保持 `time_sync`、snapshot、outbid、ended、order 事件；market 承载 `bid_success` 高 fanout。
5. 验证超出稳定压力后系统按停止条件可控停止，不产生错误业务状态。
6. 复测 public HTTPS/WSS 的用户侧体验，单独记录 public path 风险，不混入 backend 架构容量结论。

## 测试范围

- HTTP 出价：`POST /api/v1/items/{item_id}/bids`
- HTTP 查询：`GET /api/v1/items/{item_id}/ranking`、`GET /api/v1/items/{item_id}`、`GET /api/v1/rooms/{room_id}`
- WebSocket ticket：`POST /api/v1/ws-ticket`
- WebSocket 连接：`GET /ws/v1/rooms/{room_id}?ticket=<ticket>&stream=control|market`
- WebSocket 事件：`auction_snapshot`、`time_sync`、`auction_started`、`bid_success`、`user_outbid`、`auction_extended`、`auction_ended`、`order_created`
- Redis：auction state、ranking、bidder names、idempotency key、bid log stream、dead stream、room online users、ranking rebuild lock / cooldown、WS event bus
- MySQL：`auction_items`、`auction_rules`、`bid_logs`、订单抽样状态
- k8s：backend 3 副本 readiness、per-pod HTTP / WS / event bus / resource 指标

## 禁止范围

- 不修改线上 Deployment、Ingress、Service、Secret、ConfigMap 或镜像。
- 不扩缩容、不重启、不发布、不回滚。
- 不清库、不清表、不执行 `TRUNCATE`、`FLUSHALL` 或 `FLUSHDB`。
- 不操作非本批次数据。
- 不复用真实用户、真实商品、真实支付信息或非测试 token。
- 不在计划、runner、报告或证据中写入线上地址、token、DSN、Redis 密码、kubeconfig、proxy 凭据、完整 ticket 或完整 WebSocket query string。
- 不用 public HTTPS/WSS 本机单源结果证明后端架构容量上限。
- 不把 WebSocket 消息当作最终事实来源；最终状态必须与 HTTP / Redis / MySQL 对账。

## PerformanceEnvironment

主验收环境：

```text
kind: staging_capacity
service_scope: auction HTTP + WebSocket + Redis + MySQL + observability
deploy_target: 3 backend replicas, online-equivalent or production-like k8s
entrypoint: in-cluster service path, same-VPC remote runner, or approved k8s job
k8s_namespace: approved namespace, omitted from report if sensitive
app_workload: live-auction-backend, exact live value may be redacted
dependency_scope: real Redis and real MySQL limited to this batch data
observability_stack: Prometheus, logs, kubectl top/get/logs, runner stdout
risk_window: approved low-traffic window
rollback_contact: human monitor supplied during approval
```

公网体验复测环境：

```text
kind: single_source_online
service_scope: public HTTPS/WSS path
entrypoint: public domain, exact value omitted
load_source: one approved local or remote source
allowed_conclusion: public path experience and risk only
```

## LoadSource

主验收优先级：

1. `k8s_job`：同集群压测 Job，最适合 service-path 容量验收。
2. `remote_machine`：同 VPC / 同机房远端压测源。
3. `load_platform`：明确规格和网络位置的压测平台。

不得用不稳定 SSH local tunnel 作为高压容量结论来源。

每次执行必须记录：

```text
kind:
count:
cpu:
memory:
network_location:
outbound_identity: redacted
tool:
max_supported_qps:
known_limit:
open_file_limit:
load_source_cpu:
load_source_memory:
load_source_network:
connection_error_summary:
```

## 测试数据

```text
batch_id: agent_perf_auction_final_acceptance_20260607
merchant_prefix: agent_perf_final_20260607_
user_prefix: agent_perf_final_20260607_
room_prefix: room_agent_perf_final_20260607_
item_prefix: item_agent_perf_final_20260607_
idempotency_prefix: agent_perf_final_20260607_
redis_key_scope: only keys for batch-created rooms/items/tickets/idempotency/rebuild locks
```

准备：

- 1 个测试商家。
- 至少达到最大 logical users 的测试用户。
- 1 个 live 测试房间。
- 至少 2 个 ongoing 测试拍品：
  - `hot_item`：用于预热后主压测。
  - `cold_rebuild_item`：用于受控 ranking rebuild / hot state 恢复探针。
- 主压测商品建议 `deposit_amount=0`，避免保证金接口成为高压主路径噪声。
- 如需覆盖保证金语义，使用单独低 QPS 抽样或提前完成本批次用户保证金，不把支付链路放入高压流量。

## LoadModel

HTTP mix：

```text
POST /api/v1/items/{item_id}/bids: 80%
GET /api/v1/items/{item_id}/ranking: 10%
GET /api/v1/items/{item_id}: 10%
```

WebSocket model：

```text
PERF_WS_STREAM_MODE=control_market
logical_user = one test user
physical_ws = logical_user * 2
control stream = time_sync / snapshot / outbid / ended / order
market stream = bid_success
```

WS 建连策略：

```text
PERF_WS_CONNECT_CONCURRENCY: start at 8, may increase only inside approved plan
PERF_WS_UPGRADE_BATCH_SIZE: use jittered batches
PERF_WS_UPGRADE_BATCH_INTERVAL: record value in evidence
PERF_WS_CONNECT_TIMEOUT: 15s unless approval narrows it
```

### Phase 0: preflight

目标：证明环境、版本、3 副本、监控、runner、数据和停止开关可用。

必须完成：

- backend ready replicas 为 `3/3`。
- `/livez`、`/readyz` 可用，MySQL / Redis component 为 ok。
- 记录 backend image / commit 摘要。
- Prometheus ready，日志可查，`kubectl top/get/logs` 可查。
- backend restart baseline 为 0 或记录已有值并在阶段间对比不增加。
- strict log marker baseline：`panic|fatal|oom|killed` 为 0。
- 压测源 CPU / memory / fd / network baseline 可查。
- STOP 文件机制可用。

### Phase 1: smoke

| Stage | Target QPS | Logical Users | Physical WS | Duration | Purpose |
| --- | ---: | ---: | ---: | --- | --- |
| smoke_50qps_200ws | 50 | 100 | 200 | 2 min | 验证脚本、认证、预热、WS 分层、event bus、对账和 cleanup |

通过后才进入 Phase 2。

### Phase 2: cache warm and hot-path readiness

目标：证明主压测开始前 hot item 已进入稳定热路径。

步骤：

1. 创建并开始 `hot_item`。
2. 调用 item detail、ranking、room detail，打开 control / market WS。
3. 进行 30-60 秒 `50 QPS / 200 physical WS` 预热。
4. 采集：
   - `auction.hot_state.lookup.count{result=hit|miss|rebuilt|error}`
   - ranking rebuild lock / cooldown 指标或日志摘要
   - Redis state / ranking / bidder names key 存在性
   - bid log stream append / worker batch / pending / lag
5. 进入 Phase 3 前必须满足：
   - hot state lookup error 为 0。
   - 预热后 hot state hit ratio >= 99%。
   - bid log stream append error 为 0。
   - bid log dead-letter 增量为 0。
   - worker pending 不持续增长，并在预热结束后可回落。

### Phase 3: QPS ramp with fixed WS

固定：

```text
logical users = 1500
physical WS = 3000
```

| Stage | Target QPS | Logical Users | Physical WS | Duration | Variable |
| --- | ---: | ---: | ---: | --- | --- |
| qps_300_ws3000 | 300 | 1500 | 3000 | 3 min | QPS |
| qps_500_ws3000 | 500 | 1500 | 3000 | 3 min | QPS |
| qps_700_ws3000 | 700 | 1500 | 3000 | 3 min | QPS |
| qps_900_ws3000 | 900 | 1500 | 3000 | 3 min | QPS |
| qps_1100_ws3000 | 1100 | 1500 | 3000 | 3 min | QPS |

进入下一档条件：

- 当前档未触发 hard stop。
- actual QPS >= target QPS 的 95%，或未达到时有明确压测源瓶颈证据。
- server HTTP P99、WS write P99、Redis、MySQL、stream pending 均健康。
- 每档业务对账通过。

### Phase 4: WS ramp with fixed QPS

固定：

```text
target QPS = 500
```

| Stage | Target QPS | Logical Users | Physical WS | Duration | Variable |
| --- | ---: | ---: | ---: | --- | --- |
| ws_2000 | 500 | 1000 | 2000 | 3 min | WS |
| ws_4000 | 500 | 2000 | 4000 | 3 min | WS |
| ws_6000 | 500 | 3000 | 6000 | 3 min | WS |
| ws_9000 | 500 | 4500 | 9000 | 3 min | WS |

进入下一档条件同 Phase 3，额外要求：

- WS connect success rate >= 98%。
- per-pod WS active 分布有证据；任一 pod 低于总连接 15% 时记录 LB 倾斜并分析。
- control stream 不接收 market-only `bid_success`。
- market stream 不接收 control-only `time_sync` / `auction_ended` / `order_created`。

### Phase 5: multi-pod event bus acceptance

在 Phase 3 或 Phase 4 的健康档位中抽样执行，不单独扩大压力。

必须采集：

- 每个 backend pod 的 HTTP RPS、WS active、event bus publish / dispatch 计数。
- `ws:event:room` 和 `ws:event:user` publish / dispatch success 增量。
- 至少 20 个抽样 `bid_success` 事件的 event ID / auction version 摘要，确认连接在不同 pod 的客户端均收到。
- 至少 5 个 `user_outbid` 或 `order_created` 单播事件摘要，确认连接所在 pod 与业务写入 pod 不同时仍可收到。
- 随机断开并重连至少 20 条 control stream，确认新 pod 连接后收到 `auction_snapshot`，且 HTTP item / ranking 为最终事实。

通过标准：

- room fanout 不因 pod 分布丢失。
- user unicast 不因 pod 分布丢失。
- event bus dispatch error 为 0。
- backend restart 增量为 0。
- 抽样事件 payload 的 `current_price`、`leader_user_id`、`auction_version` 与 HTTP / Redis 对账一致。

### Phase 6: controlled cold rebuild probe

目标：只对本批次 `cold_rebuild_item` 验证 ranking rebuild 和 hot state 恢复不会跨 pod 放大。

执行边界：

- 只能操作本批次 item 的 Redis keys。
- 不删除非本批次 key。
- 不在 public path 执行。

步骤：

1. 对 `cold_rebuild_item` 准备一批已成功出价，并等待 bid log worker 将本批次 BidLog 持久化。
2. 在批准命令范围内移除或过期本批次 ranking key / hot state key 的指定部分。
3. 从多 pod service path 并发请求 ranking 和少量 bid。
4. 采集 ranking rebuild lock acquire / denied、MySQL ranking query RPS、cooldown、HTTP ranking 响应和最终 Redis ranking。

通过标准：

- 同一 item 同一窗口内只有一个 pod 执行 MySQL rebuild，其他 pod 等待或读取恢复后的 Redis ranking。
- MySQL ranking query 不随 pod 数或并发数线性放大。
- rebuild 后 Redis ranking、HTTP ranking 和 MySQL `bid_logs` 聚合一致。
- cooldown 生效，空窗口或错误窗口不会导致连续重建。

### Phase 7: peak hold

仅在 Phase 3 和 Phase 4 至少各有一个健康高档后执行。

| Stage | Target QPS | Physical WS | Duration | Purpose |
| --- | ---: | ---: | --- | --- |
| peak_hold | highest healthy QPS, capped by approval | highest healthy WS, capped by approval | 10 min | 稳定性、资源趋势、stream lag、event bus 和 cleanup 观测 |

通过标准：

- 不触发 hard stop。
- server HTTP P95/P99、WS write、Redis/MySQL、worker pending/lag 没有持续劣化趋势。
- 业务对账每 2 分钟至少一次，全部通过。
- backend restart、panic/fatal/OOM/killed 增量为 0。

### Phase 8: public path experience check

目标：复测 public HTTPS/WSS 用户侧体验，单独输出 `single_source_online` 结论。

| Stage | Target QPS | Logical Users | Physical WS | Duration | Purpose |
| --- | ---: | ---: | ---: | --- | --- |
| public_smoke | 50 | 100 | 200 | 2 min | public HTTPS/WSS 可用性 |
| public_150 | 150 | 400 | 800 | 3 min | public path 低压体验 |
| public_300 | 300 | 400 | 800 | 3 min | public path 中压体验 |

判定：

- public 阶段不得作为 backend 容量上限。
- 必须按接口分别输出 public client E2E P95/P99：`bid`、`ranking`、`item_detail`，如 request mix 包含其他接口也必须同步列出。
- 必须按 route 分别输出 service-side HTTP P95/P99：`/api/v1/items/{item_id}/bids`、`/api/v1/items/{item_id}/ranking`、`/api/v1/items/{item_id}`。
- 如果某个接口的 public client P99 或 WSS connect P99 劣化，而对应 service-side route 指标健康，结论归为 public path / load source 风险。
- 若 public client 和对应 server-side route 同步劣化，记录服务端风险并停止。

## Thresholds

主验收 claimed stable capacity 的通过阈值：

```text
actual_qps: >= 95% of target_qps
server_http_p95: <= 200ms
server_http_p99: <= 500ms
http_error_rate: <= 1%
timeout_rate: <= 1%
unexpected_business_failure_rate: <= 1%
ws_connect_success_rate: >= 98%
ws_write_p95: <= 50ms
ws_write_p99: <= 100ms
ws_send_queue_depth_p95: stable, no sustained growth
ws_dropped_or_slow_close: 0 unexpected sustained increase
control_time_sync_write_lag_p95: <= 20ms
event_bus_dispatch_error: 0
hot_state_hit_ratio_after_warm: >= 99%
hot_state_rebuild_error: 0
ranking_rebuild_stampede: 0 observed
bid_log_stream_append_error: 0
bid_log_worker_dead_letter_delta: 0
bid_log_worker_pending: drains after stage and does not grow monotonically
business_reconcile: all sampled states consistent
backend_restarts_delta: 0
strict_log_markers: 0
```

Client E2E latency and public WSS connect latency must be recorded. Client E2E must be split by endpoint for every request mix. Service-side HTTP latency must be split by route/method/status where metrics allow it. In service-path capacity phases, client E2E is advisory unless it correlates with matching server-side route metrics or load-source saturation.

Hard stop thresholds:

```text
HTTP 5xx rate: > 1% for 60s
unexpected business failure rate: > 1% for 60s
timeout rate: > 2% for 60s
server_http_p99: > 500ms for 60s
WS connect failure rate: > 2% for a stage
WS write p99: > 100ms for 60s
event bus dispatch errors: any sustained increase
bid log stream append errors: any occurrence
bid log dead-letter delta: any occurrence
worker pending or lag: continuous growth for a full stage
business reconcile: any final-state mismatch
backend pod restart / panic / OOM: any occurrence
Redis timeout or connection pool exhaustion: any occurrence
MySQL timeout, lock wait spike, or connection pool exhaustion: any occurrence
load source CPU: > 85% for 60s, mark load-source bottleneck and hold or stop
human monitor stop request: immediate abort_test
```

## ObservabilityPlan

每档采集：

- Runner `=== STAGE` 输出：target QPS、actual QPS、concurrency、total、success、HTTP failures、business failures、timeouts、client latency、status codes、business codes。
- Runner client E2E by endpoint：`bid`、`ranking`、`item_detail`、`room_detail` 等所有 request mix endpoint 的 P50/P95/P99/max、请求数、失败数、超时数、状态码和业务码。
- Prometheus server HTTP by route：和 request mix endpoint 对齐的 route/method/status RPS、P50/P95/P99，至少 P95/P99。
- Prometheus 全局 server HTTP P95/P99 只作为背景信号，不得替代业务接口 per-route 结论。
- Per-pod backend CPU、memory、GC、goroutines、restart count。
- Per-pod HTTP RPS、WS active、WS lifecycle accepted / closed。
- WS delivery RPS、write latency、send queue depth、dropped / slow-close、event type、stream、lane。
- WS event bus publish / dispatch count by scope / type / result。
- Redis CPU、memory、command latency、connection count、timeout/error counters、keyspace summaries for this batch。
- MySQL CPU、memory、connection count、QPS、slow query、lock wait、table write summaries。
- Bid log stream append rate、worker batch rate、pending count、lag seconds、dead-letter count。
- Hot state lookup hit / miss / rebuilt / error。
- Ranking rebuild lock acquire / denied / cooldown。
- Logs for strict markers：`panic`、`fatal`、`oom`、`killed`。
- Load-source CPU、memory、network、fd usage、connection errors。

证据格式：

```text
runner stdout blocks
redacted Prometheus query summaries
redacted kubectl top/get/logs summaries
redacted business reconcile summaries
redacted cleanup summaries
```

## BusinessReconcilePlan

每个阶段至少抽样：

- HTTP item detail：status、current price、leader user、deal price、bid count、participant count、remaining time。
- HTTP ranking：top bidder、top price、current user sample。
- HTTP room detail：status、current item、online count。
- Redis state：current price、leader user、end time、bid count、participant count、ranking top。
- MySQL `bid_logs`：本批次抽样 bid IDs 最终存在且不重复。
- Redis stream：本批次 pending / dead-letter 不异常。
- WebSocket：抽样 control / market 客户端收到的事件与 HTTP / Redis 状态一致。
- Room online state：cleanup 后 Redis online users 和 room online count 回到预期。

核心不变量：

- `current_price`、`leader_user_id`、Redis ranking top、最新成功出价响应和抽样 `bid_success` payload 必须一致。
- control stream 和 market stream 的事件隔离必须符合 `modules/ws.md`。
- 成交后 `auction_ended`、HTTP item、Redis final snapshot 和订单抽样状态必须一致。
- 非本批次数据不得被影响。
- cleanup 后 batch WebSocket 关闭，room/item 进入清理策略规定状态。

## SubagentExecutionPlan

默认：

```text
mode: main_agent_only for plan review
execution mode after approval: main_agent_with_subagents is recommended
shared_batch_id: agent_perf_auction_final_acceptance_20260607
stop_signal: PERF_STOP_FILE
```

批准执行后建议角色：

- `preflight`：环境、版本、3 副本、监控和数据准备。
- `load`：运行 performance runner。
- `monitor`：采集 Prometheus / kubectl / logs。
- `recorder`：整理 stage evidence 和脱敏输出。
- `cleanup`：关闭 WS、清理本批次数据、确认指标回落。

主 agent 必须复核所有 subagent 结论后才能写最终通过、失败或风险结论。

## Runner 代码路径

Runner 代码只在执行批准后创建或更新：

```text
docs/agent-testing/performance-runs/agent_perf_auction_final_acceptance_20260607/main.go
docs/agent-testing/performance-runs/agent_perf_auction_final_acceptance_20260607/README.md
docs/agent-testing/performance-runs/agent_perf_auction_final_acceptance_20260607/evidence-redacted.md
docs/agent-testing/performance-runs/agent_perf_auction_final_acceptance_20260607/runner-output-redacted.log
```

Runner 必须支持：

- `PERF_STOP_FILE`
- control / market split stream
- per-route latency
- per-event WS arrival latency
- per-stream and per-lane WS event counters
- public path and service path modes
- stage-level stdout blocks
- business reconcile
- batch-scoped cleanup

Runner 输出块：

```text
=== PERF_PLAN
=== PREFLIGHT
=== STAGE: <stage_name>
=== OBSERVABILITY
=== RECONCILE
=== STOP_EVENT
=== CLEANUP
=== SUMMARY
```

## 线上窗口

执行必须使用已批准低风险窗口。批准消息必须包含开始时间、结束时间、最大 QPS、最大 physical WS、最大持续时长、命令范围、人工监控者和 cleanup 授权。

## 命令范围

批准后默认允许：

- 只读 Kubernetes 检查：`rtk kubectl get`、`rtk kubectl describe`、`rtk kubectl logs`、`rtk kubectl top`。
- 只读 Prometheus / 日志查询。
- 已批准的压测 runner 命令。
- 仅作用于本批次的数据准备、对账和清理命令。

默认禁止，除非单独批准：

- 修改 production config、deployment、service、ingress、secret 或 workload。
- scaling、restart、rollout、rollback 或 delete workload。
- 任何非本批次 cleanup。

## 清理策略

完成或中止后必须：

- 停止 load runner。
- 关闭所有 batch WebSocket 连接。
- 删除或等待过期本批次 WebSocket ticket。
- 取消或结束 batch auction items。
- 将 batch room 退回非 live / idle 状态。
- 删除或标记本批次测试用户和商家，按当前接口能力记录成功数和失败数。
- 只清理本批次 Redis keys。
- 确认 bid log stream / pending / dead-letter 中无本批次异常残留，或记录未清理原因。
- 确认 backend、Redis、MySQL 和节点指标回落。
- 保留 runner 代码和脱敏证据。

## 批准方式

执行批准必须显式包含：

```text
approved_batch_id: agent_perf_auction_final_acceptance_20260607
environment_kind:
entrypoint_kind:
load_source:
window:
max_qps:
max_physical_ws:
max_duration:
allowed_commands:
human_monitor:
cleanup_allowed:
public_path_check_allowed:
subagent_mode:
```

未获得批准前，本计划只能编辑和评审，不得创建线上测试数据、连接真实依赖、发起 HTTP / WebSocket 请求或启动压测。

## 报告结论格式

最终报告必须分别给出：

- `staging_capacity` 后端架构容量结论：稳定 QPS、稳定 bid TPS、稳定 physical WS、首个瓶颈、资源水位、业务对账。
- 多实例结论：3 副本 readiness、per-pod 分布、event bus publish / dispatch、跨 pod fanout / unicast、reconnect snapshot。
- 缓存热路径结论：hot state hit、ranking rebuild 合并、bid log stream pending / lag、dead-letter。
- WS 分层结论：control / market 隔离、lane 指标、server write、control time sync。
- public path 体验结论：public HTTPS/WSS client E2E、WSS connect、入口路径风险。
- cleanup 结论：已清理、未清理、未清理原因和人工建议。

结论枚举只能使用：

```text
passed
failed
stopped
inconclusive
skipped
```
