# Item 模块 Redis 实时状态设计

**日期**: 2026-05-23  
**作者**: Claude Code  
**范围**: `internal/app/item/`

---

## 背景

Item 模块目前仅依赖 MySQL，存在两个缺口：

1. DTO 中的实时字段（`current_price`、`leader_user_id`、`bid_count` 等）以 MySQL 静态值填充，无法反映竞拍动态变化
2. C 端待竞拍列表没有 `room_id`，消费者无法知道应该加入哪个直播间参与竞拍

本设计在 Item 模块引入 Redis 层，覆盖：
- 竞拍状态（`auction:item:{item_id}:state`）的初始化与读取
- 直播间商品队列（`auction:room:{room_id}:item_queue`）的写入与清理
- `AuctionItem` model 增加 `room_id` 字段

---

## 数据模型变更

### AuctionItem 新增字段

```go
type AuctionItem struct {
    // ... 现有字段 ...
    RoomID string `gorm:"index;size:64;not null" json:"room_id"`  // 新增
}
```

**迁移**：`PreInit` 中的 `store.AutoMigrate()` 会自动加列。`room_id` 为非空，但不外键约束（room 模块尚未建立，不做 FK 校验）。

### CreateItemInput / CreateItemRequest 新增字段

```go
type CreateItemInput struct {
    RoomID      string   // 新增
    Title       string
    // ...
}

type CreateItemRequest struct {
    RoomID      string   `json:"room_id"  binding:"required,min=1,max=64"`  // 新增
    Title       string   `json:"title"    binding:"required,min=1,max=128"`
    // ...
}
```

### ItemListDTO 新增字段

```go
type ItemListDTO struct {
    RoomID string `json:"room_id"`  // 新增，C 端据此跳转直播间
    // ... 现有字段 ...
}
```

`MerchantItemDTO` 已有 `RoomID` 字段，不变。

---

## Redis 数据结构

### 竞拍状态：`auction:item:{item_id}:state`

**Type**: HASH  

| Field | 说明 |
|---|---|
| `current_price` | 当前最高价（分），初始 = `start_price` |
| `leader_user_id` | 当前领先用户，初始 = "" |
| `end_time_unix` | 实际结束时间（Unix 秒），含延时 |
| `bid_count` | 出价次数，初始 = 0 |
| `participant_count` | 参与人数，初始 = 0 |
| `is_extended` | 是否已延时，"1"/"0"，初始 = "0" |
| `extend_count` | 已延时次数，初始 = 0 |
| `total_extended_sec` | 累计延时秒数，初始 = 0 |

`extend_count` / `total_extended_sec` 由出价模块写入，Item 模块只初始化，读取但不修改。

**TTL**：不设 TTL。正常竞拍结束由出价模块负责 DEL；取消时由 Item 模块 `DeleteAuctionState` 清除。

### 直播间商品队列：`auction:room:{room_id}:item_queue`

**Type**: ZSET  
**Score**: publish_time（Unix 秒），保证按上架顺序排列  
**Member**: `item_id`

上架（`PublishItem`）→ ZADD；取消（`CancelItem`）→ ZREM。  
竞拍开始（`StartItem`）不操作此 key，出价模块通过 `current_item` 管理当前正在拍的商品。

---

## 架构

### 新增文件

**`internal/app/item/cache/cache.go`**

```
AuctionState struct
  CurrentPrice, LeaderUserID, EndTime, BidCount,
  ParticipantCount, IsExtended, ExtendCount, TotalExtendedSec

Cache interface
  // 竞拍状态
  InitAuctionState(ctx, itemID string, state AuctionState) error
  GetAuctionState(ctx, itemID string) (*AuctionState, bool, error)
  DeleteAuctionState(ctx, itemID string) error
  // 直播间队列
  PushToRoomQueue(ctx, roomID, itemID string, score float64) error
  RemoveFromRoomQueue(ctx, roomID, itemID string) error

RedisCache struct { client *redis.Client }
  — HSET      on InitAuctionState
  — HGETALL   on GetAuctionState（key 不存在返回 false, nil）
  — DEL       on DeleteAuctionState
  — ZADD      on PushToRoomQueue
  — ZREM      on RemoveFromRoomQueue
```

### 修改文件

| 文件 | 变更 |
|---|---|
| `model/item.go` | `AuctionItem` 加 `RoomID string` |
| `dto/item.go` | `CreateItemInput` / `CreateItemRequest` 加 `RoomID`；`ItemListDTO` 加 `RoomID`；`NewItemListDTO` 传入并填充 `RoomID` |
| `service/service.go` | 加 `cache cache.Cache` 字段；`NewService` 签名变更；`CreateItem`、`PublishItem`、`StartItem`、`CancelItem`、`GetItem`、`ListItems`、`ListMerchantItems` 按下文流程更新 |
| `init.go` | `Load()` 构建 `cache.NewRedisCache(engine.Cache)` 并传入 `NewService` |
| `service/service_test.go` | 加 `fakeCache`；更新 `NewService` 调用；补充相关测试 |

---

## 关键流程

### CreateItem

在现有逻辑基础上，将 `input.RoomID` 写入 `AuctionItem.RoomID`。无 Redis 操作（商品创建时只是草稿）。

### PublishItem — MySQL 优先，Redis 软失败

```
1. findMerchantItem → 验证归属 + draft 状态
2. item.Status = published; store.UpdateItemWithRule(item, rule)
   → 失败: return error
3. cache.PushToRoomQueue(item.RoomID, item.ID, now().Unix())
   → 失败: 日志记录，不返回错误（MySQL 是 item_queue 的源数据，可从 MySQL 重建）
4. return nil
```

`PublishItem` 继续复用 `transition()` helper 完成状态流转，Redis 写入在 transition 成功后追加。实际实现中需要拿到 `item.RoomID`，所以改为先 `findMerchantItem`，再手动调 `store.UpdateItemWithRule`，和 `StartItem` 处理方式相同。

### StartItem — Redis 优先，强一致

```
1. findMerchantItem → 验证归属 + published 状态
2. cache.InitAuctionState(item.ID, {CurrentPrice: rule.StartPrice, EndTime: rule.EndTime})
   → 失败: return error（MySQL 未改，状态干净）
3. item.Status = ongoing; store.UpdateItemWithRule(item, rule)
   → 失败: cache.DeleteAuctionState(item.ID)（回滚 Redis）; return error
4. return nil
```

`StartItem` 不操作 `item_queue`（队列管理由出价模块负责）。

### CancelItem — MySQL 优先，Redis 双清理

```
1. findMerchantItem → 验证归属 + 状态（published 或 ongoing）
2. item.Status = cancelled; store.UpdateItemWithRule(item, rule)
   → 失败: return error
3. cache.RemoveFromRoomQueue(item.RoomID, item.ID)  — 静默失败
4. cache.DeleteAuctionState(item.ID)                — 静默失败（ongoing 时才有）
5. return nil
```

### GetItem — Redis 富化，降级静默

```
1. store.FindItemWithRule(itemID)
2. dto.NewItemDetailDTO(item, rule, policy, now)   ← MySQL 默认值
3. if item.Status == ongoing:
     state, ok, _ = cache.GetAuctionState(itemID)
     if ok: applyStateToDetail(&dto, state, now)
4. return dto
```

### ListItems / ListMerchantItems

对结果集中每条 `ongoing` 商品调用 `cache.GetAuctionState`，失败或未命中则保留 MySQL 值。

---

## DTO 富化规则

仅当 `status == ongoing` 且 `state != nil` 时调用，各 DTO 覆盖字段：

**ItemDetailDTO / MerchantItemDTO.Progress**

| 字段 | 来源 |
|---|---|
| `CurrentPrice` | `state.CurrentPrice` |
| `LeaderUserID` | `state.LeaderUserID` |
| `BidCount` | `state.BidCount` |
| `ParticipantCount` | `state.ParticipantCount` |
| `RemainingMS` | `max(0, state.EndTime.Sub(now).Milliseconds())` |
| `IsExtended` | `state.IsExtended` |

**ItemListDTO**（无 `LeaderUserID` / `IsExtended`）

| 字段 | 来源 |
|---|---|
| `CurrentPrice` | `state.CurrentPrice` |
| `BidCount` | `state.BidCount` |
| `ParticipantCount` | `state.ParticipantCount` |
| `RemainingMS` | `max(0, state.EndTime.Sub(now).Milliseconds())` |

---

## 错误处理策略

| 操作 | Redis 失败时 |
|---|---|
| `StartItem` → InitAuctionState | 返回错误，MySQL 不更新 |
| `StartItem` → MySQL 更新失败 | DeleteAuctionState 回滚，返回错误 |
| `PublishItem` → PushToRoomQueue | 日志记录，不返回错误 |
| `CancelItem` → RemoveFromRoomQueue | 日志记录，不返回错误 |
| `CancelItem` → DeleteAuctionState | 日志记录，不返回错误 |
| `GetItem` / `ListItems` → GetAuctionState | 静默降级，返回 MySQL 数据 |

---

## 测试

**fakeCache**：内存 map 实现 `cache.Cache`，含 `states map[string]*AuctionState` 和 `queues map[string][]string`。

**新增 service 测试**

| 测试 | 验证点 |
|---|---|
| `TestCreateItemStoresRoomID` | 创建后 store 中 item.RoomID 正确 |
| `TestPublishItemPushesToRoomQueue` | PublishItem 后 fakeCache.queues 中有该 item |
| `TestCancelItemRemovesFromRoomQueueAndState` | Cancel 后队列和 state 均被清除 |
| `TestStartItemInitializesRedisState` | StartItem 后 fakeCache 中 current_price = start_price，end_time 正确 |
| `TestStartItemFailsWhenRedisInitFails` | Redis init 失败时 MySQL 状态不变（仍为 published）|
| `TestStartItemRollsBackRedisOnMySQLFailure` | MySQL 失败时 fakeCache 中 state 被删除 |
| `TestGetItemEnrichesFromCacheWhenOngoing` | ongoing 商品 GetItem 返回 Redis current_price |
| `TestGetItemFallsBackToMySQLWhenCacheMiss` | cache miss 时正常返回 MySQL 数据，不报错 |

**现有测试**：`NewService` 调用补充 `nil` 作为 cache 参数，行为不变。

---

## 不在本次范围内

- LiveRoom 模块（room 的创建、状态管理）——参见 2026-05-23-room-module-design.md
- `room_id` 存在性校验：item 模块 `CreateItem` 时传入 `room_id`，暂不校验其是否存在；`PublishItem` 时可通过 room 模块 `FindRoomByMerchantID` 验证该商家已开通直播间且 `item.RoomID` 匹配，待 room 模块稳定后补充
- `auction:room:{room_id}:current_item` 的管理（出价模块负责）
- `auction:item:{item_id}:ranking` Sorted Set（出价模块负责）
- 出价幂等键（出价模块负责）
- WebSocket 广播
