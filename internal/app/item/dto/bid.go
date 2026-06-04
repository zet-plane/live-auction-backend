package dto

import "time"

// BidderPrice 供 cache.GetRanking 和 dao.ListBidRanking 返回，不含 Rank。
type BidderPrice struct {
	UserID   string
	UserName string
	Price    int64
}

type PlaceBidRequest struct {
	Price          int64  `json:"price"           binding:"required,min=1"`
	IdempotencyKey string `json:"idempotency_key" binding:"required,min=1,max=128"`
}

type PlaceBidInput struct {
	Price          int64
	IdempotencyKey string
	UserName       string
}

func (r PlaceBidRequest) Input(userName string) PlaceBidInput {
	return PlaceBidInput{
		Price:          r.Price,
		IdempotencyKey: r.IdempotencyKey,
		UserName:       userName,
	}
}

type PlaceBidResult struct {
	BidID         string    `json:"bid_id"`
	CurrentPrice  int64     `json:"current_price"`
	DealPrice     int64     `json:"deal_price"`
	LeaderUserID  string    `json:"leader_user_id"`
	EndTime       time.Time `json:"end_time"`
	EndTimeUnixMS int64     `json:"end_time_unix_ms"`
	Status        string    `json:"status"` // "ongoing" | "ended"
}

type RankingEntry struct {
	Rank     int    `json:"rank"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	Price    int64  `json:"price"`
}

type RankingResult struct {
	List        []RankingEntry      `json:"list"`
	Page        int                 `json:"page"`
	PageSize    int                 `json:"page_size"`
	CurrentUser *CurrentUserRanking `json:"current_user,omitempty"`
}

type CurrentUserRanking struct {
	UserID   string `json:"user_id"`
	Rank     int    `json:"rank"`
	Price    int64  `json:"price"`
	IsLeader bool   `json:"is_leader"`
	HasBid   bool   `json:"has_bid"`
}
