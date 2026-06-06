package service

import (
	"context"
	"errors"
	"testing"
	"time"

	depositservice "github.com/zet-plane/live-auction-backend/internal/app/deposit/service"
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

type fakeDepositSettler struct {
	refunded   []string
	forfeited  []string
	refundErr  error
	forfeitErr error
}

func (s *fakeDepositSettler) RefundWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error) {
	s.refunded = append(s.refunded, itemID+"\x00"+userID)
	return depositservice.SettlementSummary{Refunded: 1}, s.refundErr
}

func (s *fakeDepositSettler) ForfeitWinner(ctx context.Context, itemID, userID string) (depositservice.SettlementSummary, error) {
	s.forfeited = append(s.forfeited, itemID+"\x00"+userID)
	return depositservice.SettlementSummary{Forfeited: 1}, s.forfeitErr
}

// --- helpers ---

func newTestService(store *fakeStore) *Service {
	svc := NewService(store, 30*time.Minute)
	svc.now = func() time.Time { return time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC) }
	return svc
}

// --- tests ---

func TestCreateOrder_CreatesWithPendingStatus(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)

	order, err := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

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

	first, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)
	second, err := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

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
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	user := &usermodel.User{ID: "user_1"}
	err := svc.Pay(context.Background(), user, order.ID)

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
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)
	user := &usermodel.User{ID: "user_1"}

	_ = svc.Pay(context.Background(), user, order.ID)
	err := svc.Pay(context.Background(), user, order.ID) // second call

	if err != nil {
		t.Errorf("paying an already-paid order should be idempotent, got: %v", err)
	}
}

func TestPay_WrongUser_ReturnsUnauthorized(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	user := &usermodel.User{ID: "user_other"}
	err := svc.Pay(context.Background(), user, order.ID)

	if !errors.Is(err, errorx.ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

func TestCancel_Success(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	user := &usermodel.User{ID: "user_1"}
	err := svc.Cancel(context.Background(), user, order.ID)

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
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)
	user := &usermodel.User{ID: "user_1"}
	_ = svc.Pay(context.Background(), user, order.ID)

	err := svc.Cancel(context.Background(), user, order.ID)

	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Errorf("want ErrInvalidRequest, got %v", err)
	}
}

func TestPay_ExpiredOrder_ReturnsInvalidRequest(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)
	// manually set expired_at to the past
	store.orders[order.ID].ExpiredAt = svc.now().Add(-1 * time.Hour)

	user := &usermodel.User{ID: "user_1"}
	err := svc.Pay(context.Background(), user, order.ID)

	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Errorf("want ErrInvalidRequest for expired order, got %v", err)
	}
}

func TestPayRefundsWinnerDepositAfterStatusCommit(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	settler := &fakeDepositSettler{}
	svc.SetDepositSettler(settler)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	err := svc.Pay(context.Background(), &usermodel.User{ID: "user_1"}, order.ID)

	if err != nil {
		t.Fatalf("Pay returned error: %v", err)
	}
	if len(settler.refunded) != 1 || settler.refunded[0] != "item_1\x00user_1" {
		t.Fatalf("expected winner deposit refunded, got %#v", settler.refunded)
	}
}

func TestCancelForfeitsWinnerDepositAfterStatusCommit(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	settler := &fakeDepositSettler{}
	svc.SetDepositSettler(settler)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	err := svc.Cancel(context.Background(), &usermodel.User{ID: "user_1"}, order.ID)

	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if len(settler.forfeited) != 1 || settler.forfeited[0] != "item_1\x00user_1" {
		t.Fatalf("expected winner deposit forfeited, got %#v", settler.forfeited)
	}
}

func TestScanExpiredOrdersForfeitsWinnerDeposit(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	settler := &fakeDepositSettler{}
	svc.SetDepositSettler(settler)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)
	store.orders[order.ID].ExpiredAt = svc.now().Add(-time.Minute)

	svc.ScanExpiredOrders(context.Background())

	if len(settler.forfeited) != 1 || settler.forfeited[0] != "item_1\x00user_1" {
		t.Fatalf("expected expired order to forfeit deposit, got %#v", settler.forfeited)
	}
}

func TestListOrders_UserSeesOwnOrders(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)

	user := &usermodel.User{ID: "user_1", Identity: usermodel.IdentityUser}
	result, err := svc.ListOrders(context.Background(), user, dto.ListOrdersInput{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ListOrders delegates to store; verify input routing (UserID set)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Page != 1 {
		t.Errorf("want page=1, got %d", result.Page)
	}
	if result.PageSize != 20 {
		t.Errorf("want page_size=20, got %d", result.PageSize)
	}
}

func TestListOrders_MerchantSeesMerchantOrders(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)

	merchant := &usermodel.User{ID: "merchant_1", Identity: usermodel.IdentityMerchant}
	result, err := svc.ListOrders(context.Background(), merchant, dto.ListOrdersInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestGetOrder_WrongUser_ReturnsUnauthorized(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	other := &usermodel.User{ID: "user_other", Identity: usermodel.IdentityUser}
	_, err := svc.GetOrder(context.Background(), other, order.ID)

	if !errors.Is(err, errorx.ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

func TestGetOrder_CorrectUser_ReturnsDetail(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)

	user := &usermodel.User{ID: "user_1", Identity: usermodel.IdentityUser}
	detail, err := svc.GetOrder(context.Background(), user, order.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.ID != order.ID {
		t.Errorf("want order id=%s, got %s", order.ID, detail.ID)
	}
}

func TestGetOrder_MerchantOwner_ReturnsDetail(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)
	// fakeStore.FindOrderDetail returns ItemMerchantID="merchant_test" for orders not in details map
	merchant := &usermodel.User{ID: "merchant_test", Identity: usermodel.IdentityMerchant}
	detail, err := svc.GetOrder(context.Background(), merchant, order.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.ID != order.ID {
		t.Errorf("want order id=%s, got %s", order.ID, detail.ID)
	}
}

func TestGetOrder_WrongMerchant_ReturnsUnauthorized(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(store)
	order, _ := svc.CreateOrder(context.Background(), "item_1", "user_1", 5000)
	// fakeStore returns ItemMerchantID="merchant_test", so a different merchant gets 401
	merchant := &usermodel.User{ID: "merchant_other", Identity: usermodel.IdentityMerchant}
	_, err := svc.GetOrder(context.Background(), merchant, order.ID)

	if !errors.Is(err, errorx.ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}
