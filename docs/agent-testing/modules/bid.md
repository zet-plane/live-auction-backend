# 出价模块测试说明

## 1. 模块目标

出价模块负责用户对进行中的拍品提交出价、维护实时竞拍状态、记录出价日志，并提供单个拍品的出价排行榜查询。

当前实现上，出价功能不是独立 Go module，而是扩展在 `internal/app/item/` 内的 bid 子能力。核心实体和状态包括 `AuctionItem`、`AuctionRule`、Redis `AuctionState`、Redis 排行榜、幂等 key、Redis `auction:bid_log:stream` 和 MySQL `BidLog`。

## 2. 代码定位索引

| 对象 | 代码位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/item/router/item.go` | 注册出价和排行榜 HTTP 路由 |
| Handler | `internal/app/item/handler/bid.go` | 请求绑定、鉴权用户注入、统一响应 |
| DTO | `internal/app/item/dto/bid.go` | 出价请求、出价结果、排行榜响应字段 |
| Service | `internal/app/item/service/bid_service.go` | 出价主流程、hot state 读取 / 修复、Lua 返回码处理、一口价成交、排行榜降级 |
| BidLog Worker | `internal/app/item/service/bid_log_worker.go` | 消费 Redis Stream 并异步批量持久化 `bid_logs` |
| DAO Store 接口 | `internal/app/item/dao/item.go` | `AutoMigrateBidLog`、`CreateBidLog`、`CreateBidLogs`、`ListBidRanking` 契约 |
| DAO 实现 | `internal/app/item/dao/bid_log.go` | BidLog 迁移、单条写入、幂等批量写入、MySQL 聚合排行榜 |
| Model | `internal/app/item/model/bid_log.go` | `BidLog` GORM 模型 |
| 关联 Model | `internal/app/item/model/item.go` | `AuctionItem`、`AuctionRule`、状态常量、成交字段 |
| Cache 接口和状态 | `internal/app/item/cache/cache.go` | `AuctionState`、`AuctionHotConfig`、`BidLuaArgs`、`BidLuaResult`、`BidLogEvent`、Cache 接口 |
| Redis Lua / Ranking | `internal/app/item/cache/bid.go` | 原子出价脚本、排行榜 key、幂等 key、成功出价 `XADD` |
| Redis Stream | `internal/app/item/cache/bid_log_stream.go` | BidLog stream append/read/pending/ack/dead-letter helpers |
| 商品状态前置 | `internal/app/item/service/service.go` | `StartItem` 初始化 `auction:item:{item_id}:state` |
| 单元测试建议位置 | `internal/app/item/service/bid_service_test.go` | 使用 fake store、fake cache、固定身份和可控时间 |
| Agent 测试契约 | `docs/agent-testing/modules/bid.md` | 接口契约、集成、场景、并发和一致性测试边界 |
| 关联模块契约 | `docs/agent-testing/modules/item.md` | 创建、上架、开始竞拍等出价前置状态 |
| 关联流程契约 | `docs/agent-testing/flows/auction-lifecycle.md` | 跨模块全生命周期测试 |

## 3. 测试边界

Agent 可以测试：

- HTTP 接口：`POST /api/v1/items/{item_id}/bids`、`GET /api/v1/items/{item_id}/ranking`。
- Service 方法：`PlaceBid`、`GetRanking`。
- DAO / Model：`BidLog` 单条写入、幂等批量写入、按用户最高价聚合排行榜、用户昵称 join。
- Redis key：`auction:item:{item_id}:state`、`auction:item:{item_id}:ranking`、`auction:item:{item_id}:bidder_names`、`auction:item:{item_id}:idempotency:{key}`、`auction:bid_log:stream`、`auction:bid_log:dead`。
- Redis Lua 原子行为：结束时间、价格、加价幅度、排行榜、参与人数、自动延时、幂等、一口价。
- 与出价直接相关的 `AuctionItem.Status`、`WinnerID`、`DealPrice` 状态一致性。
- 高并发出价、重复请求、排行榜读取降级和状态一致性。

Agent 不应在本模块内测试：

- 商品创建、上架、开始、取消的完整契约；这些属于 `modules/item.md`。
- 用户注册登录、token 生成和权限模型的完整契约；这些属于 `modules/user.md`。
- 房间开播、房间排队、当前拍品推进的完整契约；这些属于 `modules/room.md` 或后续场次模块。
- 支付、订单、物流、鉴定、真实第三方服务。
- WebSocket 连接、ticket、心跳、房间广播和用户单播的完整消息契约；这些属于 `modules/ws.md`。出价模块内只验证 broadcaster 调用、事件类型和 payload 与出价状态一致。

## 4. 禁止事项

- 不允许清空数据库或 Redis。
- 不允许复用线上真实拍品、真实用户或真实出价数据。
- 不允许把测试出价写入真实业务拍品。
- 不允许绕过接口直接修改非本批次数据。
- 不允许在测试报告中写入线上地址、凭据、密码、真实 token 或可复用密钥。
- 不允许调用真实支付、短信、鉴定、物流或其他第三方服务。
- 不允许自行创造代码和文档中都没有定义的出价规则。
- 本地单元测试不允许直连 MySQL、Redis、HTTP 服务、WebSocket 或外部系统，必须使用 mock/fake 数据。
- Agent 连接线上或线上等价数据库/Redis 时，只能操作本次测试创建的数据或带测试批次 ID 的数据。

## 5. 测试依赖策略

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 使用 fake store、fake cache、固定用户、固定时间；禁止直连 MySQL、Redis、HTTP 服务或 WebSocket | 稳定验证 Service 规则、Lua 返回码映射、幂等分支、排行榜分页和错误传播 |
| Agent 接口契约测试 | 使用真实 handler 或本地服务；使用真实测试数据库和真实测试 Redis；通过用户模块获取测试 token | 验证真实请求绑定、鉴权、统一响应、错误码和 Redis/MySQL 副作用 |
| Agent 模块集成测试 | 使用真实 GORM store、真实测试数据库和真实测试 Redis | 验证 Redis Lua 原子更新、成功出价 stream handoff、worker 异步落库、MySQL 排行榜 fallback |
| 场景测试 | 使用真实接口链路、真实测试数据库和真实测试 Redis；按 `modules/item.md` 准备 ongoing 拍品 | 验证用户可见出价和排名链路 |
| Agent 并发测试 | 使用真实 HTTP 并发请求、真实测试 Redis Lua 和真实测试数据库 | fake 无法证明原子出价、幂等和最高价最终一致性 |
| 状态一致性测试 | 对比 HTTP 响应、MySQL `auction_items` / `bid_logs`、Redis state/ranking/names/idempotency key/stream pending | 验证外部可见结果和内部最终一致 |
| WebSocket 测试 | 出价模块内部分适用；完整连接测试转到 `modules/ws.md` | 使用 fake broadcaster 验证 `bid_success`、`user_outbid`、`auction_extended`、`auction_ended`、`order_created` 的事件生产和 payload |

## 6. 全局测试数据准备

```text
测试批次 ID：agent_bid_<YYYYMMDDHHMMSS>
商品标题前缀：agent_bid_<batch>_
幂等 key 前缀：agent_bid_<batch>_
测试房间 ID：room_agent_bid_<batch> 或通过房间模块创建的测试房间
数据只允许操作本批次创建的数据。
测试结束后必须记录 MySQL 清理/软删除结果和 Redis key 清理结果。
```

需要准备：

- 1 个商家账号和 token，用于创建、上架、开始测试拍品。
- 至少 3 个普通用户账号和 token，用于成功出价、被超越、并发竞争和排行榜。
- 1 个无效 token 或未登录请求，用于鉴权失败验证。
- 至少 1 个状态为 `ongoing` 的测试拍品，且 Redis `auction:item:{item_id}:state` 已初始化。
- 1 个状态非 `ongoing` 的测试拍品，用于状态拒绝。
- 竞拍规则建议：`start_price=1000`、`bid_increment=100`、`price_cap=0` 或另建 `price_cap=1500` 的一口价拍品。
- 出价请求合法样例：

```json
{
  "price": 1100,
  "idempotency_key": "agent_bid_<batch>_u1_1100"
}
```

- 非法请求体集合：缺少 `price`、`price=0`、`price<0`、缺少 `idempotency_key`、`idempotency_key=""`、`idempotency_key` 超过 128 字符。
- 需要验证或清理的 MySQL 记录：`auction_items`、`auction_rules`、`bid_logs`、测试用户。
- 需要验证或清理的 Redis key：
  - `auction:item:{item_id}:state`
  - `auction:item:{item_id}:ranking`
  - `auction:item:{item_id}:bidder_names`
  - `auction:item:{item_id}:idempotency:{idempotency_key}`
  - `auction:bid_log:stream` 中本批次 item 对应消息或 pending/ack 状态
  - `auction:bid_log:dead` 中本批次 item 对应死信消息

## 7. 业务规则

事实：

- `POST /api/v1/items/{item_id}/bids` 需要通过 `/api/v1` 鉴权组，当前用户来自 token。
- `GET /api/v1/items/{item_id}/ranking` 是公开接口，不要求登录。
- 出价前 Service 会按 trim 后的 `item_id` 查询 `AuctionItem` 和 `AuctionRule`。
- 只有 `AuctionItem.Status == ongoing` 才允许出价；其他状态返回 `ErrInvalidRequest`。
- 出价模块依赖 Redis cache；Service cache 为 nil 时返回 `ErrInternal`。
- 每次非幂等成功出价会生成 `bid_` 前缀的 bid ID，并与 item、room、user、price、created_at 一起传入 Redis Lua。
- Redis Lua 使用 `auction:item:{item_id}:idempotency:{key}` 做幂等控制，TTL 固定为 86400 秒。
- 幂等 key 已存在时，Lua 返回 code 1；Service 返回 Redis 当前状态，不再追加新的 bid-log stream 事件。
- Redis state 不存在或当前时间大于等于结束时间时，Lua 返回 code 2；Service 转为 `40002 auction has ended`。
- 出价 `price <= current_price` 时，Lua 返回 code 3；Service 转为 `40003 price too low`。
- `(price - current_price) % bid_increment != 0` 时，Lua 返回 code 4；Service 转为 `40004 invalid bid increment`。
- 成功出价会更新 Redis state：`current_price`、`leader_user_id`、`end_time_unix`、`bid_count`、`participant_count`、`is_extended`、`extend_count`、`total_extended_sec`。
- 成功出价会写 Redis ranking：`auction:item:{item_id}:ranking`，member 为 `user_id`，score 为最高出价。
- 成功出价会写 Redis bidder names：`auction:item:{item_id}:bidder_names`，field 为 `user_id`，value 为当前用户名称。
- ranking 更新使用 Redis `ZADD GT`，同一用户的较低价格不会降低排行榜分数。
- 新用户首次进入 ranking 时，`participant_count` 加 1。
- 每次非幂等成功出价都会让 `bid_count` 加 1。
- 触发自动延时时，条件是剩余时间 `<= ExtendTriggerSec`、`ExtendCount < MaxExtendCount`、且 `TotalExtendedSec + AutoExtendSec <= MaxTotalExtendSec`。
- 非幂等成功出价在 Redis Lua 内原子追加 `auction:bid_log:stream`，字段包括 `bid_id`、`item_id`、`room_id`、`user_id`、`price`、`created_at_unix_ms`；HTTP hot path 不同步写 MySQL `bid_logs`。
- BidLog worker 消费 `auction:bid_log:stream`，批量调用 `CreateBidLogs` 持久化 MySQL `bid_logs`，成功后 `XACK`；持久化或 ACK 失败时消息应保持可重试。
- `CreateBidLogs` 使用主键冲突忽略，重复消费同一 stream 消息不得产生重复 BidLog。
- 当 `price_cap > 0` 且出价 `>= price_cap` 时，Service 将商品状态更新为 `ended`，并写入 `WinnerID` 和 `DealPrice`。
- 一口价成交后 HTTP 出价响应的 `status` 为 `ended`，普通成功出价响应的 `status` 为 `ongoing`。
- 排行榜优先读 Redis；Redis 返回错误或无数据时，Service 降级到 MySQL `bid_logs` 聚合查询。
- MySQL 排行榜按每个用户最高价 `MAX(price)` 聚合，按价格倒序返回，并左连接 `users` 表取昵称。
- 排行榜 `page <= 0` 归一为 1，`page_size <= 0` 归一为 10，`page_size > 100` 归一为 100。
- 排名 `rank` 从当前分页 offset + 1 开始。
- 当商品规则 `deposit_amount > 0` 时，`PlaceBid` 会在 Redis Lua 执行前调用 `depositSvc.HasPaidDeposit`；未缴纳足额 `paid` 保证金时返回 `40005 deposit required`。
- 当商品规则 `deposit_amount <= 0` 时，`PlaceBid` 不调用保证金检查。
- 当前实现写 BidLog 是 Redis Stream backed 异步落库；接口成功只证明 Redis 原子状态和 stream handoff 成功，MySQL `bid_logs` 需要按最终一致窗口验证。
- 当前实现会通过 broadcaster 广播或单播 `bid_success`、`user_outbid`、`auction_extended`、`auction_ended`、`order_created`；未看到独立 `ranking_updated` 事件。

根据当前代码结构推断：

- 普通用户和商家用户都可能通过鉴权进入出价接口；当前 Service 没有限制身份必须为普通用户。
- 当前出价接口已校验保证金；仍未看到报名、风控、黑名单、出价用户不能是拍品所属商家等限制。
- 当前出价接口没有校验一口价时 `DealPrice` 是否使用 Redis 返回的 `CurrentPrice`；代码使用请求中的 `input.Price`。
- 当前排行榜公开可见，不返回敏感 token 或联系方式。
- Redis Lua 成功但 worker 尚未消费或 MySQL 暂时失败时，Redis 状态和 stream 事件已经存在；当前语义是异步最终一致，不回滚 Redis。
- 一口价 Redis 成功、BidLog 写入成功但 MySQL 商品 ended 更新失败时，Redis 状态和 BidLog 可能已经变化；当前代码没有回滚 Redis 或 BidLog。

需确认内容集中在“需用户确认的问题”章节。

## 8. 业务不变量

- 非 ongoing 拍品不能产生有效出价。
- 已超过 Redis `end_time_unix` 的竞拍不能产生有效出价。
- 每个成功出价的 `bid_id` 必须唯一。
- 同一个幂等 key 重试不能重复追加 bid-log stream 事件、不能重复写入 `BidLog`，也不能重复增加 `bid_count`。
- 当前价不能被较低出价或相同价格覆盖。
- 不符合 `bid_increment` 的出价不能改变 Redis state/ranking，不能追加 bid-log stream 事件，也不能新增 MySQL `bid_logs`。
- Redis ranking 中每个用户只能保留该用户最高出价。
- 排行榜第一名必须与最高有效出价用户一致。
- `current_price`、`leader_user_id`、Redis ranking 最高分和最新成功出价响应必须一致。
- `bid_count` 只能随非幂等成功出价增加。
- `participant_count` 只能随首次成功参与的用户增加。
- 自动延时不能超过最大延时次数和最大总延时秒数。
- 一口价成交后，MySQL 商品状态必须为 `ended`，`WinnerID` 和 `DealPrice` 必须对应触发一口价的用户和价格。
- 一口价成交后，后续出价不应改变成交用户和成交价。
- 排行榜分页返回的 `rank` 必须连续且等于 offset + 列表位置 + 1。
- MySQL fallback 排行榜必须按用户最高价聚合，而不是按出价日志条数或最新一条排序。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 9. 字段规则索引

### PlaceBidRequest / PlaceBidResult

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `item_id` | path/db/Redis | 去除首尾空格后查询；必须存在；商品必须为 `ongoing` | `POST /api/v1/items/{item_id}/bids`、`PlaceBid` | `BID.FIELD.item_id.*` |
| `price` | request/Redis/stream/db/response | 必填；HTTP binding `min=1`；必须大于当前价；必须符合 `bid_increment`；触发 `price_cap` 可结束竞拍 | 出价接口、Redis Lua、BidLog stream、BidLog | `BID.FIELD.price.*` |
| `idempotency_key` | request/Redis | 必填；HTTP 长度 1 到 128；Redis STRING，TTL 86400 秒；重复使用时幂等返回 | 出价接口、Redis Lua | `BID.FIELD.idempotency_key.*` |
| `bid_id` | Redis/stream/db/response | 非幂等成功出价生成 `bid_` 前缀；幂等重试返回原 bid ID | 出价响应、BidLog stream、BidLog | `BID.FIELD.bid_id.*` |
| `current_price` | Redis/response | 成功后等于本次有效出价；幂等返回 Redis 当前价格 | 出价响应、Redis state | `BID.FIELD.current_price.*` |
| `leader_user_id` | Redis/response | 成功后等于当前出价用户 ID；幂等返回 Redis 当前领先用户 | 出价响应、Redis state | `BID.FIELD.leader_user_id.*` |
| `end_time` | Redis/response | 由 Redis `end_time_unix` 转换；自动延时时应增加 | 出价响应、Redis state | `BID.FIELD.end_time.*` |
| `status` | response/db | 普通成功为 `ongoing`；一口价成交为 `ended` | 出价响应、AuctionItem | `BID.FIELD.status.*` |

### BidLog / Ranking / Redis State

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `BidLog.ID` | stream/db | 主键；应等于成功出价 bid ID；重复消费时冲突忽略 | Redis Stream、`CreateBidLogs`、MySQL fallback | `BID.FIELD.bid_log_id.*` |
| `BidLog.ItemID` | stream/db | 必须等于出价拍品 ID | Redis Stream、`CreateBidLogs`、`ListBidRanking` | `BID.FIELD.bid_log_item_id.*` |
| `BidLog.RoomID` | stream/db | 必须等于拍品 `RoomID` | Redis Stream、`CreateBidLogs` | `BID.FIELD.bid_log_room_id.*` |
| `BidLog.UserID` | stream/db | 必须等于当前出价用户 ID | Redis Stream、`CreateBidLogs`、排行榜 | `BID.FIELD.bid_log_user_id.*` |
| `BidLog.Price` | stream/db | 必须等于请求出价价格 | Redis Stream、`CreateBidLogs`、排行榜 | `BID.FIELD.bid_log_price.*` |
| `BidLog.CreatedAt` | stream/db | 来自 stream `created_at_unix_ms`，应接近出价发生时间 | Redis Stream、`CreateBidLogs` | `BID.FIELD.bid_log_created_at.*` |
| `ranking.rank` | response | 从 1 开始；分页时从 offset + 1 开始 | 排行榜接口 | `BID.FIELD.rank.*` |
| `ranking.user_id` | Redis/db/response | Redis 来源为 ZSET member；MySQL 来源为 BidLog user_id | 排行榜接口 | `BID.FIELD.ranking_user_id.*` |
| `ranking.user_name` | Redis/db/response | Redis 来源为 bidder_names；MySQL fallback 来源为 users.name；可能为空 | 排行榜接口 | `BID.FIELD.ranking_user_name.*` |
| `ranking.price` | Redis/db/response | 每个用户最高出价；按倒序排列 | 排行榜接口 | `BID.FIELD.ranking_price.*` |
| `bid_count` | Redis | 非幂等成功出价加 1 | Redis Lua、商品查询 | `BID.FIELD.bid_count.*` |
| `participant_count` | Redis | 新用户首次出价加 1 | Redis Lua、商品查询 | `BID.FIELD.participant_count.*` |
| `extend_count` / `total_extended_sec` | Redis | 自动延时不能超过策略上限 | Redis Lua | `BID.FIELD.extend_limits.*` |

## 10. 接口测试契约

### `POST /api/v1/items/{item_id}/bids` 提交出价

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/item/router/item.go` | 鉴权组内 `POST /items/{item_id}/bids` |
| Handler | `internal/app/item/handler/bid.go` | `PlaceBid` 绑定 JSON、读取当前用户、调用 Service |
| DTO | `internal/app/item/dto/bid.go` | `PlaceBidRequest`、`PlaceBidInput`、`PlaceBidResult` |
| Service | `internal/app/item/service/bid_service.go` | `PlaceBid` |
| DAO | `internal/app/item/dao/item.go`、`internal/app/item/dao/bid_log.go` | 查 item/rule、worker 批量写 BidLog、更新一口价成交状态 |
| Model | `internal/app/item/model/item.go`、`internal/app/item/model/bid_log.go` | `AuctionItem`、`AuctionRule`、`BidLog` |
| Cache | `internal/app/item/cache/cache.go`、`internal/app/item/cache/bid.go` | Redis Lua 原子出价 |
| Deposit | `internal/app/deposit/service/service.go` | `HasPaidDeposit` 校验足额 `paid` 保证金 |
| WebSocket / 外部依赖 | `pkg/wsevent.Broadcaster` | 出价成功、被超越、自动延时、一口价成交和订单创建时发送事件；完整连接收发转到 `modules/ws.md` |

#### 接口职责

提交当前登录用户对指定 ongoing 拍品的一次出价。接口负责校验请求字段、按规则校验保证金、调用 Redis Lua 原子更新实时竞拍状态并追加 bid-log stream 事件；后台 worker 负责异步持久化 BidLog。达到一口价时，接口还负责结束拍品。

接口不负责创建拍品、开始竞拍、支付保证金、订单生成或 WebSocket 连接管理；当前 Service 会通过 broadcaster 生产出价相关实时事件。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | path 参数；trim 后必须存在，且商品状态必须为 `ongoing` | not found 或 invalid request |
| `price` | 是 | `min=1`；必须大于 Redis 当前价；必须符合 `bid_increment` | binding 参数错误、`40003` 或 `40004` |
| `idempotency_key` | 是 | 长度 1 到 128；同 key 重试应幂等返回 | binding 参数错误或幂等成功 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `bid_id` | 成功时非空；幂等重试返回原 bid ID | HTTP 响应 + Redis idempotency key + BidLog stream / 最终 BidLog |
| `current_price` | 等于 Redis 当前价 | HTTP 响应 + Redis state |
| `leader_user_id` | 等于当前领先用户 ID | HTTP 响应 + Redis state |
| `end_time` | 等于 Redis `end_time_unix` 转换结果 | HTTP 响应 + Redis state |
| `status` | `ongoing` 或 `ended` | HTTP 响应 + MySQL AuctionItem |

#### 测试数据准备

- 登录用户 token。
- 已 `published -> ongoing` 的测试拍品。
- 如果 `deposit_amount > 0`，需准备当前出价用户的足额 `paid` 保证金；缺失、`pending` 或金额不足都应失败。
- Redis state 已存在，包含起拍价、结束时间、出价数、参与人数和延时字段。
- 合法请求体和非法请求体集合。
- 一口价测试需使用配置了 `price_cap` 的独立拍品。
- 并发测试需为每个请求准备唯一或刻意重复的 `idempotency_key`。

#### 成功路径

- 用户首次合法出价成功，HTTP 返回 `bid_id`、当前价、领先用户、结束时间和 `ongoing` 状态。
- Redis state 更新为新当前价和领先用户，`bid_count` 加 1。
- Redis ranking 记录该用户最高价，bidder_names 记录用户名称。
- Redis Lua 在同一次原子执行内追加一条 bid-log stream 事件，字段与请求和拍品一致。
- BidLog worker 消费 stream 后，MySQL `bid_logs` 最终新增一条记录，字段与 stream 事件一致。
- 同一个 `idempotency_key` 重试返回成功，但不追加新的 stream 事件，不新增 BidLog，不重复增加 `bid_count`。
- 接近结束时间出价时，满足策略则自动延时并更新 Redis 延时字段。
- 出价达到或超过 `price_cap` 时，HTTP 返回 `ended`，MySQL 商品状态为 `ended`，`WinnerID` 和 `DealPrice` 正确。

#### 失败路径

- 未登录或 token 无效时返回鉴权错误。
- 请求体缺字段或字段非法时返回 binding 参数错误。
- `item_id` 不存在时返回 not found。
- 商品状态不是 `ongoing` 时返回 invalid request。
- 商品要求保证金但当前用户没有足额 `paid` 保证金时返回 `40005 deposit required`，且不得改变 Redis state/ranking、不得追加 stream 事件或写入 `BidLog`。
- Redis state 不存在或竞拍已结束时返回 `40002 auction has ended`。
- 出价小于等于当前价时返回 `40003 price too low`。
- 出价不符合加价幅度时返回 `40004 invalid bid increment`。
- Redis Lua 执行失败时接口失败；需验证不会追加 stream 事件或写 BidLog。
- BidLog worker 持久化失败不会回滚已成功的 HTTP 出价；需通过 pending / 未 ACK 消息和 worker 日志记录最终一致风险。
- 一口价 MySQL 状态更新失败时接口失败；需记录 BidLog 和 Redis 可能已更新的已知风险。

#### 状态和一致性验证

- HTTP 响应与 Redis state 一致。
- Redis ranking 第一名与 Redis `leader_user_id` 一致。
- 非幂等成功出价必须有对应 stream 事件，并在 worker 成功后有对应 BidLog。
- 幂等重试不能产生第二条相同业务含义 stream 事件或 BidLog。
- 一口价成交时 HTTP、MySQL `auction_items`、Redis state、stream 事件和最终 BidLog 必须一致。
- 失败请求不得改变当前价、领先用户、排行榜、stream 或 BidLog。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证成功、错误码、幂等、一口价、延时、错误传播 |
| 接口契约测试 | 是 | 验证真实请求绑定、鉴权、统一响应、错误码 |
| 模块集成测试 | 是 | 使用真实 Redis Lua 和真实 DAO 验证状态协作 |
| 场景测试 | 是 | 出价、被超越、排行榜、一口价成交场景覆盖 |
| 并发测试 | 是 | 多用户同时出价、重复 key、相同价格竞争 |
| 状态一致性测试 | 是 | 对比 HTTP、MySQL、Redis |

### `GET /api/v1/items/{item_id}/ranking` 查询排行榜

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/item/router/item.go` | 公开路由 `GET /api/v1/items/{item_id}/ranking` |
| Handler | `internal/app/item/handler/bid.go` | `GetRanking` 解析 `page`、`page_size` |
| DTO | `internal/app/item/dto/bid.go` | `RankingResult`、`RankingEntry`、`BidderPrice` |
| Service | `internal/app/item/service/bid_service.go` | Redis 优先、MySQL fallback、分页归一 |
| DAO | `internal/app/item/dao/bid_log.go` | `ListBidRanking` 聚合查询 |
| Model | `internal/app/item/model/bid_log.go` | `BidLog` |
| Cache | `internal/app/item/cache/bid.go` | `GetRanking` 读取 ZSET + names HASH |

#### 接口职责

返回指定拍品按用户最高价倒序排列的排行榜。优先返回 Redis 实时排行榜；Redis 无数据或读取失败时降级到 MySQL BidLog 聚合。

接口不负责校验拍品是否存在、不负责鉴权、不负责返回出价明细流水。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | path 参数；trim 后用于 Redis/DB 查询 | 当前实现不存在也可返回空榜 |
| `page` | 否 | 非数字解析为 0 后归一为 1；`<=0` 归一为 1 | 默认分页 |
| `page_size` | 否 | 非数字解析为 0 后归一为 10；`<=0` 归一为 10；`>100` 截断为 100 | 默认或截断 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `list` | 按价格倒序；每用户最高价一条 | HTTP 响应 + Redis ZSET 或 SQL 查询 |
| `rank` | 从 offset + 1 开始递增 | HTTP 响应 |
| `user_id` | 出价用户 ID | HTTP 响应 + Redis/DB |
| `user_name` | Redis 来源为 bidder_names；MySQL 来源为 users.name；允许为空 | HTTP 响应 + Redis/DB |
| `price` | 用户最高价 | HTTP 响应 + Redis/DB |
| `page` / `page_size` | 返回归一化后的分页参数 | HTTP 响应 |

#### 测试数据准备

- 一个有多名用户成功出价的 ongoing 拍品。
- Redis ranking 和 bidder_names 已由真实出价生成。
- MySQL fallback 测试需准备 BidLog，并模拟 Redis miss 或 Redis 读取错误。
- 分页测试至少准备 5 个用户出价。

#### 成功路径

- Redis 有排行榜时，返回 Redis 排序和昵称。
- 同一用户多次出价时，只显示该用户最高价。
- `page=1&page_size=2` 返回前两名，rank 为 1、2。
- `page=2&page_size=2` 返回下一页，rank 从 3 开始。
- Redis miss 或错误时，从 MySQL BidLog 聚合返回排行榜。
- 空榜返回空 list 和归一化分页。

#### 失败路径

- MySQL fallback 查询失败时接口失败。
- Redis bidder names 读取失败时当前 cache 方法返回错误，Service 会降级到 MySQL；需验证结果来源和昵称差异。
- `page_size` 极大时必须被限制为 100。

#### 状态和一致性验证

- Redis 来源排行榜与 `auction:item:{item_id}:ranking` 分数一致。
- MySQL fallback 排行榜与 `bid_logs` 每用户最高价一致。
- 排名、分页和价格排序一致。
- Redis 和 MySQL 同时存在时，当前实现优先 Redis；若二者不一致，报告必须标明来源。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake cache/store 验证分页、fallback、rank |
| 接口契约测试 | 是 | 验证公开访问、查询参数、响应结构 |
| 模块集成测试 | 是 | 真实 Redis/DB 验证排序和 fallback |
| 场景测试 | 是 | 多用户出价后查询排名 |
| 并发测试 | 是 | 并发出价后最终排行榜一致性 |
| 状态一致性测试 | 是 | 对比 HTTP、Redis、MySQL |

## 11. Service / DAO 测试契约

### `Service.PlaceBid`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/item/service/bid_service.go` | `PlaceBid` |
| Store 接口 | `internal/app/item/dao/item.go` | `FindItemWithRule`、`CreateBidLog`、`CreateBidLogs`、`UpdateItemWithRule` |
| Cache 接口 | `internal/app/item/cache/cache.go` | `PlaceBidLua` |
| Model | `internal/app/item/model/item.go`、`internal/app/item/model/bid_log.go` | 商品、规则、出价日志 |

#### 测试数据准备

- fake store 中准备 ongoing / non-ongoing 商品和规则。
- fake cache 模拟 Lua code 0、1、2、3、4 和错误。
- 一口价测试准备 `price_cap`。
- 固定 `now` 用于自动延时边界。

#### 单元测试点

- 成功出价通过 Redis Lua 追加 bid-log stream 事件，不同步写 BidLog。
- 非 ongoing 商品拒绝。
- cache nil 返回内部错误。
- Lua code 1 幂等返回且不追加 stream、不写 BidLog。
- Lua code 2/3/4 映射为对应业务错误码。
- 未知 Lua code 返回内部错误。
- Redis 错误、商品更新错误的错误传播。
- 一口价更新 `Status/WinnerID/DealPrice`。
- 自动延时参数正确传入 fake cache。

#### 集成测试点

- 真实 Redis Lua 一次请求内完成校验、状态更新和 stream handoff。
- BidLog worker 消费 stream 后，真实 MySQL BidLog 与 Redis 成功结果最终一致。
- worker 持久化失败、ACK 失败和 pending 重试场景需要故障注入或在报告中记录未覆盖。

### `Service.GetRanking`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/item/service/bid_service.go` | `GetRanking` |
| Cache 接口 | `internal/app/item/cache/cache.go` | `GetRanking` |
| DAO | `internal/app/item/dao/bid_log.go` | `ListBidRanking` |
| DTO | `internal/app/item/dto/bid.go` | `RankingResult` |

#### 单元测试点

- Redis 命中返回排行榜。
- Redis miss 或错误时 fallback MySQL。
- page/page_size 归一化和截断。
- offset 超过数据量返回空 list。
- DAO 错误返回。

#### 集成测试点

- Redis `ZREVRANGE WITHSCORES` 排序正确。
- `bidder_names` 缺失时 user_name 为空但不影响排序。
- MySQL `MAX(price)` 聚合和 users.name join 正确。

### `GormStore.CreateBidLog` / `GormStore.CreateBidLogs` / `GormStore.ListBidRanking`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| DAO | `internal/app/item/dao/bid_log.go` | GORM 实现 |
| Model | `internal/app/item/model/bid_log.go` | `BidLog` |
| User Model | `internal/app/user/model` | MySQL fallback join users 表 |

#### 单元测试点

- DAO 本身建议用集成测试，不在本地单元测试中直连 DB。

#### 集成测试点

- `CreateBidLog` 持久化所有字段。
- `CreateBidLogs` 批量持久化所有字段，重复 ID 时幂等忽略。
- `ListBidRanking` 只返回指定 `item_id`。
- 同一用户多条 BidLog 只取最高价。
- 多用户按最高价倒序。
- 用户不存在或 name 为空时，左连接结果不应阻塞排行榜。
- limit 生效。

## 12. 核心场景测试

### 场景 1：用户成功出价并成为领先者

#### 业务价值

这是出价模块最小可用链路，证明用户可见响应、Redis 实时状态、Redis Stream handoff 和 MySQL 出价日志最终一致。

#### 关联接口 / 方法

- `POST /api/v1/items/{item_id}/bids`
- `Service.PlaceBid`
- `Cache.PlaceBidLua`
- Redis `auction:bid_log:stream`
- `bidLogWorker`
- `GormStore.CreateBidLogs`

#### 代码定位

- Router：`internal/app/item/router/item.go`
- Handler：`internal/app/item/handler/bid.go`
- Service：`internal/app/item/service/bid_service.go`
- Redis：`internal/app/item/cache/bid.go`
- DAO：`internal/app/item/dao/bid_log.go`

#### 测试数据准备

- 商家创建、上架并开始一个测试拍品。
- 用户 A 登录。
- Redis state 当前价为起拍价。

#### Given

- 拍品状态为 `ongoing`。
- Redis state 存在且结束时间未到。

#### When

- 用户 A 以合法价格提交出价。

#### Then

- HTTP 成功，返回 `status=ongoing`。
- Redis `current_price` 和 `leader_user_id` 更新为用户 A。
- Redis ranking 中用户 A 分数等于出价。
- Redis `auction:bid_log:stream` 已追加本次出价事件。
- BidLog worker 消费后，MySQL `bid_logs` 有且仅有本次出价记录。

#### 证据要求

- HTTP 响应。
- Redis HGETALL state、ZREVRANGE ranking、HGET bidder_names。
- Redis stream 中本次 item/bid 的消息或消费后 pending/ack 证据。
- MySQL `bid_logs` 查询。
- 清理结果。

### 场景 2：多用户连续出价后排行榜正确

#### 业务价值

验证排名、最高价、用户昵称和分页对用户可见。

#### 关联接口 / 方法

- `POST /api/v1/items/{item_id}/bids`
- `GET /api/v1/items/{item_id}/ranking`
- `Service.GetRanking`

#### Given

- 拍品 ongoing。
- 用户 A、B、C 已登录。

#### When

- 用户 A 出价 1100。
- 用户 B 出价 1300。
- 用户 C 出价 1200。
- 查询排行榜第一页。

#### Then

- 排名 1 为用户 B，价格 1300。
- 排名 2 为用户 C，价格 1200。
- 排名 3 为用户 A，价格 1100。
- Redis ranking、HTTP 排行榜和 BidLog 聚合结果一致。

#### 证据要求

- 三次 HTTP 出价响应。
- 排行榜 HTTP 响应。
- Redis ranking 和 bidder_names。
- MySQL BidLog 聚合查询。

### 场景 3：幂等重试不重复出价

#### 业务价值

客户端重试是高频场景，必须避免重复扣动竞拍状态和重复日志。

#### Given

- 拍品 ongoing。
- 用户 A 已准备一个固定 `idempotency_key`。

#### When

- 用户 A 用同一个 `idempotency_key` 连续提交两次相同出价。

#### Then

- 两次 HTTP 都成功。
- 第二次返回原 bid ID 或 Redis 当前状态。
- Redis stream 不重复追加第二条相同业务含义消息。
- MySQL BidLog 最终只新增一条。
- Redis `bid_count` 只增加一次。
- Redis idempotency key TTL 存在。

#### 证据要求

- 两次 HTTP 响应。
- Redis stream 事件数量。
- MySQL BidLog count。
- Redis state 和 idempotency key TTL。

### 场景 4：一口价成交

#### 业务价值

验证出价模块会在触达封顶价时产生最终成交状态。

#### Given

- 拍品 ongoing。
- 规则 `price_cap=1500`。
- 用户 B 登录。

#### When

- 用户 B 出价 1500 或更高。

#### Then

- HTTP 返回 `status=ended`。
- MySQL `auction_items.status=ended`。
- `WinnerID` 为用户 B。
- `DealPrice` 为本次请求价格。
- Redis stream 记录本次出价，worker 最终写入 BidLog。
- 后续出价应失败，成交结果不变。

#### 证据要求

- HTTP 响应。
- MySQL `auction_items` 和最终 `bid_logs`。
- Redis state/ranking。
- Redis stream 事件。
- 后续失败出价响应。

### 场景 5：自动延时

#### 业务价值

验证防狙击策略在接近结束时生效，并受次数和总时长限制。

#### Given

- 拍品 ongoing。
- Redis state 剩余时间小于或等于 `ExtendTriggerSec`。
- `extend_count` 和 `total_extended_sec` 未达上限。

#### When

- 用户提交合法出价。

#### Then

- HTTP `end_time` 延后。
- Redis `end_time_unix` 增加 `AutoExtendSec`。
- Redis `extend_count` 加 1。
- Redis `total_extended_sec` 增加 `AutoExtendSec`。
- 达到上限后再次出价不再延时。

#### 证据要求

- 出价前后 Redis state。
- HTTP 响应中的 `end_time`。
- AuctionPolicy 配置记录。

## 13. 状态流转和一致性测试

| 当前状态 | 动作 | 目标状态 | 允许 | 涉及接口 / 方法 | 一致性证据 |
| --- | --- | --- | --- | --- | --- |
| `draft` | 出价 | `draft` | 否 | `POST /bids` | HTTP 错误 + DB 状态不变 + 无 stream/BidLog + Redis 不变 |
| `published` | 出价 | `published` | 否 | `POST /bids` | HTTP 错误 + DB 状态不变 + 无 stream/BidLog + Redis 不变 |
| `ongoing` | 合法普通出价 | `ongoing` | 是 | `POST /bids` | HTTP + Redis state/ranking + stream + 最终 BidLog |
| `ongoing` | 一口价出价 | `ended` | 是 | `POST /bids` | HTTP + AuctionItem + stream + 最终 BidLog + Redis ranking |
| `ongoing` | 幂等重试 | `ongoing` 或 `ended` | 是 | `POST /bids` | HTTP + stream/BidLog 不重复 + Redis 计数不重复 |
| `ongoing` | 低价/同价出价 | `ongoing` | 否 | `POST /bids` | HTTP `40003` + Redis/DB 不变 |
| `ongoing` | 非法加价幅度 | `ongoing` | 否 | `POST /bids` | HTTP `40004` + Redis/DB 不变 |
| `ended` | 出价 | `ended` | 否 | `POST /bids` | HTTP 错误 + 成交结果不变 |
| `cancelled` | 出价 | `cancelled` | 否 | `POST /bids` | HTTP 错误 + DB 状态不变 |

## 14. 并发测试

| 并发目标 | 是否需要 | 真实依赖 | 通过标准 |
| --- | --- | --- | --- |
| 多用户不同价格同时出价 | 是 | 真实 HTTP、Redis Lua、MySQL | 最高合法价格获胜；ranking、state、stream、最终 BidLog 一致 |
| 多用户相同价格同时出价 | 是 | 真实 HTTP、Redis Lua、MySQL | 至多一个请求成功改变当前价；失败请求错误可解释 |
| 同一用户同一幂等 key 重复提交 | 是 | 真实 HTTP、Redis Lua、MySQL | 只产生一个 bid_id、一条 stream 事件和一条 BidLog |
| 同一用户不同幂等 key 快速递增出价 | 是 | 真实 HTTP、Redis Lua、MySQL | 价格单调上升，ranking 保留最高价 |
| 一口价和普通出价同时发生 | 是 | 真实 HTTP、Redis Lua、MySQL | 成交结果唯一，成交后后续出价无效 |
| 排行榜查询与并发出价同时发生 | 是 | 真实 HTTP、Redis | 查询结果可以是某一时刻快照，但不得乱序或返回重复 rank |

并发测试必须记录：

- 请求总数、成功数、失败数。
- 每个请求的 `price`、`idempotency_key`、HTTP 状态、错误码或 bid ID。
- 最终 Redis state。
- 最终 Redis ranking。
- 最终 Redis stream 事件数量、MySQL BidLog 数量和最高价聚合。
- 如触发一口价，最终 MySQL 商品成交状态。

## 15. WebSocket / Redis / 外部副作用测试

| 副作用 | 触发动作 | 验证方式 | 清理要求 |
| --- | --- | --- | --- |
| `auction:item:{item_id}:state` HASH | 开始竞拍后出价 | HGETALL 对比当前价、领先用户、计数、结束时间和延时字段 | 删除本批次 item state |
| `auction:item:{item_id}:ranking` ZSET | 非幂等成功出价 | ZREVRANGE WITHSCORES 验证排序和最高价 | 删除本批次 ranking |
| `auction:item:{item_id}:bidder_names` HASH | 非幂等成功出价 | HGET/HMGET 验证用户昵称 | 删除本批次 bidder_names |
| `auction:item:{item_id}:idempotency:{key}` STRING | 非幂等成功出价 | GET 验证 bid ID，TTL 验证过期时间存在 | 删除本批次 idempotency keys |
| `auction:bid_log:stream` STREAM | 非幂等成功出价 | XREAD/XRANGE 或 consumer pending/ack 证据验证事件字段；消费成功后可验证无本批次 pending | 不能清空 stream；只记录本批次消息 ID，必要时 XACK 本批次 pending |
| `auction:bid_log:dead` STREAM | stream 消息解析失败 | 查询本批次 item 是否出现死信 | 不能清空 stream；只记录本批次消息 ID |
| MySQL `bid_logs` | BidLog worker 消费 stream | SQL 查询字段和数量 | 删除或标记本批次测试数据 |
| WebSocket 出价成功消息 | 非幂等成功出价 | fake broadcaster 或真实 WebSocket 客户端验证 `bid_success` payload；完整连接转到 `modules/ws.md` | 关闭本批次连接 |
| WebSocket 被超越消息 | 后一用户超越前一领先用户 | fake broadcaster 或真实 WebSocket 客户端验证 `user_outbid` 只发给前领先用户 | 关闭本批次连接 |
| WebSocket 自动延时消息 | 临近结束出价触发自动延时 | fake broadcaster 或真实 WebSocket 客户端验证 `auction_extended` payload 与 Redis end_time 一致 | 关闭本批次连接 |
| WebSocket 竞拍结束 / 订单消息 | 一口价成交或结算结束 | fake broadcaster 或真实 WebSocket 客户端验证 `auction_ended`、`order_created` payload 与 MySQL / HTTP 一致 | 关闭本批次连接 |
| 外部第三方服务 | 不适用 | 出价模块不依赖第三方服务 | 不适用 |

## 16. 回归测试

| 风险 | 回归测试位置 | 触发条件 | 证据 |
| --- | --- | --- | --- |
| 低价或同价覆盖当前价 | 单元 / 接口 / 并发 | `price <= current_price` | HTTP `40003` + Redis/DB 不变 |
| 加价幅度校验失效 | 单元 / 接口 | `(price-current_price) % bid_increment != 0` | HTTP `40004` + Redis/DB 不变 |
| 幂等重试重复写日志 | 单元 / 接口 / 并发 | 同 `idempotency_key` 重复请求 | stream 事件数不变 + BidLog count 不变 + bid_count 不变 |
| Redis ranking 与 current leader 不一致 | 集成 / 状态一致性 | 多用户连续或并发出价 | Redis state + ranking + HTTP 排行榜 |
| MySQL fallback 排行榜不按最高价 | DAO 集成 | 同一用户多条 BidLog | SQL 聚合结果 + HTTP fallback 响应 |
| 自动延时超过上限 | 单元 / 集成 | 多次临近结束出价 | Redis extend_count / total_extended_sec |
| 一口价后仍可有效出价 | 场景 / 并发 | `price >= price_cap` 后再次出价 | MySQL 成交状态不变 |
| worker 失败造成 BidLog 延迟落库 | 故障注入 / 风险报告 | CreateBidLogs 或 XACK 失败 | HTTP 成功 + Redis stream pending/未 ack + MySQL 暂未一致证据 |
| 公开排行榜泄露敏感信息 | 接口契约 | 查询 ranking | 响应只含 rank/user_id/user_name/price |

## 17. 测试类型覆盖矩阵

| 测试对象 | 单元 | 接口契约 | 集成 | 场景 | 异常 | 边界 | 并发 | 状态一致性 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v1/items/{item_id}/bids` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GET /api/v1/items/{item_id}/ranking` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `Service.PlaceBid` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `Service.GetRanking` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `Redis PlaceBidLua` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GormStore.CreateBidLog` | 否 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GormStore.CreateBidLogs` | 否 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `BidLogWorker` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GormStore.ListBidRanking` | 否 | 否 | 是 | 是 | 是 | 是 | 否 | 是 |
| `price` 字段 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `idempotency_key` 字段 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| 自动延时 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| 一口价成交 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| WebSocket 事件生产 | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |

## 18. 通过标准

**核心验证点（全部通过才算过）：**

- 合法出价接口返回成功，HTTP 响应、Redis state、Redis ranking、Redis stream handoff 和 worker 后 MySQL BidLog 最终一致。
- 非 ongoing、已结束、低价、同价、非法加价幅度、非法请求体和未登录请求均失败，且不会产生有效状态变化。
- 同一 `idempotency_key` 重试不重复追加 stream、不重复写 BidLog、不重复增加 Redis 计数。
- 多用户连续出价后，当前价、领先用户和排行榜第一名一致。
- 排行榜分页、排序、每用户最高价聚合和 page/page_size 归一化符合预期。
- 一口价出价后，商品状态、赢家和成交价正确，后续出价不能改变成交结果。
- 并发测试最终状态唯一且可解释，没有重复 rank、价格倒退或成交结果冲突。
- 所有测试只操作本批次数据，并记录清理结果。

**辅助验证点（建议验证，可附说明跳过）：**

- Redis idempotency key TTL 接近 86400 秒。
- BidLog worker 消费后，本批次 stream 消息无 pending，`auction:bid_log:dead` 无本批次死信。
- bidder_names 中昵称与当前用户名称一致。
- 自动延时字段 `extend_count`、`total_extended_sec`、`is_extended` 正确。
- Redis 失败时排行榜可从 MySQL fallback 返回。
- 出价模块没有返回 token、手机号、密码、地址等敏感字段。

## 19. 需用户确认的问题

- 当前是否允许商家身份参与出价？如果不允许，出价接口需要补身份限制，测试也应增加商家出价失败场景。
- 当前是否需要校验出价用户不能是拍品所属商家？
- 当前是否需要报名、风控或黑名单校验？代码当前仅实现保证金校验。
- Redis Lua 成功但 BidLog worker 长时间未消费成功时，是否需要后台补偿告警、人工修复入口或明确的 SLA？
- 一口价 Redis 成功但 MySQL ended 更新失败时，是否需要补偿任务或事务化状态修复？
- `GET /ranking` 是否应校验 item 存在？当前实现不存在也可能返回空榜。
- 排行榜同价时是否需要稳定排序规则？当前 Redis/MySQL 都只明确按价格倒序。
- 自动延时触发边界是否包含 `remaining == ExtendTriggerSec`？当前 Lua 使用 `<=`。
- 达到 `price_cap` 时是否允许出价大于一口价？当前代码允许 `price >= price_cap` 并以请求价格成交。
- 是否需要独立 `ranking_updated` 事件？当前代码会广播 `bid_success` 和相关状态事件，但未看到独立排行榜刷新事件。

## 20. 失败报告格式

测试失败时，agent 必须输出：

```text
失败场景：
复现步骤：
期望结果：
实际结果：
相关证据：
可能原因：
建议修复点：
建议新增的回归测试：
已知缺口：（本次测试因文档或实现原因未覆盖的风险，以及建议如何补充）
```

如果是不变量违反，额外输出：

```text
违反的不变量：
违反位置：
期望状态：
实际状态：
```
