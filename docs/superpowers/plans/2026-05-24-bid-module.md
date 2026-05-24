# 出价模块实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/app/item/` 内扩展出价与排行榜功能，实现 `POST /api/v1/items/{item_id}/bids` 和 `GET /api/v1/items/{item_id}/ranking`。

**Architecture:** 出价并发控制通过 Redis Lua 脚本原子执行（校验 + 更新一次 RTT 完成）；BidLog 同步写入 MySQL（留 TODO 标注后续异步落库）；排行榜优先读 Redis ZSET，失败降级到 MySQL 聚合查询；幂等键存 Redis STRING（TTL 24h）防止重复出价。

**Tech Stack:** Go 1.23, Redis (`github.com/redis/go-redis/v9`), GORM (`gorm.io/gorm`), flamego

---

## 文件清单

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/app/item/model/bid_log.go` | BidLog GORM model |
| 修改 | `internal/app/item/dao/item.go` | Store 接口扩展（BidLog + Ranking）|
| 新建 | `internal/app/item/dao/bid_log.go` | GormStore BidLog 实现 |
| 修改 | `internal/app/item/cache/cache.go` | Cache 接口扩展 + 新增类型 |
| 新建 | `internal/app/item/cache/bid.go` | RedisCache Lua + GetRanking 实现 |
| 新建 | `internal/app/item/dto/bid.go` | 出价与排行榜 DTO |
| 新建 | `internal/app/item/service/bid_service.go` | PlaceBid、GetRanking 业务逻辑 |
| 新建 | `internal/app/item/service/bid_service_test.go` | 出价与排行榜单元测试 |
| 修改 | `internal/app/item/service/service_test.go` | fakeStore/fakeCache 实现新接口方法 |
| 新建 | `internal/app/item/handler/bid.go` | HTTP handler |
| 修改 | `internal/app/item/router/item.go` | 注册出价与排行榜路由 |
| 修改 | `internal/app/item/init.go` | PreInit 加入 AutoMigrateBidLog |

---

> **执行顺序说明：** Task 4（DTOs）定义了 `dto.BidderPrice`，Store 接口（Task 2）和 Cache 接口（Task 3）都依赖它。**必须先完成 Task 4，再执行 Task 2 和 Task 3。** 推荐顺序：Task 1 → Task 4 → Task 2 → Task 3 → Task 5 → Task 6 → Task 7 → Task 8。

---

## Task 1: BidLog model

**Files:**
- Create: `internal/app/item/model/bid_log.go`

- [ ] **Step 1: 创建 BidLog model**

```go
package model

import "time"

type BidLog struct {
	ID        string    `gorm:"primaryKey;size:64"`
	ItemID    string    `gorm:"index;size:64;not null"`
	RoomID    string    `gorm:"index;size:64;not null"`
	UserID    string    `gorm:"index;size:64;not null"`
	Price     int64     `gorm:"not null"`
	CreatedAt time.Time
}
```

- [ ] **Step 2: 确认编译通过**

```bash
go build ./internal/app/item/...
```

期望：无报错。

- [ ] **Step 3: Commit**

```bash
git add internal/app/item/model/bid_log.go
git commit -m "feat(bid): add BidLog model"
```

---

## Task 2: Store 接口扩展 + GormStore 实现 + fakeStore 更新 + init.go

**Files:**
- Modify: `internal/app/item/dao/item.go`
- Create: `internal/app/item/dao/bid_log.go`
- Modify: `internal/app/item/service/service_test.go`
- Modify: `internal/app/item/init.go`

- [ ] **Step 1: 扩展 Store 接口**

在 `internal/app/item/dao/item.go` 的 `Store` interface 末尾追加：

```go
AutoMigrateBidLog() error
CreateBidLog(log *model.BidLog) error
ListBidRanking(itemID string, limit int) ([]dto.BidderPrice, error)
```

注意：`dto.BidderPrice` 在 Task 4 创建，但现在需要先声明接口。在 Task 4 之前，可以先写 `// TODO` 占位，或直接按下面 Task 4 的内容在 dto/bid.go 创建空的 BidderPrice struct 再回来。建议**先完成 Task 4 的 `BidderPrice` struct**，再执行此 Step。

**推荐顺序：先完成 Task 4 Step 1（仅创建 BidderPrice），再回到此步骤。**

- [ ] **Step 2: 创建 GormStore BidLog 实现**

新建 `internal/app/item/dao/bid_log.go`：

```go
package dao

import (
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

func (s *GormStore) AutoMigrateBidLog() error {
	return s.db.AutoMigrate(&model.BidLog{})
}

func (s *GormStore) CreateBidLog(log *model.BidLog) error {
	return s.db.Create(log).Error
}

func (s *GormStore) ListBidRanking(itemID string, limit int) ([]dto.BidderPrice, error) {
	var rows []struct {
		UserID   string
		Price    int64
		UserName string
	}
	err := s.db.Table("bid_logs b").
		Select("b.user_id, MAX(b.price) as price, u.name as user_name").
		Joins("LEFT JOIN users u ON b.user_id = u.id").
		Where("b.item_id = ? AND b.deleted_at IS NULL", itemID).
		Group("b.user_id, u.name").
		Order("price DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	entries := make([]dto.BidderPrice, len(rows))
	for i, r := range rows {
		entries[i] = dto.BidderPrice{
			UserID:   r.UserID,
			UserName: r.UserName,
			Price:    r.Price,
		}
	}
	return entries, nil
}
```

- [ ] **Step 3: 更新 fakeStore 实现新接口方法**

在 `internal/app/item/service/service_test.go` 的 `fakeStore` struct 中添加字段，并添加三个新方法：

在 `fakeStore` struct 中添加字段：
```go
bidLogs []*itemmodel.BidLog
```

在 `fakeStore` 的方法列表中添加：
```go
func (s *fakeStore) AutoMigrateBidLog() error { return nil }

func (s *fakeStore) CreateBidLog(log *itemmodel.BidLog) error {
	cp := *log
	s.bidLogs = append(s.bidLogs, &cp)
	return nil
}

func (s *fakeStore) ListBidRanking(itemID string, limit int) ([]itemdto.BidderPrice, error) {
	best := map[string]int64{}
	for _, l := range s.bidLogs {
		if l.ItemID != itemID {
			continue
		}
		if l.Price > best[l.UserID] {
			best[l.UserID] = l.Price
		}
	}
	entries := make([]itemdto.BidderPrice, 0, len(best))
	for uid, price := range best {
		entries = append(entries, itemdto.BidderPrice{UserID: uid, Price: price})
	}
	// sort descending
	sort.Slice(entries, func(i, j int) bool { return entries[i].Price > entries[j].Price })
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}
```

还需要在文件顶部 import 中加入 `"sort"`。

- [ ] **Step 4: 更新 init.go 加入 AutoMigrateBidLog**

在 `internal/app/item/init.go` 的 `PreInit` 方法中追加：

```go
func (i *Item) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return ErrEmptyDatabase
	}
	store := dao.NewGormStore(engine.DB)
	if err := store.AutoMigrate(); err != nil {
		return err
	}
	return store.AutoMigrateBidLog()
}
```

- [ ] **Step 5: 确认现有测试仍然通过**

```bash
go test ./internal/app/item/...
```

期望：所有已有测试通过，无编译错误。

- [ ] **Step 6: Commit**

```bash
git add internal/app/item/dao/item.go internal/app/item/dao/bid_log.go \
        internal/app/item/service/service_test.go internal/app/item/init.go
git commit -m "feat(bid): extend Store interface with BidLog and Ranking methods"
```

---

## Task 3: Cache 接口扩展 + RedisCache 实现 + fakeCache 更新

**Files:**
- Modify: `internal/app/item/cache/cache.go`
- Create: `internal/app/item/cache/bid.go`
- Modify: `internal/app/item/service/service_test.go`

- [ ] **Step 1: 扩展 Cache 接口，新增类型**

在 `internal/app/item/cache/cache.go` 中：

1. 在文件顶部 import 加入 `"github.com/zet-plane/live-auction-backend/internal/app/item/dto"`（dto 在 Task 4 创建，与 Task 2 同理，先完成 Task 4 的 BidderPrice 后再做此步）。

2. 追加以下类型定义（在 `AuctionState` 之后）：

```go
type BidLuaArgs struct {
	UserID            string
	UserName          string
	BidID             string
	Price             int64
	BidIncrement      int64
	PriceCap          int64
	ExtendTriggerSec  int
	AutoExtendSec     int
	MaxExtendCount    int
	MaxTotalExtendSec int
	NowUnix           int64
	IdempotencyKey    string
	IdempotencyTTL    int
}

type BidLuaResult struct {
	Code         int
	BidID        string
	CurrentPrice int64
	LeaderUserID string
	EndTimeUnix  int64
	IsExtended   bool
	IsCapped     bool
}
```

3. 在 `Cache` interface 末尾追加：

```go
PlaceBidLua(ctx context.Context, itemID string, args BidLuaArgs) (*BidLuaResult, error)
GetRanking(ctx context.Context, itemID string, offset, limit int) ([]dto.BidderPrice, error)
```

- [ ] **Step 2: 创建 RedisCache Lua 实现**

新建 `internal/app/item/cache/bid.go`：

```go
package cache

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
)

const bidLuaScript = `
local state_key   = KEYS[1]
local ranking_key = KEYS[2]
local names_key   = KEYS[3]
local idem_key    = KEYS[4]

local user_id     = ARGV[1]
local user_name   = ARGV[2]
local bid_id      = ARGV[3]
local price       = tonumber(ARGV[4])
local bid_incr    = tonumber(ARGV[5])
local price_cap   = tonumber(ARGV[6])
local ext_trig    = tonumber(ARGV[7])
local ext_sec     = tonumber(ARGV[8])
local max_ext_cnt = tonumber(ARGV[9])
local max_ext_tot = tonumber(ARGV[10])
local now_unix    = tonumber(ARGV[11])
local idem_ttl    = tonumber(ARGV[12])

local existing = redis.call('GET', idem_key)
if existing then
  local raw = redis.call('HGETALL', state_key)
  local m = {}
  for i = 1, #raw, 2 do m[raw[i]] = raw[i+1] end
  return {1, existing, tonumber(m['current_price'] or 0), m['leader_user_id'] or '', tonumber(m['end_time_unix'] or 0), 0, 0}
end

local raw = redis.call('HGETALL', state_key)
if #raw == 0 then return {2,'',0,'',0,0,0} end
local s = {}
for i = 1, #raw, 2 do s[raw[i]] = raw[i+1] end

local cur_price = tonumber(s['current_price'] or 0)
local end_unix  = tonumber(s['end_time_unix']  or 0)
local ext_cnt   = tonumber(s['extend_count']   or 0)
local ext_tot   = tonumber(s['total_extended_sec'] or 0)
local bid_cnt   = tonumber(s['bid_count']       or 0)
local part_cnt  = tonumber(s['participant_count'] or 0)

if now_unix >= end_unix then return {2,'',0,'',0,0,0} end
if price <= cur_price   then return {3,'',0,'',0,0,0} end
if (price - cur_price) % bid_incr ~= 0 then return {4,'',0,'',0,0,0} end

local prev_score = redis.call('ZSCORE', ranking_key, user_id)
if not prev_score then part_cnt = part_cnt + 1 end

redis.call('ZADD', ranking_key, 'GT', price, user_id)
redis.call('HSET', names_key, user_id, user_name)
bid_cnt = bid_cnt + 1

local is_extended = 0
local remaining = end_unix - now_unix
if remaining <= ext_trig and ext_cnt < max_ext_cnt and (ext_tot + ext_sec) <= max_ext_tot then
  end_unix  = end_unix  + ext_sec
  ext_cnt   = ext_cnt   + 1
  ext_tot   = ext_tot   + ext_sec
  is_extended = 1
end

redis.call('HSET', state_key,
  'current_price',      price,
  'leader_user_id',     user_id,
  'end_time_unix',      end_unix,
  'bid_count',          bid_cnt,
  'participant_count',  part_cnt,
  'is_extended',        is_extended,
  'extend_count',       ext_cnt,
  'total_extended_sec', ext_tot)

redis.call('SET', idem_key, bid_id, 'EX', idem_ttl)

local is_capped = 0
if price_cap > 0 and price >= price_cap then is_capped = 1 end

return {0, bid_id, price, user_id, end_unix, is_extended, is_capped}
`

func rankingKey(itemID string) string {
	return "auction:item:" + itemID + ":ranking"
}

func bidderNamesKey(itemID string) string {
	return "auction:item:" + itemID + ":bidder_names"
}

func idempotencyKey(itemID, key string) string {
	return "auction:item:" + itemID + ":idempotency:" + key
}

func (c *RedisCache) PlaceBidLua(ctx context.Context, itemID string, args BidLuaArgs) (*BidLuaResult, error) {
	keys := []string{
		itemStateKey(itemID),
		rankingKey(itemID),
		bidderNamesKey(itemID),
		idempotencyKey(itemID, args.IdempotencyKey),
	}
	argv := []interface{}{
		args.UserID,
		args.UserName,
		args.BidID,
		strconv.FormatInt(args.Price, 10),
		strconv.FormatInt(args.BidIncrement, 10),
		strconv.FormatInt(args.PriceCap, 10),
		strconv.Itoa(args.ExtendTriggerSec),
		strconv.Itoa(args.AutoExtendSec),
		strconv.Itoa(args.MaxExtendCount),
		strconv.Itoa(args.MaxTotalExtendSec),
		strconv.FormatInt(args.NowUnix, 10),
		strconv.Itoa(args.IdempotencyTTL),
	}

	res, err := redis.NewScript(bidLuaScript).Run(ctx, c.client, keys, argv...).Slice()
	if err != nil {
		return nil, err
	}
	if len(res) < 7 {
		return nil, fmt.Errorf("lua result length unexpected: %d", len(res))
	}

	toI64 := func(v interface{}) int64 { n, _ := v.(int64); return n }
	toStr := func(v interface{}) string { s, _ := v.(string); return s }

	return &BidLuaResult{
		Code:         int(toI64(res[0])),
		BidID:        toStr(res[1]),
		CurrentPrice: toI64(res[2]),
		LeaderUserID: toStr(res[3]),
		EndTimeUnix:  toI64(res[4]),
		IsExtended:   toI64(res[5]) == 1,
		IsCapped:     toI64(res[6]) == 1,
	}, nil
}

func (c *RedisCache) GetRanking(ctx context.Context, itemID string, offset, limit int) ([]dto.BidderPrice, error) {
	members, err := c.client.ZRevRangeWithScores(ctx, rankingKey(itemID), int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}
	userIDs := make([]string, len(members))
	for i, m := range members {
		userIDs[i] = m.Member.(string)
	}
	names, err := c.client.HMGet(ctx, bidderNamesKey(itemID), userIDs...).Result()
	if err != nil {
		return nil, err
	}
	entries := make([]dto.BidderPrice, len(members))
	for i, m := range members {
		name := ""
		if names[i] != nil {
			name, _ = names[i].(string)
		}
		entries[i] = dto.BidderPrice{
			UserID:   userIDs[i],
			UserName: name,
			Price:    int64(m.Score),
		}
	}
	return entries, nil
}
```

- [ ] **Step 3: 更新 fakeCache 实现新接口方法**

在 `internal/app/item/service/service_test.go` 中：

1. 在 `fakeCache` struct 中添加字段：

```go
ranking     map[string]map[string]int64  // itemID -> userID -> highestPrice
bidderNames map[string]map[string]string // itemID -> userID -> name
bidLuaCode  int   // 非 0 时强制返回此错误码，用于测试失败场景
bidLuaErr   error
```

2. 在 `newFakeCache()` 中初始化新字段：

```go
func newFakeCache() *fakeCache {
	return &fakeCache{
		states:     map[string]*itemcache.AuctionState{},
		queues:     map[string][]string{},
		ranking:    map[string]map[string]int64{},
		bidderNames: map[string]map[string]string{},
	}
}
```

3. 添加 `PlaceBidLua` 方法（模拟 Lua 逻辑，供测试使用）：

```go
func (c *fakeCache) PlaceBidLua(_ context.Context, itemID string, args itemcache.BidLuaArgs) (*itemcache.BidLuaResult, error) {
	if c.bidLuaErr != nil {
		return nil, c.bidLuaErr
	}
	if c.bidLuaCode != 0 {
		return &itemcache.BidLuaResult{Code: c.bidLuaCode}, nil
	}
	state, ok := c.states[itemID]
	if !ok {
		return &itemcache.BidLuaResult{Code: 2}, nil
	}
	if args.NowUnix >= state.EndTime.Unix() {
		return &itemcache.BidLuaResult{Code: 2}, nil
	}
	if args.Price <= state.CurrentPrice {
		return &itemcache.BidLuaResult{Code: 3}, nil
	}
	if args.BidIncrement > 0 && (args.Price-state.CurrentPrice)%args.BidIncrement != 0 {
		return &itemcache.BidLuaResult{Code: 4}, nil
	}
	if c.ranking[itemID] == nil {
		c.ranking[itemID] = map[string]int64{}
	}
	if _, exists := c.ranking[itemID][args.UserID]; !exists {
		state.ParticipantCount++
	}
	if args.Price > c.ranking[itemID][args.UserID] {
		c.ranking[itemID][args.UserID] = args.Price
	}
	if c.bidderNames[itemID] == nil {
		c.bidderNames[itemID] = map[string]string{}
	}
	c.bidderNames[itemID][args.UserID] = args.UserName
	state.CurrentPrice = args.Price
	state.LeaderUserID = args.UserID
	state.BidCount++

	isExtended := false
	remaining := state.EndTime.Unix() - args.NowUnix
	if remaining <= int64(args.ExtendTriggerSec) &&
		state.ExtendCount < args.MaxExtendCount &&
		state.TotalExtendedSec+args.AutoExtendSec <= args.MaxTotalExtendSec {
		state.EndTime = state.EndTime.Add(time.Duration(args.AutoExtendSec) * time.Second)
		state.ExtendCount++
		state.TotalExtendedSec += args.AutoExtendSec
		state.IsExtended = true
		isExtended = true
	}

	isCapped := args.PriceCap > 0 && args.Price >= args.PriceCap
	return &itemcache.BidLuaResult{
		Code:         0,
		BidID:        args.BidID,
		CurrentPrice: args.Price,
		LeaderUserID: args.UserID,
		EndTimeUnix:  state.EndTime.Unix(),
		IsExtended:   isExtended,
		IsCapped:     isCapped,
	}, nil
}
```

4. 添加 `GetRanking` 方法：

```go
func (c *fakeCache) GetRanking(_ context.Context, itemID string, offset, limit int) ([]itemdto.BidderPrice, error) {
	m := c.ranking[itemID]
	if len(m) == 0 {
		return nil, nil
	}
	entries := make([]itemdto.BidderPrice, 0, len(m))
	for uid, price := range m {
		name := ""
		if c.bidderNames[itemID] != nil {
			name = c.bidderNames[itemID][uid]
		}
		entries = append(entries, itemdto.BidderPrice{UserID: uid, UserName: name, Price: price})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Price > entries[j].Price })
	if offset >= len(entries) {
		return nil, nil
	}
	entries = entries[offset:]
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}
```

- [ ] **Step 4: 确认现有测试仍然通过**

```bash
go test ./internal/app/item/...
```

期望：所有已有测试通过。

- [ ] **Step 5: Commit**

```bash
git add internal/app/item/cache/cache.go internal/app/item/cache/bid.go \
        internal/app/item/service/service_test.go
git commit -m "feat(bid): extend Cache interface with PlaceBidLua and GetRanking"
```

---

## Task 4: Bid DTOs

**Files:**
- Create: `internal/app/item/dto/bid.go`

- [ ] **Step 1: 创建 bid.go**

```go
package dto

import "time"

// BidderPrice 供 cache.GetRanking 和 dao.ListBidRanking 返回，不含 Rank。
type BidderPrice struct {
	UserID   string
	UserName string
	Price    int64
}

type PlaceBidRequest struct {
	Price          int64  `json:"price"           binding:"required,min=1"`
	IdempotencyKey string `json:"idempotency_key" binding:"required,min=1,max=128"`
}

type PlaceBidInput struct {
	Price          int64
	IdempotencyKey string
	UserName       string
}

func (r PlaceBidRequest) Input(userName string) PlaceBidInput {
	return PlaceBidInput{
		Price:          r.Price,
		IdempotencyKey: r.IdempotencyKey,
		UserName:       userName,
	}
}

type PlaceBidResult struct {
	BidID        string    `json:"bid_id"`
	CurrentPrice int64     `json:"current_price"`
	LeaderUserID string    `json:"leader_user_id"`
	EndTime      time.Time `json:"end_time"`
	Status       string    `json:"status"` // "ongoing" | "ended"
}

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

- [ ] **Step 2: 确认编译通过**

```bash
go build ./internal/app/item/...
```

期望：无报错。

- [ ] **Step 3: Commit**

```bash
git add internal/app/item/dto/bid.go
git commit -m "feat(bid): add bid and ranking DTOs"
```

---

## Task 5: PlaceBid service（TDD）

**Files:**
- Create: `internal/app/item/service/bid_service.go`
- Create: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/app/item/service/bid_service_test.go`：

```go
package service

import (
	"errors"
	"testing"
	"time"

	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var (
	testPolicy = itemdto.AuctionPolicy{
		ExtendTriggerSec:  30,
		AutoExtendSec:     10,
		MaxExtendCount:    6,
		MaxTotalExtendSec: 300,
	}
	bidder = &usermodel.User{ID: "user_1", Name: "Alice", Identity: usermodel.IdentityUser}
)

func seedOngoingItem(t *testing.T, svc *Service, merchantID, roomID string, startPrice, bidIncrement, priceCap int64, endTime time.Time) string {
	t.Helper()
	start := endTime.Add(-10 * time.Minute)
	result, err := svc.CreateItem(
		&usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant},
		itemdto.CreateItemInput{
			RoomID: roomID,
			Title:  "Test Item",
			Rule: itemdto.RuleInput{
				StartPrice:   startPrice,
				BidIncrement: bidIncrement,
				PriceCap:     priceCap,
				StartTime:    start,
				EndTime:      endTime,
			},
		},
	)
	if err != nil {
		t.Fatalf("CreateItem failed: %v", err)
	}
	merchant := &usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant}
	if err := svc.PublishItem(merchant, result.ItemID); err != nil {
		t.Fatalf("PublishItem failed: %v", err)
	}
	if err := svc.StartItem(merchant, result.ItemID); err != nil {
		t.Fatalf("StartItem failed: %v", err)
	}
	return result.ItemID
}

func TestPlaceBidSucceeds(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	result, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
		Price:          100,
		IdempotencyKey: "idem_001",
		UserName:       "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if result.BidID == "" {
		t.Fatal("expected non-empty bid_id")
	}
	if result.CurrentPrice != 100 {
		t.Fatalf("expected current_price 100, got %d", result.CurrentPrice)
	}
	if result.LeaderUserID != "user_1" {
		t.Fatalf("expected leader user_1, got %q", result.LeaderUserID)
	}
	if result.Status != "ongoing" {
		t.Fatalf("expected status ongoing, got %q", result.Status)
	}
	if len(store.bidLogs) != 1 {
		t.Fatalf("expected 1 bid log, got %d", len(store.bidLogs))
	}
	if store.bidLogs[0].RoomID != "room_1" {
		t.Fatalf("expected room_id room_1, got %q", store.bidLogs[0].RoomID)
	}
}

func TestPlaceBidRejectsNonOngoingItem(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_1")

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 100, IdempotencyKey: "k1"})
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request for non-ongoing item, got %v", err)
	}
}

func TestPlaceBidRejectsPriceTooLow(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.bidLuaCode = 3
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 50, IdempotencyKey: "k1"})
	if err == nil {
		t.Fatal("expected error for price too low")
	}
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40003 {
		t.Fatalf("expected code 40003, got %v", err)
	}
}

func TestPlaceBidRejectsInvalidIncrement(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.bidLuaCode = 4
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 150, IdempotencyKey: "k1"})
	if err == nil {
		t.Fatal("expected error for invalid increment")
	}
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40004 {
		t.Fatalf("expected code 40004, got %v", err)
	}
}

func TestPlaceBidRejectsEndedAuction(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.bidLuaCode = 2
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 100, IdempotencyKey: "k1"})
	if err == nil {
		t.Fatal("expected error for ended auction")
	}
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40002 {
		t.Fatalf("expected code 40002, got %v", err)
	}
}

func TestPlaceBidIdempotent(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	if _, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "idem_dup", UserName: "Alice",
	}); err != nil {
		t.Fatalf("first bid failed: %v", err)
	}
	// Force idempotency code on second call (fakeCache returns code=1, skips BidLog write)
	fc.bidLuaCode = 1
	if _, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "idem_dup", UserName: "Alice",
	}); err != nil {
		t.Fatalf("idempotent bid should not fail: %v", err)
	}
	// BidLog must not be written a second time
	count := 0
	for _, l := range store.bidLogs {
		if l.ItemID == itemID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 bid log after idempotent retry, got %d", count)
	}
}

func TestPlaceBidPriceCapEndsAuction(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 500, endTime)

	result, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 500, IdempotencyKey: "idem_cap"})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if result.Status != "ended" {
		t.Fatalf("expected status ended when price cap reached, got %q", result.Status)
	}
	item := store.items[itemID]
	if item.Status != itemmodel.ItemEnded {
		t.Fatalf("expected item status ended in MySQL, got %q", item.Status)
	}
	if item.WinnerID != "user_1" {
		t.Fatalf("expected winner user_1, got %q", item.WinnerID)
	}
	if item.DealPrice != 500 {
		t.Fatalf("expected deal_price 500, got %d", item.DealPrice)
	}
}
```

- [ ] **Step 2: 确认测试失败（service 方法尚未实现）**

```bash
go test ./internal/app/item/service/... -run TestPlaceBid -v 2>&1 | head -20
```

期望：编译错误 `undefined: (*Service).PlaceBid`。

- [ ] **Step 3: 实现 PlaceBid**

新建 `internal/app/item/service/bid_service.go`：

```go
package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

func (s *Service) PlaceBid(current *usermodel.User, itemID string, input dto.PlaceBidInput) (*dto.PlaceBidResult, error) {
	item, rule, err := s.store.FindItemWithRule(strings.TrimSpace(itemID))
	if err != nil {
		return nil, err
	}
	if item.Status != model.ItemOngoing {
		return nil, errorx.ErrInvalidRequest
	}
	if s.cache == nil {
		return nil, errorx.ErrInternal
	}

	bidID := "bid_" + snowflake.MakeUUID()
	luaResult, err := s.cache.PlaceBidLua(context.Background(), item.ID, itemcache.BidLuaArgs{
		UserID:            current.ID,
		UserName:          input.UserName,
		BidID:             bidID,
		Price:             input.Price,
		BidIncrement:      rule.BidIncrement,
		PriceCap:          rule.PriceCap,
		ExtendTriggerSec:  s.policy.ExtendTriggerSec,
		AutoExtendSec:     s.policy.AutoExtendSec,
		MaxExtendCount:    s.policy.MaxExtendCount,
		MaxTotalExtendSec: s.policy.MaxTotalExtendSec,
		NowUnix:           s.now().Unix(),
		IdempotencyKey:    input.IdempotencyKey,
		IdempotencyTTL:    86400,
	})
	if err != nil {
		return nil, err
	}

	switch luaResult.Code {
	case 1: // idempotent: already bid, return current state without writing BidLog again
		return &dto.PlaceBidResult{
			BidID:        luaResult.BidID,
			CurrentPrice: luaResult.CurrentPrice,
			LeaderUserID: luaResult.LeaderUserID,
			EndTime:      time.Unix(luaResult.EndTimeUnix, 0),
			Status:       "ongoing",
		}, nil
	case 2:
		return nil, errorx.New(http.StatusBadRequest, 40002, "auction has ended")
	case 3:
		return nil, errorx.New(http.StatusBadRequest, 40003, "price too low")
	case 4:
		return nil, errorx.New(http.StatusBadRequest, 40004, "invalid bid increment")
	}

	// TODO: 高并发场景下改为异步落库（写入 Redis LIST，worker 批量消费）
	bidLog := &model.BidLog{
		ID:     luaResult.BidID,
		ItemID: item.ID,
		RoomID: item.RoomID,
		UserID: current.ID,
		Price:  input.Price,
	}
	if err := s.store.CreateBidLog(bidLog); err != nil {
		return nil, err
	}

	status := "ongoing"
	if luaResult.IsCapped {
		item.Status = model.ItemEnded
		item.WinnerID = current.ID
		item.DealPrice = input.Price
		if err := s.store.UpdateItemWithRule(item, rule); err != nil {
			return nil, err
		}
		status = "ended"
		// TODO: broadcast auction_ended WebSocket event (implement after WS module)
	}

	return &dto.PlaceBidResult{
		BidID:        luaResult.BidID,
		CurrentPrice: luaResult.CurrentPrice,
		LeaderUserID: luaResult.LeaderUserID,
		EndTime:      time.Unix(luaResult.EndTimeUnix, 0),
		Status:       status,
	}, nil
}
```

还需在 `usermodel.User` 中确认有 `Name` 字段。检查 `internal/app/user/model/user.go`，若存在则直接使用；若字段名不同则对应修改 handler（Task 7）中的赋值。

- [ ] **Step 4: 确认 PlaceBid 测试通过**

```bash
go test ./internal/app/item/service/... -run TestPlaceBid -v
```

期望：所有 `TestPlaceBid*` 测试通过。

- [ ] **Step 5: 确认全部测试通过**

```bash
go test ./internal/app/item/...
```

期望：全部通过。

- [ ] **Step 6: Commit**

```bash
git add internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go
git commit -m "feat(bid): implement PlaceBid service with TDD"
```

---

## Task 6: GetRanking service（TDD）

**Files:**
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: 写失败测试**

在 `bid_service_test.go` 末尾追加：

```go
func TestGetRankingFromCache(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	users := []struct {
		user  *usermodel.User
		price int64
		idem  string
	}{
		{&usermodel.User{ID: "u1", Name: "Alice"}, 300, "k1"},
		{&usermodel.User{ID: "u2", Name: "Bob"}, 500, "k2"},
		{&usermodel.User{ID: "u3", Name: "Carol"}, 400, "k3"},
	}
	for _, u := range users {
		if _, err := svc.PlaceBid(u.user, itemID, itemdto.PlaceBidInput{
			Price: u.price, IdempotencyKey: u.idem, UserName: u.user.Name,
		}); err != nil {
			t.Fatalf("PlaceBid for %s failed: %v", u.user.ID, err)
		}
	}

	result, err := svc.GetRanking(itemID, 1, 10)
	if err != nil {
		t.Fatalf("GetRanking failed: %v", err)
	}
	if len(result.List) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.List))
	}
	if result.List[0].UserID != "u2" || result.List[0].Price != 500 {
		t.Fatalf("expected rank 1 = u2/500, got %+v", result.List[0])
	}
	if result.List[0].Rank != 1 {
		t.Fatalf("expected rank 1, got %d", result.List[0].Rank)
	}
	if result.List[1].UserID != "u3" || result.List[1].Price != 400 {
		t.Fatalf("expected rank 2 = u3/400, got %+v", result.List[1])
	}
	if result.List[2].UserID != "u1" || result.List[2].Price != 300 {
		t.Fatalf("expected rank 3 = u1/300, got %+v", result.List[2])
	}
}

func TestGetRankingFallsBackToMySQL(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	// Seed bid logs directly in fakeStore (simulate Redis miss)
	store.bidLogs = append(store.bidLogs,
		&itemmodel.BidLog{ID: "b1", ItemID: itemID, RoomID: "room_1", UserID: "u1", Price: 200},
		&itemmodel.BidLog{ID: "b2", ItemID: itemID, RoomID: "room_1", UserID: "u2", Price: 300},
	)
	// Make cache return empty (simulate miss)
	delete(fc.ranking, itemID)

	result, err := svc.GetRanking(itemID, 1, 10)
	if err != nil {
		t.Fatalf("GetRanking fallback failed: %v", err)
	}
	if len(result.List) != 2 {
		t.Fatalf("expected 2 entries from MySQL fallback, got %d", len(result.List))
	}
	if result.List[0].Price != 300 {
		t.Fatalf("expected highest price first, got %d", result.List[0].Price)
	}
}

func TestGetRankingPagination(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	for i := 1; i <= 5; i++ {
		u := &usermodel.User{ID: fmt.Sprintf("u%d", i), Name: fmt.Sprintf("User%d", i)}
		_, err := svc.PlaceBid(u, itemID, itemdto.PlaceBidInput{
			Price:          int64(i * 100),
			IdempotencyKey: fmt.Sprintf("k%d", i),
			UserName:       u.Name,
		})
		if err != nil {
			t.Fatalf("PlaceBid failed: %v", err)
		}
	}

	r, err := svc.GetRanking(itemID, 1, 2)
	if err != nil {
		t.Fatalf("GetRanking page 1 failed: %v", err)
	}
	if len(r.List) != 2 {
		t.Fatalf("expected 2 entries on page 1, got %d", len(r.List))
	}
	if r.List[0].Rank != 1 || r.List[1].Rank != 2 {
		t.Fatalf("expected ranks 1,2 on page 1, got %d,%d", r.List[0].Rank, r.List[1].Rank)
	}

	r2, err := svc.GetRanking(itemID, 2, 2)
	if err != nil {
		t.Fatalf("GetRanking page 2 failed: %v", err)
	}
	if len(r2.List) != 2 {
		t.Fatalf("expected 2 entries on page 2, got %d", len(r2.List))
	}
	if r2.List[0].Rank != 3 {
		t.Fatalf("expected rank 3 on page 2, got %d", r2.List[0].Rank)
	}
}
```

需在 `bid_service_test.go` 顶部 import 中加入 `"fmt"`。

- [ ] **Step 2: 确认测试失败**

```bash
go test ./internal/app/item/service/... -run TestGetRanking -v 2>&1 | head -10
```

期望：编译错误 `undefined: (*Service).GetRanking`。

- [ ] **Step 3: 实现 GetRanking**

在 `bid_service.go` 末尾追加：

```go
func (s *Service) GetRanking(itemID string, page, pageSize int) (*dto.RankingResult, error) {
	if page <= 0 {
		page = 1
	}
	switch {
	case pageSize > 100:
		pageSize = 100
	case pageSize <= 0:
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	var entries []dto.BidderPrice
	if s.cache != nil {
		var err error
		entries, err = s.cache.GetRanking(context.Background(), strings.TrimSpace(itemID), offset, pageSize)
		if err != nil {
			entries = nil
		}
	}

	if len(entries) == 0 {
		var err error
		all, err := s.store.ListBidRanking(strings.TrimSpace(itemID), offset+pageSize)
		if err != nil {
			return nil, err
		}
		if offset < len(all) {
			entries = all[offset:]
		}
		if len(entries) > pageSize {
			entries = entries[:pageSize]
		}
	}

	list := make([]dto.RankingEntry, len(entries))
	for i, e := range entries {
		list[i] = dto.RankingEntry{
			Rank:     offset + i + 1,
			UserID:   e.UserID,
			UserName: e.UserName,
			Price:    e.Price,
		}
	}
	return &dto.RankingResult{List: list, Page: page, PageSize: pageSize}, nil
}
```

- [ ] **Step 4: 确认测试通过**

```bash
go test ./internal/app/item/service/... -v
```

期望：所有测试通过。

- [ ] **Step 5: Commit**

```bash
git add internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go
git commit -m "feat(bid): implement GetRanking service with MySQL fallback"
```

---

## Task 7: Handler + Router

**Files:**
- Create: `internal/app/item/handler/bid.go`
- Modify: `internal/app/item/router/item.go`

- [ ] **Step 1: 确认 User model 字段名**

```bash
grep -n "Name " internal/app/user/model/user.go
```

记下 `Name` 字段名，下面 handler 中会用到。若字段名为 `Name` 则直接使用 `current.Name`。

- [ ] **Step 2: 创建 bid handler**

新建 `internal/app/item/handler/bid.go`：

```go
package handler

import (
	"strconv"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

func PlaceBid(r flamego.Render, c flamego.Context, current *usermodel.User, body dto.PlaceBidRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.PlaceBid(current, c.Param("item_id"), body.Input(current.Name))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func GetRanking(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	result, err := svc.GetRanking(c.Param("item_id"), page, pageSize)
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
```

- [ ] **Step 3: 注册路由**

在 `internal/app/item/router/item.go` 的 `RegisterRoutes` 函数中追加（auth 组内追加出价路由，auth 组外追加排行榜路由）：

```go
// 在 f.Group("/api/v1", ..., auth) 的闭包内末尾追加：
f.Post("/items/{item_id}/bids", binding.JSON(dto.PlaceBidRequest{}), handler.PlaceBid)

// 在 auth 组外（与 f.Get("/api/v1/items", ...) 同级）追加：
f.Get("/api/v1/items/{item_id}/ranking", handler.GetRanking)
```

最终 `RegisterRoutes` 完整内容：

```go
func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)

	f.Get("/api/v1/items", handler.ListItems)
	f.Get("/api/v1/items/{item_id}", handler.GetItem)
	f.Get("/api/v1/items/{item_id}/ranking", handler.GetRanking)
	f.Group("/api/v1", func() {
		f.Post("/items", binding.JSON(dto.CreateItemRequest{}), handler.CreateItem)
		f.Get("/merchant/items", handler.ListMerchantItems)
		f.Put("/items/{item_id}", binding.JSON(dto.CreateItemRequest{}), handler.UpdateItem)
		f.Delete("/items/{item_id}", handler.DeleteItem)
		f.Post("/items/{item_id}/publish", handler.PublishItem)
		f.Post("/items/{item_id}/start", handler.StartItem)
		f.Post("/items/{item_id}/cancel", handler.CancelItem)
		f.Post("/items/{item_id}/bids", binding.JSON(dto.PlaceBidRequest{}), handler.PlaceBid)
	}, auth)
}
```

- [ ] **Step 4: 确认全量编译和测试通过**

```bash
go build ./... && go test ./internal/app/item/...
```

期望：编译无错，所有测试通过。

- [ ] **Step 5: Commit**

```bash
git add internal/app/item/handler/bid.go internal/app/item/router/item.go
git commit -m "feat(bid): add PlaceBid and GetRanking handler and routes"
```

---

## Task 8: 冒烟验证

- [ ] **Step 1: 启动服务**

```bash
go run main.go server -c config.yaml
```

期望：服务正常启动，无启动错误。

- [ ] **Step 2: 创建商家用户并登录**

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"account":"merchant1","password":"pass123"}' | jq .
```

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"account":"merchant1","password":"pass123"}' | jq .data.token
```

保存 token 到 `MERCHANT_TOKEN`。

- [ ] **Step 3: 升级为商家身份、开通直播间、创建商品、上架、开拍**

```bash
curl -s -X PUT http://localhost:8080/api/v1/users/me \
  -H "Authorization: Bearer $MERCHANT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"identity":"merchant"}' | jq .

curl -s -X POST http://localhost:8080/api/v1/merchant/room \
  -H "Authorization: Bearer $MERCHANT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Test Room"}' | jq .data.id
# 保存 ROOM_ID

curl -s -X POST http://localhost:8080/api/v1/items \
  -H "Authorization: Bearer $MERCHANT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"room_id\":\"$ROOM_ID\",\"title\":\"翡翠手镯\",\"rule\":{\"start_price\":0,\"bid_increment\":100,\"price_cap\":10000,\"deposit_amount\":500,\"start_time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"end_time\":\"$(date -u -v+10M +%Y-%m-%dT%H:%M:%SZ)\"}}" | jq .
# 保存 ITEM_ID

curl -s -X POST "http://localhost:8080/api/v1/items/$ITEM_ID/publish" \
  -H "Authorization: Bearer $MERCHANT_TOKEN" | jq .

curl -s -X POST "http://localhost:8080/api/v1/items/$ITEM_ID/start" \
  -H "Authorization: Bearer $MERCHANT_TOKEN" | jq .
```

- [ ] **Step 4: 注册普通用户并出价**

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"account":"bidder1","password":"pass123"}' | jq .data.token
# 保存 USER_TOKEN

curl -s -X POST "http://localhost:8080/api/v1/items/$ITEM_ID/bids" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"price":100,"idempotency_key":"test-uuid-001"}' | jq .
```

期望：返回 `{"code":0,"data":{"bid_id":"...","current_price":100,"status":"ongoing",...}}`。

- [ ] **Step 5: 验证排行榜**

```bash
curl -s "http://localhost:8080/api/v1/items/$ITEM_ID/ranking?page=1&page_size=10" | jq .
```

期望：返回包含 `bidder1` 的排行榜，`rank=1`，`price=100`。

- [ ] **Step 6: 验证幂等性**

```bash
curl -s -X POST "http://localhost:8080/api/v1/items/$ITEM_ID/bids" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"price":100,"idempotency_key":"test-uuid-001"}' | jq .
```

期望：返回相同的 `bid_id`，`code=0`，排行榜仍只有一条记录。

- [ ] **Step 7: Final commit**

```bash
git add -A
git status  # 确认无意外文件
git commit -m "feat(bid): complete bid module implementation" --allow-empty
```
