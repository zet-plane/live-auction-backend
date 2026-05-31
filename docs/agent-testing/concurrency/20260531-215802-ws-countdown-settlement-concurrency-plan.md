# WebSocket Countdown Settlement 并发一致性测试计划

## 计划状态

- 计划来源：用户要求根据 `docs/superpowers/specs/2026-05-31-ws-countdown-settlement-design.md` 开始测试。
- Review 结果：已由用户确认执行。
- 批准方式：用户在对话中回复 `gogogo！`。
- 执行结果：已执行；按当前单实例验收范围通过。真实依赖 runner 首轮包含一个超出当前范围的双实例探针，暴露后续多实例 WebSocket 广播风险；单实例 WebSocket 复测通过。

## 涉及模块

- 目标模块：`item` / `bid` / `ws`
- 关联模块：`order` / `deposit` / `room` / `user`
- 关联 flow：`docs/agent-testing/flows/auction-lifecycle.md`

## 测试目标

验证真实 MySQL、真实 Redis Lua、真实 HTTP/WebSocket 链路下，单实例 Redis 权威倒计时和结算满足以下不变量：

- 并发出价最终只有一个最高领先人和成交价。
- 出价与结算竞争时，结果可由 Redis `end_time_unix_ms` 解释，不能出现双重最终结果。
- 单实例结算 worker 对同一拍品只有一次成功 finalization。
- 连接到该实例的所有客户端收到一致的 `time_sync`、`auction_snapshot` 和 `auction_ended` 关键字段。

## 读取文档

- `docs/agent-testing/README.md`
- `docs/agent-testing/templates/protocol.md`
- `docs/agent-testing/guides/runner.md`
- `docs/agent-testing/guides/concurrency.md`
- `docs/agent-testing/guides/go-runner.md`
- `docs/agent-testing/guides/environment.md`
- `docs/agent-testing/flows/auction-lifecycle.md`
- `docs/agent-testing/modules/bid.md`
- `docs/agent-testing/modules/item.md`
- `docs/agent-testing/modules/ws.md`
- `docs/superpowers/specs/2026-05-31-ws-countdown-settlement-design.md`

## 测试范围

- 通过真实业务接口创建本批次测试用户、商家、房间、拍品和保证金数据。
- 通过真实 HTTP 出价接口制造并发竞争。
- 通过真实 Redis 查询 `auction:item:{item_id}:state`、`auction:ending`、ranking、idempotency key。
- 通过真实 MySQL 查询拍品、出价日志、订单最终状态。
- 通过真实 WebSocket 客户端采集 `time_sync`、`auction_snapshot`、`auction_ended`。

## 禁止范围

- 不操作非本批次数据。
- 不清空数据库或 Redis。
- 不调用真实第三方支付、短信、物流或鉴定服务。
- 不在报告中写入 DSN、Redis 密码、服务地址、完整 token 或完整 ticket。
- 不执行容量或性能压测。

## 测试类型

Agent 并发一致性测试、状态一致性测试、WebSocket 场景测试。

## 测试数据

- 测试批次 ID：`agent_ws_settlement_concurrency_20260531215802`
- 用户名前缀：`agent_user_20260531215802_`
- 商家名前缀：`agent_merchant_20260531215802_`
- 拍品标题前缀：`agent_item_20260531215802_`
- 幂等 key 前缀：`agent_bid_20260531215802_`
- Redis 只清理本批次 item/room 对应 key 和本批次 idempotency key。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | 本地服务或用户批准的线上等价服务 | 验证真实 handler、中间件和序列化边界 |
| MySQL | 用户批准的测试库或线上等价库 | 验证真实持久化、最终成交和订单状态 |
| Redis | 用户批准的测试 Redis | 验证 Lua 原子性、`auction:ending` 和最终 snapshot |
| WebSocket | 真实客户端连接 | 验证真实推送和重连 snapshot |
| 第三方服务 | 不使用 | 不在本次核心测试范围 |

## 执行步骤

1. 检查 `go test ./...` 结果；若仍因 performance runner 模板编译失败，记录为前置 smoke 阻塞但继续执行目标包测试。
2. 检查目标包测试：`go test ./internal/app/item/... ./internal/app/ws/... ./internal/app/order/... -count=1`。
3. 准备本批次真实依赖环境和本地服务。
4. 使用 Go runner 创建测试商家、用户、房间、拍品和必要保证金。
5. 建立 3 个 WebSocket 客户端连接同一房间，并采集初始 `auction_snapshot`。
6. 执行并发场景 A-D。
7. 对每个场景采集 HTTP 响应、Redis 状态、MySQL 状态、WebSocket 消息摘要。
8. 清理本批次数据和 Redis key。
9. 写入 `docs/agent-testing/reports/` 测试报告。

## 验证方式

- Go runner `CASE` / `SUMMARY` / `CLEANUP` 输出。
- HTTP 出价响应摘要。
- Redis `HGETALL auction:item:{item_id}:state`、`ZRANGE auction:ending`、ranking 和 idempotency key 摘要。
- MySQL `auction_items`、`bid_logs`、`orders` 查询摘要。
- WebSocket 消息类型和 payload 关键字段摘要。

## 预计输出

- Go runner stdout。
- 测试报告 Markdown。
- 若失败，按协议输出失败场景、复现步骤、期望、实际、证据、可能原因、影响范围、建议修复点和回归测试建议。

## 并发场景设计

### 场景 A：临近结束多用户不同价格并发出价

- 场景名称：near-end-multi-price-bids
- 竞争对象：同一个 `auction:item:{item_id}:state` 的 `leader_user_id`、`deal_price`、`bid_count`、`participant_count`、Redis ranking 和 MySQL `bid_logs`
- 并发请求：5 个已缴保证金用户在 `end_time_unix_ms` 前 1-2 秒同步起跑，分别提交递增有效价格，例如 `1100`、`1200`、`1300`、`1400`、`1500`，每个请求使用唯一 idempotency key。
- 预期成功：所有在 Redis 判断未结束时到达的有效出价可以成功；至少最高价 `1500` 成为最终 Redis leader/deal，除非请求实际到达时间已不满足 `now_unix_ms < end_time_unix_ms`。
- 预期失败：若某些请求实际到达时已过 Redis 结束时间，允许返回 `40002 auction has ended`；不允许出现价格低于最终 deal 但覆盖 leader/deal 的成功结果。
- 最终不变量：Redis `deal_price`、Redis ranking 最高分、HTTP 最终详情、MySQL 结算 `winner_id/deal_price` 必须一致；`bid_count` 等于非幂等成功出价数；`participant_count` 等于成功参与用户数。

### 场景 B：相同幂等 key 并发重复出价

- 场景名称：same-idempotency-key-concurrent-bids
- 竞争对象：同一个 `auction:item:{item_id}:idempotency:{key}`、Redis state、Redis ranking、MySQL `bid_logs`
- 并发请求：同一用户用同一 idempotency key 同步发起 10 个相同价格出价请求。
- 预期成功：允许多个请求返回成功或幂等成功，但只能产生一次非幂等副作用。
- 预期失败：不应出现多条同 item/user/price/key 语义的 `bid_logs`，不应重复增加 `bid_count`。
- 最终不变量：Redis idempotency key 指向同一个 bid ID；MySQL 对该用户该价格仅有 1 条本批次 bid log；Redis `bid_count` 只增加 1。

### 场景 C：结算 worker 与最后一笔出价竞争

- 场景名称：settlement-races-last-bid
- 竞争对象：同一个 `auction:item:{item_id}:state.status`、`leader_user_id`、`deal_price`、`ended_at_unix_ms`、`auction:ending`
- 并发请求：一个 goroutine 在接近 `end_time_unix_ms` 时提交最后一笔有效出价；另一个 goroutine 同步触发结算入口或等待 1s settlement worker 触发。
- 预期成功：如果出价 Lua 先在 `now_unix_ms < end_time_unix_ms` 成功，则结算结果包含该出价；如果 settlement Lua 先在 `now_unix_ms >= end_time_unix_ms` claim，则出价返回 auction ended。
- 预期失败：不允许出价和结算各自写出互相矛盾的最终 leader/deal；不允许产生两个 `auction_ended` 最终结果。
- 最终不变量：Redis status 为 `ended`；MySQL item 为 `ended`；Redis/MySQL/WebSocket 的 `leader_user_id` 和 `deal_price` 一致；`auction:ending` 不再包含该 item。

### 场景 D：多个结算 worker 重复结算同一 due item

- 场景名称：multi-settlement-workers-single-item
- 竞争对象：同一个 due item 的 Redis settlement Lua claim、MySQL `auction_items`、`orders`、WebSocket `auction_ended`
- 并发请求：将同一拍品放入 due 状态后，同步启动 5 个 settlement 调用。
- 预期成功：只有一个 settlement 调用成功 claim 并执行持久化；其他调用 no-op 或发现已 ended。
- 预期失败：重复 settlement 不得创建重复订单，不得重复广播多个最终业务事件，不得改写 final leader/deal。
- 最终不变量：MySQL item 只有一个最终 `winner_id/deal_price`；本批次该 item 最多一个订单；Redis snapshot status 为 `ended` 且保留最终字段；`auction:ending` 已移除该 item。

### 场景 E：三客户端时间同步与重连 snapshot

- 场景名称：three-client-time-sync-and-reconnect
- 竞争对象：同一 room 内 3 个 WebSocket 连接收到的 `time_sync`、`auction_snapshot`、`auction_ended`
- 并发请求：用户 A、B、C 同时连接同一房间；A/B 参与出价，C 只旁观；C 断开并重连。
- 预期成功：3 个客户端收到同一 item 的 `time_sync`，`end_time_unix_ms` 一致；重连客户端立即收到当前 `auction_snapshot`；结算后所有连接收到一致 `auction_ended`。
- 预期失败：C 不应收到只属于 A/B 的用户单播；WebSocket 消息不能与最终 HTTP/Redis/MySQL 状态冲突。
- 最终不变量：所有客户端记录的最终 `leader_user_id` 和 `deal_price` 与 Redis/MySQL 最终状态一致。

## 清理策略

- 只清理 `agent_ws_settlement_concurrency_20260531215802` 批次创建的实体和 Redis key。
- MySQL 清理限定本批次 item/user/room/order/bid id 或标题/名称前缀。
- Redis 清理限定本批次 item/room key，或从共享 `auction:ending` 中移除本批次 item member。
- 禁止 `DROP`、`TRUNCATE`、`FLUSHALL`、`FLUSHDB` 和无条件批量删除。

## 执行许可

已确认并执行。执行报告见 `docs/agent-testing/reports/20260531-221420-ws-countdown-settlement-concurrency.md`。
