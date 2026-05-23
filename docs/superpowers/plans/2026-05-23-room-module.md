# Room Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the `internal/app/room/` module from scratch: a live-room management system where each merchant has one room, state cycles `idle ↔ live`, and a Redis ZSET tracks the queued auction items.

**Architecture:** New module under `internal/app/room/` follows the same sub-package pattern as `item`: model → dao → dto → cache → service (TDD) → handler → router → init. `Service` depends on `dao.Store` and `cache.Cache` interfaces. Redis is soft-fail on `StartRoom`/`EndRoom`; `GetRoom` and `ListRooms` degrade silently on cache miss. No Redis TTL — state lives until explicitly updated.

**Tech Stack:** go-redis/v9 (`github.com/redis/go-redis/v9`), GORM, flamego, standard `context` + `errors` packages.

---

## File Map

| Action | File | What changes |
|---|---|---|
| Create | `internal/app/room/model/room.go` | `RoomStatus` constants + `LiveRoom` GORM struct |
| Create | `internal/app/room/dao/room.go` | `Store` interface + `GormStore` (5 methods) |
| Create | `internal/app/room/dto/room.go` | Request/response types + DTO constructors |
| Create | `internal/app/room/cache/cache.go` | `RoomState`, `Cache` interface, `RedisCache` |
| Create | `internal/app/room/service/service_test.go` | `fakeStore`, `fakeCache`, 11 tests |
| Create | `internal/app/room/service/service.go` | Service with all 6 methods |
| Create | `internal/app/room/handler/room.go` | 6 flamego handlers |
| Create | `internal/app/room/router/room.go` | Route registration |
| Create | `internal/app/room/init.go` | `Module` implementation |
| Create | `internal/app/appInitialize/room.go` | Register module in `apps` slice |

---

### Task 1: model/room.go

**Files:**
- Create: `internal/app/room/model/room.go`

- [ ] **Step 1: Write the model file**

```go
package model

import (
	"time"

	"gorm.io/gorm"
)

type RoomStatus string

const (
	RoomIdle RoomStatus = "idle"
	RoomLive RoomStatus = "live"
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

- [ ] **Step 2: Verify compile**

```bash
go build ./internal/app/room/model/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/app/room/model/room.go
git commit -m "feat(room): add LiveRoom model"
```

---

### Task 2: dao/room.go

**Files:**
- Create: `internal/app/room/dao/room.go`

- [ ] **Step 1: Write the DAO file**

```go
package dao

import (
	"errors"

	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"gorm.io/gorm"
)

type Store interface {
	AutoMigrate() error
	CreateRoom(room *model.LiveRoom) error
	FindRoomByID(roomID string) (*model.LiveRoom, error)
	FindRoomByMerchantID(merchantID string) (*model.LiveRoom, error)
	UpdateRoom(room *model.LiveRoom) error
	ListRooms(status model.RoomStatus) ([]*model.LiveRoom, error)
}

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) AutoMigrate() error {
	return s.db.AutoMigrate(&model.LiveRoom{})
}

func (s *GormStore) CreateRoom(room *model.LiveRoom) error {
	return s.db.Create(room).Error
}

func (s *GormStore) FindRoomByID(roomID string) (*model.LiveRoom, error) {
	var room model.LiveRoom
	if err := s.db.First(&room, "id = ?", roomID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &room, nil
}

func (s *GormStore) FindRoomByMerchantID(merchantID string) (*model.LiveRoom, error) {
	var room model.LiveRoom
	if err := s.db.First(&room, "merchant_id = ?", merchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &room, nil
}

func (s *GormStore) UpdateRoom(room *model.LiveRoom) error {
	return s.db.Save(room).Error
}

func (s *GormStore) ListRooms(status model.RoomStatus) ([]*model.LiveRoom, error) {
	var rooms []*model.LiveRoom
	db := s.db.Model(&model.LiveRoom{})
	if status != "" {
		db = db.Where("status = ?", status)
	}
	if err := db.Order("created_at DESC").Find(&rooms).Error; err != nil {
		return nil, err
	}
	return rooms, nil
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./internal/app/room/dao/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/app/room/dao/room.go
git commit -m "feat(room): add DAO Store interface and GormStore"
```

---

### Task 3: dto/room.go

**Files:**
- Create: `internal/app/room/dto/room.go`

- [ ] **Step 1: Write the DTO file**

```go
package dto

import (
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
)

type CreateRoomInput struct {
	Title string
}

type CreateRoomRequest struct {
	Title string `json:"title" binding:"required,min=1,max=128"`
}

func (r CreateRoomRequest) Input() CreateRoomInput {
	return CreateRoomInput{Title: r.Title}
}

type RoomDetailDTO struct {
	ID            string           `json:"id"`
	MerchantID    string           `json:"merchant_id"`
	Title         string           `json:"title"`
	Status        model.RoomStatus `json:"status"`
	CurrentItemID string           `json:"current_item_id,omitempty"`
	OnlineCount   int              `json:"online_count"`
	ItemQueue     []string         `json:"item_queue"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type MerchantRoomDTO struct {
	ID            string           `json:"id"`
	MerchantID    string           `json:"merchant_id"`
	Title         string           `json:"title"`
	Status        model.RoomStatus `json:"status"`
	StatusText    string           `json:"status_text"`
	CurrentItemID string           `json:"current_item_id,omitempty"`
	OnlineCount   int              `json:"online_count"`
	QueuedCount   int              `json:"queued_count"`
	Actions       RoomActionsDTO   `json:"actions"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type RoomActionsDTO struct {
	CanStart bool `json:"can_start"`
	CanEnd   bool `json:"can_end"`
}

func NewRoomDetailDTO(room *model.LiveRoom, onlineCount int, itemQueue []string) RoomDetailDTO {
	if itemQueue == nil {
		itemQueue = []string{}
	}
	return RoomDetailDTO{
		ID:            room.ID,
		MerchantID:    room.MerchantID,
		Title:         room.Title,
		Status:        room.Status,
		CurrentItemID: room.CurrentItemID,
		OnlineCount:   onlineCount,
		ItemQueue:     itemQueue,
		CreatedAt:     room.CreatedAt,
		UpdatedAt:     room.UpdatedAt,
	}
}

func NewMerchantRoomDTO(room *model.LiveRoom, onlineCount int, queuedCount int) MerchantRoomDTO {
	return MerchantRoomDTO{
		ID:            room.ID,
		MerchantID:    room.MerchantID,
		Title:         room.Title,
		Status:        room.Status,
		StatusText:    roomStatusText(room.Status),
		CurrentItemID: room.CurrentItemID,
		OnlineCount:   onlineCount,
		QueuedCount:   queuedCount,
		Actions: RoomActionsDTO{
			CanStart: room.Status == model.RoomIdle,
			CanEnd:   room.Status == model.RoomLive,
		},
		CreatedAt: room.CreatedAt,
		UpdatedAt: room.UpdatedAt,
	}
}

func roomStatusText(status model.RoomStatus) string {
	switch status {
	case model.RoomIdle:
		return "未开播"
	case model.RoomLive:
		return "直播中"
	default:
		return string(status)
	}
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./internal/app/room/dto/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/app/room/dto/room.go
git commit -m "feat(room): add DTO types and constructors"
```

---

### Task 4: cache/cache.go

**Files:**
- Create: `internal/app/room/cache/cache.go`

- [ ] **Step 1: Write the cache file**

```go
package cache

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const (
	keyRoomState = "auction:room:%s:state"
	keyItemQueue = "auction:room:%s:item_queue"
)

type RoomState struct {
	MerchantID    string
	Status        string
	CurrentItemID string
	OnlineCount   int
}

type Cache interface {
	InitRoomState(ctx context.Context, roomID string, state RoomState) error
	GetRoomState(ctx context.Context, roomID string) (*RoomState, bool, error)
	UpdateRoomStatus(ctx context.Context, roomID, status string) error
	GetItemQueue(ctx context.Context, roomID string) ([]string, error)
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

func (c *RedisCache) InitRoomState(ctx context.Context, roomID string, state RoomState) error {
	key := fmt.Sprintf(keyRoomState, roomID)
	return c.client.HSet(ctx, key,
		"merchant_id", state.MerchantID,
		"status", state.Status,
		"current_item_id", state.CurrentItemID,
		"online_count", "0",
	).Err()
}

func (c *RedisCache) GetRoomState(ctx context.Context, roomID string) (*RoomState, bool, error) {
	key := fmt.Sprintf(keyRoomState, roomID)
	result, err := c.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, false, err
	}
	if len(result) == 0 {
		return nil, false, nil
	}
	onlineCount, _ := strconv.Atoi(result["online_count"])
	return &RoomState{
		MerchantID:    result["merchant_id"],
		Status:        result["status"],
		CurrentItemID: result["current_item_id"],
		OnlineCount:   onlineCount,
	}, true, nil
}

func (c *RedisCache) UpdateRoomStatus(ctx context.Context, roomID, status string) error {
	key := fmt.Sprintf(keyRoomState, roomID)
	return c.client.HSet(ctx, key, "status", status).Err()
}

func (c *RedisCache) GetItemQueue(ctx context.Context, roomID string) ([]string, error) {
	key := fmt.Sprintf(keyItemQueue, roomID)
	result, err := c.client.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return []string{}, err
	}
	return result, nil
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./internal/app/room/cache/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/app/room/cache/cache.go
git commit -m "feat(room): add Cache interface and RedisCache"
```

---

### Task 5: Write failing tests

**Files:**
- Create: `internal/app/room/service/service_test.go`
- Create: `internal/app/room/service/service.go` (skeleton only — just enough to compile)

- [ ] **Step 1: Write the skeleton service.go so tests compile**

```go
package service

import (
	"context"
	"errors"
	"strings"

	roomcache "github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store dao.Store
	cache roomcache.Cache
}

func NewService(store dao.Store, cache roomcache.Cache) *Service {
	return &Service{store: store, cache: cache}
}

func (s *Service) ActivateRoom(current *usermodel.User, input dto.CreateRoomInput) (*dto.MerchantRoomDTO, error) {
	panic("not implemented")
}

func (s *Service) GetMerchantRoom(current *usermodel.User) (*dto.MerchantRoomDTO, error) {
	panic("not implemented")
}

func (s *Service) StartRoom(current *usermodel.User, roomID string) error {
	panic("not implemented")
}

func (s *Service) EndRoom(current *usermodel.User, roomID string) error {
	panic("not implemented")
}

func (s *Service) GetRoom(roomID string) (*dto.RoomDetailDTO, error) {
	panic("not implemented")
}

func (s *Service) ListRooms(statusFilter model.RoomStatus) ([]*dto.RoomDetailDTO, error) {
	panic("not implemented")
}

func (s *Service) findMerchantRoom(current *usermodel.User, roomID string) (*model.LiveRoom, error) {
	panic("not implemented")
}

func isMerchant(current *usermodel.User) bool {
	return current != nil && current.Identity == usermodel.IdentityMerchant
}

// keep compiler happy — used in Task 6 implementations
var _ = context.Background
var _ = errors.Is
var _ = strings.TrimSpace
var _ = snowflake.MakeUUID
var _ = errorx.ErrUnauthorized
```

- [ ] **Step 2: Write service_test.go with fakeStore, fakeCache, and all 11 tests**

```go
package service

import (
	"context"
	"errors"
	"testing"

	roomcache "github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

// ─── fakeStore ────────────────────────────────────────────────────────────────

type fakeStore struct {
	rooms      map[string]*model.LiveRoom
	byMerchant map[string]*model.LiveRoom
	createErr  error
	updateErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		rooms:      map[string]*model.LiveRoom{},
		byMerchant: map[string]*model.LiveRoom{},
	}
}

func (s *fakeStore) AutoMigrate() error { return nil }

func (s *fakeStore) CreateRoom(room *model.LiveRoom) error {
	if s.createErr != nil {
		return s.createErr
	}
	cp := *room
	s.rooms[room.ID] = &cp
	s.byMerchant[room.MerchantID] = &cp
	return nil
}

func (s *fakeStore) FindRoomByID(roomID string) (*model.LiveRoom, error) {
	r, ok := s.rooms[roomID]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *fakeStore) FindRoomByMerchantID(merchantID string) (*model.LiveRoom, error) {
	r, ok := s.byMerchant[merchantID]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *fakeStore) UpdateRoom(room *model.LiveRoom) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	cp := *room
	s.rooms[room.ID] = &cp
	s.byMerchant[room.MerchantID] = &cp
	return nil
}

func (s *fakeStore) ListRooms(status model.RoomStatus) ([]*model.LiveRoom, error) {
	var result []*model.LiveRoom
	for _, r := range s.rooms {
		if status == "" || r.Status == status {
			cp := *r
			result = append(result, &cp)
		}
	}
	return result, nil
}

// ─── fakeCache ────────────────────────────────────────────────────────────────

type fakeCache struct {
	states    map[string]*roomcache.RoomState
	queues    map[string][]string
	initErr   error
	updateErr error
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		states: map[string]*roomcache.RoomState{},
		queues: map[string][]string{},
	}
}

func (c *fakeCache) InitRoomState(_ context.Context, roomID string, state roomcache.RoomState) error {
	if c.initErr != nil {
		return c.initErr
	}
	cp := state
	c.states[roomID] = &cp
	return nil
}

func (c *fakeCache) GetRoomState(_ context.Context, roomID string) (*roomcache.RoomState, bool, error) {
	s, ok := c.states[roomID]
	if !ok {
		return nil, false, nil
	}
	cp := *s
	return &cp, true, nil
}

func (c *fakeCache) UpdateRoomStatus(_ context.Context, roomID, status string) error {
	if c.updateErr != nil {
		return c.updateErr
	}
	if s, ok := c.states[roomID]; ok {
		s.Status = status
	} else {
		c.states[roomID] = &roomcache.RoomState{Status: status}
	}
	return nil
}

func (c *fakeCache) GetItemQueue(_ context.Context, roomID string) ([]string, error) {
	q := c.queues[roomID]
	if q == nil {
		return []string{}, nil
	}
	return q, nil
}

// ─── tests ────────────────────────────────────────────────────────────────────

func merchant() *usermodel.User {
	return &usermodel.User{ID: "merch_1", Identity: usermodel.IdentityMerchant}
}

func TestActivateRoomRequiresMerchant(t *testing.T) {
	svc := NewService(newFakeStore(), newFakeCache())
	user := &usermodel.User{ID: "user_1", Identity: usermodel.IdentityUser}
	_, err := svc.ActivateRoom(user, dto.CreateRoomInput{Title: "My Room"})
	if !errors.Is(err, errorx.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestActivateRoomIsIdempotent(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r1, err := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	r2, err := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "Different Title"})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if r1.ID != r2.ID {
		t.Fatalf("expected same room_id, got %q and %q", r1.ID, r2.ID)
	}
	if len(store.rooms) != 1 {
		t.Fatalf("expected 1 room in store, got %d", len(store.rooms))
	}
}

func TestStartRoomTransitionsToLive(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	if err := svc.StartRoom(m, r.ID); err != nil {
		t.Fatalf("StartRoom: %v", err)
	}

	room, _ := store.FindRoomByID(r.ID)
	if room.Status != model.RoomLive {
		t.Fatalf("expected live, got %q", room.Status)
	}
}

func TestStartRoomRejectsNonIdle(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(m, r.ID)

	if err := svc.StartRoom(m, r.ID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEndRoomTransitionsToIdle(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(m, r.ID)

	if err := svc.EndRoom(m, r.ID); err != nil {
		t.Fatalf("EndRoom: %v", err)
	}

	room, _ := store.FindRoomByID(r.ID)
	if room.Status != model.RoomIdle {
		t.Fatalf("expected idle, got %q", room.Status)
	}
}

func TestEndRoomRejectsNonLive(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})

	if err := svc.EndRoom(m, r.ID); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestStartRoomInitializesRedisState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(m, r.ID)

	state, ok := fc.states[r.ID]
	if !ok {
		t.Fatal("expected Redis state to be initialized after StartRoom")
	}
	if state.Status != "live" {
		t.Fatalf("expected status live, got %q", state.Status)
	}
}

func TestEndRoomUpdatesRedisStatus(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	_ = svc.StartRoom(m, r.ID)
	_ = svc.EndRoom(m, r.ID)

	state, ok := fc.states[r.ID]
	if !ok {
		t.Fatal("expected Redis state after EndRoom")
	}
	if state.Status != "idle" {
		t.Fatalf("expected status idle, got %q", state.Status)
	}
}

func TestGetRoomEnrichesOnlineCountFromCache(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	fc.states[r.ID] = &roomcache.RoomState{Status: "live", OnlineCount: 42}

	result, err := svc.GetRoom(r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if result.OnlineCount != 42 {
		t.Fatalf("expected online_count 42, got %d", result.OnlineCount)
	}
}

func TestGetRoomReturnsItemQueue(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, fc)
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})
	fc.queues[r.ID] = []string{"item_1", "item_2"}

	result, err := svc.GetRoom(r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if len(result.ItemQueue) != 2 || result.ItemQueue[0] != "item_1" {
		t.Fatalf("expected item_queue [item_1 item_2], got %v", result.ItemQueue)
	}
}

func TestGetRoomFallsBackWhenCacheMiss(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, newFakeCache())
	m := merchant()

	r, _ := svc.ActivateRoom(m, dto.CreateRoomInput{Title: "My Room"})

	result, err := svc.GetRoom(r.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if result.OnlineCount != 0 {
		t.Fatalf("expected online_count 0, got %d", result.OnlineCount)
	}
	if len(result.ItemQueue) != 0 {
		t.Fatalf("expected empty item_queue, got %v", result.ItemQueue)
	}
}
```

- [ ] **Step 3: Run tests to confirm they all panic (fail)**

```bash
go test ./internal/app/room/service/... -v 2>&1 | head -60
```

Expected: all 11 tests fail with `panic: not implemented` (or similar).

- [ ] **Step 4: Commit the skeleton and tests**

```bash
git add internal/app/room/service/service.go internal/app/room/service/service_test.go
git commit -m "test(room): add failing service tests"
```

---

### Task 6: Implement service.go

**Files:**
- Modify: `internal/app/room/service/service.go`

- [ ] **Step 1: Replace the skeleton service.go with the full implementation**

Replace the entire file content:

```go
package service

import (
	"context"
	"errors"
	"strings"

	roomcache "github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store dao.Store
	cache roomcache.Cache
}

func NewService(store dao.Store, cache roomcache.Cache) *Service {
	return &Service{store: store, cache: cache}
}

func (s *Service) ActivateRoom(current *usermodel.User, input dto.CreateRoomInput) (*dto.MerchantRoomDTO, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	existing, err := s.store.FindRoomByMerchantID(current.ID)
	if err == nil {
		onlineCount := 0
		if state, ok, _ := s.cache.GetRoomState(context.Background(), existing.ID); ok {
			onlineCount = state.OnlineCount
		}
		itemQueue, _ := s.cache.GetItemQueue(context.Background(), existing.ID)
		result := dto.NewMerchantRoomDTO(existing, onlineCount, len(itemQueue))
		return &result, nil
	}
	if !errors.Is(err, errorx.ErrNotFound) {
		return nil, err
	}
	room := &model.LiveRoom{
		ID:         "room_" + snowflake.MakeUUID(),
		MerchantID: current.ID,
		Title:      strings.TrimSpace(input.Title),
		Status:     model.RoomIdle,
	}
	if err := s.store.CreateRoom(room); err != nil {
		return nil, err
	}
	result := dto.NewMerchantRoomDTO(room, 0, 0)
	return &result, nil
}

func (s *Service) GetMerchantRoom(current *usermodel.User) (*dto.MerchantRoomDTO, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	room, err := s.store.FindRoomByMerchantID(current.ID)
	if err != nil {
		return nil, err
	}
	onlineCount := 0
	if state, ok, _ := s.cache.GetRoomState(context.Background(), room.ID); ok {
		onlineCount = state.OnlineCount
	}
	itemQueue, _ := s.cache.GetItemQueue(context.Background(), room.ID)
	result := dto.NewMerchantRoomDTO(room, onlineCount, len(itemQueue))
	return &result, nil
}

func (s *Service) StartRoom(current *usermodel.User, roomID string) error {
	room, err := s.findMerchantRoom(current, roomID)
	if err != nil {
		return err
	}
	if room.Status != model.RoomIdle {
		return errorx.ErrInvalidRequest
	}
	room.Status = model.RoomLive
	if err := s.store.UpdateRoom(room); err != nil {
		return err
	}
	if initErr := s.cache.InitRoomState(context.Background(), room.ID, roomcache.RoomState{
		MerchantID: room.MerchantID,
		Status:     "live",
	}); initErr != nil {
		_ = initErr // soft fail
	}
	return nil
}

func (s *Service) EndRoom(current *usermodel.User, roomID string) error {
	room, err := s.findMerchantRoom(current, roomID)
	if err != nil {
		return err
	}
	if room.Status != model.RoomLive {
		return errorx.ErrInvalidRequest
	}
	room.Status = model.RoomIdle
	if err := s.store.UpdateRoom(room); err != nil {
		return err
	}
	if updateErr := s.cache.UpdateRoomStatus(context.Background(), room.ID, "idle"); updateErr != nil {
		_ = updateErr // soft fail
	}
	return nil
}

func (s *Service) GetRoom(roomID string) (*dto.RoomDetailDTO, error) {
	room, err := s.store.FindRoomByID(strings.TrimSpace(roomID))
	if err != nil {
		return nil, err
	}
	onlineCount := 0
	if state, ok, _ := s.cache.GetRoomState(context.Background(), room.ID); ok {
		onlineCount = state.OnlineCount
	}
	itemQueue, _ := s.cache.GetItemQueue(context.Background(), room.ID)
	result := dto.NewRoomDetailDTO(room, onlineCount, itemQueue)
	return &result, nil
}

func (s *Service) ListRooms(statusFilter model.RoomStatus) ([]*dto.RoomDetailDTO, error) {
	if statusFilter == "" {
		statusFilter = model.RoomLive
	}
	rooms, err := s.store.ListRooms(statusFilter)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.RoomDetailDTO, 0, len(rooms))
	for _, room := range rooms {
		onlineCount := 0
		if state, ok, _ := s.cache.GetRoomState(context.Background(), room.ID); ok {
			onlineCount = state.OnlineCount
		}
		itemQueue, _ := s.cache.GetItemQueue(context.Background(), room.ID)
		d := dto.NewRoomDetailDTO(room, onlineCount, itemQueue)
		result = append(result, &d)
	}
	return result, nil
}

func (s *Service) findMerchantRoom(current *usermodel.User, roomID string) (*model.LiveRoom, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	room, err := s.store.FindRoomByID(strings.TrimSpace(roomID))
	if err != nil {
		return nil, err
	}
	if room.MerchantID != current.ID {
		return nil, errorx.ErrNotFound
	}
	return room, nil
}

func isMerchant(current *usermodel.User) bool {
	return current != nil && current.Identity == usermodel.IdentityMerchant
}
```

- [ ] **Step 2: Run tests — all 11 must pass**

```bash
go test ./internal/app/room/service/... -v
```

Expected: all 11 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/app/room/service/service.go
git commit -m "feat(room): implement service methods — all tests pass"
```

---

### Task 7: handler, router, init, and module registration

**Files:**
- Create: `internal/app/room/handler/room.go`
- Create: `internal/app/room/router/room.go`
- Create: `internal/app/room/init.go`
- Create: `internal/app/appInitialize/room.go`

- [ ] **Step 1: Write handler/room.go**

```go
package handler

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/model"
	"github.com/zet-plane/live-auction-backend/internal/app/room/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var svc *service.Service

func Init(s *service.Service) { svc = s }

func ActivateRoom(r flamego.Render, current *usermodel.User, body dto.CreateRoomRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.ActivateRoom(current, body.Input())
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func GetMerchantRoom(r flamego.Render, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetMerchantRoom(current)
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func StartRoom(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.StartRoom(current, c.Param("room_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func EndRoom(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := svc.EndRoom(current, c.Param("room_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func GetRoom(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetRoom(c.Param("room_id"))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func ListRooms(r flamego.Render, c flamego.Context) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	statusFilter := model.RoomStatus(c.Query("status"))
	result, err := svc.ListRooms(statusFilter)
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
```

- [ ] **Step 2: Write router/room.go**

```go
package router

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/room/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)

	f.Get("/api/v1/rooms", handler.ListRooms)
	f.Get("/api/v1/rooms/{room_id}", handler.GetRoom)
	f.Group("/api/v1", func() {
		f.Post("/merchant/room", binding.JSON(dto.CreateRoomRequest{}), handler.ActivateRoom)
		f.Get("/merchant/room", handler.GetMerchantRoom)
		f.Post("/rooms/{room_id}/start", handler.StartRoom)
		f.Post("/rooms/{room_id}/end", handler.EndRoom)
	}, auth)
}
```

- [ ] **Step 3: Write init.go**

```go
package room

import (
	"context"
	"errors"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/room/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/room/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/room/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/room/router"
	"github.com/zet-plane/live-auction-backend/internal/app/room/service"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

var ErrEmptyDatabase = errors.New("database pointer is nil")

type Room struct {
	Name string
	app.UnimplementedModule
}

func (r *Room) Info() string { return r.Name }

func (r *Room) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return ErrEmptyDatabase
	}
	store := dao.NewGormStore(engine.DB)
	return store.AutoMigrate()
}

func (r *Room) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)
	c := cache.NewRedisCache(engine.Cache)
	svc := service.NewService(store, c)
	handler.Init(svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (r *Room) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
```

- [ ] **Step 4: Write appInitialize/room.go**

```go
package appInitialize

import "github.com/zet-plane/live-auction-backend/internal/app/room"

func init() {
	apps = append(apps, &room.Room{Name: "room"})
}
```

- [ ] **Step 5: Build the whole project**

```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 6: Run all tests**

```bash
go test ./...
```

Expected: all tests pass, no failures.

- [ ] **Step 7: Commit**

```bash
git add internal/app/room/handler/room.go \
        internal/app/room/router/room.go \
        internal/app/room/init.go \
        internal/app/appInitialize/room.go
git commit -m "feat(room): wire handler, router, module init — room module complete"
```
