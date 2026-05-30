# WebSocket 可靠性补强设计文档

**日期**：2026-05-30  
**状态**：已确认，待实现

---

## 1. 背景与目标

现有 WebSocket 模块已经支持短期 ticket 鉴权、房间连接、实时广播、文本心跳和 Redis 在线人数同步。当前需要补强四类可靠性能力：

- 服务端主动心跳：服务端定时发送 ping/control frame，尽快发现 NAT、弱网、客户端进程挂起导致的僵尸连接。
- 倒计时同步：服务端每秒广播当前拍品的结束时间，客户端以服务端时间为准渲染倒计时。
- 断线重连恢复说明：明确重连后通过 HTTP 快照恢复现场，不依赖 WebSocket 历史消息。
- 在线数可信度：连接断开、`leave_room`、异常关闭后，Redis 在线状态必须及时收敛。

本设计补充 `docs/superpowers/specs/2026-05-25-websocket-module-design.md`，不改变 WebSocket 只负责实时同步、不作为最终数据来源的原则。

---

## 2. 范围与非目标

### 范围

- `internal/app/ws/hub/`：连接生命周期、主动心跳、关闭幂等、在线状态同步。
- `internal/app/item/service/`：竞拍开始、延时、结束、取消时的倒计时同步事件和 room current item 维护。
- `internal/app/room/cache/` 与 room 状态：支持 `current_item_id` 和可信 `online_count` 读取。
- `internal/app/item/dto/events.go`：新增 `time_sync` 事件类型和 payload。
- WebSocket 模块测试文档：补充心跳、重连恢复、在线状态收敛测试点。

### 非目标

- 不支持 WebSocket 历史消息回放。
- 不实现 `auction:room:{room_id}:events` 事件补偿。
- 不支持同一用户在同一直播间同时保留多条 WebSocket 连接。
- 不引入 Redis Pub/Sub 多实例广播架构。
- 不做大规模压测或容量调优，只保证 V1 功能正确性和可测试性。

---

## 3. 关键约束

### 3.1 单用户单房间单连接

V1 竞拍场景暂不支持同一用户在同一直播间多连接。服务端必须把这个约束写进连接生命周期：

- 同一个 `user_id` 再次连接同一个 `room_id` 时，新连接生效，旧连接被服务端关闭。
- Redis `online_users` 以 `user_id` 为成员，表示该用户是否在房间在线。
- `online_count` 表示当前房间在线用户数，等于 `SCARD auction:room:{room_id}:online_users`。
- 不为同一用户多 tab、多设备、多连接引入 `conn set` 或复杂引用计数。

### 3.2 WebSocket 不是事实来源

客户端收到的 WebSocket 消息只是实时增量。刷新页面、断线重连、慢连接被剔除后，客户端必须通过 HTTP 查询恢复当前事实状态。

---

## 4. 服务端主动心跳

### 4.1 当前问题

当前 `Conn.StartReadLoop()` 只在读到客户端消息后刷新 `readDeadline`，并支持文本 `{"type":"ping"}` 返回 `pong`。如果客户端、NAT 或网络中间层静默断开，服务端可能无法及时发现僵尸连接。

### 4.2 设计

在 `Conn.StartWriteLoop()` 增加服务端主动 ping：

- `pingInterval = 25s`。
- 每个连接启动一个 ticker，定时发送 WebSocket control `PingMessage`。
- 写 ping 前设置 `writeDeadline = now + 10s`。
- ping 写失败时关闭连接。

在 `Conn.StartReadLoop()` 配置 pong 处理：

- 初始设置 `readDeadline = now + 60s`。
- `SetPongHandler` 中刷新 `readDeadline = now + 60s`。
- 每次读到任意客户端消息后也刷新 `readDeadline`。
- 超时或读失败时关闭连接。

保留文本心跳兼容：

| 客户端消息 | 服务端行为 |
| --- | --- |
| `{"type":"ping"}` | 投递 `{"type":"pong"}` 文本事件，并刷新 read deadline |
| WebSocket control pong | 刷新 read deadline，不广播文本事件 |
| `{"type":"leave_room"}` | 主动关闭连接，触发在线状态收敛 |

### 4.3 关闭幂等

`Conn` 增加统一关闭方法，例如 `Close()`：

- 内部使用 `sync.Once`。
- 只允许一次 `hub.Remove(conn)`。
- 只允许一次关闭 `send` channel。
- 只允许一次关闭底层 websocket。

`readLoop`、`writeLoop`、慢连接剔除、`leave_room` 都调用这个统一关闭方法，避免重复 `Remove` 造成 Redis 在线数漂移。

---

## 5. 倒计时毫秒级同步

### 5.1 事件契约

新增服务端事件：

```json
{
  "type": "time_sync",
  "payload": {
    "item_id": "item_123",
    "server_unix_ms": 1780123456789,
    "ends_at_unix_ms": 1780123465000
  }
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `item_id` | 当前同步的竞拍商品 ID |
| `server_unix_ms` | 服务端当前 Unix 毫秒时间，客户端用于估算服务端时间偏移 |
| `ends_at_unix_ms` | 服务端认定的竞拍结束 Unix 毫秒时间 |

`server_unix_ms` 必须保留。如果只广播 `ends_at_unix_ms`，客户端仍会用本机 `Date.now()` 渲染剩余时间，无法解决本地时钟漂移。

### 5.2 广播节奏

- 对每个正在竞拍的 item，每秒广播一次 `time_sync` 到所属 room。
- `ends_at_unix_ms` 从 Redis `auction:item:{item_id}:state.end_time_unix` 读取后乘以 1000。
- 反狙击延时由出价 Lua 更新 Redis 结束时间，下一次 `time_sync` 自动携带新结束时间。
- `auction_started`、`bid_success`、`auction_extended` 仍可继续携带 `end_time` 字段；`time_sync` 是稳定校时通道。

### 5.3 生命周期

竞拍开始：

1. `StartItem` 初始化 Redis auction state。
2. 商品状态更新为 `ongoing`。
3. 写入 room 当前商品：
   - MySQL `LiveRoom.CurrentItemID = item_id`
   - Redis `auction:room:{room_id}:state.current_item_id = item_id`
4. 广播 `auction_started`。
5. 启动或登记该 item 的 `time_sync` 每秒广播。

竞拍结束或取消：

1. 商品状态更新为 `ended` 或 `cancelled`。
2. 删除 Redis auction state 或停止使用该 state。
3. 清空 room 当前商品：
   - MySQL `LiveRoom.CurrentItemID = ""`
   - Redis `auction:room:{room_id}:state.current_item_id = ""`
4. 停止该 item 的 `time_sync` 广播。
5. 广播 `auction_ended` 或 `auction_cancelled`。

### 5.4 实现建议

优先实现一个轻量的 `TimeSyncManager`，由 item service 在生命周期节点调用：

- `Start(roomID, itemID string)`
- `Stop(itemID string)`
- `StopRoom(roomID string)` 可选，用于下播兜底

Manager 内部每秒 tick 时：

1. 遍历活跃 item。
2. 读取 Redis auction state。
3. state 不存在或结束时间已过，停止该 item。
4. 广播 `time_sync`。

如果暂时不引入 manager，也可以在 item 模块 cron 中每秒扫描 ongoing item，但这会增加数据库扫描或 Redis key 管理复杂度。推荐 manager，因为 V1 单进程 Hub 已经是当前架构假设。

---

## 6. 断线重连恢复

### 6.1 原则

重连后不回放 WebSocket 历史消息。客户端必须通过 HTTP 快照恢复现场，再订阅后续 WebSocket 增量。

### 6.2 前端恢复流程

1. 调用 `GET /api/v1/rooms/{room_id}`。
   - 获取 `status`、`current_item_id`、`online_count`、`item_queue`。
2. 如果 `current_item_id` 非空，调用 `GET /api/v1/items/{item_id}`。
   - 获取当前价、领先用户、出价次数、参与人数、剩余时间、是否已延时。
3. 调用 `GET /api/v1/items/{item_id}/ranking`。
   - 恢复排行榜。
4. 调用 `POST /api/v1/ws-ticket` 获取新 ticket。
5. 连接 `GET /ws/v1/rooms/{room_id}?ticket=...`。
6. 连接成功后以 `time_sync` 和业务事件继续增量刷新。

### 6.3 后端要求

- room detail 的 `current_item_id` 必须来自 MySQL 基础字段，并允许 Redis state 覆盖同值。
- item detail 进行中实时字段优先读取 Redis auction state。
- ranking 优先读取 Redis ZSET。
- WebSocket ticket 仍是一次性，重连必须重新申请。
- 不在日志中记录完整 WebSocket query string。

---

## 7. 在线状态可信度

### 7.1 当前问题

当前实现使用：

- `SADD auction:room:{room_id}:online_users user_id`
- `HINCRBY auction:room:{room_id}:state online_count 1`
- leave 时 `SREM` + `HINCRBY -1`

风险：

- 关闭路径可能重复执行，导致 `online_count` 负数或漂移。
- `HINCRBY` 是增量计数，一旦某次 join/leave Redis 写失败，后续无法自动校正。
- 设计文档中曾提到同用户多连接，但 V1 实际不支持，会干扰在线数口径。

### 7.2 新口径

Redis 在线用户集合是事实来源：

```text
auction:room:{room_id}:online_users  SET user_id
auction:room:{room_id}:state         HASH online_count = SCARD(online_users)
```

`online_count` 是派生值，只为 room detail/list 快速读取。每次 join/leave 后从 `SCARD online_users` 重新写回，避免增量漂移。

### 7.3 Register 流程

同步内存步骤：

1. 加锁检查 `rooms[roomID]` 是否已有同 `userID` 连接。
2. 如果有旧连接，将旧连接移出索引，并异步关闭旧 websocket。
3. 写入新连接：
   - `rooms[roomID][connID] = conn`
   - `users[userID] = conn` 或保留 `users[userID][]*Conn` 但同 room 只允许一个。

Redis 步骤：

1. `SADD auction:room:{roomID}:online_users userID`
2. `SCARD auction:room:{roomID}:online_users`
3. `HSET auction:room:{roomID}:state online_count <scard>`

Redis 写失败不阻断连接，但必须记录 warning 日志。

### 7.4 Remove 流程

同步内存步骤：

1. 加锁检查该 `connID` 是否仍是 room 中当前有效连接。
2. 如果不是当前有效连接，说明它是已被新连接替换的旧连接，只做本地关闭，不删除新连接对应的在线状态。
3. 如果是当前有效连接，从 room/user 索引删除。

Redis 步骤：

1. `SREM auction:room:{roomID}:online_users userID`
2. `SCARD auction:room:{roomID}:online_users`
3. `HSET auction:room:{roomID}:state online_count <scard>`

这个检查保证“新连接踢旧连接”时，旧连接后续退出不会误删新连接的在线状态。

### 7.5 收敛触发点

以下路径都必须最终调用统一关闭方法：

- 客户端发送 `leave_room`
- readLoop 读失败或 read deadline 超时
- writeLoop 写业务事件失败
- 服务端主动 ping 写失败
- send channel 满，慢连接被 Hub 剔除
- handler 或服务停止时的连接关闭兜底

---

## 8. 错误处理与降级

| 场景 | 处理 |
| --- | --- |
| 服务端 ping 失败 | 关闭连接，触发 Remove |
| 客户端不回 pong | read deadline 超时后关闭连接 |
| `time_sync` 读取 Redis state 失败 | 本轮不广播，记录 warning，下一秒重试 |
| Redis state 不存在 | 停止该 item 的 `time_sync` |
| online Redis 写失败 | 连接不失败，记录 warning；下一次 join/leave 可通过 SCARD 重新校正 |
| WebSocket 广播失败 | 不回滚业务状态 |
| HTTP 恢复接口 Redis 失败 | 按现有策略降级，online_count=0，item_queue=[] |

---

## 9. 测试计划

### 9.1 单元测试

Hub/Conn：

- `Register` 同一 room 同一 user 新连接会替换旧连接。
- 旧连接被替换后再关闭，不会删除新连接的在线状态。
- `Remove` 对同一连接重复调用只执行一次。
- `leave_room` 触发连接关闭和索引清理。
- send channel 满触发慢连接关闭，并且关闭幂等。
- `Fanout` 不向已移除连接投递。

在线状态：

- join 后 `online_users` 包含 user，`online_count = SCARD`。
- leave 后 `online_users` 删除 user，`online_count = SCARD`。
- Redis 某次 HSET 前计数漂移时，下一次 join/leave 会重新写回 SCARD。

Time sync：

- ongoing item 每秒广播 `time_sync`，payload 包含 `item_id`、`server_unix_ms`、`ends_at_unix_ms`。
- Redis end time 更新后下一次广播使用新值。
- auction state 缺失后停止广播。

### 9.2 Agent 接口/集成测试

按 `docs/agent-testing/README.md` 路由执行，不直接读取深层测试文档。

建议覆盖：

- 客户端连接后不发送文本 ping，仅依赖 control pong，连接保持。
- 客户端不响应服务端 ping，连接在 read deadline 后被清理。
- 正常 `leave_room` 后 Redis 在线集合和 `online_count` 收敛。
- 异常关闭 WebSocket 后 Redis 在线集合和 `online_count` 收敛。
- 同一用户重复连接同一 room，只保留新连接，在线数仍为 1。
- 开始竞拍后收到 `auction_started` 和后续 `time_sync`。
- 出价触发自动延时后，`time_sync.ends_at_unix_ms` 更新。
- 断线后通过 room detail、item detail、ranking 恢复现场，再重连接收后续事件。

---

## 10. 文档更新

实现时需要同步更新：

- `docs/agent-testing/modules/ws.md`
  - 明确 V1 不支持同一用户同房间多连接。
  - 更新在线数口径：`online_count = SCARD online_users`。
  - 增加服务端主动 ping/control pong 测试点。
  - 增加 `time_sync` 事件契约。
- `docs/5-21.md`
  - 可补充 `time_sync` 服务端事件。
  - 可明确断线重连 HTTP 恢复顺序。

---

## 11. 验收标准

- 服务端能主动发现僵尸连接，并在超时后关闭连接。
- `leave_room`、异常关闭、慢连接剔除后，Hub 内存索引清理完成。
- Redis `online_users` 和 room state `online_count` 在 join/leave 后一致。
- 同一用户重复连接同一直播间时，旧连接被关闭，新连接保留，在线数不增加。
- 进行中竞拍每秒广播 `time_sync`。
- 反狙击延时后，`time_sync.ends_at_unix_ms` 反映新的结束时间。
- 前端重连恢复不依赖 WebSocket 历史消息，只依赖 HTTP room/item/ranking 快照和后续实时事件。
