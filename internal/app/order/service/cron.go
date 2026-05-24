package service

import (
	"log"

	"github.com/zet-plane/live-auction-backend/internal/app/order/model"
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
		if _, err := s.store.UpdateOrderStatus(o.ID, model.OrderPending, model.OrderExpired); err != nil {
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
