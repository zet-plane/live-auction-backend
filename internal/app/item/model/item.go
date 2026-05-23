package model

import (
	"time"

	"gorm.io/gorm"
)

type AuctionItemStatus string

const (
	ItemDraft     AuctionItemStatus = "draft"
	ItemPublished AuctionItemStatus = "published"
	ItemOngoing   AuctionItemStatus = "ongoing"
	ItemEnded     AuctionItemStatus = "ended"
	ItemCancelled AuctionItemStatus = "cancelled"
)

type AuctionItem struct {
	ID          string            `gorm:"primaryKey;size:64" json:"id"`
	MerchantID  string            `gorm:"index;size:64;not null" json:"merchant_id"`
	RoomID      string            `gorm:"index;size:64;not null" json:"room_id"`
	Title       string            `gorm:"size:128;not null" json:"title"`
	Description string            `gorm:"size:1024" json:"description"`
	ImageURL    string            `gorm:"size:512" json:"image_url"`
	Tags        []string          `gorm:"serializer:json;type:json" json:"tags"`
	Status      AuctionItemStatus `gorm:"index;size:32;not null" json:"status"`
	RuleID      string            `gorm:"index;size:64;not null" json:"rule_id"`
	WinnerID    string            `gorm:"size:64" json:"winner_id,omitempty"`
	DealPrice   int64             `json:"deal_price"`

	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type AuctionRule struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	ItemID        string    `gorm:"uniqueIndex;size:64;not null" json:"item_id"`
	StartPrice    int64     `json:"start_price"`
	BidIncrement  int64     `json:"bid_increment"`
	PriceCap      int64     `json:"price_cap"`
	DepositAmount int64     `json:"deposit_amount"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ItemWithRule struct {
	Item *AuctionItem
	Rule *AuctionRule
}
