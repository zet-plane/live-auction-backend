package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	"github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	"github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
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

	if s.cache == nil {
		bidResult = "error"
		bidReason = "internal"
		return nil, errorx.ErrInternal
	}
	snapshot := s.availabilitySnapshot()
	if !snapshot.Valid {
		bidResult = "rejected"
		bidReason = "availability_invalid"
		return nil, ErrAvailabilityUnavailable
	}
	if snapshot.ActiveRedis == availability.RedisNone {
		bidResult = "rejected"
		if mysqlUnavailableForBids(snapshot) && snapshot.Reason == "mysql_buffering_expired" {
			bidReason = "mysql_buffering_timeout"
		} else {
			bidReason = "redis_unavailable"
		}
		return nil, ErrAvailabilityUnavailable
	}
	if mysqlBufferingExpiredForBids(snapshot, s.now(), s.mysqlBufferingWindow) {
		bidResult = "rejected"
		bidReason = "mysql_buffering_timeout"
		return nil, ErrAvailabilityUnavailable
	}
	if snapshot.Mode == availability.ModeAuctionProtected && !mysqlUnavailableForBids(snapshot) {
		bidResult = "rejected"
		bidReason = "auction_protected"
		return nil, ErrAvailabilityUnavailable
	}
	if !redisWritableForBids(snapshot) {
		bidResult = "rejected"
		bidReason = "redis_unavailable"
		return nil, ErrAvailabilityUnavailable
	}
	if snapshot.ActiveRedis == availability.RedisLocal {
		fenced, fenceErr := s.bidWriteFenced(ctx)
		if fenceErr != nil {
			bidResult = "error"
			bidReason = "redis_error"
			return nil, fenceErr
		}
		if fenced {
			bidResult = "rejected"
			bidReason = "redis_failback_cutover"
			return nil, ErrAvailabilityUnavailable
		}
	}
	hot, err := s.bidHotConfig(ctx, itemID)
	if err != nil {
		if errors.Is(err, errorx.ErrInvalidRequest) {
			bidResult = "rejected"
			bidReason = "item_not_ongoing"
		} else {
			bidResult = "error"
			bidReason = "db_error"
		}
		return nil, err
	}
	if hot.DepositAmount > 0 {
		if s.depositSvc == nil {
			bidResult = "error"
			bidReason = "internal"
			return nil, errorx.ErrInternal
		}
		ok, err := s.depositSvc.HasPaidDeposit(ctx, hot.ItemID, current.ID, hot.DepositAmount)
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
	bidID = "bid_" + snowflake.MakeUUID()
	now := s.now()
	streamStart := time.Now()
	luaResult, err := s.cache.PlaceBidLua(ctx, hot.ItemID, itemcache.BidLuaArgs{
		AuthorityEpoch:    0,
		AuthorityState:    itemcache.AuthorityReady,
		UserID:            current.ID,
		UserName:          input.UserName,
		BidID:             bidID,
		RoomID:            hot.RoomID,
		Price:             input.Price,
		BidIncrement:      hot.BidIncrement,
		PriceCap:          hot.PriceCap,
		ExtendTriggerSec:  hot.ExtendTriggerSec,
		AutoExtendSec:     hot.AutoExtendSec,
		MaxExtendCount:    hot.MaxExtendCount,
		MaxTotalExtendSec: hot.MaxTotalExtendSec,
		NowUnix:           now.Unix(),
		CreatedAtUnixMS:   now.UnixMilli(),
		IdempotencyKey:    input.IdempotencyKey,
		IdempotencyTTL:    86400,
	})
	if err != nil {
		observability.DefaultRecorder().BidLogStream(ctx, observability.BidLogStreamMetric{
			Result:   "error",
			Duration: time.Since(streamStart),
		})
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
	case 5:
		bidResult = "rejected"
		bidReason = "authority_epoch_mismatch"
		return nil, ErrAvailabilityUnavailable
	case 6:
		bidResult = "rejected"
		bidReason = "authority_not_ready"
		return nil, ErrAvailabilityUnavailable
	default:
		if luaResult.Code != 0 {
			bidResult = "error"
			bidReason = "internal"
			return nil, errorx.ErrInternal
		}
	}

	observability.DefaultRecorder().BidLogStream(ctx, observability.BidLogStreamMetric{
		Result:   "success",
		Duration: time.Since(streamStart),
	})

	if s.broadcaster != nil {
		endTime := bidEndTime(luaResult)
		endTimeUnixMS := bidEndTimeUnixMS(luaResult)
		s.enqueueBidSuccess(hot.RoomID, dto.BidSuccessPayload{
			ItemID:           hot.ItemID,
			UserID:           current.ID,
			Price:            input.Price,
			CurrentPrice:     luaResult.CurrentPrice,
			LeaderUserID:     luaResult.LeaderUserID,
			EndTime:          endTime,
			ServerTimeUnixMS: now.UnixMilli(),
			EndTimeUnixMS:    endTimeUnixMS,
			AuctionVersion:   luaResult.AuctionVersion,
		})
		if luaResult.PrevLeaderUserID != "" && luaResult.PrevLeaderUserID != luaResult.LeaderUserID {
			_ = s.broadcaster.Unicast(wsevent.UserAddr(luaResult.PrevLeaderUserID), wsevent.Event{
				Type: dto.EventUserOutbid,
				Payload: dto.UserOutbidPayload{
					ItemID:           hot.ItemID,
					NewLeaderID:      luaResult.LeaderUserID,
					CurrentPrice:     luaResult.CurrentPrice,
					ServerTimeUnixMS: now.UnixMilli(),
					EndTimeUnixMS:    endTimeUnixMS,
					AuctionVersion:   luaResult.AuctionVersion,
				},
			})
		}
		if luaResult.IsExtended {
			_ = s.broadcaster.Fanout(wsevent.RoomTopic(hot.RoomID), wsevent.Event{
				Type: dto.EventAuctionExtended,
				Payload: dto.AuctionExtendedPayload{
					ItemID:           hot.ItemID,
					NewEndTime:       endTime,
					ExtendSeconds:    hot.AutoExtendSec,
					ServerTimeUnixMS: now.UnixMilli(),
					NewEndTimeUnixMS: endTimeUnixMS,
					AuctionVersion:   luaResult.AuctionVersion,
				},
			})
		}
	}

	status = bidStatus(luaResult, "ongoing")
	if luaResult.IsCapped {
		if mysqlUnavailableForBids(snapshot) {
			if s.cache != nil {
				_ = s.cache.RemoveFromRoomQueue(ctx, hot.RoomID, hot.ItemID)
				_ = s.cache.UnscheduleAuctionEnd(ctx, hot.ItemID)
				_ = s.cache.ExpireAuctionState(ctx, hot.ItemID, itemcache.FinalSnapshotTTL)
				_ = s.cache.ClearRoomCurrentItem(ctx, hot.RoomID, hot.ItemID)
			}
			status = bidStatus(luaResult, "ended")
			if s.broadcaster != nil {
				s.flushBidSuccessNow(hot.RoomID, hot.ItemID)
				endedAtUnixMS := s.now().UnixMilli()
				_ = s.broadcaster.Fanout(wsevent.RoomTopic(hot.RoomID), wsevent.Event{
					Type: dto.EventAuctionEnded,
					Payload: dto.AuctionEndedPayload{
						ItemID:           hot.ItemID,
						WinnerUserID:     current.ID,
						LeaderUserID:     current.ID,
						DealPrice:        input.Price,
						ServerTimeUnixMS: endedAtUnixMS,
						EndedAtUnixMS:    endedAtUnixMS,
						EndReason:        "price_cap",
						AuctionVersion:   luaResult.AuctionVersion,
					},
				})
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
		item, rule, err := s.store.FindItemWithRule(hot.ItemID)
		if err != nil {
			bidResult = "error"
			bidReason = "db_error"
			return nil, err
		}
		item.Status = model.ItemEnded
		item.WinnerID = current.ID
		item.DealPrice = input.Price
		if err := s.store.UpdateItemWithRule(item, rule); err != nil {
			bidResult = "error"
			bidReason = "db_error"
			return nil, err
		}
		s.cacheItemDetail(ctx, item, rule)
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
					AuctionVersion:   luaResult.AuctionVersion,
				},
			})
			if orderID != "" {
				orderEvt := wsevent.Event{
					Type: dto.EventOrderCreated,
					Payload: dto.OrderCreatedPayload{
						ItemID:         item.ID,
						OrderID:        orderID,
						WinnerID:       current.ID,
						DealPrice:      input.Price,
						AuctionVersion: luaResult.AuctionVersion,
					},
				}
				_ = s.broadcaster.Unicast(wsevent.UserAddr(current.ID), orderEvt)
			}
		}
		if s.depositSvc != nil {
			if _, refundErr := s.depositSvc.RefundNonWinners(ctx, item.ID, current.ID); refundErr != nil {
				logx.Warnw("item.PlaceBid refund non-winners failed", "item_id", item.ID, "winner_user_id", current.ID, "err", refundErr)
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

func redisWritableForBids(snapshot availability.Snapshot) bool {
	if !snapshot.Valid || (snapshot.ActiveRedis != availability.RedisCloud && snapshot.ActiveRedis != availability.RedisLocal) {
		return false
	}
	return snapshot.Mode != availability.ModeAuctionProtected || mysqlUnavailableForBids(snapshot)
}

func (s *Service) bidWriteFenced(ctx context.Context) (bool, error) {
	fence, ok := s.cache.(itemcache.BidWriteFence)
	if !ok {
		return false, nil
	}
	return fence.BidWriteFenced(ctx)
}

func mysqlUnavailableForBids(snapshot availability.Snapshot) bool {
	return snapshot.MySQLState == availability.MySQLBuffering ||
		snapshot.MySQLState == availability.MySQLDown ||
		(!snapshot.MySQL.Healthy && snapshot.MySQL.Error != "")
}

func mysqlBufferingExpiredForBids(snapshot availability.Snapshot, now time.Time, window time.Duration) bool {
	if snapshot.Reason == "mysql_buffering_expired" {
		return true
	}
	return snapshot.MySQLBufferingExpired(now, window)
}

func (s *Service) bidHotConfig(ctx context.Context, itemID string) (*itemcache.AuctionHotConfig, error) {
	start := time.Now()
	result := "miss"
	defer func() {
		observability.DefaultRecorder().BidHotState(ctx, observability.BidHotStateMetric{
			Result:   result,
			Duration: time.Since(start),
		})
	}()

	snapshot := s.availabilitySnapshot()
	hot, ok, err := s.cache.GetAuctionHotConfig(ctx, itemID)
	if err != nil {
		result = "error"
		return nil, err
	}
	if ok {
		if hot.Status != string(model.ItemOngoing) {
			result = "rejected"
			return nil, errorx.ErrInvalidRequest
		}
	}

	var existing *itemcache.AuctionState
	if state, stateOK, stateErr := s.cache.GetAuctionState(ctx, itemID); stateErr != nil {
		result = "error"
		return nil, stateErr
	} else if stateOK {
		if state.Status != "" && state.Status != string(model.ItemOngoing) {
			result = "rejected"
			return nil, errorx.ErrInvalidRequest
		}
		existing = state
	}
	if existing != nil && existing.AuthorityState == "" {
		if err := s.cache.SetItemAuthority(ctx, itemID, existing.AuthorityEpoch, itemcache.AuthorityReady); err != nil {
			result = "error"
			return nil, err
		}
		existing.AuthorityState = itemcache.AuthorityReady
	}

	if snapshot.Valid && snapshot.ActiveRedis == availability.RedisLocal && existing == nil {
		if rebuildErr := s.rebuildLocalAuctionState(ctx, itemID, 0); rebuildErr != nil {
			result = "rejected"
			return nil, rebuildErr
		}
		if state, stateOK, stateErr := s.cache.GetAuctionState(ctx, itemID); stateErr != nil {
			result = "error"
			return nil, stateErr
		} else if stateOK {
			existing = state
		}
		if hot, ok, err := s.cache.GetAuctionHotConfig(ctx, itemID); err != nil {
			result = "error"
			return nil, err
		} else if ok && hot.Status == string(model.ItemOngoing) {
			result = "rebuilt"
			return hot, nil
		}
	}
	if ok && hot.Status == string(model.ItemOngoing) {
		result = "hit"
		return hot, nil
	}

	item, rule, err := s.getItemDetailSource(ctx, itemID)
	if err != nil {
		result = "error"
		return nil, err
	}
	if item.Status != model.ItemOngoing {
		result = "rejected"
		return nil, errorx.ErrInvalidRequest
	}

	endTimeUnixMS := rule.EndTime.UnixMilli()
	hot = &itemcache.AuctionHotConfig{
		ItemID:            item.ID,
		RoomID:            item.RoomID,
		Status:            string(model.ItemOngoing),
		BidIncrement:      rule.BidIncrement,
		PriceCap:          rule.PriceCap,
		DepositAmount:     rule.DepositAmount,
		ExtendTriggerSec:  s.policy.ExtendTriggerSec,
		AutoExtendSec:     s.policy.AutoExtendSec,
		MaxExtendCount:    s.policy.MaxExtendCount,
		MaxTotalExtendSec: s.policy.MaxTotalExtendSec,
		EndTimeUnixMS:     endTimeUnixMS,
	}
	if existing != nil {
		if existing.EndTimeUnixMS > 0 {
			hot.EndTimeUnixMS = existing.EndTimeUnixMS
			endTimeUnixMS = existing.EndTimeUnixMS
		} else if !existing.EndTime.IsZero() && existing.EndTime.UnixMilli() > 0 {
			hot.EndTimeUnixMS = existing.EndTime.UnixMilli()
			endTimeUnixMS = hot.EndTimeUnixMS
		}
		if err := s.cache.UpdateAuctionHotFields(ctx, item.ID, *hot); err != nil {
			result = "error"
			return nil, err
		}
		result = "rebuilt"
		return hot, nil
	}

	state := itemcache.AuctionState{
		Status:            string(model.ItemOngoing),
		RoomID:            item.RoomID,
		CurrentPrice:      rule.StartPrice,
		DealPrice:         rule.StartPrice,
		EndTime:           rule.EndTime,
		EndTimeUnixMS:     endTimeUnixMS,
		BidIncrement:      rule.BidIncrement,
		PriceCap:          rule.PriceCap,
		DepositAmount:     rule.DepositAmount,
		ExtendTriggerSec:  s.policy.ExtendTriggerSec,
		AutoExtendSec:     s.policy.AutoExtendSec,
		MaxExtendCount:    s.policy.MaxExtendCount,
		MaxTotalExtendSec: s.policy.MaxTotalExtendSec,
	}
	if err := s.cache.InitAuctionState(ctx, item.ID, state); err != nil {
		result = "error"
		return nil, err
	}
	if err := s.cache.ScheduleAuctionEnd(ctx, item.ID, endTimeUnixMS); err != nil {
		result = "error"
		return nil, err
	}
	result = "rebuilt"
	return hot, nil
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

func (s *Service) GetRanking(ctx context.Context, itemID string, page, pageSize int, currentUsers ...*usermodel.User) (result *dto.RankingResult, err error) {
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
		if err := s.ensureLocalAuctionState(ctx, itemID); err != nil {
			return nil, err
		}
		var err error
		entries, err = s.cache.GetRanking(ctx, itemID, offset, pageSize)
		if err != nil {
			logx.Warnw("item.GetRanking get redis ranking failed", "item_id", itemID, "err", err)
			entries = nil
		}
		if len(entries) == 0 && s.shouldRebuildRanking(ctx, itemID) {
			rebuilt, err := s.rebuildRankingOnce(ctx, itemID, offset+pageSize)
			if err != nil {
				return nil, err
			}
			if offset < len(rebuilt) {
				entries = rebuilt[offset:]
			}
			if len(entries) > pageSize {
				entries = entries[:pageSize]
			}
		}
	}

	if len(entries) == 0 && s.cache == nil {
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
	ranking := &dto.RankingResult{List: list, Page: page, PageSize: pageSize}
	if current := firstCurrentUser(currentUsers); current != nil && current.ID != "" {
		ranking.CurrentUser, err = s.currentUserRanking(ctx, itemID, current.ID)
		if err != nil {
			return nil, err
		}
	}
	return ranking, nil
}

func (s *Service) shouldRebuildRanking(ctx context.Context, itemID string) bool {
	if s.cache == nil {
		return false
	}
	state, ok, err := s.cache.GetAuctionState(ctx, itemID)
	if err != nil {
		logx.Warnw("item.GetRanking get auction state failed", "item_id", itemID, "err", err)
		return true
	}
	return !ok || state.BidCount > 0
}

func (s *Service) ensureLocalAuctionState(ctx context.Context, itemID string) error {
	if s.cache == nil || s.availabilitySnapshot().ActiveRedis != availability.RedisLocal {
		return nil
	}
	state, ok, err := s.cache.GetAuctionState(ctx, itemID)
	if err != nil || (ok && usableAuctionState(state)) {
		return err
	}
	return s.rebuildLocalAuctionState(ctx, itemID, 0)
}

func (s *Service) rebuildRankingOnce(ctx context.Context, itemID string, limit int) ([]dto.BidderPrice, error) {
	if limit <= 0 {
		return nil, nil
	}
	value, err, _ := s.rankingRebuilds.Do("ranking:"+itemID, func() (any, error) {
		if s.cache != nil {
			coolingDown, err := s.cache.RankingRebuildCoolingDown(ctx, itemID)
			if err != nil {
				logx.Warnw("item.GetRanking check rebuild cooldown failed", "item_id", itemID, "err", err)
			}
			if coolingDown {
				return s.waitForRankingRebuild(ctx, itemID, limit), nil
			}
			acquired, err := s.cache.AcquireRankingRebuild(ctx, itemID, s.rankingRebuildOwner, s.rankingRebuildLockTTL)
			if err != nil {
				logx.Warnw("item.GetRanking acquire ranking rebuild lock failed", "item_id", itemID, "err", err)
				return s.waitForRankingRebuild(ctx, itemID, limit), nil
			}
			if !acquired {
				return s.waitForRankingRebuild(ctx, itemID, limit), nil
			}
		}

		entries, err := s.store.ListBidRanking(itemID, limit)
		if err != nil {
			if s.cache != nil {
				_ = s.cache.SetRankingRebuildCooldown(ctx, itemID, s.rankingRebuildCooldownTTL)
			}
			return nil, err
		}
		if s.cache != nil {
			if len(entries) > 0 {
				if err := s.cache.SetRanking(ctx, itemID, entries); err != nil {
					logx.Warnw("item.GetRanking set rebuilt redis ranking failed", "item_id", itemID, "err", err)
				}
			} else {
				_ = s.cache.SetRankingRebuildCooldown(ctx, itemID, s.rankingRebuildCooldownTTL)
			}
		}
		return entries, nil
	})
	if err != nil {
		return nil, err
	}
	entries, _ := value.([]dto.BidderPrice)
	return entries, nil
}

func (s *Service) rebuildLocalAuctionState(ctx context.Context, itemID string, epoch int64) error {
	_, err, _ := s.rankingRebuilds.Do("availability-rebuild:"+itemID+":"+strconv.FormatInt(epoch, 10), func() (any, error) {
		worker := newAvailabilityRebuildWorker(s.store, s.cache, availabilityRebuildConfig{BatchSize: 1, Policy: s.policy})
		switch worker.rebuildItem(ctx, itemID, epoch) {
		case rebuildReady:
			return nil, nil
		case rebuildProtected:
			return nil, ErrAvailabilityUnavailable
		default:
			return nil, ErrAvailabilityUnavailable
		}
	})
	return err
}

func (s *Service) waitForRankingRebuild(ctx context.Context, itemID string, limit int) []dto.BidderPrice {
	if s.cache == nil {
		return nil
	}
	timer := time.NewTimer(s.rankingRebuildWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil
	case <-timer.C:
	}
	entries, err := s.cache.GetRanking(ctx, itemID, 0, limit)
	if err != nil {
		logx.Warnw("item.GetRanking reread ranking after rebuild wait failed", "item_id", itemID, "err", err)
		return nil
	}
	return entries
}

func firstCurrentUser(users []*usermodel.User) *usermodel.User {
	if len(users) == 0 {
		return nil
	}
	return users[0]
}

func (s *Service) currentUserRanking(ctx context.Context, itemID, userID string) (*dto.CurrentUserRanking, error) {
	if s.cache != nil {
		result, err := s.cache.GetUserRanking(ctx, itemID, userID)
		if err != nil {
			logx.Warnw("item.GetRanking get redis user ranking failed", "item_id", itemID, "user_id", userID, "err", err)
			return &dto.CurrentUserRanking{UserID: userID}, nil
		}
		if result != nil {
			return result, nil
		}
		return &dto.CurrentUserRanking{UserID: userID}, nil
	}
	result, err := s.store.GetUserRanking(itemID, userID)
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}
	return &dto.CurrentUserRanking{UserID: userID}, nil
}
