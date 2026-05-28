package service

import (
	"context"
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

	svc.ScanExpiredOrders(context.Background())

	exp, _ := store.FindOrder("order_exp1")
	if exp.Status != model.OrderExpired {
		t.Errorf("want expired order status=expired, got %s", exp.Status)
	}

	val, _ := store.FindOrder("order_val1")
	if val.Status != model.OrderPending {
		t.Errorf("want valid order status=pending, got %s", val.Status)
	}
}
