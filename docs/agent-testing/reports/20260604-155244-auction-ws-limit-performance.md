# 测试报告：auction-ws-limit-performance

## 基本信息

- 测试目标：在 `bid_only` 暂停后，探测 WebSocket 连接保持上限，以及 bid fanout 场景下 WS 推送和 `time_sync` 的性能退化点。
- 测试类型：性能压测，`single_source_online`。
- 测试时间：2026-06-04 14:50:00 +0800 至 15:52:44 +0800。
- 执行 agent：Codex 主 agent。
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:d78d1a66`。
- 观测：Prometheus 临时 SSH tunnel，只读查询。

## 测试环境

- 服务地址：线上入口，完整地址已省略。
- MySQL：线上 MySQL，地址和凭据已省略。
- Redis：线上 Redis，地址和凭据已省略。
- WebSocket：线上 WebSocket 入口。
- 测试 runner：`docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`。

## 测试数据

- hold 批次：`agent_perf_auction_20260604_ws_limit_probe_hold`。
- fanout 失败清理批次：`agent_perf_auction_20260604_ws_limit_probe_fanout`。
- fanout 探针批次：`agent_perf_auction_20260604_ws_limit_probe_fanout2`。
- fanout 夹逼批次：`agent_perf_auction_20260604_ws_limit_probe_fanout_bracket`。
- 每个批次均创建独立测试商家、房间、拍品和测试用户。

## Runner 变更

- 新增 `PERF_STAGE_WS`，用于自定义每档 WS 目标数。
- 新增 `PERF_REQUEST_MIX=item_only`，用于低 HTTP 干扰的 WS hold 测试。
- 新增 `PERF_CLEANUP_ONLY=true`，用于 setup 中途失败后按批次清理测试账号、房间和拍品。
- 本地验证通过：`go test ./docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws ./internal/app/ws/hub ./internal/app/item/service`。

## 执行摘要

| 场景 | 目标 | 结果 | 结论 |
| --- | --- | --- | --- |
| WS hold | 200/300/400/500 WS，5 QPS item-only | 500 WS 连接成功，连接失败 0，P95/P99 约 1.28s/1.51s | 纯连接保持和 `time_sync` 到 500 WS 基本稳定 |
| fanout 首探 | 300 WS + 70 QPS 混合流量 | `time_sync` P95 3.60s，P99 5.15s，触发停止 | 300 WS 在 bid fanout 下不稳 |
| fanout 夹逼 | 200/240/280 WS + 70 QPS 混合流量 | 240 WS 稳定，280 WS 触发停止 | 70 QPS fanout 下 WS 稳定边界约在 240 到 280 WS 之间 |

## 关键证据

### WS Hold

| WS 目标 | 实际连接 | WS 连接失败 | `time_sync` 数量 | P95 | P99 | Max |
| --- | --- | --- | --- | --- | --- | --- |
| 200 | 200 | 0 | 36000 | 1.127s | 1.318s | 2.255s |
| 300 | 300 | 0 | 54000 | 1.241s | 1.384s | 3.289s |
| 400 | 400 | 0 | 72000 | 1.225s | 1.358s | 2.381s |
| 500 | 500 | 0 | 89822 | 1.283s | 1.513s | 10.495s |

### Fanout 首探

| 指标 | 300 WS + 70 QPS |
| --- | --- |
| WS connected | 300 |
| WS connect failures | 0 |
| Actual QPS | 45.69 |
| `time_sync` P50/P95/P99/Max | 1.258s / 3.605s / 5.154s / 9.112s |
| Client E2E P95/P99 | 3.410s / 5.476s |
| Server HTTP P95/P99 max | 67.6ms / 119.2ms |
| `ws_active` max/last | 300 / 280 |
| Stop reason | `time_sync_p95_interval_gt_3s` |

### Fanout 夹逼

| WS 目标 | Actual QPS | WS connected | `time_sync` P95 | `time_sync` P99 | Server HTTP P95 max | Server HTTP P99 max | 结果 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 200 | 53.89 | 200 | 2.871s | 4.703s | 117.4ms | 465.9ms | 通过但接近红线 |
| 240 | 69.99 | 240 | 1.269s | 1.661s | 117.4ms | 444.0ms | 稳定 |
| 280 | 52.08 | 280 | 3.107s | 4.092s | 39.1ms | 161.6ms | 触发停止 |

## 结论

- 纯 WS hold 不是当前首要瓶颈：低 HTTP 干扰下 500 WS 可以保持，连接失败为 0。
- bid fanout 是 WS 退化触发器：70 QPS 混合流量下，280 到 300 WS 会让 `time_sync` P95 超过 3s。
- 服务端 HTTP 总体延迟不是 fanout 失败主因：280/300 WS 失败时，Prometheus 里的 server HTTP P95/P99 仍处在几十到一百多毫秒级。
- 连接建立也不是主因：失败档 WS connect failures 均为 0，`ws_active` 基本能达到目标。
- 更可能的瓶颈在 WS 写入/广播路径：`bid_success` fanout 量很大，`time_sync` 被挤压，表现为实际 QPS 下滑、客户端 E2E 延迟上升和 `time_sync` 间隔扩大。

## 清理结果

- `agent_perf_auction_20260604_ws_limit_probe_hold`：`closed_ws=500 cancel_item=ok end_room=ok delete_users_attempted=561`。
- setup 中途失败批次 `agent_perf_auction_20260604_ws_limit_probe_fanout`：cleanup-only 清理结果为 `merchant_login=ok batch_items_seen=1 cancel_ok=1 cancel_err=0 end_room=ok user_login_ok=267 user_delete_ok=267 user_delete_err=0 user_accounts_scanned=360 delete_merchant=ok`。
- `agent_perf_auction_20260604_ws_limit_probe_fanout2`：`closed_ws=300 cancel_item=ok end_room=ok delete_users_attempted=361`。
- `agent_perf_auction_20260604_ws_limit_probe_fanout_bracket`：`closed_ws=280 cancel_item=ok end_room=ok delete_users_attempted=321`。

## 建议下一步

- 给 WS 广播链路补分段指标：enqueue、coalesce flush、per-connection write latency、write queue depth、dropped/timeout count、active writer goroutine 数。
- 把 `time_sync` 和 bid broadcast 的写入队列隔离，避免高频 `bid_success` fanout 挤压心跳类消息。
- 继续验证 240 WS + 70 QPS 的短时回归门：`time_sync` P95 < 2s，WS connect failures = 0，backend restart = 0。
- 在实现分段指标后再复测 260/280/300 WS，确认瓶颈是在消息生成、广播调度、单连接写锁还是客户端读侧。
