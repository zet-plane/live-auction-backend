# bid hot path 并发一致性测试计划

## 基本信息

- 计划时间：2026-06-04 19:26 Asia/Shanghai
- 计划目标：补测 bid 模块在上一轮模块集成测试中未覆盖的并发一致性缺口。
- 目标模块：`bid`
- 计划来源：`docs/agent-testing/modules/bid.md` 第 14 节并发测试和第 18 节通过标准。
- 执行环境：本地真实依赖。
- 计划状态：待用户确认后执行。

涉及模块：

- 目标模块：`bid`
- 关联模块：`item`、`user`
- 关联 flow：无

## 文档路线

按 `docs/agent-testing/guides/concurrency.md` 要求，本次使用以下路线：

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
docs/agent-testing/guides/concurrency.md
docs/agent-testing/guides/go-runner.md
docs/agent-testing/modules/bid.md
docs/agent-testing/guides/environment.md
docs/agent-testing/reports/README.md
```

## 依赖和数据边界

- HTTP：本地后端 `127.0.0.1:8080`。
- MySQL：本地测试 MySQL，只操作本批次创建的数据。
- Redis：本地测试 Redis，只操作本批次 item keys、idempotency keys，并只读取本批次 stream/dead-stream 事件。
- WebSocket：不连接，实时消息完整契约转 `modules/ws.md`。
- 第三方服务：不使用。
- 测试批次 ID：`agent_bid_concurrency_20260604192617`。
- 清理原则：删除本批次 `bid_logs` / `auction_rules`；软删除本批次 `auction_items` / `users`；删除本批次 item Redis keys；不清空 Redis stream。
- 敏感信息：runner 可使用本地 DSN/token，但报告中省略 DSN、token、密码和可复用凭据。

## 计划前置检查

- `go test ./...` 或目标模块测试无编译错误。
- 本地 MySQL/Redis 可用。
- 本地后端可用。
- 竞拍规则使用 `start_price=1000`、`bid_increment=100`，普通竞拍 `price_cap=0`，一口价竞拍 `price_cap=1500`。
- 每个场景使用独立拍品，避免场景间状态污染。

## 并发场景设计

### 场景 A：多用户不同价格同时出价

- 竞争对象：同一 ongoing 拍品的 Redis state、ranking、bid-log stream 和最终 MySQL `bid_logs`。
- 并发请求：12 个普通用户同步起跑，分别以 `1100,1200,...,2200` 调用 `POST /api/v1/items/{item_id}/bids`，每个请求使用不同 `idempotency_key`。
- 预期成功：至少最高价 `2200` 请求成功；其他更低价格是否成功取决于 Lua 串行执行顺序，但所有成功价格必须严格递增且符合 `bid_increment`。
- 预期失败：被更高成功出价覆盖后的较低请求返回 `40003 price too low`；不允许出现不可解释 5xx。
- 最终不变量：最终 `current_price=2200`；`leader_user_id` 为 2200 请求用户；Redis ranking 第一名为 2200 用户；HTTP ranking 第一名一致；stream 事件数、MySQL BidLog 数量、Redis `bid_count` 均等于非幂等成功请求数；无本批次 dead-stream 事件。

### 场景 B：多用户相同价格同时出价

- 竞争对象：同一 ongoing 拍品的当前价、领先用户、ranking、stream 和 BidLog。
- 并发请求：10 个普通用户同步起跑，均以 `price=1100` 调用 `POST /api/v1/items/{item_id}/bids`，每个请求使用不同 `idempotency_key`。
- 预期成功：恰好 1 个请求成功改变当前价。
- 预期失败：其余请求返回 `40003 price too low`。
- 最终不变量：最终 `current_price=1100`；leader 是唯一成功用户；Redis ranking 只有 1 个参与者；stream 事件数、MySQL BidLog 数量、Redis `bid_count` 均为 1。

### 场景 C：同一用户同一幂等 key 并发重复提交

- 竞争对象：同一 item/user/idempotency key 对应的副作用唯一性。
- 并发请求：同一普通用户同步发起 10 个完全相同请求，`price=1100`、同一个 `idempotency_key`。
- 预期成功：所有请求均可返回 200；只有第一个非幂等执行产生新 bid，其他请求返回同一个 `bid_id` 的幂等结果。
- 预期失败：不预期业务失败；若出现失败必须可解释且不得产生重复副作用。
- 最终不变量：所有成功响应的 `bid_id` 唯一且相同；Redis idempotency key 指向该 `bid_id` 且有 TTL；stream 事件数、MySQL BidLog 数量、Redis `bid_count` 均为 1；ranking 只有该用户一条最高价。

### 场景 D：一口价和普通出价同时发生

- 竞争对象：同一 ongoing 拍品的终态 `ended`、Redis state、MySQL `auction_items`、stream 和 BidLog。
- 并发请求：6 个普通用户同步起跑，价格为 `1100,1200,1300,1400,1500,1600`，`price_cap=1500`，每个请求不同 `idempotency_key`。
- 预期成功：触发一口价的请求中最终只能有一个成为成交赢家；允许一口价前的普通递增请求先成功。
- 预期失败：Redis state 进入 `ended` 或 MySQL 商品状态已更新为 `ended` 后，后续请求返回 `40001 invalid request`、`40002 auction has ended` 或 `40003 price too low`；不允许成交结果被后续请求覆盖。
- 最终不变量：Redis state 和 MySQL `auction_items` 均为 `ended`；`WinnerID` / `DealPrice` 对应最终成交请求；最终 `current_price` / `deal_price` 与成交价一致；stream 事件和 MySQL BidLog 仅对应成功请求；成交后失败请求不改变 winner/deal_price。

## Runner 输出要求

- 每个并发请求输出一个 `CASE`，包含 actor、price、idempotency key、开始/结束时间、HTTP 状态、业务码或 bid_id。
- 每个场景输出一个 summary `CASE`，包含成功数、失败数、重叠窗口、Redis state/ranking、stream 事件数、MySQL BidLog 数量、HTTP ranking 或 MySQL item 状态。
- `overlap = first_end - last_start` 必须大于 0，否则该场景只记为快速连续请求，不判定为有效并发。

## 报告输出

- 报告路径：`docs/agent-testing/reports/<timestamp>-bid-concurrency.md`
- 报告必须包含：
  - 并发设计
  - 请求矩阵
  - 并发隔离证明
  - 最终状态对账
  - 清理结果
  - 跳过项和已知缺口

## Review 和批准

- Review 结果：已按 `docs/agent-testing/modules/bid.md` 复核；场景 D 失败码包含非 ongoing 商品的 `ErrInvalidRequest` / `40001`。
- 批准方式：用户在对话中回复“开始测试”。
- 执行结果：待执行后更新报告，不在本计划文件中记录测试结果。
