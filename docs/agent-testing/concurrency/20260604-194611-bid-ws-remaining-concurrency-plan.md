# bid/ws 剩余并发与实时一致性测试计划

## 基本信息

- 计划时间：2026-06-04 19:46 Asia/Shanghai
- 计划目标：补测上一轮仍未覆盖的自动延时并发、排行榜读写混合并发、真实 WebSocket 推送一致性。
- 执行环境：本地真实依赖。
- 计划状态：待用户确认后执行。

涉及模块：

- 目标模块：`bid`、`ws`
- 关联模块：`item`、`user`
- 关联 flow：无

## 文档路线

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/concurrency.md
docs/agent-testing/guides/go-runner.md
docs/agent-testing/modules/bid.md
docs/agent-testing/modules/ws.md
docs/agent-testing/guides/environment.md
docs/agent-testing/reports/README.md
```

## 依赖和数据边界

- HTTP：本地后端 `127.0.0.1:8080`。
- WebSocket：真实本地 WebSocket `GET /ws/v1/rooms/{room_id}?ticket=<ticket>`；报告中只记录脱敏 ticket，不记录完整 query string。
- MySQL：本地测试 MySQL，只操作本批次创建的数据。
- Redis：本地测试 Redis，只操作本批次 item/room/ticket/idempotency keys，并只读取本批次 stream/dead-stream 事件。
- 第三方服务：不使用。
- 测试批次 ID：`agent_bid_ws_remaining_20260604194611`。
- 清理原则：删除本批次 `bid_logs` / `auction_rules`；软删除本批次 `auction_items` / `users`；删除本批次 item/room Redis keys 和本批次可识别 ticket 残留；不清空 Redis stream。
- 敏感信息：runner 可使用本地 DSN/token/ticket，但报告中省略 DSN、token、密码、完整 ticket 和完整 WebSocket URL。

## 计划前置检查

- `go test ./internal/app/item/... ./internal/app/ws/... ./internal/core/observability/...` 无编译错误。
- 本地 MySQL/Redis 可用。
- 本地后端可启动。
- 每个场景使用独立拍品和房间，避免场景间状态污染。

## 并发场景设计

### 场景 A：自动延时并发

- 竞争对象：同一 ongoing 拍品的 Redis `end_time_unix_ms`、`extend_count`、`total_extended_sec`、`is_extended`、ranking、bid-log stream 和最终 MySQL BidLog。
- 并发请求：6 个普通用户同步起跑，拍品 end_time 设置为当前时间后约 20 秒，满足 `remaining <= ExtendTriggerSec`；价格为 `1100,1200,1300,1400,1500,1600`，每个请求使用不同 `idempotency_key`。
- 预期成功：成功请求必须价格严格递增且符合 `bid_increment`；至少一个成功请求触发自动延时。
- 预期失败：被更高成功出价覆盖后的较低请求返回 `40003 price too low`，或竞拍已被终态影响时返回文档允许的业务拒绝；不允许不可解释 5xx。
- 最终不变量：Redis `extend_count`、`total_extended_sec` 和 `end_time_unix_ms` 增量不超过策略上限；最终 `current_price`、`leader_user_id`、Redis ranking 第一名、HTTP ranking 第一名一致；stream 事件数、MySQL BidLog 数量、Redis `bid_count` 均等于非幂等成功请求数；无本批次 dead-stream 事件。

### 场景 B：排行榜读写混合并发

- 竞争对象：同一 ongoing 拍品的写入路径 `POST /bids` 和读取路径 `GET /ranking`，以及 Redis ranking / bidder_names / MySQL BidLog 最终对账。
- 并发请求：8 个普通用户同步起跑出价 `1100..1800`，同时启动 20 个 `GET /api/v1/items/{item_id}/ranking?page=1&page_size=10` 读请求。
- 预期成功：写请求按 Lua 串行结果产生若干成功出价；读请求均应返回 200，读到的是某一时刻快照。
- 预期失败：低于当前价的写请求可返回 `40003`；读请求不应返回 5xx、不应出现重复 rank、不应出现 rank 倒序或价格升序错误。
- 最终不变量：最终 HTTP ranking、Redis ranking、MySQL BidLog 最高价聚合一致；最终 rank 连续且第一名为最终 leader；并发期间每个成功的 ranking 响应内部不重复 user/rank，价格按倒序排列。

### 场景 C：真实 WebSocket 推送一致性

- 竞争对象：真实 WebSocket 业务事件序列与 HTTP / Redis / MySQL 最终业务状态。
- 并发请求：为同一 room 建立至少 3 条真实 WebSocket 连接：用户 A、用户 A 第二连接、用户 B。随后通过真实业务接口触发：开始竞拍、用户 A 出价、用户 B 超越用户 A、临近结束出价触发自动延时、一口价成交或另一个独立拍品成交。
- 预期成功：目标 room 连接收到对应 `auction_started`、`bid_success`、`user_outbid`、`auction_extended`、`auction_ended` / `order_created` 中本次真实触发的事件；用户 A 两条连接均收到 `user_outbid`，用户 B 不收到该单播；连接期间可收到 `time_sync`，且延时后 `end_time_unix_ms` 与 Redis end time 一致。
- 预期失败：未触发的事件不计失败，必须在报告中说明跳过原因；WebSocket 临时读超时只可用于“未收到事件”证据，不可替代最终状态对账。
- 最终不变量：每条收到的业务事件 payload 与对应 HTTP 响应、Redis state/ranking、MySQL `auction_items` / `bid_logs` 一致；ticket 被一次性消费；Redis online state 最终 `online_count = SCARD online_users`，连接关闭后本批次用户从 online set 移除；报告不得包含完整 token、ticket 或完整 WS query。

## Runner 输出要求

- 每个并发请求输出一条 `CASE`，包含 actor、price、idempotency key、开始/结束时间、HTTP 状态、业务码或 bid_id。
- 每个 WebSocket 连接输出 ticket 脱敏值、连接结果、收到的事件类型序列和关键 payload 字段。
- 每个场景输出 summary `CASE`，包含成功数、失败数、重叠窗口、Redis state/ranking、stream 事件数、MySQL BidLog 数量、HTTP ranking 或 MySQL item 状态。
- `overlap = first_end - last_start` 必须大于 0，否则对应并发场景只记为快速连续请求，不判定为有效并发。

## 报告输出

- 报告路径：`docs/agent-testing/reports/<timestamp>-bid-ws-remaining-concurrency.md`
- 报告必须包含：
  - 并发设计
  - 请求矩阵
  - WebSocket 消息矩阵
  - 并发隔离证明
  - 最终状态对账
  - 清理结果
  - 跳过项和已知缺口

## Review 和批准

- Review 结果：已按 `docs/agent-testing/modules/bid.md` 和 `docs/agent-testing/modules/ws.md` 复核；三个场景均有明确最终不变量。
- 批准方式：用户在对话中回复“开始”。
- 执行结果：待执行后写入报告，不在本计划文件中记录测试结果。
