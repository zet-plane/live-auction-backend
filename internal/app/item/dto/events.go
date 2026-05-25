package dto

import "time"

const (
	EventAuctionStarted   = "auction_started"
	EventBidSuccess       = "bid_success"
	EventAuctionExtended  = "auction_extended"
	EventUserOutbid       = "user_outbid"
	EventAuctionEnded     = "auction_ended"
	EventAuctionCancelled = "auction_cancelled"
	EventOrderCreated     = "order_created"
)

type AuctionStartedPayload struct {
	ItemID    string    `json:"item_id"`
	RoomID    string    `json:"room_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

type BidSuccessPayload struct {
	ItemID       string    `json:"item_id"`
	UserID       string    `json:"user_id"`
	Price        int64     `json:"price"`
	CurrentPrice int64     `json:"current_price"`
	LeaderUserID string    `json:"leader_user_id"`
	EndTime      time.Time `json:"end_time"`
}

type AuctionExtendedPayload struct {
	ItemID        string    `json:"item_id"`
	OldEndTime    time.Time `json:"old_end_time"`
	NewEndTime    time.Time `json:"new_end_time"`
	ExtendSeconds int       `json:"extend_seconds"`
}

type UserOutbidPayload struct {
	ItemID       string `json:"item_id"`
	NewLeaderID  string `json:"new_leader_user_id"`
	CurrentPrice int64  `json:"current_price"`
}

type AuctionEndedPayload struct {
	ItemID       string `json:"item_id"`
	WinnerUserID string `json:"winner_user_id"`
	DealPrice    int64  `json:"deal_price"`
}

type AuctionCancelledPayload struct {
	ItemID string `json:"item_id"`
}

type OrderCreatedPayload struct {
	ItemID    string `json:"item_id"`
	OrderID   string `json:"order_id"`
	WinnerID  string `json:"winner_user_id"`
	DealPrice int64  `json:"deal_price"`
}
