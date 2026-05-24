package service

import (
	"context"
	"strings"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	orderservice "github.com/zet-plane/live-auction-backend/internal/app/order/service"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store    dao.Store
	cache    itemcache.Cache
	policy   dto.AuctionPolicy
	now      func() time.Time
	orderSvc *orderservice.Service
}

func NewService(store dao.Store, policy dto.AuctionPolicy, cache itemcache.Cache, orderSvc *orderservice.Service) *Service {
	return &Service{
		store:    store,
		cache:    cache,
		policy:   policy,
		now:      time.Now,
		orderSvc: orderSvc,
	}
}

func (s *Service) CreateItem(current *usermodel.User, input dto.CreateItemInput) (*dto.CreateItemResult, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	input = normalizeCreateInput(input)
	if err := validateCreateInput(input); err != nil {
		return nil, err
	}

	itemID := "item_" + snowflake.MakeUUID()
	ruleID := "rule_" + snowflake.MakeUUID()
	item := &model.AuctionItem{
		ID:          itemID,
		MerchantID:  current.ID,
		RoomID:      input.RoomID,
		Title:       input.Title,
		Description: input.Description,
		ImageURL:    input.ImageURL,
		Tags:        input.Tags,
		Status:      model.ItemDraft,
		RuleID:      ruleID,
	}
	rule := &model.AuctionRule{
		ID:            ruleID,
		ItemID:        itemID,
		StartPrice:    input.Rule.StartPrice,
		BidIncrement:  input.Rule.BidIncrement,
		PriceCap:      input.Rule.PriceCap,
		DepositAmount: input.Rule.DepositAmount,
		StartTime:     input.Rule.StartTime,
		EndTime:       input.Rule.EndTime,
	}
	if err := s.store.CreateItemWithRule(item, rule); err != nil {
		return nil, err
	}
	return &dto.CreateItemResult{ItemID: itemID, RuleID: ruleID}, nil
}

func (s *Service) ListItems(query dto.ListItemsInput) (*dto.ItemListResult, error) {
	query = normalizeListInput(query)
	items, total, err := s.store.ListItems(query)
	if err != nil {
		return nil, err
	}
	list := make([]dto.ItemListDTO, 0, len(items))
	now := s.now()
	for _, iwr := range items {
		d := dto.NewItemListDTO(iwr.Item, iwr.Rule, s.policy, now)
		if iwr.Item.Status == model.ItemOngoing && s.cache != nil {
			if state, ok, _ := s.cache.GetAuctionState(context.Background(), iwr.Item.ID); ok {
				applyStateToList(&d, state, now)
			}
		}
		list = append(list, d)
	}
	return &dto.ItemListResult{
		List:     list,
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    total,
	}, nil
}

func (s *Service) ListMerchantItems(current *usermodel.User, query dto.ListItemsInput) (*dto.MerchantItemListResult, error) {
	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	query.MerchantID = current.ID
	query = normalizeListInput(query)
	items, total, err := s.store.ListItems(query)
	if err != nil {
		return nil, err
	}
	list := make([]dto.MerchantItemDTO, 0, len(items))
	now := s.now()
	for _, iwr := range items {
		d := dto.NewMerchantItemDTO(iwr.Item, iwr.Rule, s.policy, now)
		if iwr.Item.Status == model.ItemOngoing && s.cache != nil {
			if state, ok, _ := s.cache.GetAuctionState(context.Background(), iwr.Item.ID); ok {
				d.Progress.CurrentPrice = state.CurrentPrice
				d.Progress.LeaderUserID = state.LeaderUserID
				d.Progress.BidCount = state.BidCount
				d.Progress.ParticipantCount = state.ParticipantCount
				d.Progress.IsExtended = state.IsExtended
				remaining := state.EndTime.Sub(now).Milliseconds()
				if remaining < 0 {
					remaining = 0
				}
				d.Progress.RemainingMS = remaining
			}
		}
		list = append(list, d)
	}
	return &dto.MerchantItemListResult{
		List:     list,
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    total,
	}, nil
}

func (s *Service) GetItem(itemID string) (*dto.ItemDetailDTO, error) {
	item, rule, err := s.store.FindItemWithRule(strings.TrimSpace(itemID))
	if err != nil {
		return nil, err
	}
	now := s.now()
	result := dto.NewItemDetailDTO(item, rule, s.policy, now)
	if item.Status == model.ItemOngoing && s.cache != nil {
		if state, ok, _ := s.cache.GetAuctionState(context.Background(), item.ID); ok {
			applyStateToDetail(&result, state, now)
		}
	}
	return &result, nil
}

func (s *Service) UpdateItem(current *usermodel.User, itemID string, input dto.CreateItemInput) error {
	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemDraft {
		return errorx.ErrInvalidRequest
	}
	input = normalizeCreateInput(input)
	if err := validateCreateInput(input); err != nil {
		return err
	}
	item.RoomID = input.RoomID
	item.Title = input.Title
	item.Description = input.Description
	item.ImageURL = input.ImageURL
	item.Tags = input.Tags
	rule.StartPrice = input.Rule.StartPrice
	rule.BidIncrement = input.Rule.BidIncrement
	rule.PriceCap = input.Rule.PriceCap
	rule.DepositAmount = input.Rule.DepositAmount
	rule.StartTime = input.Rule.StartTime
	rule.EndTime = input.Rule.EndTime
	return s.store.UpdateItemWithRule(item, rule)
}

func (s *Service) DeleteItem(current *usermodel.User, itemID string) error {
	item, _, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemDraft && item.Status != model.ItemPublished {
		return errorx.ErrInvalidRequest
	}
	return s.store.DeleteItem(item.ID)
}

func (s *Service) PublishItem(current *usermodel.User, itemID string) error {
	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemDraft {
		return errorx.ErrInvalidRequest
	}
	item.Status = model.ItemPublished
	if err := s.store.UpdateItemWithRule(item, rule); err != nil {
		return err
	}
	if s.cache != nil {
		_ = s.cache.PushToRoomQueue(context.Background(), item.RoomID, item.ID, float64(s.now().Unix()))
	}
	return nil
}

func (s *Service) StartItem(current *usermodel.User, itemID string) error {
	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemPublished {
		return errorx.ErrInvalidRequest
	}
	if s.cache != nil {
		state := itemcache.AuctionState{
			CurrentPrice: rule.StartPrice,
			EndTime:      rule.EndTime,
		}
		if err := s.cache.InitAuctionState(context.Background(), item.ID, state); err != nil {
			return err
		}
	}
	item.Status = model.ItemOngoing
	if err := s.store.UpdateItemWithRule(item, rule); err != nil {
		if s.cache != nil {
			_ = s.cache.DeleteAuctionState(context.Background(), item.ID)
		}
		return err
	}
	return nil
}

func (s *Service) CancelItem(current *usermodel.User, itemID string) error {
	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemPublished && item.Status != model.ItemOngoing {
		return errorx.ErrInvalidRequest
	}
	item.Status = model.ItemCancelled
	if err := s.store.UpdateItemWithRule(item, rule); err != nil {
		return err
	}
	if s.cache != nil {
		_ = s.cache.RemoveFromRoomQueue(context.Background(), item.RoomID, item.ID)
		_ = s.cache.DeleteAuctionState(context.Background(), item.ID)
	}
	return nil
}

func (s *Service) findMerchantItem(current *usermodel.User, itemID string) (*model.AuctionItem, *model.AuctionRule, error) {
	if !isMerchant(current) {
		return nil, nil, errorx.ErrUnauthorized
	}
	item, rule, err := s.store.FindItemWithRule(strings.TrimSpace(itemID))
	if err != nil {
		return nil, nil, err
	}
	if item.MerchantID != current.ID {
		return nil, nil, errorx.ErrNotFound
	}
	return item, rule, nil
}

func isMerchant(current *usermodel.User) bool {
	return current != nil && current.Identity == usermodel.IdentityMerchant
}

func normalizeCreateInput(input dto.CreateItemInput) dto.CreateItemInput {
	input.RoomID = strings.TrimSpace(input.RoomID)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.ImageURL = strings.TrimSpace(input.ImageURL)
	input.Tags = dto.NormalizeTags(input.Tags)
	return input
}

func validateCreateInput(input dto.CreateItemInput) error {
	if input.RoomID == "" || input.Title == "" || input.Rule.BidIncrement <= 0 {
		return errorx.ErrInvalidRequest
	}
	if input.Rule.StartPrice < 0 || input.Rule.PriceCap < 0 || input.Rule.DepositAmount < 0 {
		return errorx.ErrInvalidRequest
	}
	if input.Rule.PriceCap > 0 && input.Rule.PriceCap < input.Rule.StartPrice {
		return errorx.ErrInvalidRequest
	}
	if input.Rule.StartTime.IsZero() || !input.Rule.EndTime.After(input.Rule.StartTime) {
		return errorx.ErrInvalidRequest
	}
	return nil
}

func normalizeListInput(query dto.ListItemsInput) dto.ListItemsInput {
	query.Keyword = strings.TrimSpace(query.Keyword)
	if query.Page <= 0 {
		query.Page = 1
	}
	switch {
	case query.PageSize > 100:
		query.PageSize = 100
	case query.PageSize <= 0:
		query.PageSize = 10
	}
	return query
}

func (s *Service) EndExpiredAuctions() {
	items, err := s.store.ListOngoingItemsPastEndTime(s.now(), 50)
	if err != nil || len(items) == 0 {
		return
	}
	for _, iwr := range items {
		item, rule := iwr.Item, iwr.Rule
		var winnerID string
		var dealPrice int64
		if s.cache != nil {
			if state, ok, _ := s.cache.GetAuctionState(context.Background(), item.ID); ok {
				winnerID = state.LeaderUserID
				dealPrice = state.CurrentPrice
			}
		}
		item.Status = model.ItemEnded
		item.WinnerID = winnerID
		item.DealPrice = dealPrice
		if err := s.store.UpdateItemWithRule(item, rule); err != nil {
			continue
		}
		if s.cache != nil {
			_ = s.cache.DeleteAuctionState(context.Background(), item.ID)
		}
		if winnerID != "" && s.orderSvc != nil {
			if _, err := s.orderSvc.CreateOrder(item.ID, winnerID, dealPrice); err != nil {
				// non-fatal: compensation cron will retry
				_ = err
			}
		}
	}
}

func applyStateToDetail(d *dto.ItemDetailDTO, state *itemcache.AuctionState, now time.Time) {
	d.CurrentPrice = state.CurrentPrice
	d.LeaderUserID = state.LeaderUserID
	d.BidCount = state.BidCount
	d.ParticipantCount = state.ParticipantCount
	d.IsExtended = state.IsExtended
	remaining := state.EndTime.Sub(now).Milliseconds()
	if remaining < 0 {
		remaining = 0
	}
	d.RemainingMS = remaining
}

func applyStateToList(d *dto.ItemListDTO, state *itemcache.AuctionState, now time.Time) {
	d.CurrentPrice = state.CurrentPrice
	d.BidCount = state.BidCount
	d.ParticipantCount = state.ParticipantCount
	remaining := state.EndTime.Sub(now).Milliseconds()
	if remaining < 0 {
		remaining = 0
	}
	d.RemainingMS = remaining
}
