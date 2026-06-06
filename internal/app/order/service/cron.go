package service

import (
	"context"

	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
)

// ScanExpiredOrders updates pending orders past their ExpiredAt to expired.
// Called by cron every 5 minutes. Processes up to 100 orders per run.
func (s *Service) ScanExpiredOrders(ctx context.Context) {
	var err error
	updatedCount := 0
	finish := observability.Track(ctx, "order.scan_expired")
	defer func() {
		finish(&err, "updated_count", updatedCount)
	}()

	orders, listErr := s.store.ListExpiredPendingOrders(s.now(), 100)
	if listErr != nil {
		err = listErr
		logx.Errorf("[order] ScanExpiredOrders list error: %v", listErr)
		return
	}
	for _, o := range orders {
		ok, err := s.store.UpdateOrderStatus(o.ID, model.OrderPending, model.OrderExpired)
		if err != nil {
			logx.Errorf("[order] ScanExpiredOrders update %s error: %v", o.ID, err)
			continue
		}
		if !ok {
			continue
		}
		if s.depositSettler != nil {
			if _, settleErr := s.depositSettler.ForfeitWinner(ctx, o.ItemID, o.UserID); settleErr != nil {
				logx.Warnw("[order] ScanExpiredOrders deposit forfeit failed", "order_id", o.ID, "item_id", o.ItemID, "user_id", o.UserID, "err", settleErr)
			}
		}
		updatedCount++
	}
}

// ScanCompensation creates orders for ended auction items that have no order yet.
// Called by cron every 10 minutes as a safety net for CreateOrder failures.
func (s *Service) ScanCompensation(ctx context.Context) {
	var err error
	createdCount := 0
	finish := observability.Track(ctx, "order.compensation_scan")
	defer func() {
		finish(&err, "created_count", createdCount)
	}()

	items, listErr := s.store.ListEndedItemsWithoutOrder(50)
	if listErr != nil {
		err = listErr
		logx.Errorf("[order] ScanCompensation list error: %v", listErr)
		return
	}
	for _, item := range items {
		if _, err := s.CreateOrder(ctx, item.ItemID, item.WinnerID, item.DealPrice); err != nil {
			logx.Errorf("[order] ScanCompensation create order for item %s error: %v", item.ItemID, err)
			continue
		}
		createdCount++
	}
}
