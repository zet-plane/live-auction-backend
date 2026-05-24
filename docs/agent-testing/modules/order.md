# 订单模块测试说明

## 1. 模块目标

订单模块负责竞拍成交后的订单生成、订单查询、订单状态流转和订单定时补偿。核心实体是 `Order`，状态包括 `pending`、`paid`、`cancelled`、`expired`。

当前订单创建不是公开 HTTP 接口：商品模块在一口价封顶成交或定时结算竞拍结束商品时调用 `order.Service.CreateOrder`；订单模块自身提供用户/商家订单列表和详情查询；支付模块通过订单服务完成支付和取消。

## 2. 代码定位索引

| 对象 | 代码位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/order/router/order.go` | 注册鉴权订单查询接口 |
| Handler | `internal/app/order/handler/order.go` | 查询参数解析、鉴权上下文、统一响应 |
| DTO | `internal/app/order/dto/order.go` | 列表、详情、补偿扫描和支付请求 DTO |
| Service | `internal/app/order/service/service.go` | 创建订单、支付、取消、查询、身份隔离 |
| Cron Service | `internal/app/order/service/cron.go` | 过期订单扫描、成交未建单补偿扫描 |
| DAO | `internal/app/order/dao/order.go` | `Store` 接口、GORM 查询、状态条件更新、商品表 JOIN |
| Model | `internal/app/order/model/order.go` | `Order` GORM 模型和状态常量 |
| 初始化 | `internal/app/order/init.go` | AutoMigrate、导出 `order.Svc`、注册 cron |
| 单元测试建议位置 | `internal/app/order/service/*_test.go` | 使用 fake store、固定时间、固定身份 |
| Agent 测试契约 | `docs/agent-testing/modules/order.md` | 接口契约、集成、场景和一致性测试边界 |

## 3. 测试边界

Agent 可以测试：

- HTTP 接口：`GET /api/v1/orders`、`GET /api/v1/orders/{order_id}`。
- Service 方法：`CreateOrder`、`Pay`、`Cancel`、`ListOrders`、`GetOrder`、`ScanExpiredOrders`、`ScanCompensation`。
- DAO / Model：`orders` 表写入、按 `item_id` 幂等查询、状态条件更新、订单列表 JOIN `auction_items`。
- 与订单直接相关的商品表状态：`auction_items.status='ended'`、`winner_id`、`deal_price`、`merchant_id`、软删除过滤。
- 订单 cron 日志关键字：`[order] ScanExpiredOrders`、`[order] ScanCompensation`。

当前订单模块不测试保证金支付、出价排序、WebSocket 推送、真实三方支付、发货、售后或退款。支付接口应转到 `payment.md`，保证金接口应转到 `deposit.md`，完整竞拍到订单链路应转到流程文档。

## 4. 禁止事项

- 不调用真实支付、短信、物流、退款或其他第三方服务。
- 不直接清空 `orders`、`auction_items` 或其他业务表。
- 不修改生产配置或复用线上真实订单数据。
- 不绕过业务接口批量改写非本批次订单状态。
- 不自行创造当前代码没有定义的订单状态。
- 不把订单模块测试扩大为完整竞拍、支付、履约或退款流程。
- 本地单元测试不允许直接连接数据库、Redis、HTTP 服务、WebSocket 或外部系统，必须使用 mock/fake 数据。
- Agent 连接线上或线上等价数据库时，只能操作本次测试创建的数据或带测试批次 ID 的数据。
- 不在测试报告中写入线上地址、凭据、密码、真实 token 或可复用密钥。

## 5. 测试依赖策略

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 使用 fake store、固定时间和固定用户身份；禁止直连 MySQL、Redis、HTTP 服务或 WebSocket | 稳定验证订单幂等、身份隔离、状态流转、过期判断和 cron 分支 |
| Agent 接口契约测试 | 使用真实 handler 或本地服务；使用真实测试数据库；通过用户模块获取 token | 验证鉴权、查询参数、响应结构、用户/商家可见范围 |
| Agent 模块集成测试 | 使用真实 GORM store 和真实测试数据库 | 验证 JOIN、状态条件更新、分页、排序和补偿扫描 SQL |
| 场景测试 | 使用真实接口链路和真实测试数据库；订单创建可通过商品成交链路或 Service/DAO 准备本批次数据 | 验证用户可见业务链路和跨接口状态变化 |
| Agent 并发测试 | 使用真实测试数据库和真实 HTTP 并发请求 | mock/fake 无法证明状态条件更新和重复创建的真实结果 |
| 状态一致性测试 | 对比 HTTP 响应、`orders` 表、`auction_items` 表和日志证据 | 验证接口返回和持久化状态一致 |
| Redis / WebSocket 测试 | 订单模块内不适用 | 当前订单模块不直接读写 Redis，也不发送 WebSocket 消息 |

## 6. 全局测试数据准备

```text
测试批次 ID：agent_order_<YYYYMMDDHHMMSS>
商品标题前缀：agent_order_<batch>_
订单数据只允许关联本批次创建的 auction_items 和 users。
测试结束后必须记录 orders 与 auction_items 的清理结果或软删除验证结果。
```

需要准备：

- 至少 1 个普通用户账号、2 个商家账号、对应 token、1 个无效 token。
- 至少 1 个本批次 `ended` 商品，包含 `winner_id`、`deal_price` 和所属 `merchant_id`。
- 至少 1 个本批次已软删除商品，用于验证订单 JOIN 过滤。
- 订单状态数据：`pending`、`paid`、`cancelled`、`expired`。
- 过期订单数据：`pending` 且 `expired_at < now`。
- 未过期订单数据：`pending` 且 `expired_at > now`。
- 补偿扫描数据：`ended`、`winner_id != ''`、无订单的商品。
- 非法输入集合：不存在的 `order_id`、他人用户订单、非所属商家订单、未知 `status`、非法分页参数。

## 7. 业务规则

事实：

- 订单 ID 使用 `order_` 前缀。
- `CreateOrder(itemID, userID, price)` 创建 `pending` 订单，`ExpiredAt = now + paymentTimeout`；当前初始化中 `paymentTimeout` 为 30 分钟。
- `CreateOrder` 先按 `item_id` 查询已有订单，存在则返回已有订单；`orders.item_id` 也声明了命名唯一索引 `idx_orders_item_id_unique`，用于数据库层强制一个商品最多一个订单。
- `CreateOrder` 如果查询已有订单遇到非 not found 错误，会直接返回错误，不创建新订单。
- `Pay` 只允许订单所属用户操作。
- `Pay` 会拒绝 `now > ExpiredAt` 的订单。
- `Pay` 使用 `UpdateOrderStatus(orderID, pending, paid)` 做条件更新；如果条件更新失败但重新查询发现状态已是 `paid`，当前实现视为幂等成功。
- `Cancel` 只允许订单所属用户操作。
- `Cancel` 使用 `UpdateOrderStatus(orderID, pending, cancelled)` 做条件更新；非 pending 状态取消失败。
- `ListOrders` 登录用户只能看自己的订单；商家只能看自己商品产生的订单。
- `ListOrders` 支持按 `status`、`page`、`page_size` 查询；`page <= 0` 归一为 1；`page_size <= 0` 或 `page_size > 100` 归一为 20。
- 订单列表按 `orders.created_at DESC` 排序。
- `GetOrder` 普通用户只能看自己的订单；商家只能看自己商品产生的订单。
- 订单详情和列表都 JOIN 未软删除的 `auction_items`；商品被软删除时订单查询结果会被过滤或 not found。
- `ScanExpiredOrders` 每次最多处理 100 条已过期 pending 订单，将其更新为 `expired`。
- `ScanCompensation` 每次最多扫描 50 个已结束、有赢家、无订单的商品并调用 `CreateOrder`。
- `order.Load` 注册 `@every 5m` 过期扫描和 `@every 10m` 补偿扫描。

根据当前代码结构推断：

- 订单创建的业务入口是商品成交，不应由普通客户端直接创建。
- 商家可以查询自己商品产生的订单详情，但不能通过订单服务支付或取消订单。
- 未知 `status` 当前会作为数据库过滤条件，不会在 handler 层被拒绝。
- 支付成功后当前订单模块只更新订单状态，不触发外部支付流水、库存、发货或通知。

需确认内容集中在“需用户确认的问题”章节。

## 8. 业务不变量

- 每个成交商品最多只能有一个订单。
- 订单创建后初始状态必须为 `pending`。
- 订单 `item_id`、`user_id`、`price` 必须来自成交商品、赢家和成交价，不能由查询接口修改。
- 非订单所属用户不能支付或取消订单。
- 非订单所属用户、非商品所属商家不能查看订单详情。
- 订单状态只能按当前实现允许的路径变化：`pending -> paid`、`pending -> cancelled`、`pending -> expired`。
- `paid` 订单不能被取消，`cancelled` 或 `expired` 订单不能被支付。
- 已过期订单不能支付。
- 订单列表和详情不得泄露其他用户或其他商家的订单。
- HTTP 响应中的订单状态必须与 `orders` 表一致。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 9. 字段规则索引

### Order / OrderDetail / OrderWithTitle

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `id` | db/response | 订单 ID 使用 `order_` 前缀，主键长度 64 | 全部订单查询、支付、取消 | `ORDER.FIELD.id.*` |
| `item_id` | db/response | 关联成交商品；`CreateOrder` 按该字段幂等 | 创建、列表、详情、补偿扫描 | `ORDER.FIELD.item_id.*` |
| `item_title` | db/response | 来自未软删除 `auction_items.title` | 列表、详情 | `ORDER.FIELD.item_title.*` |
| `item_merchant_id` | db/internal | 详情查询内部字段，不输出 JSON；用于商家归属校验 | `GetOrder` | `ORDER.FIELD.item_merchant_id.*` |
| `user_id` | db/response/auth | 订单所属用户；支付、取消、用户查询必须匹配 | 创建、支付、取消、列表、详情 | `ORDER.FIELD.user_id.*` |
| `price` | db/response | 成交价，创建后当前无修改入口 | 创建、列表、详情 | `ORDER.FIELD.price.*` |
| `status` | db/response/query | 枚举：`pending`、`paid`、`cancelled`、`expired`；查询按字符串过滤 | 列表、详情、支付、取消、过期扫描 | `ORDER.FIELD.status.*` |
| `expired_at` | db/response/time | 创建时为当前时间加支付超时；支付时若已过期则失败 | 创建、支付、列表、详情、过期扫描 | `ORDER.FIELD.expired_at.*` |
| `created_at` / `updated_at` | db/response | GORM 自动维护；列表按 `created_at DESC` | 列表、详情 | `ORDER.FIELD.timestamps.*` |

### ListOrdersInput / PayOrderRequest

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `status` | query | 可选；当前无枚举校验，直接用于 DB 过滤 | `GET /api/v1/orders` | `ORDER.FIELD.query_status.*` |
| `page` | query | 非正数归一为 1 | `GET /api/v1/orders` | `ORDER.FIELD.page.*` |
| `page_size` | query | 非正数或大于 100 归一为 20 | `GET /api/v1/orders` | `ORDER.FIELD.page_size.*` |
| `result` | request | 支付接口 JSON 必填且只能为 `success`；订单服务仍不读取具体值 | `POST /api/v1/orders/{order_id}/pay`（payment 模块） | `ORDER.FIELD.pay_result.*` |

## 10. 接口测试契约

### `GET /api/v1/orders` 订单列表

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/order/router/order.go` | 鉴权路由 |
| Handler | `internal/app/order/handler/order.go` | `ListOrders` 查询参数解析 |
| DTO | `internal/app/order/dto/order.go` | `ListOrdersInput`、`ListOrdersResult`、`OrderWithTitle` |
| Service | `internal/app/order/service/service.go` | `ListOrders` 身份过滤、分页归一 |
| DAO | `internal/app/order/dao/order.go` | `ListOrders` JOIN 商品表、分页、排序 |
| Model | `internal/app/order/model/order.go` | `Order` 状态字段 |

#### 接口职责

返回当前登录身份可见的订单列表；普通用户按 `orders.user_id` 隔离，商家按 `auction_items.merchant_id` 隔离。该接口不负责创建、支付、取消或修改订单。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `status` | 否 | 当前无枚举校验，直接过滤 DB | 未命中返回空列表 |
| `page` | 否 | 非正数归一为 1 | 返回归一后分页 |
| `page_size` | 否 | 非正数或大于 100 归一为 20 | 返回归一后分页 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data.list` | 仅包含当前身份可见订单 | HTTP 响应 + DB |
| `data.page` / `data.page_size` / `data.total` | 与归一化分页和 DB total 一致 | HTTP 响应 + DB |
| `item_title` | 来自未软删除商品标题 | HTTP 响应 + `auction_items` |

#### 测试数据准备

- 同一普通用户不同状态订单、其他用户订单、同一商家商品订单、其他商家商品订单。
- 至少 1 条商品已软删除的订单，用于验证 JOIN 过滤。
- 分页数据量超过 1 页。

#### 成功路径

- 普通用户只看到自己的订单。
- 商家只看到自己商品产生的订单。
- `status=pending` 只返回 pending 订单。
- 分页参数归一和排序符合预期。

#### 失败路径

- 未登录或无效 token 返回未授权。
- DAO 查询失败返回错误响应。
- 未知 `status` 当前返回空列表或数据库过滤结果，必须记录当前行为。

#### 状态和一致性验证

- HTTP 列表与 `orders` 和 `auction_items` JOIN 结果一致。
- 不返回软删除商品对应的订单。
- 不返回其他用户或其他商家的订单。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store 验证身份过滤和分页归一 |
| 接口契约测试 | 是 | 真实 handler 验证鉴权、查询参数和响应形状 |
| 模块集成测试 | 是 | 真实 DAO 验证 JOIN、分页、排序、过滤 |
| 场景测试 | 是 | 订单生成后用户/商家查询场景覆盖 |
| 并发测试 | 否 | 列表接口不改变状态 |
| 状态一致性测试 | 是 | 对比 HTTP 和 DB |

### `GET /api/v1/orders/{order_id}` 订单详情

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/order/router/order.go` | 鉴权路由 |
| Handler | `internal/app/order/handler/order.go` | `GetOrder` 路由参数 |
| DTO | `internal/app/order/dto/order.go` | `OrderDetail` |
| Service | `internal/app/order/service/service.go` | `GetOrder` 用户/商家归属校验 |
| DAO | `internal/app/order/dao/order.go` | `FindOrderDetail` JOIN 商品表 |
| Model | `internal/app/order/model/order.go` | `Order` |

#### 接口职责

返回当前登录身份有权访问的单个订单详情；不负责支付、取消、退款、发货或商品详情展开。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `order_id` | 是 | 路由参数；建议使用 `order_` 前缀本批次 ID | not found 或未授权 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `id` / `item_id` / `user_id` / `price` / `status` | 与 `orders` 表一致 | HTTP 响应 + DB |
| `item_title` | 与未软删除商品标题一致 | HTTP 响应 + DB |
| `item_merchant_id` | 不输出 JSON | HTTP 响应 |
| `expired_at` / `created_at` / `updated_at` | RFC3339 字符串 | HTTP 响应 |

#### 测试数据准备

- 当前用户订单、其他用户订单、当前商家商品订单、其他商家商品订单。
- 商品已软删除的订单。
- 不存在的订单 ID。

#### 成功路径

- 订单所属用户能查看详情。
- 商品所属商家能查看详情。
- 响应不包含 `item_merchant_id` 内部字段。

#### 失败路径

- 未登录或无效 token 返回未授权。
- 其他用户访问返回未授权。
- 非商品所属商家访问返回未授权。
- 不存在订单或商品已软删除导致 JOIN 不命中时返回 not found。

#### 状态和一致性验证

- HTTP 详情与 `orders` 和 `auction_items` 一致。
- 权限失败不泄露订单详情。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store 验证用户/商家归属 |
| 接口契约测试 | 是 | 验证鉴权、路由参数和响应字段 |
| 模块集成测试 | 是 | 验证详情 JOIN 和软删除过滤 |
| 场景测试 | 是 | 成交后用户/商家查看订单场景覆盖 |
| 并发测试 | 否 | 详情接口不改变状态 |
| 状态一致性测试 | 是 | 对比 HTTP 和 DB |

## 11. Service / DAO 测试契约

### `CreateOrder`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/order/service/service.go` | 创建 pending 订单和按 item 幂等 |
| Store 接口 | `internal/app/order/dao/order.go` | `FindOrderByItemID`、`CreateOrder` |
| DAO 实现 | `internal/app/order/dao/order.go` | `CreateOrder`、`FindOrderByItemID` |
| Model | `internal/app/order/model/order.go` | `Order` |

#### 测试数据准备

- fake store 中无订单、有同 `item_id` 订单、查询错误三种状态。
- 固定 `now` 和 `paymentTimeout`。

#### 单元测试点

- 首次创建生成 `order_` ID、`pending` 状态、正确 `item_id/user_id/price/expired_at`。
- 同一 `item_id` 重复创建返回已有订单，不创建第二条。
- `FindOrderByItemID` 非 not found 错误时直接返回错误。

#### 集成测试点

- 真实数据库创建订单字段完整。
- 并发同一 `item_id` 创建不会成功产生多条订单；若现存数据库历史数据已有重复 `item_id`，迁移唯一索引前必须先清理或补偿。

### `Pay` / `Cancel`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/order/service/service.go` | 支付、取消状态流转 |
| Store 接口 | `internal/app/order/dao/order.go` | `FindOrder`、`UpdateOrderStatus` |
| DAO 实现 | `internal/app/order/dao/order.go` | 状态条件更新 |
| Model | `internal/app/order/model/order.go` | 状态枚举 |

#### 测试数据准备

- pending 未过期订单、pending 已过期订单、paid 订单、cancelled 订单、其他用户订单。

#### 单元测试点

- 所属用户支付 pending 未过期订单成功。
- 重复支付已 paid 订单当前幂等成功。
- 已过期 pending 订单支付失败。
- 非所属用户支付或取消失败。
- pending 订单取消成功。
- paid 订单取消失败。

#### 集成测试点

- 真实数据库 `WHERE id = ? AND status = ?` 条件更新保证状态不被错误覆盖。
- 并发支付同一 pending 订单最终只有 paid 状态，无中间脏状态。

### `ScanExpiredOrders` / `ScanCompensation`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/order/service/cron.go` | 定时扫描逻辑 |
| Store 接口 | `internal/app/order/dao/order.go` | 过期订单和未建单成交商品扫描 |
| DAO 实现 | `internal/app/order/dao/order.go` | `ListExpiredPendingOrders`、`ListEndedItemsWithoutOrder` |
| 初始化 | `internal/app/order/init.go` | cron 注册 |

#### 测试数据准备

- 已过期 pending 订单、未过期 pending 订单、非 pending 订单。
- `ended` 且有 `winner_id`、无订单的商品。
- 已有订单的 ended 商品、无 winner 的 ended 商品、软删除商品。

#### 单元测试点

- 过期扫描只更新过期 pending 订单。
- 补偿扫描只为符合条件的 ended 商品创建订单。
- store 查询或更新失败时记录日志，不 panic。

#### 集成测试点

- 真实 SQL 只扫描符合条件的数据，limit 生效。
- 补偿创建订单与 `CreateOrder` 幂等规则一致。

## 12. 核心场景测试

### 场景 1：成交商品生成订单后，用户和商家都能查询

#### 业务价值

订单是竞拍成交后的用户可见结果，必须保证买家和卖家都能看到同一笔订单。

#### 关联接口 / 方法

- `CreateOrder`
- `GET /api/v1/orders`
- `GET /api/v1/orders/{order_id}`

#### 代码定位

- `internal/app/order/service/service.go`
- `internal/app/order/handler/order.go`
- `internal/app/order/dao/order.go`
- `internal/app/order/model/order.go`

#### 测试数据准备

- 本批次 ended 商品，`winner_id` 为测试用户，`merchant_id` 为测试商家，`deal_price` 为固定金额。
- 通过商品成交链路或订单 service 创建本批次订单。

#### Given

- 商品已成交且订单为 `pending`。

#### When

- 用户查询订单列表和详情。
- 商品所属商家查询订单列表和详情。

#### Then

- 用户和商家看到同一订单 ID、商品 ID、成交价和状态。
- 其他用户和其他商家不能看到该订单。

#### 证据要求

- HTTP 响应。
- `orders` 表记录。
- `auction_items` 表记录。

### 场景 2：订单支付、重复支付和取消互斥

#### 业务价值

订单状态一旦支付成功，不能被取消或重复错误改写。

#### 关联接口 / 方法

- `Pay`
- `Cancel`
- `GET /api/v1/orders/{order_id}`

#### 代码定位

- `internal/app/order/service/service.go`
- `internal/app/order/dao/order.go`
- `docs/agent-testing/modules/payment.md`

#### 测试数据准备

- pending 未过期订单。
- 订单所属用户 token。

#### Given

- 订单状态为 `pending` 且未过期。

#### When

- 所属用户支付订单。
- 再次支付同一订单。
- 再尝试取消同一订单。

#### Then

- 第一次支付成功，订单状态为 `paid`。
- 第二次支付当前幂等成功。
- 支付后取消失败，订单仍为 `paid`。

#### 证据要求

- Service 或 HTTP 响应。
- `orders.status`。
- 订单详情响应。

### 场景 3：过期订单扫描

#### 业务价值

超时未支付订单必须自动变为 `expired`，避免长期占用成交结果。

#### 关联接口 / 方法

- `ScanExpiredOrders`
- `GET /api/v1/orders/{order_id}`

#### 代码定位

- `internal/app/order/service/cron.go`
- `internal/app/order/dao/order.go`

#### 测试数据准备

- 一条已过期 pending 订单。
- 一条未过期 pending 订单。

#### Given

- 两条订单分别处于已过期和未过期状态。

#### When

- 执行 `ScanExpiredOrders`。

#### Then

- 已过期 pending 订单变为 `expired`。
- 未过期 pending 订单仍为 `pending`。

#### 证据要求

- 测试命令输出。
- `orders` 表状态。

## 13. 状态流转和一致性测试

| 当前状态 | 动作 | 目标状态 | 允许 | 涉及接口 / 方法 | 一致性证据 |
| --- | --- | --- | --- | --- | --- |
| 无订单 | 成交创建 | `pending` | 是 | `CreateOrder` | Service 返回 + `orders` |
| `pending` | 支付 | `paid` | 是 | `Pay` / payment 接口 | HTTP/Service + `orders` |
| `pending` | 取消 | `cancelled` | 是 | `Cancel` / payment 接口 | HTTP/Service + `orders` |
| `pending` | 过期扫描 | `expired` | 是 | `ScanExpiredOrders` | 测试输出 + `orders` |
| `paid` | 支付 | `paid` | 是（当前幂等） | `Pay` | HTTP/Service + `orders` |
| `paid` | 取消 | 无变化 | 否 | `Cancel` | 错误响应 + `orders` |
| `cancelled` | 支付 | 无变化 | 否 | `Pay` | 错误响应 + `orders` |
| `expired` | 支付 | 无变化 | 否 | `Pay` | 错误响应 + `orders` |

## 14. 并发测试

| 并发目标 | 是否需要 | 真实依赖 | 通过标准 |
| --- | --- | --- | --- |
| 同一订单并发支付 | 是 | 测试数据库 / HTTP | 最终状态为 `paid`；无错误覆盖；重复支付结果可解释 |
| 同一订单支付和取消并发 | 是 | 测试数据库 / HTTP | 最终只落入 `paid` 或 `cancelled` 之一；失败响应可解释 |
| 同一商品并发 `CreateOrder` | 是 | 测试数据库 / Service | 最多一条订单；若出现唯一索引冲突，记录返回错误和最终订单数 |
| 列表/详情并发查询 | 否 | 不适用 | 只读接口不单独做并发结论 |

## 15. WebSocket / Redis / 外部副作用测试

| 副作用 | 触发动作 | 验证方式 | 清理要求 |
| --- | --- | --- | --- |
| Cron 日志 | `ScanExpiredOrders` / `ScanCompensation` 查询或更新失败 | grep 日志关键字 `[order]` | 不写入敏感连接信息 |
| Redis | 不适用 | 当前订单模块不读写 Redis | 不适用 |
| WebSocket | 不适用 | 当前订单模块不发送消息 | 不适用 |
| 第三方支付 | 禁止 | 支付模块当前也不调用真实第三方 | 不适用 |

## 16. 回归测试

| 风险 | 回归测试位置 | 触发条件 | 证据 |
| --- | --- | --- | --- |
| 重复成交导致同一商品多订单 | 单元 / 集成 / 并发 | 多次或并发调用 `CreateOrder` | 订单数量和返回订单 ID |
| 其他用户查看或支付订单 | 单元 / 接口 | 使用非所属用户 token | 错误响应 + DB 未变化 |
| 商家看到其他商家的订单 | 单元 / 接口 | 使用非所属商家 token | 错误响应或列表为空 |
| 已过期订单仍可支付 | 单元 / 集成 | `ExpiredAt < now` 时调用 `Pay` | 错误响应 + `orders.status` |
| 软删除商品订单仍被查询 | 集成 | 商品 `deleted_at` 非空 | 查询不返回该订单 |
| 补偿扫描漏建订单 | 单元 / 集成 | ended、有 winner、无订单商品 | 新订单记录 |

## 17. 测试类型覆盖矩阵

| 测试对象 | 单元 | 接口契约 | 集成 | 场景 | 异常 | 边界 | 并发 | 状态一致性 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `GET /api/v1/orders` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `GET /api/v1/orders/{order_id}` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `CreateOrder` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `Pay` | 是 | 通过 payment | 是 | 是 | 是 | 是 | 是 | 是 |
| `Cancel` | 是 | 通过 payment | 是 | 是 | 是 | 是 | 是 | 是 |
| `ScanExpiredOrders` | 是 | 否 | 是 | 是 | 是 | 是 | 否 | 是 |
| `ScanCompensation` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| 订单身份隔离 | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |

## 18. 通过标准

**核心验证点（全部通过才算过）：**

- 订单创建、支付、取消、过期扫描的状态流转符合第 13 节矩阵，证据为 Service/HTTP 响应和 `orders` 表。
- 普通用户和商家订单查询只返回自己可见的数据，证据为 HTTP 响应和 DB 对照。
- `CreateOrder` 对同一 `item_id` 的幂等行为和数据库唯一约束符合当前实现，证据为返回 ID、唯一索引和订单数量。
- `ScanExpiredOrders` 只更新已过期 pending 订单，证据为扫描前后 DB 状态。
- `ScanCompensation` 只为 ended、有 winner、无订单商品创建订单，证据为 `auction_items` 和 `orders`。

**辅助验证点（建议验证，可附说明跳过）：**

- cron 错误日志格式包含 `[order]` 关键字。
- 订单详情时间字段为 RFC3339 字符串。
- 未知 `status` 查询行为被记录为当前实现。

## 19. 需用户确认的问题

- 订单过期后是否允许用户重新发起支付或重新生成订单？
- 商家查看订单详情时返回 `user_id` 是否符合隐私边界？
- 商品软删除后，历史订单是否应继续可查询？
- 订单支付成功后是否需要触发保证金抵扣、退款、通知、发货或审计流水？

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
