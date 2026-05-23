# Room 模块设计

**日期**: 2026-05-23  
**作者**: Claude Code  
**范围**: `internal/app/room/`（新建模块）

---

## 背景

Room（直播间）是整个竞拍系统的容器。商家在一个直播间里按顺序拍卖多件商品，C 端用户通过直播间 WebSocket 连接参与实时竞拍。

本文档设计 Room 模块的 HTTP 管理接口、MySQL 持久层、Redis 实时状态层。WebSocket 连接管理、在线人数广播、事件流属于独立的 WebSocket 模块，不在本文档范围内。

---

## 核心概念

**每个商家有且仅有一个房间**，代表其固定的直播频道（类似抖音直播间）。

房间不随商家身份自动创建，需要商家**主动开通直播**才分配（类似平台资质申请）。开通后，商家可以反复开播/下播，房间本身永久存在。房间内维护一个有序的待拍商品队列，商品按上架时间（`PublishItem`）自动入队，竞拍结束后从队列移出（由出价模块负责）。

**房间与商品的关系**：
- `AuctionItem.RoomID` → 商品归属哪个房间（在 item 模块 `CreateItem` 时确定）
- `LiveRoom.CurrentItemID` → 当前正在竞拍的商品（由出价模块在 `StartItem` 后写入）
- `auction:room:{room_id}:item_queue` ZSET → 已上架待拍的商品 ID 有序列表（item 模块写入，room 模块读取）

---

## 数据模型

### LiveRoom

```go
type RoomStatus string

const (
    RoomIdle RoomStatus = "idle" // 已开通，未开播（含下播后）
    RoomLive RoomStatus = "live" // 直播中
)

type LiveRoom struct {
    ID            string     `gorm:"primaryKey;size:64" json:"id"`
    MerchantID    string     `gorm:"uniqueIndex;size:64;not null" json:"merchant_id"`
    Title         string     `gorm:"size:128;not null" json:"title"`
    Status        RoomStatus `gorm:"size:32;not null" json:"status"`
    CurrentItemID string     `gorm:"size:64" json:"current_item_id,omitempty"`

    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
    CreatedAt time.Time      `json:"created_at"`
    UpdatedAt time.Time      `json:"updated_at"`
}
```

字段说明：
- `MerchantID`：**唯一索引**，一个商家只能开通一个房间。
- `CurrentItemID`：当前正在竞拍的商品 ID，对应设计文档的 `ItemID`，改名更语义化。由出价模块在竞拍开始时写入，不由 room 模块管理。
- `Title`：直播间标题，用于 C 端列表展示。

---

## Redis 数据结构

### 直播间状态：`auction:room:{room_id}:state`

**Type**: HASH

| Field | 说明 |
|---|---|
| `merchant_id` | 商家 ID |
| `status` | "idle" / "live" / "ended" |
| `current_item_id` | 当前正在拍的商品 ID，初始 = "" |
| `online_count` | 在线人数，由 WebSocket 模块维护，room 模块只读 |

Room 模块负责 Init（StartRoom 时）和读取，不维护 `online_count`（WebSocket 模块负责）。

### 待拍队列：`auction:room:{room_id}:item_queue`

**Type**: ZSET，score = 上架时间 Unix 秒  
由 **item 模块**写入（`PublishItem` ZADD，`CancelItem` ZREM），**room 模块只读**（ZRANGE 返回待拍列表）。

---

## 模块结构

```
internal/app/room/
├── model/room.go      — LiveRoom, RoomStatus
├── dao/room.go        — Store interface + GormStore
├── dto/room.go        — 请求/响应类型、DTO 构造函数
├── cache/cache.go     — Cache interface + RedisCache
├── service/service.go — 业务逻辑
├── handler/room.go    — flamego handlers
├── router/room.go     — 路由注册
└── init.go            — Module 实现
```

---

## HTTP 接口

### 商家端（需要商家身份）

| Method | Path | 描述 |
|---|---|---|
| `POST` | `/api/v1/merchant/room` | 开通直播（幂等：已开通则返回已有房间）|
| `GET` | `/api/v1/merchant/room` | 获取我的直播间 |
| `POST` | `/api/v1/rooms/{room_id}/start` | 开始直播（idle → live）|
| `POST` | `/api/v1/rooms/{room_id}/end` | 结束直播（live → idle）|

### C 端（公开）

| Method | Path | 描述 |
|---|---|---|
| `GET` | `/api/v1/rooms` | 直播间列表（默认只返回 live 状态）|
| `GET` | `/api/v1/rooms/{room_id}` | 直播间详情（含待拍队列）|

---

## 状态机

```
idle ↔ live
```

- `idle`：已开通直播权限，当前未开播（含每次下播后回归的状态）
- `live`：直播中，用户可连接 WebSocket 参与竞拍

商家可以反复开播/下播。没有 `ended`——房间一旦开通，永久存在。

---

## 关键流程

### ActivateRoom — 开通直播（幂等）

```
1. 验证商家身份
2. store.FindRoomByMerchantID(merchantID)
   → 已存在: 直接返回已有房间（不报错，不重建）
   → 不存在: store.CreateRoom(&LiveRoom{
         ID: "room_" + snowflake, MerchantID, Title, Status: idle
     })
3. 返回 room_id 和房间信息
```

Redis 不初始化（房间未开播前不需要实时状态）。

### StartRoom（idle → live）

```
1. findMerchantRoom → 验证归属 + idle 状态
2. room.Status = live; store.UpdateRoom(room)
   → 失败: return error
3. cache.InitRoomState(roomID, {MerchantID, Status: "live", CurrentItemID: ""})
   → 失败: 日志记录，不返回错误（MySQL 是源数据，Redis 可重建）
4. return nil
```

StartRoom Redis 软失败：直播间 Redis 状态不像竞拍 `InitAuctionState` 那么关键（不阻断出价链路），允许短暂不一致。

### EndRoom（live → idle）

```
1. findMerchantRoom → 验证归属 + live 状态
2. room.Status = idle; store.UpdateRoom(room)
   → 失败: return error
3. cache.UpdateRoomStatus(roomID, "idle")  — 软失败
4. return nil
```

EndRoom 后房间回到 `idle`，下次可以再次 StartRoom。

### GetRoom（C 端公开）

```
1. store.FindRoomByID(roomID)
2. dto.NewRoomDetailDTO(room)
3. state, ok, _ = cache.GetRoomState(roomID)
   if ok: 填充 online_count
4. itemQueue, _ = cache.GetItemQueue(roomID)  — ZRANGE 全量
   填充 item_queue 字段（item ID 有序列表）
5. return dto
```

### ListRooms（C 端公开）

- 默认过滤 `status=live`，支持查询参数 `status` 覆盖
- 批量读取 Redis online_count，失败则返回 0

### ListMerchantRooms（商家端）

- 返回当前商家所有房间（含 idle/live/ended）
- 字段比 C 端 DTO 更丰富（含待拍商品数量等）

---

## Cache 接口

**`internal/app/room/cache/cache.go`**

```go
type RoomState struct {
    MerchantID    string
    Status        string
    CurrentItemID string
    OnlineCount   int  // 只读，由 WebSocket 模块维护
}

type Cache interface {
    InitRoomState(ctx context.Context, roomID string, state RoomState) error
    GetRoomState(ctx context.Context, roomID string) (*RoomState, bool, error)
    UpdateRoomStatus(ctx context.Context, roomID, status string) error
    GetItemQueue(ctx context.Context, roomID string) ([]string, error)  // ZRANGE，按 score 升序
}
```

`GetItemQueue` 读取 item 模块写入的 `auction:room:{room_id}:item_queue` ZSET，room 模块不拥有写权限。

---

## DTO

### CreateRoomRequest

```go
type CreateRoomRequest struct {
    Title string `json:"title" binding:"required,min=1,max=128"`
}
```

### RoomDetailDTO（C 端）

```go
type RoomDetailDTO struct {
    ID            string     `json:"id"`
    MerchantID    string     `json:"merchant_id"`
    Title         string     `json:"title"`
    Status        RoomStatus `json:"status"`
    CurrentItemID string     `json:"current_item_id,omitempty"`
    OnlineCount   int        `json:"online_count"`
    ItemQueue     []string   `json:"item_queue"`  // 待拍 item ID，按上架顺序
    CreatedAt     time.Time  `json:"created_at"`
    UpdatedAt     time.Time  `json:"updated_at"`
}
```

### MerchantRoomDTO（商家端）

```go
type MerchantRoomDTO struct {
    ID            string         `json:"id"`
    MerchantID    string         `json:"merchant_id"`
    Title         string         `json:"title"`
    Status        RoomStatus     `json:"status"`
    StatusText    string         `json:"status_text"`  // "未开播" / "直播中"
    CurrentItemID string         `json:"current_item_id,omitempty"`
    OnlineCount   int            `json:"online_count"`
    QueuedCount   int            `json:"queued_count"`  // 待拍商品数量（ZCARD）
    Actions       RoomActionsDTO `json:"actions"`
    CreatedAt     time.Time      `json:"created_at"`
    UpdatedAt     time.Time      `json:"updated_at"`
}

type RoomActionsDTO struct {
    CanStart bool `json:"can_start"` // status == idle
    CanEnd   bool `json:"can_end"`   // status == live
}
```

---

## 错误处理策略

| 操作 | Redis 失败时 |
|---|---|
| `StartRoom` → InitRoomState | 日志记录，不返回错误 |
| `EndRoom` → UpdateRoomStatus | 日志记录，不返回错误 |
| `GetRoom` → GetRoomState | 静默降级，online_count = 0 |
| `GetRoom` → GetItemQueue | 静默降级，item_queue = [] |

---

## 跨模块关系

| 模块 | 与 room 的关系 |
|---|---|
| **item 模块** | `CreateItem` 时携带 `room_id`；`PublishItem` ZADD `item_queue`；`CancelItem` ZREM `item_queue` |
| **bid 模块**（未来） | `StartItem` 成功后更新 `LiveRoom.CurrentItemID` 和 Redis `state.current_item_id`；竞拍结束后从 `item_queue` 中推进到下一件商品 |
| **WebSocket 模块**（未来） | 维护 `online_users` SET 和 `state.online_count`；订阅 room 事件并广播 |

**room_id 校验**：item 模块 `CreateItem` 时不校验 `room_id` 是否存在（room 模块尚未提供校验服务），校验逻辑待 room 模块稳定后通过 service 层跨模块调用或在 `PublishItem` 时补充。

---

## 测试

**fakeCache** 实现 `cache.Cache`，内存 map。

| 测试 | 验证点 |
|---|---|
| `TestActivateRoomRequiresMerchant` | 非商家身份返回 ErrUnauthorized |
| `TestActivateRoomIsIdempotent` | 同一商家调用两次返回相同 room_id，不创建新房间 |
| `TestStartRoomTransitionsToLive` | idle → live，MySQL 状态正确 |
| `TestStartRoomRejectsNonIdle` | live 状态再次 StartRoom 返回 ErrInvalidRequest |
| `TestEndRoomTransitionsToIdle` | live → idle，MySQL 状态正确（可再次开播）|
| `TestEndRoomRejectsNonLive` | idle 状态调用 EndRoom 返回 ErrInvalidRequest |
| `TestStartRoomInitializesRedisState` | StartRoom 后 fakeCache 中存在 room state，status = "live" |
| `TestEndRoomUpdatesRedisStatus` | EndRoom 后 fakeCache 中 status = "idle" |
| `TestGetRoomEnrichesOnlineCountFromCache` | GetRoom 返回 Redis online_count |
| `TestGetRoomReturnsItemQueue` | GetRoom 返回 item_queue 列表 |
| `TestGetRoomFallsBackWhenCacheMiss` | cache miss 时正常返回，online_count=0，item_queue=[] |

---

## 不在本次范围内

- WebSocket 连接处理（`GET /ws/v1/rooms/{room_id}`）
- 在线人数维护（WebSocket 模块负责）
- `auction:room:{room_id}:events` 事件流（WebSocket 模块负责）
- `LiveRoom.CurrentItemID` 的更新（出价模块负责）
- room_id 存在性校验（待跨模块校验机制确定后补充）
- 商家修改直播间标题（可后续添加）
