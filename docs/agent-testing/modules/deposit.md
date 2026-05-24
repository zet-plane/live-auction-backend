# 保证金模块测试说明

## 1. 模块目标

保证金模块负责用户按拍品缴纳竞拍保证金、查询自己的保证金记录，并向商品出价服务提供 `HasPaidDeposit` 校验能力。核心实体是 `Deposit`，状态包括 `pending`、`paid`、`refunded`、`forfeited`。

当前实现中的“支付保证金”是站内状态写入，不调用真实第三方支付。拍品是否需要保证金由商品规则 `auction_rules.deposit_amount` 决定；出价时商品模块会在规则要求保证金时调用保证金服务确认用户是否已足额支付。

## 2. 代码定位索引

| 对象 | 代码位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/deposit/router/deposit.go` | 注册鉴权保证金接口 |
| Handler | `internal/app/deposit/handler/deposit.go` | 路由参数、鉴权上下文、统一响应 |
| DTO | `internal/app/deposit/dto/deposit.go` | `DepositDetail` 响应结构 |
| Service | `internal/app/deposit/service/service.go` | 支付保证金、查询保证金、出价前校验 |
| DAO | `internal/app/deposit/dao/deposit.go` | `Store` 接口、GORM 查询和唯一记录读写 |
| Model | `internal/app/deposit/model/deposit.go` | `Deposit` GORM 模型和状态常量 |
| 初始化 | `internal/app/deposit/init.go` | AutoMigrate、导出 `deposit.Svc`、注册路由 |
| 调用方 | `internal/app/item/service/bid_service.go` | 出价前调用 `HasPaidDeposit` |
| 单元测试建议位置 | `internal/app/deposit/service/*_test.go` | 使用 fake store、固定时间、固定用户 |
| Agent 测试契约 | `docs/agent-testing/modules/deposit.md` | 接口契约、集成、场景和一致性测试边界 |

## 3. 测试边界

Agent 可以测试：

- HTTP 接口：`POST /api/v1/items/{item_id}/deposit/pay`、`GET /api/v1/items/{item_id}/deposit`。
- Service 方法：`PayDeposit`、`GetMyDeposit`、`HasPaidDeposit`。
- DAO / Model：`deposits` 表、`idx_deposit_item_user` 唯一索引、`auction_rules.deposit_amount` 查询。
- 与保证金直接相关的商品规则：`auction_items.rule_id`、`auction_rules.deposit_amount`、商品软删除过滤。
- 与出价前置条件的协作：商品模块在 `DepositAmount > 0` 时调用 `HasPaidDeposit`。

当前保证金模块不测试真实支付、退款打款、罚没结算、财务流水、WebSocket 推送或订单支付。完整“缴纳保证金后出价”应与商品/出价模块或流程文档联动。

## 4. 禁止事项

- 不调用真实支付、退款、短信或任何第三方服务。
- 不直接清空 `deposits`、`auction_items`、`auction_rules` 或其他业务表。
- 不修改生产配置或复用线上真实保证金记录。
- 不绕过业务接口改写非本批次保证金状态，除非当前测试明确是状态故障注入。
- 不自行创造当前代码没有定义的保证金状态。
- 不把保证金模块测试扩大为完整订单支付、退款、售后或财务对账流程。
- 本地单元测试不允许直接连接数据库、Redis、HTTP 服务、WebSocket 或外部系统，必须使用 mock/fake 数据。
- Agent 连接线上或线上等价数据库时，只能操作本次测试创建的数据或带测试批次 ID 的数据。
- 不在测试报告中写入线上地址、凭据、密码、真实 token 或可复用密钥。

## 5. 测试依赖策略

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 使用 fake store、固定时间和固定用户身份；禁止直连 MySQL、Redis、HTTP 服务或 WebSocket | 稳定验证金额读取、幂等、状态分支、出价前校验和错误传播 |
| Agent 接口契约测试 | 使用真实 handler 或本地服务；使用真实测试数据库；通过用户模块获取 token | 验证鉴权、路由参数、响应结构和错误返回 |
| Agent 模块集成测试 | 使用真实 GORM store 和真实测试数据库 | 验证商品规则 JOIN、唯一索引、记录创建和更新 |
| 场景测试 | 使用真实接口链路和真实测试数据库；出价联动场景可调用商品模块 | 验证缴纳保证金后用户可出价的业务链路 |
| Agent 并发测试 | 使用真实测试数据库和真实 HTTP 并发请求 | mock/fake 无法证明唯一索引和重复支付真实结果 |
| 状态一致性测试 | 对比 HTTP 响应、`deposits` 表、`auction_items` / `auction_rules` | 验证接口返回、规则金额和持久化状态一致 |
| Redis / WebSocket 测试 | 保证金模块内不适用 | 当前保证金模块不直接读写 Redis，也不发送 WebSocket 消息 |

## 6. 全局测试数据准备

```text
测试批次 ID：agent_deposit_<YYYYMMDDHHMMSS>
商品标题前缀：agent_deposit_<batch>_
保证金记录只允许关联本批次创建的 item、rule 和 user。
测试结束后必须记录 deposits、auction_items、auction_rules 的清理结果或软删除验证结果。
```

需要准备：

- 至少 2 个普通用户账号和 token，用于验证同一拍品不同用户的隔离。
- 至少 1 个商家账号和 token，用于创建本批次商品及规则。
- 至少 1 个无效 token。
- 至少 1 个 `deposit_amount > 0` 的本批次商品。
- 至少 1 个 `deposit_amount <= 0` 或无保证金要求的本批次商品，用于验证拒绝支付或跳过校验。
- 至少 1 个商品被软删除的记录，用于验证 `FindRequiredAmount` not found。
- 非法输入集合：空 `item_id`、不存在的 `item_id`、无鉴权、他人保证金查询。
- 如需测试 refunded/forfeited 分支，可仅创建本批次记录并记录为故障注入，不要改写真实数据。

## 7. 业务规则

事实：

- 保证金接口都需要登录用户。
- `PayDeposit` 会 trim `item_id`，空值返回 invalid request。
- `PayDeposit` 先通过 `FindRequiredAmount` 从 `auction_rules.deposit_amount` 获取拍品要求金额。
- `FindRequiredAmount` 只查询未软删除的商品；商品不存在或已软删除时返回 not found。
- 保证金要求金额 `amount <= 0` 时，`PayDeposit` 返回 invalid request，不创建保证金记录。
- 新建保证金 ID 使用 `deposit_` 前缀。
- 新建保证金状态为 `paid`，金额等于规则 `deposit_amount`，`paid_at` 为当前时间。
- 同一 `item_id + user_id` 在 `deposits` 表上有唯一索引 `idx_deposit_item_user`。
- 已存在且状态为 `paid`、金额足额的保证金，再次 `PayDeposit` 返回原记录，保持幂等。
- 已存在但未足额或非终态的保证金，再次 `PayDeposit` 会把金额更新为当前规则金额，状态更新为 `paid`，刷新 `paid_at`。
- 已存在且状态为 `refunded` 或 `forfeited` 的保证金，再次 `PayDeposit` 返回 invalid request。
- `GetMyDeposit` 只能按当前用户和 `item_id` 查询自己的记录。
- `HasPaidDeposit` 在 `requiredAmount <= 0` 时直接返回 true。
- `HasPaidDeposit` 对空 `itemID` 或空 `userID` 返回 false，不报错。
- `HasPaidDeposit` 找不到记录时返回 false，不报错。
- `HasPaidDeposit` 只有在状态为 `paid` 且金额大于等于要求金额时返回 true。
- 商品出价服务在 `rule.DepositAmount > 0` 时调用 `HasPaidDeposit`；未足额支付会返回 `deposit required`。

根据当前代码结构推断：

- 当前保证金支付是模拟成功，不区分支付渠道或支付失败。
- `pending` 状态当前没有 HTTP 创建入口，只能通过测试数据或未来支付流程产生。
- `refunded_at` 当前只在模型和 DTO 中存在，没有退款接口写入。
- 商家身份当前也能调用保证金接口，因为 handler 只要求登录，没有限制普通用户身份。

需确认内容集中在“需用户确认的问题”章节。

## 8. 业务不变量

- 同一用户对同一拍品最多只能有一条保证金记录。
- 保证金支付金额必须等于当前拍品规则要求金额，或至少不低于出价要求金额。
- 不需要保证金的拍品不能通过 `PayDeposit` 创建无意义保证金记录。
- `refunded` 或 `forfeited` 记录不能被再次支付覆盖为 `paid`。
- 用户不能查询其他用户的保证金记录。
- 商品不存在或已软删除时不能支付保证金。
- 出价需要保证金时，只有 `paid` 且足额的保证金能通过校验。
- HTTP 响应中的保证金状态、金额、时间必须与 `deposits` 表一致。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 9. 字段规则索引

### Deposit / DepositDetail

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `id` | db/response | 保证金 ID 使用 `deposit_` 前缀，主键长度 64 | 支付、查询 | `DEPOSIT.FIELD.id.*` |
| `item_id` | path/db/response | route 参数 trim；必须存在且未软删除；与 `auction_items.id` 对应 | 支付、查询、校验 | `DEPOSIT.FIELD.item_id.*` |
| `user_id` | auth/db/response | 来自当前登录用户；同一 `item_id` 唯一 | 支付、查询、校验 | `DEPOSIT.FIELD.user_id.*` |
| `amount` | db/response/rule | 来自 `auction_rules.deposit_amount`；支付时必须大于 0；校验时必须足额 | 支付、查询、校验 | `DEPOSIT.FIELD.amount.*` |
| `status` | db/response | 枚举：`pending`、`paid`、`refunded`、`forfeited`；支付写入 `paid` | 支付、查询、校验 | `DEPOSIT.FIELD.status.*` |
| `paid_at` | db/response/time | 支付成功时写入当前时间；幂等返回原 paid 记录时不刷新 | 支付、查询 | `DEPOSIT.FIELD.paid_at.*` |
| `refunded_at` | db/response/time | 当前无写入接口；仅随已有记录返回 | 查询 | `DEPOSIT.FIELD.refunded_at.*` |
| `created_at` / `updated_at` | db/response | GORM 自动维护 | 支付、查询 | `DEPOSIT.FIELD.timestamps.*` |

### AuctionRule.deposit_amount

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `deposit_amount` | `auction_rules` | `PayDeposit` 要求大于 0；`HasPaidDeposit` requiredAmount 小于等于 0 直接通过 | 支付、出价前校验 | `DEPOSIT.FIELD.deposit_amount.*` |

## 10. 接口测试契约

### `POST /api/v1/items/{item_id}/deposit/pay` 支付保证金

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/deposit/router/deposit.go` | 鉴权路由 |
| Handler | `internal/app/deposit/handler/deposit.go` | `PayDeposit` 路由参数 |
| DTO | `internal/app/deposit/dto/deposit.go` | `DepositDetail` |
| Service | `internal/app/deposit/service/service.go` | `PayDeposit` 规则金额、幂等和状态分支 |
| DAO | `internal/app/deposit/dao/deposit.go` | `FindRequiredAmount`、`FindDeposit`、`CreateDeposit`、`UpdateDeposit` |
| Model | `internal/app/deposit/model/deposit.go` | `Deposit` |

#### 接口职责

为当前用户按拍品规则金额创建或更新一条 paid 保证金记录；不负责真实支付扣款、退款、财务流水或出价动作。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | 路由参数；trim 后不能为空；商品必须存在且未软删除；规则保证金必须大于 0 | 参数错误或 not found |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `id` | `deposit_` 前缀；重复足额支付返回同一 ID | HTTP 响应 + DB |
| `amount` | 等于 `auction_rules.deposit_amount` | HTTP 响应 + DB + rule |
| `status` | 支付成功为 `paid` | HTTP 响应 + DB |
| `paid_at` | 新建或更新支付时非空 | HTTP 响应 + DB |

#### 测试数据准备

- 当前用户 token。
- `deposit_amount > 0` 的本批次商品。
- `deposit_amount <= 0` 的本批次商品。
- 已有 `paid` 足额、不足额、`pending`、`refunded`、`forfeited` 记录。
- 不存在或已软删除商品。

#### 成功路径

- 首次支付创建 `paid` 记录。
- 已 paid 且足额时重复支付返回原记录。
- 已 pending 或不足额记录再次支付更新为当前规则金额和 `paid`。

#### 失败路径

- 未登录或无效 token 返回未授权。
- 空 `item_id`、不存在商品或已软删除商品失败。
- 规则保证金金额小于等于 0 时失败，不创建记录。
- `refunded` 或 `forfeited` 记录再次支付失败，原记录不被覆盖。
- DAO 创建或更新失败返回错误响应，不能留下半成功状态。

#### 状态和一致性验证

- HTTP 响应与 `deposits` 表一致。
- 保证金金额与 `auction_rules.deposit_amount` 一致。
- 失败时 `deposits` 表无新增或不应变更。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store 验证金额、幂等、终态拒绝和错误传播 |
| 接口契约测试 | 是 | 真实 handler 验证鉴权、路由参数和响应结构 |
| 模块集成测试 | 是 | 真实 DAO 验证 JOIN、唯一索引和 DB 写入 |
| 场景测试 | 是 | 缴纳保证金后出价场景覆盖 |
| 并发测试 | 是 | 同一用户同一商品重复支付 |
| 状态一致性测试 | 是 | 对比 HTTP、DB、商品规则 |

### `GET /api/v1/items/{item_id}/deposit` 查询我的保证金

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/deposit/router/deposit.go` | 鉴权路由 |
| Handler | `internal/app/deposit/handler/deposit.go` | `GetMyDeposit` 路由参数 |
| DTO | `internal/app/deposit/dto/deposit.go` | `DepositDetail` |
| Service | `internal/app/deposit/service/service.go` | `GetMyDeposit` |
| DAO | `internal/app/deposit/dao/deposit.go` | `FindDeposit` |
| Model | `internal/app/deposit/model/deposit.go` | `Deposit` |

#### 接口职责

返回当前用户在指定拍品下的保证金记录；不返回其他用户记录，也不展开商品详情。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `item_id` | 是 | 路由参数；trim 后不能为空 | 参数错误或 not found |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `id` / `item_id` / `user_id` / `amount` / `status` | 与当前用户的 `deposits` 记录一致 | HTTP 响应 + DB |
| `paid_at` / `refunded_at` / timestamps | 与 DB 一致 | HTTP 响应 + DB |

#### 测试数据准备

- 当前用户保证金记录。
- 其他用户同一商品保证金记录。
- 当前用户无保证金记录。

#### 成功路径

- 当前用户查询自己的保证金成功。
- 同一商品存在其他用户保证金时不影响当前用户结果。

#### 失败路径

- 未登录或无效 token 返回未授权。
- 空 `item_id` 返回参数错误。
- 当前用户没有该商品保证金时返回 not found。

#### 状态和一致性验证

- HTTP 响应只对应当前 `user_id + item_id`。
- 不泄露其他用户保证金记录。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake store 验证归属查询和 not found |
| 接口契约测试 | 是 | 验证鉴权和响应字段 |
| 模块集成测试 | 是 | 验证唯一索引查询 |
| 场景测试 | 是 | 支付后查询场景覆盖 |
| 并发测试 | 否 | 查询接口不改变状态 |
| 状态一致性测试 | 是 | 对比 HTTP 和 DB |

## 11. Service / DAO 测试契约

### `HasPaidDeposit`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Service | `internal/app/deposit/service/service.go` | 出价前保证金校验 |
| Store 接口 | `internal/app/deposit/dao/deposit.go` | `FindDeposit` |
| 调用方 | `internal/app/item/service/bid_service.go` | `PlaceBid` 调用 |
| Model | `internal/app/deposit/model/deposit.go` | 状态和金额 |

#### 测试数据准备

- 无保证金、pending、paid 足额、paid 不足额、refunded、forfeited 记录。
- requiredAmount 为 0、负数、正数。

#### 单元测试点

- `requiredAmount <= 0` 返回 true。
- 空 itemID 或 userID 返回 false。
- 找不到记录返回 false。
- paid 足额返回 true。
- paid 不足额、pending、refunded、forfeited 返回 false。
- store 查询异常向上传播。

#### 集成测试点

- 商品出价接口在 `DepositAmount > 0` 且未支付时返回 `deposit required`。
- 足额支付后同一用户可以通过出价前置校验。

### `FindRequiredAmount`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| DAO 实现 | `internal/app/deposit/dao/deposit.go` | JOIN `auction_rules` 和 `auction_items` |
| Model 依赖 | `internal/app/item/model/item.go` | `AuctionItem`、`AuctionRule` |

#### 测试数据准备

- 正常商品和规则。
- 已软删除商品。
- 不存在商品。
- 规则保证金为 0 的商品。

#### 单元测试点

- 本方法主要做 DAO 集成测试；本地单元测试可在 service fake store 中模拟返回金额和错误。

#### 集成测试点

- 正常商品返回 `auction_rules.deposit_amount`。
- 不存在或软删除商品返回 not found。
- 数据库错误向上传播。

## 12. 核心场景测试

### 场景 1：用户缴纳保证金后可以通过出价前置校验

#### 业务价值

有保证金要求的拍品必须先缴纳保证金再出价，避免绕过准入门槛。

#### 关联接口 / 方法

- `POST /api/v1/items/{item_id}/deposit/pay`
- `HasPaidDeposit`
- 商品模块 `PlaceBid`

#### 代码定位

- `internal/app/deposit/service/service.go`
- `internal/app/deposit/dao/deposit.go`
- `internal/app/item/service/bid_service.go`

#### 测试数据准备

- `deposit_amount > 0` 的 ongoing 拍品。
- 用户 token。
- Redis 竞拍状态按商品/出价模块要求准备。

#### Given

- 拍品规则要求保证金，用户尚未缴纳。

#### When

- 用户先尝试出价。
- 用户缴纳保证金。
- 用户再次出价。

#### Then

- 第一次出价因 `deposit required` 失败。
- 保证金支付成功并生成 paid 记录。
- 第二次出价通过保证金前置校验。

#### 证据要求

- 保证金 HTTP 响应。
- `deposits` 表记录。
- 出价响应或 Service 返回。

### 场景 2：重复支付保证金保持同一条记录

#### 业务价值

重复点击支付不应产生多条保证金记录。

#### 关联接口 / 方法

- `POST /api/v1/items/{item_id}/deposit/pay`
- `GET /api/v1/items/{item_id}/deposit`

#### 代码定位

- `internal/app/deposit/service/service.go`
- `internal/app/deposit/dao/deposit.go`
- `internal/app/deposit/model/deposit.go`

#### 测试数据准备

- `deposit_amount > 0` 的商品。
- 同一用户 token。

#### Given

- 用户没有该商品保证金记录。

#### When

- 连续两次支付同一商品保证金。
- 查询我的保证金。

#### Then

- 两次支付返回同一 `deposit_id`。
- `deposits` 表只有一条 `item_id + user_id` 记录。
- 查询结果与支付结果一致。

#### 证据要求

- 两次 HTTP 响应。
- `deposits` 表数量和记录。

### 场景 3：退款或罚没后的记录不能再次支付覆盖

#### 业务价值

终态保证金不能被用户再次支付覆盖，否则会破坏退款/罚没结果。

#### 关联接口 / 方法

- `POST /api/v1/items/{item_id}/deposit/pay`

#### 代码定位

- `internal/app/deposit/service/service.go`
- `internal/app/deposit/model/deposit.go`

#### 测试数据准备

- 本批次 `refunded` 或 `forfeited` 保证金记录。

#### Given

- 用户已有终态保证金记录。

#### When

- 用户再次支付同一商品保证金。

#### Then

- 接口返回 invalid request。
- 原记录状态、金额、时间不被覆盖。

#### 证据要求

- HTTP 响应。
- 支付前后 `deposits` 表记录。

## 13. 状态流转和一致性测试

| 当前状态 | 动作 | 目标状态 | 允许 | 涉及接口 / 方法 | 一致性证据 |
| --- | --- | --- | --- | --- | --- |
| 无记录 | 支付保证金 | `paid` | 是 | `PayDeposit` | HTTP + `deposits` |
| `paid` 且足额 | 重复支付 | `paid` | 是（幂等） | `PayDeposit` | HTTP + `deposits` |
| `pending` | 支付保证金 | `paid` | 是 | `PayDeposit` | HTTP + `deposits` |
| `paid` 但不足额 | 支付保证金 | `paid` 足额 | 是 | `PayDeposit` | HTTP + `deposits` |
| `refunded` | 支付保证金 | 无变化 | 否 | `PayDeposit` | 错误响应 + `deposits` |
| `forfeited` | 支付保证金 | 无变化 | 否 | `PayDeposit` | 错误响应 + `deposits` |

## 14. 并发测试

| 并发目标 | 是否需要 | 真实依赖 | 通过标准 |
| --- | --- | --- | --- |
| 同一用户同一商品并发支付 | 是 | 测试数据库 / HTTP | 最终只有一条 `item_id + user_id` 记录；成功/失败响应可解释 |
| 不同用户同一商品并发支付 | 是 | 测试数据库 / HTTP | 每个用户各自最多一条记录，互不覆盖 |
| 支付保证金与出价并发 | 是 | 测试数据库 / 商品模块真实依赖 | 出价只能在可见 paid 记录后通过，失败响应可解释 |
| 查询我的保证金并发 | 否 | 不适用 | 只读接口不单独做并发结论 |

## 15. WebSocket / Redis / 外部副作用测试

| 副作用 | 触发动作 | 验证方式 | 清理要求 |
| --- | --- | --- | --- |
| Redis | 不适用 | 当前保证金模块不读写 Redis | 不适用 |
| WebSocket | 不适用 | 当前保证金模块不发送消息 | 不适用 |
| 真实支付服务 | 禁止 | 当前实现为站内状态写入 | 不适用 |
| 出价前置校验 | `HasPaidDeposit` | 通过商品出价接口或 Service 返回验证 | 清理本批次出价/保证金数据 |

## 16. 回归测试

| 风险 | 回归测试位置 | 触发条件 | 证据 |
| --- | --- | --- | --- |
| 无保证金也能出价 | 单元 / 场景 | `DepositAmount > 0` 且无 paid 记录 | 出价错误响应 |
| 重复支付产生多条记录 | 单元 / 集成 / 并发 | 同一用户同一商品重复支付 | `deposits` 记录数量 |
| refunded/forfeited 被覆盖为 paid | 单元 / 接口 | 终态记录再次支付 | 错误响应 + DB 未变 |
| 保证金金额与规则不一致 | 单元 / 集成 | 规则金额变化后支付 | 响应金额 + DB 金额 |
| 用户查询到他人保证金 | 单元 / 接口 | 同一商品不同用户记录 | HTTP 响应只含当前用户 |
| 软删除商品仍可缴纳保证金 | 集成 | 商品 `deleted_at` 非空 | not found + 无新增记录 |

## 17. 测试类型覆盖矩阵

| 测试对象 | 单元 | 接口契约 | 集成 | 场景 | 异常 | 边界 | 并发 | 状态一致性 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v1/items/{item_id}/deposit/pay` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GET /api/v1/items/{item_id}/deposit` | 是 | 是 | 是 | 是 | 是 | 是 | 否 | 是 |
| `HasPaidDeposit` | 是 | 通过商品接口 | 是 | 是 | 是 | 是 | 是 | 是 |
| `FindRequiredAmount` | 否 | 否 | 是 | 是 | 是 | 是 | 否 | 是 |
| 保证金状态终态 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| 出价前置校验 | 是 | 通过 item | 是 | 是 | 是 | 是 | 是 | 是 |

## 18. 通过标准

**核心验证点（全部通过才算过）：**

- 保证金支付、查询、出价前校验符合第 7 节业务规则，证据为 HTTP/Service 响应和 `deposits` 表。
- 同一用户同一商品只存在一条保证金记录，证据为唯一索引查询和 DB 记录数。
- 保证金金额与 `auction_rules.deposit_amount` 一致，证据为 DB 对照。
- `refunded` 和 `forfeited` 记录不会被再次支付覆盖，证据为支付前后 DB 对照。
- 未缴纳或不足额用户不能通过有保证金要求的出价前置校验。

**辅助验证点（建议验证，可附说明跳过）：**

- `paid_at`、`created_at`、`updated_at` 与支付动作时间一致。
- 商品软删除后 `FindRequiredAmount` 返回 not found。
- 当前“支付保证金”不调用第三方服务的事实被记录。

## 19. 需用户确认的问题

- 商家身份是否允许缴纳保证金，还是应限制为普通用户？
- `deposit_amount <= 0` 时 `PayDeposit` 返回 invalid request 是否符合产品预期，还是应返回“无需保证金”？
- 保证金支付是否需要真实支付渠道、支付单号或流水表？
- 退款和罚没的触发时机、接口和状态流转规则是什么？
- 拍品规则保证金金额变更后，已支付保证金是否需要补差价或继续有效？
- 保证金是否应在订单支付成功后自动退款或抵扣？

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
