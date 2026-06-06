# WebSocket 模块测试说明

## 1. 模块目标

WebSocket 模块负责直播间实时连接、短期一次性 ticket 鉴权、房间内消息广播、用户定向推送、心跳响应和在线人数 Redis 同步。

本模块只负责实时同步通道，不作为最终业务数据来源。页面刷新或断线重连后的最终状态恢复，应通过 HTTP 商品详情、排行榜、订单等查询接口完成。

## 2. 代码定位索引

| 对象 | 代码位置 | 说明 |
| --- | --- | --- |
| Module init | `internal/app/ws/init.go` | 创建 Hub、初始化 handler、注册路由，并暴露包级 `ws.Hub` broadcaster |
| Router | `internal/app/ws/router/router.go` | 注册 `POST /api/v1/ws-ticket` 和 `GET /ws/v1/rooms/{room_id}` |
| Ticket Handler | `internal/app/ws/handler/ticket.go` | 生成 ticket、写入 Redis `ws:ticket:{ticket}`、统一 HTTP 响应 |
| WebSocket Handler | `internal/app/ws/handler/ws.go` | 校验 ticket、Redis `GETDEL`、origin policy、解析 `stream` 参数、升级连接、创建 Conn |
| Hub | `internal/app/ws/hub/hub.go` | 连接索引、房间广播、用户单播、慢连接剔除、Redis 在线状态同步 |
| Conn | `internal/app/ws/hub/conn.go` | 读写循环、心跳、离开房间、读写 deadline |
| Stream routing | `internal/app/ws/hub/stream.go` | `all` / `control` / `market` 连接分流和事件类型分类 |
| Event interface | `pkg/wsevent/broadcaster.go` | `Event`、`Broadcaster`、`RoomTopic`、`UserAddr` |
| 事件生产方 | `internal/app/item/service/service.go`、`internal/app/item/service/bid_service.go` | 竞拍开始、出价、延时、结束、取消、订单创建事件 |
| 事件 DTO | `internal/app/item/dto/events.go` | 服务端事件类型和 payload 字段 |
| 单元测试建议位置 | `internal/app/ws/hub/*_test.go`、`pkg/wsevent/*_test.go`、`internal/app/ws/handler/*_test.go` | 使用 fake websocket、fake Redis 或 in-process test server |
| Agent 测试契约 | `docs/agent-testing/modules/ws.md` | 接口契约、WebSocket、Redis、并发和一致性测试边界 |

当前模块没有独立 DAO、GORM Model 或 HTTP JSON Request DTO。

## 3. 测试边界

Agent 可以测试：

- HTTP 接口：`POST /api/v1/ws-ticket`。
- WebSocket 握手：`GET /ws/v1/rooms/{room_id}?ticket=<ticket>`。
- Hub 方法：`Register`、`Remove`、`Fanout`、`Unicast`。
- Conn 行为：`ping -> pong`、`leave_room` 断开、非法 JSON 忽略、未知事件忽略。
- 连接分流：`stream=all|control|market`，以及旧值 `stream=user` 映射到 `control`。
- Redis key：`ws:ticket:{ticket}`、`auction:room:{room_id}:state`、`auction:room:{room_id}:online_users`。
- `pkg/wsevent` topic / addr 格式。
- 由 item / bid / order 链路触发的 WebSocket 服务端事件收发。
- 慢连接 send channel 满时的连接剔除。
- 同房间隔离、跨房间隔离、同用户多连接单播。

Agent 不应在本模块内完整测试：

- 用户注册、登录和 JWT 生成规则；这些属于 `modules/user.md`。
- 拍品创建、上架、开始、取消、结算的完整业务；这些属于 `modules/item.md` 和跨模块流程。
- 出价合法性、排行榜和一口价成交规则；这些属于 `modules/bid.md`。
- 订单创建、支付、履约；这些属于 `modules/order.md`、`modules/payment.md`。
- 大规模压测或生产容量评估；本模块文档只定义功能、并发正确性和小规模扇出验证。

## 4. 禁止事项

- 不允许清空 Redis 或删除非本批次 key。
- 不允许复用线上真实 ticket、真实用户 token 或真实直播间连接。
- 不允许在测试报告中写入线上地址、凭据、密码、真实 token、完整可复用 ticket 或完整 WebSocket query string。
- 不允许把 WebSocket 消息当作最终业务事实；必须与 HTTP / MySQL / Redis 最终状态交叉验证。
- 不允许绕过业务模块直接广播未定义业务事件，并把它记为业务流程通过。
- 不允许自行扩展客户端事件语义，例如把 `join_room` 写成已实现的房间切换。
- 本地单元测试不允许直连 MySQL、Redis、HTTP 服务、WebSocket 或外部系统，必须使用 mock/fake 或 in-process 构造。
- Agent 连接线上或线上等价 Redis / 数据库时，只能操作本次测试创建的数据或带测试批次 ID 的数据。
- 不调用真实第三方服务。

## 5. 测试依赖策略

| 测试类型 | 依赖策略 | 原因 |
| --- | --- | --- |
| 本地单元测试 | 使用 fake Hub、fake Redis、fake websocket 或 gorilla in-process test server；禁止直连 MySQL、Redis、HTTP 服务或外部系统 | 稳定验证 ticket、Hub 索引、消息收发和错误分支 |
| Agent 接口契约测试 | 使用真实本地服务、测试 Redis、通过用户模块获取测试 token | 验证真实鉴权、Redis TTL、`GETDEL` 一次性消费和 WebSocket upgrade |
| Agent 模块集成测试 | 使用真实 Hub、真实测试 Redis、真实 WebSocket 客户端 | 验证连接注册、Redis 在线状态同步、房间广播和用户单播 |
| 场景测试 | 使用真实接口链路、真实测试 Redis，并按 item / bid 文档准备可触发事件的拍品 | 验证用户可见实时消息和最终 HTTP 状态一致 |
| Agent 并发测试 | 使用真实测试 Redis、真实 WebSocket 客户端和并发连接 | fake 无法证明 ticket 一次性消费、多连接索引和扇出隔离 |
| 状态一致性测试 | 对比 WebSocket 消息、HTTP 查询、Redis room state、Redis online users 和日志证据 | 验证实时通道和最终状态来源不冲突 |

## 6. 全局测试数据准备

```text
测试批次 ID：agent_ws_<YYYYMMDDHHMMSS>
账号前缀：agent_ws_<batch>_
房间 ID 前缀：room_agent_ws_<batch>_
ticket Redis 前缀：ws:ticket:<generated>
数据只允许操作本批次创建的数据。
测试结束后必须记录 Redis key 清理结果、WebSocket 连接关闭结果和测试用户清理/软删除结果。
```

需要准备：

- 至少 1 个普通用户账号和有效 token，用于 ticket 获取和连接。
- 至少 1 个商家账号和 token，用于按 item / room 文档准备可触发业务事件的房间和拍品。
- 至少 2 个普通用户账号，用于验证同房间广播、被超越单播和多用户在线状态。
- 至少 1 个无效 token 或未登录请求，用于 `POST /api/v1/ws-ticket` 鉴权失败。
- 至少 1 个有效房间 ID。若只测试 WebSocket 模块握手，可使用本批次房间 ID；若测试业务事件，必须通过房间 / 商品模块创建真实测试房间和拍品。
- 非法握手集合：缺少 ticket、缺少 room_id、伪造 ticket、已消费 ticket、过期 ticket。
- 客户端消息集合：合法 `ping`、合法 `leave_room`、非法 JSON、未知 `type`。
- 需要验证或清理的 Redis key：
  - `ws:ticket:{ticket}`
  - `auction:room:{room_id}:state`
  - `auction:room:{room_id}:online_users`

## 7. 业务规则

事实：

- `POST /api/v1/ws-ticket` 位于 `/api/v1`，需要 JWT 鉴权。
- ticket 由 16 字节随机数转为 32 位 hex 字符串。
- ticket 写入 Redis key `ws:ticket:{ticket}`，value 为当前用户 ID，TTL 为 45 秒。
- ticket 响应通过统一响应格式返回 `data.ticket`。
- WebSocket 连接地址为 `GET /ws/v1/rooms/{room_id}?ticket=<ticket>`。
- WebSocket 连接可选 query 参数 `stream`：空值、非法值或 `all` 表示接收全部事件；`control` 只接收控制类事件；`market` 只接收行情类事件；兼容旧值 `user`，按 `control` 处理。
- 握手时缺少 ticket 或 room_id 返回 HTTP 400，响应文本为 `missing ticket or room_id`。
- 握手时 Redis `GETDEL ws:ticket:{ticket}` 失败返回 HTTP 401，响应文本为 `invalid or expired ticket`。
- ticket 在握手成功前通过 Redis `GETDEL` 原子读取并删除，因此只能使用一次。
- WebSocket upgrade 成功后连接绑定 `conn_id`、`user_id`、`room_id`，后续客户端消息不再携带 ticket。
- `CheckOrigin` 在模块加载时由 `web.NewOriginPolicy(engine.Config.Mode, engine.Config.Security.AllowedOrigins)` 配置；debug / 未配置策略和显式允许列表的行为应按 `internal/middleware/web` 的 origin policy 验证。
- `Hub.Register` 将连接加入 `rooms[roomID][connID]` 和 `users[userID]` 两个内存索引。
- 同一用户、同一房间、同一 stream 再次连接会替换旧连接；同一用户、同一房间、不同 stream 可并存。
- `Hub.Remove` 从房间索引和用户索引移除连接；房间无连接时删除房间索引。
- Hub 有 Redis client 时，注册连接会异步执行 `SADD auction:room:{room_id}:online_users user_id`，再用 `SCARD online_users` 回写 `auction:room:{room_id}:state.online_count`。
- Hub 有 Redis client 时，移除连接会异步执行 `SREM auction:room:{room_id}:online_users user_id`，再用 `SCARD online_users` 回写 `auction:room:{room_id}:state.online_count`。
- `Fanout(room:<room_id>, event)` 向该 room 内所有连接投递事件。
- `Unicast(user:<user_id>, event)` 向该 user 的所有连接投递事件。
- 事件类型 `time_sync`、`auction_snapshot`、`auction_started`、`auction_extended`、`auction_ended`、`auction_cancelled`、`user_outbid`、`order_created` 属于 control stream；其他事件默认属于 market stream，例如 `bid_success`。
- 连接 send channel 满时，Hub 会关闭并移除该慢连接。
- Conn 读循环收到 `ping` 后向当前连接发送 `{ "type": "pong", "payload": null }` 形状的事件。
- Conn 读循环收到 `leave_room` 后结束读循环并移除连接。
- Conn 读循环收到非法 JSON 或未知 `type` 时不主动关闭连接。
- Conn 读 deadline 为 60 秒；每次成功读取消息后刷新 read deadline。
- Conn 写 deadline 为 10 秒。
- `pkg/wsevent.RoomTopic(roomID)` 返回 `room:{roomID}`；`pkg/wsevent.UserAddr(userID)` 返回 `user:{userID}`。
- item / bid service 当前会通过 broadcaster 发出 `auction_started`、`bid_success`、`auction_extended`、`user_outbid`、`auction_ended`、`auction_cancelled`、`order_created`。

根据当前代码结构推断：

- WebSocket 模块不校验 `room_id` 是否存在；只要 ticket 有效，握手会绑定到 path 中的 room_id。
- `join_room` 文档中有设计样例，但当前 Conn 未处理该事件；连接所属房间来自 URL path。
- Redis 在线状态同步是异步软写入；写入失败不会影响连接建立、广播或单播。
- `online_count` 当前按 `SCARD online_users` 派生，口径是同房间在线唯一用户数；同一用户同一房间重复连接时新连接替换旧连接。
- WebSocket 事件发送失败或 broadcaster 返回错误不会回滚 item / bid / order 的业务状态。
- 当前实现没有对完整 query string 做日志脱敏的显式逻辑；测试只能确认模块自身没有主动记录 ticket。

需确认内容集中在“需用户确认的问题”章节。

## 8. 业务不变量

- ticket 必须短期有效，过期 ticket 不能建立连接。
- ticket 必须一次性使用，同一个 ticket 最多只能成功建立 1 条 WebSocket 连接。
- 未携带 ticket 或携带无效 ticket 时不能建立 WebSocket 连接。
- 建立连接后的 user_id 必须来自 Redis ticket value，不能由客户端消息覆盖。
- 房间广播不能泄漏到其他 room。
- 用户单播必须发送给该用户所有在线连接，不能发送给其他用户。
- control stream 连接不能收到 market-only 事件；market stream 连接不能收到 control-only 事件；all stream 必须保持向后兼容，接收全部事件。
- 慢连接被剔除后不能继续留在 Hub 索引中。
- `leave_room` 或连接断开后，Hub 内存索引必须移除该连接。
- WebSocket 实时消息不能替代最终状态查询；最终业务状态必须以 HTTP / MySQL / Redis 业务状态为准。
- 业务事件 payload 必须与触发该事件的业务状态一致。
- 测试报告不得泄露真实 token、ticket 或完整 query string。

### 断线重连恢复

前端断线、刷新页面或被慢连接剔除后，不依赖 WebSocket 历史消息回放恢复现场。推荐恢复顺序：

1. 调用房间详情 `GET /api/v1/rooms/{room_id}`，恢复直播间状态、当前拍品 ID、在线人数和待拍队列。
2. 若房间详情返回 `current_item_id`，调用商品详情 `GET /api/v1/items/{item_id}`，恢复竞拍状态、当前价、领先用户、剩余时间、出价次数、参与人数和规则。
3. 调用排行榜接口，恢复当前拍品的出价排名和领先态。
4. 若用户已登录且存在订单或保证金相关 UI，再调用订单、保证金等对应 HTTP 查询接口恢复个人状态。
5. 重新申请 WebSocket ticket 并连接 `GET /ws/v1/rooms/{room_id}?ticket=<ticket>`，连接成功后以 `auction_snapshot`、`time_sync` 和后续业务事件继续增量刷新。

WebSocket 侧只保证“连接后新事件”和“当前拍品快照”尽快送达；缺失的历史事件必须由 HTTP 查询补齐。测试时应把 HTTP 房间详情、商品详情、排行榜结果作为恢复后的最终事实来源，WebSocket 消息只作为实时增量证据。

不变量失败时，agent 除常规失败报告外，必须额外输出：

```text
违反的不变量：<不变量名称>
违反位置：<模块/接口/步骤编号>
期望状态：
实际状态：
```

## 9. 字段规则索引

### Ticket / Handshake

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `ticket` | response/query/Redis | 32 位 hex；Redis key 为 `ws:ticket:{ticket}`；TTL 45 秒；一次性消费；不可复用 | `POST /api/v1/ws-ticket`、`GET /ws/v1/rooms/{room_id}` | `WS.FIELD.ticket.*` |
| `user_id` | auth/Redis/Conn | ticket value；握手成功后绑定连接；后续客户端事件不能覆盖 | `IssueTicket`、`ServeWS`、`NewConn` | `WS.FIELD.user_id.*` |
| `room_id` | path/Conn/Redis topic | path 必填；连接绑定房间；广播 topic 使用 `room:{room_id}` | `GET /ws/v1/rooms/{room_id}`、`Fanout` | `WS.FIELD.room_id.*` |
| `conn_id` | server memory | `conn_` 前缀；每次连接生成；Hub 索引用于移除和投递 | `ServeWS`、`Register`、`Remove` | `WS.FIELD.conn_id.*` |
| `stream` | query/Conn | 可选；空值、非法值和 `all` 接收全部事件；`control` 接收控制事件；`market` 接收行情事件；`user` 兼容映射到 `control` | `ServeWS`、`ParseConnStream`、`Hub.deliver` | `WS.FIELD.stream.*` |

### WebSocket Message / Event

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `type` | client/server event | 客户端当前支持 `ping`、`leave_room`；服务端事件使用 item DTO 常量和 `pong` | `StartReadLoop`、`StartWriteLoop`、item service | `WS.FIELD.event_type.*` |
| `payload` | client/server event | `ping` / `leave_room` 可为空；业务事件必须符合对应 DTO | Conn、item / bid service | `WS.FIELD.payload.*` |
| `send` | Conn memory | buffer size 为 64；满时关闭慢连接 | `deliver`、`closeConn` | `WS.FIELD.send_buffer.*` |
| event stream | server event classification | control 事件：`time_sync`、`auction_snapshot`、`auction_started`、`auction_extended`、`auction_ended`、`auction_cancelled`、`user_outbid`、`order_created`；其他事件默认 market | `classifyEventStream`、`streamAccepts` | `WS.FIELD.event_stream.*` |

### Time Sync Event

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `type` | server event | 固定为 `time_sync` | `BroadcastTimeSync` | `WS.FIELD.time_sync.type` |
| `item_id` | Redis active auction item | 当前正在竞拍且仍在 `auction:ending` 中的 item ID | `BroadcastTimeSync`、`ListActiveAuctionEnds` | `WS.FIELD.time_sync.item_id` |
| `server_time_unix_ms` | service clock | 服务端当前 Unix 毫秒时间，客户端用于校准本机时钟漂移 | `BroadcastTimeSync` | `WS.FIELD.time_sync.server_time_unix_ms` |
| `end_time_unix_ms` | Redis auction state | 服务端认定的竞拍结束 Unix 毫秒时间；反狙击延时后下一次广播应反映新值 | `BroadcastTimeSync`、bid Lua | `WS.FIELD.time_sync.end_time_unix_ms` |
| `status` | Redis auction state | 只广播 `ongoing` 状态的 active auction | `BroadcastTimeSync` | `WS.FIELD.time_sync.status` |

### Redis Online State

| 字段 | 来源 | 规则 | 涉及接口 / 方法 | 测试点 ID |
| --- | --- | --- | --- | --- |
| `auction:room:{room_id}:state.online_count` | Redis HASH | Register / Remove 后回写为 `SCARD online_users` | `redisPresenceStore.JoinRoom`、`redisPresenceStore.LeaveRoom` | `WS.FIELD.online_count.*` |
| `auction:room:{room_id}:online_users` | Redis SET | Register 时 SADD user_id；Remove 时 SREM user_id；按同房间唯一用户集合存储 | `redisPresenceStore.JoinRoom`、`redisPresenceStore.LeaveRoom` | `WS.FIELD.online_users.*` |

## 10. 接口测试契约

### `POST /api/v1/ws-ticket` 获取 WebSocket ticket

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/ws/router/router.go` | `/api/v1` 鉴权组内 POST `/ws-ticket` |
| Handler | `internal/app/ws/handler/ticket.go` | `IssueTicket` |
| Auth | `internal/app/user/handler`、`internal/middleware/web` | `AuthenticateToken` 和 Authorization middleware |
| Redis | `internal/app/ws/handler/ticket.go` | `SET ws:ticket:{ticket} user_id EX 45s` |
| Response | `internal/middleware/response` | `response.OK` / `response.Error` |

#### 接口职责

为当前已登录用户签发短期一次性 WebSocket ticket。该接口不建立 WebSocket 连接，不校验 room_id，也不返回长期身份凭证。

#### 请求字段

无 JSON 请求体；身份来自 `Authorization: Bearer <token>`。

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `Authorization` | 是 | 必须是有效登录 token | 未授权错误响应 |

#### 响应字段

| 字段 | 规则 | 证据 |
| --- | --- | --- |
| `data.ticket` | 32 位 hex 字符串；对应 Redis key 存在；TTL 约 45 秒 | HTTP 响应 + Redis TTL |
| `code` / `message` | 成功为统一响应成功形状 | HTTP 响应 |

#### 测试数据准备

- 有效普通用户 token。
- 有效商家 token。
- 无效 token 和未登录请求。
- 测试 Redis 可查询 `ws:ticket:{ticket}` 和 TTL。

#### 成功路径

- 普通用户获取 ticket 成功，Redis 中 value 等于当前用户 ID，TTL 大于 0 且不超过 45 秒。
- 商家用户获取 ticket 成功，Redis 中 value 等于当前商家用户 ID。
- 连续获取两次 ticket 返回不同 ticket，并创建两个独立 Redis key。

#### 失败路径

- 未登录请求失败，不创建 ticket key。
- 无效 token 请求失败，不创建 ticket key。
- Redis 写入失败时返回内部错误，不返回可用 ticket。

#### 状态和一致性验证

- HTTP `data.ticket` 与 Redis key 后缀一致。
- Redis value 与 token 对应用户 ID 一致。
- 测试报告只记录 ticket 掩码，例如 `abcd****1234`。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake Redis / fake user 验证 ticket 格式、TTL、错误传播 |
| 接口契约测试 | 是 | 真实 handler 验证鉴权、统一响应和 Redis key |
| 模块集成测试 | 是 | 真实 Redis 验证 TTL 和 value |
| 场景测试 | 是 | 与 WebSocket 握手串联 |
| 并发测试 | 是 | 并发签发 ticket 应唯一 |
| 状态一致性测试 | 是 | 对比 HTTP 响应和 Redis |

### `GET /ws/v1/rooms/{room_id}?ticket=<ticket>` 建立房间 WebSocket

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Router | `internal/app/ws/router/router.go` | 注册 WebSocket path |
| Handler | `internal/app/ws/handler/ws.go` | `ServeWS`、`GETDEL`、upgrade、创建 Conn |
| Hub | `internal/app/ws/hub/hub.go` | `Register`、Redis 在线状态同步 |
| Conn | `internal/app/ws/hub/conn.go` | 启动读写 goroutine |
| Redis | `internal/app/ws/handler/ws.go`、`internal/app/ws/hub/hub.go` | ticket 消费和在线状态 |

#### 接口职责

使用短期一次性 ticket 建立指定房间 WebSocket 连接，并把连接注册到 Hub。该接口不负责签发 ticket，不负责校验房间是否存在，不负责返回 HTTP JSON。

#### 请求字段

| 字段 | 必填 | 规则 | 失败表现 |
| --- | --- | --- | --- |
| `room_id` | 是 | path 参数；当前代码只校验非空 | HTTP 400 或连接到指定 room |
| `ticket` | 是 | query 参数；Redis `GETDEL` 成功后才允许 upgrade | HTTP 400 / 401 |
| `stream` | 否 | `all`、`control`、`market` 或兼容旧值 `user`；空值和未知值按 `all` | 连接成功但事件接收范围不同 |

#### 响应字段

WebSocket upgrade 成功后无 HTTP JSON 响应。失败时返回 plain text：

| 场景 | HTTP 状态 | 响应文本 | 证据 |
| --- | --- | --- | --- |
| 缺少 ticket 或 room_id | 400 | `missing ticket or room_id` | HTTP 响应 |
| ticket 无效、过期或已消费 | 401 | `invalid or expired ticket` | HTTP 响应 |
| upgrade 成功 | 101 | WebSocket 连接建立 | WebSocket client |

#### 测试数据准备

- 已通过 `POST /api/v1/ws-ticket` 获取的有效 ticket。
- 伪造 ticket、已消费 ticket、过期 ticket。
- 本批次 room_id。
- 可查询 Redis 的测试环境。

#### 成功路径

- 有效 ticket 首次握手成功，Redis `ws:ticket:{ticket}` 被删除。
- `stream=control` 连接只接收 control 事件；`stream=market` 连接只接收 market 事件；未传 stream 或 `stream=all` 接收全部事件。
- `stream=user` 按 control stream 处理，未知 stream 按 all stream 处理。
- 握手成功后发送 `ping`，收到 `pong`。
- 握手成功后 Hub 注册连接，房间广播可投递到该连接。
- 握手成功后 Redis online state 被异步更新。

#### 失败路径

- 缺少 ticket 返回 400。
- 伪造 ticket 返回 401。
- 同一 ticket 第二次握手返回 401。
- 过期 ticket 返回 401。
- Redis 不可用时握手失败，不能建立连接。

#### 状态和一致性验证

- ticket 消费后 Redis key 不存在。
- Hub room / user 索引包含连接；断开后索引移除。
- Redis `online_users` 以 `user_id` 为成员，`online_count` 等于 `SCARD online_users`。
- 跨房间连接不能收到本房间广播。
- stream 分流结果与 `internal/app/ws/hub/stream.go` 的分类一致。

#### 适用测试方法

| 测试类型 | 是否适用 | 说明 |
| --- | --- | --- |
| 本地单元测试 | 是 | fake Redis 和 in-process websocket 验证握手分支 |
| 接口契约测试 | 是 | 真实本地服务验证 HTTP 400/401/101 |
| 模块集成测试 | 是 | 真实 Redis 验证 GETDEL 和在线状态 |
| 场景测试 | 是 | 与 ticket、ping、业务广播串联 |
| 并发测试 | 是 | 同一 ticket 并发握手只允许 1 个成功 |
| 状态一致性测试 | 是 | 对比 WebSocket、Redis、Hub 可观测结果 |

## 11. Service / DAO 测试契约

本模块没有 Service / DAO 层。以下内部对象按 Service / DAO 测试契约处理。

### `Hub.Register` / `Hub.Remove`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Hub | `internal/app/ws/hub/hub.go` | 内存索引和 Redis 在线状态同步 |
| Conn | `internal/app/ws/hub/conn.go` | 连接字段 |
| Redis | `internal/app/ws/hub/hub.go` | room state 和 online users |

#### 测试数据准备

- fake Conn：不同 user、不同 room、同一 user 多连接。
- fake 或真实测试 Redis：`auction:room:{room_id}:state`、`auction:room:{room_id}:online_users`。

#### 单元测试点

- Register 后 rooms 和 users 索引都包含连接。
- Remove 后 rooms 和 users 索引都移除连接；房间空时删除房间索引。
- 同一用户、同一房间、同一 stream 重复连接时，新连接替换旧连接；同一用户、同一房间、不同 stream 可并存；同一用户不同房间连接互不影响。
- Redis 为 nil 时 Register / Remove 不 panic。
- `closeConn` 重复执行时只触发一次有效 Remove，不会重复扣减在线状态。
- `StartWriteLoop` 定时发送 WebSocket control `ping`；control `pong` 会刷新 read deadline。

#### 集成测试点

- Register 后 Redis Set 包含 user_id，HASH `online_count` 回写为 `SCARD online_users`。
- Remove 后 Redis Set 移除 user_id，HASH `online_count` 回写为 `SCARD online_users`。
- 若 Redis `online_count` 曾漂移，下一次 Register / Remove 后应按 `SCARD` 自动收敛。
- 异步 Redis 写入需要轮询等待，并设置超时。

### `Hub.Fanout` / `Hub.Unicast`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Hub | `internal/app/ws/hub/hub.go` | topic / addr 解析和 deliver |
| Event | `pkg/wsevent/broadcaster.go` | `Event`、`RoomTopic`、`UserAddr` |
| Conn | `internal/app/ws/hub/conn.go` | send channel |

#### 测试数据准备

- 同一 room 的多个连接。
- 不同 room 的连接。
- 同一 user 的多个连接。
- send channel 已满的慢连接。

#### 单元测试点

- Fanout 只投递到目标 room。
- Unicast 投递到目标 user 的所有连接。
- control 事件只投递给 control/all 连接，market 事件只投递给 market/all 连接。
- send channel 满时连接被移除并关闭。
- 空 room / 空 user 调用不报错。

#### 集成测试点

- 多个真实 WebSocket 客户端连接同一 room 后，Fanout 的事件都能收到。
- 不同 room 客户端不会收到目标 room 事件。
- 同一 user 多端连接后，Unicast 的事件每端都收到。

### `Conn.StartReadLoop` / `Conn.StartWriteLoop`

#### 代码定位

| 层级 | 位置 | 说明 |
| --- | --- | --- |
| Conn | `internal/app/ws/hub/conn.go` | 客户端消息处理和服务端事件写出 |
| WebSocket | gorilla websocket | ReadMessage / WriteJSON |

#### 测试数据准备

- in-process WebSocket server。
- 已连接客户端。
- 合法和非法客户端消息。

#### 单元测试点

- 发送 `{"type":"ping","payload":{}}` 后收到 `pong`。
- 发送 `{"type":"leave_room","payload":{}}` 后连接关闭并从 Hub 移除。
- 发送非法 JSON 不关闭连接。
- 发送未知 `type` 不关闭连接且不产生业务事件。

#### 集成测试点

- Hub Fanout 后客户端收到 JSON 事件。
- 客户端主动断开后 Hub 移除连接，Redis online state 被更新。

## 12. 核心场景测试

### 场景 1：ticket 签发、一次性消费和心跳

#### 业务价值

验证 WebSocket 鉴权链路不会暴露长期 JWT，并且 ticket 不能被复用。

#### 关联接口 / 方法

- `POST /api/v1/ws-ticket`
- `GET /ws/v1/rooms/{room_id}?ticket=<ticket>`
- `Conn.StartReadLoop`

#### 代码定位

- `internal/app/ws/router/router.go`
- `internal/app/ws/handler/ticket.go`
- `internal/app/ws/handler/ws.go`
- `internal/app/ws/hub/conn.go`

#### 测试数据准备

- 有效用户 token。
- 本批次 room_id。
- 测试 Redis。

#### Given

- 用户已登录并拥有有效 token。
- Redis 可用。

#### When

1. 调用 `POST /api/v1/ws-ticket` 获取 ticket。
2. 使用 ticket 连接 `GET /ws/v1/rooms/{room_id}`。
3. 发送 `ping`。
4. 再次使用同一个 ticket 建立第二条连接。

#### Then

- 第 1 步返回统一成功响应和 ticket。
- 第 2 步 WebSocket upgrade 成功。
- 第 3 步收到 `pong`。
- 第 4 步返回 401。
- Redis `ws:ticket:{ticket}` 已被删除。

#### 证据要求

- HTTP ticket 响应，ticket 需脱敏。
- WebSocket 101 / pong 消息。
- 第二次握手 401 响应。
- Redis key 不存在证据。

### 场景 2：同房间广播和跨房间隔离

#### 业务价值

验证实时消息只在目标直播间内扇出，避免不同直播间互相污染。

#### 关联接口 / 方法

- `GET /ws/v1/rooms/{room_id}?ticket=<ticket>`
- `Hub.Fanout`
- `pkg/wsevent.RoomTopic`

#### 代码定位

- `internal/app/ws/hub/hub.go`
- `pkg/wsevent/broadcaster.go`

#### 测试数据准备

- room A 和 room B。
- 用户 A、用户 B 连接 room A。
- 用户 C 连接 room B。
- 测试事件：`{"type":"bid_success","payload":{"item_id":"item_agent_ws","price":1200}}`。

#### Given

- 三个客户端均已成功连接。

#### When

- 调用 broadcaster `Fanout(wsevent.RoomTopic(roomA), event)`。

#### Then

- room A 的两个客户端都收到事件。
- room B 的客户端收不到事件。
- Hub 不返回错误。

#### 证据要求

- 三个客户端的消息接收记录。
- Hub / 测试命令输出。

### 场景 3：用户被超越单播

#### 业务价值

验证只向被超越用户发送定向提醒，并覆盖同一用户多端在线。

#### 关联接口 / 方法

- `Hub.Unicast`
- `pkg/wsevent.UserAddr`
- bid service `user_outbid` 事件。

#### 代码定位

- `internal/app/ws/hub/hub.go`
- `internal/app/item/service/bid_service.go`
- `internal/app/item/dto/events.go`

#### 测试数据准备

- 用户 A 两条连接。
- 用户 B 一条连接。
- `user_outbid` 事件。

#### Given

- 用户 A 和用户 B 均已连接。

#### When

- 调用 broadcaster `Unicast(wsevent.UserAddr(userA), event)`，或通过出价流程让用户 B 超越用户 A。

#### Then

- 用户 A 的所有连接都收到 `user_outbid`。
- 用户 B 不收到该单播事件。
- 如果通过真实出价触发，HTTP 出价结果、Redis ranking 和 WebSocket payload 一致。

#### 证据要求

- WebSocket 消息。
- 出价 HTTP 响应和 Redis / DB 状态，若走真实出价链路。

### 场景 4：连接关闭和在线状态同步

#### 业务价值

验证连接生命周期能更新内存索引和 Redis 在线状态，供房间详情读取。

#### 关联接口 / 方法

- `GET /ws/v1/rooms/{room_id}?ticket=<ticket>`
- `Hub.Register`
- `Hub.Remove`
- `Conn.StartReadLoop`

#### 代码定位

- `internal/app/ws/hub/hub.go`
- `internal/app/ws/hub/conn.go`
- `internal/app/room/cache/cache.go`

#### 测试数据准备

- 1 个 room。
- 2 个用户连接。
- 测试 Redis 初始状态可清理。

#### Given

- Redis room state 中 `online_count` 初始为 0 或不存在。

#### When

1. 用户 A 连接房间。
2. 用户 B 连接房间。
3. 用户 A 发送 `leave_room` 或关闭连接。

#### Then

- Redis `online_users` 曾包含用户 A 和 B。
- Redis `online_count` 按 `SCARD online_users` 收敛。
- 用户 A 离开后不能再收到房间广播。
- 用户 B 仍能收到房间广播。

#### 证据要求

- WebSocket 连接和关闭记录。
- Redis `HGET online_count`、`SMEMBERS online_users`。
- 广播接收记录。

### 场景 5：竞拍业务事件实时推送

#### 业务价值

验证 WebSocket 模块能承载业务模块产生的真实事件，并与最终查询结果一致。

#### 关联接口 / 方法

- item 开始竞拍接口。
- bid 出价接口。
- item 取消 / 结算逻辑。
- `Hub.Fanout` / `Hub.Unicast`。

#### 代码定位

- `internal/app/item/service/service.go`
- `internal/app/item/service/bid_service.go`
- `internal/app/item/dto/events.go`
- `internal/app/ws/hub/hub.go`

#### 测试数据准备

- 按 `modules/item.md` 准备可开始竞拍的拍品。
- 按 `modules/bid.md` 准备两个可出价用户。
- 至少一个用户 WebSocket 连接到拍品所在 room。

#### Given

- WebSocket 客户端已连接拍品所在 room。
- 拍品和规则处于可测试状态。

#### When

1. 商家开始竞拍。
2. 用户 A 出价。
3. 用户 B 出价超越用户 A。
4. 触发自动延时或一口价成交，若规则允许。
5. 商家取消另一个测试拍品，若覆盖取消事件。

#### Then

- 客户端收到对应 `auction_started`、`time_sync`、`bid_success`、`user_outbid`、`auction_extended`、`auction_ended`、`auction_cancelled`、`order_created` 中适用的事件。
- 进行中的竞拍每秒广播 `time_sync`，payload 包含 `item_id`、`server_time_unix_ms`、`end_time_unix_ms`、`status`。
- 每条事件 payload 与 HTTP 响应、MySQL / Redis 最终状态一致。
- 未实现或未触发的事件必须标记为跳过原因，不能记为失败。

#### 证据要求

- WebSocket 消息序列。
- HTTP 响应。
- MySQL / Redis 状态。
- 跳过事件的原因。

## 13. 状态流转和一致性测试

WebSocket 模块自身没有持久化业务状态机，但存在连接生命周期状态。

| 当前状态 | 动作 | 目标状态 | 允许 | 涉及接口 / 方法 | 一致性证据 |
| --- | --- | --- | --- | --- | --- |
| 无 ticket | `POST /api/v1/ws-ticket` | ticket 已签发 | 是 | `IssueTicket` | HTTP + Redis key + TTL |
| ticket 已签发 | WebSocket 握手成功 | ticket 已消费 + 连接已注册 | 是 | `ServeWS`、`Register` | 101 + Redis key missing + Hub 索引 |
| ticket 已签发 | ticket 过期后握手 | 连接拒绝 | 是 | `ServeWS` | 401 + Redis key missing |
| ticket 已消费 | 重复握手 | 连接拒绝 | 是 | `ServeWS` | 401 |
| 连接已注册 | `ping` | 连接保持 + pong | 是 | `StartReadLoop` | WebSocket message |
| 连接已注册 | `leave_room` | 连接关闭 + 索引移除 | 是 | `StartReadLoop`、`Remove` | close + Hub / Redis |
| 连接已注册 | 网络断开 | 连接关闭 + 索引移除 | 是 | `StartReadLoop`、`Remove` | close + Hub / Redis |
| 连接已注册 | send channel 满 | 慢连接关闭 + 索引移除 | 是 | `deliver`、`closeConn` | Hub 索引 |

## 14. 并发测试

| 并发目标 | 是否需要 | 真实依赖 | 通过标准 |
| --- | --- | --- | --- |
| 同一 ticket 并发握手 | 是 | 测试 Redis + WebSocket 客户端 | 只有 1 个连接成功，其余返回 401 |
| 同一用户多连接 | 是 | WebSocket 客户端 + Hub | `Unicast` 能投递到该用户所有连接 |
| 多用户同房间广播 | 是 | WebSocket 客户端 + Hub | 每个目标连接收到一次事件，非目标房间不收到 |
| 连接断开与广播同时发生 | 是 | WebSocket 客户端 + Hub | 不 panic；已断开连接最终从索引移除 |
| 慢连接和高频 Fanout | 是 | Hub 或 WebSocket 客户端 | 慢连接被剔除，不阻塞其他连接 |
| Redis 在线状态并发增减 | 是 | 测试 Redis | online state 不出现负数；`online_count` 始终可解释为 `SCARD online_users` |

## 15. WebSocket / Redis / 外部副作用测试

| 副作用 | 触发动作 | 验证方式 | 清理要求 |
| --- | --- | --- | --- |
| `ws:ticket:{ticket}` | `POST /api/v1/ws-ticket` | 查询 Redis value 和 TTL；握手后确认 key 删除 | 删除本批次残留 ticket key |
| WebSocket `pong` | 客户端发送 `ping` | 客户端接收 JSON event | 关闭连接 |
| WebSocket 房间广播 | `Hub.Fanout` 或真实业务事件 | 目标房间客户端接收；其他房间不接收 | 关闭连接 |
| WebSocket 用户单播 | `Hub.Unicast` 或真实 `user_outbid` / `order_created` | 目标用户所有连接接收；其他用户不接收 | 关闭连接 |
| Redis `auction:room:{room_id}:state.online_count` | Register / Remove | `HGET online_count`，验证等于 `SCARD online_users` | 删除本批次 room state 或恢复初始值 |
| Redis `auction:room:{room_id}:online_users` | Register / Remove | `SMEMBERS` / `SISMEMBER` 验证 user_id | 删除本批次 online_users key |
| 日志敏感信息 | ticket 签发和握手 | 检查测试日志不包含完整 token、ticket、query string | 报告只写脱敏值 |

## 16. 回归测试

| 风险 | 回归测试位置 | 触发条件 | 证据 |
| --- | --- | --- | --- |
| ticket 可复用导致连接劫持 | 接口契约 / 并发 | 同一 ticket 重复或并发握手 | 仅一次 101，其余 401 |
| Redis ticket TTL 错误导致长期有效 | 接口契约 | 签发 ticket 后检查 TTL | TTL 大于 0 且不超过 45 秒 |
| 房间广播串房 | 单元 / 集成 / 场景 | 两个 room 同时连接并 Fanout | 非目标 room 无消息 |
| 单播误发给其他用户 | 单元 / 集成 / 场景 | 多用户连接后 Unicast | 只有目标 user 收到 |
| stream 分流失效 | 单元 / 集成 / 场景 | control / market / all 三类连接同时存在 | control 不收 market-only 事件，market 不收 control-only 事件，all 全收 |
| 慢连接阻塞广播 | 单元 / 并发 | send channel 满后 Fanout | 慢连接移除，其他连接可继续收消息 |
| 离开房间后仍收消息 | 集成 / 场景 | `leave_room` 后 Fanout | 离开连接无消息，Hub 索引已移除 |
| 在线人数和在线用户集合不一致 | 集成 / 并发 | 同一用户重复连接或异常断开 | 验证 `online_count = SCARD online_users` 并记录 Redis 证据 |
| WebSocket 事件与最终状态不一致 | 场景 / 状态一致性 | 出价、延时、成交、取消事件 | WebSocket payload 与 HTTP / Redis / MySQL 一致 |

## 17. 测试类型覆盖矩阵

| 测试对象 | 单元 | 接口契约 | 集成 | 场景 | 异常 | 边界 | 并发 | 状态一致性 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v1/ws-ticket` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `GET /ws/v1/rooms/{room_id}` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `Hub.Register` / `Hub.Remove` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `Hub.Fanout` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| `Hub.Unicast` | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| WebSocket stream 分流 | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| `Conn ping / leave_room` | 是 | 否 | 是 | 是 | 是 | 是 | 否 | 是 |
| Redis ticket key | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 是 |
| Redis online state | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| 业务事件 payload | 是 | 否 | 是 | 是 | 是 | 是 | 是 | 是 |
| 日志敏感信息 | 否 | 是 | 是 | 是 | 是 | 否 | 否 | 是 |

## 18. 通过标准

核心验证点（全部通过才算过）：

- `POST /api/v1/ws-ticket` 只允许已登录用户获取 ticket，响应结构符合统一格式。
- ticket Redis value 与当前用户 ID 一致，TTL 合理，握手后被原子删除。
- 同一 ticket 重复或并发使用时最多一次 WebSocket upgrade 成功。
- 无 ticket、无 room_id、无效 ticket、过期 ticket 均不能建立连接。
- `ping` 能收到 `pong`，`leave_room` 或断开后连接从 Hub 索引移除。
- 房间广播只到目标 room，用户单播只到目标 user。
- 慢连接不会阻塞其他连接，并会被 Hub 移除。
- Redis online state 的变化有证据，并按确认后的在线人数语义验证。
- 进行中竞拍的 `time_sync` payload 使用服务端时间和 Redis 结束时间，反狙击延时后 `end_time_unix_ms` 随之更新。
- 业务事件的 WebSocket payload 与 HTTP / Redis / MySQL 最终状态一致。
- 测试报告不包含真实 token、完整 ticket、完整 query string 或可复用密钥。

辅助验证点（建议验证，可附说明跳过）：

- `CheckOrigin` 当前允许所有来源；若执行安全测试，应作为风险记录，不作为本模块功能失败。
- 连接建立 P95、关键事件端到端 P95 可在专项性能测试中验证；本模块契约测试只记录小规模延迟证据。
- 日志中不应主动记录完整 ticket query；如果日志系统不可观测，应标记为未覆盖。

## 19. 需用户确认的问题

- WebSocket 握手是否必须校验 `room_id` 对应直播间存在且可访问？当前代码只校验 room_id 非空。
- `CheckOrigin` 是否应限制允许的前端域名？当前代码允许所有 origin。
- 文档中的 `join_room` 客户端事件是否仍需要实现？当前连接房间来自 URL path，Conn 未处理 `join_room`。
- 客户端未知事件是否需要返回错误消息？当前实现静默忽略。
- WebSocket 业务事件发送失败是否需要日志、重试或补偿？当前 broadcaster 错误被业务服务忽略。
- ticket 是否需要绑定目标 room_id 或设备信息？当前 ticket 只绑定 user_id。
- `auction_extended` payload 是否必须包含 `old_end_time`？DTO 有字段，但当前出价广播只填充 `new_end_time` 和 `extend_seconds`。

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
