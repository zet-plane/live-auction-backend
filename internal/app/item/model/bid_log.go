package model

import "time"

type BidLog struct {
	ID        string    `gorm:"primaryKey;size:64"`
	ItemID    string    `gorm:"index;size:64;not null"`
	RoomID    string    `gorm:"index;size:64;not null"`
	UserID    string    `gorm:"index;size:64;not null"`
	Price     int64     `gorm:"not null"`
	CreatedAt time.Time
}
