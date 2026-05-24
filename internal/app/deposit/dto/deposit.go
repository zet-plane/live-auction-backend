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
