# Auction Internal Architecture Capacity Plan

## 状态

- 计划状态：待批准执行。
- 执行授权：未授权。执行前必须由用户在对话中明确批准压测窗口、最大压力边界、命令范围、数据批次和人工监控者。
- 计划批次 ID：`agent_perf_auction_arch_capacity_20260606170223`。
- 目标结论：评估内部 service path / in-cluster / staging-equivalent 下的后端架构上限，不评估公网 HTTPS/WSS 端到端体验。

## 已知背景

公网多 WebSocket 连接下的端到端性能已经确认受外网链路影响。本计划将公网入口噪声从容量结论中剥离，优先使用内网、同集群、同 VPC 或线上等价压测源观测后端、Redis、MySQL 和 WebSocket fanout 的稳定承载能力。

已有脱敏证据显示 service-path 路径曾达到约 `485 HTTP RPS / 388 bid RPS / 2000 active WS`，且 backend restart 为 `0`、严格 `panic|fatal|oom|killed` 日志标记为 `0`。本计划在此基础上继续逼近架构上限，并补足分档停止条件、压测源瓶颈判定和业务对账。

## 读取文档

执行前必须按 progressive disclosure 读取：

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
```

如果写报告，再读取：

```text
docs/agent-testing/reports/README.md
```

项目本地测试技能：

```text
skills/agent-testing-gate/SKILL.md
```

## 测试目标

1. 测出内部 service path 下稳定承载的 HTTP QPS、bid TPS 和 active WS 数。
2. 测出 WebSocket 建连、在线保持、`bid_success` fanout 和 `time_sync` 推送的服务端瓶颈。
3. 识别首个瓶颈属于 backend CPU/内存/GC/goroutine、Redis、MySQL、入口层、压测源还是 WebSocket 写出队列。
4. 验证超过稳定压力后的退化是否可控，不产生错误业务状态。
5. 验证出价结果、排行榜、Redis state、WebSocket payload 和房间在线状态一致。

## 测试范围

- HTTP 出价：`POST /api/v1/items/{item_id}/bids`。
- HTTP 查询：`GET /api/v1/items/{item_id}/ranking`、`GET /api/v1/items/{item_id}`。
- WebSocket ticket：`POST /api/v1/ws-ticket`。
- WebSocket 连接：`GET /ws/v1/rooms/{room_id}?ticket=<ticket>`。
- WebSocket 事件：`bid_success`、`user_outbid`、`auction_extended`、`auction_ended`、`time_sync`。
- Redis：auction state、ranking、bidder names、idempotency key、bid log stream、room online users。
- MySQL：auction item status、bid logs、最终成交字段。

## 禁止范围

- 不使用公网 HTTPS/WSS 结果证明后端架构上限。
- 不修改线上 Deployment、Ingress、Service、Secret、ConfigMap 或镜像。
- 不扩缩容、不重启、不发布、不回滚。
- 不清库、不清表、不执行 `TRUNCATE`、`FLUSHALL` 或 `FLUSHDB`。
- 不操作非本批次数据。
- 不复用真实用户、真实商品、真实支付信息或非测试 token。
- 不在计划、runner、报告或证据中写入线上地址、token、DSN、Redis 密码、kubeconfig、proxy 凭据、完整 ticket 或完整 WebSocket query string。

## PerformanceEnvironment

```text
kind: staging_capacity
service_scope: auction HTTP + WebSocket + Redis + MySQL
deploy_target: online-equivalent or in-cluster service path
entrypoint: service path, cluster-internal address, k8s job, or same-VPC remote runner
k8s_namespace: approved test namespace or production-equivalent namespace, value omitted from report
app_workload: backend workload, exact name omitted from report if sensitive
dependency_scope: real Redis and real MySQL limited to this batch data
observability_stack: Prometheus, logs, kubectl top/get/logs, runner stdout
risk_window: low-traffic approved window
rollback_contact: human monitor supplied during execution approval
```

Allowed conclusion:

- Can support a staging or online-equivalent backend architecture capacity conclusion.
- Cannot prove public domain user experience.
- If load source saturates first, conclusion must be `inconclusive` for backend upper bound and record load-source bottleneck.

## LoadSource

Preferred order:

1. `k8s_job` in the same cluster or same network zone as backend.
2. `remote_machine` in the same VPC or LAN segment.
3. `load_platform` with known network location and capacity.

Minimum load-source evidence:

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
```

The run must record load-source CPU, memory, network, open file limit, connection errors and outbound saturation signals for each stage.

## LoadModel

HTTP mix:

```text
POST /api/v1/items/{item_id}/bids: 80%
GET /api/v1/items/{item_id}/ranking: 10%
GET /api/v1/items/{item_id}: 10%
```

WebSocket model:

```text
logical_user = one test bidder or watcher
physical_ws = control connection + market connection when runner supports control_market
```

### Phase 0: smoke

| Stage | Target QPS | Logical Users | Physical WS | Duration | Purpose |
| --- | ---: | ---: | ---: | --- | --- |
| smoke_50qps_200ws | 50 | 100 | 200 | 2 min | prove script, auth, data, WS and reconcile path |

### Phase 1: QPS ramp with fixed WS

Fixed WS target: `2000 physical WS`.

| Stage | Target QPS | Logical Users | Physical WS | Duration | Variable |
| --- | ---: | ---: | ---: | --- | --- |
| qps_300_ws2000 | 300 | 1000 | 2000 | 3 min | QPS |
| qps_500_ws2000 | 500 | 1000 | 2000 | 3 min | QPS |
| qps_800_ws2000 | 800 | 1000 | 2000 | 3 min | QPS |
| qps_1000_ws2000 | 1000 | 1000 | 2000 | 3 min | QPS |

### Phase 2: WS ramp with fixed QPS

Fixed HTTP target: `500 QPS`.

| Stage | Target QPS | Logical Users | Physical WS | Duration | Variable |
| --- | ---: | ---: | ---: | --- | --- |
| qps500_ws3000 | 500 | 1500 | 3000 | 3 min | WS |
| qps500_ws4000 | 500 | 2000 | 4000 | 3 min | WS |
| qps500_ws6000 | 500 | 3000 | 6000 | 3 min | WS |
| qps500_ws8000 | 500 | 4000 | 8000 | 3 min | WS |

### Phase 3: peak hold

Only execute after Phase 1 and Phase 2 remain healthy.

| Stage | Target QPS | Physical WS | Duration | Purpose |
| --- | ---: | ---: | --- | --- |
| peak_hold | highest healthy combination from Phase 1/2 | highest healthy WS from Phase 1/2 | 10 min | stability and resource trend |

### Optional Phase 4: soak

Only execute in a low-risk staging environment.

| Stage | Target QPS | Physical WS | Duration | Purpose |
| --- | ---: | ---: | --- | --- |
| soak | 70%-80% of stable peak | stable WS target | 30-60 min | GC, memory, goroutine and Redis/MySQL drift |

## Thresholds

Hard stop thresholds:

```text
HTTP 5xx rate: > 1% for 60s
unexpected business failure rate: > 1% for 60s
timeout rate: > 2% for 60s
server_http_p99: > 500ms for 60s
WS connect failure rate: > 2% for a stage
WS write p99: > 100ms for 60s
WS dropped or slow-close count: any sustained increase not explained by planned disconnect
business reconcile: any final-state mismatch
backend pod restart/panic/OOM: any occurrence
Redis timeout or connection pool exhaustion: any occurrence
MySQL timeout, lock wait spike, or connection pool exhaustion: any occurrence
load source CPU: > 85% for 60s, mark load-source bottleneck and hold or stop
load source network saturation: any sustained saturation, mark load-source bottleneck and hold or stop
```

Target pass thresholds at claimed stable capacity:

```text
actual_qps: >= 95% of target_qps
server_http_p95: <= 200ms
server_http_p99: <= 500ms
HTTP error rate: <= 1%
timeout rate: <= 1%
unexpected business failure rate: <= 1%
WS connect success rate: >= 98%
WS write p95: <= 50ms
WS write p99: <= 100ms
business reconcile: all sampled states consistent
backend restarts: 0
panic/fatal/oom/killed strict log markers: 0
```

Client E2E latency is recorded but advisory for this plan. It must not override server-side metrics when deciding backend architecture capacity.

## StopCondition

```text
metric: hard stop threshold breach
threshold: as listed above
duration: listed per metric
action: abort_test
```

```text
metric: load-source bottleneck
threshold: CPU > 85% for 60s or outbound/network/connect saturation
duration: 60s
action: hold_stage, then stop if repeated
```

```text
metric: human monitor stop request
threshold: any explicit stop request
duration: immediate
action: abort_test
```

After any stop condition, runner must stop increasing load, close WebSocket connections, run reconcile, run cleanup and record the stop event.

## ObservabilityPlan

Collect per stage:

- Runner `=== STAGE` output with QPS, concurrency, totals, success, failures, business codes and client latency.
- Prometheus server HTTP RPS, route latency P95/P99 and status code distribution.
- Backend CPU, memory, GC, goroutines and restart count.
- WebSocket active connections, delivery RPS, write latency, queue depth, dropped/slow-close counters and event-type metrics where available.
- Redis CPU, memory, command latency, connection count, timeout/error counters and keyspace summaries for this batch.
- MySQL CPU, memory, connection count, QPS, slow query, lock wait and table write summaries.
- Logs for strict markers: `panic`, `fatal`, `oom`, `killed`.
- Load-source CPU, memory, network, fd usage and connection error summaries.

Evidence format:

```text
runner stdout blocks
redacted Prometheus query summaries
redacted kubectl top/get/logs summaries
redacted business reconcile summaries
```

## BusinessReconcilePlan

Per stage sample at least one active auction item and final post-run state:

- HTTP item detail: current price, leader user, status, bid count, participant count and remaining time.
- HTTP ranking: top bidder and prices.
- Redis state: current price, leader user, end time, bid count, participant count and ranking top.
- MySQL `bid_logs`: sampled successful bid IDs eventually persisted without duplicate primary keys.
- WebSocket: sampled clients receive `bid_success` payload consistent with HTTP and Redis state.
- Room online state: Redis online users and room `online_count` return to expected values after cleanup.

Invariant:

- `current_price`, `leader_user_id`, Redis ranking top, latest successful bid response and sampled `bid_success` payload must agree.
- Non-batch data must not be touched.
- Cleanup must close all batch WebSocket connections and restore room/item state according to cleanup rules.

Failure action:

- Stop the test, preserve evidence, reconcile current state, clean up only this batch, write failure report.

## 测试数据

```text
batch_id: agent_perf_auction_arch_capacity_20260606170223
merchant_prefix: agent_perf_arch_20260606170223_
user_prefix: agent_perf_arch_20260606170223_
room_prefix: room_agent_perf_arch_20260606170223_
item_prefix: item_agent_perf_arch_20260606170223_
idempotency_prefix: agent_perf_arch_20260606170223_
redis_key_scope: only keys for batch-created rooms/items/tickets/idempotency
```

Prepare:

- 1 test merchant.
- Enough test users for the maximum WS target plus bid distribution.
- 1 live test room.
- 1 or more ongoing auction items with rules suitable for high-volume bidding.
- Paid deposit state for bidder users if the rule requires deposit.
- Watcher users for WS fanout load.

## Runner 代码路径

Runner code must be created only after execution approval:

```text
docs/agent-testing/performance-runs/agent_perf_auction_arch_capacity_20260606170223/main.go
docs/agent-testing/performance-runs/agent_perf_auction_arch_capacity_20260606170223/README.md
docs/agent-testing/performance-runs/agent_perf_auction_arch_capacity_20260606170223/evidence-redacted.md
```

Runner requirements:

- Use environment variables for endpoint, token and observability access.
- Do not write sensitive values to stdout or files.
- Support `PERF_STOP_FILE`.
- Output `=== PERF_PLAN`, `=== PREFLIGHT`, `=== STAGE`, `=== STOP_EVENT`, `=== RECONCILE`, `=== CLEANUP`, `=== SUMMARY`.
- Persist redacted stdout summary for each stage.

## 线上窗口

Execution must use an approved low-risk window. The approval message must state start time, end time, maximum QPS, maximum WS connections, maximum duration, command scope and human monitor.

## 命令范围

Allowed after approval:

- Read-only Kubernetes inspection: `rtk kubectl get`, `rtk kubectl describe`, `rtk kubectl logs`, `rtk kubectl top`.
- Read-only Prometheus/log queries.
- Approved load runner command.
- Batch-scoped data preparation, reconciliation and cleanup commands.

Disallowed unless separately approved:

- Any production config, deployment, service, ingress, secret or workload mutation.
- Scaling, restart, rollout, rollback or delete operations.
- Any non-batch cleanup.

## 清理策略

After stop or completion:

- Stop load runner.
- Close all batch WebSocket connections.
- Remove or expire batch WebSocket tickets.
- End/cancel batch auction items as allowed by the test fixture.
- Return batch room to non-live or idle state as allowed by the test fixture.
- Delete or mark inactive batch test users and merchant if supported by current test utilities.
- Remove only batch Redis keys where safe.
- Record any data that cannot be safely removed.
- Confirm backend metrics return to normal baseline.

## 批准方式

Execution approval must be explicit and include:

```text
approved_batch_id: agent_perf_auction_arch_capacity_20260606170223
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
```

Without this approval, the plan may be edited but must not execute.

