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
	Result string `json:"result" binding:"required,oneof=success"`
}
