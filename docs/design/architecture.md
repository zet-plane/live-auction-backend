# Live Auction Backend Architecture

## 1. 架构目标

本系统是面向直播电商竞拍场景的 Go 后端，核心目标是在保持实现复杂度可控的前提下，支撑高价值非标品的实时出价、排名、反狙击延时、成交与订单闭环。

当前架构选择是单体应用优先：

- 减少跨服务网络调用，降低出价、落槌、订单创建等关键链路的不确定性。
- 将业务边界拆在代码模块内，而不是过早拆成微服务。
- 使用 MySQL 保存权威业务数据，使用 Redis 承载竞拍中的高频实时状态。
- 通过 WebSocket 将竞拍事件推送到房间和用户维度，HTTP API 负责管理、查询和命令入口。
- 通过定时任务补偿非关键同步失败，避免把所有副作用都塞进高频请求路径。

## 2. 总体结构

```text
Client / Merchant Admin / Live Room UI
        |
        | HTTP API / WebSocket
        v
Flamego Server
        |
        | middleware: recovery, request log, renderer, CORS, auth, binding
        v
Business Modules
        |
        | service -> dao.Store / cache.Cache / other module service / broadcaster
        v
MySQL + Redis + Cron + WebSocket Hub
```

启动入口在 `cmd/server/server.go`：

1. 读取 `config.yaml` 并初始化日志。
2. 打开 MySQL 与 Redis。
3. 创建 `kernel.Engine`，携带 `Flame`、`DB`、`Cache`、`Config`、`Cron` 和根 `Context`。
4. 按 `internal/app/appInitialize` 中的顺序执行模块 `PreInit` 与 `Load`。
5. 启动 cron 与 HTTP server。
6. 收到退出信号后取消根 context，停止 cron，并调用模块 `Stop`。

## 3. 模块边界

业务模块放在 `internal/app/<module>/` 下，每个模块按固定结构组织：

```text
model/    GORM 数据模型和状态常量
dao/      Store 接口和 GORM 实现
dto/      HTTP 请求/响应对象与 service input
service/  业务逻辑，只依赖 Store 接口和必要的外部端口
handler/  Flamego handler，负责绑定错误、认证用户和响应转换
router/   路由注册
init.go   模块生命周期和依赖装配
```

模块生命周期由 `app.Module` 约定：

- `PreInit(engine)`：模块加载前执行，主要用于表结构迁移。
- `Load(engine)`：装配 store、service、handler、router 和定时任务。
- `Stop(wg, ctx)`：服务退出时做清理。

当前模块：

- `user`：注册、登录、用户资料和身份切换，提供 token 认证函数。
- `room`：直播间创建、查询、开始和结束。
- `item`：拍品、拍卖规则、出价、排行、竞拍状态流转。
- `deposit`：拍品保证金支付状态，供出价前校验。
- `order`：成交订单创建、查询、过期和补偿。
- `payment`：订单支付与取消入口，当前是内部模拟支付边界。
- `ws`：WebSocket ticket、连接管理、房间广播和用户单播。

模块间依赖保持在装配层和 service 端口上。典型例子是 `item` 模块在 `Load` 中注入 `order.Svc`、`deposit.Svc` 和 `ws.Hub`，业务代码通过 `DepositChecker` 与 `wsevent.Broadcaster` 这类接口表达依赖。

## 4. 分层约定

### Handler

Handler 是 HTTP 适配层：

- 接收 Flamego 注入的 request DTO、binding errors、当前用户和路径参数。
- 对 JSON binding 使用 `web.BindingErrors` 统一处理。
- 成功返回 `response.OK`，失败返回 `response.Error`。
- 不直接写业务规则，不直接操作 GORM 或 Redis。

### DTO

DTO 层隔离 HTTP 形态和 service input：

- request struct 持有 `json` 和 `binding` 标签。
- request 通过 `Input()` 转成 service 使用的 input。
- response DTO 将模型、实时状态和可展示字段组合成前端友好的结构。

### Service

Service 是业务规则所在层：

- 校验身份、状态机、金额、时间窗口和权限。
- 只依赖 `dao.Store` 接口，便于用 fake store 做单元测试。
- 对跨模块能力使用显式 service 或小接口注入。
- 对外返回 `pkg/errorx.CodeError`，交给 handler 转 HTTP 响应。

### DAO

DAO 层负责 MySQL 持久化：

- 每个模块定义自己的 `Store` 接口。
- GORM 实现隐藏在 `dao.GormStore` 中。
- 表结构迁移在模块 `PreInit` 阶段执行。

### Cache

Cache 层负责 Redis 中的实时态：

- `item/cache` 保存竞拍进行中的价格、领先者、倒计时、延时次数、排行和幂等键。
- `room/cache` 保存直播间实时状态。
- `ws/hub` 将在线用户和在线人数同步到 Redis。

## 5. 核心数据职责

### MySQL

MySQL 是权威持久化存储，保存：

- 用户、身份和登录资料。
- 直播间资料。
- 拍品 `AuctionItem` 与拍卖规则 `AuctionRule`。
- 出价日志 `BidLog`。
- 保证金记录。
- 成交订单。

所有最终状态，例如拍品是否结束、赢家、成交价、订单状态，都必须落到 MySQL。

### Redis

Redis 是竞拍进行中的高速状态层，保存：

- `auction:item:<item_id>:state`：当前价、领先者、结束时间、出价数、参与人数、延时信息。
- `auction:item:<item_id>:ranking`：用户最高出价排行。
- `auction:item:<item_id>:bidder_names`：排行展示所需用户名。
- `auction:item:<item_id>:idempotency:<key>`：出价幂等键。
- `auction:room:<room_id>:item_queue`：直播间拍品队列。
- `auction:room:<room_id>:state` 和 `online_users`：房间在线态。
- `ws:ticket:<ticket>`：WebSocket 一次性连接票据。

Redis 中的竞拍态服务于实时读写；MySQL 中的拍品、出价日志和订单是最终审计与恢复依据。

## 6. 竞拍状态机

拍品状态是线性流转：

```text
draft -> published -> ongoing -> ended
                  \-> cancelled
```

规则：

- 只有商家身份可以创建、编辑、发布、开始、取消自己的拍品。
- `draft` 可以编辑和删除。
- `published` 可以开始、取消或删除。
- `ongoing` 可以出价、取消或由定时任务结束。
- `ended` 表示已落槌，可能生成订单。
- `cancelled` 表示商家主动取消。

状态转换由 `item/service` 统一校验，非法状态返回 `errorx.ErrInvalidRequest`。

## 7. 出价链路

出价是系统最关键的写路径：

```text
POST /api/v1/items/{item_id}/bids
        |
        v
auth + binding
        |
        v
item.Service.PlaceBid
        |
        | 1. 读取 MySQL 中的拍品和规则
        | 2. 校验 ongoing 状态
        | 3. 如果需要保证金，调用 deposit 服务校验
        | 4. 调用 Redis Lua 原子更新竞拍态、排行、幂等键
        | 5. 写 BidLog 到 MySQL
        | 6. WebSocket 推送出价成功、被超越、自动延时等事件
        | 7. 如达到一口价，结束拍卖并创建订单
        v
PlaceBidResult
```

Redis Lua 脚本保证单次出价内的关键实时态原子更新：

- 幂等请求直接返回已有 bid id。
- 拒绝已结束竞拍。
- 拒绝低于当前价或不满足加价幅度的价格。
- 更新最高价、领先用户、排行和参与人数。
- 在反狙击窗口内自动延长结束时间。
- 达到 `price_cap` 时标记可立即结束。

当前实现会同步写入 `BidLog`。后续高并发优化可以把出价日志先写入 Redis Stream/List，再由 worker 批量落库，但最终一致性和补偿策略需要同时设计。

## 8. 实时事件链路

WebSocket 模块提供 `wsevent.Broadcaster`：

- `Fanout(room:<room_id>, event)`：向直播间内所有连接广播。
- `Unicast(user:<user_id>, event)`：向某个用户的所有连接单播。

连接流程：

1. 用户通过认证的 HTTP API 获取一次性 `ws-ticket`。
2. 前端用 ticket 连接 `/ws/v1/rooms/{room_id}`。
3. WS handler 消费 ticket，创建连接并注册到 Hub。
4. Hub 在内存中维护房间连接表和用户连接表。
5. 加入/离开房间时异步同步在线状态到 Redis。

主要事件：

- `auction_started`
- `bid_success`
- `user_outbid`
- `auction_extended`
- `auction_ended`
- `auction_cancelled`
- `order_created`

当前 Hub 是进程内广播器，适合单实例或粘性连接场景。多实例部署时，需要增加 Redis Pub/Sub、消息队列或网关层广播，以便跨实例投递房间事件。

## 9. 成交与订单链路

订单由两条路径创建：

1. 出价达到一口价，`PlaceBid` 立即结束拍卖并调用 `order.Service.CreateOrder`。
2. 定时任务 `EndExpiredAuctions` 扫描已过结束时间的 ongoing 拍品，落槌后创建订单。

`CreateOrder` 以 `item_id` 做幂等查询：如果订单已存在，直接返回已有订单。

订单模块还有两个补偿任务：

- 每 5 分钟扫描超时未支付订单，将 `pending` 更新为 `expired`。
- 每 10 分钟扫描已结束但没有订单的拍品，补偿创建订单。

支付模块目前是对订单 service 的 HTTP 适配：

- `POST /api/v1/orders/{order_id}/pay`
- `POST /api/v1/orders/{order_id}/cancel`

未来接入真实支付时，建议把第三方支付请求、回调验签和状态同步封装在 payment service 内，订单状态仍由 order service 统一维护。

## 10. 配置与运行时

主要配置项：

- `http`：监听地址。
- `database`：MySQL driver、DSN、连接池。
- `redis`：Redis 地址、密码和 DB。
- `auth`：token secret 和 TTL。
- `auction`：反狙击策略，包括触发窗口、单次延时、最大延时次数和最大累计延时。

配置启动时通过 viper 读取。`item` 模块加载时会将 `auction` 配置覆盖到默认 `AuctionPolicy`，未配置或为 0 的字段继续使用默认值。

## 11. 错误处理与响应

错误处理规则：

- Service 边界返回 `pkg/errorx.CodeError`。
- Handler 使用 `response.Error` 统一转成 HTTP 响应。
- 未识别错误按 500 处理。
- 参数绑定错误统一交给 `web.BindingErrors`。
- Handler 不直接调用底层失败响应，保持响应格式一致。

这种约定让业务错误码、HTTP 状态码和前端处理逻辑集中在一处维护。

## 12. 可用性与降级

当前系统的可用性策略偏轻量：

- MySQL 是权威数据源；Redis 异常时，出价等强实时写路径直接失败，查询类路径尽量降级到 MySQL。
- Redis 排行读取失败时，`GetRanking` 会回退到 MySQL 的出价日志排行。
- 拍卖结束、订单创建等非高频副作用有 cron 补偿。
- WebSocket 推送失败不阻断主业务写入。
- 进程退出时按信号优雅关闭 HTTP server、cron 和模块。

需要重点注意的是：竞拍中的实时状态主要在 Redis，如果 Redis 丢失 ongoing 状态，需要依赖 MySQL 拍品、规则和出价日志进行恢复。恢复命令或后台任务目前还没有专门实现。

## 13. 测试策略

本地单元测试以 service 层为主：

- 使用 fake store、fake cache、fake broadcaster。
- 不连接 MySQL、Redis、HTTP、WebSocket 或外部系统。
- 注入 fake time，避免时间相关测试不稳定。
- 覆盖状态机、权限、金额校验、反狙击延时、订单补偿和幂等行为。

接口、集成、端到端、并发和在线依赖测试应按 `docs/agent-testing/README.md` 的路由执行，只操作当前测试批次创建的数据，并记录证据与清理结果。

## 14. 演进方向

近期优先级：

- 为 Redis 竞拍态增加恢复工具：从 MySQL ongoing item 和 bid log 重建 state/ranking。
- 将高频 `BidLog` 同步落库改造成异步批量落库，并明确失败补偿。
- 为 WebSocket 多实例广播增加 Redis Pub/Sub 或消息总线。
- 补充竞拍链路的观测指标：出价 QPS、Lua 延迟、失败码分布、自动延时次数、落槌延迟、订单补偿次数。
- 为关键任务增加报警：Redis 不可用、cron 补偿持续失败、订单创建失败、WS 连接异常下降。

中长期方向：

- 继续保持核心竞拍链路在单体内闭环，避免把出价拆成多个强依赖远程调用。
- 将支付、风控、AI 氛围营造等非核心实时链路做成清晰边界，可独立演进。
- 当单实例 WebSocket 成为瓶颈时，优先扩展广播层和连接层，而不是拆散拍卖状态机。
- AI 模块可先从运营辅助切入，例如动态定价建议、标题/话术生成、竞拍热度摘要和异常竞价提示，避免直接介入强一致出价路径。
