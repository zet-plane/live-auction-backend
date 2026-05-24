# Deposit Before Bid Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an independent deposit module and require a paid item deposit before a user can bid when `AuctionRule.DepositAmount > 0`.

**Architecture:** `deposit` owns deposit records and payment state. `item.Service.PlaceBid` receives a narrow deposit checker dependency and calls it before Redis bidding. Deposit payment amount is derived from `auction_rules.deposit_amount`, not client input.

**Tech Stack:** Go, GORM, Flamego, existing `pkg/errorx`, existing module lifecycle pattern, service tests with fake stores.

---

## File Structure

- Create `internal/app/deposit/model/deposit.go`: `Deposit` GORM model and status constants.
- Create `internal/app/deposit/dto/deposit.go`: response DTO and constructors.
- Create `internal/app/deposit/dao/deposit.go`: deposit store interface, GORM implementation, and narrow item rule lookup.
- Create `internal/app/deposit/service/service.go`: simulated deposit payment, status query, and paid-deposit check.
- Create `internal/app/deposit/service/service_test.go`: fake-store TDD coverage for deposit service behavior.
- Create `internal/app/deposit/handler/deposit.go`: Flamego handlers for pay and status.
- Create `internal/app/deposit/router/deposit.go`: authenticated deposit routes.
- Create `internal/app/deposit/init.go`: module lifecycle, migration, service export.
- Modify `internal/app/appInitialize/init.go`: register `deposit` before `item`.
- Modify `internal/app/item/service/service.go`: add deposit checker dependency to `Service` and `NewService`.
- Modify `internal/app/item/service/bid_service.go`: enforce deposit precheck before Redis Lua.
- Modify `internal/app/item/service/service_test.go`: add fake deposit checker helper.
- Modify `internal/app/item/service/bid_service_test.go`: add bid precheck tests.
- Modify `internal/app/item/init.go`: pass `deposit.Svc` into item service.

## Task 1: Deposit Domain Model and Service Tests

**Files:**
- Create: `internal/app/deposit/model/deposit.go`
- Create: `internal/app/deposit/dto/deposit.go`
- Create: `internal/app/deposit/dao/deposit.go`
- Create: `internal/app/deposit/service/service.go`
- Create: `internal/app/deposit/service/service_test.go`

- [ ] **Step 1: Write the failing deposit service tests**

Create `internal/app/deposit/service/service_test.go`:

```go
package service

import (
	"errors"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/deposit/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

type fakeStore struct {
	deposits map[string]*model.Deposit
	amounts  map[string]int64
	now      time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		deposits: map[string]*model.Deposit{},
		amounts:  map[string]int64{},
		now:      time.Date(2026, 5, 24, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
	}
}

func depositKey(itemID, userID string) string { return itemID + "\x00" + userID }

func (s *fakeStore) AutoMigrate() error { return nil }

func (s *fakeStore) FindRequiredAmount(itemID string) (int64, error) {
	amount, ok := s.amounts[itemID]
	if !ok {
		return 0, errorx.ErrNotFound
	}
	return amount, nil
}

func (s *fakeStore) FindDeposit(itemID, userID string) (*model.Deposit, error) {
	d, ok := s.deposits[depositKey(itemID, userID)]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (s *fakeStore) CreateDeposit(d *model.Deposit) error {
	cp := *d
	s.deposits[depositKey(d.ItemID, d.UserID)] = &cp
	return nil
}

func (s *fakeStore) UpdateDeposit(d *model.Deposit) error {
	if _, ok := s.deposits[depositKey(d.ItemID, d.UserID)]; !ok {
		return errorx.ErrNotFound
	}
	cp := *d
	s.deposits[depositKey(d.ItemID, d.UserID)] = &cp
	return nil
}

func TestPayDepositCreatesPaidDepositUsingRuleAmount(t *testing.T) {
	store := newFakeStore()
	store.amounts["item_1"] = 5000
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }

	result, err := svc.PayDeposit(&usermodel.User{ID: "user_1"}, " item_1 ")
	if err != nil {
		t.Fatalf("PayDeposit returned error: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected deposit id")
	}
	if result.ItemID != "item_1" || result.UserID != "user_1" {
		t.Fatalf("unexpected deposit owner: %+v", result)
	}
	if result.Amount != 5000 {
		t.Fatalf("expected amount 5000, got %d", result.Amount)
	}
	if result.Status != model.DepositPaid {
		t.Fatalf("expected paid status, got %q", result.Status)
	}
	if result.PaidAt == nil {
		t.Fatal("expected paid_at")
	}
}

func TestPayDepositIsIdempotentWhenAlreadyPaid(t *testing.T) {
	store := newFakeStore()
	store.amounts["item_1"] = 5000
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }
	user := &usermodel.User{ID: "user_1"}

	first, err := svc.PayDeposit(user, "item_1")
	if err != nil {
		t.Fatalf("first PayDeposit returned error: %v", err)
	}
	second, err := svc.PayDeposit(user, "item_1")
	if err != nil {
		t.Fatalf("second PayDeposit returned error: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same deposit id, got %q and %q", first.ID, second.ID)
	}
	if len(store.deposits) != 1 {
		t.Fatalf("expected one deposit row, got %d", len(store.deposits))
	}
}

func TestPayDepositRejectsItemWithoutRequiredDeposit(t *testing.T) {
	store := newFakeStore()
	store.amounts["item_1"] = 0
	svc := NewService(store)

	_, err := svc.PayDeposit(&usermodel.User{ID: "user_1"}, "item_1")
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request, got %v", err)
	}
}

func TestGetMyDepositReturnsExistingDeposit(t *testing.T) {
	store := newFakeStore()
	store.amounts["item_1"] = 5000
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }
	user := &usermodel.User{ID: "user_1"}

	created, _ := svc.PayDeposit(user, "item_1")
	found, err := svc.GetMyDeposit(user, "item_1")
	if err != nil {
		t.Fatalf("GetMyDeposit returned error: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected id %q, got %q", created.ID, found.ID)
	}
}

func TestHasPaidDeposit(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store)
	paidAt := store.now
	store.deposits[depositKey("item_paid", "user_1")] = &model.Deposit{
		ID: "deposit_1", ItemID: "item_paid", UserID: "user_1", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt,
	}
	store.deposits[depositKey("item_pending", "user_1")] = &model.Deposit{
		ID: "deposit_2", ItemID: "item_pending", UserID: "user_1", Amount: 5000, Status: model.DepositPending,
	}
	store.deposits[depositKey("item_underpaid", "user_1")] = &model.Deposit{
		ID: "deposit_3", ItemID: "item_underpaid", UserID: "user_1", Amount: 1000, Status: model.DepositPaid, PaidAt: &paidAt,
	}

	tests := []struct {
		name   string
		itemID string
		amount int64
		want   bool
	}{
		{name: "zero required amount skips check", itemID: "missing", amount: 0, want: true},
		{name: "paid sufficient deposit passes", itemID: "item_paid", amount: 5000, want: true},
		{name: "missing deposit fails", itemID: "missing", amount: 5000, want: false},
		{name: "pending deposit fails", itemID: "item_pending", amount: 5000, want: false},
		{name: "underpaid deposit fails", itemID: "item_underpaid", amount: 5000, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.HasPaidDeposit(tt.itemID, "user_1", tt.amount)
			if err != nil {
				t.Fatalf("HasPaidDeposit returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run deposit service tests to verify they fail**

Run:

```bash
rtk go test ./internal/app/deposit/service/... -run "TestPayDeposit|TestGetMyDeposit|TestHasPaidDeposit" -v
```

Expected: FAIL because the `deposit` package files do not exist yet.

- [ ] **Step 3: Add deposit model**

Create `internal/app/deposit/model/deposit.go`:

```go
package model

import "time"

type DepositStatus string

const (
	DepositPending   DepositStatus = "pending"
	DepositPaid      DepositStatus = "paid"
	DepositRefunded  DepositStatus = "refunded"
	DepositForfeited DepositStatus = "forfeited"
)

type Deposit struct {
	ID         string        `gorm:"primaryKey;size:64" json:"id"`
	ItemID     string        `gorm:"uniqueIndex:idx_deposit_item_user;index;size:64;not null" json:"item_id"`
	UserID     string        `gorm:"uniqueIndex:idx_deposit_item_user;index;size:64;not null" json:"user_id"`
	Amount     int64         `gorm:"not null" json:"amount"`
	Status     DepositStatus `gorm:"index;size:32;not null" json:"status"`
	PaidAt     *time.Time    `json:"paid_at"`
	RefundedAt *time.Time    `json:"refunded_at"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}
```

- [ ] **Step 4: Add deposit DTO**

Create `internal/app/deposit/dto/deposit.go`:

```go
package dto

import (
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/deposit/model"
)

type DepositDetail struct {
	ID         string              `json:"id"`
	ItemID     string              `json:"item_id"`
	UserID     string              `json:"user_id"`
	Amount     int64               `json:"amount"`
	Status     model.DepositStatus `json:"status"`
	PaidAt     *time.Time          `json:"paid_at"`
	RefundedAt *time.Time          `json:"refunded_at"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

func NewDepositDetail(d *model.Deposit) *DepositDetail {
	if d == nil {
		return nil
	}
	return &DepositDetail{
		ID:         d.ID,
		ItemID:     d.ItemID,
		UserID:     d.UserID,
		Amount:     d.Amount,
		Status:     d.Status,
		PaidAt:     d.PaidAt,
		RefundedAt: d.RefundedAt,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	}
}
```

- [ ] **Step 5: Add deposit DAO**

Create `internal/app/deposit/dao/deposit.go`:

```go
package dao

import (
	"errors"

	"github.com/zet-plane/live-auction-backend/internal/app/deposit/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"gorm.io/gorm"
)

type Store interface {
	AutoMigrate() error
	FindRequiredAmount(itemID string) (int64, error)
	FindDeposit(itemID, userID string) (*model.Deposit, error)
	CreateDeposit(deposit *model.Deposit) error
	UpdateDeposit(deposit *model.Deposit) error
}

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) AutoMigrate() error {
	return s.db.AutoMigrate(&model.Deposit{})
}

func (s *GormStore) FindRequiredAmount(itemID string) (int64, error) {
	var row struct {
		DepositAmount int64 `gorm:"column:deposit_amount"`
	}
	err := s.db.Table("auction_rules").
		Select("auction_rules.deposit_amount").
		Joins("JOIN auction_items ON auction_items.rule_id = auction_rules.id AND auction_items.deleted_at IS NULL").
		Where("auction_items.id = ?", itemID).
		Scan(&row).Error
	if err != nil {
		return 0, err
	}
	var count int64
	if err := s.db.Table("auction_items").Where("id = ? AND deleted_at IS NULL", itemID).Count(&count).Error; err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, errorx.ErrNotFound
	}
	return row.DepositAmount, nil
}

func (s *GormStore) FindDeposit(itemID, userID string) (*model.Deposit, error) {
	var d model.Deposit
	if err := s.db.First(&d, "item_id = ? AND user_id = ?", itemID, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (s *GormStore) CreateDeposit(deposit *model.Deposit) error {
	return s.db.Create(deposit).Error
}

func (s *GormStore) UpdateDeposit(deposit *model.Deposit) error {
	return s.db.Save(deposit).Error
}
```

- [ ] **Step 6: Add deposit service implementation**

Create `internal/app/deposit/service/service.go`:

```go
package service

import (
	"errors"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/deposit/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store dao.Store
	now   func() time.Time
}

func NewService(store dao.Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (s *Service) PayDeposit(current *usermodel.User, itemID string) (*dto.DepositDetail, error) {
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return nil, errorx.ErrUnauthorized
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, errorx.ErrInvalidRequest
	}
	amount, err := s.store.FindRequiredAmount(itemID)
	if err != nil {
		return nil, err
	}
	if amount <= 0 {
		return nil, errorx.ErrInvalidRequest
	}

	existing, err := s.store.FindDeposit(itemID, current.ID)
	if err == nil {
		if existing.Status == model.DepositPaid && existing.Amount >= amount {
			return dto.NewDepositDetail(existing), nil
		}
		if existing.Status == model.DepositRefunded || existing.Status == model.DepositForfeited {
			return nil, errorx.ErrInvalidRequest
		}
		now := s.now()
		existing.Amount = amount
		existing.Status = model.DepositPaid
		existing.PaidAt = &now
		if err := s.store.UpdateDeposit(existing); err != nil {
			return nil, err
		}
		return dto.NewDepositDetail(existing), nil
	}
	if !errors.Is(err, errorx.ErrNotFound) {
		return nil, err
	}

	now := s.now()
	deposit := &model.Deposit{
		ID:     "deposit_" + snowflake.MakeUUID(),
		ItemID: itemID,
		UserID: current.ID,
		Amount: amount,
		Status: model.DepositPaid,
		PaidAt: &now,
	}
	if err := s.store.CreateDeposit(deposit); err != nil {
		return nil, err
	}
	return dto.NewDepositDetail(deposit), nil
}

func (s *Service) GetMyDeposit(current *usermodel.User, itemID string) (*dto.DepositDetail, error) {
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return nil, errorx.ErrUnauthorized
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, errorx.ErrInvalidRequest
	}
	deposit, err := s.store.FindDeposit(itemID, current.ID)
	if err != nil {
		return nil, err
	}
	return dto.NewDepositDetail(deposit), nil
}

func (s *Service) HasPaidDeposit(itemID, userID string, requiredAmount int64) (bool, error) {
	if requiredAmount <= 0 {
		return true, nil
	}
	itemID = strings.TrimSpace(itemID)
	userID = strings.TrimSpace(userID)
	if itemID == "" || userID == "" {
		return false, nil
	}
	deposit, err := s.store.FindDeposit(itemID, userID)
	if errors.Is(err, errorx.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return deposit.Status == model.DepositPaid && deposit.Amount >= requiredAmount, nil
}
```

- [ ] **Step 7: Run deposit service tests to verify they pass**

Run:

```bash
rtk go test ./internal/app/deposit/service/... -run "TestPayDeposit|TestGetMyDeposit|TestHasPaidDeposit" -v
```

Expected: PASS.

- [ ] **Step 8: Commit Task 1**

Run:

```bash
rtk git add internal/app/deposit/model/deposit.go internal/app/deposit/dto/deposit.go internal/app/deposit/dao/deposit.go internal/app/deposit/service/service.go internal/app/deposit/service/service_test.go
rtk git commit -m "feat(deposit): add deposit service"
```

## Task 2: Deposit HTTP Module Wiring

**Files:**
- Create: `internal/app/deposit/handler/deposit.go`
- Create: `internal/app/deposit/router/deposit.go`
- Create: `internal/app/deposit/init.go`
- Modify: `internal/app/appInitialize/init.go`

- [ ] **Step 1: Add deposit handlers**

Create `internal/app/deposit/handler/deposit.go`:

```go
package handler

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var svc *service.Service

func Init(s *service.Service) {
	svc = s
}

func PayDeposit(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.PayDeposit(current, c.Param("item_id"))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func GetMyDeposit(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	result, err := svc.GetMyDeposit(current, c.Param("item_id"))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
```

- [ ] **Step 2: Add deposit router**

Create `internal/app/deposit/router/deposit.go`:

```go
package router

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)
	f.Group("/api/v1", func() {
		f.Post("/items/{item_id}/deposit/pay", handler.PayDeposit)
		f.Get("/items/{item_id}/deposit", handler.GetMyDeposit)
	}, auth)
}
```

- [ ] **Step 3: Add deposit module lifecycle**

Create `internal/app/deposit/init.go`:

```go
package deposit

import (
	"context"
	"errors"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/router"
	"github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

var Svc *service.Service

var errNilDB = errors.New("database pointer is nil")

type Deposit struct {
	Name string
	app.UnimplementedModule
}

func (d *Deposit) Info() string { return d.Name }

func (d *Deposit) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return errNilDB
	}
	return dao.NewGormStore(engine.DB).AutoMigrate()
}

func (d *Deposit) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)
	Svc = service.NewService(store)
	handler.Init(Svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (d *Deposit) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
```

- [ ] **Step 4: Register deposit module before item**

Modify `internal/app/appInitialize/init.go`:

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
)

var apps = []app.Module{
	&user.User{Name: "user"},
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

- [ ] **Step 5: Run package tests/build for deposit wiring**

Run:

```bash
rtk go test ./internal/app/deposit/...
rtk go test ./internal/app/appInitialize/...
```

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

Run:

```bash
rtk git add internal/app/deposit/handler/deposit.go internal/app/deposit/router/deposit.go internal/app/deposit/init.go internal/app/appInitialize/init.go
rtk git commit -m "feat(deposit): register deposit HTTP module"
```

## Task 3: Item Bid Deposit Precheck

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/service_test.go`
- Modify: `internal/app/item/service/bid_service_test.go`
- Modify: `internal/app/item/init.go`

- [ ] **Step 1: Write failing item service tests**

Append these tests and helpers to `internal/app/item/service/bid_service_test.go`:

```go
type fakeDepositChecker struct {
	paid  bool
	err   error
	calls int
}

func (f *fakeDepositChecker) HasPaidDeposit(itemID, userID string, requiredAmount int64) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.paid, nil
}

func TestPlaceBidSkipsDepositCheckWhenNotRequired(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	deposits := &fakeDepositChecker{paid: false}
	svc := NewService(store, testPolicy, fc, nil, deposits)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "no_deposit_required", UserName: "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if deposits.calls != 0 {
		t.Fatalf("expected deposit checker not to be called, got %d calls", deposits.calls)
	}
}

func TestPlaceBidRejectsMissingDepositBeforeRedis(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	deposits := &fakeDepositChecker{paid: false}
	svc := NewService(store, testPolicy, fc, nil, deposits)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)
	rule := store.rules[store.items[itemID].RuleID]
	rule.DepositAmount = 5000

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "missing_deposit", UserName: "Alice",
	})
	if err == nil {
		t.Fatal("expected deposit required error")
	}
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40005 {
		t.Fatalf("expected code 40005, got %v", err)
	}
	if deposits.calls != 1 {
		t.Fatalf("expected one deposit checker call, got %d", deposits.calls)
	}
	if len(store.bidLogs) != 0 {
		t.Fatalf("expected no bid logs, got %d", len(store.bidLogs))
	}
	if len(fc.ranking[itemID]) != 0 {
		t.Fatalf("expected Redis fake ranking not to record bids, got %d", len(fc.ranking[itemID]))
	}
}

func TestPlaceBidAllowsPaidDeposit(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	deposits := &fakeDepositChecker{paid: true}
	svc := NewService(store, testPolicy, fc, nil, deposits)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)
	rule := store.rules[store.items[itemID].RuleID]
	rule.DepositAmount = 5000

	result, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "paid_deposit", UserName: "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if result.CurrentPrice != 100 {
		t.Fatalf("expected current price 100, got %d", result.CurrentPrice)
	}
	if deposits.calls != 1 {
		t.Fatalf("expected one deposit checker call, got %d", deposits.calls)
	}
	if len(store.bidLogs) != 1 {
		t.Fatalf("expected one bid log, got %d", len(store.bidLogs))
	}
}
```

- [ ] **Step 2: Run item bid tests to verify they fail**

Run:

```bash
rtk go test ./internal/app/item/service/... -run "TestPlaceBidSkipsDepositCheckWhenNotRequired|TestPlaceBidRejectsMissingDepositBeforeRedis|TestPlaceBidAllowsPaidDeposit" -v
```

Expected: FAIL because `NewService` does not accept a deposit checker yet.

- [ ] **Step 3: Add deposit checker dependency to item service**

Modify the top of `internal/app/item/service/service.go` so the imports and service shape include deposit:

```go
import (
	"context"
	"strings"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	orderservice "github.com/zet-plane/live-auction-backend/internal/app/order/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type DepositChecker interface {
	HasPaidDeposit(itemID, userID string, requiredAmount int64) (bool, error)
}

type Service struct {
	store      dao.Store
	cache      itemcache.Cache
	policy     dto.AuctionPolicy
	now        func() time.Time
	orderSvc   *orderservice.Service
	depositSvc DepositChecker
}

func NewService(store dao.Store, policy dto.AuctionPolicy, cache itemcache.Cache, orderSvc *orderservice.Service, depositSvc DepositChecker) *Service {
	return &Service{
		store:      store,
		cache:      cache,
		policy:     policy,
		now:        time.Now,
		orderSvc:   orderSvc,
		depositSvc: depositSvc,
	}
}
```

Update all existing `NewService` call sites in item tests. There are call sites in
`internal/app/item/service/service_test.go` and
`internal/app/item/service/bid_service_test.go`.

Change this shape:

```go
svc := NewService(store, testPolicy, fc, nil)
```

to:

```go
svc := NewService(store, testPolicy, fc, nil, nil)
```

Change this shape:

```go
svc := NewService(store, itemdto.AuctionPolicy{}, nil, nil)
```

to:

```go
svc := NewService(store, itemdto.AuctionPolicy{}, nil, nil, nil)
```

- [ ] **Step 4: Add deposit required error and precheck**

Modify `internal/app/item/service/bid_service.go`.

Add the package-level error after imports:

```go
var ErrDepositRequired = errorx.New(http.StatusBadRequest, 40005, "deposit required")
```

Add this check after `if item.Status != model.ItemOngoing` and before `if s.cache == nil`:

```go
	if rule.DepositAmount > 0 {
		if s.depositSvc == nil {
			return nil, errorx.ErrInternal
		}
		ok, err := s.depositSvc.HasPaidDeposit(item.ID, current.ID, rule.DepositAmount)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrDepositRequired
		}
	}
```

- [ ] **Step 5: Wire deposit service into item module**

Modify `internal/app/item/init.go`.

Add the import:

```go
	depositapp "github.com/zet-plane/live-auction-backend/internal/app/deposit"
```

Change service construction from:

```go
svc := service.NewService(store, policy, c, orderapp.Svc)
```

to:

```go
svc := service.NewService(store, policy, c, orderapp.Svc, depositapp.Svc)
```

- [ ] **Step 6: Run item service tests**

Run:

```bash
rtk go test ./internal/app/item/service/... -run "TestPlaceBid|TestGetRanking|TestCreateItem|TestPublishItem|TestStartItem|TestCancelItem" -v
```

Expected: PASS.

- [ ] **Step 7: Commit Task 3**

Run:

```bash
rtk git add internal/app/item/service/service.go internal/app/item/service/bid_service.go internal/app/item/service/service_test.go internal/app/item/service/bid_service_test.go internal/app/item/init.go
rtk git commit -m "feat(item): require paid deposit before bidding"
```

## Task 4: Full Verification and Cleanup

**Files:**
- Modify only files that fail formatting or tests.

- [ ] **Step 1: Format Go files**

Run:

```bash
rtk gofmt -w internal/app/deposit internal/app/item/service internal/app/item/init.go internal/app/appInitialize/init.go
```

Expected: command succeeds with no output.

- [ ] **Step 2: Run focused tests**

Run:

```bash
rtk go test ./internal/app/deposit/... ./internal/app/item/service/... ./internal/app/order/service/...
```

Expected: PASS.

- [ ] **Step 3: Run broader build**

Run:

```bash
rtk go test ./...
```

Expected: PASS. If this fails because unrelated packages require external services, record the failure and keep the focused test output as the primary verification.

- [ ] **Step 4: Inspect git diff**

Run:

```bash
rtk git diff --stat
rtk git diff -- internal/app/deposit internal/app/item internal/app/appInitialize docs/superpowers/plans/2026-05-24-deposit-before-bid.md
```

Expected: diff only contains deposit module, item deposit precheck wiring, app registration, and this plan.

- [ ] **Step 5: Commit final formatting or test fixes**

If Step 1 through Step 4 changed files after Task 3, run:

```bash
rtk git add internal/app/deposit internal/app/item internal/app/appInitialize
rtk git commit -m "chore: verify deposit before bid flow"
```

If there are no changes, do not create an empty commit.

## Self-Review

- Spec coverage: the plan creates the deposit module, derives payment amount from item rules, adds authenticated pay/status routes, wires deposit before item, checks paid deposits before Redis bidding, and covers the no-deposit path.
- Placeholder scan: no deferred implementation placeholders are intentionally left in the task steps.
- Type consistency: `PayDeposit(current, itemID)`, `GetMyDeposit(current, itemID)`, and `HasPaidDeposit(itemID, userID, requiredAmount)` match across DAO, service, handler, item integration, and tests.
