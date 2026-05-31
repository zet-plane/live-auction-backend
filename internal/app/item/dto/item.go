package dto

import (
	"strings"
	"time"

	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
)

type AuctionPolicy struct {
	ExtendTriggerSec  int `json:"extend_trigger_sec"`
	AutoExtendSec     int `json:"auto_extend_sec"`
	MaxExtendCount    int `json:"max_extend_count"`
	MaxTotalExtendSec int `json:"max_total_extend_sec"`
}

func DefaultAuctionPolicy() AuctionPolicy {
	return AuctionPolicy{
		ExtendTriggerSec:  30,
		AutoExtendSec:     10,
		MaxExtendCount:    6,
		MaxTotalExtendSec: 300,
	}
}

type RuleInput struct {
	StartPrice    int64     `json:"start_price"    binding:"required,min=1"`
	BidIncrement  int64     `json:"bid_increment"  binding:"required,min=1"`
	PriceCap      int64     `json:"price_cap"      binding:"omitempty,min=1"`
	DepositAmount int64     `json:"deposit_amount" binding:"omitempty,min=1"`
	StartTime     time.Time `json:"start_time"     binding:"required"`
	EndTime       time.Time `json:"end_time"       binding:"required"`
}

type CreateItemInput struct {
	RoomID      string
	Title       string
	Description string
	ImageURL    string
	Tags        []string
	Rule        RuleInput
}

type CreateItemRequest struct {
	RoomID      string    `json:"room_id"     binding:"required,min=1,max=64"`
	Title       string    `json:"title"       binding:"required,min=1,max=128"`
	Description string    `json:"description" binding:"omitempty,max=1024"`
	ImageURL    string    `json:"image_url"   binding:"omitempty,max=512"`
	Tags        []string  `json:"tags"        binding:"omitempty,dive,min=1,max=64"`
	Rule        RuleInput `json:"rule"        binding:"required"`
}

func (r CreateItemRequest) Input() CreateItemInput {
	return CreateItemInput{
		RoomID:      r.RoomID,
		Title:       r.Title,
		Description: r.Description,
		ImageURL:    r.ImageURL,
		Tags:        r.Tags,
		Rule:        r.Rule,
	}
}

type CreateItemResult struct {
	ItemID string `json:"item_id"`
	RuleID string `json:"rule_id"`
}

type ListItemsInput struct {
	Status     itemmodel.AuctionItemStatus
	Keyword    string
	MerchantID string
	Page       int
	PageSize   int
}

type ItemListResult struct {
	List     []ItemListDTO `json:"list"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Total    int64         `json:"total"`
}

type MerchantItemListResult struct {
	List     []MerchantItemDTO `json:"list"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int64             `json:"total"`
}

type RuleDTO struct {
	StartPrice    int64     `json:"start_price"`
	BidIncrement  int64     `json:"bid_increment"`
	PriceCap      int64     `json:"price_cap"`
	DepositAmount int64     `json:"deposit_amount"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
}

type ItemListDTO struct {
	ID               string                      `json:"id"`
	RoomID           string                      `json:"room_id"`
	Title            string                      `json:"title"`
	Description      string                      `json:"description"`
	ImageURL         string                      `json:"image_url"`
	Tags             []string                    `json:"tags"`
	Status           itemmodel.AuctionItemStatus `json:"status"`
	CurrentPrice     int64                       `json:"current_price"`
	DealPrice        int64                       `json:"deal_price"`
	StartPrice       int64                       `json:"start_price"`
	BidIncrement     int64                       `json:"bid_increment"`
	PriceCap         int64                       `json:"price_cap"`
	ExtendTriggerSec int                         `json:"extend_trigger_sec"`
	AutoExtendSec    int                         `json:"auto_extend_sec"`
	ParticipantCount int                         `json:"participant_count"`
	BidCount         int                         `json:"bid_count"`
	StartTime        time.Time                   `json:"start_time"`
	EndTime          time.Time                   `json:"end_time"`
	EndTimeUnixMS    int64                       `json:"end_time_unix_ms"`
	EndedAtUnixMS    int64                       `json:"ended_at_unix_ms"`
	EndReason        string                      `json:"end_reason"`
	RemainingMS      int64                       `json:"remaining_ms"`
}

type ItemDetailDTO struct {
	ID               string                      `json:"id"`
	MerchantID       string                      `json:"merchant_id"`
	Title            string                      `json:"title"`
	Description      string                      `json:"description"`
	ImageURL         string                      `json:"image_url"`
	Tags             []string                    `json:"tags"`
	Status           itemmodel.AuctionItemStatus `json:"status"`
	Rule             RuleDTO                     `json:"rule"`
	AuctionPolicy    AuctionPolicy               `json:"auction_policy"`
	CurrentPrice     int64                       `json:"current_price"`
	DealPrice        int64                       `json:"deal_price"`
	LeaderUserID     string                      `json:"leader_user_id"`
	ParticipantCount int                         `json:"participant_count"`
	BidCount         int                         `json:"bid_count"`
	EndTimeUnixMS    int64                       `json:"end_time_unix_ms"`
	EndedAtUnixMS    int64                       `json:"ended_at_unix_ms"`
	EndReason        string                      `json:"end_reason"`
	RemainingMS      int64                       `json:"remaining_ms"`
	IsExtended       bool                        `json:"is_extended"`
	CreatedAt        time.Time                   `json:"created_at"`
	UpdatedAt        time.Time                   `json:"updated_at"`
}

type MerchantItemDTO struct {
	ID                string                      `json:"id"`
	MerchantID        string                      `json:"merchant_id"`
	RoomID            string                      `json:"room_id"`
	Title             string                      `json:"title"`
	Description       string                      `json:"description"`
	ImageURL          string                      `json:"image_url"`
	Tags              []string                    `json:"tags"`
	Status            itemmodel.AuctionItemStatus `json:"status"`
	DealPrice         int64                       `json:"deal_price"`
	EndTimeUnixMS     int64                       `json:"end_time_unix_ms"`
	EndedAtUnixMS     int64                       `json:"ended_at_unix_ms"`
	EndReason         string                      `json:"end_reason"`
	StatusText        string                      `json:"status_text"`
	ExplainStatus     string                      `json:"explain_status"`
	ExplainStatusText string                      `json:"explain_status_text"`
	RuleSummary       RuleDTO                     `json:"rule_summary"`
	AuctionPolicy     AuctionPolicy               `json:"auction_policy"`
	Progress          ProgressDTO                 `json:"progress"`
	Result            ResultDTO                   `json:"result"`
	Actions           ActionsDTO                  `json:"actions"`
	CreatedAt         time.Time                   `json:"created_at"`
	UpdatedAt         time.Time                   `json:"updated_at"`
}

type ProgressDTO struct {
	CurrentPrice     int64  `json:"current_price"`
	DealPrice        int64  `json:"deal_price"`
	LeaderUserID     string `json:"leader_user_id"`
	BidCount         int    `json:"bid_count"`
	ParticipantCount int    `json:"participant_count"`
	OnlineCount      int    `json:"online_count"`
	EndTimeUnixMS    int64  `json:"end_time_unix_ms"`
	EndedAtUnixMS    int64  `json:"ended_at_unix_ms"`
	EndReason        string `json:"end_reason"`
	RemainingMS      int64  `json:"remaining_ms"`
	IsExtended       bool   `json:"is_extended"`
}

type ResultDTO struct {
	DealPrice      int64  `json:"deal_price"`
	WinnerUserID   string `json:"winner_user_id"`
	WinnerUserName string `json:"winner_user_name"`
	OrderID        string `json:"order_id"`
	OrderStatus    string `json:"order_status"`
}

type ActionsDTO struct {
	CanEdit       bool `json:"can_edit"`
	CanPublish    bool `json:"can_publish"`
	CanStart      bool `json:"can_start"`
	CanCancel     bool `json:"can_cancel"`
	CanUnpublish  bool `json:"can_unpublish"`
	CanViewDetail bool `json:"can_view_detail"`
}

func NormalizeTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		normalized = append(normalized, tag)
	}
	return normalized
}

func NewRuleDTO(rule *itemmodel.AuctionRule) RuleDTO {
	return RuleDTO{
		StartPrice:    rule.StartPrice,
		BidIncrement:  rule.BidIncrement,
		PriceCap:      rule.PriceCap,
		DepositAmount: rule.DepositAmount,
		StartTime:     rule.StartTime,
		EndTime:       rule.EndTime,
	}
}

func NewItemListDTO(item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule, policy AuctionPolicy, now time.Time) ItemListDTO {
	price := currentPrice(item, rule)
	finalDealPrice := dealPrice(item, rule)
	return ItemListDTO{
		ID:               item.ID,
		RoomID:           item.RoomID,
		Title:            item.Title,
		Description:      item.Description,
		ImageURL:         item.ImageURL,
		Tags:             item.Tags,
		Status:           item.Status,
		CurrentPrice:     price,
		DealPrice:        finalDealPrice,
		StartPrice:       rule.StartPrice,
		BidIncrement:     rule.BidIncrement,
		PriceCap:         rule.PriceCap,
		ExtendTriggerSec: policy.ExtendTriggerSec,
		AutoExtendSec:    policy.AutoExtendSec,
		StartTime:        rule.StartTime,
		EndTime:          rule.EndTime,
		EndTimeUnixMS:    rule.EndTime.UnixMilli(),
		RemainingMS:      remainingMS(item.Status, rule.EndTime, now),
	}
}

func NewItemDetailDTO(item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule, policy AuctionPolicy, now time.Time) ItemDetailDTO {
	price := currentPrice(item, rule)
	finalDealPrice := dealPrice(item, rule)
	return ItemDetailDTO{
		ID:            item.ID,
		MerchantID:    item.MerchantID,
		Title:         item.Title,
		Description:   item.Description,
		ImageURL:      item.ImageURL,
		Tags:          item.Tags,
		Status:        item.Status,
		Rule:          NewRuleDTO(rule),
		AuctionPolicy: policy,
		CurrentPrice:  price,
		DealPrice:     finalDealPrice,
		LeaderUserID:  item.WinnerID,
		EndTimeUnixMS: rule.EndTime.UnixMilli(),
		RemainingMS:   remainingMS(item.Status, rule.EndTime, now),
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}

func NewMerchantItemDTO(item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule, policy AuctionPolicy, now time.Time) MerchantItemDTO {
	price := currentPrice(item, rule)
	finalDealPrice := dealPrice(item, rule)
	return MerchantItemDTO{
		ID:                item.ID,
		MerchantID:        item.MerchantID,
		Title:             item.Title,
		Description:       item.Description,
		ImageURL:          item.ImageURL,
		Tags:              item.Tags,
		Status:            item.Status,
		DealPrice:         finalDealPrice,
		EndTimeUnixMS:     rule.EndTime.UnixMilli(),
		StatusText:        statusText(item.Status),
		ExplainStatus:     explainStatus(item.Status),
		ExplainStatusText: statusText(item.Status),
		RuleSummary:       NewRuleDTO(rule),
		AuctionPolicy:     policy,
		Progress: ProgressDTO{
			CurrentPrice:  price,
			DealPrice:     finalDealPrice,
			LeaderUserID:  item.WinnerID,
			EndTimeUnixMS: rule.EndTime.UnixMilli(),
			RemainingMS:   remainingMS(item.Status, rule.EndTime, now),
		},
		Result: ResultDTO{
			DealPrice:    item.DealPrice,
			WinnerUserID: item.WinnerID,
		},
		Actions:   actions(item.Status),
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func RefreshMerchantItemDerivedFields(d *MerchantItemDTO) {
	d.StatusText = statusText(d.Status)
	d.ExplainStatus = explainStatus(d.Status)
	d.ExplainStatusText = statusText(d.Status)
	d.Actions = actions(d.Status)
}

func currentPrice(item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule) int64 {
	if item.DealPrice > 0 {
		return item.DealPrice
	}
	return rule.StartPrice
}

func dealPrice(item *itemmodel.AuctionItem, rule *itemmodel.AuctionRule) int64 {
	if item.Status == itemmodel.ItemPublished || item.Status == itemmodel.ItemOngoing {
		return currentPrice(item, rule)
	}
	return item.DealPrice
}

func remainingMS(status itemmodel.AuctionItemStatus, end time.Time, now time.Time) int64 {
	if status != itemmodel.ItemPublished && status != itemmodel.ItemOngoing {
		return 0
	}
	if !end.After(now) {
		return 0
	}
	return end.Sub(now).Milliseconds()
}

func statusText(status itemmodel.AuctionItemStatus) string {
	switch status {
	case itemmodel.ItemDraft:
		return "草稿"
	case itemmodel.ItemPublished:
		return "已上架"
	case itemmodel.ItemOngoing:
		return "竞价中"
	case itemmodel.ItemEnded:
		return "已结束"
	case itemmodel.ItemCancelled:
		return "已取消"
	default:
		return string(status)
	}
}

func explainStatus(status itemmodel.AuctionItemStatus) string {
	switch status {
	case itemmodel.ItemOngoing:
		return "explaining"
	case itemmodel.ItemEnded:
		return "ended"
	default:
		return string(status)
	}
}

func actions(status itemmodel.AuctionItemStatus) ActionsDTO {
	return ActionsDTO{
		CanEdit:       status == itemmodel.ItemDraft,
		CanPublish:    status == itemmodel.ItemDraft,
		CanStart:      status == itemmodel.ItemPublished,
		CanCancel:     status == itemmodel.ItemPublished || status == itemmodel.ItemOngoing,
		CanUnpublish:  status == itemmodel.ItemEnded,
		CanViewDetail: true,
	}
}
