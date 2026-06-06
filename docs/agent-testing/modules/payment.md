# 支付模块测试说明

## 1. 模块目标

支付模块负责暴露订单支付和订单取消 HTTP 入口，并把操作委托给订单服务完成状态流转。当前实现没有独立支付模型、DAO、真实第三方支付调用或支付流水表；核心测试对象是支付接口的请求绑定、鉴权、订单服务调用、响应结构和订单状态一致性。

当前支付接口包括：

- `POST /api/v1/orders/{order_id}/pay`
- `POST /api/v1/orders/{order_id}/cancel`

## 2. 代码定位索引

| 对象 | 代码位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/payment/router/payment.go` | 注册鉴权支付/取消接口，支付接口绑定 JSON |
| Handler | `internal/app/payment/handler/payment.go` | 处理 binding error、调用订单服务、统一响应 |
| Request DTO | `internal/app/order/dto/order.go` | `PayOrderRequest`，`result` 必填且只能为 `success` |
| 依赖 Service | `internal/app/order/service/service.go` | `Pay`、`Cancel` 执行订单状态流转 |
| 依赖 DAO / Model | `internal/app/order/dao/order.go`、`internal/app/order/model/order.go` | 订单查询和状态条件更新 |
| 初始化 | `internal/app/payment/init.go` | 从 `orderapp.Svc` 注入订单服务，注册路由 |
| 单元测试建议位置 | `internal/app/payment/handler/*_test.go` 或 `internal/app/order/service/*_test.go` | handler 用 fake order service 时需先抽接口；订单规则继续放订单 service 测 |
| Agent 测试契约 | `docs/agent-testing/modules/payment.md` | 接口契约、场景和状态一致性测试边界 |

## 3. 测试边界

Agent 可以测试：

- HTTP 接口：`POST /api/v1/orders/{order_id}/pay`、`POST /api/v1/orders/{order_id}/cancel`。
- Handler 行为：JSON 绑定错误、鉴权上下文、`orderSvc == nil` 时内部错误、`response.OK(nil)`。
- 订单服务协作：`orderSvc.Pay`、`orderSvc.Cancel` 的返回错误和成功结果。
- 订单状态一致性：`orders.status` 从 `pending` 变为 `paid` 或 `cancelled`。

当前支付模块不测试真实第三方支付、支付渠道、支付回调、支付流水、签名验签、真实退款打款、保证金抵扣、发货或财务对账。当前成功订单支付会触发赢家保证金 `paid -> refunded`；当前订单取消会触发赢家保证金 `paid -> forfeited`。订单状态规则和保证金结算失败边界以 `order.md` 为准，保证金终态规则以 `deposit.md` 为准。

## 4. 禁止事项

- 不调用真实支付、退款、银行、短信或任何第三方服务。
- 不伪造真实支付平台回调或签名。
- 不直接清空 `orders`、`auction_items` 或其他业务表。
- 不修改生产配置或复用线上真实订单。
- 不把支付模块测试扩大为保证金、退款、发货、售后或财务对账流程。
- 不自行创造当前代码没有定义的支付状态；当前只有订单状态变化，没有支付单状态。
- 本地单元测试不允许直接连接数据库、Redis、HTTP 服务、WebSocket 或外部系统，必须使用 mock/fake 数据。
- Agent 连接线上或线上等价数据库时，只能操作本次测试创建的数据或带测试批次 ID 的数据。
- 不在测试报告中写入线上地址、凭据、密码、真实 token 或可复用密钥。

## 5. 测试依赖策略

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 优先测试订单 service；若测 payment handler，应抽象或注入 fake order service；禁止直连 MySQL、Redis、HTTP 服务或外部系统 | 当前支付模块主要是薄 handler，核心业务在订单服务 |
| Agent 接口契约测试 | 使用真实 handler 或本地服务；使用真实测试数据库；通过用户模块获取 token | 验证真实请求绑定、鉴权、响应结构和订单状态变化 |
| Agent 模块集成测试 | 使用真实 payment handler、真实 order service、真实 GORM store 和测试数据库 | 验证模块装配、状态条件更新和错误传播 |
| 场景测试 | 使用真实接口链路和真实测试数据库 | 验证用户可见支付/取消链路 |
| Agent 并发测试 | 使用真实测试数据库和真实 HTTP 并发请求 | 验证支付/取消互斥和重复支付结果 |
| 状态一致性测试 | 对比 HTTP 响应、`orders` 表和订单详情接口 | 验证外部可见结果与内部状态一致 |
| Redis / WebSocket 测试 | 支付模块内不适用 | 当前支付模块不直接读写 Redis，也不发送 WebSocket 消息 |

## 6. 全局测试数据准备

```text
测试批次 ID：agent_payment_<YYYYMMDDHHMMSS>
商品标题前缀：agent_payment_<batch>_
订单数据只允许关联本批次创建的 item、user 和 merchant。
测试结束后必须记录 orders 与 auction_items 的清理结果或软删除验证结果。
```

需要准备：

- 至少 2 个普通用户账号和 token，用于验证订单归属。
- 至少 1 个商家账号和 token，用于创建本批次商品。
- 至少 1 个无效 token。
- 本批次 pending 未过期订单。
- 本批次 pending 已过期订单。
- 本批次 paid、cancelled、expired 订单。
- 非法请求集合：无 JSON body、缺少 `result`、空 `result`、非 `success` 的 `result`、不存在的 `order_id`、他人订单、重复支付、支付后取消、取消后支付。

## 7. 业务规则

事实：

- 支付和取消接口都需要登录用户。
- 支付接口绑定 `PayOrderRequest`，字段 `result` 必填且只能为 `success`。
- 当前支付 handler 先处理绑定错误，再显式拒绝 `body.Result != "success"`；校验通过后不把具体 `result` 值传给订单服务。
- 支付接口调用 `orderSvc.Pay(current, order_id)`。
- 取消接口调用 `orderSvc.Cancel(current, order_id)`。
- `orderSvc == nil` 时两个接口都返回 internal error。
- 支付成功时返回 `response.OK(r, nil)`，订单状态由订单服务更新为 `paid`。
- 取消成功时返回 `response.OK(r, nil)`，订单状态由订单服务更新为 `cancelled`。
- 支付成功后触发赢家保证金从 `paid` 变为 `refunded`。
- 取消成功后触发赢家保证金从 `paid` 变为 `forfeited`。
- 支付或取消失败不能覆盖已有 `refunded` / `forfeited` 终态保证金。
- 订单服务要求只有订单所属用户能支付或取消。
- 订单服务拒绝已过期订单支付。
- 订单服务允许已 paid 订单重复支付幂等成功。
- 订单服务拒绝非 pending 订单取消。

根据当前代码结构推断：

- `result` 当前只表达“发起支付成功确认”的接口契约，非 `success` 会被 handler 显式拒绝。
- 当前支付模块是订单状态操作入口，不代表真实资金已到账。
- 商家身份如果不是订单 `user_id`，调用支付或取消会被订单服务拒绝。
- 保证金结算失败不回滚已经提交的订单支付或取消状态，但必须由订单模块记录日志或风险证据。

需确认内容集中在“需用户确认的问题”章节。

## 8. 业务不变量

- 非登录用户不能支付或取消订单。
- 非订单所属用户不能支付或取消订单。
- 支付接口不能接受非 `success` 的 `result`，也不能绕过订单归属、状态或过期校验。
- `pending` 未过期订单支付成功后必须变为 `paid`。
- `pending` 订单取消成功后必须变为 `cancelled`。
- 订单支付成功后赢家保证金必须变为 `refunded`。
- 订单取消成功后赢家保证金必须变为 `forfeited`。
- 失败支付或失败取消不得覆盖保证金 `refunded` / `forfeited` 终态。
- `paid`、`cancelled`、`expired` 订单不能被取消成其他状态。
- 已过期 pending 订单不能支付成 `paid`。
- 支付/取消失败时 `orders.status` 不能改变。
- HTTP 成功响应必须与订单详情和 `orders` 表状态一致。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 9. 字段规则索引

### PayOrderRequest

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `result` | request | 必填；声明 HTTP binding `oneof=success`，并由 handler 显式校验只能为 `success`；通过校验后不参与订单服务调用 | `POST /api/v1/orders/{order_id}/pay` | `PAYMENT.FIELD.result.*` |
| `order_id` | path/db | 路由参数；必须存在且属于当前用户才能支付/取消 | 支付、取消 | `PAYMENT.FIELD.order_id.*` |
| `current.ID` | auth | 订单服务用它校验 `orders.user_id` | 支付、取消 | `PAYMENT.FIELD.current_user.*` |

### Order 状态字段

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `status` | db/response | 支付：`pending -> paid`；取消：`pending -> cancelled`；其他状态按订单服务规则拒绝 | 支付、取消、订单详情 | `PAYMENT.FIELD.order_status.*` |
| `expired_at` | db/time | `now > expired_at` 时支付失败 | 支付 | `PAYMENT.FIELD.expired_at.*` |

### Deposit 状态字段

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `status` | db/response | 支付成功：赢家保证金 `paid -> refunded`；取消成功：赢家保证金 `paid -> forfeited`；失败路径不覆盖 `refunded` / `forfeited` | 支付、取消、订单详情、保证金查询 | `PAYMENT.FIELD.deposit_status.*` |
| `refunded_at` | db/response/time | 支付退款或取消罚没成功时写入终态结算时间；当前模型中 `refunded` 和 `forfeited` 都复用该字段 | 支付、取消、保证金查询 | `PAYMENT.FIELD.deposit_refunded_at.*` |

## 10. 接口测试契约

### `POST /api/v1/orders/{order_id}/pay` 支付订单

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/payment/router/payment.go` | 鉴权路由，绑定 JSON |
| Handler | `internal/app/payment/handler/payment.go` | `Pay`、`web.BindingErrors`、调用订单服务 |
| DTO | `internal/app/order/dto/order.go` | `PayOrderRequest` |
| Service | `internal/app/order/service/service.go` | `Pay` |
| DAO | `internal/app/order/dao/order.go` | `FindOrder`、`UpdateOrderStatus` |
| Model | `internal/app/order/model/order.go` | `Order` 状态 |

#### 接口职责

为当前用户拥有的 pending 未过期订单发起支付状态变更。当前接口不负责真实资金扣款、支付渠道结果校验、支付单创建或回调处理。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `order_id` | 是 | 路由参数；订单必须存在且属于当前用户 | not found 或未授权 |
| `result` | 是 | JSON 必填且只能为 `success` | 绑定参数错误 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data` | 成功时为 `null` 或统一响应中的空数据 | HTTP 响应 |
| `orders.status` | 成功后为 `paid` | DB + 订单详情接口 |
| `deposits.status` | 成功后赢家保证金为 `refunded` | DB + 保证金查询 |

#### 测试数据准备

- pending 未过期订单、pending 已过期订单、paid 订单、cancelled 订单、expired 订单。
- 订单所属用户 token、其他用户 token、无效 token。
- 合法请求体：`{"result":"success"}`。
- 非法请求体：空 body、`{}`、`{"result":""}`、`{"result":"failed"}`。

#### 成功路径

- 订单所属用户支付 pending 未过期订单成功。
- 已 paid 订单重复支付当前幂等成功。

#### 失败路径

- 未登录、无效 token 或非所属用户失败。
- 缺少 `result` 绑定失败。
- `result` 非 `success` 参数校验失败。
- 不存在订单返回 not found。
- 已过期 pending 订单支付失败。
- cancelled 或 expired 订单支付失败。
- 订单服务未初始化时返回 internal error。

#### 状态和一致性验证

- 成功响应后 `orders.status=paid`。
- 成功响应后赢家 `deposits.status=refunded`。
- 失败响应后 `orders.status` 不变。
- 失败响应后保证金终态不变。
- 订单详情接口返回状态与 DB 一致。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | 订单 service 单测覆盖核心规则；handler 绑定可单独补 |
| 接口契约测试 | 是 | 验证 JSON 绑定、鉴权、响应结构 |
| 模块集成测试 | 是 | 验证 payment -> order service -> DB |
| 场景测试 | 是 | 订单支付链路覆盖 |
| 并发测试 | 是 | 重复支付、支付取消并发 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、订单详情 |

### `POST /api/v1/orders/{order_id}/cancel` 取消订单

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/payment/router/payment.go` | 鉴权路由 |
| Handler | `internal/app/payment/handler/payment.go` | `Cancel` 调用订单服务 |
| Service | `internal/app/order/service/service.go` | `Cancel` |
| DAO | `internal/app/order/dao/order.go` | `FindOrder`、`UpdateOrderStatus` |
| Model | `internal/app/order/model/order.go` | `Order` 状态 |

#### 接口职责

取消当前用户拥有的 pending 订单。该接口不负责退款、恢复竞拍、发通知或删除订单。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `order_id` | 是 | 路由参数；订单必须存在且属于当前用户；状态必须 pending | not found、未授权或业务错误 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data` | 成功时为 `null` 或统一响应中的空数据 | HTTP 响应 |
| `orders.status` | 成功后为 `cancelled` | DB + 订单详情接口 |
| `deposits.status` | 成功后赢家保证金为 `forfeited` | DB + 保证金查询 |

#### 测试数据准备

- pending 订单、paid 订单、cancelled 订单、expired 订单。
- 订单所属用户 token、其他用户 token、无效 token。

#### 成功路径

- 订单所属用户取消 pending 订单成功。

#### 失败路径

- 未登录、无效 token 或非所属用户失败。
- 不存在订单返回 not found。
- paid、cancelled、expired 订单取消失败。
- 订单服务未初始化时返回 internal error。

#### 状态和一致性验证

- 成功响应后 `orders.status=cancelled`。
- 成功响应后赢家 `deposits.status=forfeited`。
- 失败响应后 `orders.status` 不变。
- 失败响应后保证金终态不变。
- 订单详情接口返回状态与 DB 一致。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | 订单 service 单测覆盖核心规则 |
| 接口契约测试 | 是 | 验证鉴权、响应结构 |
| 模块集成测试 | 是 | 验证 payment -> order service -> DB |
| 场景测试 | 是 | 用户取消订单链路覆盖 |
| 并发测试 | 是 | 支付取消并发 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、订单详情 |

## 11. Service / DAO 测试契约

支付模块当前没有自己的 service、dao 或 model。内部业务能力由订单服务提供，按 `docs/agent-testing/modules/order.md` 的 `Pay` / `Cancel` 契约测试。

### `payment.handler.Pay`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Handler | `internal/app/payment/handler/payment.go` | binding error、nil service、错误传播、OK 响应 |
| Request DTO | `internal/app/order/dto/order.go` | `PayOrderRequest` |
| Router | `internal/app/payment/router/payment.go` | JSON 绑定 |

#### 测试数据准备

- fake 或真实 `orderSvc`。
- 参数错误集合：缺少 body、缺少 `result`、`result` 非 `success`。

#### 单元测试点

- binding error 时不调用订单服务。
- `orderSvc == nil` 返回 internal error。
- 订单服务返回错误时透传到统一错误响应。
- 订单服务成功时返回 OK。

#### 集成测试点

- 真实路由、真实订单服务和测试数据库完成状态变更。

### `payment.handler.Cancel`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Handler | `internal/app/payment/handler/payment.go` | nil service、错误传播、OK 响应 |
| Router | `internal/app/payment/router/payment.go` | 鉴权路由 |

#### 测试数据准备

- fake 或真实 `orderSvc`。
- pending、paid、cancelled、expired 订单。

#### 单元测试点

- `orderSvc == nil` 返回 internal error。
- 订单服务返回错误时透传到统一错误响应。
- 订单服务成功时返回 OK。

#### 集成测试点

- 真实路由、真实订单服务和测试数据库完成状态变更。

## 12. 核心场景测试

### 场景 1：用户支付自己的未过期订单

#### 业务价值

成交订单必须能从待支付进入已支付状态。

#### 关联接口 / 方法

- `POST /api/v1/orders/{order_id}/pay`
- `GET /api/v1/orders/{order_id}`

#### 代码定位

- `internal/app/payment/router/payment.go`
- `internal/app/payment/handler/payment.go`
- `internal/app/order/service/service.go`
- `internal/app/order/dao/order.go`

#### 测试数据准备

- 本批次 pending 未过期订单。
- 订单所属用户 token。

#### Given

- 订单状态为 `pending`，`expired_at` 晚于当前时间。

#### When

- 用户提交 `{"result":"success"}` 到支付接口。

#### Then

- 支付接口返回成功。
- `orders.status` 为 `paid`。
- 赢家保证金状态为 `refunded`。
- 订单详情接口返回 `paid`。

#### 证据要求

- HTTP 支付响应。
- `orders` 表记录。
- `deposits` 表记录。
- 订单详情 HTTP 响应。

### 场景 2：用户取消自己的 pending 订单

#### 业务价值

用户应能取消尚未支付的订单，且取消后不能再支付。

#### 关联接口 / 方法

- `POST /api/v1/orders/{order_id}/cancel`
- `POST /api/v1/orders/{order_id}/pay`
- `GET /api/v1/orders/{order_id}`

#### 代码定位

- `internal/app/payment/handler/payment.go`
- `internal/app/order/service/service.go`

#### 测试数据准备

- 本批次 pending 订单。
- 订单所属用户 token。

#### Given

- 订单状态为 `pending`。

#### When

- 用户取消订单。
- 用户再尝试支付同一订单。

#### Then

- 取消成功，订单状态为 `cancelled`。
- 赢家保证金状态为 `forfeited`。
- 后续支付失败，订单保持 `cancelled`。
- 后续支付失败不能把保证金从 `forfeited` 覆盖为 `refunded`。

#### 证据要求

- HTTP 取消响应。
- HTTP 支付失败响应。
- `orders` 表状态。
- `deposits` 表状态。

### 场景 3：支付与取消权限隔离

#### 业务价值

订单资金动作必须只能由买家本人发起。

#### 关联接口 / 方法

- `POST /api/v1/orders/{order_id}/pay`
- `POST /api/v1/orders/{order_id}/cancel`

#### 代码定位

- `internal/app/payment/handler/payment.go`
- `internal/app/order/service/service.go`

#### 测试数据准备

- 用户 A 的 pending 订单。
- 用户 B token。
- 商家 token。

#### Given

- 订单属于用户 A。

#### When

- 用户 B 或商家尝试支付/取消该订单。

#### Then

- 接口返回未授权或等价错误。
- `orders.status` 不变。

#### 证据要求

- HTTP 响应。
- `orders` 表支付前后状态。

## 13. 状态流转和一致性测试

| 当前状态 | 动作 | 目标状态 | 允许 | 涉及接口 / 方法 | 一致性证据 |
| --- | --- | --- | --- | --- | --- |
| `pending` 未过期 | 支付 | `paid` + 保证金 `refunded` | 是 | `POST /orders/{order_id}/pay` | HTTP + DB + 详情 + 保证金 |
| `pending` 已过期 | 支付 | 无变化，保证金不变 | 否 | `POST /orders/{order_id}/pay` | 错误响应 + DB |
| `paid` | 支付 | `paid` | 是（当前幂等） | `POST /orders/{order_id}/pay` | HTTP + DB |
| `cancelled` | 支付 | 无变化，保证金终态不变 | 否 | `POST /orders/{order_id}/pay` | 错误响应 + DB |
| `expired` | 支付 | 无变化，保证金终态不变 | 否 | `POST /orders/{order_id}/pay` | 错误响应 + DB |
| `pending` | 取消 | `cancelled` + 保证金 `forfeited` | 是 | `POST /orders/{order_id}/cancel` | HTTP + DB + 详情 + 保证金 |
| `paid` | 取消 | 无变化，保证金终态不变 | 否 | `POST /orders/{order_id}/cancel` | 错误响应 + DB |
| `cancelled` | 取消 | 无变化，保证金终态不变 | 否 | `POST /orders/{order_id}/cancel` | 错误响应 + DB |
| `expired` | 取消 | 无变化，保证金终态不变 | 否 | `POST /orders/{order_id}/cancel` | 错误响应 + DB |

## 14. 并发测试

| 并发目标 | 是否需要 | 真实依赖 | 通过标准 |
| --- | --- | --- | --- |
| 同一订单并发支付 | 是 | 测试数据库 / HTTP | 最终状态为 `paid`；重复请求结果可解释 |
| 同一订单支付和取消并发 | 是 | 测试数据库 / HTTP | 最终状态只能是 `paid` 或 `cancelled`；不出现双成功矛盾证据 |
| 同一订单并发取消 | 是 | 测试数据库 / HTTP | 最终状态为 `cancelled`；至少一个请求成功，其余失败可解释 |
| 不同订单并发支付 | 否 | 不适用 | 可由订单模块批量场景覆盖 |

## 15. WebSocket / Redis / 外部副作用测试

| 副作用 | 触发动作 | 验证方式 | 清理要求 |
| --- | --- | --- | --- |
| Redis | 不适用 | 当前支付模块不读写 Redis | 不适用 |
| WebSocket | 不适用 | 当前支付模块不发送消息 | 不适用 |
| 真实支付渠道 | 禁止 | 当前实现不调用第三方服务 | 不适用 |
| 订单状态 | 支付 / 取消 | 查询 `orders` 和订单详情接口 | 清理本批次订单和商品 |

## 16. 回归测试

| 风险 | 回归测试位置 | 触发条件 | 证据 |
| --- | --- | --- | --- |
| 缺少 `result` 仍支付成功 | 接口 | `{}` 或空 body 请求支付 | 绑定错误 + DB 未变 |
| 非 `success` 仍支付成功 | 接口 | `{"result":"failed"}` 请求支付 | 绑定错误 + DB 未变 |
| 他人订单被支付或取消 | 接口 / 集成 | 使用非所属用户 token | 错误响应 + DB 未变 |
| 过期订单被支付 | 接口 / 集成 | `expired_at < now` 的 pending 订单 | 错误响应 + DB 未变 |
| 支付后仍可取消 | 接口 / 集成 | paid 订单调用取消 | 错误响应 + DB 未变 |
| 支付后保证金未退款 | 接口 / 集成 / 场景 | pending 未过期订单支付成功 | `orders.status=paid` + `deposits.status=refunded` |
| 取消后保证金未罚没 | 接口 / 集成 / 场景 | pending 订单取消成功 | `orders.status=cancelled` + `deposits.status=forfeited` |
| 失败支付/取消覆盖保证金终态 | 接口 / 集成 / 场景 | 对 terminal deposit 的订单重复操作 | DB 未变 |
| 支付取消并发产生矛盾状态 | 并发 | 同一订单同时支付和取消 | 最终单一状态 + 请求结果记录 |
| 订单服务未注入导致 panic | 单元 / 接口 | `orderSvc == nil` | internal error 响应 |

## 17. 测试类型覆盖矩阵

| 测试对象 | 单元 | 接口契约 | 集成 | 场景 | 异常 | 边界 | 并发 | 状态一致性 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v1/orders/{order_id}/pay` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `POST /api/v1/orders/{order_id}/cancel` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `PayOrderRequest.result` | 是 | 是 | 否 | 是 | 是 | 是 | 否 | 否 |
| `orderSvc == nil` | 是 | 是 | 否 | 否 | 是 | 是 | 否 | 否 |
| 支付/取消权限隔离 | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| 支付取消并发 | 否 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |

## 18. 通过标准

**核心验证点（全部通过才算过）：**

- 支付和取消接口的状态流转符合第 13 节矩阵，证据为 HTTP 响应、`orders` 表、订单详情接口和关联 `deposits` 表。
- 未登录、无效 token、非订单所属用户不能支付或取消订单，证据为错误响应和 DB 未变化。
- 支付接口缺少必填 `result` 或 `result` 非 `success` 时触发参数错误，证据为 HTTP 响应和 DB 未变化。
- 支付/取消失败时订单状态不被修改，保证金终态不被覆盖。
- 并发支付/取消后最终状态唯一且可解释。

**辅助验证点（建议验证，可附说明跳过）：**

- 成功响应 `data` 为空符合统一响应格式。
- 当前 `result=success` 通过绑定后不影响订单服务调用入参的事实被记录。
- 当前支付模块没有真实第三方调用被记录为测试边界。

## 19. 需用户确认的问题

- 支付模块是否计划新增支付单、支付流水、渠道单号或回调验签？
- 取消订单是否需要区分用户主动取消和超时取消？
- 订单支付成功后当前 P0 规则触发保证金退款；后续是否新增抵扣、通知或发货流程仍需确认。
- 商家是否应该有取消订单的能力，还是只能买家取消？

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
