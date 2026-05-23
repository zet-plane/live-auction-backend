package service

import (
	"strings"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dao"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
)

type Service struct {
	store  dao.Store
	cache  itemcache.Cache
	policy dto.AuctionPolicy
	now    func() time.Time
}

func NewService(store dao.Store, policy dto.AuctionPolicy, cache itemcache.Cache) *Service {
	return &Service{
		store:  store,
		cache:  cache,
		policy: policy,
		now:    time.Now,
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
	for _, item := range items {
		list = append(list, dto.NewItemListDTO(item.Item, item.Rule, s.policy, now))
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
	for _, item := range items {
		list = append(list, dto.NewMerchantItemDTO(item.Item, item.Rule, s.policy, now))
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
	result := dto.NewItemDetailDTO(item, rule, s.policy, s.now())
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
	return s.transition(current, itemID, model.ItemDraft, model.ItemPublished)
}

func (s *Service) StartItem(current *usermodel.User, itemID string) error {
	return s.transition(current, itemID, model.ItemPublished, model.ItemOngoing)
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
	return s.store.UpdateItemWithRule(item, rule)
}

func (s *Service) transition(current *usermodel.User, itemID string, from model.AuctionItemStatus, to model.AuctionItemStatus) error {
	item, rule, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != from {
		return errorx.ErrInvalidRequest
	}
	item.Status = to
	return s.store.UpdateItemWithRule(item, rule)
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
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.ImageURL = strings.TrimSpace(input.ImageURL)
	input.Tags = dto.NormalizeTags(input.Tags)
	return input
}

func validateCreateInput(input dto.CreateItemInput) error {
	if input.Title == "" || input.Rule.BidIncrement <= 0 {
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
