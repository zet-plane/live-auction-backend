# 商品模块测试说明

## 1. 模块目标

商品模块负责商家拍品的创建、公开查询、商家查询、修改、删除、上架、开始竞拍、取消竞拍和过期结算，并维护拍品对应的竞拍规则、直播间归属和 Redis 协作状态。

核心实体是 `AuctionItem` 和 `AuctionRule`。商家创建商品时必须绑定 `room_id`，商品初始状态为 `draft`。当前商品状态动作包括 `draft -> published -> ongoing`、`published/ongoing -> cancelled`，以及后台过期结算将 `ongoing -> ended`。

## 2. 代码定位索引

| 对象 | 代码位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/item/router/item.go` | 注册公开商品接口、排行榜接口和鉴权后的商家商品/出价接口 |
| Handler | `internal/app/item/handler/item.go` | 商品请求绑定、鉴权上下文、查询参数解析、统一响应 |
| Bid Handler | `internal/app/item/handler/bid.go` | 出价和排行榜 HTTP 入口；完整契约见 `modules/bid.md` |
| DTO | `internal/app/item/dto/item.go` | 创建/修改请求、列表/详情/商家视图响应、字段绑定规则和派生字段 |
| Event DTO | `internal/app/item/dto/events.go` | `auction_started`、`auction_cancelled`、`auction_ended`、`order_created` payload |
| Service | `internal/app/item/service/service.go` | 商品规则校验、状态流转、归属校验、Redis 协作、过期结算 |
| Bid Service | `internal/app/item/service/bid_service.go` | 出价子能力；完整测试边界见 `modules/bid.md` |
| DAO | `internal/app/item/dao/item.go` | `Store` 接口、GORM 事务、软删除、分页查询、过期商品查询 |
| Model | `internal/app/item/model/item.go` | `AuctionItem`、`AuctionRule`、状态常量和字段约束 |
| Cache | `internal/app/item/cache/cache.go` | Redis 竞拍状态和房间待拍队列读写 |
| 单元测试建议位置 | `internal/app/item/service/*_test.go` | 使用 fake store、fake cache、fake broadcaster、固定时间和固定身份 |
| Agent 测试契约 | `docs/agent-testing/modules/item.md` | 接口契约、集成、场景和一致性测试边界 |
| 关联模块契约 | `docs/agent-testing/modules/bid.md` | 出价、排行榜、一口价成交和出价事件 |
| 关联流程契约 | `docs/agent-testing/flows/auction-lifecycle.md` | 跨模块竞拍全生命周期 |

## 3. 测试边界

Agent 可以测试：

- HTTP 接口：`GET /api/v1/items`、`GET /api/v1/items/{item_id}`、`POST /api/v1/items`、`GET /api/v1/merchant/items`、`PUT /api/v1/items/{item_id}`、`DELETE /api/v1/items/{item_id}`、`POST /api/v1/items/{item_id}/publish`、`POST /api/v1/items/{item_id}/start`、`POST /api/v1/items/{item_id}/cancel`。
- Service 方法：`CreateItem`、`ListItems`、`ListMerchantItems`、`GetItem`、`UpdateItem`、`DeleteItem`、`PublishItem`、`StartItem`、`CancelItem`、`EndExpiredAuctions`。
- DAO / Model：`AuctionItem`、`AuctionRule` 的事务写入、关联关系、分页查询、商家过滤、状态过滤、软删除和过期 ongoing 查询。
- Redis key：`auction:item:{item_id}:state`、`auction:room:{room_id}:item_queue`。
- WebSocket 事件生产：商品模块内只验证 broadcaster 调用和 payload，不验证真实 WebSocket 连接。
- 商品状态、规则字段、分页、关键词、商家归属、统一响应结构、错误返回、Redis 派生字段和 `AuctionPolicy` 响应字段。

Agent 不应在商品模块内测试：

- 出价落库、排行榜排序、幂等出价、自动延时和一口价完整契约；这些属于 `modules/bid.md`。
- 保证金支付和出价资格校验；这些属于 `modules/deposit.md` 和 `modules/bid.md`。
- WebSocket ticket、连接、心跳、断线重连和真实房间广播；这些属于 `modules/ws.md`。
- 订单支付、模拟支付、退款、物流、鉴定或真实第三方服务。
- 房间开播/下播完整业务；这些属于 `modules/room.md`。

## 4. 禁止事项

- 不测试出价、排行榜、订单、支付、物流或直播间开播/下播的完整业务。
- 不调用真实支付、短信、鉴定、物流或其他第三方服务。
- 不直接清空数据库或 Redis。
- 不修改生产配置或复用线上真实商品数据。
- 不把本次测试创建的商品用于真实竞拍。
- 不绕过业务接口直接修改商品状态，除非当前测试明确是故障注入。
- 不自行创造文档和代码中都没有定义的商品状态。
- 本地单元测试不允许直接连接数据库、Redis、HTTP 服务、WebSocket 或外部系统，必须使用 mock/fake 数据。
- Agent 连接线上或线上等价数据库/Redis 时，只能操作本次测试创建的数据或带测试批次 ID 的数据。
- 不在测试报告中写入线上地址、凭据、密码、真实 token 或可复用密钥。

## 5. 测试依赖策略

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 使用 fake store、fake cache、fake broadcaster、固定时间和固定用户身份；禁止直连 MySQL、Redis、HTTP 服务或 WebSocket | 稳定验证商品规则、状态流转、归属校验、Redis 协作语义、事件 payload 和 DTO 计算 |
| Agent 接口契约测试 | 使用真实 handler 或本地服务；使用真实测试数据库和测试 Redis；通过用户模块获取测试 token | 验证真实请求绑定、鉴权中间件、响应结构、错误码和 Redis 侧效果 |
| Agent 模块集成测试 | 使用真实 GORM store、真实测试数据库和真实测试 Redis | 验证事务创建、软删除、待拍队列、竞拍状态初始化、分页和查询过滤 |
| 场景测试 | 使用真实接口链路、真实测试数据库和真实测试 Redis | 验证用户可见业务链路和跨接口状态变化 |
| Agent 并发测试 | 使用真实数据库事务、真实 Redis 和真实 HTTP 并发请求 | mock/fake 无法证明并发状态动作、Redis 初始化和最终一致性 |
| 状态一致性测试 | 对比 HTTP 响应、`auction_items`、`auction_rules`、Redis item state、Redis room queue、事件 payload | 验证接口返回、持久化状态、缓存状态和副作用一致 |
| WebSocket 测试 | 商品模块内部分适用；完整连接测试转到 `modules/ws.md` | 使用 fake broadcaster 或测试 broadcaster 验证商品状态动作产生的事件类型和 payload |

## 6. 全局测试数据准备

```text
测试批次 ID：agent_item_<YYYYMMDDHHMMSS>
商品标题前缀：agent_item_<batch>_
测试房间 ID：room_agent_item_<batch> 或通过房间模块接口创建的测试房间
数据只允许操作本批次创建的数据。
测试结束后必须记录数据库软删除/清理结果和 Redis key 清理结果。
```

需要准备：

- 至少 1 个普通用户账号、2 个商家账号、1 个普通用户 token、2 个商家 token、1 个无效 token。
- 至少 1 个已存在或可识别的测试直播间 ID，用作 `room_id`。
- 至少 1 个合法创建商品请求，必须包含 `room_id`、`title` 和完整 `rule`。
- 非法请求体集合：缺少 `room_id`、缺少 `title`、缺少 `rule`、金额非法、时间非法、字段超长、tag 非法。
- 至少 1 个 `draft`、`published`、`ongoing`、`ended`、`cancelled` 商品。
- Redis 相关测试必须准备或验证：
  - `auction:item:{item_id}:state`
  - `auction:room:{room_id}:item_queue`
- 过期结算测试必须准备 `status=ongoing` 且规则 `end_time` 已早于当前时间的本批次商品。

合法请求体样例：

```json
{
  "room_id": "room_agent_item_<batch>",
  "title": "agent_item_<batch>_jade",
  "description": "天然翡翠，支持鉴定",
  "image_url": "https://example.com/item.png",
  "tags": ["jewelry", "jade"],
  "rule": {
    "start_price": 1000,
    "bid_increment": 100,
    "price_cap": 100000,
    "deposit_amount": 5000,
    "start_time": "2026-05-21T20:00:00+08:00",
    "end_time": "2026-05-21T20:10:00+08:00"
  }
}
```

## 7. 业务规则

事实：

- 商家身份才能创建、修改、删除、上架、开始、取消商品。
- 普通用户或未登录用户不能调用需要商家身份的商品管理接口。
- 创建商品时同步创建 `AuctionItem` 和 `AuctionRule`。
- 创建成功后商品状态为 `draft`，返回 `item_id` 和 `rule_id`。
- 商品 ID 使用 `item_` 前缀，规则 ID 使用 `rule_` 前缀。
- `AuctionItem.RuleID` 必须指向同一个商品的 `AuctionRule.ID`，`AuctionRule.ItemID` 必须等于 `AuctionItem.ID`。
- `room_id` 是 HTTP 创建/修改请求的必填字段，绑定层要求长度 1 到 64。
- 当前 Service 层会保存 `room_id`，但不会校验 `room_id` 是否真实存在或是否属于当前商家。
- `title`、`description`、`image_url` 会去除首尾空格；`tags` 会逐个去除首尾空格，并丢弃空标签。
- `bid_increment` 必须大于 0；`start_price`、`price_cap`、`deposit_amount` 不能小于 0。
- `price_cap > 0` 时不能小于 `start_price`。
- `start_time` 和 `end_time` 必须存在，且 `end_time` 必须晚于 `start_time`。
- HTTP 绑定层要求 `start_price`、`bid_increment` 最小为 1，`price_cap` 和 `deposit_amount` 如果传入则最小为 1。
- Service 层允许 `start_price=0` 和 `deposit_amount=0`，但 HTTP 绑定层当前不允许 `start_price=0`。
- 公开列表和商家列表支持按 `status`、`keyword`、`page`、`page_size` 查询；`page <= 0` 归一为 1，`page_size <= 0` 归一为 10，`page_size > 100` 归一为 100。
- 列表按 `created_at DESC` 排序。
- 公开列表当前没有强制只展示 `published` 或 `ongoing`；不传 `status` 时会返回所有未删除状态商品。
- `current_price` 默认来自 `deal_price`；如果 `deal_price <= 0`，则使用 `start_price`。
- `remaining_ms` 只在状态为 `published` 或 `ongoing` 且结束时间晚于当前时间时大于 0，否则为 0。
- ongoing 商品查询时会尝试读取 `auction:item:{item_id}:state`；Redis 命中时覆盖价格、领先用户、出价数、参与人数、扩展状态和剩余时间。
- ongoing 商品 Redis 未命中或读取错误时，当前实现静默回退到 MySQL/规则字段派生结果。
- 修改商品只允许修改 `draft` 状态商品。
- 删除商品只允许删除 `draft` 或 `published` 状态商品。
- 删除商品使用 GORM `DeletedAt` 对 `AuctionItem` 执行软删除，当前 DAO 不同步删除 `AuctionRule`。
- 上架商品只允许 `draft -> published`；上架成功后若 cache 可用，会写入 `auction:room:{room_id}:item_queue` ZSET。
- 上架写入 Redis room queue 失败当前会被忽略，不影响 HTTP 成功结果。
- 开始竞拍只允许 `published -> ongoing`；开始时若 cache 可用，会初始化 `auction:item:{item_id}:state`。
- 开始竞拍时 Redis 初始化失败会阻止状态变为 `ongoing`。
- 开始竞拍时 Redis 初始化成功但 MySQL 状态更新失败，当前实现会尝试删除 Redis 竞拍状态。
- 开始竞拍成功后会通过 broadcaster 发送 `auction_started` 事件。
- 取消商品只允许从 `published` 或 `ongoing` 变为 `cancelled`。
- 取消成功后若 cache 可用，会从待拍队列移除商品，并删除商品竞拍状态。
- 取消时 Redis 清理失败被当前实现忽略，不影响 HTTP 成功结果。
- 取消成功后会通过 broadcaster 发送 `auction_cancelled` 事件。
- `EndExpiredAuctions` 查询最多 50 个已过 `end_time` 的 ongoing 商品，设置为 `ended`，从 Redis 读取最终领先用户和成交价，删除 Redis item state，并广播 `auction_ended`。
- `EndExpiredAuctions` 在存在 winner 且注入订单服务时会尝试创建订单，并广播/单播 `order_created`；订单失败当前被视为非致命。
- 非商品所属商家操作商品时返回 not found，避免暴露其他商家的商品存在性。

根据当前代码结构推断：

- 商品详情公开可见，不要求登录。
- 商家列表中的 `online_count`、`winner_user_name`、`order_id`、`order_status` 当前不由商品模块填充。
- `CanUnpublish` 当前在 `ended` 状态返回 true，但商品模块没有下架接口。
- 当前状态动作使用先查再保存；并发重复上架、开始或取消是否必须只有一个请求成功，需要用户确认。

需确认内容集中在“需用户确认的问题”章节。

## 8. 业务不变量

- 非商家身份不能创建、修改、删除或改变商品状态。
- 非所属商家不能修改、删除或改变别人的商品状态。
- 每个有效商品必须有且只有一个关联竞拍规则。
- 商品与规则必须互相引用同一组 `item_id` 和 `rule_id`。
- 创建商品和创建规则必须事务一致，不能出现商品存在但规则缺失的成功结果。
- 商品创建后初始状态必须是 `draft`。
- 商品必须保存创建/修改请求中的 `room_id`。
- 商品状态不能跳过定义的状态流转。
- `draft` 以外的商品不能被修改基础信息和竞拍规则。
- `ongoing`、`ended`、`cancelled` 商品不能被删除。
- `cancelled` 商品不能再次上架、开始或取消。
- 规则的 `bid_increment` 必须大于 0。
- 规则的 `price_cap` 如果大于 0，不能低于 `start_price`。
- 规则的 `end_time` 必须晚于 `start_time`。
- 上架成功后，商品应进入所属直播间待拍队列。
- 开始竞拍成功后，商品状态必须是 `ongoing`，Redis 竞拍状态应以规则起拍价和结束时间初始化。
- 开始竞拍失败时，MySQL 状态和 Redis 状态不能留下互相矛盾的成功结果。
- 取消成功后，商品状态必须是 `cancelled`，直播间待拍队列和商品竞拍状态不应继续保留该商品。
- 过期结算后，商品状态必须是 `ended`，`winner_id` 和 `deal_price` 应与 Redis 最终状态一致。
- HTTP 响应中的商品状态必须与数据库状态一致。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 9. 字段规则索引

### CreateItemRequest / UpdateItemRequest

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `room_id` | request/db/response | 必填；HTTP 长度 1 到 64；Service 去除首尾空格；当前不校验真实房间归属 | `POST /api/v1/items`、`PUT /api/v1/items/{item_id}` | `ITEM.FIELD.room_id.*` |
| `title` | request/db/response | 必填；HTTP 长度 1 到 128；Service 去除首尾空格，空标题失败 | 创建、修改、列表、详情 | `ITEM.FIELD.title.*` |
| `description` | request/db/response | 可选；HTTP 最大 1024；Service 去除首尾空格 | 创建、修改、列表、详情 | `ITEM.FIELD.description.*` |
| `image_url` | request/db/response | 可选；HTTP 最大 512；Service 去除首尾空格 | 创建、修改、列表、详情 | `ITEM.FIELD.image_url.*` |
| `tags` | request/db/response | 可选；单个 tag 长度 1 到 64；Service trim 并丢弃空 tag；DB JSON serializer | 创建、修改、列表、详情 | `ITEM.FIELD.tags.*` |
| `rule.start_price` | request/db/response | HTTP 最小 1；Service 允许 0；不能为负 | 创建、修改、详情、商家列表 | `ITEM.FIELD.start_price.*` |
| `rule.bid_increment` | request/db/response | 必填；必须大于 0 | 创建、修改、详情、商家列表 | `ITEM.FIELD.bid_increment.*` |
| `rule.price_cap` | request/db/response | 可选；HTTP 最小 1；Service 允许 0；大于 0 时不能小于 `start_price` | 创建、修改、详情、商家列表 | `ITEM.FIELD.price_cap.*` |
| `rule.deposit_amount` | request/db/response | 可选；HTTP 最小 1；Service 允许 0；不能为负 | 创建、修改、详情、商家列表、保证金模块 | `ITEM.FIELD.deposit_amount.*` |
| `rule.start_time` | request/db/response | 必填；不能为零值 | 创建、修改、详情、商家列表 | `ITEM.FIELD.start_time.*` |
| `rule.end_time` | request/db/response/Redis | 必填；必须晚于 `start_time`；ongoing 时 Redis `end_time_unix` 可覆盖剩余时间计算 | 创建、修改、开始、查询、过期结算 | `ITEM.FIELD.end_time.*` |

### AuctionItem / AuctionRule / Redis State

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `id` | db/response | 商品 ID 以 `item_` 开头，主键长度 64 | 全部商品接口 | `ITEM.FIELD.id.*` |
| `merchant_id` | db/response/auth | 创建时来自当前商家；商家操作必须匹配 | 商家接口、归属隔离 | `ITEM.FIELD.merchant_id.*` |
| `status` | db/response | 枚举：`draft`、`published`、`ongoing`、`ended`、`cancelled`；动作只能进入允许状态 | 状态动作、列表、详情 | `ITEM.FIELD.status.*` |
| `rule_id` / `item_id` | db | 双向关联必须一致；`AuctionRule.ItemID` 唯一 | 创建、修改、DAO 集成 | `ITEM.FIELD.rule_link.*` |
| `winner_id` | db/response/Redis | 过期结算从 Redis `leader_user_id` 写回；详情默认映射为 `leader_user_id` | 详情、商家列表、过期结算 | `ITEM.FIELD.winner_id.*` |
| `deal_price` | db/response/Redis | 过期结算从 Redis `current_price` 写回；`current_price` 默认优先使用成交价 | 商家列表、过期结算 | `ITEM.FIELD.deal_price.*` |
| `current_price` | response/Redis/db | ongoing Redis 命中时用 Redis；否则 `deal_price > 0` 用成交价，否则用起拍价 | 列表、详情、商家列表 | `ITEM.FIELD.current_price.*` |
| `remaining_ms` | response/Redis/time | 仅 `published/ongoing` 且结束时间晚于当前时间时大于 0；Redis 命中时按 Redis `end_time_unix` 计算 | 列表、详情、商家列表 | `ITEM.FIELD.remaining_ms.*` |
| `leader_user_id` | response/Redis/db | 详情和商家列表 ongoing Redis 命中时使用 Redis；否则来自 `WinnerID` | 详情、商家列表 | `ITEM.FIELD.leader_user_id.*` |
| `bid_count` / `participant_count` | response/Redis | ongoing Redis 命中时使用 Redis；miss 时为默认值 | 列表、详情、商家列表 | `ITEM.FIELD.auction_stats.*` |
| `is_extended` | response/Redis | ongoing Redis 命中时使用 Redis `is_extended` | 详情、商家列表 | `ITEM.FIELD.is_extended.*` |
| `actions` | response | `can_edit/can_publish` 仅 draft；`can_start` 仅 published；`can_cancel` 为 published/ongoing；`can_unpublish` 为 ended | 商家列表 | `ITEM.FIELD.actions.*` |

## 10. 接口测试契约

### `GET /api/v1/items` 公开商品列表

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/item/router/item.go` | 公开路由，无鉴权 |
| Handler | `internal/app/item/handler/item.go` | `ListItems`、`listInput`、`queryInt` |
| DTO | `internal/app/item/dto/item.go` | `ListItemsInput`、`ItemListResult`、`ItemListDTO` |
| Service | `internal/app/item/service/service.go` | `ListItems`、`normalizeListInput`、`applyStateToList` |
| DAO | `internal/app/item/dao/item.go` | `ListItems` |
| Cache | `internal/app/item/cache/cache.go` | ongoing 商品读取 `GetAuctionState` |

#### 接口职责

返回公开商品列表、分页信息和公开可见派生字段；不负责鉴权、商家归属管理、出价或成交。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `status` | 否 | 当前按字符串过滤；未知值返回空列表或数据库过滤结果 | 需确认是否应参数错误 |
| `keyword` | 否 | 去除首尾空格；匹配标题或描述 | 不命中返回空列表 |
| `page` | 否 | 非数字解析为 0 后归一为 1；小于等于 0 归一为 1 | 默认分页 |
| `page_size` | 否 | 非数字解析为 0 后归一为 10；大于 100 限制为 100 | 默认或截断 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data.list` | 数组；元素为 `ItemListDTO` | HTTP 响应 |
| `data.page` / `data.page_size` / `data.total` | 与归一化分页和 DAO total 一致 | HTTP 响应 + DB |
| `current_price` / `remaining_ms` / `bid_count` / `participant_count` | ongoing Redis 命中时使用 Redis 派生值 | HTTP 响应 + Redis |

#### 测试数据准备

- 创建本批次多个状态商品，至少覆盖 `draft`、`published`、`ongoing`、`cancelled`。
- 为 ongoing 商品准备 Redis `auction:item:{item_id}:state`。
- 准备关键词命中标题、命中描述、不命中和空白关键词。

#### 成功路径

- 不传参数时返回第一页、每页 10 条、按 `created_at DESC` 排序。
- 指定 `status` 时只返回该状态商品。
- 指定 `keyword` 时只返回标题或描述命中的商品。
- ongoing 商品 Redis 命中时，列表字段使用 Redis 状态。

#### 失败路径

- DAO 查询失败返回错误响应。
- Redis miss 或读取错误不应导致接口失败，应回退到 MySQL/规则字段。
- 未知 `status` 当前不报参数错误，需记录当前行为。

#### 状态和一致性验证

- HTTP 列表字段与数据库商品、规则记录一致。
- ongoing Redis 命中时 HTTP 派生字段与 Redis state 一致。
- Redis miss 时 HTTP 派生字段与 MySQL/规则字段一致。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证分页归一、Redis 覆盖和降级 |
| 接口契约测试 | 是 | 真实 handler 或服务验证查询参数和响应结构 |
| 模块集成测试 | 是 | 真实 DAO 验证分页、排序、过滤 |
| 场景测试 | 是 | 商品创建后公开可查场景覆盖 |
| 并发测试 | 否 | 列表本身不要求并发控制 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

### `GET /api/v1/items/{item_id}` 公开商品详情

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/item/router/item.go` | 公开详情路由 |
| Handler | `internal/app/item/handler/item.go` | `GetItem` |
| DTO | `internal/app/item/dto/item.go` | `ItemDetailDTO`、`RuleDTO`、`AuctionPolicy` |
| Service | `internal/app/item/service/service.go` | `GetItem`、`applyStateToDetail` |
| DAO | `internal/app/item/dao/item.go` | `FindItemWithRule` |
| Cache | `internal/app/item/cache/cache.go` | ongoing 商品读取 `GetAuctionState` |

#### 接口职责

返回单个商品详情、竞拍规则、平台竞拍策略和公开派生状态；不负责商家操作和出价。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | Service 去除首尾空格；不存在或软删除返回 not found | not found |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `rule` | 与 `AuctionRule` 一致 | HTTP 响应 + DB |
| `auction_policy` | 来自 Service 注入的策略 | HTTP 响应 |
| `current_price` / `leader_user_id` / `remaining_ms` | ongoing Redis 命中时使用 Redis state | HTTP 响应 + Redis |

#### 测试数据准备

- 一个 draft/published 商品，用于验证 MySQL/规则派生。
- 一个 ongoing 商品和 Redis state，用于验证实时字段覆盖。
- 一个已软删除商品，用于验证 not found。

#### 成功路径

- 返回商品基础字段、规则字段、竞拍策略和派生字段。
- ongoing Redis 命中时，价格、领先用户、出价数、参与人数、延时状态和剩余时间来自 Redis。
- Redis miss 时，详情仍成功返回。

#### 失败路径

- 商品不存在、被软删除或规则缺失返回 not found。
- DAO 错误返回错误响应。

#### 状态和一致性验证

- 详情中的商品、规则、状态与 DB 一致。
- Redis 命中时详情派生字段与 Redis 一致。
- Redis miss 时详情派生字段与规则/DB 一致。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证 DTO 派生和降级 |
| 接口契约测试 | 是 | 验证公开详情响应结构 |
| 模块集成测试 | 是 | 验证 item/rule 关联查询 |
| 场景测试 | 是 | 创建后详情、开始后详情覆盖 |
| 并发测试 | 否 | 详情本身不要求并发控制 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

### `POST /api/v1/items` 创建商品和竞拍规则

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/item/router/item.go` | 鉴权组内 JSON 绑定 |
| Handler | `internal/app/item/handler/item.go` | `CreateItem`、`web.BindingErrors` |
| DTO | `internal/app/item/dto/item.go` | `CreateItemRequest`、`RuleInput` |
| Service | `internal/app/item/service/service.go` | `CreateItem`、`validateCreateInput` |
| DAO | `internal/app/item/dao/item.go` | `CreateItemWithRule` 事务 |
| Model | `internal/app/item/model/item.go` | `AuctionItem`、`AuctionRule` |

#### 接口职责

商家创建一个草稿商品，并在同一事务中创建竞拍规则；不负责上架、开始竞拍、出价或校验房间真实归属。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `room_id` | 是 | 1 到 64；Service trim 后不能为空 | 参数错误或业务错误 |
| `title` | 是 | 1 到 128；Service trim 后不能为空 | 参数错误或业务错误 |
| `description` | 否 | 最大 1024 | 参数错误 |
| `image_url` | 否 | 最大 512 | 参数错误 |
| `tags` | 否 | 每个 tag 1 到 64；Service trim 并丢弃空 tag | 参数错误或规范化 |
| `rule.start_price` | 是 | HTTP 当前要求最小 1；Service 允许 0 | 参数错误或业务错误 |
| `rule.bid_increment` | 是 | 大于 0 | 参数错误或业务错误 |
| `rule.price_cap` | 否 | 大于等于 0；大于 0 时不能小于起拍价 | 业务错误 |
| `rule.deposit_amount` | 否 | 大于等于 0 | 业务错误 |
| `rule.start_time` / `rule.end_time` | 是 | end 必须晚于 start | 业务错误 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `item_id` | `item_` 前缀 | HTTP 响应 + DB |
| `rule_id` | `rule_` 前缀；与 item 双向关联 | HTTP 响应 + DB |

#### 测试数据准备

- 商家 token、普通用户 token、无效 token、未登录请求。
- 合法请求体和非法请求体集合。
- 如果需要验证事务失败，使用 fake store 或测试 DB 故障注入。

#### 成功路径

- 商家创建成功，返回 `item_id` 和 `rule_id`。
- DB 中商品状态为 `draft`，`merchant_id` 来自当前商家，`room_id` 正确保存。
- 商品和规则双向关联一致，字段已按 Service 规则规范化。

#### 失败路径

- 未登录、普通用户或无效 token 返回未授权。
- 字段绑定失败或业务校验失败时不创建商品/规则。
- DAO 事务失败不能留下孤立商品或孤立规则。

#### 状态和一致性验证

- HTTP 返回、`auction_items`、`auction_rules` 三方一致。
- 失败时数据库无本次请求的新商品和新规则。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store 验证权限、校验、规范化、关联关系 |
| 接口契约测试 | 是 | 验证鉴权、绑定、响应结构 |
| 模块集成测试 | 是 | 验证事务创建和 DB 字段 |
| 场景测试 | 是 | 创建后公开详情 |
| 并发测试 | 是 | 并发创建应无孤立规则 |
| 状态一致性测试 | 是 | HTTP + DB item/rule |

### `GET /api/v1/merchant/items` 商家商品列表

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/item/router/item.go` | 鉴权组内商家列表 |
| Handler | `internal/app/item/handler/item.go` | `ListMerchantItems` |
| DTO | `internal/app/item/dto/item.go` | `MerchantItemListResult`、`MerchantItemDTO`、`ActionsDTO` |
| Service | `internal/app/item/service/service.go` | `ListMerchantItems` |
| DAO | `internal/app/item/dao/item.go` | `ListItems`，注入 `MerchantID` 过滤 |
| Cache | `internal/app/item/cache/cache.go` | ongoing 商品读取 Redis state |

#### 接口职责

返回当前商家自己的商品列表、规则摘要、进度、成交结果占位和当前可执行动作；不返回其他商家的商品。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `status` | 否 | 状态过滤 | 空列表或当前过滤结果 |
| `keyword` | 否 | 标题/描述模糊匹配 | 空列表 |
| `page` / `page_size` | 否 | 与公开列表相同 | 默认或截断 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `merchant_id` | 必须等于当前商家 | HTTP 响应 + DB |
| `rule_summary` | 与规则一致 | HTTP 响应 + DB |
| `progress` | ongoing Redis 命中时使用 Redis state | HTTP 响应 + Redis |
| `result` | 来自 `deal_price`、`winner_id`；订单字段当前可能为空 | HTTP 响应 + DB |
| `actions` | 随状态派生 | HTTP 响应 |

#### 测试数据准备

- 商家 A 和商家 B 各自创建多个商品。
- 至少一个 ongoing 商品带 Redis state。
- 各状态商品用于 actions 验证。

#### 成功路径

- 商家只看到自己的商品。
- 状态、关键词和分页过滤有效。
- ongoing 商品进度来自 Redis。
- actions 与当前状态匹配。

#### 失败路径

- 未登录、普通用户或无效 token 返回未授权。
- DAO 查询失败返回错误响应。
- Redis miss 或读取错误不影响列表成功。

#### 状态和一致性验证

- HTTP 列表与当前商家的 DB 商品集合一致。
- ongoing 进度与 Redis state 一致。
- 非当前商家商品不出现在列表中。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store/cache 验证商家过滤和 actions |
| 接口契约测试 | 是 | 验证鉴权和响应结构 |
| 模块集成测试 | 是 | 验证 DB 过滤、分页、排序 |
| 场景测试 | 是 | 商家后台列表场景覆盖 |
| 并发测试 | 否 | 列表本身不要求并发控制 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、Redis |

### `PUT /api/v1/items/{item_id}` 修改商品和竞拍规则

#### 接口职责

修改当前商家自己的草稿商品及其规则；不允许修改 `published`、`ongoing`、`ended` 或 `cancelled` 商品。

#### 请求字段

字段规则同 `POST /api/v1/items`，`item_id` 为必填路由参数。

#### 响应字段

成功时 `data=null`。后续应通过详情接口或 DB 验证修改结果。

#### 测试数据准备

- 当前商家的 `draft` 商品。
- 当前商家的 `published`、`ongoing`、`cancelled` 商品。
- 另一个商家的商品。
- 合法修改请求和非法请求体集合。

#### 成功路径

- 所属商家修改 draft 成功。
- 商品字段、`room_id` 和规则字段同步更新。
- 商品状态保持 `draft`。

#### 失败路径

- 非商家、未登录、无效 token 失败。
- 非所属商家返回 not found。
- 非 draft 商品返回 invalid request。
- 非法字段失败，DB 不应半更新。

#### 状态和一致性验证

- HTTP 成功后详情和 DB 均返回新字段。
- 失败后商品和规则保持原值。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store 验证状态和归属 |
| 接口契约测试 | 是 | 验证绑定、鉴权、响应 |
| 模块集成测试 | 是 | 验证事务更新 |
| 场景测试 | 是 | 草稿修改场景 |
| 并发测试 | 是 | 修改与上架/删除并发 |
| 状态一致性测试 | 是 | HTTP + DB item/rule |

### `DELETE /api/v1/items/{item_id}` 删除商品

#### 接口职责

删除当前商家自己的未开始商品。当前实现允许删除 `draft` 和 `published`，使用 GORM 软删除 `AuctionItem`，不级联删除 `AuctionRule`。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | 当前商家商品；状态必须为 `draft` 或 `published` | not found 或 invalid request |

#### 响应字段

成功时 `data=null`。

#### 测试数据准备

- `draft`、`published`、`ongoing`、`ended`、`cancelled` 商品。
- 另一个商家的商品。

#### 成功路径

- 删除 draft 或 published 成功。
- 普通详情和列表查不到该商品。
- DB 中 `auction_items.deleted_at` 被设置，或普通查询排除该记录。

#### 失败路径

- 非商家、未登录、无效 token 失败。
- 非所属商家返回 not found。
- ongoing、ended、cancelled 删除失败，DB 不变。

#### 状态和一致性验证

- 删除后普通查询不返回商品。
- 当前实现保留 `AuctionRule`，报告需记录该行为。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store 验证状态限制 |
| 接口契约测试 | 是 | 验证鉴权、路由参数、响应 |
| 模块集成测试 | 是 | 验证软删除和查询排除 |
| 场景测试 | 是 | 删除未开始商品场景 |
| 并发测试 | 是 | 删除与上架/修改并发 |
| 状态一致性测试 | 是 | HTTP + DB |

### `POST /api/v1/items/{item_id}/publish` 上架商品

#### 接口职责

将当前商家的草稿商品上架为 `published`，并将商品加入所属直播间待拍队列。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | 当前商家商品；状态必须为 `draft` | not found 或 invalid request |

#### 响应字段

成功时 `data=null`。

#### 测试数据准备

- 当前商家的 draft 商品。
- 非 draft 商品和另一个商家的商品。
- 测试 Redis `auction:room:{room_id}:item_queue`。

#### 成功路径

- 商品状态变为 `published`。
- Redis room queue 包含该 `item_id`。
- 后台 actions 变为 `can_start=true`、`can_cancel=true`。

#### 失败路径

- 非所属商家返回 not found。
- 非 draft 商品返回 invalid request。
- Redis 写入失败当前不影响成功，但必须记录当前行为。

#### 状态和一致性验证

- DB 状态与详情/商家列表一致。
- Redis ZSET 包含该商品。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake cache 验证 PushToRoomQueue |
| 接口契约测试 | 是 | 验证鉴权、响应 |
| 模块集成测试 | 是 | 验证 DB + Redis |
| 场景测试 | 是 | 发布进入队列 |
| 并发测试 | 是 | 同一商品并发上架 |
| 状态一致性测试 | 是 | HTTP + DB + Redis |

### `POST /api/v1/items/{item_id}/start` 开始竞拍

#### 接口职责

将当前商家的已上架商品开始竞拍为 `ongoing`，初始化 Redis item state，并广播 `auction_started` 事件。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | 当前商家商品；状态必须为 `published` | not found 或 invalid request |

#### 响应字段

成功时 `data=null`。

#### 测试数据准备

- 当前商家的 published 商品和规则。
- Redis 可用、Redis 初始化失败、MySQL 更新失败的场景。
- fake broadcaster 或测试 broadcaster。

#### 成功路径

- Redis item state 初始化，`current_price=start_price`，`end_time_unix=rule.end_time`。
- DB 状态变为 `ongoing`。
- `auction_started` payload 包含 item、room、start_time、end_time。
- 详情接口返回 Redis 派生状态。

#### 失败路径

- 非 published 商品失败。
- Redis 初始化失败时 DB 保持 `published`。
- MySQL 更新失败时尝试删除 Redis item state。
- 非所属商家返回 not found。

#### 状态和一致性验证

- 成功时 HTTP、DB、Redis、事件 payload 一致。
- 失败时不能留下 DB/Redis 矛盾状态。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake cache/broadcaster 验证初始化、回滚、事件 |
| 接口契约测试 | 是 | 验证鉴权、响应 |
| 模块集成测试 | 是 | 验证真实 Redis state |
| 场景测试 | 是 | 开始竞拍初始化状态 |
| 并发测试 | 是 | 同一商品并发开始 |
| 状态一致性测试 | 是 | HTTP + DB + Redis + event |

### `POST /api/v1/items/{item_id}/cancel` 取消竞拍

#### 接口职责

将当前商家的 `published` 或 `ongoing` 商品取消为 `cancelled`，清理 Redis room queue 和 item state，并广播 `auction_cancelled`。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | 当前商家商品；状态必须为 `published` 或 `ongoing` | not found 或 invalid request |

#### 响应字段

成功时 `data=null`。

#### 测试数据准备

- 当前商家的 published 商品和 ongoing 商品。
- Redis room queue 与 item state。
- fake broadcaster 或测试 broadcaster。

#### 成功路径

- DB 状态变为 `cancelled`。
- Redis room queue 不再包含商品。
- Redis item state 被删除。
- 广播 `auction_cancelled`，payload 包含 item ID。

#### 失败路径

- draft、ended、cancelled 商品取消失败。
- 非所属商家返回 not found。
- Redis 清理失败当前不影响成功，但报告需记录当前行为。

#### 状态和一致性验证

- HTTP、DB、Redis 清理结果和事件 payload 一致。
- 失败时 DB/Redis 不应被修改。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake cache/broadcaster 验证清理和事件 |
| 接口契约测试 | 是 | 验证鉴权、响应 |
| 模块集成测试 | 是 | 验证真实 Redis 清理 |
| 场景测试 | 是 | 取消清理状态 |
| 并发测试 | 是 | 同一商品并发取消 |
| 状态一致性测试 | 是 | HTTP + DB + Redis + event |

## 11. Service / DAO 测试契约

### `CreateItem` / `CreateItemWithRule`

#### 测试数据准备

- fake store 初始为空。
- 真实 DB 集成测试准备可识别批次数据。

#### 单元测试点

- 非商家返回 unauthorized。
- 合法输入创建 `draft` 商品和规则，ID 前缀正确。
- `room_id`、标题、描述、图片、tags 规范化。
- 非法金额、时间、空标题、空 room ID 被拒绝。

#### 集成测试点

- 真实 DB 事务创建商品和规则。
- 事务失败时没有孤立记录。

### `UpdateItem` / `UpdateItemWithRule`

#### 单元测试点

- 只允许所属商家修改 `draft` 商品。
- 修改时同步更新商品字段、`room_id` 和规则字段。
- 修改 `published`、`ongoing`、`ended`、`cancelled` 商品失败。
- 非所属商家返回 not found。

#### 集成测试点

- 真实 DB 事务保存商品和规则。
- 更新失败时无半更新。

### `DeleteItem`

#### 单元测试点

- 只允许所属商家删除 `draft` 或 `published` 商品。
- 删除 `ongoing`、`ended`、`cancelled` 商品失败。
- 非所属商家返回 not found。

#### 集成测试点

- GORM 软删除后普通查询和公开详情无法查到商品。
- 当前实现不级联删除 `AuctionRule`，报告需记录该行为。

### `PublishItem` / `StartItem` / `CancelItem`

#### 单元测试点

- 上架只允许 `draft -> published`，成功调用 `PushToRoomQueue(room_id, item_id, score)`。
- 开始只允许 `published -> ongoing`，成功调用 `InitAuctionState(item_id, {current_price:start_price, end_time:rule.end_time})`。
- Redis 初始化失败时开始竞拍失败，商品状态保持 `published`。
- MySQL 更新失败时开始竞拍失败，并尝试删除已初始化的 Redis item state。
- 开始成功后产生 `auction_started` 事件。
- 取消只允许 `published/ongoing -> cancelled`，成功调用 `RemoveFromRoomQueue` 和 `DeleteAuctionState`。
- 取消成功后产生 `auction_cancelled` 事件。
- 取消时 Redis 清理失败不影响当前 Service 返回。

#### 集成测试点

- 真实 DB 状态与真实 Redis key 保持一致。
- 并发状态动作不会进入未定义状态。

### `EndExpiredAuctions`

#### 单元测试点

- 只处理状态为 `ongoing` 且规则 `end_time` 早于当前时间的商品。
- Redis state 命中时将 `leader_user_id` 和 `current_price` 写回 `winner_id` 和 `deal_price`。
- 成功结算后删除 Redis item state，并广播 `auction_ended`。
- 有获胜用户且订单服务可用时尝试创建订单并产生 `order_created` 事件。
- 单个商品更新失败不应阻断后续商品处理。

#### 集成测试点

- 真实 DB 查询不处理未过期 ongoing 商品。
- 结算后 DB、Redis 和事件 payload 一致。

### DTO 派生字段

#### 单元测试点

- `remaining_ms` 在 `draft/cancelled/ended` 状态下为 0。
- `remaining_ms` 在 `published/ongoing` 且未结束时为剩余毫秒数。
- `current_price` 在 `deal_price > 0` 时使用成交价，否则使用起拍价。
- ongoing 商品 Redis 命中时，详情和列表使用 Redis 状态覆盖价格、领先用户、出价数、参与人数和扩展状态。
- ongoing 商品 Redis 未命中时，详情和列表回退到 MySQL/规则字段。
- 后台 `actions` 随状态返回正确布尔值。
- `AuctionPolicy` 默认值和配置覆盖值进入 DTO。

## 12. 核心场景测试

### 场景 1：商家创建商品后公开可查

#### 业务价值

验证商品创建、规则事务、字段归一化和公开详情的基础链路。

#### 关联接口 / 方法

- `POST /api/v1/items`
- `GET /api/v1/items/{item_id}`
- `CreateItem`
- `GetItem`

#### Given

- 已存在一个商家账号和有效 token。
- 已准备一个测试 `room_id`。

#### When

- 调用 `POST /api/v1/items` 创建商品。
- 使用返回的 `item_id` 调用 `GET /api/v1/items/{item_id}`。

#### Then

- 创建接口返回 `item_id` 和 `rule_id`。
- 商品详情中的标题、描述、标签、状态、规则字段与创建请求和规范化结果一致。
- 商品状态为 `draft`。
- 数据库中 `AuctionItem.RuleID` 与 `AuctionRule.ID` 一致，`AuctionRule.ItemID` 与 `AuctionItem.ID` 一致。

#### 证据要求

- HTTP 响应、数据库商品记录、数据库规则记录、清理结果。

### 场景 2：商家修改草稿商品

#### 业务价值

验证草稿商品可编辑性和商品/规则同步更新。

#### 关联接口 / 方法

- `PUT /api/v1/items/{item_id}`
- `GET /api/v1/items/{item_id}`
- `UpdateItem`

#### Given

- 商家已创建一个 `draft` 商品。

#### When

- 调用 `PUT /api/v1/items/{item_id}` 修改商品基础信息、`room_id` 和规则。
- 调用 `GET /api/v1/items/{item_id}` 查询详情。

#### Then

- 修改接口成功。
- 查询详情返回修改后的字段。
- 商品状态仍为 `draft`。
- 数据库商品记录和规则记录均已更新。

#### 证据要求

- HTTP 响应、数据库商品记录、数据库规则记录。

### 场景 3：商品发布进入房间待拍队列

#### 业务价值

验证商品状态与房间待拍队列协作。

#### 关联接口 / 方法

- `POST /api/v1/items/{item_id}/publish`
- `PublishItem`
- Redis `auction:room:{room_id}:item_queue`

#### Given

- 商家已创建一个绑定测试 `room_id` 的 `draft` 商品。

#### When

- 调用 `POST /api/v1/items/{item_id}/publish`。
- 查询数据库商品状态。
- 查询 Redis `auction:room:{room_id}:item_queue`。

#### Then

- HTTP 响应成功。
- 商品状态变为 `published`。
- Redis 待拍队列包含该 `item_id`。
- 详情或商家列表的 actions 显示可开始、可取消。

#### 证据要求

- HTTP 响应、数据库状态、Redis ZSET、后续详情接口。

### 场景 4：商品开始竞拍初始化竞拍状态

#### 业务价值

验证商品进入 ongoing 前必须有 Redis 竞拍状态，避免后续出价读取缺失。

#### 关联接口 / 方法

- `POST /api/v1/items/{item_id}/start`
- `GET /api/v1/items/{item_id}`
- `StartItem`
- Redis `auction:item:{item_id}:state`
- `auction_started` 事件

#### Given

- 商家已创建并上架一个 `published` 商品。

#### When

- 调用 `POST /api/v1/items/{item_id}/start`。
- 查询数据库商品状态。
- 查询 Redis `auction:item:{item_id}:state`。
- 调用公开详情接口。

#### Then

- HTTP 响应成功。
- 商品状态变为 `ongoing`。
- Redis state 中 `current_price` 等于规则 `start_price`。
- Redis state 中 `end_time_unix` 等于规则结束时间。
- 详情接口返回的 `current_price`、`remaining_ms` 与 Redis state 一致。
- 捕获到 `auction_started` 事件时，payload 与商品、房间和规则一致。

#### 证据要求

- HTTP 响应、数据库状态、Redis HGETALL、公开详情、事件记录。

### 场景 5：商品取消清理 Redis 状态

#### 业务价值

验证取消商品不会继续出现在待拍队列或竞拍状态中。

#### 关联接口 / 方法

- `POST /api/v1/items/{item_id}/cancel`
- `CancelItem`
- Redis room queue 和 item state
- `auction_cancelled` 事件

#### Given

- 商家已创建一个 `published` 或 `ongoing` 商品。
- Redis 待拍队列和竞拍状态中存在该商品。

#### When

- 调用 `POST /api/v1/items/{item_id}/cancel`。
- 查询数据库商品状态。
- 查询 Redis 待拍队列和竞拍状态。

#### Then

- HTTP 响应成功。
- 商品状态变为 `cancelled`。
- Redis `auction:room:{room_id}:item_queue` 不再包含该商品。
- Redis `auction:item:{item_id}:state` 不再存在。
- 捕获到 `auction_cancelled` 事件时，payload 包含该 item ID。

#### 证据要求

- HTTP 响应、数据库状态、Redis ZSET、Redis HGETALL 或 key 查询、事件记录。

### 场景 6：商家归属隔离

#### 业务价值

验证商家无法探测或操作其他商家的商品。

#### 关联接口 / 方法

- `GET /api/v1/merchant/items`
- `PUT /api/v1/items/{item_id}`
- `DELETE /api/v1/items/{item_id}`
- `POST /api/v1/items/{item_id}/publish`
- `POST /api/v1/items/{item_id}/start`
- `POST /api/v1/items/{item_id}/cancel`

#### Given

- 商家 A 创建一个商品。
- 商家 B 拥有有效 token。

#### When

- 商家 B 调用修改、删除、上架、开始或取消该商品。
- 商家 B 查询自己的商品列表。

#### Then

- 操作请求返回 not found。
- 商品内容、规则、Redis 状态和数据库状态没有被改变。
- 商家 B 的商品列表不包含商家 A 的商品。

#### 证据要求

- HTTP 响应、数据库记录、Redis 状态、商家列表。

### 场景 7：删除未开始商品

#### 业务价值

验证允许删除的状态和软删除对公开查询的影响。

#### 关联接口 / 方法

- `DELETE /api/v1/items/{item_id}`
- `GET /api/v1/items/{item_id}`
- `DeleteItem`

#### Given

- 商家创建一个 `draft` 或 `published` 商品。

#### When

- 调用 `DELETE /api/v1/items/{item_id}`。
- 再次调用详情接口和列表接口。

#### Then

- 删除接口成功。
- 普通查询无法查到该商品。
- 数据库中 `AuctionItem` 为软删除或无法通过普通查询查到。
- 如需检查规则记录，必须记录当前实现是否保留 `AuctionRule`。

#### 证据要求

- HTTP 响应、详情 not found、列表不包含、DB 软删除记录。

### 场景 8：过期竞拍结算为已结束

#### 业务价值

验证后台结算能把 Redis 最终竞拍状态固化到 MySQL，避免页面和订单链路看到未结算状态。

#### 关联接口 / 方法

- `EndExpiredAuctions`
- Redis `auction:item:{item_id}:state`
- `auction_ended` 事件
- 可选 `order_created` 事件

#### Given

- 存在一个 `ongoing` 商品，规则 `end_time` 已早于固定当前时间。
- Redis state 中存在 `leader_user_id` 和 `current_price`。

#### When

- 调用 `EndExpiredAuctions` 或触发对应 worker。

#### Then

- 商品状态变为 `ended`。
- `winner_id` 等于 Redis `leader_user_id`。
- `deal_price` 等于 Redis `current_price`。
- Redis item state 被删除。
- 产生 `auction_ended` 事件。
- 如果注入订单服务且有 winner，应尝试创建订单并产生 `order_created` 事件。

#### 证据要求

- Service 执行日志或测试命令输出、数据库状态、Redis key 状态、事件记录、订单记录或订单失败记录。

## 13. 状态流转和一致性测试

| 当前状态 | 动作 | 目标状态 | 允许 | 涉及接口 / 方法 | 一致性证据 |
| --- | --- | --- | --- | --- | --- |
| none | create | `draft` | 是 | `POST /api/v1/items` / `CreateItem` | HTTP + DB item/rule |
| `draft` | update | `draft` | 是 | `PUT /api/v1/items/{item_id}` / `UpdateItem` | HTTP + DB item/rule |
| `draft` | delete | soft deleted | 是 | `DELETE /api/v1/items/{item_id}` / `DeleteItem` | HTTP + DB DeletedAt |
| `draft` | publish | `published` | 是 | `POST /api/v1/items/{item_id}/publish` / `PublishItem` | HTTP + DB + Redis room queue |
| `draft` | start | `ongoing` | 否 | `POST /api/v1/items/{item_id}/start` / `StartItem` | 错误响应 + DB 不变 |
| `published` | update | `published` | 否 | `PUT /api/v1/items/{item_id}` / `UpdateItem` | 错误响应 + DB 不变 |
| `published` | delete | soft deleted | 是 | `DELETE /api/v1/items/{item_id}` / `DeleteItem` | HTTP + DB DeletedAt |
| `published` | start | `ongoing` | 是 | `POST /api/v1/items/{item_id}/start` / `StartItem` | HTTP + DB + Redis item state + event |
| `published` | cancel | `cancelled` | 是 | `POST /api/v1/items/{item_id}/cancel` / `CancelItem` | HTTP + DB + Redis 清理 + event |
| `ongoing` | update/delete/publish/start | 不变 | 否 | 对应接口 | 错误响应 + DB/Redis 不变 |
| `ongoing` | cancel | `cancelled` | 是 | `POST /api/v1/items/{item_id}/cancel` / `CancelItem` | HTTP + DB + Redis 清理 + event |
| `ongoing` | expired settlement | `ended` | 是 | `EndExpiredAuctions` | DB + Redis 删除 + event + order 可选 |
| `cancelled` | publish/start/cancel/update/delete | 不变 | 否 | 对应接口 | 错误响应 + DB 不变 |
| `ended` | 商品管理动作 | 不变 | 否 | 修改/删除/状态动作 | 错误响应 + DB 不变 |

状态不一致时，agent 必须记录哪两个数据源不一致、触发步骤，以及是否影响商品展示、商家操作、竞拍启动、房间待拍队列、后续出价或订单流程。

## 14. 并发测试

| 并发目标 | 是否需要 | 真实依赖 | 通过标准 |
| --- | --- | --- | --- |
| 同一商家并发创建多个商品 | 是 | 测试数据库 / HTTP | 每个成功商品都有且只有一个规则，无孤立记录 |
| 同一房间内并发上架多个商品 | 是 | 测试数据库 / Redis / HTTP | Redis 待拍队列包含成功上架商品，无重复 member |
| 同一商品并发上架 | 是 | 测试数据库 / Redis / HTTP | 最终状态合法，Redis ZSET 不出现重复 member，错误可解释 |
| 同一商品并发开始 | 是 | 测试数据库 / Redis / HTTP | 最终 DB 和 Redis item state 一致，不进入未定义状态 |
| 同一商品并发取消 | 是 | 测试数据库 / Redis / HTTP | 最终为 `cancelled`，Redis room queue 和 item state 不保留商品 |
| 修改与上架/删除并发 | 是 | 测试数据库 / HTTP | 非 `draft` 商品不能被修改成功，无半更新 |
| 删除与上架并发 | 是 | 测试数据库 / Redis / HTTP | 最终状态或软删除结果合法，Redis 不留下矛盾状态 |
| 过期结算与取消并发 | 是 | 测试数据库 / Redis / broadcaster | 最终只能是 `ended` 或 `cancelled` 中一个合法结果，事件与 DB 一致 |
| 不同商家并发操作各自商品 | 是 | 测试数据库 / Redis / HTTP | 互不影响 |

根据当前代码结构推断：

- 当前状态流转使用先查再保存，并发重复状态动作可能都返回成功或发生最后写入覆盖；如果要验证严格幂等或单成功语义，需要用户确认预期。
- Redis ZSET 以 item ID 作为 member，并发重复上架同一商品应覆盖 score 而不是产生重复 member；是否允许 score 被后一次请求覆盖需要用户确认。

## 15. WebSocket / Redis / 外部副作用测试

| 副作用 | 触发动作 | 验证方式 | 清理要求 |
| --- | --- | --- | --- |
| Redis `auction:room:{room_id}:item_queue` | `PublishItem` / `POST /publish` | `ZRANGE` 验证成员和 score | 删除本批次 room queue 或移除本批次 item member |
| Redis `auction:item:{item_id}:state` | `StartItem` / `POST /start` | `HGETALL` 验证 `current_price`、`end_time_unix`、统计字段 | 删除本批次 item state |
| Redis room queue 和 item state 清理 | `CancelItem` / `POST /cancel` | `ZRANGE`、`EXISTS` 或 `HGETALL` 验证不再存在 | 记录清理结果 |
| `auction_started` | `StartItem` | fake/test broadcaster 记录 event type 和 payload | 不写真实用户信息 |
| `auction_cancelled` | `CancelItem` | fake/test broadcaster 记录 event type 和 payload | 不写真实用户信息 |
| `auction_ended` | `EndExpiredAuctions` | fake/test broadcaster 记录 event type 和 payload | 不写真实用户信息 |
| `order_created` | `EndExpiredAuctions` 且有 winner/order service | fake/test broadcaster 与订单记录 | 清理本批次订单 |
| 第三方外部服务 | 不适用 | 商品模块当前不应调用真实第三方 | 不允许引入真实第三方依赖 |

## 16. 回归测试

| 风险 | 回归测试位置 | 触发条件 | 证据 |
| --- | --- | --- | --- |
| 普通用户可以创建或管理商品 | 单元 / 接口 | 权限校验变更 | 错误响应或单元断言 |
| 商家可以操作其他商家的商品 | 单元 / 接口 / 场景 | 归属校验变更 | not found 响应 + DB 不变 |
| 创建成功但商品和规则关联缺失 | 单元 / 集成 | DAO 事务或 ID 逻辑变更 | DB item/rule 记录 |
| 创建或修改后 `room_id` 丢失 | 单元 / 接口 | DTO 或 Service 变更 | HTTP 详情 + DB |
| 商品状态可以跳过合法流转 | 单元 / 接口 / 状态一致性 | 状态动作变更 | 错误响应 + DB 不变 |
| 非 `draft` 商品可以被修改 | 单元 / 接口 | 更新规则变更 | 错误响应 + DB 不变 |
| `ongoing` 或 `cancelled` 商品可以被删除 | 单元 / 接口 | 删除规则变更 | 错误响应 + DB 不变 |
| 非法价格或时间被保存 | 单元 / 接口 | 校验逻辑变更 | 错误响应或 DB 不存在 |
| 上架成功但 Redis room queue 没有商品 | 集成 / 场景 | Redis 协作变更 | Redis ZSET |
| 开始成功但 Redis item state 没有初始化 | 集成 / 场景 | Redis 协作变更 | Redis HGETALL |
| Redis 初始化失败但商品状态仍变成 `ongoing` | 单元 / 集成 | 错误处理变更 | DB 状态 + Redis |
| 开始竞拍失败后 Redis 留下孤立 item state | 单元 / 集成 | 回滚逻辑变更 | Redis key 不存在 |
| 取消成功后 Redis 仍保留商品 | 集成 / 场景 | 清理逻辑变更 | Redis ZSET/HGETALL |
| 过期结算没有写回 winner/deal_price | 单元 / 集成 | 结算逻辑变更 | DB + Redis + event |
| ongoing 查询没有使用 Redis 状态 | 单元 / 接口 | DTO 派生变更 | HTTP + Redis |
| Redis miss 导致查询失败 | 单元 / 接口 | 降级逻辑变更 | HTTP 成功响应 |
| 删除后商品仍能通过普通详情查到 | 集成 / 接口 | 软删除逻辑变更 | 详情 not found |
| 接口绑定规则与 Service 规则不一致 | 单元 / 接口 | DTO binding 或 Service 校验变更 | 对比 HTTP 和 Service 单元测试 |
| 并发状态动作导致 MySQL/Redis 状态矛盾 | 并发 / 状态一致性 | 状态动作或 Redis 逻辑变更 | 并发结果 + DB + Redis |

## 17. 测试类型覆盖矩阵

| 测试对象 | 单元 | 接口契约 | 集成 | 场景 | 异常 | 边界 | 并发 | 状态一致性 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v1/items` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GET /api/v1/items` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `GET /api/v1/items/{item_id}` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `GET /api/v1/merchant/items` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `PUT /api/v1/items/{item_id}` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `DELETE /api/v1/items/{item_id}` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `POST /api/v1/items/{item_id}/publish` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `POST /api/v1/items/{item_id}/start` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `POST /api/v1/items/{item_id}/cancel` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `EndExpiredAuctions` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `AuctionItem` / `AuctionRule` 字段 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| Redis item state / room queue | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| 商家归属隔离 | 是 | 是 | 是 | 是 | 是 | 否 | 是 | 是 |
| WebSocket 真实连接 | 否 | 否 | 否 | 否 | 否 | 否 | 否 | 否 |

## 18. 通过标准

**核心验证点（全部通过才算过）：**

- 商品创建、查询、修改、删除和状态动作接口响应结构符合统一响应格式，并有 HTTP 响应作为证据。
- 商家权限和商品归属隔离成立，并有接口响应或 Service 单元测试作为证据。
- 创建商品和规则事务一致，并有数据库查询或 fake store 断言作为证据。
- 商品保存并返回正确的 `room_id`，并有 HTTP 响应或数据库记录作为证据。
- 商品状态只能按文档定义流转，并有状态动作响应和后续详情查询作为证据。
- 非法规则字段被拒绝，并有错误响应或单元测试断言作为证据。
- 修改只允许 `draft` 商品，删除只允许 `draft` 或 `published` 商品，并有接口响应或单元测试作为证据。
- 上架写入 Redis room queue，并有 Redis 查询或 fake cache 断言作为证据。
- 开始初始化 Redis item state，并有 Redis 查询或 fake cache 断言作为证据。
- 取消清理 Redis room queue 和 item state，并有 Redis 查询或 fake cache 断言作为证据。
- 过期结算写回 ended、winner、deal_price，清理 Redis item state，并有 DB、Redis 和事件证据。
- ongoing 查询使用 Redis 状态补全派生字段，并有接口响应和 Redis 状态作为证据。
- 商品详情、列表、数据库和 Redis 状态一致，并有 HTTP 响应、数据库记录和 Redis 记录作为证据。
- 删除后普通查询无法查到商品，并有后续查询响应或数据库普通查询作为证据。

**辅助验证点（建议验证，可附说明跳过）：**

- `AuctionPolicy` 默认值和配置覆盖值正确体现在 DTO 中。
- `remaining_ms`、`current_price`、后台 `actions` 等派生字段符合 DTO 规则。
- 商品模块自动迁移成功创建或更新 `auction_items`、`auction_rules` 表结构。
- 列表分页、关键词和状态过滤符合预期。
- Redis key 的字段类型和序列化结果符合 `item/cache` 当前实现。
- 错误响应中的业务错误码与 `pkg/errorx` 定义一致。

## 19. 需用户确认的问题

- HTTP 接口是否应支持 `start_price=0` 和 `deposit_amount=0`；当前 Service 层支持 0，但接口绑定层对 `start_price` 要求最小为 1。
- 创建或修改商品时是否必须校验 `room_id` 真实存在且属于当前商家；当前商品模块只保存，不校验。
- 公开商品列表不传 `status` 时是否允许返回 `draft` 商品；当前 DAO 会返回所有未删除状态。
- 删除商品时是否应同步删除或软删除 `AuctionRule`；当前实现只删除 `AuctionItem`。
- `published` 商品是否允许删除；当前 Service 允许删除 `draft` 和 `published`。
- 上架写入 Redis room queue 失败时，是否应该阻止商品变为 `published`；当前实现忽略该错误。
- 取消清理 Redis 失败时，是否应该阻止商品变为 `cancelled`；当前实现忽略该错误。
- 并发重复上架、开始或取消时，预期是严格只有一个请求成功，还是允许幂等成功。
- Redis room queue 中同一商品重复上架时 score 被后一次请求覆盖是否符合预期。
- 未知 `status` 查询参数应返回空列表、参数错误，还是保持当前按字符串过滤的行为。
- `CanUnpublish` 在 `ended` 状态返回 true 但没有下架接口，这是否为前端预留字段。
- `EndExpiredAuctions` 创建订单失败时当前只忽略并注释由补偿任务重试；补偿任务的测试契约是否应独立补充。

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
