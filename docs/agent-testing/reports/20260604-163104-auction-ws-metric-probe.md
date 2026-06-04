# 测试报告：auction-ws-metric-probe

## 基本信息

- 测试目标：部署 `auction.bid_broadcast.*` 指标后，复测 70 QPS bid fanout 下 240/280/300 WS 的退化点，并区分 coalescing、Hub delivery、socket write 三层压力。
- 测试类型：性能压测，`single_source_online`。
- 测试时间：2026-06-04 16:19:13 +0800 至 16:28:27 +0800。
- 执行 agent：Codex 主 agent。
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:988275eb`。
- 观测：Prometheus 临时 SSH tunnel，只读查询。

## 测试数据

- 测试批次 ID：`agent_perf_auction_20260604_ws_metric_probe_fanout`。
- 创建数据：1 个测试商家、1 个测试房间、1 个测试拍品、340 个测试用户。
- 清理结果：`closed_ws=300 cancel_item=ok end_room=ok delete_users_attempted=341`。

## 本轮新增观测

- `auction.bid_broadcast.count`
- `auction.bid_broadcast.duration`
- `auction.bid_broadcast.bids`
- `auction.bid_broadcast.pending`
- runner Prometheus 查询同时补充了 `ws_delivery_count_total`、`ws_write_count_total`、`ws_write_duration_bucket`、`ws_send_queue_depth_bucket`。

## 执行摘要

| WS 目标 | Actual QPS | WS connected | `time_sync` P95 | `time_sync` P99 | HTTP P99 max | 结果 |
| --- | --- | --- | --- | --- | --- | --- |
| 240 | 53.51 | 240 | 2.918s | 4.117s | 98.2ms | 通过但接近 3s 红线 |
| 280 | 55.41 | 280 | 2.794s | 4.159s | 111.1ms | 通过但接近红线 |
| 300 | 66.50 | 300 | 1.801s | 3.020s | 217.8ms | 通过 |

## 关键指标

| 指标 | 240 WS | 280 WS | 300 WS |
| --- | --- | --- | --- |
| `bid_broadcast_flush_p95` | 242.5ms | 242.5ms | 242.5ms |
| `bid_broadcast_bids_p95` | 9.91 | 11.30 | 9.99 |
| `bid_broadcast_pending_p95` | 4.70 | 4.71 | 4.71 |
| `ws_write_p95` max | 0.972ms | 0.981ms | 0.973ms |
| `ws_send_queue_depth_p95` max | 4.5 | 4.5 | 4.5 |
| `ws_delivery_rps` max | 1738.7/s | 2065.8/s | 2398.6/s |
| `backend_restarts` | 0 | 0 | 0 |

## 15 分钟聚合明细

- `auction_bid_broadcast_count_total` increase：`enqueue_create` 约 3848，`enqueue_update` 约 16456，`flush` 约 3848。
- `auction_bid_broadcast_duration_bucket{action="flush"}` P95：242.5ms。
- `auction_bid_broadcast_bids_bucket{action="flush"}` P95：9.67。
- `auction_bid_broadcast_pending_bucket` P95：4.70。
- `ws_delivery_count_total{result="dropped"}`：0。
- `ws_write_duration_bucket` P95：约 0.95ms 到 0.97ms。
- 压测后资源样本：backend 约 3m CPU / 37Mi，MySQL 10m / 669Mi，Redis 8m / 104Mi，otel-collector 2m / 182Mi。

## 结论

- 部署新指标后，本轮 70 QPS + 300 WS 没有复现 `time_sync_p95_interval_gt_3s`；300 WS 阶段 `time_sync` P95 为 1.801s，P99 为 3.020s。
- 当前证据不支持“socket write 慢”是瓶颈：`ws_write_p95` 始终低于 1ms，delivery dropped 为 0，send queue depth P95 最高 4.5。
- 当前证据更支持“bid_success coalescing 与 fanout 批次节奏造成 `time_sync` 尾延迟”：每次 flush P95 合并约 10 个 bid，pending P95 约 4.7，WS delivery/write RPS 随 WS 数量放大，但写入本身仍快。
- 240/280 WS 仍贴近 3s 红线，说明系统有波动；上一轮 280/300 WS 触发过停止，本轮未触发，容量结论应按“边界区间仍需保守”处理。

## 下一步建议

- 在 300 WS + 70 QPS 追加 10 到 15 分钟 soak，验证 `time_sync` P95 是否稳定低于 3s。
- 若仍出现 P95 波动，优先尝试把 `time_sync` 和 `bid_success` 写路径做优先级隔离，避免 `bid_success` fanout 峰值挤压心跳类事件。
- Prometheus 面板增加三组图：`bid_broadcast_bids_p95`、`bid_broadcast_pending_p95`、`ws_write_p95 by event_type`，与 `time_sync` P95 并排看。
