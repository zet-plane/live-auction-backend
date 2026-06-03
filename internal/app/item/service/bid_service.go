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
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

var ErrDepositRequired = errorx.New(http.StatusBadRequest, 40005, "deposit required")

func (s *Service) PlaceBid(ctx context.Context, current *usermodel.User, itemID string, input dto.PlaceBidInput) (result *dto.PlaceBidResult, err error) {
	itemID = strings.TrimSpace(itemID)
	var bidID string
	status := ""
	bidResult := "success"
	bidReason := "accepted"
	finish := observability.Track(ctx, "auction.place_bid", "user_id", userID(current), "item_id", itemID, "price", input.Price)
	defer func() {
		if err != nil && bidResult == "success" {
			bidResult = "error"
			bidReason = "internal"
		}
		finish(&err, "bid_id", bidID, "status", status, "result", bidResult, "reason", bidReason)
	}()

	item, rule, err := s.store.FindItemWithRule(itemID)
	if err != nil {
		bidResult = "error"
		bidReason = "db_error"
		return nil, err
	}
	if item.Status != model.ItemOngoing {
		bidResult = "rejected"
		bidReason = "item_not_ongoing"
		return nil, errorx.ErrInvalidRequest
	}
	if rule.DepositAmount > 0 {
		if s.depositSvc == nil {
			bidResult = "error"
			bidReason = "internal"
			return nil, errorx.ErrInternal
		}
		ok, err := s.depositSvc.HasPaidDeposit(ctx, item.ID, current.ID, rule.DepositAmount)
		if err != nil {
			bidResult = "error"
			bidReason = "db_error"
			return nil, err
		}
		if !ok {
			bidResult = "rejected"
			bidReason = "deposit_required"
			return nil, ErrDepositRequired
		}
	}
	if s.cache == nil {
		bidResult = "error"
		bidReason = "internal"
		return nil, errorx.ErrInternal
	}

	bidID = "bid_" + snowflake.MakeUUID()
	now := s.now()
	luaResult, err := s.cache.PlaceBidLua(ctx, item.ID, itemcache.BidLuaArgs{
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
		NowUnix:           now.Unix(),
		IdempotencyKey:    input.IdempotencyKey,
		IdempotencyTTL:    86400,
	})
	if err != nil {
		bidResult = "error"
		bidReason = "redis_error"
		return nil, err
	}

	switch luaResult.Code {
	case 1: // idempotent: already bid, return current state without writing BidLog again
		bidID = luaResult.BidID
		status = bidStatus(luaResult, "ongoing")
		bidResult = "idempotent"
		bidReason = "idempotency_key"
		return &dto.PlaceBidResult{
			BidID:         luaResult.BidID,
			CurrentPrice:  luaResult.CurrentPrice,
			DealPrice:     luaResult.CurrentPrice,
			LeaderUserID:  luaResult.LeaderUserID,
			EndTime:       time.Unix(luaResult.EndTimeUnix, 0),
			EndTimeUnixMS: bidEndTimeUnixMS(luaResult),
			Status:        status,
		}, nil
	case 2:
		bidResult = "rejected"
		bidReason = "auction_ended"
		return nil, errorx.New(http.StatusBadRequest, 40002, "auction has ended")
	case 3:
		bidResult = "rejected"
		bidReason = "price_too_low"
		return nil, errorx.New(http.StatusBadRequest, 40003, "price too low")
	case 4:
		bidResult = "rejected"
		bidReason = "invalid_bid_increment"
		return nil, errorx.New(http.StatusBadRequest, 40004, "invalid bid increment")
	default:
		if luaResult.Code != 0 {
			bidResult = "error"
			bidReason = "internal"
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
		bidResult = "error"
		bidReason = "db_error"
		return nil, err
	}

	if s.broadcaster != nil {
		endTime := bidEndTime(luaResult)
		endTimeUnixMS := bidEndTimeUnixMS(luaResult)
		s.enqueueBidSuccess(item.RoomID, dto.BidSuccessPayload{
			ItemID:           item.ID,
			UserID:           current.ID,
			Price:            input.Price,
			CurrentPrice:     luaResult.CurrentPrice,
			LeaderUserID:     luaResult.LeaderUserID,
			EndTime:          endTime,
			ServerTimeUnixMS: now.UnixMilli(),
			EndTimeUnixMS:    endTimeUnixMS,
		})
		if luaResult.PrevLeaderUserID != "" && luaResult.PrevLeaderUserID != luaResult.LeaderUserID {
			_ = s.broadcaster.Unicast(wsevent.UserAddr(luaResult.PrevLeaderUserID), wsevent.Event{
				Type: dto.EventUserOutbid,
				Payload: dto.UserOutbidPayload{
					ItemID:           item.ID,
					NewLeaderID:      luaResult.LeaderUserID,
					CurrentPrice:     luaResult.CurrentPrice,
					ServerTimeUnixMS: now.UnixMilli(),
					EndTimeUnixMS:    endTimeUnixMS,
				},
			})
		}
		if luaResult.IsExtended {
			_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
				Type: dto.EventAuctionExtended,
				Payload: dto.AuctionExtendedPayload{
					ItemID:           item.ID,
					NewEndTime:       endTime,
					ExtendSeconds:    s.policy.AutoExtendSec,
					ServerTimeUnixMS: now.UnixMilli(),
					NewEndTimeUnixMS: endTimeUnixMS,
				},
			})
		}
	}

	status = bidStatus(luaResult, "ongoing")
	if luaResult.IsCapped {
		item.Status = model.ItemEnded
		item.WinnerID = current.ID
		item.DealPrice = input.Price
		if err := s.store.UpdateItemWithRule(item, rule); err != nil {
			bidResult = "error"
			bidReason = "db_error"
			return nil, err
		}
		if err := s.store.ClearRoomCurrentItem(item.RoomID, item.ID); err != nil {
			bidResult = "error"
			bidReason = "db_error"
			return nil, err
		}
		if s.cache != nil {
			_ = s.cache.RemoveFromRoomQueue(ctx, item.RoomID, item.ID)
			_ = s.cache.UnscheduleAuctionEnd(ctx, item.ID)
			_ = s.cache.ExpireAuctionState(ctx, item.ID, itemcache.FinalSnapshotTTL)
			_ = s.cache.ClearRoomCurrentItem(ctx, item.RoomID, item.ID)
		}
		status = bidStatus(luaResult, "ended")
		var orderID string
		if s.orderSvc != nil {
			if order, err := s.orderSvc.CreateOrder(ctx, item.ID, current.ID, input.Price); err == nil && order != nil {
				orderID = order.ID
			}
		}
		if s.broadcaster != nil {
			s.flushBidSuccessNow(item.RoomID, item.ID)
			endedAtUnixMS := s.now().UnixMilli()
			_ = s.broadcaster.Fanout(wsevent.RoomTopic(item.RoomID), wsevent.Event{
				Type: dto.EventAuctionEnded,
				Payload: dto.AuctionEndedPayload{
					ItemID:           item.ID,
					WinnerUserID:     current.ID,
					LeaderUserID:     current.ID,
					DealPrice:        input.Price,
					ServerTimeUnixMS: endedAtUnixMS,
					EndedAtUnixMS:    endedAtUnixMS,
					EndReason:        "price_cap",
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
		BidID:         luaResult.BidID,
		CurrentPrice:  luaResult.CurrentPrice,
		DealPrice:     luaResult.CurrentPrice,
		LeaderUserID:  luaResult.LeaderUserID,
		EndTime:       time.Unix(luaResult.EndTimeUnix, 0),
		EndTimeUnixMS: bidEndTimeUnixMS(luaResult),
		Status:        status,
	}, nil
}

func bidEndTimeUnixMS(result *itemcache.BidLuaResult) int64 {
	if result.EndTimeUnixMS > 0 {
		return result.EndTimeUnixMS
	}
	if result.EndTimeUnix > 0 {
		return result.EndTimeUnix * 1000
	}
	return 0
}

func bidEndTime(result *itemcache.BidLuaResult) time.Time {
	if result.EndTimeUnixMS > 0 {
		return time.UnixMilli(result.EndTimeUnixMS)
	}
	if result.EndTimeUnix > 0 {
		return time.Unix(result.EndTimeUnix, 0)
	}
	return time.Time{}
}

func bidStatus(result *itemcache.BidLuaResult, fallback string) string {
	if result.Status != "" {
		return result.Status
	}
	return fallback
}

func (s *Service) GetRanking(ctx context.Context, itemID string, page, pageSize int) (result *dto.RankingResult, err error) {
	itemID = strings.TrimSpace(itemID)
	if page <= 0 {
		page = 1
	}
	switch {
	case pageSize > 100:
		pageSize = 100
	case pageSize <= 0:
		pageSize = 10
	}
	defer observability.Track(ctx, "auction.get_ranking", "item_id", itemID, "page", page, "page_size", pageSize)(&err)

	offset := (page - 1) * pageSize

	var entries []dto.BidderPrice
	if s.cache != nil {
		var err error
		entries, err = s.cache.GetRanking(ctx, itemID, offset, pageSize)
		if err != nil {
			entries = nil // degrade to MySQL fallback; read errors are non-fatal
		}
	}

	if len(entries) == 0 {
		// TODO: ListBidRanking takes a single limit; we pass offset+pageSize and slice in Go.
		// For large page numbers this is an O(page) query — acceptable given leaderboard caps.
		all, err := s.store.ListBidRanking(itemID, offset+pageSize)
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
