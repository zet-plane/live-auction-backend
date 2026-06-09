package model

import "time"

type BidLog struct {
	ID             string `gorm:"primaryKey;size:64"`
	ItemID         string `gorm:"index:idx_bid_logs_item_epoch_version,priority:1;index;size:64;not null"`
	RoomID         string `gorm:"index;size:64;not null"`
	UserID         string `gorm:"index:idx_bid_logs_item_user_idem,priority:2;index;size:64;not null"`
	Price          int64  `gorm:"not null"`
	AuthorityEpoch int64  `gorm:"index:idx_bid_logs_item_epoch_version,priority:2;not null;default:0"`
	AuctionVersion int64  `gorm:"index:idx_bid_logs_item_epoch_version,priority:3;not null;default:0"`
	IdempotencyKey string `gorm:"index:idx_bid_logs_item_user_idem,priority:3;size:128"`
	CreatedAt      time.Time
}
