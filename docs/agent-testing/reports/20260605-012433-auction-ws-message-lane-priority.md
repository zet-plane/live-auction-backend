# 测试报告：auction-ws-message-lane-priority

## 基本信息

- 测试目标：验证 WS 消息分级与 `time_sync` latest slot 优化上线后，300 WS + 70 QPS fanout 场景下 `time_sync` 是否满足 P95/P99 验收线。
- 测试类型：线上受控回归，`single_source_online`。
- 测试时间：2026-06-05 01:21:33 +0800 至 01:24:33 +0800。
- 批次 ID：`agent_perf_auction_20260605_ws_message_lane_probe`。
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:6ed885f0`。
- runner：`docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`。
- 入口：线上公网 Ingress，完整地址已省略。
- 观测：Prometheus SSH tunnel + 服务器侧 `kubectl` 只读查询。

## Preflight

- backend rollout：success。
- backend Pod：Running，restart 0。
- health：HTTP 200，MySQL ok，Redis ok。
- Prometheus：ready。
- 压测前资源：backend 7m CPU / 13Mi，MySQL 10m / 669Mi，Redis 10m / 82Mi。

## 压测配置

```text
PERF_STAGE_QPS=70
PERF_STAGE_WS=300
PERF_USER_COUNT=340
PERF_REQUEST_MIX=core_bid_80_ranking_10_item_10
PERF_WS_CONNECT_CONCURRENCY=8
PERF_WS_CONNECT_MAX_ATTEMPTS=760
PERF_REQUEST_TIMEOUT=15s
```

## 阶段结果

| 指标 | 结果 |
| --- | ---: |
| Actual QPS | 69.99 |
| WS connected | 300 |
| WS connect failures | 0 |
| Total requests | 12,599 |
| Success | 11,318 |
| Expected business rejects | 1,256 |
| HTTP failures / timeouts | 25 / 25 |
| Error rate / timeout rate | 0.20% / 0.20% |
| Client E2E P95 / P99 | 1.175s / 1.605s |
| `time_sync` count | 54,001 |
| `time_sync` P50 / P95 / P99 / Max | 988ms / 1.456s / 1.982s / 3.550s |

WS event counts during stage:

```text
auction_snapshot=1
bid_success=420,764
time_sync=54,001
user_outbid=7,763
```

Reconcile after stage:

```text
item_detail=ok ranking=ok room=ok ws_connected=300 bid_attempts=10079
ws_events={"auction_snapshot":300,"bid_success":421800,"time_sync":61005,"user_outbid":7774}
```

Cleanup:

```text
closed_ws=300 cancel_item=ok end_room=ok delete_users_attempted=341
```

## Prometheus 指标

| 指标 | Max |
| --- | ---: |
| server HTTP P95 | 84.7ms |
| server HTTP P99 | 96.9ms |
| HTTP RPS | 70.38/s |
| bid RPS | 56.04/s |
| DB ops | 80.62/s |
| bid broadcast flush P95 | 242.5ms |
| bid broadcast bids P95 | 9.77 |
| bid broadcast pending P95 | 4.71 |
| ws active | 300 |
| ws delivery RPS | 2,433.33/s |
| ws write P95 | 0.989ms |
| ws send queue depth P95 | 4.5 |
| `ws_time_sync_write_lag_p95` | 4.528ms |
| backend restarts | 0 |

Lane 明细：

| event_type | lane / reason | result | Max RPS |
| --- | --- | --- | ---: |
| `auction_snapshot` | high | success | 5.46/s |
| `user_outbid` | high | success | 44.87/s |
| `time_sync` | latest | success | 300.00/s |
| `bid_success` | normal | success | 2,433.33/s |

`ws_time_sync_overwrite_rps` 没有样本，说明本轮 latest slot 基本没有发生覆盖积压。

## 结论

- 消息分级行为生效：high / latest / normal 三类 lane 都在 Prometheus 中可见，且没有 delivery dropped。
- 服务端写出链路健康：`ws_write_p95` < 1ms，`ws_time_sync_write_lag_p95` 约 4.5ms，backend restart 0。
- 但是本次没有通过计划的严格 `time_sync` 验收线：目标是 P95 < 1s、P99 < 1.5s；实际为 P95 1.456s、P99 1.982s。
- 这说明服务端 latest slot 和写循环优先级已把服务端排队压低到毫秒级，但客户端观察到的 `time_sync` 间隔仍受广播量、公网链路、客户端读循环或 runner 统计口径影响。

## 建议

- 不建议把本轮标记为完全通过；应标记为“服务端分级生效，但端到端 `time_sync` 未达严格验收线”。
- 下一步优先区分客户端读侧和公网链路：用内网入口或服务器侧压测源复跑同一场景。
- 若仍超过 P95/P99 目标，再考虑降低 `time_sync` 统计口径的端到端噪声，或在客户端/runner 侧记录接收处理延迟。
