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
	ItemID    string      `gorm:"uniqueIndex:idx_orders_item_id_unique;size:64;not null" json:"item_id"`
	UserID    string      `gorm:"index;size:64;not null" json:"user_id"`
	Price     int64       `gorm:"not null" json:"price"`
	Status    OrderStatus `gorm:"index;size:32;not null" json:"status"`
	ExpiredAt time.Time   `gorm:"index" json:"expired_at"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}
