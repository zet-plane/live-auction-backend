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
)

func (s *Service) PlaceBid(current *usermodel.User, itemID string, input dto.PlaceBidInput) (*dto.PlaceBidResult, error) {
	item, rule, err := s.store.FindItemWithRule(strings.TrimSpace(itemID))
	if err != nil {
		return nil, err
	}
	if item.Status != model.ItemOngoing {
		return nil, errorx.ErrInvalidRequest
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

	status := "ongoing"
	if luaResult.IsCapped {
		item.Status = model.ItemEnded
		item.WinnerID = current.ID
		item.DealPrice = input.Price
		if err := s.store.UpdateItemWithRule(item, rule); err != nil {
			return nil, err
		}
		status = "ended"
		// TODO: broadcast auction_ended WebSocket event (implement after WS module)
	}

	return &dto.PlaceBidResult{
		BidID:        luaResult.BidID,
		CurrentPrice: luaResult.CurrentPrice,
		LeaderUserID: luaResult.LeaderUserID,
		EndTime:      time.Unix(luaResult.EndTimeUnix, 0),
		Status:       status,
	}, nil
}
