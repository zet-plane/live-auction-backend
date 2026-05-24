# 商品模块测试说明

## 1. 模块目标

商品模块负责商家拍品的创建、查询、修改、删除和状态流转，并维护拍品对应的竞拍规则、直播间归属和竞拍运行状态的 Redis 协作数据。

当前代码中商品模块的核心实体是 `AuctionItem` 和 `AuctionRule`。商家创建商品时必须绑定 `room_id`，商品初始状态为 `draft`。商品可执行 `draft -> published -> ongoing` 以及 `published/ongoing -> cancelled` 的状态动作。用户侧可以公开查询商品列表和商品详情；商家侧可以查询自己的商品列表，并看到更完整的状态、进度、结果和可操作动作。

## 2. 测试边界

Agent 可以测试：

- 公开商品列表接口 `GET /api/v1/items`。
- 公开商品详情接口 `GET /api/v1/items/{item_id}`。
- 商家创建商品接口 `POST /api/v1/items`。
- 商家商品列表接口 `GET /api/v1/merchant/items`。
- 商家修改商品接口 `PUT /api/v1/items/{item_id}`。
- 商家删除商品接口 `DELETE /api/v1/items/{item_id}`。
- 商家上架商品接口 `POST /api/v1/items/{item_id}/publish`。
- 商家开始竞拍接口 `POST /api/v1/items/{item_id}/start`。
- 商家取消竞拍接口 `POST /api/v1/items/{item_id}/cancel`。
- `CreateItem`、`ListItems`、`ListMerchantItems`、`GetItem`、`UpdateItem`、`DeleteItem`、`PublishItem`、`StartItem`、`CancelItem` Service 逻辑。
- `AuctionItem`、`AuctionRule` 模型字段、关联关系、软删除行为和数据库事务写入。
- `itemcache.Cache` 接口及 Redis key：
  - `auction:item:{item_id}:state`
  - `auction:room:{room_id}:item_queue`
- 商品状态、规则字段、分页、关键词、商家归属、统一响应结构和错误返回。
- ongoing 商品查询时 MySQL 基础数据与 Redis 竞拍状态的合成结果。
- `AuctionPolicy` 默认值和配置覆盖后的响应字段。

当前商品模块不直接处理出价落库、排行榜排序、WebSocket 连接或成交订单。若要验证实时推送、落锤成交、订单和房间当前拍品推进，应转到对应模块或跨模块流程文档。

## 3. 禁止事项

- 不测试出价、排行榜、订单、支付、物流或直播间开播/下播的完整业务。
- 不调用真实支付、短信、鉴定、物流或其他第三方服务。
- 不直接清空数据库或 Redis。
- 不修改生产配置或复用线上真实商品数据。
- 不把本次测试创建的商品用于真实竞拍。
- 不在测试报告中写入数据库地址、Redis 地址、凭据、真实 token 或可复用密钥。
- 不绕过业务接口直接修改商品状态，除非文档明确要求用于故障注入。
- 不自行创造文档和代码中都没有定义的商品状态。
- 本地单元测试不允许直接连接数据库或 Redis，必须使用 mock/fake store 和 fake cache。
- Agent 连接线上或线上等价数据库/Redis 时，只能操作本次测试创建的数据或带测试批次 ID 的数据。

## 4. 业务规则

- 商家身份才能创建、修改、删除、上架、开始、取消商品。
- 普通用户或未登录用户不能调用需要商家身份的商品管理接口。
- 创建商品时同步创建 `AuctionItem` 和 `AuctionRule`。
- 创建成功后商品状态为 `draft`。
- 创建成功后返回 `item_id` 和 `rule_id`。
- 商品 ID 使用 `item_` 前缀，规则 ID 使用 `rule_` 前缀。
- `AuctionItem.RuleID` 必须指向同一个商品的 `AuctionRule.ID`。
- `AuctionRule.ItemID` 必须等于 `AuctionItem.ID`。
- `room_id` 是 HTTP 创建/修改请求的必填字段，绑定层要求长度 1 到 64。
- 当前 Service 层会保存 `room_id`，但不会校验 `room_id` 是否真实存在或是否属于当前商家。
- `title` 会去除首尾空格，不能为空。
- `description`、`image_url` 会去除首尾空格。
- `tags` 会逐个去除首尾空格，并丢弃空标签。
- `bid_increment` 必须大于 0。
- `start_price`、`price_cap`、`deposit_amount` 不能小于 0。
- `price_cap` 大于 0 时不能小于 `start_price`。
- `start_time` 和 `end_time` 必须存在，且 `end_time` 必须晚于 `start_time`。
- 接口绑定层要求 `title` 长度 1 到 128，`description` 最大 1024，`image_url` 最大 512，每个 tag 长度 1 到 64。
- 接口绑定层要求 `start_price`、`bid_increment` 最小为 1，`price_cap` 和 `deposit_amount` 如果传入则最小为 1。
- 当前 Service 层允许 `start_price=0` 和 `deposit_amount=0`，但 HTTP 绑定层不允许这两个值为 0。
- 公开列表和商家列表支持按 `status`、`keyword`、`page`、`page_size` 查询。
- `page <= 0` 时归一为 1。
- `page_size <= 0` 时归一为 10，`page_size > 100` 时归一为 100。
- 列表按 `created_at DESC` 排序。
- 公开列表当前没有强制只展示 `published` 或 `ongoing`；不传 `status` 时会返回所有未删除状态商品。
- 公开列表和详情中的 `current_price` 默认来自 `deal_price`；如果 `deal_price <= 0`，则使用 `start_price`。
- `remaining_ms` 只在状态为 `published` 或 `ongoing` 且结束时间晚于当前时间时大于 0，否则为 0。
- ongoing 商品的公开列表、公开详情和商家列表会尝试从 Redis 读取 `auction:item:{item_id}:state`。
- ongoing 商品 Redis 命中时，公开列表会使用 Redis 的 `current_price`、`bid_count`、`participant_count`、`end_time` 计算字段。
- ongoing 商品 Redis 命中时，公开详情会使用 Redis 的 `current_price`、`leader_user_id`、`bid_count`、`participant_count`、`is_extended`、`end_time` 计算字段。
- ongoing 商品 Redis 命中时，商家列表会使用 Redis 的 `current_price`、`leader_user_id`、`bid_count`、`participant_count`、`is_extended`、`end_time` 计算进度字段。
- ongoing 商品 Redis 未命中或读取错误时，当前实现静默回退到 MySQL/规则字段派生结果。
- 修改商品只允许修改 `draft` 状态商品。
- 删除商品只允许删除 `draft` 或 `published` 状态商品。
- 删除商品使用 GORM `DeletedAt` 对 `AuctionItem` 执行软删除，当前 DAO 不同步删除 `AuctionRule`。
- 上架商品只允许 `draft -> published`。
- 上架成功后，如果 cache 可用，会向 `auction:room:{room_id}:item_queue` ZSET 写入商品 ID，score 为当前 Unix 秒。
- 开始竞拍只允许 `published -> ongoing`。
- 开始竞拍时，如果 cache 可用，会先初始化 `auction:item:{item_id}:state`，其中 `current_price=start_price`，`end_time=rule.end_time`。
- 开始竞拍时 Redis 初始化失败会阻止状态变为 `ongoing`。
- 开始竞拍时 Redis 初始化成功但 MySQL 状态更新失败，当前实现会尝试删除 Redis 竞拍状态。
- 取消商品只允许从 `published` 或 `ongoing` 变为 `cancelled`。
- 取消成功后，如果 cache 可用，会从 `auction:room:{room_id}:item_queue` 移除商品，并删除 `auction:item:{item_id}:state`。
- 取消时 Redis 清理失败被当前实现忽略，不影响 HTTP 成功结果。
- 非商品所属商家操作商品时返回 not found，避免暴露其他商家的商品存在性。

根据当前代码结构推断：

- 商品详情公开可见，不要求登录。
- 商家列表中的 `online_count`、`winner_user_name`、`order_id`、`order_status` 当前不由商品模块填充。
- `ended` 状态在商品模块中只有 DTO 和模型常量，没有对应状态动作入口。
- `CanUnpublish` 当前在 `ended` 状态返回 true，但商品模块没有下架接口。

## 5. 业务不变量

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
- HTTP 响应中的商品状态必须与数据库状态一致。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 6. 测试数据准备

需要准备：

- 至少 1 个普通用户账号。
- 至少 2 个商家账号，用于验证商品归属隔离。
- 至少 1 个有效商家 token。
- 至少 1 个普通用户 token。
- 至少 1 个无效 token。
- 至少 1 个已存在或可识别的测试直播间 ID，用作 `room_id`。
- 至少 1 个合法创建商品请求，必须包含 `room_id`、`title` 和完整 `rule`。
- 至少 1 个非法创建商品请求集合，用于覆盖 `room_id`、标题、金额和时间规则。
- 至少 1 个 `draft` 商品。
- 至少 1 个 `published` 商品。
- 至少 1 个 `ongoing` 商品。
- 至少 1 个 `cancelled` 商品。
- 至少 1 个带测试批次 ID 的商品标题前缀，例如 `agent_item_<batch>_jade`。
- 若执行 Redis 相关测试，必须准备可识别的 Redis key 前缀或 item/room ID，并在报告中记录清理结果。

如果执行接口契约、模块集成、Redis 状态一致性或并发测试，测试商品、测试商家、测试房间和 Redis key 必须可识别，并在测试结束后验证清理或软删除结果。

## 7. 依赖策略建议

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 使用 fake store、fake cache、固定时间、固定用户身份；禁止直连数据库和 Redis | 稳定验证商品规则、状态流转、归属校验、Redis 协作语义和 DTO 计算 |
| Agent 接口契约测试 | 使用真实 handler 或本地服务；允许连接测试数据库和测试 Redis；通过用户模块获取测试 token | 验证真实请求绑定、鉴权中间件、响应结构、错误码和 Redis 侧效果 |
| Agent 模块集成测试 | 使用真实 GORM store、真实 Redis 或线上等价依赖 | 验证事务创建、软删除、待拍队列、竞拍状态初始化、分页和查询过滤 |
| Agent 并发测试 | 使用真实数据库事务、真实 Redis 和真实 HTTP 并发请求 | mock 会掩盖并发状态动作、Redis 初始化和最终一致性问题 |
| 状态一致性测试 | 对比 HTTP 响应、商品表、规则表、Redis item state、Redis room queue 和后续查询接口 | 验证接口返回、持久化状态、缓存状态和派生字段一致 |
| WebSocket 测试 | 商品模块内不适用；如测试推送，应转到实时竞拍模块或跨模块流程文档 | 商品模块当前只写 Redis 协作状态，不直接维护 WebSocket 连接 |

## 8. 单元测试

需要覆盖：

- 非商家创建商品返回未授权。
- 商家创建商品成功，生成 `item_` 和 `rule_` 前缀 ID。
- 创建商品时保存 `room_id`。
- 创建商品时标题、描述、图片 URL、标签被规范化。
- 创建商品时写入 `draft` 状态和当前商家 ID。
- 创建商品时商品与规则双向关联正确。
- 创建商品时 `title` 为空失败。
- 创建商品时 `bid_increment <= 0` 失败。
- 创建商品时金额为负失败。
- 创建商品时 `price_cap < start_price` 失败。
- 创建商品时起止时间为空或 `end_time <= start_time` 失败。
- 公开列表默认分页为 `page=1`、`page_size=10`。
- 列表 `page_size > 100` 被限制为 100。
- 列表关键词去除首尾空格。
- 商家列表只能返回当前商家的商品。
- 非商家查询商家列表返回未授权。
- 获取商品详情时 item ID 去除首尾空格。
- 修改商品只允许所属商家修改 `draft` 商品。
- 修改商品时同步更新商品字段、`room_id` 和规则字段。
- 修改 `published`、`ongoing`、`cancelled` 商品失败。
- 删除商品只允许所属商家删除 `draft` 或 `published` 商品。
- 删除 `ongoing`、`ended`、`cancelled` 商品失败。
- 上架只允许 `draft -> published`。
- 上架成功时调用 `PushToRoomQueue(room_id, item_id, score)`。
- 开始只允许 `published -> ongoing`。
- 开始成功时调用 `InitAuctionState(item_id, {current_price:start_price, end_time:rule.end_time})`。
- Redis 初始化失败时开始竞拍失败，商品状态保持 `published`。
- MySQL 更新失败时开始竞拍失败，并尝试删除已初始化的 Redis item state。
- 取消只允许 `published/ongoing -> cancelled`。
- 取消成功时调用 `RemoveFromRoomQueue(room_id, item_id)` 和 `DeleteAuctionState(item_id)`。
- 取消时 Redis 清理失败不影响当前 Service 返回。
- 非所属商家执行修改、删除、状态动作时返回 not found。
- `remaining_ms` 在 `draft/cancelled/ended` 状态下为 0。
- `remaining_ms` 在 `published/ongoing` 且未结束时为剩余毫秒数。
- `current_price` 在 `deal_price > 0` 时使用成交价，否则使用起拍价。
- ongoing 商品查询 Redis 命中时，详情和列表使用 Redis 状态覆盖价格、领先用户、出价数、参与人数和扩展状态。
- ongoing 商品查询 Redis 未命中时，详情和列表回退到 MySQL/规则字段。
- 后台 `actions` 随状态返回正确布尔值。
- `AuctionPolicy` 默认值和配置覆盖值进入 DTO。

## 9. 接口契约测试

需要覆盖：

- `GET /api/v1/items` 成功响应结构：`code`、`message`、`data.list`、`data.page`、`data.page_size`、`data.total`。
- `GET /api/v1/items` 支持 `status`、`keyword`、`page`、`page_size` 查询参数。
- `GET /api/v1/items` ongoing 商品在 Redis 有状态时返回 Redis 派生的 `current_price`、`bid_count`、`participant_count`、`remaining_ms`。
- `GET /api/v1/items/{item_id}` 成功响应商品详情、规则、平台竞拍策略和 Redis 派生状态。
- `GET /api/v1/items/{item_id}` 查询不存在或已软删除商品返回 not found。
- `POST /api/v1/items` 未登录、普通用户 token、无效 token 均不能创建商品。
- `POST /api/v1/items` 商家创建成功返回 `data.item_id` 和 `data.rule_id`。
- `POST /api/v1/items` 请求体缺少 `room_id`、`title`、`rule`、`rule.start_time` 或 `rule.end_time` 时返回参数错误。
- `POST /api/v1/items` 请求体字段超过绑定长度限制时返回参数错误。
- `POST /api/v1/items` 金额字段不符合接口绑定或 Service 校验时返回错误。
- `GET /api/v1/merchant/items` 需要商家身份，并只返回当前商家的商品。
- `GET /api/v1/merchant/items` ongoing 商品在 Redis 有状态时返回 Redis 派生的 `progress` 字段。
- `PUT /api/v1/items/{item_id}` 修改 `draft` 商品成功响应 `data=null`。
- `PUT /api/v1/items/{item_id}` 修改非 `draft` 商品失败。
- `PUT /api/v1/items/{item_id}` 非所属商家修改返回 not found。
- `DELETE /api/v1/items/{item_id}` 删除 `draft` 或 `published` 商品成功响应 `data=null`。
- `DELETE /api/v1/items/{item_id}` 删除 `ongoing`、`ended`、`cancelled` 商品失败。
- `POST /api/v1/items/{item_id}/publish` 上架 `draft` 商品成功，并在 Redis room queue 中出现该商品。
- `POST /api/v1/items/{item_id}/start` 开始 `published` 商品成功，并在 Redis item state 中出现该商品。
- `POST /api/v1/items/{item_id}/cancel` 取消 `published` 或 `ongoing` 商品成功，并清理 Redis room queue 和 item state。
- 状态动作重复提交或非法状态提交返回业务错误。

## 10. 业务场景测试

### 商家创建商品后公开可查

Given:

- 已存在一个商家账号和有效 token。
- 已准备一个测试 `room_id`。
- 创建请求包含合法商品信息和竞拍规则。

When:

- 调用 `POST /api/v1/items` 创建商品。
- 使用返回的 `item_id` 调用 `GET /api/v1/items/{item_id}`。

Then:

- 创建接口返回 `item_id` 和 `rule_id`。
- 商品详情中的标题、描述、标签、状态、规则字段与创建请求和规范化结果一致。
- 商品详情或商家列表中的 `room_id` 与请求一致。
- 商品状态为 `draft`。
- 数据库中 `AuctionItem.RuleID` 与 `AuctionRule.ID` 一致，`AuctionRule.ItemID` 与 `AuctionItem.ID` 一致。

### 商家修改草稿商品

Given:

- 商家已创建一个 `draft` 商品。

When:

- 调用 `PUT /api/v1/items/{item_id}` 修改商品基础信息、`room_id` 和规则。
- 调用 `GET /api/v1/items/{item_id}` 查询详情。

Then:

- 修改接口成功。
- 查询详情返回修改后的字段。
- 商品状态仍为 `draft`。
- 数据库商品记录和规则记录均已更新。

### 商品发布进入房间待拍队列

Given:

- 商家已创建一个绑定测试 `room_id` 的 `draft` 商品。

When:

- 调用 `POST /api/v1/items/{item_id}/publish`。
- 查询数据库商品状态。
- 查询 Redis `auction:room:{room_id}:item_queue`。

Then:

- HTTP 响应成功。
- 商品状态变为 `published`。
- Redis 待拍队列包含该 `item_id`。
- score 可作为上架时间排序证据。

### 商品开始竞拍初始化竞拍状态

Given:

- 商家已创建并上架一个 `published` 商品。

When:

- 调用 `POST /api/v1/items/{item_id}/start`。
- 查询数据库商品状态。
- 查询 Redis `auction:item:{item_id}:state`。
- 调用公开详情接口。

Then:

- HTTP 响应成功。
- 商品状态变为 `ongoing`。
- Redis state 中 `current_price` 等于规则 `start_price`。
- Redis state 中 `end_time` 等于规则 `end_time`。
- 详情接口返回的 `current_price`、`remaining_ms` 与 Redis state 一致。

### 商品取消清理 Redis 状态

Given:

- 商家已创建一个 `published` 或 `ongoing` 商品。
- Redis 待拍队列和竞拍状态中存在该商品。

When:

- 调用 `POST /api/v1/items/{item_id}/cancel`。
- 查询数据库商品状态。
- 查询 Redis 待拍队列和竞拍状态。

Then:

- HTTP 响应成功。
- 商品状态变为 `cancelled`。
- Redis `auction:room:{room_id}:item_queue` 不再包含该商品。
- Redis `auction:item:{item_id}:state` 不再存在。

### 商家归属隔离

Given:

- 商家 A 创建一个商品。
- 商家 B 拥有有效 token。

When:

- 商家 B 调用修改、删除、上架、开始或取消该商品。

Then:

- 请求返回 not found。
- 商品内容、规则、Redis 状态和数据库状态没有被改变。
- 商家 B 的商品列表不包含商家 A 的商品。

### 删除未开始商品

Given:

- 商家创建一个 `draft` 或 `published` 商品。

When:

- 调用 `DELETE /api/v1/items/{item_id}`。
- 再次调用详情接口和列表接口。

Then:

- 删除接口成功。
- 普通查询无法查到该商品。
- 数据库中 `AuctionItem` 为软删除或无法通过普通查询查到。
- 如需检查规则记录，必须记录当前实现是否保留 `AuctionRule`。

## 11. 异常测试

需要覆盖：

- 未登录创建商品。
- 普通用户创建商品。
- 商家 token 无效或过期。
- 创建商品时 `room_id` 缺失。
- 创建商品时 `room_id` 为空或超过 64。
- 创建商品时 `room_id` 不存在或不属于当前商家的当前实现行为。
- 创建商品时标题为空或只有空格。
- 创建商品时标题超过 128。
- 描述超过 1024。
- 图片 URL 超过 512。
- tag 为空字符串。
- tag 超过 64。
- `rule` 缺失。
- `start_price` 小于 0。
- `bid_increment` 等于 0 或小于 0。
- `price_cap` 小于 0。
- `price_cap` 大于 0 但小于 `start_price`。
- `deposit_amount` 小于 0。
- `start_time` 为空或格式非法。
- `end_time` 为空或格式非法。
- `end_time` 等于或早于 `start_time`。
- 查询不存在商品。
- 修改不存在商品。
- 删除不存在商品。
- 状态动作传入不存在商品。
- 非所属商家修改、删除或状态动作。
- 非商家访问商家商品列表。
- 修改非 `draft` 商品。
- 删除非 `draft/published` 商品。
- `draft` 商品直接开始竞拍。
- `ongoing` 商品重复上架。
- `cancelled` 商品再次取消、上架或开始。
- 上架时 Redis 写入待拍队列失败的当前实现行为。
- 开始竞拍时 Redis 初始化失败。
- 开始竞拍时 MySQL 状态更新失败后的 Redis 回滚。
- 取消时 Redis 删除待拍队列或竞拍状态失败的当前实现行为。
- 查询 ongoing 商品时 Redis key 不存在。
- 查询 ongoing 商品时 Redis 字段缺失或格式异常。
- 查询参数 `page`、`page_size` 不是数字时按当前实现会被解析为 0 并走默认分页。

## 12. 边界测试

需要覆盖：

- `room_id` 长度刚好 1。
- `room_id` 长度刚好 64。
- `room_id` 长度为 65。
- `title` 长度刚好 1。
- `title` 长度刚好 128。
- `title` 长度为 129。
- `description` 长度刚好 1024。
- `description` 长度为 1025。
- `image_url` 长度刚好 512。
- `image_url` 长度为 513。
- tag 长度刚好 1。
- tag 长度刚好 64。
- tag 长度为 65。
- `start_price` 为 0 的 Service 层行为。
- `start_price` 为 1 的接口层行为。
- `bid_increment` 为 1。
- `price_cap` 等于 `start_price`。
- `price_cap` 比 `start_price` 小 1。
- `deposit_amount` 为 0 的 Service 层行为。
- `deposit_amount` 为 1 的接口层行为。
- `end_time` 比 `start_time` 晚 1 纳秒或最小可表达时间单位。
- `end_time` 等于 `start_time`。
- Redis `end_time_unix` 早于、等于、晚于当前时间时 `remaining_ms` 的返回值。
- Redis `current_price` 等于起拍价、超过起拍价、超过一口价时的当前实现行为。
- `page` 为 0、负数和正数。
- `page_size` 为 0、负数、1、100、101。
- `status` 为空、合法状态和未知状态。
- `keyword` 为空、只有空格、命中标题、命中描述和不命中。
- `remaining_ms` 在结束时间刚好早于、等于、晚于当前时间时的返回值。

## 13. 并发测试

需要覆盖：

- 同一商家并发创建多个商品。
- 同一房间内并发上架多个商品。
- 同一商品并发执行上架。
- 同一商品并发执行开始。
- 同一商品并发执行取消。
- 同一商品并发修改和上架。
- 同一商品并发删除和修改。
- 同一商品并发删除和上架。
- 两个不同商家同时操作各自商品，互不影响。

验证要求：

- 每个成功创建的商品都有且只有一个规则。
- 并发上架后 Redis 待拍队列不应出现同一商品的重复成员。
- 并发开始后 Redis item state 和 MySQL 商品状态不能互相矛盾。
- 并发取消后 Redis 待拍队列和 item state 最终不保留已取消商品。
- 并发状态动作最终只能落入一个合法状态。
- 非法重复状态动作不能导致跳转到未定义状态。
- 并发修改和状态动作不能让非 `draft` 商品被修改成功。
- 并发删除后普通查询不能继续查到已删除商品。
- 数据库中不能出现商品存在但规则缺失的成功创建结果。

根据当前代码结构推断：

- 当前状态流转使用先查再保存，并发重复状态动作可能都返回成功或发生最后写入覆盖；如果要验证严格幂等或单成功语义，需要用户确认预期。
- Redis ZSET 以 item ID 作为 member，并发重复上架同一商品应覆盖 score 而不是产生重复 member；是否允许 score 被后一次请求覆盖需要用户确认。

## 14. 状态一致性测试

需要验证：

- 创建接口返回的 `item_id`、`rule_id` 与数据库记录一致。
- 商品详情接口与数据库商品记录、规则记录一致。
- 公开列表中的商品字段与详情接口一致。
- 商家列表只包含当前商家的商品。
- 修改商品后详情接口、商家列表和数据库记录一致。
- 删除商品后详情接口、列表接口和数据库普通查询均不再返回该商品。
- 上架后 HTTP 响应、数据库商品状态、详情接口和 Redis room queue 一致。
- 开始后 HTTP 响应、数据库商品状态、Redis item state、详情接口和商家列表一致。
- 取消后 HTTP 响应、数据库商品状态、Redis room queue、Redis item state、详情接口和商家列表一致。
- ongoing 商品 Redis 命中时，详情、公开列表、商家列表中的价格、领先用户、出价数、参与人数、扩展状态和剩余时间与 Redis state 一致。
- Redis 未命中或读取错误时，查询接口能回退到 MySQL/规则字段，且报告中记录降级证据。
- `remaining_ms` 与状态、结束时间、当前时间一致。
- `current_price` 与 Redis state、`deal_price` 或 `start_price` 的选择规则一致。
- 错误响应的 `code`、`message` 与错误类型一致。

状态不一致时，agent 必须记录：

- 哪两个数据源不一致。
- 哪个接口或步骤触发不一致。
- 不一致是否影响商品展示、商家操作、竞拍启动、房间待拍队列或后续出价流程。

## 15. WebSocket 测试

当前商品模块内不直接测试 WebSocket。

商品模块现在会写入 Redis 协作状态：

- 上架写入 `auction:room:{room_id}:item_queue`。
- 开始写入 `auction:item:{item_id}:state`。
- 取消清理上述 Redis 状态。

如果需要验证 `auction_started`、取消通知、排行榜、实时价格或直播间事件推送，应在实时竞拍模块、房间模块或跨模块流程文档中定义，并把商品模块的 Redis 写入作为前置条件或状态一致性证据。

## 16. 回归测试

以下问题一旦出现，必须沉淀为回归测试：

- 普通用户可以创建或管理商品。
- 商家可以修改或删除其他商家的商品。
- 创建成功但商品和规则关联缺失。
- 创建成功后初始状态不是 `draft`。
- 创建或修改后 `room_id` 丢失。
- 商品状态可以跳过合法流转。
- 非 `draft` 商品可以被修改。
- `ongoing` 或 `cancelled` 商品可以被删除。
- `price_cap` 小于 `start_price` 仍被保存。
- `end_time <= start_time` 仍被保存。
- 上架成功但 Redis room queue 没有商品。
- 开始成功但 Redis item state 没有初始化。
- Redis 初始化失败但商品状态仍变成 `ongoing`。
- 开始竞拍失败后 Redis 留下孤立 item state。
- 取消成功后 Redis room queue 或 item state 仍保留商品。
- ongoing 商品查询没有使用 Redis 中的当前价、领先用户或统计字段。
- Redis miss 导致商品详情或列表接口失败。
- 公开详情或列表返回的状态与数据库不一致。
- 删除后商品仍能通过普通详情接口查到。
- 接口绑定规则与 Service 规则不一致导致 HTTP 入口和 Service 单元测试结论冲突。
- 并发状态动作导致商品进入未定义状态或 MySQL/Redis 状态矛盾。

## 17. 通过标准

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

## 18. 需用户确认的问题

- HTTP 接口是否应支持 `start_price=0` 和 `deposit_amount=0`；当前 Service 层支持 0，但接口绑定层要求最小为 1。
- 创建或修改商品时是否必须校验 `room_id` 真实存在且属于当前商家；当前商品模块只保存，不校验。
- 公开商品列表不传 `status` 时是否允许返回 `draft` 商品；当前 DAO 会返回所有未删除状态。
- 删除商品时是否应同步删除或软删除 `AuctionRule`；当前实现只删除 `AuctionItem`。
- `published` 商品是否允许删除；当前 Service 允许删除 `draft` 和 `published`。
- 上架写入 Redis room queue 失败时，是否应该阻止商品变为 `published`；当前实现忽略该错误。
- 取消清理 Redis 失败时，是否应该阻止商品变为 `cancelled`；当前实现忽略该错误。
- 并发重复上架、开始或取消时，预期是严格只有一个请求成功，还是允许幂等成功。
- Redis room queue 中同一商品重复上架时 score 被后一次请求覆盖是否符合预期。
- 未知 `status` 查询参数应返回空列表、参数错误，还是保持当前按字符串过滤的行为。
- 商品状态 `ended` 当前没有商品模块入口触发，是否由后续出价/结算模块负责。
- `CanUnpublish` 在 `ended` 状态返回 true 但没有下架接口，这是否为前端预留字段。

## 19. 失败报告格式

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
