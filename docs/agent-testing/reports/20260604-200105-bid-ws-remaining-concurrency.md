# 测试报告：bid/ws 剩余并发与实时一致性

## 基本信息

- 测试目标：补测自动延时并发、排行榜读写混合并发、真实 WebSocket 推送一致性。
- 测试类型：并发一致性测试 / WebSocket 真实链路状态一致性测试。
- 测试时间：2026-06-04 19:58-20:01 Asia/Shanghai。
- 执行 agent：主 agent Codex。
- 主 agent：Codex。
- 子 agent：未使用。
- 主 agent 复核结论：本轮 3 个目标中 2 个通过，真实 WebSocket 推送一致性发现稳定失败。
- 冲突和处理：首次自动延时失败来自 runner 用毫秒原值比较 `end_time`，而接口按 RFC3339 秒级保存；修正为秒级后重跑，自动延时通过。
- 批次 ID：`agent_bid_ws_remaining_20260604194611`。
- Runner：`/private/tmp/agent-runner-agent_bid_ws_remaining_20260604194611/main.go`。

## 测试环境

- 服务地址：本地服务 `127.0.0.1:8080`。
- WebSocket：真实本地 WS 连接，报告只记录脱敏 ticket。
- MySQL：本地 MySQL，DSN 已省略。
- Redis：本地 Redis。
- 读取文档：`docs/agent-testing/README.md`、`docs/agent-testing/templates/protocol.md`、`docs/agent-testing/guides/runner.md`、`docs/agent-testing/guides/concurrency.md`、`docs/agent-testing/guides/go-runner.md`、`docs/agent-testing/modules/bid.md`、`docs/agent-testing/modules/ws.md`、`docs/agent-testing/guides/environment.md`、`docs/agent-testing/reports/README.md`。

## 执行步骤

1. 前置验证：`rtk env GOCACHE=/tmp/live-auction-go-cache go test ./internal/app/item/... ./internal/app/ws/... ./internal/core/observability/...` 通过。
2. 启动本地后端：`rtk env GOCACHE=/tmp/live-auction-go-cache go run main.go server -c config.yaml`。
3. 运行 runner：`rtk env GOCACHE=/tmp/live-auction-go-cache TEST_DSN=<omitted> go run main.go`。
4. 首次运行发现自动延时判定误差和 WS 失败；修正 runner 秒级 end_time 判定后重跑。
5. 第二次运行结果：`PASS: 3  FAIL: 1`。
6. runner 执行 cleanup；随后额外按 item_id 删除两次运行产生的一口价订单。
7. 停止本地后端。

## 结果摘要

| 场景 | 请求数 / 连接数 | 结果 | 关键证据 |
| --- | ---: | --- | --- |
| 自动延时并发 | 6 个并发出价 | PASS | 成功 1、失败 5；`extend_count=1`、`total_extended_sec=10`、`end_time_unix_ms` 增加 10 秒；stream=1、BidLog=1、dead=0 |
| 排行榜读写混合并发 | 8 个并发出价 + 20 个并发 ranking 读取 | PASS | 写成功 3、失败 5；20/20 ranking 响应内部有效；最终 HTTP ranking、Redis ranking、BidLog 对账一致 |
| 真实 WebSocket 推送一致性 | 3 条 WS 连接 + 真实出价/成交 | FAIL | 同一用户同房间第一条连接被关闭，未收到 `user_outbid`；第二条连接收到；业务最终状态一致 |

## 并发隔离证明

- 自动延时并发：
  - 请求开始范围：20:00:45.136924-20:00:45.137090。
  - 重叠窗口：`7.252542ms`。
  - 最终状态：`current_price=1600`、`leader_user_id=user_2062504827664994304`、`extend_count=1`、`total_extended_sec=10`。
- 排行榜读写混合：
  - 请求开始范围：20:00:45.745874-20:00:45.746089。
  - 重叠窗口：`5.733416ms`。
  - 读请求结果：20/20 HTTP 200，快照 rows 为 0、1、2 均内部有序且无重复 rank/user。
  - 最终状态：`current_price=1800`、leader 和 HTTP ranking 第一名均为 `user_2062504830244491264`。
- WebSocket：
  - 连接成功：3/3，ticket 均只记录脱敏值。
  - 事件流：
    - `userA_conn1`：连接后被关闭，未收到事件。
    - `userA_conn2`：收到 `auction_started`、`time_sync`、`auction_extended`、`bid_success`、`user_outbid`、`auction_ended`、`order_created`。
    - `userB_conn`：收到 `auction_started`、`time_sync`、`auction_extended`、两次 `bid_success`、`auction_ended`、两次 `order_created`。

## 最终状态对账

| 数据源 | 自动延时 | 排行榜读写混合 | WebSocket 场景 |
| --- | --- | --- | --- |
| HTTP | 1 个有效出价成功，低价请求 `40003` | 3 个有效出价成功，低价请求 `40003`，20 个 ranking 读取均 200 | start=200，A bid=200，B bid=200 |
| Redis state | `extend_count=1`、`end_time_unix_ms=1780574480000`、leader 为 1600 出价用户 | `current_price=1800`、`bid_count=3`、leader 为 1800 出价用户 | `status=ended`、`leader_user_id=user_2062504831318233088`、`deal_price=1300` |
| Redis ranking / HTTP ranking | 第一名为 1600 出价用户 | 第一名均为 1800 出价用户 | WS 场景以业务事件和 ended state 对账 |
| Redis stream / dead | stream=1、dead=0 | stream=3、dead=0 | BidLog=2，业务状态一致 |
| MySQL | BidLog=1 | BidLog=3 | `auction_items.status=ended`、`winner_id=user_2062504831318233088`、`deal_price=1300`、BidLog=2 |

## 失败项

### WS-1：同一用户同房间多连接没有同时收到单播

- 期望：用户 A 的两条 WebSocket 连接都收到 `user_outbid`。
- 实际：
  - `userA_conn1`：连接成功后被关闭，`close 1006 unexpected EOF`，没有收到 `user_outbid`。
  - `userA_conn2`：收到 `user_outbid`。
  - `userB_conn`：未收到 `user_outbid`，符合单播隔离。
- 最终业务状态：Redis/MySQL/HTTP 均一致，未发现竞拍状态错误。
- 判断：这是 WebSocket 多连接语义问题。代码当前在同一 room 注册同一 user 的新连接时会替换旧连接；如果产品预期“同一用户多端同房间都在线并都收到单播”，这是 bug。如果产品预期“同一用户同房间只保留最新连接”，则需要修正文档和测试契约。

### WS-2：赢家连接收到两次 `order_created`

- 观察：`userB_conn` 收到两次 `order_created`。
- 可能原因：一口价成交后订单事件既 fanout 到 room，又 unicast 给 winner；winner 同时在目标 room 在线，因此收到两份。
- 判断：未作为本轮 hard-fail 条件，但从客户端体验看很可能导致重复提示或重复处理，建议明确事件投递语义。

## 通过项

- 自动延时并发：临近结束并发出价能触发反狙击延时，延时字段、ranking、stream、BidLog 一致。
- 排行榜读写混合并发：并发读取期间所有 ranking 响应内部有效；最终 HTTP ranking、Redis ranking、MySQL BidLog 聚合一致。
- WebSocket 房间广播：活跃连接收到 `auction_started`、`bid_success`、`auction_extended`、`auction_ended`，payload 与最终 Redis/MySQL 状态一致。
- WebSocket 单播隔离：`user_outbid` 没有发给新领先者 B。
- Ticket 脱敏：报告中未记录完整 ticket、完整 query string、token 或 DSN。

## 清理结果

- runner cleanup：
  - `item_2062504827920846848`：BidLog=1、auction rule=1、item soft delete=1。
  - `item_2062504830475177984`：BidLog=3、auction rule=1、item soft delete=1。
  - `item_2062504831544725504`：BidLog=2、auction rule=1、item soft delete=1。
  - `users WHERE account LIKE agent_bid_ws_remaining_20260604194611%` soft delete rows=17。
- 额外订单清理：
  - `item_2062504578351370240`：orders rows=1。
  - `item_2062504831544725504`：orders rows=1。
- Redis：删除本批次 item state/ranking/bidder_names/idempotency、room state/online_users、ticket 残留；不清空全局 stream。

## 建议

- 明确同一用户同房间多连接语义：
  - 若要支持多端，移除 `Hub.Register` 中同 room 同 user 替换旧连接的逻辑，或按设备/conn 维度维护 presence。
  - 若只允许单连接，更新 `docs/agent-testing/modules/ws.md`，并让客户端预期旧连接会被踢下线。
- 明确 `order_created` 投递语义，避免 winner 同时收到 room fanout 和 user unicast 的重复事件，或为事件添加幂等 ID 供客户端去重。
- 将自动延时并发和排行榜读写混合沉淀为轻量回归 runner。
