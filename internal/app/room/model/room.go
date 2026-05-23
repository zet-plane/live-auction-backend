package model

import (
	"time"

	"gorm.io/gorm"
)

type RoomStatus string

const (
	RoomIdle RoomStatus = "idle"
	RoomLive RoomStatus = "live"
)

type LiveRoom struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	MerchantID    string     `gorm:"uniqueIndex;size:64;not null" json:"merchant_id"`
	Title         string     `gorm:"size:128;not null" json:"title"`
	Status        RoomStatus `gorm:"size:32;not null" json:"status"`
	CurrentItemID string     `gorm:"size:64" json:"current_item_id,omitempty"`

	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
