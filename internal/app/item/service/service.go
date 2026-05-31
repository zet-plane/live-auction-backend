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
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type Service struct {
	store       dao.Store
	cache       itemcache.Cache
	policy      dto.AuctionPolicy
	now         func() time.Time
	orderSvc    *orderservice.Service
	depositSvc  DepositChecker
	broadcaster wsevent.Broadcaster
}

type DepositChecker interface {
	HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (bool, error)
}

func NewService(store dao.Store, policy dto.AuctionPolicy, cache itemcache.Cache, orderSvc *orderservice.Service, depositSvc DepositChecker, broadcaster wsevent.Broadcaster) *Service {
	return &Service{
		store:       store,
		cache:       cache,
		policy:      policy,
		now:         time.Now,
		orderSvc:    orderSvc,
		depositSvc:  depositSvc,
		broadcaster: broadcaster,
	}
}

func (s *Service) CreateItem(ctx context.Context, current *usermodel.User, input dto.CreateItemInput) (result *dto.CreateItemResult, err error) {
	var itemID string
	finish := observability.Track(ctx, "item.create", "merchant_id", userID(current), "room_id", strings.TrimSpace(input.RoomID))
	defer func() {
		finish(&err, "item_id", itemID)
	}()

	if !isMerchant(current) {
		return nil, errorx.ErrUnauthorized
	}
	input = normalizeCreateInput(input)
	if err := validateCreateInput(input); err != nil {
		return nil, err
	}

	itemID = "item_" + snowflake.MakeUUID()
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

func (s *Service) ListItems(ctx context.Context, query dto.ListItemsInput) (result *dto.ItemListResult, err error) {
	query = normalizeListInput(query)
	defer observability.Track(ctx, "item.list",
		"status", query.Status,
		"merchant_id", query.MerchantID,
		"page", query.Page,
		"page_size", query.PageSize,
	)(&err)

	items, total, err := s.store.ListItems(query)
	if err != nil {
		return nil, err
	}
	list := make([]dto.ItemListDTO, 0, len(items))
	now := s.now()
	for _, iwr := range items {
		d := dto.NewItemListDTO(iwr.Item, iwr.Rule, s.policy, now)
		if iwr.Item.Status == model.ItemOngoing && s.cache != nil {
			if state, ok, _ := s.cache.GetAuctionState(ctx, iwr.Item.ID); ok {
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

func (s *Service) ListMerchantItems(ctx context.Context, current *usermodel.User, query dto.ListItemsInput) (result *dto.MerchantItemListResult, err error) {
	finish := observability.Track(ctx, "item.list_merchant",
		"merchant_id", userID(current),
	)
	defer func() {
		finish(&err, "status", query.Status, "page", query.Page, "page_size", query.PageSize)
	}()

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
			if state, ok, _ := s.cache.GetAuctionState(ctx, iwr.Item.ID); ok {
				applyStateToMerchant(&d, state, now)
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

func (s *Service) GetItem(ctx context.Context, itemID string) (result *dto.ItemDetailDTO, err error) {
	itemID = strings.TrimSpace(itemID)
	defer observability.Track(ctx, "item.get", "item_id", itemID)(&err)

	item, rule, err := s.store.FindItemWithRule(itemID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	detail := dto.NewItemDetailDTO(item, rule, s.policy, now)
	if item.Status == model.ItemOngoing && s.cache != nil {
		if state, ok, _ := s.cache.GetAuctionState(ctx, item.ID); ok {
			applyStateToDetail(&detail, state, now)
		}
	}
	return &detail, nil
}

func (s *Service) UpdateItem(ctx context.Context, current *usermodel.User, itemID string, input dto.CreateItemInput) (err error) {
	defer observability.Track(ctx, "item.update", "merchant_id", userID(current), "item_id", strings.TrimSpace(itemID))(&err)

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

func (s *Service) DeleteItem(ctx context.Context, current *usermodel.User, itemID string) (err error) {
	defer observability.Track(ctx, "item.delete", "merchant_id", userID(current), "item_id", strings.TrimSpace(itemID))(&err)

	item, _, err := s.findMerchantItem(current, itemID)
	if err != nil {
		return err
	}
	if item.Status != model.ItemDraft && item.Status != model.ItemPublished {
		return errorx.ErrInvalidRequest
	}
	return s.store.DeleteItem(item.ID)
}

func (s *Service) PublishItem(ctx context.Context, current *usermodel.User, itemID string) (err error) {
	defer observability.Track(ctx, "item.publish", "merchant_id", userID(current), "item_id", strings.TrimSpace(itemID))(&err)

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
		_ = s.cache.PushToRoomQueue(ctx, item.RoomID, item.ID, float64(s.now().Unix()))
	}
	return nil
}

func (s *Service) StartItem(ctx context.Context, current *usermodel.User, itemID string) (err error) {
	defer observability.Track(ctx, "item.start", "merchant_id", userID(current), "item_id", strings.TrimSpace(itemID))(&err)

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
		if err := s.cache.InitAuctionState(ctx, item.ID, state); err != nil {
			return err
		}
		if err := s.cache.ScheduleAuctionEnd(ctx, item.ID, rule.EndTime.UnixMilli()); err != nil {
			_ = s.cache.DeleteAuctionState(ctx, item.ID)
			return err
		}
	}
	item.Status = model.ItemOngoing
	if err := s.store.UpdateItemWithRule(item, rule); err != nil {
		if s.cache != nil {
			_ = s.cache.UnscheduleAuctionEnd(ctx, item.ID)
			_ = s.cache.DeleteAuctionState(ctx, item.ID)
		}
		return err
	}
	if err := s.store.SetRoomCurrentItem(item.RoomID, item.ID); err != nil {
		if s.cache != nil {
			_ = s.cache.UnscheduleAuctionEnd(ctx, item.ID)
			_ = s.cache.DeleteAuctionState(ctx, item.ID)
		}
		return err
	}
	if s.cache != nil {
		_ = s.cache.SetRoomCurrentItem(ctx, item.RoomID, item.ID)
	}
	if s.broadcaster != nil {
		_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
			Type: dto.EventAuctionStarted,
			Payload: dto.AuctionStartedPayload{
				ItemID:    item.ID,
				RoomID:    item.RoomID,
				StartTime: s.now(),
				EndTime:   rule.EndTime,
			},
		})
	}
	return nil
}

func (s *Service) CancelItem(ctx context.Context, current *usermodel.User, itemID string) (err error) {
	defer observability.Track(ctx, "item.cancel", "merchant_id", userID(current), "item_id", strings.TrimSpace(itemID))(&err)

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
	if err := s.store.ClearRoomCurrentItem(item.RoomID, item.ID); err != nil {
		return err
	}
	if s.cache != nil {
		_ = s.cache.RemoveFromRoomQueue(ctx, item.RoomID, item.ID)
		_ = s.cache.UnscheduleAuctionEnd(ctx, item.ID)
		_ = s.cache.DeleteAuctionState(ctx, item.ID)
		_ = s.cache.ClearRoomCurrentItem(ctx, item.RoomID, item.ID)
	}
	if s.broadcaster != nil {
		_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
			Type:    dto.EventAuctionCancelled,
			Payload: dto.AuctionCancelledPayload{ItemID: item.ID},
		})
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

func userID(current *usermodel.User) string {
	if current == nil {
		return ""
	}
	return current.ID
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

func (s *Service) EndExpiredAuctions(ctx context.Context) {
	var err error
	endedCount := 0
	finish := observability.Track(ctx, "auction.end_expired")
	defer func() {
		finish(&err, "ended_count", endedCount)
	}()

	items, listErr := s.store.ListOngoingItemsPastEndTime(s.now(), 50)
	if listErr != nil {
		err = listErr
		return
	}
	if len(items) == 0 {
		return
	}
	for _, iwr := range items {
		item, rule := iwr.Item, iwr.Rule
		var winnerID string
		var dealPrice int64
		if s.cache != nil {
			if state, ok, _ := s.cache.GetAuctionState(ctx, item.ID); ok {
				winnerID = state.LeaderUserID
				dealPrice = stateDealPrice(state)
			}
		}
		item.Status = model.ItemEnded
		item.WinnerID = winnerID
		item.DealPrice = dealPrice
		if err := s.store.UpdateItemWithRule(item, rule); err != nil {
			logx.Warnw("item.EndExpiredAuctions update failed", "item_id", item.ID, "err", err)
			continue
		}
		if err := s.store.ClearRoomCurrentItem(item.RoomID, item.ID); err != nil {
			logx.Warnw("item.EndExpiredAuctions clear room current item failed", "room_id", item.RoomID, "item_id", item.ID, "err", err)
			continue
		}
		endedCount++
		if s.cache != nil {
			_ = s.cache.UnscheduleAuctionEnd(ctx, item.ID)
			_ = s.cache.DeleteAuctionState(ctx, item.ID)
			_ = s.cache.ClearRoomCurrentItem(ctx, item.RoomID, item.ID)
		}
		if s.broadcaster != nil {
			_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
				Type: dto.EventAuctionEnded,
				Payload: dto.AuctionEndedPayload{
					ItemID:       item.ID,
					WinnerUserID: winnerID,
					LeaderUserID: winnerID,
					DealPrice:    dealPrice,
				},
			})
		}
		if winnerID != "" && s.orderSvc != nil {
			var orderID string
			if order, err := s.orderSvc.CreateOrder(ctx, item.ID, winnerID, dealPrice); err != nil {
				// non-fatal: compensation cron will retry
				logx.Warnw("item.EndExpiredAuctions create order failed", "item_id", item.ID, "winner_id", winnerID, "err", err)
			} else if order != nil {
				orderID = order.ID
			}
			if s.broadcaster != nil && orderID != "" {
				orderEvt := wsevent.Event{
					Type: dto.EventOrderCreated,
					Payload: dto.OrderCreatedPayload{
						ItemID:    item.ID,
						OrderID:   orderID,
						WinnerID:  winnerID,
						DealPrice: dealPrice,
					},
				}
				_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), orderEvt)
				_ = s.broadcaster.Unicast(wsevent.UserAddr(winnerID), orderEvt)
			}
		}
	}
}

func applyStateToDetail(d *dto.ItemDetailDTO, state *itemcache.AuctionState, now time.Time) {
	if status, ok := stateItemStatus(state); ok {
		d.Status = status
	}
	d.CurrentPrice = state.CurrentPrice
	d.DealPrice = stateDealPrice(state)
	d.LeaderUserID = state.LeaderUserID
	d.BidCount = state.BidCount
	d.ParticipantCount = state.ParticipantCount
	d.EndTimeUnixMS = stateEndTimeUnixMS(state)
	d.EndedAtUnixMS = state.EndedAtUnixMS
	d.EndReason = state.EndReason
	d.IsExtended = state.IsExtended
	d.RemainingMS = stateRemainingMS(state, now)
}

func applyStateToList(d *dto.ItemListDTO, state *itemcache.AuctionState, now time.Time) {
	if status, ok := stateItemStatus(state); ok {
		d.Status = status
	}
	d.CurrentPrice = state.CurrentPrice
	d.DealPrice = stateDealPrice(state)
	d.BidCount = state.BidCount
	d.ParticipantCount = state.ParticipantCount
	d.EndTimeUnixMS = stateEndTimeUnixMS(state)
	d.EndedAtUnixMS = state.EndedAtUnixMS
	d.EndReason = state.EndReason
	d.RemainingMS = stateRemainingMS(state, now)
}

func applyStateToMerchant(d *dto.MerchantItemDTO, state *itemcache.AuctionState, now time.Time) {
	if status, ok := stateItemStatus(state); ok {
		d.Status = status
	}
	d.DealPrice = stateDealPrice(state)
	d.EndTimeUnixMS = stateEndTimeUnixMS(state)
	d.EndedAtUnixMS = state.EndedAtUnixMS
	d.EndReason = state.EndReason
	if d.Status == model.ItemEnded {
		d.Result.DealPrice = stateDealPrice(state)
		d.Result.WinnerUserID = state.LeaderUserID
	}
	d.Progress.CurrentPrice = state.CurrentPrice
	d.Progress.DealPrice = stateDealPrice(state)
	d.Progress.LeaderUserID = state.LeaderUserID
	d.Progress.BidCount = state.BidCount
	d.Progress.ParticipantCount = state.ParticipantCount
	d.Progress.EndTimeUnixMS = stateEndTimeUnixMS(state)
	d.Progress.EndedAtUnixMS = state.EndedAtUnixMS
	d.Progress.EndReason = state.EndReason
	d.Progress.IsExtended = state.IsExtended
	d.Progress.RemainingMS = stateRemainingMS(state, now)
	dto.RefreshMerchantItemDerivedFields(d)
}

func stateItemStatus(state *itemcache.AuctionState) (model.AuctionItemStatus, bool) {
	if state.Status == "" {
		return "", false
	}
	return model.AuctionItemStatus(state.Status), true
}

func stateDealPrice(state *itemcache.AuctionState) int64 {
	if state.DealPrice > 0 {
		return state.DealPrice
	}
	return state.CurrentPrice
}

func stateEndTimeUnixMS(state *itemcache.AuctionState) int64 {
	if state.EndTimeUnixMS > 0 {
		return state.EndTimeUnixMS
	}
	if !state.EndTime.IsZero() {
		return state.EndTime.UnixMilli()
	}
	return 0
}

func stateRemainingMS(state *itemcache.AuctionState, now time.Time) int64 {
	if state.Status == string(model.ItemEnded) {
		return 0
	}
	endUnixMS := stateEndTimeUnixMS(state)
	if endUnixMS == 0 {
		return 0
	}
	remaining := endUnixMS - now.UnixMilli()
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}
