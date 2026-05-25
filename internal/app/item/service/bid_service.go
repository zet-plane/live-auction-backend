package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

var ErrDepositRequired = errorx.New(http.StatusBadRequest, 40005, "deposit required")

func (s *Service) PlaceBid(current *usermodel.User, itemID string, input dto.PlaceBidInput) (*dto.PlaceBidResult, error) {
	item, rule, err := s.store.FindItemWithRule(strings.TrimSpace(itemID))
	if err != nil {
		return nil, err
	}
	if item.Status != model.ItemOngoing {
		return nil, errorx.ErrInvalidRequest
	}
	if rule.DepositAmount > 0 {
		if s.depositSvc == nil {
			return nil, errorx.ErrInternal
		}
		ok, err := s.depositSvc.HasPaidDeposit(item.ID, current.ID, rule.DepositAmount)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrDepositRequired
		}
	}
	if s.cache == nil {
		return nil, errorx.ErrInternal
	}

	bidID := "bid_" + snowflake.MakeUUID()
	luaResult, err := s.cache.PlaceBidLua(context.Background(), item.ID, itemcache.BidLuaArgs{
		UserID:            current.ID,
		UserName:          input.UserName,
		BidID:             bidID,
		Price:             input.Price,
		BidIncrement:      rule.BidIncrement,
		PriceCap:          rule.PriceCap,
		ExtendTriggerSec:  s.policy.ExtendTriggerSec,
		AutoExtendSec:     s.policy.AutoExtendSec,
		MaxExtendCount:    s.policy.MaxExtendCount,
		MaxTotalExtendSec: s.policy.MaxTotalExtendSec,
		NowUnix:           s.now().Unix(),
		IdempotencyKey:    input.IdempotencyKey,
		IdempotencyTTL:    86400,
	})
	if err != nil {
		return nil, err
	}

	switch luaResult.Code {
	case 1: // idempotent: already bid, return current state without writing BidLog again
		return &dto.PlaceBidResult{
			BidID:        luaResult.BidID,
			CurrentPrice: luaResult.CurrentPrice,
			LeaderUserID: luaResult.LeaderUserID,
			EndTime:      time.Unix(luaResult.EndTimeUnix, 0),
			Status:       "ongoing",
		}, nil
	case 2:
		return nil, errorx.New(http.StatusBadRequest, 40002, "auction has ended")
	case 3:
		return nil, errorx.New(http.StatusBadRequest, 40003, "price too low")
	case 4:
		return nil, errorx.New(http.StatusBadRequest, 40004, "invalid bid increment")
	default:
		if luaResult.Code != 0 {
			return nil, errorx.ErrInternal
		}
	}

	// TODO: 高并发场景下改为异步落库（写入 Redis LIST，worker 批量消费）
	bidLog := &model.BidLog{
		ID:     luaResult.BidID,
		ItemID: item.ID,
		RoomID: item.RoomID,
		UserID: current.ID,
		Price:  input.Price,
	}
	if err := s.store.CreateBidLog(bidLog); err != nil {
		return nil, err
	}

	if s.broadcaster != nil {
		endTime := time.Unix(luaResult.EndTimeUnix, 0)
		_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
			Type: dto.EventBidSuccess,
			Payload: dto.BidSuccessPayload{
				ItemID:       item.ID,
				UserID:       current.ID,
				Price:        input.Price,
				CurrentPrice: luaResult.CurrentPrice,
				LeaderUserID: luaResult.LeaderUserID,
				EndTime:      endTime,
			},
		})
		if luaResult.PrevLeaderUserID != "" && luaResult.PrevLeaderUserID != luaResult.LeaderUserID {
			_ = s.broadcaster.Unicast(wsevent.UserAddr(luaResult.PrevLeaderUserID), wsevent.Event{
				Type: dto.EventUserOutbid,
				Payload: dto.UserOutbidPayload{
					ItemID:       item.ID,
					NewLeaderID:  luaResult.LeaderUserID,
					CurrentPrice: luaResult.CurrentPrice,
				},
			})
		}
		if luaResult.IsExtended {
			_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
				Type: dto.EventAuctionExtended,
				Payload: dto.AuctionExtendedPayload{
					ItemID:        item.ID,
					NewEndTime:    endTime,
					ExtendSeconds: s.policy.AutoExtendSec,
				},
			})
		}
	}

	status := "ongoing"
	if luaResult.IsCapped {
		item.Status = model.ItemEnded
		item.WinnerID = current.ID
		item.DealPrice = input.Price
		if err := s.store.UpdateItemWithRule(item, rule); err != nil {
			return nil, err
		}
		status = "ended"
		var orderID string
		if s.orderSvc != nil {
			if order, err := s.orderSvc.CreateOrder(item.ID, current.ID, input.Price); err == nil && order != nil {
				orderID = order.ID
			}
		}
		if s.broadcaster != nil {
			_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
				Type: dto.EventAuctionEnded,
				Payload: dto.AuctionEndedPayload{
					ItemID:       item.ID,
					WinnerUserID: current.ID,
					DealPrice:    input.Price,
				},
			})
			if orderID != "" {
				orderEvt := wsevent.Event{
					Type: dto.EventOrderCreated,
					Payload: dto.OrderCreatedPayload{
						ItemID:    item.ID,
						OrderID:   orderID,
						WinnerID:  current.ID,
						DealPrice: input.Price,
					},
				}
				_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), orderEvt)
				_ = s.broadcaster.Unicast(wsevent.UserAddr(current.ID), orderEvt)
			}
		}
	}

	return &dto.PlaceBidResult{
		BidID:        luaResult.BidID,
		CurrentPrice: luaResult.CurrentPrice,
		LeaderUserID: luaResult.LeaderUserID,
		EndTime:      time.Unix(luaResult.EndTimeUnix, 0),
		Status:       status,
	}, nil
}

func (s *Service) GetRanking(itemID string, page, pageSize int) (*dto.RankingResult, error) {
	if page <= 0 {
		page = 1
	}
	switch {
	case pageSize > 100:
		pageSize = 100
	case pageSize <= 0:
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	var entries []dto.BidderPrice
	if s.cache != nil {
		var err error
		entries, err = s.cache.GetRanking(context.Background(), strings.TrimSpace(itemID), offset, pageSize)
		if err != nil {
			entries = nil // degrade to MySQL fallback; read errors are non-fatal
		}
	}

	if len(entries) == 0 {
		// TODO: ListBidRanking takes a single limit; we pass offset+pageSize and slice in Go.
		// For large page numbers this is an O(page) query — acceptable given leaderboard caps.
		all, err := s.store.ListBidRanking(strings.TrimSpace(itemID), offset+pageSize)
		if err != nil {
			return nil, err
		}
		if offset < len(all) {
			entries = all[offset:]
		}
		if len(entries) > pageSize {
			entries = entries[:pageSize]
		}
	}

	list := make([]dto.RankingEntry, len(entries))
	for i, e := range entries {
		list[i] = dto.RankingEntry{
			Rank:     offset + i + 1,
			UserID:   e.UserID,
			UserName: e.UserName,
			Price:    e.Price,
		}
	}
	return &dto.RankingResult{List: list, Page: page, PageSize: pageSize}, nil
}
