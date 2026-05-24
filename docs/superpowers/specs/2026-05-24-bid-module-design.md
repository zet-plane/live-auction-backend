# 出价模块设计文档

**日期**：2026-05-24  
**范围**：`internal/app/item/` 内扩展出价与排行榜功能  
**接口**：`POST /api/v1/items/{item_id}/bids`、`GET /api/v1/items/{item_id}/ranking`

---

## 1. 模块归属

出价功能扩展在 `internal/app/item/` 内，不新建独立模块。原因：

- 出价的核心逻辑是读写 `AuctionState`（`auction:item:{id}:state` HASH），与 item cache 高度耦合。
- 独立模块需要跨模块依赖 item cache，反而增加边界复杂度。
- 路由前缀 `/api/v1/items/{item_id}/` 与 item 模块一致。

新增文件，不修改现有文件逻辑：

```
item/
  model/bid_log.go          ← BidLog GORM model
  dao/bid_log.go            ← Store 接口扩展 + GormStore 实现
  cache/bid.go              ← PlaceBidLua、GetRanking、bidder_names 操作
  service/bid_service.go    ← PlaceBid、GetRanking 业务逻辑
  handler/bid.go            ← HTTP handler
  router/item.go            ← 追加路由（现有文件）
  init.go                   ← PreInit 加入 AutoMigrateBidLog（现有文件）
```

---

## 2. Redis Key 设计

| Key | 类型 | 用途 |
|-----|------|------|
| `auction:item:{item_id}:state` | HASH | 竞拍实时状态（已有）|
| `auction:item:{item_id}:ranking` | ZSET | member=user_id，score=最高出价 |
| `auction:item:{item_id}:bidder_names` | HASH | user_id → 昵称，供排行榜展示 |
| `auction:item:{item_id}:idempotency:{key}` | STRING | 幂等控制，TTL 24h |

---

## 3. 数据模型

### BidLog

```go
type BidLog struct {
    ID        string    `gorm:"primaryKey;size:64"`
    ItemID    string    `gorm:"index;size:64;not null"`
    RoomID    string    `gorm:"index;size:64;not null"`
    UserID    string    `gorm:"index;size:64;not null"`
    Price     int64     `gorm:"not null"`
    CreatedAt time.Time
}
```

`RoomID` 冗余存储，避免 WebSocket 广播时回查 item 表，同时支持房间维度查询。

### DAO 接口扩展

```go
// 追加到现有 dao.Store interface
AutoMigrateBidLog() error
CreateBidLog(log *model.BidLog) error
```

### Cache 接口扩展

```go
// 追加到现有 cache.Cache interface
PlaceBidLua(ctx context.Context, itemID string, args BidLuaArgs) (*BidLuaResult, error)
GetRanking(ctx context.Context, itemID string, offset, limit int) ([]RankingEntry, error)

type BidLuaArgs struct {
    UserID           string
    UserName         string
    BidID            string
    Price            int64
    BidIncrement     int64
    PriceCap         int64
    ExtendTriggerSec int
    AutoExtendSec    int
    MaxExtendCount   int
    MaxTotalExtendSec int
    NowUnix          int64
    IdempotencyKey   string
    IdempotencyTTL   int // 秒，固定 86400
}

type BidLuaResult struct {
    Code         int   // 0=成功 1=幂等 2=已结束 3=出价不足 4=幅度不符
    CurrentPrice int64
    LeaderUserID string
    EndTimeUnix  int64
    IsExtended   bool
    IsCapped     bool
}
```

---

## 4. DTO

### 出价

```go
type PlaceBidRequest struct {
    Price          int64  `json:"price" binding:"required,min=1"`
    IdempotencyKey string `json:"idempotency_key" binding:"required"`
}

type PlaceBidInput struct {
    Price          int64
    IdempotencyKey string
    UserName       string // 来自 current user，传给 Lua 写 bidder_names
}

type PlaceBidResult struct {
    BidID        string    `json:"bid_id"`
    CurrentPrice int64     `json:"current_price"`
    LeaderUserID string    `json:"leader_user_id"`
    EndTime      time.Time `json:"end_time"`
    Status       string    `json:"status"` // "ongoing" | "ended"
}
```

### 排行榜

```go
type RankingEntry struct {
    Rank     int    `json:"rank"`
    UserID   string `json:"user_id"`
    UserName string `json:"user_name"`
    Price    int64  `json:"price"`
}

type RankingResult struct {
    List     []RankingEntry `json:"list"`
    Page     int            `json:"page"`
    PageSize int            `json:"page_size"`
}
```

---

## 5. 出价主流程

```
POST /api/v1/items/{item_id}/bids
  │
  ├─ 1. Handler 绑定请求体，从 flamego DI 取 *usermodel.User
  │
  ├─ 2. Service 从 MySQL 查 item + rule
  │      校验：item.Status == ongoing
  │      校验：Redis 中存在 auction state
  │
  ├─ 3. Cache.PlaceBidLua（原子，单次 RTT）
  │      ├─ 检查 idempotency key → 已存在返回码 1
  │      ├─ 读 state HASH
  │      ├─ now >= end_time → 返回码 2
  │      ├─ price <= current_price → 返回码 3
  │      ├─ (price - current_price) % bid_increment != 0 → 返回码 4
  │      ├─ ZSCORE ranking 判断是否新用户 → 更新 participant_count
  │      ├─ ZADD ranking GT price user_id
  │      ├─ HSET bidder_names user_id name
  │      ├─ 更新 state：current_price、leader_user_id、bid_count++
  │      ├─ 判断自动延时：remaining <= extend_trigger_sec 且未超上限
  │      │     → end_time += auto_extend_sec，extend_count++，is_extended=1
  │      ├─ SET idempotency key EX 86400
  │      └─ 返回：{new_price, new_end_time_unix, is_extended, is_capped}
  │
  ├─ 4. Service 写 BidLog 到 MySQL（同步）
  │      // TODO: 高并发场景下改为异步落库（写入 Redis LIST，worker 批量消费）
  │
  ├─ 5. 若 is_capped：
  │      更新 MySQL item.Status=ended、WinnerID、DealPrice
  │      // TODO: 广播 auction_ended WebSocket 事件（WS 模块实现后补入）
  │
  └─ 6. 返回 PlaceBidResult
```

---

## 6. Lua 返回码

| 返回码 | 含义 | Go 处理 |
|--------|------|---------|
| `0` | 成功 | 正常流程 |
| `1` | idempotency key 已使用 | 幂等返回，不写 BidLog |
| `2` | 竞拍已结束（end_time 已过）| `errorx.New(400, 40002, "auction has ended")` |
| `3` | 出价不足当前最高价 | `errorx.New(400, 40003, "price too low")` |
| `4` | 加价幅度不符 | `errorx.New(400, 40004, "invalid bid increment")` |

---

## 7. GetRanking 流程

```
GET /api/v1/items/{item_id}/ranking?page=1&page_size=10
  │
  ├─ 1. Cache.GetRanking：ZREVRANGE ranking offset limit WITHSCORES
  │      + HMGET bidder_names user_ids
  │
  ├─ 2. Redis 不存在（竞拍未开始或 Redis 故障）→ fallback MySQL：
  │      SELECT user_id, MAX(price) FROM bid_logs
  │      WHERE item_id = ? GROUP BY user_id
  │      ORDER BY MAX(price) DESC LIMIT ?
  │      + JOIN users 表获取昵称
  │
  └─ 3. 返回 RankingResult，rank 从 1 开始递增
```

---

## 8. 前置校验汇总

| 层 | 校验项 | 错误 |
|----|--------|------|
| 路由中间件 | JWT 认证 | 401 |
| Service | item 不存在 | ErrNotFound |
| Service | item.Status != ongoing | ErrInvalidRequest |
| Service | Redis 无 auction state | ErrInvalidRequest |
| Lua | end_time 已过 | 400 40002 |
| Lua | price <= current_price | 400 40003 |
| Lua | 加价幅度不符 | 400 40004 |
| Lua | idempotency key 重复 | 幂等 200 |

---

## 9. 测试策略

沿用现有 `fakeStore` 模式（`service_test.go`）：

- 新增 `fakeBidCache`，实现 `cache.Cache` 接口中的出价方法，在内存中模拟 Lua 逻辑。
- 不依赖真实 Redis，测试以下场景：
  - 正常出价成功
  - 幂等键重复
  - 出价不足
  - 加价幅度不符
  - 竞拍结束后出价
  - 自动延时触发
  - 封顶价成交
  - GetRanking 分页
