# 诊断报告：100 QPS / 200 WS time_sync_missing_or_low_rate

## 1. 场景

- 测试批次：`agent_perf_auction_20260603_core_bid_ws`
- 测试环境：`single_source_online`
- 后端镜像：`ghcr.io/zet-plane/live-auction-backend:ba7098c5`
- 压测阶段：`step_100qps_200ws`
- 阶段目标：
  - HTTP 目标 QPS：`100`
  - WebSocket 目标连接数：`200`
  - 单阶段持续时间：`3min`
- HTTP 请求比例：
  - 核心出价写链路 `POST /api/v1/items/{item_id}/bids`：`80%`
  - 排行榜读取 `GET /api/v1/items/{item_id}/ranking`：`10%`
  - 商品详情/当前价读取 `GET /api/v1/items/{item_id}`：`10%`
- WebSocket 模型：
  - 200 个同房间连接。
  - 每个连接接收出价广播、用户被超价事件、拍卖快照和每秒 `time_sync`。
- 判停条件：
  - `time_sync` 接收数量低于预期阈值，或
  - `time_sync` P95 接收间隔超过 3s。

本轮关注的是核心出价写链路在 Redis、MySQL 和 WebSocket 同步参与时的瓶颈方向。`time_sync` 是服务端每秒广播的房间级心跳/校时事件，因此它可以暴露 WebSocket 广播链路是否被出价事件挤压。

## 2. 现象

100 QPS / 200 WS 阶段触发 runner 判停：

- 停止原因：`time_sync_missing_or_low_rate`
- 实际 HTTP QPS：`88.34`，低于目标 `100`
- WebSocket 连接：阶段开始已达到 `200/200`
- `time_sync` 实际接收数量：`17318`
- `time_sync` 理论接收数量：约 `36000`，即 `200 WS * 180s`
- `time_sync` P50 接收间隔：`1.662s`
- `time_sync` P95 接收间隔：`4.602s`
- `time_sync` P99 接收间隔：`13.248s`
- `time_sync` 最大接收间隔：`20.748s`
- HTTP failure / timeout：`229`
- HTTP error rate / timeout rate：`1.44%`
- 客户端端到端 P99：`3.527s`

服务端 WebSocket 活跃连接数出现明显下滑：

- `ws_connection_active` max：`201`
- `ws_connection_active` last：`42`

这说明问题不只是客户端少收到了 `time_sync`，服务端指标也观察到活跃 WS 连接数从约 200 下降到 42。

## 3. 推测结论

高概率瓶颈方向是 WebSocket 写出/连接保持侧，而不是出价接口服务端执行链路。

现有指标支持以下判断：

1. 出价 HTTP handler 和业务服务层耗时并没有明显打满。
   - `/api/v1/items/{item_id}/bids` HTTP P95 最大约 `22.39ms`。
   - HTTP P99 最大约 `163.42ms`。
   - `auction_bid_duration` success P95 最大约 `17.38ms`。
   - `auction_bid_duration` success P99 最大约 `23.48ms`。
   - Redis Lua P99 最大约 `4.43ms`。

2. 当前观测到的 WS fanout 入队耗时也不高。
   - `ws_broadcast_duration` P95 最大约 `1.18ms`。
   - `ws_broadcast_duration` P99 最大约 `3.56ms`。
   - 说明 Hub 将事件分发到连接 send queue 的循环本身不是主要耗时点。

3. 但 WS 真实写出链路承压明显。
   - 100 QPS 阶段出价 RPS 最大约 `80/s`。
   - 同房间约 200 个连接，出价广播理论上会放大成最高约 `16000` 条 WS 消息/s。
   - Prometheus 观察到 `ws_delivery_success_rps` 最大约 `13379/s`。
   - 同时还叠加每秒 `time_sync`、`user_outbid` 等事件。
   - `time_sync` 和业务广播共用同一连接写队列/写循环，因此会被大量出价广播挤压，表现为 `time_sync` 间隔变大、数量不足。

4. 连接下降不像是 send channel 满导致的显式 dropped 路径。
   - `ws_delivery_dropped_rps` 无有效序列。
   - 但 `ws_connection_active` 从 max `201` 降到 last `42`。
   - 因此更可能是 socket `WriteJSON` 写出失败、写 deadline 超时、客户端 drain 跟不上或入口网络层断开造成连接退出。

需要注意：当前 `ws_broadcast_duration` 统计的是 Hub fanout 入队耗时，不是实际 socket 写出耗时。代码路径上，fanout 入队在 `internal/app/ws/hub/hub.go` 的 `SendToRoom` / `deliver`，真实写 socket 在 `internal/app/ws/hub/conn.go` 的 `StartWriteLoop` / `WriteJSON`。`WriteJSON` 失败会结束写循环并关闭连接，但现有指标没有记录具体失败原因和写出耗时，所以本结论是基于指标交叉验证后的高置信推测，还不是完整闭环证明。

## 4. 具体系统指标

### 4.1 Runner 侧指标

| 指标 | 数值 |
| --- | ---: |
| 阶段 | `step_100qps_200ws` |
| 目标 QPS | `100` |
| 实际 QPS | `88.34` |
| 目标 WS | `200` |
| 已连接 WS | `200` |
| 请求总数 | `15901` |
| 成功数 | `12333` |
| HTTP failures | `229` |
| Business fails | `0` |
| Expected rejects | `3339` |
| Timeouts | `229` |
| Error rate | `1.44%` |
| Timeout rate | `1.44%` |
| Client P95 | `1.914s` |
| Client P99 | `3.527s` |
| `time_sync` count | `17318` |
| `time_sync` P50 interval | `1.662s` |
| `time_sync` P95 interval | `4.602s` |
| `time_sync` P99 interval | `13.248s` |
| `time_sync` max interval | `20.748s` |

### 4.2 出价接口指标

| 指标 | Last | Max |
| --- | ---: | ---: |
| bid HTTP P50 | `5.80ms` | `6.26ms` |
| bid HTTP P95 | `22.30ms` | `22.39ms` |
| bid HTTP P99 | `163.42ms` | `163.42ms` |
| bid HTTP RPS | `47.71/s` | `80.02/s` |
| `auction_bid_duration` P95 all | `11.58ms` | `13.95ms` |
| `auction_bid_duration` P99 all | `22.32ms` | `22.88ms` |
| `auction_bid_duration` P95 success | `16.14ms` | `17.38ms` |
| `auction_bid_duration` P99 success | `23.23ms` | `23.48ms` |
| Redis Lua P95 | `1.66ms` | `1.86ms` |
| Redis Lua P99 | `4.31ms` | `4.43ms` |

### 4.3 WebSocket fanout / delivery 指标

| 指标 | Last | Max |
| --- | ---: | ---: |
| `ws_broadcast_duration` P50 all | `0.516ms` | `0.531ms` |
| `ws_broadcast_duration` P95 all | `0.980ms` | `1.178ms` |
| `ws_broadcast_duration` P99 all | `3.064ms` | `3.558ms` |
| `ws_broadcast_duration` P95 success | `0.980ms` | `1.178ms` |
| `ws_broadcast_duration` P99 success | `3.064ms` | `3.558ms` |
| `ws_broadcast_rps` | `36.13/s` | `72.36/s` |
| `ws_delivery_success_rps` | `3571.31/s` | `13379.11/s` |
| `ws_delivery_dropped_rps` | 无有效序列 | 无有效序列 |
| `ws_broadcast_recipients` P95 | `232.12` | `241.82` |
| `ws_connection_active` | `42` | `201` |

### 4.4 服务端总体指标

| 指标 | Max |
| --- | ---: |
| server HTTP P95 | `21.16ms` |
| server HTTP P99 | `136.09ms` |
| all HTTP RPS | `101.49/s` |
| auction bid RPS | `80.00/s` |
| Lua result RPS | `79.98/s` |
| DB operation RPS | `327.29/s` |
| backend restarts | `0` |

### 4.5 阶段前后资源观测

| 时间点 | 指标 |
| --- | --- |
| 50 QPS 阶段后资源样本 | backend 约 `253m CPU / 49Mi`，MySQL 约 `77m CPU / 548Mi`，Redis 约 `19m CPU / 13Mi` |
| 压测后资源样本 | backend 约 `5m CPU / 52Mi`，MySQL 约 `14m CPU / 558Mi`，Redis 约 `8m CPU / 16Mi` |
| 后端重启 | `0` |

## 建议补充指标

为了把当前推测闭环到具体代码点，建议下一轮补充以下观测：

1. `StartWriteLoop` 内单次 `WriteJSON` duration、失败原因和写 deadline 超时计数。
2. 每个连接 send channel 当前长度或高水位。
3. 按 event type 拆分的 WS delivery/fanout 指标，至少区分 `bid_success`、`user_outbid`、`time_sync`。
4. 慢连接剔除数量、剔除原因和 room 维度统计。
5. `BroadcastTimeSync` 本身执行耗时、跳过 overlapping run 次数和每轮广播房间数/连接数。

## 关联证据

- 总测试报告：`docs/agent-testing/reports/20260603-152601-auction-core-bid-performance.md`
- redacted evidence：`docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/evidence-redacted.md`
- runner：`docs/agent-testing/performance-runs/agent_perf_auction_20260603_core_bid_ws/main.go`
