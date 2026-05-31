# 并发一致性测试计划：room / websocket / user

## 基本信息

- 计划来源：用户要求执行房间并发、WebSocket 并发正确性、用户唯一性并发测试。
- 计划时间：2026-05-31 20:10 Asia/Shanghai。
- 目标：验证真实并发条件下，房间状态、WebSocket 实时通道、Redis 在线状态和用户账号唯一性是否满足契约不变量。
- Review 结果：通过。
- 批准方式：用户在对话中回复“开始测试”。
- 执行结果：未执行。

## 读取文档

- `docs/agent-testing/README.md`
- `docs/agent-testing/templates/protocol.md`
- `docs/agent-testing/guides/runner.md`
- `docs/agent-testing/guides/concurrency.md`
- `docs/agent-testing/guides/go-runner.md`
- `docs/agent-testing/guides/environment.md`
- `docs/agent-testing/modules/room.md`
- `docs/agent-testing/modules/ws.md`
- `docs/agent-testing/modules/user.md`

## 涉及模块

- 目标模块：room、ws、user。
- 关联模块：item、bid、deposit、order 仅作为 WebSocket 业务事件触发和最终状态对账来源。
- 关联 flow：无完整 flow 扩展；只覆盖本计划列出的跨模块触发点。

## 测试范围

- 房间并发：同商家激活、同房间开播、同房间下播、不同商家并发操作各自房间。
- WebSocket 并发：同 ticket 并发握手、同用户多连接单播、多用户同房间广播与跨房间隔离、断开与广播并发、Redis 在线状态并发增减。
- 用户唯一性：同账号并发注册。

## 禁止范围

- 不覆盖 I6 类过期结算 / 取消竞态。
- 不做容量压测、长稳压测或吞吐阈值评估。
- 不清空数据库或 Redis。
- 不写入真实 token、ticket、DSN、Redis 地址密码到报告。
- 不把 WebSocket 消息作为最终业务事实；所有 WS 结论必须附带 HTTP / Redis / MySQL 对账。

## 测试类型

- 并发一致性测试。
- 状态一致性测试。
- WebSocket 真实连接测试。
- 少量接口契约抽样，用于证明请求和响应可解释。

## 测试数据

- 批次 ID：`agent_room_ws_user_concurrency_<YYYYMMDDHHMMSS>`。
- 账号前缀：`agent_rwu_<batch>_`。
- 房间 title 前缀：`agent_rwu_<batch>_room_`。
- 商品 title 前缀：`agent_rwu_<batch>_item_`。
- Redis 只触达本批次房间 key、item key、ticket key 和在线状态 key。
- 清理仅按本批次创建的 user、room、item、rule、bid_log、deposit、order 和 Redis key 执行。

## 依赖策略

| 依赖 | 使用方式 | 原因 |
| --- | --- | --- |
| HTTP | 真实本地服务 | 验证 handler、鉴权、绑定和统一响应 |
| WebSocket | 真实 gorilla websocket 客户端连接 | 验证 ticket GETDEL、Hub 注册、广播、单播、断开 |
| MySQL | 真实测试库，仅本批次数据 | 验证唯一约束、状态最终值和查询视图 |
| Redis | 真实测试 Redis，仅本批次 key | 验证 ticket、room state、online_users、item queue |
| 第三方 | 不使用 | 不属于本批次目标 |

## 执行步骤

1. 运行 `go test ./...` 确认编译和单元测试基线。
2. 检查 MySQL、Redis、本地服务可用性。
3. 创建 Go runner，使用同步起跑 gate 发起并发 HTTP / WebSocket 请求。
4. 按场景打印每个 attempt 的开始/结束时间、HTTP 状态、业务 code、WS 结果摘要。
5. 每个场景结束后执行 MySQL、Redis、HTTP 公开详情、商家视图对账。
6. 清理本批次数据和 Redis key。
7. 写入 `docs/agent-testing/reports/` 报告。

## 验证方式

- HTTP：状态码、统一响应 `code/message/data`、关键 ID 和状态。
- MySQL：`users`、`live_rooms`、`auction_items`、`auction_rules`、`bid_logs`、必要时 `orders/deposits`。
- Redis：`ws:ticket:*`、`auction:room:{room_id}:state`、`auction:room:{room_id}:online_users`、`auction:room:{room_id}:item_queue`、item state/ranking。
- WebSocket：只记录事件类型、目标连接数量和房间隔离结果；不记录完整 ticket 或完整 URL。
- 最终事实：以 HTTP 查询 + MySQL + Redis 对账为准。

## 并发场景设计

### R1 同一商家并发激活房间

- 场景名称：同一商家并发激活房间。
- 竞争对象：`live_rooms.merchant_id` 唯一约束和 `ActivateRoom` 幂等语义。
- 并发请求：同一 merchant token 同步发起 10 个 `POST /api/v1/merchant/room`，body title 可相同。
- 预期成功：所有请求应可解释；理想结果为全部 200 且返回同一个 room_id，或 1 个创建成功其余返回已有房间。
- 预期失败：不允许 HTTP 500 / 原始唯一索引错误；不允许创建多条有效 room。
- 最终不变量：`live_rooms` 中该 merchant 只有 1 条未删除记录；公开详情和商家视图 room_id/status/title 与 DB 一致；Redis room state 若存在不与 DB 状态矛盾。

### R2 同一直播间并发开播

- 场景名称：同一直播间并发开播。
- 竞争对象：同一 `live_rooms.id` 的 `idle -> live` 状态流转。
- 并发请求：对一个 idle 房间同步发起 8 个 `POST /api/v1/rooms/{room_id}/start`。
- 预期成功：只有一个状态流转成功；重复请求可以返回业务错误或幂等可解释响应，但不能产生未定义状态。
- 预期失败：非成功请求应为业务错误，不应 HTTP 500；不允许最终状态不是 `live`。
- 最终不变量：DB status=`live`；Redis room state status 若存在应为 `live`；公开详情和商家视图 status=`live` 且 `current_item_id` 字段存在。

### R3 同一直播间并发下播

- 场景名称：同一直播间并发下播。
- 竞争对象：同一 `live_rooms.id` 的 `live -> idle` 状态流转。
- 并发请求：对一个 live 房间同步发起 8 个 `POST /api/v1/rooms/{room_id}/end`。
- 预期成功：只有一个状态流转成功；重复请求可以返回业务错误或幂等可解释响应，但不能产生未定义状态。
- 预期失败：非成功请求应为业务错误，不应 HTTP 500；不允许最终状态不是 `idle`。
- 最终不变量：DB status=`idle` 且 `current_item_id=""`；Redis room state status 若存在应为 `idle` 且 current_item_id 为空；公开详情和商家视图一致。

### R4 不同商家并发操作各自房间

- 场景名称：不同商家并发操作各自房间。
- 竞争对象：多个 merchant 各自的 `live_rooms` 行和 Redis room state。
- 并发请求：5 个商家分别对自己的房间执行激活 / 开播 / 下播组合，所有请求同步起跑但互不共享 room_id。
- 预期成功：各商家只影响自己的房间；可解释业务错误只允许来自自身状态不满足动作前置条件。
- 预期失败：不允许跨 merchant 修改别人的 room；不允许 HTTP 500；不允许 Redis state 写到错误 room key。
- 最终不变量：每个 merchant 恰好 1 个 room；DB、Redis、公开详情、商家视图按 room_id 分别一致。

### W1 同一 ticket 并发握手

- 场景名称：同一 ticket 并发 WebSocket 握手。
- 竞争对象：`ws:ticket:{ticket}` 的 Redis `GETDEL` 原子消费。
- 并发请求：同一 ticket 同步发起 5 个 `GET /ws/v1/rooms/{room_id}?ticket=<ticket>` WebSocket dial。
- 预期成功：最多 1 个握手成功。
- 预期失败：其余握手返回 401 invalid/expired ticket 或连接失败；不能成功建立第二条连接。
- 最终不变量：Redis ticket key 不存在；Redis online_count 与成功连接数一致；关闭连接后 online_count 最终回到 0 且 online_users 不含测试用户。

### W2 同一用户多连接，单播到所有连接

- 场景名称：同一用户多连接单播。
- 竞争对象：Hub `users[user_id]` 多连接索引和 `Unicast(user:<id>)`。
- 并发请求：同一用户使用不同 ticket 建立 2 条同房间连接；另一个用户出价超过该用户，触发 `user_outbid` 单播。
- 预期成功：该用户的 2 条连接都收到 `user_outbid`；其他用户连接不收到该单播。
- 预期失败：任一目标连接漏收、非目标用户收到单播、业务出价状态与事件 payload 不一致均为失败。
- 最终不变量：HTTP item detail / Redis item state / MySQL bid_logs 显示新 leader 和当前价与 `user_outbid` payload 一致；WebSocket 消息只作为实时证据。

### W3 多用户同房间广播，非目标房间不能收到

- 场景名称：同房间广播与跨房间隔离。
- 竞争对象：Hub `rooms[room_id]` 广播索引。
- 并发请求：多个用户连接 room A，另有用户连接 room B；启动 room A 中的商品竞拍或发起 room A 出价，触发房间广播。
- 预期成功：room A 所有连接收到目标广播；room B 连接在观察窗口内不收到 room A 事件。
- 预期失败：room A 漏播或 room B 串房收到事件均为失败。
- 最终不变量：HTTP item detail / MySQL item 状态 / Redis room state 与广播事件一致；非目标房间 DB/Redis 状态不受影响。

### W4 连接断开与广播同时发生

- 场景名称：连接断开与广播同时发生。
- 竞争对象：Hub `Remove` 与 `Fanout` 对同一 room 索引的并发读写。
- 并发请求：一批连接建立后，同步触发部分连接 `leave_room` / close，同时触发房间广播事件。
- 预期成功：服务不崩溃，请求不出现 HTTP 500；剩余连接可继续收消息；断开的连接是否收到并发窗口内消息不作为最终事实。
- 预期失败：panic、服务不可用、Redis online_count 长期为负、最终业务状态不一致均为失败。
- 最终不变量：HTTP / MySQL / Redis 业务状态与触发动作一致；所有连接关闭后 Redis online_count 最终为 0 且 online_users 清空。

### W5 Redis 在线状态并发增减

- 场景名称：Redis 在线状态并发增减。
- 竞争对象：`auction:room:{room_id}:state.online_count` 和 `auction:room:{room_id}:online_users`。
- 并发请求：10 个不同用户并发连接同一 room，再并发断开。
- 预期成功：连接稳定后 online_count=10、online_users size=10；全部断开后最终 online_count=0、online_users size=0。
- 预期失败：online_count 负数、断开后残留用户、连接期间计数与连接数不可解释均为失败。
- 最终不变量：Redis 在线状态与实际连接生命周期一致；HTTP 房间详情读取的 online_count 与 Redis 一致。

### U1 同一账号并发注册

- 场景名称：同一账号并发注册。
- 竞争对象：`users.account` 唯一约束和 `Register` 查重语义。
- 并发请求：同一 account/password 同步发起 10 个 `POST /api/v1/auth/register`。
- 预期成功：最多 1 个请求注册成功并返回 token/user；其他请求应返回可解释业务错误。
- 预期失败：HTTP 500 / 原始唯一索引错误、多条有效同账号用户、响应中泄露 password 均为失败。
- 最终不变量：`users` 中该 account 只有 1 条未删除记录；成功响应 user.id 与 DB 一致；DB password 不等于明文；后续 login/account 查询可解释。

## 预计输出

- 并发计划文件：本文件。
- Go runner 输出：每个 CASE 的 HTTP / WS / DB / Redis 摘要，SUMMARY 和 CLEANUP。
- 测试报告：`docs/agent-testing/reports/<timestamp>-room-ws-user-concurrency.md`。
