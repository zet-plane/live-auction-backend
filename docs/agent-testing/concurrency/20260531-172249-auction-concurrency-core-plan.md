# Auction Concurrency Core Plan

计划来源：
- 用户要求覆盖出价并发、成交 / 订单唯一性、保证金与出价竞态、商品状态流转并发。
- 读取入口：`docs/agent-testing/README.md` -> `templates/protocol.md` -> `guides/runner.md` -> `guides/concurrency.md` -> `guides/go-runner.md`。

Review 结果：approved。三个只读 subagent 已完成合约梳理；用户已在对话中确认阻塞语义，本计划可进入真实依赖并发测试执行阶段。

批准方式：用户对话确认，2026-05-31。确认语义如下：
- 一旦达到 `price_cap`，立即竞拍成交；后续普通出价不得覆盖成交结果。
- 过期结算 / 取消等终态竞争允许最终落入一个合法终态，但 HTTP、Redis、MySQL 不得矛盾。
- 同一商品重复上架、开始、取消不要求 HTTP 恰好一个成功；最终状态合法、副作用唯一即可。
- `CreateOrder` 并发应直接返回已有订单，不允许重复创建订单；唯一索引冲突不作为理想通过标准。
- 支付 / 取消并发不要求 HTTP 恰好一个成功；最终状态唯一且失败可解释即可。
- 已 `paid` 订单重复支付应返回“已支付”幂等语义，即使超过 `expired_at` 也不应报过期。
- 已过期 `pending` 订单不能被用户取消；最终只能保持 pending 后由扫描变为 `expired`，或在未过期取消成功时为 `cancelled`。
- 本轮只测试普通用户参与竞拍，不考虑商家缴保证金或出价。
- 同一 `idempotency_key` 携带不同价格时仍保持幂等，不产生第二次副作用。

执行结果：executed。2026-05-31 18:39 Asia/Shanghai 使用本地 HTTP、MySQL、Redis 执行 Go runner；runner 汇总 18 PASS / 5 FAIL，主 agent 复核额外标出 D1 HTTP 500 风险。报告路径：`docs/agent-testing/reports/20260531-183908-auction-concurrency.md`。

## 阻塞项

无。上述语义已确认。

## 测试目标

用真实 HTTP、真实测试 MySQL、真实测试 Redis 和 Go runner 制造并发窗口，并完成最终对账。请求返回不是终点；每个场景必须核对 HTTP 响应、Redis state、Redis ranking、MySQL 业务表和最终商品 / 订单状态是否符合不变量。

## 涉及模块

- 目标模块：bid, order, payment, deposit, item
- 关联模块：user, merchant, room
- 关联 flow：auction-lifecycle

## 测试范围

- 出价并发：不同价格、相同价格、幂等 key、同一用户快速递增、一口价与普通出价、排行榜读写并发。
- 成交 / 订单唯一性：同一商品并发 CreateOrder、补偿建单与一口价建单、同一订单并发支付、支付取消竞争、并发取消。
- 保证金与出价竞态：同一用户同一商品并发支付、支付保证金与出价同时发生、不同用户同一商品并发支付。
- 商品状态流转并发：并发上架、并发开始、并发取消、删除与上架、修改与上架/删除、过期结算与取消。

## 禁止范围

- 不使用真实支付、短信、物流、鉴定或其他第三方服务。
- 不清空数据库或 Redis。
- 不操作非本批次数据。
- 不写入地址、凭据、密码、真实 token 或可复用 token 到计划或报告。
- 不把本计划扩大到性能压测、WebSocket 完整连接测试或订单履约测试。

## 测试类型

并发一致性测试、状态一致性测试、接口契约对账、模块集成对账。

## 测试数据

批次 ID：

```text
agent_auction_concurrency_20260531172249
```

数据前缀：

```text
agent_auction_concurrency_20260531172249_
```

需要准备：

- 商家账号和 token。
- 至少 6 个普通用户账号和 token。
- 本批次测试房间。
- 多个本批次商品，分别用于出价、订单、保证金和状态流转场景。
- 有保证金规则的商品，规则 `deposit_amount > 0`。
- 无保证金规则或 `deposit_amount <= 0` 的对照商品。
- 有一口价 `price_cap` 的商品。
- pending / paid / cancelled / expired 订单。

## 依赖策略

- HTTP：真实本地或线上等价测试服务。
- MySQL：真实测试库，仅写本批次数据。
- Redis：真实测试 Redis，仅写本批次 key 或从共享结构移除本批次 member。
- Go runner：使用结构化 CASE / SUMMARY / CLEANUP 输出。
- 第三方服务：禁止调用。

## 执行步骤

1. 运行 `go test ./...` 和目标模块单元测试，确认无编译错误。
2. 按 batch_id 创建测试用户、商家、房间、商品、规则、保证金和订单数据。
3. 记录每个场景的并发前基线：HTTP 查询、MySQL 查询、Redis key。
4. 用 Go runner start gate 同步发起并发请求，记录每个请求开始 / 结束时间。
5. 每个请求单独输出 CASE：actor、关键字段、HTTP、业务码 / ID、开始和结束时间。
6. 每个场景完成后查询 HTTP、MySQL、Redis，输出 summary CASE。
7. 对账通过后清理本批次数据和 Redis member/key。
8. 写入测试报告，并把计划更新为 executed，关联报告路径和 cleanup 结果。

## 验证方式

- 请求矩阵：每个并发请求都有独立证据。
- 并发隔离证明：`first_start`、`last_start`、`first_end`、overlap、最大耗时。
- Redis 对账：`auction:item:{item_id}:state`、`auction:item:{item_id}:ranking`、`auction:item:{item_id}:bidder_names`、幂等 key、room queue、item state。
- MySQL 对账：`auction_items`、`auction_rules`、`bid_logs`、`orders`、`deposits`。
- HTTP 对账：商品详情、排行榜、订单详情、保证金查询、支付 / 取消响应。

## 预计输出

- Go runner stdout，包含 CASE / SUMMARY / CLEANUP。
- 并发隔离证明。
- 最终状态对账表。
- `docs/agent-testing/reports/<timestamp>-auction-concurrency-core.md`。

## 并发场景设计

### B1 多用户不同价格同时出价

竞争对象：同一 ongoing 商品的 Redis state、Redis ranking、MySQL `bid_logs`。

并发请求：5 个已缴保证金用户同时 `POST /api/v1/items/{item_id}/bids`，价格分别为 1100、1200、1300、1400、1500，幂等 key 各不相同。

预期成功：所有符合当前价格和加价幅度时序的请求可成功；最终最高合法价获胜。

预期失败：被更高价格抢先后变成低价的请求返回 `price too low` 或等价业务错误，且不写副作用。

最终不变量：Redis `current_price`、`leader_user_id`、ranking 第一名、最高成功 HTTP 响应、MySQL 最高 BidLog 一致；BidLog 条数等于非幂等成功请求数。

### B2 多用户相同价格同时出价

竞争对象：同一 ongoing 商品的当前价和排行榜。

并发请求：5 个已缴保证金用户同时以相同价格 1100 出价，幂等 key 各不相同。

预期成功：至多一个请求成功改变当前价。

预期失败：其余请求返回 `price too low` 或等价业务错误，且不改变 Redis state/ranking 或写 BidLog。

最终不变量：最终当前价为 1100；ranking 只有成功用户获得 1100 分；MySQL BidLog 对应该价格最多一条。

### B3 同一用户同一 idempotency_key 重复提交

竞争对象：同一商品、同一用户、同一 Redis idempotency key、BidLog。

并发请求：同一用户同时提交 5 个完全相同 `price=1100` 和同一 `idempotency_key` 的出价。

预期成功：请求结果可幂等返回；只允许一次非幂等副作用。

预期失败：无不可解释错误；重复请求不得新增 BidLog 或递增计数。

最终不变量：只有一个 `bid_id`、一条 BidLog、Redis `bid_count` 只增加一次、幂等 key 存在且有 TTL。

### B4 同一用户不同 idempotency_key 快速递增出价

竞争对象：同一用户在同一商品的多次合法价格、ranking score、BidLog。

并发请求：同一用户同时提交 1100、1200、1300、1400，幂等 key 不同。

预期成功：按 Redis Lua 实际串行化结果，合法递增请求可成功。

预期失败：被更高价抢先后不再满足递增条件的请求失败可解释。

最终不变量：Redis ranking 中该用户 score 等于该用户最高成功价；当前价不回退；BidLog 只记录成功出价。

### B5 一口价和普通出价同时发生

竞争对象：同一 ongoing 商品的 Redis state、MySQL `auction_items` 成交字段、BidLog、订单创建路径。

并发请求：一名用户以普通价 1400 出价，另一名用户以一口价 1500 出价，`price_cap=1500`，同步起跑。

预期成功：一口价请求成功时商品立即进入 `ended`；普通价请求若先成功也不得阻止最终一口价唯一成交。

预期失败：一口价成交后到达的普通出价失败，且不能覆盖 winner/deal_price。

最终不变量：商品最终 `ended`；`winner_id` 和 `deal_price` 对应唯一一口价成交结果；达到 `price_cap` 后后续普通或更高出价不得覆盖成交结果；订单最多一条；后续出价失败且成交结果不变。

### B6 排行榜查询与并发出价同时发生

竞争对象：Redis ranking、Redis bidder_names、HTTP ranking 响应。

并发请求：5 个用户并发出价，同时 10 个 goroutine 查询 `GET /api/v1/items/{item_id}/ranking`。

预期成功：查询请求均返回结构合法的某一时刻快照。

预期失败：无重复 rank、无乱序、无 500；出价失败仍需可解释。

最终不变量：最终排行榜与 Redis ranking、MySQL BidLog 聚合一致；查询过程中的 rank 连续且价格降序。

### O1 同一商品并发 CreateOrder

竞争对象：同一 `item_id` 的 `orders.item_id` 唯一约束和订单 service 幂等逻辑。

并发请求：10 个 goroutine 并发调用订单 Service `CreateOrder(itemID, winnerID, dealPrice)`。

预期成功：一个或多个调用可返回同一个最终订单结果；并发重复创建必须直接返回已有订单语义。

预期失败：不得把唯一索引冲突暴露为最终通过语义；如果出现唯一冲突，记录为失败或实现缺口。

最终不变量：同一 `item_id` 最多一条订单；订单为 `pending`；`user_id` 和 `price` 等于成交商品 winner/deal_price。

### O2 过期结算补偿与一口价成交并发建单

竞争对象：同一成交商品的 item ended 状态、`orders.item_id` 唯一约束、补偿扫描和一口价建单路径。

并发请求：一个 goroutine 触发一口价出价成交并建单；另一个 goroutine 调用 `ScanCompensation` 或等价补偿入口扫描同一商品。

预期成功：最多一个路径创建订单；另一路应返回已有订单或无待补偿数据。

预期失败：不得创建第二条订单；不得把唯一索引冲突作为理想通过语义。

最终不变量：商品最多一个订单；订单 user/price 与最终成交商品一致。

### O3 同一订单并发支付

竞争对象：同一 pending 未过期订单的 `orders.status`。

并发请求：5 个同一订单所属用户同时 `POST /api/v1/orders/{order_id}/pay`，body 为 `{"result":"success"}`。

预期成功：最终支付成功；重复支付返回已支付幂等语义。

预期失败：不得因 `expired_at` 已过而拒绝已 paid 订单的重复支付；其他失败必须可解释。

最终不变量：订单最终 `paid`；HTTP 订单详情和 MySQL 一致；不会落入 cancelled/expired；重复支付响应表达已支付语义。

### O4 同一订单支付和取消并发

竞争对象：同一 pending 订单的终态。

并发请求：同一用户同时发起 3 个 pay 和 3 个 cancel。

预期成功：只能落到 `paid` 或 `cancelled` 其中一个合法终态。

预期失败：输掉竞争的一侧返回非 pending 或等价业务错误；若最终 paid，cancel 失败；若最终 cancelled，pay 失败。

最终不变量：订单只有一个最终状态；不存在同时有支付成功和取消成功且最终状态矛盾的证据。

### O5 同一订单并发取消

竞争对象：同一 pending 订单的 `orders.status`。

并发请求：5 个同一订单所属用户同时 `POST /api/v1/orders/{order_id}/cancel`。

预期成功：至少一个请求成功取消。

预期失败：其余请求因非 pending 等业务原因失败可解释。

最终不变量：订单最终 `cancelled`；不会变为 paid/expired；失败请求不改变状态。

### D1 同一用户同一商品并发支付保证金

竞争对象：`deposits` 的 `item_id + user_id` 唯一记录。

并发请求：同一用户对同一有保证金商品同时调用 5 次 `POST /api/v1/items/{item_id}/deposit/pay`。

预期成功：请求可成功或因唯一冲突后返回可解释错误。

预期失败：不得产生多条记录。

最终不变量：同一 `item_id + user_id` 只有一条 `paid` 记录；金额等于 `auction_rules.deposit_amount`；HTTP 查询我的保证金与 DB 一致。

### D2 支付保证金与出价同时发生

竞争对象：同一用户的保证金可见性和出价前置校验。

并发请求：同一用户同时调用保证金支付和出价接口。

预期成功：如果出价在 paid 记录可见后执行则可成功。

预期失败：如果出价先于 paid 记录可见，则返回 `deposit required` 且不产生出价副作用。

最终不变量：最终保证金为 `paid`；出价是否成功与请求时间和 DB 可见状态一致；失败出价不写 BidLog、不改 Redis state/ranking。

### D3 不同用户同一商品并发支付保证金

竞争对象：同一商品下多个 `item_id + user_id` 保证金记录。

并发请求：5 个不同用户同时支付同一商品保证金。

预期成功：每个用户最多一条 paid 记录。

预期失败：单用户内部不得重复；不同用户不得互相覆盖。

最终不变量：每个用户各自一条记录，金额均等于规则金额；出价前置校验对 paid 用户通过。

### I1 同一商品并发上架

竞争对象：同一 draft 商品的 DB status 和 Redis room queue。

并发请求：同一商家同时调用 5 次 `POST /api/v1/items/{item_id}/publish`。

预期成功：一个或多个请求可返回成功或业务错误。

预期失败：失败请求必须可解释。

最终不变量：商品最终状态为 `published` 或仍为一个合法状态；Redis room queue 不出现重复 member；HTTP、MySQL、Redis 不矛盾。

### I2 同一商品并发开始竞拍

竞争对象：同一 published 商品的 DB status 和 Redis item state。

并发请求：同一商家同时调用 5 次 `POST /api/v1/items/{item_id}/start`。

预期成功：一个或多个请求可返回成功或业务错误。

预期失败：失败请求必须可解释。

最终不变量：若最终 `ongoing`，Redis item state 必须存在且字段与规则一致；若失败，MySQL 和 Redis 不能留下互相矛盾状态。

### I3 同一商品并发取消

竞争对象：同一 published 或 ongoing 商品的 DB status、Redis room queue、Redis item state。

并发请求：同一商家同时调用 5 次 `POST /api/v1/items/{item_id}/cancel`。

预期成功：一个或多个请求可返回成功或业务错误。

预期失败：失败请求必须可解释。

最终不变量：商品最终 `cancelled`；Redis room queue 和 item state 不保留该商品；HTTP、MySQL、Redis 一致。

### I4 删除与上架并发

竞争对象：同一 draft 商品的软删除状态、published 状态和 Redis room queue。

并发请求：同一商家同时调用 `DELETE /api/v1/items/{item_id}` 和 `POST /api/v1/items/{item_id}/publish`。

预期成功：只能形成一个合法最终结果：软删除或 published。

预期失败：输掉竞争的请求返回 not found/invalid request 或等价业务错误。

最终不变量：如果软删除，Redis room queue 不保留商品；如果 published，商品未软删除且 Redis queue 可解释；不得出现已删除但仍可见为已上架的矛盾状态。

### I5 修改与上架 / 删除并发

竞争对象：同一 draft 商品的基础字段、规则字段、状态和软删除标记。

并发请求：一组同时执行 `PUT /api/v1/items/{item_id}`、`POST /api/v1/items/{item_id}/publish`、`DELETE /api/v1/items/{item_id}`。

预期成功：修改只能在仍为 draft 且未删除时成功；上架或删除成功后修改不得半更新。

预期失败：非 draft 或已删除情况下修改失败可解释。

最终不变量：DB item/rule 不出现半更新；状态或软删除结果唯一合法；HTTP 详情与 DB 一致。

### I6 过期结算与取消并发

竞争对象：同一 ongoing 已过期商品的 `ended` 与 `cancelled` 终态、Redis item state、订单创建路径。

并发请求：一个 goroutine 调用 `EndExpiredAuctions`；另一个 goroutine 调用 `POST /api/v1/items/{item_id}/cancel`。

预期成功：最终只能落入 `ended` 或 `cancelled` 其中一个合法终态。

预期失败：输掉竞争的一侧返回业务错误或无待处理数据。

最终不变量：若 ended，则 winner/deal_price 与 Redis 结算快照一致且最多一个订单；若 cancelled，则 Redis room queue 和 item state 清理且不得创建订单；HTTP、MySQL、Redis 不矛盾。

## 清理策略

- 只清理 batch_id 前缀用户、商家、房间、商品、规则、保证金、订单、出价日志。
- Redis 删除本批次 item state、ranking、bidder_names、idempotency key；共享 room queue 仅移除本批次 item member。
- 禁止 `DROP`、`TRUNCATE`、`FLUSHDB`、`FLUSHALL`。

## 执行许可

本计划当前未获批准。获得用户确认后，执行 agent 必须先把批准记录写回本文件，再连接真实依赖、创建测试数据、启动 Go runner 或发起并发请求。
