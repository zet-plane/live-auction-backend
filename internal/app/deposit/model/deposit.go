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
