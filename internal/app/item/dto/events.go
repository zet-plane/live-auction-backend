package dto

import "time"

const (
	EventAuctionStarted   = "auction_started"
	EventBidSuccess       = "bid_success"
	EventAuctionExtended  = "auction_extended"
	EventTimeSync         = "time_sync"
	EventAuctionSnapshot  = "auction_snapshot"
	EventUserOutbid       = "user_outbid"
	EventAuctionEnded     = "auction_ended"
	EventAuctionCancelled = "auction_cancelled"
	EventOrderCreated     = "order_created"
)

type AuctionStartedPayload struct {
	ItemID           string    `json:"item_id"`
	RoomID           string    `json:"room_id"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	ServerTimeUnixMS int64     `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64     `json:"end_time_unix_ms"`
	AuctionVersion   int64     `json:"auction_version"`
}

type BidSuccessPayload struct {
	ItemID           string    `json:"item_id"`
	UserID           string    `json:"user_id"`
	Price            int64     `json:"price"`
	CurrentPrice     int64     `json:"current_price"`
	LeaderUserID     string    `json:"leader_user_id"`
	EndTime          time.Time `json:"end_time"`
	ServerTimeUnixMS int64     `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64     `json:"end_time_unix_ms"`
	AuctionVersion   int64     `json:"auction_version"`
	CoalescedBids    int64     `json:"coalesced_bids,omitempty"`
}

type AuctionExtendedPayload struct {
	ItemID           string    `json:"item_id"`
	OldEndTime       time.Time `json:"old_end_time"`
	NewEndTime       time.Time `json:"new_end_time"`
	ExtendSeconds    int       `json:"extend_seconds"`
	ServerTimeUnixMS int64     `json:"server_time_unix_ms"`
	OldEndTimeUnixMS int64     `json:"old_end_time_unix_ms,omitempty"`
	NewEndTimeUnixMS int64     `json:"new_end_time_unix_ms"`
	AuctionVersion   int64     `json:"auction_version"`
}

type TimeSyncPayload struct {
	ItemID           string `json:"item_id"`
	ServerTimeUnixMS int64  `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64  `json:"end_time_unix_ms"`
	Status           string `json:"status"`
	AuctionVersion   int64  `json:"auction_version"`
}

type AuctionSnapshotPayload struct {
	ItemID           string `json:"item_id"`
	Status           string `json:"status"`
	ServerTimeUnixMS int64  `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64  `json:"end_time_unix_ms"`
	EndedAtUnixMS    int64  `json:"ended_at_unix_ms,omitempty"`
	LeaderUserID     string `json:"leader_user_id"`
	DealPrice        int64  `json:"deal_price"`
	BidCount         int    `json:"bid_count"`
	ParticipantCount int    `json:"participant_count"`
	EndReason        string `json:"end_reason,omitempty"`
	AuctionVersion   int64  `json:"auction_version"`
}

type UserOutbidPayload struct {
	ItemID           string `json:"item_id"`
	NewLeaderID      string `json:"new_leader_user_id"`
	CurrentPrice     int64  `json:"current_price"`
	ServerTimeUnixMS int64  `json:"server_time_unix_ms"`
	EndTimeUnixMS    int64  `json:"end_time_unix_ms"`
	AuctionVersion   int64  `json:"auction_version"`
}

type AuctionEndedPayload struct {
	ItemID           string `json:"item_id"`
	WinnerUserID     string `json:"winner_user_id"`
	LeaderUserID     string `json:"leader_user_id"`
	DealPrice        int64  `json:"deal_price"`
	ServerTimeUnixMS int64  `json:"server_time_unix_ms"`
	EndedAtUnixMS    int64  `json:"ended_at_unix_ms,omitempty"`
	EndReason        string `json:"end_reason,omitempty"`
	AuctionVersion   int64  `json:"auction_version"`
}

type AuctionCancelledPayload struct {
	ItemID         string `json:"item_id"`
	AuctionVersion int64  `json:"auction_version"`
}

type OrderCreatedPayload struct {
	ItemID         string `json:"item_id"`
	OrderID        string `json:"order_id"`
	WinnerID       string `json:"winner_user_id"`
	DealPrice      int64  `json:"deal_price"`
	AuctionVersion int64  `json:"auction_version"`
}
