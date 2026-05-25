# WebSocket 模块设计文档

**日期**：2026-05-25  
**状态**：已确认，待实现

---

## 1. 背景与目标

为直播竞拍系统实现 WebSocket 实时推送模块，负责：

- 客户端连接管理（join / leave / heartbeat）
- 竞拍关键事件广播（出价、延时、结束、取消）
- 定向推送（被超越通知、订单生成通知）
- 直播间在线人数维护

WebSocket 不作为最终数据来源，只负责实时同步。客户端断线重连后通过 HTTP 商品详情接口恢复状态。

---

## 2. 技术选型

| 项目 | 选择 | 理由 |
|------|------|------|
| WS 库 | `github.com/gorilla/websocket` | 生态最成熟，与 flamego 集成有现成参考 |
| 广播接口 | `wsevent.Broadcaster` interface 注入 | 与现有 Service 依赖注入模式一致，便于 mock 测试，后续转多实例只需替换实现 |
| Hub 连接索引 | 双索引（room + user） | 广播和定向推送都高效 |
| 通知持久化 | 本版不做 | notification 模块独立迭代 |

---

## 3. 包结构

```
pkg/wsevent/
  broadcaster.go   # Broadcaster interface + Event struct + topic helper

internal/app/ws/
  hub/
    hub.go         # Hub struct，双索引，主循环，Fanout/Unicast 实现
    conn.go        # Conn struct，readLoop，writeLoop
  handler/
    ticket.go      # POST /api/v1/ws-ticket
    ws.go          # GET /ws/v1/rooms/{room_id}，握手升级
  router/
    router.go      # 路由注册
  init.go          # Module 实现，暴露 var Hub *hub.Hub

internal/app/item/dto/
  events.go        # item 模块事件常量 + payload struct

internal/app/order/dto/
  events.go        # order 模块事件常量 + payload struct
```

---

## 4. 共享契约：pkg/wsevent

```go
// broadcaster.go

type Event struct {
    Type    string `json:"type"`
    Payload any    `json:"payload"`
}

type Broadcaster interface {
    Fanout(topic string, event Event) error  // 一对多（全房间广播）
    Unicast(addr string, event Event) error  // 一对一（定向推送）
}

// topic 辅助函数，调用方无需记格式
func RoomTopic(roomID string) string { return "room:" + roomID }
func UserAddr(userID string) string  { return "user:" + userID }
```

`pkg/wsevent` 无任何业务概念，不知道 room、user、auction 是什么。业务 event 常量和 payload struct 由各模块自己的 dto 定义。

---

## 5. Hub 架构

### 5.1 数据结构

```go
// hub/hub.go
type Hub struct {
    mu    sync.RWMutex
    rooms map[string]map[string]*Conn  // roomID → connID → Conn
    users map[string][]*Conn           // userID → []*Conn（支持多 tab）
    redis *redis.Client
}

// hub/conn.go
type Conn struct {
    id     string             // snowflake 生成
    userID string
    roomID string
    ws     *websocket.Conn
    send   chan wsevent.Event  // 有缓冲 channel，writeLoop 消费
}
```

### 5.2 连接生命周期

**连接建立（`Hub.Register(conn)`）：**

```
同步（写锁保护）：
  rooms[roomID][connID] = conn
  users[userID] = append(users[userID], conn)

异步（go syncRedisOnJoin，失败不阻断连接建立）：
  Redis SADD auction:room:{roomID}:online_users {userID}
  Redis HINCRBY auction:room:{roomID}:state online_count +1
```

**连接断开（`Hub.Remove(conn)`）：**

```
同步（写锁保护）：从 rooms / users 双索引删除
异步（go syncRedisOnLeave）：Redis SREM + HINCRBY -1
```

### 5.3 慢连接处理

投递到 `conn.send` 时使用非阻塞 select：

```go
select {
case conn.send <- event:
default:
    // send channel 已满 → 该连接是慢连接
    // 主动关闭连接，让客户端感知断线并重连拉 HTTP 快照
    h.closeConn(conn)
}
```

竞拍事件不可静默丢弃，断开慢连接比丢数据更安全。

### 5.4 广播实现

- `Fanout`：持 `RWMutex.RLock`，解析 `room:{roomID}`，遍历 `rooms[roomID]`，逐个投递
- `Unicast`：持 `RWMutex.RLock`，解析 `user:{userID}`，遍历 `users[userID]`，逐个投递

### 5.5 Conn goroutine

每个连接启动两条 goroutine：

- **readLoop**：读客户端消息，处理 ping/leave_room，每次收到消息重置 ReadDeadline（60s），超时自动触发断开
- **writeLoop**：消费 `conn.send` channel，序列化后写入 WebSocket

---

## 6. Ticket 系统 & 连接建立

### 6.1 Ticket 申请

```
POST /api/v1/ws-ticket
Authorization: Bearer <jwt>
```

```
1. 验证 JWT → userID
2. 生成随机 ticket（crypto/rand UUID）
3. Redis SET ws:ticket:{ticket} {userID} EX 45
4. 返回 {"ticket": "..."}
```

TTL 45 秒，一次性使用。

### 6.2 WebSocket 握手

```
GET /ws/v1/rooms/{room_id}?ticket=<ticket>
```

```
1. 取 query 中的 ticket
2. Redis GETDEL ws:ticket:{ticket} → userID（原子读删）
3. 不存在或已过期 → 401，拒绝升级
4. gorilla Upgrade → *websocket.Conn
5. 构造 Conn，注册到 Hub
6. 启动 readLoop + writeLoop
```

### 6.3 客户端消息处理

| 客户端消息 | 服务端动作 |
|---|---|
| `{"type":"ping"}` | 回 `{"type":"pong"}`，重置 ReadDeadline |
| `{"type":"join_room",...}` | 忽略（URL 已绑定房间） |
| `{"type":"leave_room"}` | 关闭连接，触发 Hub remove |
| 超时 / 网络断开 | readLoop 退出，触发 Hub remove |

---

## 7. 模块注入

### 7.1 ws 模块暴露 singleton

```go
// internal/app/ws/init.go
var Hub *hub.Hub  // 实现 wsevent.Broadcaster

func (w *WS) Load(engine *kernel.Engine) error {
    Hub = hub.NewHub(engine.Cache)
    // 无需 Run() goroutine，Hub 通过 sync.RWMutex 保护内部状态
    handler.Init(Hub)
    router.RegisterRoutes(engine.Flame)
    return nil
}
```

### 7.2 模块注册顺序

WS 模块必须在 item / room / order **之前**注册，保证其他模块 `Load()` 时 `wsapp.Hub` 已初始化：

```go
// internal/app/appInitialize/init.go
var apps = []app.Module{
    &user.User{},
    &ws.WS{},      // 新增，排在 item/room/order 前
    &room.Room{},
    &order.Order{},
    &payment.Payment{},
    &deposit.Deposit{},
    &item.Item{},
}
```

### 7.3 Service 持有 Broadcaster

需要注入 Hub 的模块：**item** 和 **order**（两者都会触发广播事件）。

```go
// item/service/service.go
type Service struct {
    ...
    broadcaster wsevent.Broadcaster  // nil-safe，测试时可不传
}

// order/service/service.go
type Service struct {
    ...
    broadcaster wsevent.Broadcaster  // nil-safe，测试时可不传
}
```

调用时守卫：

```go
if s.broadcaster != nil {
    _ = s.broadcaster.Fanout(wsevent.RoomTopic(roomID), evt)
}
```

order 模块的 `Load()` 同样注入 `wsapp.Hub`：

```go
// internal/app/order/init.go
func (o *Order) Load(engine *kernel.Engine) error {
    ...
    Svc = service.NewService(store, wsapp.Hub)
    ...
}
```

---

## 8. 事件触发点

| 触发位置 | 事件 | 广播方式 |
|---|---|---|
| `item/service.StartItem()` | `auction_started` | `Fanout(RoomTopic(roomID))` |
| `item/service.PlaceBid()` — 出价成功 | `bid_success` | `Fanout(RoomTopic)` |
| `item/service.PlaceBid()` — 原领先者被超越 | `user_outbid` | `Unicast(UserAddr(prevLeaderID))`，prevLeaderID 在调用 Lua 前从 Redis state 读取当前 `leader_user_id`，出价成功后与新领先者对比，不同则触发 |
| `item/service.PlaceBid()` — 触发自动延时 | `auction_extended` | `Fanout(RoomTopic)` |
| `item/service.PlaceBid()` — 达到封顶价 | `auction_ended` | `Fanout(RoomTopic)` |
| `item/service.CancelItem()` | `auction_cancelled` | `Fanout(RoomTopic)` |
| `item/service.EndExpiredAuctions()` | `auction_ended` | `Fanout(RoomTopic)` |
| `order/service.CreateOrder()` | `order_created` | `Fanout(RoomTopic)` + `Unicast(UserAddr(winnerID))` |

---

## 9. 业务事件 payload 归属

各模块在自己的 `dto/events.go` 定义事件常量和 payload struct，示例：

```go
// item/dto/events.go
const EventBidSuccess = "bid_success"

type BidSuccessPayload struct {
    ItemID       string    `json:"item_id"`
    UserID       string    `json:"user_id"`
    Price        int64     `json:"price"`
    CurrentPrice int64     `json:"current_price"`
    LeaderUserID string    `json:"leader_user_id"`
    EndTime      time.Time `json:"end_time"`
}
```

```go
// order/dto/events.go
const EventOrderCreated = "order_created"

type OrderCreatedPayload struct {
    ItemID      string `json:"item_id"`
    OrderID     string `json:"order_id"`
    WinnerID    string `json:"winner_user_id"`
    DealPrice   int64  `json:"deal_price"`
}
```

---

## 10. 未来多实例迁移路径

当前 `Hub` 是进程内内存实现。转多实例时：

1. 实现 `RedisBroadcaster`（`wsevent.Broadcaster` 的另一个实现），`Fanout`/`Unicast` 发布到 Redis Pub/Sub channel
2. 每个节点订阅自己管理的 room channel，收到消息后在本地 Hub 分发
3. 其他模块（item/order service）无需改动，只需在 `Load()` 时注入 `RedisBroadcaster` 而非 `hub.Hub`
