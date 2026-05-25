# WebSocket 模块 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为直播竞拍系统实现 WebSocket 实时推送模块，支持全房间广播和定向推送，将竞拍事件实时同步给客户端。

**Architecture:** `pkg/wsevent` 定义无业务依赖的 `Broadcaster` interface；`internal/app/ws` 模块实现 gorilla/websocket Hub，通过 `sync.RWMutex` 保护双索引（room/user）；item 和 order 服务通过构造函数注入 `Broadcaster`，nil-safe 守卫保证测试不依赖 WS 模块。

**Tech Stack:** `github.com/gorilla/websocket`，Redis（ticket 存储 + 在线人数），Go `sync.RWMutex`，goroutine per connection（readLoop + writeLoop）

---

## File Map

**新建：**
- `pkg/wsevent/broadcaster.go` — Broadcaster interface，Event struct，RoomTopic/UserAddr helper
- `internal/app/ws/hub/hub.go` — Hub struct，Register/Remove/Fanout/Unicast/closeConn/syncRedis
- `internal/app/ws/hub/hub_test.go` — Hub 单元测试
- `internal/app/ws/hub/conn.go` — Conn struct，readLoop，writeLoop
- `internal/app/ws/handler/ticket.go` — ticket service + POST /api/v1/ws-ticket handler
- `internal/app/ws/handler/ws.go` — WebSocket 升级 handler，握手流程
- `internal/app/ws/router/router.go` — 路由注册
- `internal/app/ws/init.go` — Module 实现，暴露 `var Hub *hub.Hub`
- `internal/app/item/dto/events.go` — item 模块事件常量 + payload struct

**修改：**
- `go.mod` / `go.sum` — 添加 gorilla/websocket
- `internal/app/item/cache/cache.go` — BidLuaResult 新增 PrevLeaderUserID 字段
- `internal/app/item/cache/bid.go` — Lua 脚本返回 prev_leader，parser 解析第 8 个返回值
- `internal/app/item/service/service_test.go` — fakeCache.PlaceBidLua 返回 PrevLeaderUserID
- `internal/app/item/service/service.go` — Service 新增 broadcaster 字段，NewService 新增参数
- `internal/app/item/service/bid_service.go` — PlaceBid 和 EndExpiredAuctions 新增广播调用
- `internal/app/item/init.go` — 注入 wsapp.Hub
- `internal/app/appInitialize/init.go` — 注册 ws 模块，排在 item/order 前

---

## Task 1: 添加 gorilla/websocket 依赖

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: 添加依赖**

```bash
cd /Users/echin/echin/go/live-auction-backend
go get github.com/gorilla/websocket@v1.5.3
```

- [ ] **Step 2: 验证依赖已写入**

```bash
grep "gorilla/websocket" go.mod
```

Expected: `github.com/gorilla/websocket v1.5.3`

- [ ] **Step 3: 确认构建通过**

```bash
go build ./...
```

Expected: 无报错输出

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add gorilla/websocket dependency"
```

---

## Task 2: pkg/wsevent — Broadcaster interface

**Files:**
- Create: `pkg/wsevent/broadcaster.go`
- Create: `pkg/wsevent/broadcaster_test.go`

- [ ] **Step 1: 写测试**

创建 `pkg/wsevent/broadcaster_test.go`：

```go
package wsevent_test

import (
	"testing"

	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

func TestRoomTopic(t *testing.T) {
	if got := wsevent.RoomTopic("room_123"); got != "room:room_123" {
		t.Errorf("RoomTopic = %q, want %q", got, "room:room_123")
	}
}

func TestUserAddr(t *testing.T) {
	if got := wsevent.UserAddr("user_456"); got != "user:user_456" {
		t.Errorf("UserAddr = %q, want %q", got, "user:user_456")
	}
}

func TestEventJSON(t *testing.T) {
	// 确认 Event.Payload 为 any，可接受任意 struct
	evt := wsevent.Event{Type: "ping", Payload: map[string]string{"k": "v"}}
	if evt.Type != "ping" {
		t.Errorf("unexpected type %q", evt.Type)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./pkg/wsevent/... -v
```

Expected: FAIL — package not found

- [ ] **Step 3: 创建 broadcaster.go**

创建 `pkg/wsevent/broadcaster.go`：

```go
package wsevent

type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type Broadcaster interface {
	Fanout(topic string, event Event) error
	Unicast(addr string, event Event) error
}

func RoomTopic(roomID string) string { return "room:" + roomID }
func UserAddr(userID string) string  { return "user:" + userID }
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./pkg/wsevent/... -v
```

Expected: PASS — 3 tests

- [ ] **Step 5: Commit**

```bash
git add pkg/wsevent/
git commit -m "feat: add wsevent Broadcaster interface and topic helpers"
```

---

## Task 3: item/dto/events.go — 事件常量与 payload

**Files:**
- Create: `internal/app/item/dto/events.go`

- [ ] **Step 1: 创建 events.go**

创建 `internal/app/item/dto/events.go`：

```go
package dto

import "time"

const (
	EventAuctionStarted   = "auction_started"
	EventBidSuccess       = "bid_success"
	EventAuctionExtended  = "auction_extended"
	EventUserOutbid       = "user_outbid"
	EventAuctionEnded     = "auction_ended"
	EventAuctionCancelled = "auction_cancelled"
	EventOrderCreated     = "order_created"
)

type AuctionStartedPayload struct {
	ItemID    string    `json:"item_id"`
	RoomID    string    `json:"room_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

type BidSuccessPayload struct {
	ItemID       string    `json:"item_id"`
	UserID       string    `json:"user_id"`
	Price        int64     `json:"price"`
	CurrentPrice int64     `json:"current_price"`
	LeaderUserID string    `json:"leader_user_id"`
	EndTime      time.Time `json:"end_time"`
}

type AuctionExtendedPayload struct {
	ItemID        string    `json:"item_id"`
	OldEndTime    time.Time `json:"old_end_time"`
	NewEndTime    time.Time `json:"new_end_time"`
	ExtendSeconds int       `json:"extend_seconds"`
}

type UserOutbidPayload struct {
	ItemID       string `json:"item_id"`
	NewLeaderID  string `json:"new_leader_user_id"`
	CurrentPrice int64  `json:"current_price"`
}

type AuctionEndedPayload struct {
	ItemID       string `json:"item_id"`
	WinnerUserID string `json:"winner_user_id"`
	DealPrice    int64  `json:"deal_price"`
}

type AuctionCancelledPayload struct {
	ItemID string `json:"item_id"`
}

type OrderCreatedPayload struct {
	ItemID      string `json:"item_id"`
	OrderID     string `json:"order_id"`
	WinnerID    string `json:"winner_user_id"`
	DealPrice   int64  `json:"deal_price"`
}
```

- [ ] **Step 2: 确认构建通过**

```bash
go build ./internal/app/item/...
```

Expected: 无报错

- [ ] **Step 3: Commit**

```bash
git add internal/app/item/dto/events.go
git commit -m "feat: add item ws event constants and payload types"
```

---

## Task 4: 扩展 BidLuaResult，Lua 脚本返回 prev_leader

`user_outbid` 需要知道出价前的领先者。在 Lua 脚本成功分支捕获旧 leader 并作为第 8 个返回值，避免额外的 Redis 读取。

**Files:**
- Modify: `internal/app/item/cache/cache.go`
- Modify: `internal/app/item/cache/bid.go`
- Modify: `internal/app/item/service/service_test.go` (fakeCache)

- [ ] **Step 1: 更新 BidLuaResult struct**

编辑 `internal/app/item/cache/cache.go`，在 `BidLuaResult` 末尾新增字段：

```go
type BidLuaResult struct {
	Code             int
	BidID            string
	CurrentPrice     int64
	LeaderUserID     string
	EndTimeUnix      int64
	IsExtended       bool
	IsCapped         bool
	PrevLeaderUserID string // leader before this bid; empty if no previous leader
}
```

- [ ] **Step 2: 更新 Lua 脚本**

编辑 `internal/app/item/cache/bid.go`，找到 `const bidLuaScript`，按以下方式修改：

在所有本地变量声明之后（`local part_cnt = ...` 后面），新增一行：
```lua
local prev_leader = s['leader_user_id'] or ''
```

将末尾成功返回 `return {0, bid_id, price, user_id, end_unix, is_extended, is_capped}` 改为：
```lua
return {0, bid_id, price, user_id, end_unix, is_extended, is_capped, prev_leader}
```

将所有其他返回语句统一改为 8 个元素：
```lua
-- idempotent (code=1):
return {1, existing, tonumber(m['current_price'] or 0), m['leader_user_id'] or '', tonumber(m['end_time_unix'] or 0), 0, 0, ''}
-- error codes (2, 3, 4):
return {2,'',0,'',0,0,0,''}
return {3,'',0,'',0,0,0,''}
return {4,'',0,'',0,0,0,''}
```

- [ ] **Step 3: 更新 parser，解析第 8 个返回值**

编辑 `internal/app/item/cache/bid.go`，在 `PlaceBidLua` 函数中：

将 `if len(res) < 7` 改为 `if len(res) < 8`。

在 return 语句中新增 `PrevLeaderUserID`：

```go
return &BidLuaResult{
    Code:             int(toI64(res[0])),
    BidID:            toStr(res[1]),
    CurrentPrice:     toI64(res[2]),
    LeaderUserID:     toStr(res[3]),
    EndTimeUnix:      toI64(res[4]),
    IsExtended:       toI64(res[5]) == 1,
    IsCapped:         toI64(res[6]) == 1,
    PrevLeaderUserID: toStr(res[7]),
}, nil
```

- [ ] **Step 4: 更新 fakeCache.PlaceBidLua 返回 PrevLeaderUserID**

编辑 `internal/app/item/service/service_test.go`，找到 `fakeCache.PlaceBidLua` 方法，在计算 `isCapped` 之前捕获旧 leader：

```go
prevLeader := state.LeaderUserID
// ... 之后原有 state.CurrentPrice = args.Price; state.LeaderUserID = args.UserID 等更新 ...
return &itemcache.BidLuaResult{
    Code:             0,
    BidID:            args.BidID,
    CurrentPrice:     args.Price,
    LeaderUserID:     args.UserID,
    EndTimeUnix:      state.EndTime.Unix(),
    IsExtended:       isExtended,
    IsCapped:         isCapped,
    PrevLeaderUserID: prevLeader,
}, nil
```

- [ ] **Step 5: 运行现有测试确认不破坏已有逻辑**

```bash
go test ./internal/app/item/... -v
```

Expected: 所有测试 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/app/item/cache/ internal/app/item/service/service_test.go
git commit -m "feat: extend BidLuaResult with PrevLeaderUserID for user_outbid event"
```

---

## Task 5: ws/hub/hub.go — Hub 核心

**Files:**
- Create: `internal/app/ws/hub/hub.go`
- Create: `internal/app/ws/hub/hub_test.go`

- [ ] **Step 1: 写 Hub 单元测试**

创建 `internal/app/ws/hub/hub_test.go`：

```go
package hub

import (
	"testing"

	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

func newTestConn(userID, roomID string) *Conn {
	return &Conn{
		id:     "conn_" + userID,
		userID: userID,
		roomID: roomID,
		send:   make(chan wsevent.Event, 8),
	}
}

func TestRegisterAddsToIndexes(t *testing.T) {
	h := NewHub(nil)
	c := newTestConn("user_1", "room_1")
	h.Register(c)

	h.mu.RLock()
	defer h.mu.RUnlock()

	if _, ok := h.rooms["room_1"]["conn_user_1"]; !ok {
		t.Error("expected conn in rooms index")
	}
	found := false
	for _, uc := range h.users["user_1"] {
		if uc.id == "conn_user_1" {
			found = true
		}
	}
	if !found {
		t.Error("expected conn in users index")
	}
}

func TestRemoveCleansIndexes(t *testing.T) {
	h := NewHub(nil)
	c := newTestConn("user_1", "room_1")
	h.Register(c)
	h.Remove(c)

	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.rooms["room_1"]) != 0 {
		t.Error("expected rooms index cleared")
	}
	if len(h.users["user_1"]) != 0 {
		t.Error("expected users index cleared")
	}
}

func TestFanoutDeliversToRoom(t *testing.T) {
	h := NewHub(nil)
	c1 := newTestConn("user_1", "room_1")
	c2 := newTestConn("user_2", "room_1")
	c3 := newTestConn("user_3", "room_2") // 不同房间
	h.Register(c1)
	h.Register(c2)
	h.Register(c3)

	evt := wsevent.Event{Type: "test_event"}
	if err := h.Fanout(wsevent.RoomTopic("room_1"), evt); err != nil {
		t.Fatalf("Fanout error: %v", err)
	}

	if len(c1.send) != 1 {
		t.Errorf("c1 should receive 1 event, got %d", len(c1.send))
	}
	if len(c2.send) != 1 {
		t.Errorf("c2 should receive 1 event, got %d", len(c2.send))
	}
	if len(c3.send) != 0 {
		t.Errorf("c3 (different room) should receive 0 events, got %d", len(c3.send))
	}
}

func TestUnicastDeliversToUser(t *testing.T) {
	h := NewHub(nil)
	c1 := newTestConn("user_1", "room_1")
	c2 := newTestConn("user_2", "room_1")
	h.Register(c1)
	h.Register(c2)

	evt := wsevent.Event{Type: "user_outbid"}
	if err := h.Unicast(wsevent.UserAddr("user_1"), evt); err != nil {
		t.Fatalf("Unicast error: %v", err)
	}

	if len(c1.send) != 1 {
		t.Errorf("c1 should receive 1 event, got %d", len(c1.send))
	}
	if len(c2.send) != 0 {
		t.Errorf("c2 should not receive event, got %d", len(c2.send))
	}
}

func TestSlowConnectionIsClosedOnFullChannel(t *testing.T) {
	h := NewHub(nil)
	// send channel 容量为 1，填满后下次 Fanout 应触发 closeConn
	c := &Conn{
		id:     "slow_conn",
		userID: "user_slow",
		roomID: "room_1",
		send:   make(chan wsevent.Event, 1), // 容量1
	}
	h.Register(c)
	c.send <- wsevent.Event{Type: "fill"} // 填满

	h.Fanout(wsevent.RoomTopic("room_1"), wsevent.Event{Type: "overflow"})

	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, ok := h.rooms["room_1"]["slow_conn"]; ok {
		t.Error("slow connection should have been removed from rooms index")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/app/ws/hub/... -v
```

Expected: FAIL — package not found

- [ ] **Step 3: 创建 hub.go**

创建 `internal/app/ws/hub/hub.go`：

```go
package hub

import (
	"context"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[string]*Conn // roomID → connID → Conn
	users map[string][]*Conn          // userID → []*Conn
	redis *redis.Client
}

func NewHub(redisClient *redis.Client) *Hub {
	return &Hub{
		rooms: make(map[string]map[string]*Conn),
		users: make(map[string][]*Conn),
		redis: redisClient,
	}
}

func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	if h.rooms[c.roomID] == nil {
		h.rooms[c.roomID] = make(map[string]*Conn)
	}
	h.rooms[c.roomID][c.id] = c
	h.users[c.userID] = append(h.users[c.userID], c)
	h.mu.Unlock()

	if h.redis != nil {
		go h.syncRedisOnJoin(c.roomID, c.userID)
	}
}

func (h *Hub) Remove(c *Conn) {
	h.mu.Lock()
	if room, ok := h.rooms[c.roomID]; ok {
		delete(room, c.id)
		if len(room) == 0 {
			delete(h.rooms, c.roomID)
		}
	}
	conns := h.users[c.userID]
	filtered := conns[:0]
	for _, uc := range conns {
		if uc.id != c.id {
			filtered = append(filtered, uc)
		}
	}
	if len(filtered) == 0 {
		delete(h.users, c.userID)
	} else {
		h.users[c.userID] = filtered
	}
	h.mu.Unlock()

	if h.redis != nil {
		go h.syncRedisOnLeave(c.roomID, c.userID)
	}
}

func (h *Hub) Fanout(topic string, event wsevent.Event) error {
	roomID := strings.TrimPrefix(topic, "room:")
	h.mu.RLock()
	room := h.rooms[roomID]
	h.mu.RUnlock()

	for _, c := range room {
		h.deliver(c, event)
	}
	return nil
}

func (h *Hub) Unicast(addr string, event wsevent.Event) error {
	userID := strings.TrimPrefix(addr, "user:")
	h.mu.RLock()
	conns := h.users[userID]
	h.mu.RUnlock()

	for _, c := range conns {
		h.deliver(c, event)
	}
	return nil
}

func (h *Hub) deliver(c *Conn, event wsevent.Event) {
	select {
	case c.send <- event:
	default:
		h.closeConn(c)
	}
}

func (h *Hub) closeConn(c *Conn) {
	h.Remove(c)
	if c.ws != nil {
		c.ws.Close()
	}
	close(c.send)
}

func (h *Hub) syncRedisOnJoin(roomID, userID string) {
	ctx := context.Background()
	stateKey := "auction:room:" + roomID + ":state"
	onlineKey := "auction:room:" + roomID + ":online_users"
	_ = h.redis.SAdd(ctx, onlineKey, userID).Err()
	_ = h.redis.HIncrBy(ctx, stateKey, "online_count", 1).Err()
}

func (h *Hub) syncRedisOnLeave(roomID, userID string) {
	ctx := context.Background()
	stateKey := "auction:room:" + roomID + ":state"
	onlineKey := "auction:room:" + roomID + ":online_users"
	_ = h.redis.SRem(ctx, onlineKey, userID).Err()
	_ = h.redis.HIncrBy(ctx, stateKey, "online_count", -1).Err()
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/app/ws/hub/... -v
```

Expected: PASS — 5 tests

- [ ] **Step 5: Commit**

```bash
git add internal/app/ws/hub/hub.go internal/app/ws/hub/hub_test.go
git commit -m "feat: implement WS Hub with dual-index and mutex-based broadcast"
```

---

## Task 6: ws/hub/conn.go — 连接读写循环

**Files:**
- Create: `internal/app/ws/hub/conn.go`

- [ ] **Step 1: 创建 conn.go**

创建 `internal/app/ws/hub/conn.go`：

```go
package hub

import (
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

const (
	readDeadline  = 60 * time.Second
	writeDeadline = 10 * time.Second
	sendBufSize   = 64
)

type Conn struct {
	id     string
	userID string
	roomID string
	ws     *websocket.Conn
	send   chan wsevent.Event
	hub    *Hub
}

type clientMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func newConn(id, userID, roomID string, ws *websocket.Conn, hub *Hub) *Conn {
	return &Conn{
		id:     id,
		userID: userID,
		roomID: roomID,
		ws:     ws,
		send:   make(chan wsevent.Event, sendBufSize),
		hub:    hub,
	}
}

func (c *Conn) readLoop() {
	defer func() {
		c.hub.Remove(c)
		c.ws.Close()
	}()

	c.ws.SetReadDeadline(time.Now().Add(readDeadline))

	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		c.ws.SetReadDeadline(time.Now().Add(readDeadline))

		var cm clientMessage
		if err := json.Unmarshal(msg, &cm); err != nil {
			continue
		}

		switch cm.Type {
		case "ping":
			c.send <- wsevent.Event{Type: "pong"}
		case "leave_room":
			return
		}
	}
}

func (c *Conn) writeLoop() {
	defer c.ws.Close()

	for event := range c.send {
		c.ws.SetWriteDeadline(time.Now().Add(writeDeadline))
		if err := c.ws.WriteJSON(event); err != nil {
			return
		}
	}
}
```

- [ ] **Step 2: 确认构建通过**

```bash
go build ./internal/app/ws/...
```

Expected: 无报错

- [ ] **Step 3: Commit**

```bash
git add internal/app/ws/hub/conn.go
git commit -m "feat: implement WS Conn with readLoop and writeLoop"
```

---

## Task 7: ws/handler/ticket.go — WebSocket ticket 发放

**Files:**
- Create: `internal/app/ws/handler/ticket.go`

- [ ] **Step 1: 创建 ticket.go**

创建 `internal/app/ws/handler/ticket.go`：

```go
package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/flamego/flamego"
	"github.com/redis/go-redis/v9"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/response"
)

const ticketTTL = 45 * time.Second

var redisClient *redis.Client

func InitTicket(r *redis.Client) {
	redisClient = r
}

func IssueTicket(c flamego.Context, current *usermodel.User, r *http.Request) {
	_ = userhandler.AuthenticateToken // 确保认证中间件已运行
	ticket, err := generateTicket()
	if err != nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	key := fmt.Sprintf("ws:ticket:%s", ticket)
	if err := redisClient.Set(context.Background(), key, current.ID, ticketTTL).Err(); err != nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	response.Success(r, map[string]string{"ticket": ticket})
}

func generateTicket() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 2: 确认构建通过**

```bash
go build ./internal/app/ws/...
```

Expected: 无报错

- [ ] **Step 3: Commit**

```bash
git add internal/app/ws/handler/ticket.go
git commit -m "feat: add ws-ticket HTTP endpoint for WebSocket auth"
```

---

## Task 8: ws/handler/ws.go + router + init.go

**Files:**
- Create: `internal/app/ws/handler/ws.go`
- Create: `internal/app/ws/router/router.go`
- Create: `internal/app/ws/init.go`

- [ ] **Step 1: 创建 ws.go（WebSocket 升级 handler）**

创建 `internal/app/ws/handler/ws.go`：

```go
package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/flamego/flamego"
	"github.com/gorilla/websocket"
	wshub "github.com/zet-plane/live-auction-backend/internal/app/ws/hub"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

var (
	hub      *wshub.Hub
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
)

func Init(h *wshub.Hub) {
	hub = h
}

func ServeWS(c flamego.Context, w http.ResponseWriter, r *http.Request) {
	roomID := c.Param("room_id")
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" || roomID == "" {
		http.Error(w, "missing ticket or room_id", http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("ws:ticket:%s", ticket)
	userID, err := redisClient.GetDel(context.Background(), key).Result()
	if err != nil {
		http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}

	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	conn := wshub.NewConn("conn_"+snowflake.MakeUUID(), userID, roomID, wsConn, hub)
	hub.Register(conn)
	go conn.StartReadLoop()
	go conn.StartWriteLoop()
}
```

- [ ] **Step 2: 将 newConn 和 loop 方法导出**

编辑 `internal/app/ws/hub/conn.go`，将 `newConn` 改为 `NewConn`（首字母大写导出），将 `readLoop`/`writeLoop` 改为 `StartReadLoop`/`StartWriteLoop`：

```go
func NewConn(id, userID, roomID string, ws *websocket.Conn, hub *Hub) *Conn {
    return &Conn{...}
}

func (c *Conn) StartReadLoop() {
    // 原 readLoop 内容不变
}

func (c *Conn) StartWriteLoop() {
    // 原 writeLoop 内容不变
}
```

- [ ] **Step 3: 创建 router.go**

创建 `internal/app/ws/router/router.go`：

```go
package router

import (
	"github.com/flamego/flamego"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/handler"
)

func RegisterRoutes(f *flamego.Flame) {
	f.Group("/api/v1", func() {
		f.Post("/ws-ticket", userhandler.Authenticate, handler.IssueTicket)
	})
	f.Get("/ws/v1/rooms/{room_id}", handler.ServeWS)
}
```

- [ ] **Step 4: 创建 init.go**

创建 `internal/app/ws/init.go`：

```go
package ws

import (
	"context"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	wshub "github.com/zet-plane/live-auction-backend/internal/app/ws/hub"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/ws/router"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

// Hub is the package-level Hub singleton implementing wsevent.Broadcaster.
var Hub wsevent.Broadcaster

type WS struct {
	Name string
	app.UnimplementedModule
}

func (w *WS) Info() string { return w.Name }

func (w *WS) Load(engine *kernel.Engine) error {
	h := wshub.NewHub(engine.Cache)
	Hub = h
	handler.Init(h)
	handler.InitTicket(engine.Cache)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (w *WS) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
```

- [ ] **Step 5: 确认构建通过**

```bash
go build ./internal/app/ws/...
```

Expected: 无报错

- [ ] **Step 6: Commit**

```bash
git add internal/app/ws/
git commit -m "feat: add WS module handler, router, and init"
```

---

## Task 9: 注册 WS 模块到 appInitialize

**Files:**
- Modify: `internal/app/appInitialize/init.go`

- [ ] **Step 1: 更新 appInitialize/init.go**

编辑 `internal/app/appInitialize/init.go`，添加 ws 模块导入并在 apps 列表中将其排在 item/room/order 之前：

```go
package appInitialize

import (
	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit"
	"github.com/zet-plane/live-auction-backend/internal/app/item"
	"github.com/zet-plane/live-auction-backend/internal/app/order"
	"github.com/zet-plane/live-auction-backend/internal/app/payment"
	"github.com/zet-plane/live-auction-backend/internal/app/room"
	"github.com/zet-plane/live-auction-backend/internal/app/user"
	"github.com/zet-plane/live-auction-backend/internal/app/ws"
)

var apps = []app.Module{
	&user.User{Name: "user"},
	&ws.WS{Name: "ws"},       // ws 必须在 item/room/order 前，保证 Hub 已初始化
	&room.Room{Name: "room"},
	&order.Order{Name: "order"},
	&payment.Payment{Name: "payment"},
	&deposit.Deposit{Name: "deposit"},
	&item.Item{Name: "item"},
}

func GetApps() []app.Module {
	return apps
}
```

- [ ] **Step 2: 确认整体构建通过**

```bash
go build ./...
```

Expected: 无报错

- [ ] **Step 3: Commit**

```bash
git add internal/app/appInitialize/init.go
git commit -m "feat: register ws module in app initializer"
```

---

## Task 10: item.Service 注入 Broadcaster，StartItem / CancelItem 广播

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/init.go`

- [ ] **Step 1: 更新 Service struct 和 NewService**

编辑 `internal/app/item/service/service.go`。

在 `import` 中新增：
```go
"github.com/zet-plane/live-auction-backend/pkg/wsevent"
```

在 `Service` struct 末尾新增字段：
```go
broadcaster wsevent.Broadcaster
```

将 `NewService` 签名改为：
```go
func NewService(store dao.Store, policy dto.AuctionPolicy, cache itemcache.Cache, orderSvc *orderservice.Service, depositSvc DepositChecker, broadcaster wsevent.Broadcaster) *Service {
    return &Service{
        store:       store,
        cache:       cache,
        policy:      policy,
        now:         time.Now,
        orderSvc:    orderSvc,
        depositSvc:  depositSvc,
        broadcaster: broadcaster,
    }
}
```

- [ ] **Step 2: 在 StartItem 末尾广播 auction_started**

编辑 `StartItem` 方法，在 `item.Status = model.ItemOngoing` + `UpdateItemWithRule` 成功后、`return nil` 之前添加：

```go
if s.broadcaster != nil {
    _ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
        Type: dto.EventAuctionStarted,
        Payload: dto.AuctionStartedPayload{
            ItemID:    item.ID,
            RoomID:    item.RoomID,
            StartTime: s.now(),
            EndTime:   rule.EndTime,
        },
    })
}
```

- [ ] **Step 3: 在 CancelItem 末尾广播 auction_cancelled**

编辑 `CancelItem` 方法，在 cache 清理之后、`return nil` 之前添加：

```go
if s.broadcaster != nil {
    _ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
        Type:    dto.EventAuctionCancelled,
        Payload: dto.AuctionCancelledPayload{ItemID: item.ID},
    })
}
```

- [ ] **Step 4: 更新 item/init.go 注入 wsapp.Hub**

编辑 `internal/app/item/init.go`，在 import 中添加：
```go
wsapp "github.com/zet-plane/live-auction-backend/internal/app/ws"
```

在 `Load` 方法的 `service.NewService(...)` 调用末尾追加 `wsapp.Hub`：
```go
svc := service.NewService(store, policy, c, orderapp.Svc, depositapp.Svc, wsapp.Hub)
```

- [ ] **Step 5: 修复 service_test.go 中 NewService 调用（新增 nil 参数）**

在 `internal/app/item/service/service_test.go` 和 `bid_service_test.go` 中，所有 `NewService(store, testPolicy, fc, nil, nil)` 调用改为：
```go
NewService(store, testPolicy, fc, nil, nil, nil)
```

（broadcaster 传 nil，测试不验证广播行为）

- [ ] **Step 6: 运行测试确认通过**

```bash
go test ./internal/app/item/... -v
```

Expected: 所有测试 PASS

- [ ] **Step 7: Commit**

```bash
git add internal/app/item/service/service.go internal/app/item/init.go \
        internal/app/item/service/service_test.go internal/app/item/service/bid_service_test.go
git commit -m "feat: inject Broadcaster into item.Service, broadcast auction_started and auction_cancelled"
```

---

## Task 11: PlaceBid 广播（bid_success / user_outbid / auction_extended / auction_ended / order_created）

**Files:**
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_service_test.go`

- [ ] **Step 1: 写失败测试（验证 bid_success 广播）**

在 `bid_service_test.go` 末尾追加：

```go
type fakeBroadcaster struct {
	fanouts  []fakeFanout
	unicasts []fakeUnicast
}
type fakeFanout  struct{ topic string; event wsevent.Event }
type fakeUnicast struct{ addr string; event wsevent.Event }

func (f *fakeBroadcaster) Fanout(topic string, event wsevent.Event) error {
	f.fanouts = append(f.fanouts, fakeFanout{topic, event})
	return nil
}
func (f *fakeBroadcaster) Unicast(addr string, event wsevent.Event) error {
	f.unicasts = append(f.unicasts, fakeUnicast{addr, event})
	return nil
}

func TestPlaceBidBroadcastsBidSuccess(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "k1", UserName: "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if len(fb.fanouts) == 0 {
		t.Fatal("expected at least one Fanout call")
	}
	found := false
	for _, f := range fb.fanouts {
		if f.event.Type == itemdto.EventBidSuccess {
			found = true
		}
	}
	if !found {
		t.Errorf("expected bid_success fanout, got: %v", fb.fanouts)
	}
}

func TestPlaceBidBroadcastsUserOutbid(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	user1 := &usermodel.User{ID: "user_1", Name: "Alice", Identity: usermodel.IdentityUser}
	user2 := &usermodel.User{ID: "user_2", Name: "Bob", Identity: usermodel.IdentityUser}

	_, _ = svc.PlaceBid(user1, itemID, itemdto.PlaceBidInput{Price: 100, IdempotencyKey: "k1", UserName: "Alice"})
	fb.unicasts = nil // 清空第一次出价的 unicast

	_, err := svc.PlaceBid(user2, itemID, itemdto.PlaceBidInput{Price: 200, IdempotencyKey: "k2", UserName: "Bob"})
	if err != nil {
		t.Fatalf("second PlaceBid failed: %v", err)
	}

	found := false
	for _, u := range fb.unicasts {
		if u.event.Type == itemdto.EventUserOutbid && u.addr == wsevent.UserAddr("user_1") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected user_outbid unicast to user_1, got unicasts: %v", fb.unicasts)
	}
}
```

需在 `bid_service_test.go` 顶部追加 import：
```go
"github.com/zet-plane/live-auction-backend/pkg/wsevent"
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/app/item/service/... -run "TestPlaceBidBroadcast" -v
```

Expected: FAIL — fakeBroadcaster undefined / no broadcasts

- [ ] **Step 3: 在 PlaceBid 成功分支新增广播**

编辑 `internal/app/item/service/bid_service.go`，在 import 中添加：
```go
"github.com/zet-plane/live-auction-backend/pkg/wsevent"
```

在 `// TODO: 高并发场景...` 注释之前，在 BidLog 写入成功后（switch luaResult.Code default 分支末尾），添加：

```go
if s.broadcaster != nil {
    endTime := time.Unix(luaResult.EndTimeUnix, 0)
    _ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
        Type: dto.EventBidSuccess,
        Payload: dto.BidSuccessPayload{
            ItemID:       item.ID,
            UserID:       current.ID,
            Price:        input.Price,
            CurrentPrice: luaResult.CurrentPrice,
            LeaderUserID: luaResult.LeaderUserID,
            EndTime:      endTime,
        },
    })
    // user_outbid: 仅在领先者发生变化时通知旧领先者
    if luaResult.PrevLeaderUserID != "" && luaResult.PrevLeaderUserID != luaResult.LeaderUserID {
        _ = s.broadcaster.Unicast(wsevent.UserAddr(luaResult.PrevLeaderUserID), wsevent.Event{
            Type: dto.EventUserOutbid,
            Payload: dto.UserOutbidPayload{
                ItemID:       item.ID,
                NewLeaderID:  luaResult.LeaderUserID,
                CurrentPrice: luaResult.CurrentPrice,
            },
        })
    }
    // auction_extended
    if luaResult.IsExtended {
        _ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
            Type: dto.EventAuctionExtended,
            Payload: dto.AuctionExtendedPayload{
                ItemID:        item.ID,
                NewEndTime:    endTime,
                ExtendSeconds: s.policy.AutoExtendSec,
            },
        })
    }
}
```

在 `luaResult.IsCapped` 分支（竞拍结束），在 `CreateOrder` 调用之后追加：

```go
if s.broadcaster != nil {
    orderID := ""
    if order != nil {
        orderID = order.ID
    }
    endEvt := wsevent.Event{
        Type: dto.EventAuctionEnded,
        Payload: dto.AuctionEndedPayload{
            ItemID:       item.ID,
            WinnerUserID: current.ID,
            DealPrice:    input.Price,
        },
    }
    _ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), endEvt)
    if orderID != "" {
        orderEvt := wsevent.Event{
            Type: dto.EventOrderCreated,
            Payload: dto.OrderCreatedPayload{
                ItemID:    item.ID,
                OrderID:   orderID,
                WinnerID:  current.ID,
                DealPrice: input.Price,
            },
        }
        _ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), orderEvt)
        _ = s.broadcaster.Unicast(wsevent.UserAddr(current.ID), orderEvt)
    }
}
```

注意：`CreateOrder` 调用处需要捕获返回值：
```go
var order *ordermodel.Order
if s.orderSvc != nil {
    order, _ = s.orderSvc.CreateOrder(item.ID, current.ID, input.Price)
}
```

需在 import 中添加：
```go
ordermodel "github.com/zet-plane/live-auction-backend/internal/app/order/model"
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/app/item/... -v
```

Expected: 所有测试 PASS，包括两个新广播测试

- [ ] **Step 5: Commit**

```bash
git add internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go
git commit -m "feat: broadcast bid_success, user_outbid, auction_extended, auction_ended, order_created from PlaceBid"
```

---

## Task 12: EndExpiredAuctions 广播

**Files:**
- Modify: `internal/app/item/service/service.go`

- [ ] **Step 1: 在 EndExpiredAuctions 结算循环末尾新增广播**

编辑 `internal/app/item/service/service.go`，在 `EndExpiredAuctions` 中，`s.orderSvc.CreateOrder(...)` 调用后追加广播（与 Task 11 的 IsCapped 分支结构对称）：

在 `for _, iwr := range items` 循环体末尾，`if s.cache != nil { _ = s.cache.DeleteAuctionState(...) }` 之后添加：

```go
if s.broadcaster != nil {
    _ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
        Type: dto.EventAuctionEnded,
        Payload: dto.AuctionEndedPayload{
            ItemID:       item.ID,
            WinnerUserID: winnerID,
            DealPrice:    dealPrice,
        },
    })
}
```

将 `s.orderSvc.CreateOrder(item.ID, winnerID, dealPrice)` 调用修改为捕获返回值，并在成功后广播 order_created：

```go
if winnerID != "" && s.orderSvc != nil {
    if order, err := s.orderSvc.CreateOrder(item.ID, winnerID, dealPrice); err == nil && s.broadcaster != nil {
        orderEvt := wsevent.Event{
            Type: dto.EventOrderCreated,
            Payload: dto.OrderCreatedPayload{
                ItemID:    item.ID,
                OrderID:   order.ID,
                WinnerID:  winnerID,
                DealPrice: dealPrice,
            },
        }
        _ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), orderEvt)
        _ = s.broadcaster.Unicast(wsevent.UserAddr(winnerID), orderEvt)
    }
}
```

- [ ] **Step 2: 运行全量测试**

```bash
go test ./...
```

Expected: 所有测试 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/app/item/service/service.go
git commit -m "feat: broadcast auction_ended and order_created from EndExpiredAuctions"
```

---

## Self-Review Checklist

**Spec 覆盖确认：**
- [x] §4 pkg/wsevent Broadcaster interface → Task 2
- [x] §5 Hub 双索引，Register/Remove，Fanout/Unicast，慢连接断开 → Task 5
- [x] §5.5 readLoop/writeLoop → Task 6
- [x] §6.1 POST /api/v1/ws-ticket → Task 7
- [x] §6.2 GET /ws/v1/rooms/{room_id} 握手升级 → Task 8
- [x] §6.3 ping/pong/leave_room 客户端消息 → Task 6 (conn.go)
- [x] §7 模块注入顺序 → Task 9
- [x] §8 StartItem → auction_started → Task 10
- [x] §8 PlaceBid → bid_success, user_outbid, auction_extended, auction_ended, order_created → Task 11
- [x] §8 CancelItem → auction_cancelled → Task 10
- [x] §8 EndExpiredAuctions → auction_ended, order_created → Task 12
- [x] §5.2 Redis 在线人数异步更新 → Task 5 (syncRedisOnJoin/Leave)
- [x] §4 item/dto/events.go 业务 payload → Task 3
- [x] §4 PrevLeaderUserID 来源 → Task 4 (Lua 脚本扩展)
