package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/app/deposit/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

type fakeStore struct {
	deposits       map[string]*model.Deposit
	amounts        map[string]int64
	now            time.Time
	findDepositErr error
	findCalls      int
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
	s.findCalls++
	if s.findDepositErr != nil {
		return nil, s.findDepositErr
	}
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

func (s *fakeStore) ListPaidDepositsByItem(itemID string) ([]model.Deposit, error) {
	var result []model.Deposit
	for _, d := range s.deposits {
		if d.ItemID == itemID && d.Status == model.DepositPaid {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (s *fakeStore) TransitionDepositStatus(itemID, userID string, from, to model.DepositStatus, terminalAt *time.Time) (bool, error) {
	d, ok := s.deposits[depositKey(itemID, userID)]
	if !ok || d.Status != from {
		return false, nil
	}
	d.Status = to
	d.RefundedAt = terminalAt
	return true, nil
}

type fakePaidDepositCache struct {
	paid     map[string]int64
	markErr  error
	checkErr error
	marks    int
}

func newFakePaidDepositCache() *fakePaidDepositCache {
	return &fakePaidDepositCache{paid: map[string]int64{}}
}

func (c *fakePaidDepositCache) MarkPaidDeposit(_ context.Context, itemID, userID string, amount int64) error {
	c.marks++
	if c.markErr != nil {
		return c.markErr
	}
	c.paid[depositKey(itemID, userID)] = amount
	return nil
}

func (c *fakePaidDepositCache) HasPaidDeposit(_ context.Context, itemID, userID string, requiredAmount int64) (bool, error) {
	if c.checkErr != nil {
		return false, c.checkErr
	}
	amount, ok := c.paid[depositKey(itemID, userID)]
	return ok && amount >= requiredAmount, nil
}

type fakeAvailability struct {
	snapshot availability.Snapshot
}

func (f fakeAvailability) Snapshot() availability.Snapshot {
	return f.snapshot
}

func TestPayDepositCreatesPaidDepositUsingRuleAmount(t *testing.T) {
	store := newFakeStore()
	store.amounts["item_1"] = 5000
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }

	result, err := svc.PayDeposit(context.Background(), &usermodel.User{ID: "user_1"}, " item_1 ")
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

func TestPayDepositCachesPaidDeposit(t *testing.T) {
	store := newFakeStore()
	store.amounts["item_1"] = 5000
	cache := newFakePaidDepositCache()
	svc := NewService(store, cache)
	svc.now = func() time.Time { return store.now }

	_, err := svc.PayDeposit(context.Background(), &usermodel.User{ID: "user_1"}, "item_1")
	if err != nil {
		t.Fatalf("PayDeposit returned error: %v", err)
	}

	if cache.marks != 1 {
		t.Fatalf("expected one cache mark, got %d", cache.marks)
	}
	ok, err := cache.HasPaidDeposit(context.Background(), "item_1", "user_1", 5000)
	if err != nil {
		t.Fatalf("cache HasPaidDeposit returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected paid deposit cached")
	}
}

func TestRefundNonWinnersRefundsPaidDepositsExceptWinner(t *testing.T) {
	store := newFakeStore()
	paidAt := store.now.Add(-time.Minute)
	store.deposits[depositKey("item_1", "user_a")] = &model.Deposit{ID: "deposit_a", ItemID: "item_1", UserID: "user_a", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt}
	store.deposits[depositKey("item_1", "user_b")] = &model.Deposit{ID: "deposit_b", ItemID: "item_1", UserID: "user_b", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt}
	store.deposits[depositKey("item_1", "user_c")] = &model.Deposit{ID: "deposit_c", ItemID: "item_1", UserID: "user_c", Amount: 5000, Status: model.DepositRefunded, PaidAt: &paidAt}
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }

	summary, err := svc.RefundNonWinners(context.Background(), "item_1", "user_b")

	if err != nil {
		t.Fatalf("RefundNonWinners returned error: %v", err)
	}
	if summary.Refunded != 1 || summary.Forfeited != 0 || summary.Skipped != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if got := store.deposits[depositKey("item_1", "user_a")]; got.Status != model.DepositRefunded || got.RefundedAt == nil {
		t.Fatalf("expected user_a refunded with terminal time, got %+v", got)
	}
	if got := store.deposits[depositKey("item_1", "user_b")]; got.Status != model.DepositPaid {
		t.Fatalf("expected winner to remain paid, got %+v", got)
	}
}

func TestRefundWinnerAndForfeitWinnerAreIdempotent(t *testing.T) {
	store := newFakeStore()
	paidAt := store.now.Add(-time.Minute)
	store.deposits[depositKey("item_1", "winner_paid")] = &model.Deposit{ID: "deposit_w", ItemID: "item_1", UserID: "winner_paid", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt}
	store.deposits[depositKey("item_1", "winner_done")] = &model.Deposit{ID: "deposit_done", ItemID: "item_1", UserID: "winner_done", Amount: 5000, Status: model.DepositRefunded, PaidAt: &paidAt}
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }

	refundSummary, err := svc.RefundWinner(context.Background(), "item_1", "winner_paid")
	if err != nil {
		t.Fatalf("RefundWinner returned error: %v", err)
	}
	if refundSummary.Refunded != 1 {
		t.Fatalf("expected one refund, got %+v", refundSummary)
	}
	if got := store.deposits[depositKey("item_1", "winner_paid")]; got.Status != model.DepositRefunded || got.RefundedAt == nil {
		t.Fatalf("expected winner_paid refunded, got %+v", got)
	}

	second, err := svc.RefundWinner(context.Background(), "item_1", "winner_done")
	if err != nil {
		t.Fatalf("second RefundWinner returned error: %v", err)
	}
	if second.Refunded != 0 || second.Skipped != 1 {
		t.Fatalf("expected terminal deposit skipped, got %+v", second)
	}

	store.deposits[depositKey("item_2", "winner_default")] = &model.Deposit{ID: "deposit_f", ItemID: "item_2", UserID: "winner_default", Amount: 5000, Status: model.DepositPaid, PaidAt: &paidAt}
	forfeitSummary, err := svc.ForfeitWinner(context.Background(), "item_2", "winner_default")
	if err != nil {
		t.Fatalf("ForfeitWinner returned error: %v", err)
	}
	if forfeitSummary.Forfeited != 1 {
		t.Fatalf("expected one forfeiture, got %+v", forfeitSummary)
	}
	if got := store.deposits[depositKey("item_2", "winner_default")]; got.Status != model.DepositForfeited || got.RefundedAt == nil {
		t.Fatalf("expected winner_default forfeited, got %+v", got)
	}
}

func TestPayDepositIsIdempotentWhenAlreadyPaid(t *testing.T) {
	store := newFakeStore()
	store.amounts["item_1"] = 5000
	svc := NewService(store)
	svc.now = func() time.Time { return store.now }
	user := &usermodel.User{ID: "user_1"}

	first, err := svc.PayDeposit(context.Background(), user, "item_1")
	if err != nil {
		t.Fatalf("first PayDeposit returned error: %v", err)
	}
	second, err := svc.PayDeposit(context.Background(), user, "item_1")
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

	_, err := svc.PayDeposit(context.Background(), &usermodel.User{ID: "user_1"}, "item_1")
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

	created, _ := svc.PayDeposit(context.Background(), user, "item_1")
	found, err := svc.GetMyDeposit(context.Background(), user, "item_1")
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
			got, err := svc.HasPaidDeposit(context.Background(), tt.itemID, "user_1", tt.amount)
			if err != nil {
				t.Fatalf("HasPaidDeposit returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestHasPaidDepositFallsBackToCacheWhenStoreUnavailable(t *testing.T) {
	store := newFakeStore()
	store.findDepositErr = errors.New("dial tcp 172.31.20.91:3306: connect: connection refused")
	cache := newFakePaidDepositCache()
	cache.paid[depositKey("item_1", "user_1")] = 5000
	svc := NewService(store, cache)

	got, err := svc.HasPaidDeposit(context.Background(), "item_1", "user_1", 5000)
	if err != nil {
		t.Fatalf("HasPaidDeposit returned error: %v", err)
	}
	if !got {
		t.Fatal("expected cached paid deposit to pass when store is unavailable")
	}
}

func TestHasPaidDepositUsesCacheBeforeStore(t *testing.T) {
	store := newFakeStore()
	store.findDepositErr = errors.New("store should not be called")
	cache := newFakePaidDepositCache()
	cache.paid[depositKey("item_1", "user_1")] = 5000
	svc := NewService(store, cache)
	svc.SetAvailability(fakeAvailability{snapshot: availability.Snapshot{
		Valid:       true,
		MySQLState:  availability.MySQLBuffering,
		MySQL:       availability.DependencyStatus{Healthy: false, Error: "dial tcp mysql:3306: i/o timeout"},
		ActiveRedis: availability.RedisCloud,
		Mode:        availability.ModeMySQLBuffering,
	}})

	got, err := svc.HasPaidDeposit(context.Background(), "item_1", "user_1", 5000)
	if err != nil {
		t.Fatalf("HasPaidDeposit returned error: %v", err)
	}
	if !got {
		t.Fatal("expected cached paid deposit to pass")
	}
	if store.findCalls != 0 {
		t.Fatalf("expected store not called, got %d calls", store.findCalls)
	}
}

func TestHasPaidDepositSkipsStoreWhenMySQLUnavailableAndCacheMisses(t *testing.T) {
	store := newFakeStore()
	store.findDepositErr = errors.New("store should not be called")
	cache := newFakePaidDepositCache()
	svc := NewService(store, cache)
	svc.SetAvailability(fakeAvailability{snapshot: availability.Snapshot{
		Valid:       true,
		MySQLState:  availability.MySQLBuffering,
		MySQL:       availability.DependencyStatus{Healthy: false, Error: "dial tcp mysql:3306: i/o timeout"},
		ActiveRedis: availability.RedisCloud,
		Mode:        availability.ModeMySQLBuffering,
	}})

	got, err := svc.HasPaidDeposit(context.Background(), "item_1", "user_1", 5000)
	if err != nil {
		t.Fatalf("HasPaidDeposit returned error: %v", err)
	}
	if got {
		t.Fatal("expected cache miss to fail closed while MySQL is unavailable")
	}
	if store.findCalls != 0 {
		t.Fatalf("expected store not called, got %d calls", store.findCalls)
	}
}
