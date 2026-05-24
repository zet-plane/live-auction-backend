# Order & Payment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `order` and `payment` modules so that when a live auction ends, a pending order is automatically created, and the winner can pay or cancel via simulated payment.

**Architecture:** Two new modules (`order`, `payment`) following the existing `internal/app/<module>/{model,dao,dto,service,handler,router,init.go}` pattern. `order` owns the state machine and exports a package-level `Svc`; `payment` is a thin handler layer that calls `orderSvc.Pay/Cancel`. The `item` module is wired to call `orderSvc.CreateOrder` on auction end. Module load order is: user → room → order → payment → item.

**Tech Stack:** Go, GORM (MySQL), flamego, robfig/cron, pkg/snowflake, pkg/errorx

**Design doc:** `docs/superpowers/specs/2026-05-24-order-payment-design.md`

---

## File Map

**New files:**
- `internal/app/order/model/order.go`
- `internal/app/order/dto/order.go`
- `internal/app/order/dao/order.go`
- `internal/app/order/service/service.go`
- `internal/app/order/service/service_test.go`
- `internal/app/order/service/cron.go`
- `internal/app/order/service/cron_test.go`
- `internal/app/order/handler/order.go`
- `internal/app/order/router/order.go`
- `internal/app/order/init.go`
- `internal/app/payment/handler/payment.go`
- `internal/app/payment/router/payment.go`
- `internal/app/payment/init.go`

**Modified files:**
- `internal/app/appInitialize/init.go` — consolidate all registrations with explicit load order
- `internal/app/appInitialize/item.go` — delete (merged into init.go)
- `internal/app/appInitialize/room.go` — delete (merged into init.go)
- `internal/app/appInitialize/user.go` — delete (merged into init.go)
- `internal/app/item/service/service.go` — add `orderSvc` field + inject
- `internal/app/item/service/bid_service.go` — call `CreateOrder` on IsCapped
- `internal/app/item/init.go` — inject `order.Svc`, register auction-end cron

---

## Task 1: Order Model

**Files:**
- Create: `internal/app/order/model/order.go`

- [ ] **Step 1: Create the model file**

```go
package model

import "time"

type OrderStatus string

const (
	OrderPending   OrderStatus = "pending"
	OrderPaid      OrderStatus = "paid"
	OrderCancelled OrderStatus = "cancelled"
	OrderExpired   OrderStatus = "expired"
)

type Order struct {
	ID        string      `gorm:"primaryKey;size:64" json:"id"`
	ItemID    string      `gorm:"index;size:64;not null" json:"item_id"`
	UserID    string      `gorm:"index;size:64;not null" json:"user_id"`
	Price     int64       `gorm:"not null" json:"price"`
	Status    OrderStatus `gorm:"index;size:32;not null" json:"status"`
	ExpiredAt time.Time   `gorm:"index" json:"expired_at"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/app/order/...
```

Expected: no output (no other files yet, but the package itself must parse cleanly)

- [ ] **Step 3: Commit**

```bash
git add internal/app/order/model/order.go
git commit -m "feat(order): add Order model and OrderStatus constants"
```

---

## Task 2: Order DTOs

**Files:**
- Create: `internal/app/order/dto/order.go`

- [ ] **Step 1: Create the DTO file**

```go
package dto

import (
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
)

// ListOrdersInput is passed from handler → service → dao.
// Exactly one of UserID or MerchantID is set depending on caller identity.
type ListOrdersInput struct {
	UserID     string
	MerchantID string
	Status     model.OrderStatus
	Page       int
	PageSize   int
}

// OrderWithTitle is returned by ListOrders (includes item title from JOIN).
type OrderWithTitle struct {
	ID        string            `json:"id"`
	ItemID    string            `json:"item_id"`
	ItemTitle string            `json:"item_title"`
	UserID    string            `json:"user_id"`
	Price     int64             `json:"price"`
	Status    model.OrderStatus `json:"status"`
	ExpiredAt string            `json:"expired_at"`
	CreatedAt string            `json:"created_at"`
}

// OrderDetail is returned by GetOrder (adds UpdatedAt + merchant check field).
type OrderDetail struct {
	ID             string            `json:"id"`
	ItemID         string            `json:"item_id"`
	ItemTitle      string            `json:"item_title"`
	ItemMerchantID string            `json:"-"`
	UserID         string            `json:"user_id"`
	Price          int64             `json:"price"`
	Status         model.OrderStatus `json:"status"`
	ExpiredAt      string            `json:"expired_at"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
}

// EndedItemSummary is used by the compensation cron to create missing orders.
type EndedItemSummary struct {
	ItemID    string `gorm:"column:item_id"`
	WinnerID  string `gorm:"column:winner_id"`
	DealPrice int64  `gorm:"column:deal_price"`
}

// ListOrdersResult is the paginated response returned by ListOrders handler.
type ListOrdersResult struct {
	List     []OrderWithTitle `json:"list"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int64            `json:"total"`
}

// PayOrderRequest is the request body for POST /orders/{id}/pay.
type PayOrderRequest struct {
	Result string `json:"result" binding:"required"`
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/app/order/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/app/order/dto/order.go
git commit -m "feat(order): add order DTOs"
```

---

## Task 3: Order DAO

**Files:**
- Create: `internal/app/order/dao/order.go`

- [ ] **Step 1: Create Store interface and GormStore**

```go
package dao

import (
	"errors"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/page"
	"gorm.io/gorm"
)

type Store interface {
	AutoMigrate() error
	CreateOrder(order *model.Order) error
	FindOrder(orderID string) (*model.Order, error)
	FindOrderByItemID(itemID string) (*model.Order, error)
	FindOrderDetail(orderID string) (*dto.OrderDetail, error)
	UpdateOrderStatus(orderID string, from, to model.OrderStatus) (bool, error)
	ListOrders(input dto.ListOrdersInput) ([]dto.OrderWithTitle, int64, error)
	ListExpiredPendingOrders(before time.Time, limit int) ([]model.Order, error)
	ListEndedItemsWithoutOrder(limit int) ([]dto.EndedItemSummary, error)
}

type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) AutoMigrate() error {
	return s.db.AutoMigrate(&model.Order{})
}

func (s *GormStore) CreateOrder(order *model.Order) error {
	return s.db.Create(order).Error
}

func (s *GormStore) FindOrder(orderID string) (*model.Order, error) {
	var o model.Order
	if err := s.db.First(&o, "id = ?", orderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (s *GormStore) FindOrderByItemID(itemID string) (*model.Order, error) {
	var o model.Order
	if err := s.db.First(&o, "item_id = ?", itemID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errorx.ErrNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (s *GormStore) FindOrderDetail(orderID string) (*dto.OrderDetail, error) {
	var result struct {
		model.Order
		ItemTitle      string `gorm:"column:item_title"`
		ItemMerchantID string `gorm:"column:item_merchant_id"`
	}
	err := s.db.Table("orders").
		Select("orders.*, auction_items.title as item_title, auction_items.merchant_id as item_merchant_id").
		Joins("JOIN auction_items ON auction_items.id = orders.item_id AND auction_items.deleted_at IS NULL").
		Where("orders.id = ?", orderID).
		Scan(&result).Error
	if err != nil {
		return nil, err
	}
	if result.ID == "" {
		return nil, errorx.ErrNotFound
	}
	return &dto.OrderDetail{
		ID:             result.ID,
		ItemID:         result.ItemID,
		ItemTitle:      result.ItemTitle,
		ItemMerchantID: result.ItemMerchantID,
		UserID:         result.UserID,
		Price:          result.Price,
		Status:         result.Status,
		ExpiredAt:      result.ExpiredAt.Format(time.RFC3339),
		CreatedAt:      result.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      result.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *GormStore) UpdateOrderStatus(orderID string, from, to model.OrderStatus) (bool, error) {
	result := s.db.Model(&model.Order{}).
		Where("id = ? AND status = ?", orderID, from).
		Update("status", to)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *GormStore) ListOrders(input dto.ListOrdersInput) ([]dto.OrderWithTitle, int64, error) {
	db := s.db.Table("orders").
		Select("orders.*, auction_items.title as item_title").
		Joins("JOIN auction_items ON auction_items.id = orders.item_id AND auction_items.deleted_at IS NULL")

	if input.UserID != "" {
		db = db.Where("orders.user_id = ?", input.UserID)
	}
	if input.MerchantID != "" {
		db = db.Where("auction_items.merchant_id = ?", input.MerchantID)
	}
	if input.Status != "" {
		db = db.Where("orders.status = ?", input.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []struct {
		model.Order
		ItemTitle string `gorm:"column:item_title"`
	}
	if err := db.Order("orders.created_at DESC").
		Scopes(page.Paginate(input.Page, input.PageSize)).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	list := make([]dto.OrderWithTitle, len(rows))
	for i, r := range rows {
		list[i] = dto.OrderWithTitle{
			ID:        r.ID,
			ItemID:    r.ItemID,
			ItemTitle: r.ItemTitle,
			UserID:    r.UserID,
			Price:     r.Price,
			Status:    r.Status,
			ExpiredAt: r.ExpiredAt.Format(time.RFC3339),
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		}
	}
	return list, total, nil
}

func (s *GormStore) ListExpiredPendingOrders(before time.Time, limit int) ([]model.Order, error) {
	var orders []model.Order
	err := s.db.Where("status = ? AND expired_at < ?", model.OrderPending, before).
		Limit(limit).Find(&orders).Error
	return orders, err
}

func (s *GormStore) ListEndedItemsWithoutOrder(limit int) ([]dto.EndedItemSummary, error) {
	var results []dto.EndedItemSummary
	err := s.db.Raw(`
		SELECT ai.id as item_id, ai.winner_id, ai.deal_price
		FROM auction_items ai
		LEFT JOIN orders o ON o.item_id = ai.id
		WHERE ai.status = 'ended'
		  AND ai.winner_id != ''
		  AND ai.deleted_at IS NULL
		  AND o.id IS NULL
		LIMIT ?
	`, limit).Scan(&results).Error
	return results, err
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/app/order/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/app/order/dao/order.go
git commit -m "feat(order): add order DAO Store interface and GormStore"
```

---

## Task 4: Order Service — Core Methods + Tests

**Files:**
- Create: `internal/app/order/service/service.go`
- Create: `internal/app/order/service/service_test.go`

- [ ] **Step 1: Write the failing tests first**

Create `internal/app/order/service/service_test.go`:

```go
package service

import (
	"errors"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

// fakeStore implements dao.Store with in-memory maps for unit tests.
type fakeStore struct {
	orders      map[string]*model.Order
	orderByItem map[string]*model.Order
	details     map[string]*dto.OrderDetail
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		orders:      map[string]*model.Order{},
		orderByItem: map[string]*model.Order{},
		details:     map[string]*dto.OrderDetail{},
	}
}

func (s *fakeStore) AutoMigrate() error { return nil }

func (s *fakeStore) CreateOrder(order *model.Order) error {
	cp := *order
	s.orders[order.ID] = &cp
	s.orderByItem[order.ItemID] = &cp
	return nil
}

func (s *fakeStore) FindOrder(orderID string) (*model.Order, error) {
	o, ok := s.orders[orderID]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (s *fakeStore) FindOrderByItemID(itemID string) (*model.Order, error) {
	o, ok := s.orderByItem[itemID]
	if !ok {
		return nil, errorx.ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (s *fakeStore) FindOrderDetail(orderID string) (*dto.OrderDetail, error) {
	d, ok := s.details[orderID]
	if !ok {
		o, ok2 := s.orders[orderID]
		if !ok2 {
			return nil, errorx.ErrNotFound
		}
		return &dto.OrderDetail{
			ID:             o.ID,
			ItemID:         o.ItemID,
			ItemTitle:      "test item",
			ItemMerchantID: "merchant_test",
			UserID:         o.UserID,
			Price:          o.Price,
			Status:         o.Status,
		}, nil
	}
	return d, nil
}

func (s *fakeStore) UpdateOrderStatus(orderID string, from, to model.OrderStatus) (bool, error) {
	o, ok := s.orders[orderID]
	if !ok {
		return false, nil
	}
	if o.Status != from {
		return false, nil
	}
	o.Status = to
	if byItem := s.orderByItem[o.ItemID]; byItem != nil {
		byItem.Status = to
	}
	return true, nil
}

func (s *fakeStore) ListOrders(input dto.ListOrdersInput) ([]dto.OrderWithTitle, int64, error) {
	return nil, 0, nil
}

func (s *fakeStore) ListExpiredPendingOrders(before time.Time, limit int) ([]model.Order, error) {
	var result []model.Order
	for _, o := range s.orders {
		if o.Status == model.OrderPending && o.ExpiredAt.Before(before) {
			result = append(result, *o)
		}
	}
	return result, nil
}

func (s *fakeStore) ListEndedItemsWithoutOrder(limit int) ([]dto.EndedItemSummary, error) {
	return nil, nil
}

// --- tests ---

func newTestService(store *fakeStore) *Service {
	svc := NewService(store, 30*time.Minute)
	svc.now = func() time.Time { return time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC) }
	return svc
}

func TestCreateOrder_CreatesWithPendingStatus(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)

	order, err := svc.CreateOrder("item_1", "user_1", 5000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != model.OrderPending {
		t.Errorf("want status=pending, got %s", order.Status)
	}
	if order.UserID != "user_1" {
		t.Errorf("want user_id=user_1, got %s", order.UserID)
	}
	if order.Price != 5000 {
		t.Errorf("want price=5000, got %d", order.Price)
	}
	want := svc.now().Add(30 * time.Minute)
	if !order.ExpiredAt.Equal(want) {
		t.Errorf("want expired_at=%v, got %v", want, order.ExpiredAt)
	}
}

func TestCreateOrder_Idempotent(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)

	first, _ := svc.CreateOrder("item_1", "user_1", 5000)
	second, err := svc.CreateOrder("item_1", "user_1", 5000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("idempotent call should return same order: got %s vs %s", first.ID, second.ID)
	}
}

func TestPay_Success(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder("item_1", "user_1", 5000)

	user := &usermodel.User{ID: "user_1"}
	err := svc.Pay(user, order.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	saved, _ := store.FindOrder(order.ID)
	if saved.Status != model.OrderPaid {
		t.Errorf("want status=paid, got %s", saved.Status)
	}
}

func TestPay_AlreadyPaid_IsIdempotent(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder("item_1", "user_1", 5000)
	user := &usermodel.User{ID: "user_1"}

	_ = svc.Pay(user, order.ID)
	err := svc.Pay(user, order.ID) // second call

	if err != nil {
		t.Errorf("paying an already-paid order should be idempotent, got: %v", err)
	}
}

func TestPay_WrongUser_ReturnsUnauthorized(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder("item_1", "user_1", 5000)

	user := &usermodel.User{ID: "user_other"}
	err := svc.Pay(user, order.ID)

	if !errors.Is(err, errorx.ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

func TestCancel_Success(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder("item_1", "user_1", 5000)

	user := &usermodel.User{ID: "user_1"}
	err := svc.Cancel(user, order.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	saved, _ := store.FindOrder(order.ID)
	if saved.Status != model.OrderCancelled {
		t.Errorf("want status=cancelled, got %s", saved.Status)
	}
}

func TestCancel_PaidOrder_ReturnsInvalidRequest(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder("item_1", "user_1", 5000)
	user := &usermodel.User{ID: "user_1"}
	_ = svc.Pay(user, order.ID)

	err := svc.Cancel(user, order.ID)

	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Errorf("want ErrInvalidRequest, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/app/order/service/... -run "TestCreateOrder|TestPay|TestCancel" -v
```

Expected: FAIL — `service.go` does not exist yet

- [ ] **Step 3: Create the service implementation**

Create `internal/app/order/service/service.go`:

```go
package service

import (
	"errors"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/order/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store          dao.Store
	paymentTimeout time.Duration
	now            func() time.Time
}

func NewService(store dao.Store, paymentTimeout time.Duration) *Service {
	return &Service{
		store:          store,
		paymentTimeout: paymentTimeout,
		now:            time.Now,
	}
}

func (s *Service) CreateOrder(itemID, userID string, price int64) (*model.Order, error) {
	existing, err := s.store.FindOrderByItemID(itemID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, errorx.ErrNotFound) {
		return nil, err
	}
	order := &model.Order{
		ID:        "order_" + snowflake.MakeUUID(),
		ItemID:    itemID,
		UserID:    userID,
		Price:     price,
		Status:    model.OrderPending,
		ExpiredAt: s.now().Add(s.paymentTimeout),
	}
	if err := s.store.CreateOrder(order); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *Service) Pay(current *usermodel.User, orderID string) error {
	order, err := s.store.FindOrder(orderID)
	if err != nil {
		return err
	}
	if order.UserID != current.ID {
		return errorx.ErrUnauthorized
	}
	ok, err := s.store.UpdateOrderStatus(orderID, model.OrderPending, model.OrderPaid)
	if err != nil {
		return err
	}
	if !ok {
		refetched, err2 := s.store.FindOrder(orderID)
		if err2 != nil {
			return err2
		}
		if refetched.Status == model.OrderPaid {
			return nil
		}
		return errorx.ErrInvalidRequest
	}
	return nil
}

func (s *Service) Cancel(current *usermodel.User, orderID string) error {
	order, err := s.store.FindOrder(orderID)
	if err != nil {
		return err
	}
	if order.UserID != current.ID {
		return errorx.ErrUnauthorized
	}
	ok, err := s.store.UpdateOrderStatus(orderID, model.OrderPending, model.OrderCancelled)
	if err != nil {
		return err
	}
	if !ok {
		return errorx.ErrInvalidRequest
	}
	return nil
}

func (s *Service) ListOrders(current *usermodel.User, input dto.ListOrdersInput) (*dto.ListOrdersResult, error) {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PageSize <= 0 || input.PageSize > 100 {
		input.PageSize = 20
	}
	if current.Identity == usermodel.IdentityMerchant {
		input.MerchantID = current.ID
	} else {
		input.UserID = current.ID
	}
	list, total, err := s.store.ListOrders(input)
	if err != nil {
		return nil, err
	}
	return &dto.ListOrdersResult{
		List:     list,
		Page:     input.Page,
		PageSize: input.PageSize,
		Total:    total,
	}, nil
}

func (s *Service) GetOrder(current *usermodel.User, orderID string) (*dto.OrderDetail, error) {
	detail, err := s.store.FindOrderDetail(orderID)
	if err != nil {
		return nil, err
	}
	if current.Identity == usermodel.IdentityMerchant {
		if detail.ItemMerchantID != current.ID {
			return nil, errorx.ErrUnauthorized
		}
	} else {
		if detail.UserID != current.ID {
			return nil, errorx.ErrUnauthorized
		}
	}
	return detail, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/app/order/service/... -run "TestCreateOrder|TestPay|TestCancel" -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/app/order/service/service.go internal/app/order/service/service_test.go
git commit -m "feat(order): add order service with CreateOrder, Pay, Cancel + tests"
```

---

## Task 5: Order Service — Cron Methods + Tests

**Files:**
- Create: `internal/app/order/service/cron.go`
- Create: `internal/app/order/service/cron_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/app/order/service/cron_test.go`:

```go
package service

import (
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
)

func TestScanExpiredOrders_UpdatesExpiredPendingOrders(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)

	// seed: one order already expired
	now := svc.now()
	expiredOrder := &model.Order{
		ID:        "order_exp1",
		ItemID:    "item_exp1",
		UserID:    "user_1",
		Price:     1000,
		Status:    model.OrderPending,
		ExpiredAt: now.Add(-1 * time.Hour),
	}
	_ = store.CreateOrder(expiredOrder)

	// seed: one order still valid
	validOrder := &model.Order{
		ID:        "order_val1",
		ItemID:    "item_val1",
		UserID:    "user_1",
		Price:     2000,
		Status:    model.OrderPending,
		ExpiredAt: now.Add(1 * time.Hour),
	}
	_ = store.CreateOrder(validOrder)

	svc.ScanExpiredOrders()

	exp, _ := store.FindOrder("order_exp1")
	if exp.Status != model.OrderExpired {
		t.Errorf("want expired order status=expired, got %s", exp.Status)
	}

	val, _ := store.FindOrder("order_val1")
	if val.Status != model.OrderPending {
		t.Errorf("want valid order status=pending, got %s", val.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/app/order/service/... -run "TestScan" -v
```

Expected: FAIL — `ScanExpiredOrders` not defined

- [ ] **Step 3: Create cron.go**

```go
package service

import (
	"log"
)

// ScanExpiredOrders updates pending orders past their ExpiredAt to expired.
// Called by cron every 5 minutes. Processes up to 100 orders per run.
func (s *Service) ScanExpiredOrders() {
	orders, err := s.store.ListExpiredPendingOrders(s.now(), 100)
	if err != nil {
		log.Printf("[order] ScanExpiredOrders list error: %v", err)
		return
	}
	for _, o := range orders {
		if _, err := s.store.UpdateOrderStatus(o.ID, orderPending, orderExpired); err != nil {
			log.Printf("[order] ScanExpiredOrders update %s error: %v", o.ID, err)
		}
	}
}

// ScanCompensation creates orders for ended auction items that have no order yet.
// Called by cron every 10 minutes as a safety net for CreateOrder failures.
func (s *Service) ScanCompensation() {
	items, err := s.store.ListEndedItemsWithoutOrder(50)
	if err != nil {
		log.Printf("[order] ScanCompensation list error: %v", err)
		return
	}
	for _, item := range items {
		if _, err := s.CreateOrder(item.ItemID, item.WinnerID, item.DealPrice); err != nil {
			log.Printf("[order] ScanCompensation create order for item %s error: %v", item.ItemID, err)
		}
	}
}
```

Note: `orderPending` and `orderExpired` are unexported aliases — replace them with the model constants:

```go
import "github.com/zet-plane/live-auction-backend/internal/app/order/model"
```

And use `model.OrderPending`, `model.OrderExpired` directly. The final file:

```go
package service

import (
	"log"

	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
)

func (s *Service) ScanExpiredOrders() {
	orders, err := s.store.ListExpiredPendingOrders(s.now(), 100)
	if err != nil {
		log.Printf("[order] ScanExpiredOrders list error: %v", err)
		return
	}
	for _, o := range orders {
		if _, err := s.store.UpdateOrderStatus(o.ID, model.OrderPending, model.OrderExpired); err != nil {
			log.Printf("[order] ScanExpiredOrders update %s error: %v", o.ID, err)
		}
	}
}

func (s *Service) ScanCompensation() {
	items, err := s.store.ListEndedItemsWithoutOrder(50)
	if err != nil {
		log.Printf("[order] ScanCompensation list error: %v", err)
		return
	}
	for _, item := range items {
		if _, err := s.CreateOrder(item.ItemID, item.WinnerID, item.DealPrice); err != nil {
			log.Printf("[order] ScanCompensation create order for item %s error: %v", item.ItemID, err)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/app/order/service/... -run "TestScan" -v
```

Expected: PASS

- [ ] **Step 5: Run all order service tests**

```bash
go test ./internal/app/order/service/... -v
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/app/order/service/cron.go internal/app/order/service/cron_test.go
git commit -m "feat(order): add expiry and compensation cron methods with tests"
```

---

## Task 6: Order Handler + Router

**Files:**
- Create: `internal/app/order/handler/order.go`
- Create: `internal/app/order/router/order.go`

- [ ] **Step 1: Create handler**

```go
package handler

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	"github.com/zet-plane/live-auction-backend/internal/app/order/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var svc *service.Service

func Init(s *service.Service) {
	svc = s
}

func ListOrders(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	input := dto.ListOrdersInput{
		Status:   model.OrderStatus(c.Query("status")),
		Page:     c.QueryInt("page"),
		PageSize: c.QueryInt("page_size"),
	}
	result, err := svc.ListOrders(current, input)
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}

func GetOrder(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if svc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	detail, err := svc.GetOrder(current, c.Param("order_id"))
	if err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, detail)
}
```

- [ ] **Step 2: Create router**

```go
package router

import (
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/order/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)
	f.Group("/api/v1", func() {
		f.Get("/orders", handler.ListOrders)
		f.Get("/orders/{order_id}", handler.GetOrder)
	}, auth)
}
```

- [ ] **Step 3: Build**

```bash
go build ./internal/app/order/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/app/order/handler/order.go internal/app/order/router/order.go
git commit -m "feat(order): add list and detail handlers with auth routes"
```

---

## Task 7: Order Module Init

**Files:**
- Create: `internal/app/order/init.go`

- [ ] **Step 1: Create init.go**

```go
package order

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/order/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/order/router"
	"github.com/zet-plane/live-auction-backend/internal/app/order/service"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

// Svc is the package-level service instance exported for use by item and payment modules.
var Svc *service.Service

var errNilDB = errors.New("database pointer is nil")

type Order struct {
	Name string
	app.UnimplementedModule
}

func (o *Order) Info() string { return o.Name }

func (o *Order) PreInit(engine *kernel.Engine) error {
	if engine.DB == nil {
		return errNilDB
	}
	return dao.NewGormStore(engine.DB).AutoMigrate()
}

func (o *Order) Load(engine *kernel.Engine) error {
	store := dao.NewGormStore(engine.DB)
	Svc = service.NewService(store, 30*time.Minute)
	handler.Init(Svc)
	router.RegisterRoutes(engine.Flame)

	engine.Cron.AddFunc("@every 5m", Svc.ScanExpiredOrders)
	engine.Cron.AddFunc("@every 10m", Svc.ScanCompensation)
	return nil
}

func (o *Order) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/app/order/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/app/order/init.go
git commit -m "feat(order): add Order module init with cron registration and Svc export"
```

---

## Task 8: Payment Module

**Files:**
- Create: `internal/app/payment/handler/payment.go`
- Create: `internal/app/payment/router/payment.go`
- Create: `internal/app/payment/init.go`

- [ ] **Step 1: Create handler**

```go
package handler

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	orderservice "github.com/zet-plane/live-auction-backend/internal/app/order/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/middleware/response"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var orderSvc *orderservice.Service

func Init(s *orderservice.Service) {
	orderSvc = s
}

func Pay(r flamego.Render, c flamego.Context, current *usermodel.User, body dto.PayOrderRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	if orderSvc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := orderSvc.Pay(current, c.Param("order_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}

func Cancel(r flamego.Render, c flamego.Context, current *usermodel.User) {
	if orderSvc == nil {
		response.Error(r, errorx.ErrInternal)
		return
	}
	if err := orderSvc.Cancel(current, c.Param("order_id")); err != nil {
		response.Error(r, err)
		return
	}
	response.OK(r, nil)
}
```

- [ ] **Step 2: Create router**

```go
package router

import (
	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/order/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/payment/handler"
	userhandler "github.com/zet-plane/live-auction-backend/internal/app/user/handler"
	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func RegisterRoutes(f *flamego.Flame) {
	auth := web.Authorization(userhandler.AuthenticateToken)
	f.Group("/api/v1", func() {
		f.Post("/orders/{order_id}/pay", binding.JSON(dto.PayOrderRequest{}), handler.Pay)
		f.Post("/orders/{order_id}/cancel", handler.Cancel)
	}, auth)
}
```

- [ ] **Step 3: Create init.go**

```go
package payment

import (
	"context"
	"sync"

	"github.com/zet-plane/live-auction-backend/internal/app"
	orderapp "github.com/zet-plane/live-auction-backend/internal/app/order"
	"github.com/zet-plane/live-auction-backend/internal/app/payment/handler"
	"github.com/zet-plane/live-auction-backend/internal/app/payment/router"
	"github.com/zet-plane/live-auction-backend/internal/core/kernel"
)

type Payment struct {
	Name string
	app.UnimplementedModule
}

func (p *Payment) Info() string { return p.Name }

func (p *Payment) Load(engine *kernel.Engine) error {
	handler.Init(orderapp.Svc)
	router.RegisterRoutes(engine.Flame)
	return nil
}

func (p *Payment) Stop(wg *sync.WaitGroup, _ context.Context) error {
	defer wg.Done()
	return nil
}
```

- [ ] **Step 4: Build**

```bash
go build ./internal/app/payment/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/app/payment/
git commit -m "feat(payment): add payment module with Pay and Cancel handlers"
```

---

## Task 9: Consolidate Module Registration

**Files:**
- Modify: `internal/app/appInitialize/init.go`
- Delete: `internal/app/appInitialize/item.go`
- Delete: `internal/app/appInitialize/room.go`
- Delete: `internal/app/appInitialize/user.go`

The separate `init()` files run in alphabetical filename order: item → order → payment → room → user. This causes `item.Load()` to run before `order.Load()`, so `order.Svc` is nil when item tries to use it. Fix by consolidating into one file with explicit ordering.

- [ ] **Step 1: Rewrite `appInitialize/init.go` with explicit module order**

```go
package appInitialize

import (
	"github.com/zet-plane/live-auction-backend/internal/app"
	"github.com/zet-plane/live-auction-backend/internal/app/item"
	orderapp "github.com/zet-plane/live-auction-backend/internal/app/order"
	"github.com/zet-plane/live-auction-backend/internal/app/payment"
	"github.com/zet-plane/live-auction-backend/internal/app/room"
	"github.com/zet-plane/live-auction-backend/internal/app/user"
)

var apps []app.Module

func init() {
	apps = []app.Module{
		&user.User{Name: "user"},
		&room.Room{Name: "room"},
		&orderapp.Order{Name: "order"},
		&payment.Payment{Name: "payment"},
		&item.Item{Name: "item"},
	}
}

func GetApps() []app.Module {
	return apps
}
```

- [ ] **Step 2: Delete the now-redundant separate files**

```bash
rm internal/app/appInitialize/item.go
rm internal/app/appInitialize/room.go
rm internal/app/appInitialize/user.go
```

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/app/appInitialize/
git commit -m "chore(appInitialize): consolidate module registration with explicit load order"
```

---

## Task 10: Wire item Module to Call CreateOrder

**Files:**
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/init.go`

- [ ] **Step 1: Add `orderSvc` to item Service struct**

In `internal/app/item/service/service.go`, add the field and update `NewService`. No interface needed — directly import `order/service` (no circular dep since order does not import item).

Add import:
```go
orderservice "github.com/zet-plane/live-auction-backend/internal/app/order/service"
```

New field in `Service` struct — add after `now`:
```go
type Service struct {
    store    dao.Store
    cache    itemcache.Cache
    policy   dto.AuctionPolicy
    now      func() time.Time
    orderSvc *orderservice.Service
}
```

Update `NewService` signature and body:
```go
func NewService(store dao.Store, policy dto.AuctionPolicy, cache itemcache.Cache, orderSvc *orderservice.Service) *Service {
    return &Service{
        store:    store,
        cache:    cache,
        policy:   policy,
        now:      time.Now,
        orderSvc: orderSvc,
    }
}
```

- [ ] **Step 2: Call `CreateOrder` on price-cap hit in bid_service.go**

In `internal/app/item/service/bid_service.go`, find the `IsCapped` block (around line 83):

Replace the existing block:
```go
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
```

With:
```go
if luaResult.IsCapped {
    item.Status = model.ItemEnded
    item.WinnerID = current.ID
    item.DealPrice = input.Price
    if err := s.store.UpdateItemWithRule(item, rule); err != nil {
        return nil, err
    }
    status = "ended"
    if s.orderSvc != nil {
        if _, err := s.orderSvc.CreateOrder(item.ID, current.ID, input.Price); err != nil {
            // non-fatal: compensation cron will retry
            _ = err
        }
    }
    // TODO: broadcast auction_ended WebSocket event (implement after WS module)
}
```

- [ ] **Step 3: Update item/init.go to inject order.Svc**

In `internal/app/item/init.go`, update the `Load` method to pass `order.Svc`:

Find in `Load()`:
```go
svc := service.NewService(store, policy, c)
```

Replace with:
```go
svc := service.NewService(store, policy, c, orderapp.Svc)
```

Add the import at the top:
```go
orderapp "github.com/zet-plane/live-auction-backend/internal/app/order"
```

- [ ] **Step 4: Add `EndExpiredAuctions` method to item Service**

When time expires, the auction winner lives in Redis (not the DB — DB only stores winner on price-cap hit). The method needs both `s.cache` and `s.orderSvc`, so it belongs on the `Service` struct.

Add to `internal/app/item/service/service.go`:

```go
import "context"

// EndExpiredAuctions is called by cron every minute. It finds ongoing items
// whose end_time has passed, reads the final leader from Redis, writes the
// result to MySQL, and triggers order creation.
func (s *Service) EndExpiredAuctions() {
    items, _, err := s.store.ListItems(dto.ListItemsInput{
        Status:   model.ItemOngoing,
        Page:     1,
        PageSize: 50,
    })
    if err != nil {
        return
    }
    now := s.now()
    for _, iwr := range items {
        if iwr.Rule == nil || now.Before(iwr.Rule.EndTime) {
            continue
        }
        // Resolve final leader from Redis; fall back to DB WinnerID if Redis is cold.
        if s.cache != nil {
            if state, found, _ := s.cache.GetAuctionState(context.Background(), iwr.Item.ID); found && state.LeaderUserID != "" {
                iwr.Item.WinnerID = state.LeaderUserID
                iwr.Item.DealPrice = state.CurrentPrice
            }
        }
        iwr.Item.Status = model.ItemEnded
        if err := s.store.UpdateItemWithRule(iwr.Item, iwr.Rule); err != nil {
            continue
        }
        if iwr.Item.WinnerID != "" && s.orderSvc != nil {
            _, _ = s.orderSvc.CreateOrder(iwr.Item.ID, iwr.Item.WinnerID, iwr.Item.DealPrice)
        }
    }
}
```

Note: `dto.ListItemsInput.Status` is of type `model.AuctionItemStatus` — pass `model.ItemOngoing` directly without casting.

- [ ] **Step 5: Register auction-end cron in item/init.go**

In `item/init.go`'s `Load()` method, after `handler.Init(svc)`, add:

```go
engine.Cron.AddFunc("@every 1m", svc.EndExpiredAuctions)
```

- [ ] **Step 6: Update item service tests to pass `nil` as orderSvc**

In `internal/app/item/service/service_test.go`, any call to `NewService` currently passes 3 args. Add `nil` as the 4th:

```bash
grep -n "NewService" internal/app/item/service/service_test.go
grep -n "NewService" internal/app/item/service/bid_service_test.go
```

For every occurrence like `service.NewService(store, policy, cache)`, change to `service.NewService(store, policy, cache, nil)`.

- [ ] **Step 7: Build and test**

```bash
go build ./...
go test ./internal/app/item/service/... -v
go test ./internal/app/order/service/... -v
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add internal/app/item/service/service.go \
        internal/app/item/service/bid_service.go \
        internal/app/item/service/service_test.go \
        internal/app/item/service/bid_service_test.go \
        internal/app/item/init.go
git commit -m "feat(item): wire order.Svc into item module for auction-end order creation"
```

---

## Task 11: Full Build + Test Verification

- [ ] **Step 1: Build everything**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 2: Run all tests**

```bash
go test ./...
```

Expected: all PASS

- [ ] **Step 3: Final commit if any loose files remain**

```bash
git status
```

If clean, done. If any tracked modifications remain, stage and commit.
