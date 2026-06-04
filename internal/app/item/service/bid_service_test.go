package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	itemcache "github.com/zet-plane/live-auction-backend/internal/app/item/cache"
	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

var (
	testPolicy = itemdto.AuctionPolicy{
		ExtendTriggerSec:  30,
		AutoExtendSec:     10,
		MaxExtendCount:    6,
		MaxTotalExtendSec: 300,
	}
	bidder = &usermodel.User{ID: "user_1", Name: "Alice", Identity: usermodel.IdentityUser}
)

func seedOngoingItem(t *testing.T, svc *Service, merchantID, roomID string, startPrice, bidIncrement, priceCap int64, endTime time.Time) string {
	t.Helper()
	start := endTime.Add(-10 * time.Minute)
	result, err := svc.CreateItem(context.Background(),
		&usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant},
		itemdto.CreateItemInput{
			RoomID: roomID,
			Title:  "Test Item",
			Rule: itemdto.RuleInput{
				StartPrice:   startPrice,
				BidIncrement: bidIncrement,
				PriceCap:     priceCap,
				StartTime:    start,
				EndTime:      endTime,
			},
		},
	)
	if err != nil {
		t.Fatalf("CreateItem failed: %v", err)
	}
	merchant := &usermodel.User{ID: merchantID, Identity: usermodel.IdentityMerchant}
	if err := svc.PublishItem(context.Background(), merchant, result.ItemID); err != nil {
		t.Fatalf("PublishItem failed: %v", err)
	}
	if err := svc.StartItem(context.Background(), merchant, result.ItemID); err != nil {
		t.Fatalf("StartItem failed: %v", err)
	}
	return result.ItemID
}

func TestPlaceBidSucceeds(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	result, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          100,
		IdempotencyKey: "idem_001",
		UserName:       "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if result.BidID == "" {
		t.Fatal("expected non-empty bid_id")
	}
	if result.CurrentPrice != 100 {
		t.Fatalf("expected current_price 100, got %d", result.CurrentPrice)
	}
	if result.DealPrice != 100 {
		t.Fatalf("expected deal_price 100, got %d", result.DealPrice)
	}
	if result.EndTimeUnixMS == 0 {
		t.Fatal("expected end_time_unix_ms")
	}
	if result.LeaderUserID != "user_1" {
		t.Fatalf("expected leader user_1, got %q", result.LeaderUserID)
	}
	if result.Status != "ongoing" {
		t.Fatalf("expected status ongoing, got %q", result.Status)
	}
	if len(store.bidLogs) != 1 {
		t.Fatalf("expected 1 bid log, got %d", len(store.bidLogs))
	}
	if store.bidLogs[0].RoomID != "room_1" {
		t.Fatalf("expected room_id room_1, got %q", store.bidLogs[0].RoomID)
	}
}

func TestPlaceBidUsesHotStateWithoutStoreLookup(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	store.findItemWithRuleCalls = 0
	fc.states[itemID] = &itemcache.AuctionState{
		Status:            "ongoing",
		RoomID:            "room_1",
		CurrentPrice:      1000,
		DealPrice:         1000,
		EndTime:           endTime,
		EndTimeUnixMS:     endTime.UnixMilli(),
		BidIncrement:      100,
		PriceCap:          0,
		DepositAmount:     0,
		ExtendTriggerSec:  testPolicy.ExtendTriggerSec,
		AutoExtendSec:     testPolicy.AutoExtendSec,
		MaxExtendCount:    testPolicy.MaxExtendCount,
		MaxTotalExtendSec: testPolicy.MaxTotalExtendSec,
	}
	fc.bidLuaResult = &itemcache.BidLuaResult{
		Code:          0,
		BidID:         "bid_hot",
		CurrentPrice:  1100,
		LeaderUserID:  "user_1",
		EndTimeUnix:   endTime.Unix(),
		EndTimeUnixMS: endTime.UnixMilli(),
		Status:        "ongoing",
	}

	result, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1100,
		IdempotencyKey: "hot_state",
		UserName:       "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if result.BidID != "bid_hot" {
		t.Fatalf("expected bid_id bid_hot, got %q", result.BidID)
	}
	if store.findItemWithRuleCalls != 0 {
		t.Fatalf("expected no FindItemWithRule calls, got %d", store.findItemWithRuleCalls)
	}
}

func TestPlaceBidSuccessfulBidAppendsBidLogEvent(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	now := time.Date(2026, 6, 1, 12, 0, 0, 123_000_000, time.UTC)
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	svc.now = func() time.Time { return now }
	endTime := now.Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	fc.states[itemID] = &itemcache.AuctionState{
		Status:            "ongoing",
		RoomID:            "room_1",
		CurrentPrice:      1000,
		DealPrice:         1000,
		EndTime:           endTime,
		EndTimeUnixMS:     endTime.UnixMilli(),
		BidIncrement:      100,
		PriceCap:          0,
		DepositAmount:     0,
		ExtendTriggerSec:  testPolicy.ExtendTriggerSec,
		AutoExtendSec:     testPolicy.AutoExtendSec,
		MaxExtendCount:    testPolicy.MaxExtendCount,
		MaxTotalExtendSec: testPolicy.MaxTotalExtendSec,
	}
	fc.bidLuaResult = &itemcache.BidLuaResult{
		Code:          0,
		BidID:         "bid_stream",
		CurrentPrice:  1200,
		LeaderUserID:  "user_1",
		EndTimeUnix:   endTime.Unix(),
		EndTimeUnixMS: endTime.UnixMilli(),
		Status:        "ongoing",
	}

	if _, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1200,
		IdempotencyKey: "stream_success",
		UserName:       "Alice",
	}); err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}

	if len(fc.bidLogEvents) != 1 {
		t.Fatalf("expected 1 bid log event, got %d", len(fc.bidLogEvents))
	}
	event := fc.bidLogEvents[0]
	if event.BidID != "bid_stream" ||
		event.ItemID != itemID ||
		event.RoomID != "room_1" ||
		event.UserID != "user_1" ||
		event.Price != 1200 {
		t.Fatalf("unexpected bid log event: %+v", event)
	}
	if event.CreatedAtUnixMS != now.UnixMilli() {
		t.Fatalf("expected created_at_unix_ms %d, got %d", now.UnixMilli(), event.CreatedAtUnixMS)
	}
}

func TestPlaceBidIdempotentResultDoesNotAppendBidLogEvent(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	fc.states[itemID] = &itemcache.AuctionState{
		Status:            "ongoing",
		RoomID:            "room_1",
		CurrentPrice:      1200,
		DealPrice:         1200,
		LeaderUserID:      "user_1",
		EndTime:           endTime,
		EndTimeUnixMS:     endTime.UnixMilli(),
		BidIncrement:      100,
		PriceCap:          0,
		DepositAmount:     0,
		ExtendTriggerSec:  testPolicy.ExtendTriggerSec,
		AutoExtendSec:     testPolicy.AutoExtendSec,
		MaxExtendCount:    testPolicy.MaxExtendCount,
		MaxTotalExtendSec: testPolicy.MaxTotalExtendSec,
	}
	fc.bidLuaResult = &itemcache.BidLuaResult{
		Code:          1,
		BidID:         "bid_existing",
		CurrentPrice:  1200,
		LeaderUserID:  "user_1",
		EndTimeUnix:   endTime.Unix(),
		EndTimeUnixMS: endTime.UnixMilli(),
		Status:        "ongoing",
	}

	if _, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1200,
		IdempotencyKey: "stream_idempotent",
		UserName:       "Alice",
	}); err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}

	if len(fc.bidLogEvents) != 0 {
		t.Fatalf("expected no bid log events, got %d: %+v", len(fc.bidLogEvents), fc.bidLogEvents)
	}
}

func TestPlaceBidLowPriceRejectionDoesNotTouchStore(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	store.findItemWithRuleCalls = 0
	fc.states[itemID] = &itemcache.AuctionState{
		Status:            "ongoing",
		RoomID:            "room_1",
		CurrentPrice:      1000,
		DealPrice:         1000,
		EndTime:           endTime,
		EndTimeUnixMS:     endTime.UnixMilli(),
		BidIncrement:      100,
		PriceCap:          0,
		DepositAmount:     0,
		ExtendTriggerSec:  testPolicy.ExtendTriggerSec,
		AutoExtendSec:     testPolicy.AutoExtendSec,
		MaxExtendCount:    testPolicy.MaxExtendCount,
		MaxTotalExtendSec: testPolicy.MaxTotalExtendSec,
	}
	fc.bidLuaResult = &itemcache.BidLuaResult{Code: 3}

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1000,
		IdempotencyKey: "low_price_hot_state",
		UserName:       "Alice",
	})
	if err == nil {
		t.Fatal("expected error for price too low")
	}
	if store.findItemWithRuleCalls != 0 {
		t.Fatalf("expected no FindItemWithRule calls, got %d", store.findItemWithRuleCalls)
	}
	if len(store.bidLogs) != 0 {
		t.Fatalf("expected no bid logs, got %d", len(store.bidLogs))
	}
}

func TestPlaceBidRejectsNonOngoingHotStateWithoutStoreLookup(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	store.findItemWithRuleCalls = 0
	fc.states[itemID] = &itemcache.AuctionState{
		Status:            "ended",
		RoomID:            "room_1",
		CurrentPrice:      1000,
		DealPrice:         1000,
		EndTime:           endTime,
		EndTimeUnixMS:     endTime.UnixMilli(),
		BidIncrement:      100,
		PriceCap:          0,
		DepositAmount:     0,
		ExtendTriggerSec:  testPolicy.ExtendTriggerSec,
		AutoExtendSec:     testPolicy.AutoExtendSec,
		MaxExtendCount:    testPolicy.MaxExtendCount,
		MaxTotalExtendSec: testPolicy.MaxTotalExtendSec,
	}

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1100,
		IdempotencyKey: "ended_hot_state",
		UserName:       "Alice",
	})
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request for non-ongoing hot state, got %v", err)
	}
	if store.findItemWithRuleCalls != 0 {
		t.Fatalf("expected no FindItemWithRule calls, got %d", store.findItemWithRuleCalls)
	}
}

func TestPlaceBidRebuildsHotStateWhenStatusMissing(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	store.findItemWithRuleCalls = 0
	fc.initCalls = 0
	fc.hotFieldUpdates = 0
	fc.states[itemID] = &itemcache.AuctionState{
		RoomID:            "room_1",
		CurrentPrice:      1000,
		DealPrice:         1000,
		EndTime:           endTime,
		EndTimeUnixMS:     endTime.UnixMilli(),
		BidIncrement:      100,
		PriceCap:          0,
		DepositAmount:     0,
		ExtendTriggerSec:  testPolicy.ExtendTriggerSec,
		AutoExtendSec:     testPolicy.AutoExtendSec,
		MaxExtendCount:    testPolicy.MaxExtendCount,
		MaxTotalExtendSec: testPolicy.MaxTotalExtendSec,
	}

	result, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1100,
		IdempotencyKey: "missing_status_hot_state",
		UserName:       "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed after hot state rebuild: %v", err)
	}
	if result.CurrentPrice != 1100 {
		t.Fatalf("expected current_price 1100, got %d", result.CurrentPrice)
	}
	if store.findItemWithRuleCalls != 1 {
		t.Fatalf("expected one FindItemWithRule call for rebuild, got %d", store.findItemWithRuleCalls)
	}
	if fc.initCalls != 0 {
		t.Fatalf("expected no full InitAuctionState call for existing missing-status repair, got %d", fc.initCalls)
	}
	if fc.hotFieldUpdates != 1 {
		t.Fatalf("expected one hot field update for missing-status repair, got %d", fc.hotFieldUpdates)
	}
	if fc.states[itemID].Status != "" {
		t.Fatalf("expected missing status to remain untouched during hot repair, got %q", fc.states[itemID].Status)
	}
}

func TestPlaceBidHotFieldRepairDoesNotOverwriteEndedStatus(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	store.findItemWithRuleCalls = 0
	fc.initCalls = 0
	fc.hotFieldUpdates = 0
	fc.endBeforeHotUpdate = true
	fc.states[itemID] = &itemcache.AuctionState{
		Status:        "ongoing",
		RoomID:        "room_1",
		CurrentPrice:  1000,
		DealPrice:     1000,
		EndTime:       endTime,
		EndTimeUnixMS: endTime.UnixMilli(),
		BidIncrement:  100,
	}

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1100,
		IdempotencyKey: "ended_during_hot_repair",
		UserName:       "Alice",
	})
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40002 {
		t.Fatalf("expected auction ended after concurrent status change, got %v", err)
	}
	if store.findItemWithRuleCalls != 1 {
		t.Fatalf("expected one FindItemWithRule call for repair, got %d", store.findItemWithRuleCalls)
	}
	if fc.initCalls != 0 {
		t.Fatalf("expected no full InitAuctionState call for hot repair, got %d", fc.initCalls)
	}
	if fc.hotFieldUpdates != 1 {
		t.Fatalf("expected one hot field update, got %d", fc.hotFieldUpdates)
	}
	if fc.states[itemID].Status != "ended" {
		t.Fatalf("expected repair to preserve ended status, got %q", fc.states[itemID].Status)
	}
}

func TestPlaceBidCompletesIncompleteHotStateWithoutResettingPrice(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	store.findItemWithRuleCalls = 0
	fc.initCalls = 0
	fc.hotFieldUpdates = 0
	fc.states[itemID] = &itemcache.AuctionState{
		Status:            "ongoing",
		CurrentPrice:      1500,
		DealPrice:         1500,
		LeaderUserID:      "user_2",
		EndTime:           endTime,
		EndTimeUnixMS:     endTime.UnixMilli(),
		BidCount:          3,
		ParticipantCount:  2,
		ExtendCount:       1,
		TotalExtendedSec:  10,
		IsExtended:        true,
		ExtendTriggerSec:  testPolicy.ExtendTriggerSec,
		AutoExtendSec:     testPolicy.AutoExtendSec,
		MaxExtendCount:    testPolicy.MaxExtendCount,
		MaxTotalExtendSec: testPolicy.MaxTotalExtendSec,
	}

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1100,
		IdempotencyKey: "preserve_dynamic_state",
		UserName:       "Alice",
	})
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40003 {
		t.Fatalf("expected price too low after preserving current price, got %v", err)
	}
	if store.findItemWithRuleCalls != 1 {
		t.Fatalf("expected one FindItemWithRule call for repair, got %d", store.findItemWithRuleCalls)
	}
	if fc.initCalls != 0 {
		t.Fatalf("expected no full InitAuctionState call for existing state repair, got %d", fc.initCalls)
	}
	if fc.hotFieldUpdates != 1 {
		t.Fatalf("expected one hot field update for existing state repair, got %d", fc.hotFieldUpdates)
	}
	state := fc.states[itemID]
	if state.CurrentPrice != 1500 || state.DealPrice != 1500 || state.LeaderUserID != "user_2" {
		t.Fatalf("expected dynamic state preserved, got price/deal/leader %d/%d/%q", state.CurrentPrice, state.DealPrice, state.LeaderUserID)
	}
	if state.RoomID != "room_1" || state.BidIncrement != 100 {
		t.Fatalf("expected hot fields completed from DB, got room=%q increment=%d", state.RoomID, state.BidIncrement)
	}
}

func TestPlaceBidRepairsMissingPolicyHotFields(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 1000, 100, 0, endTime)

	store.findItemWithRuleCalls = 0
	fc.initCalls = 0
	fc.hotFieldUpdates = 0
	fc.states[itemID] = &itemcache.AuctionState{
		Status:        "ongoing",
		RoomID:        "room_1",
		CurrentPrice:  1000,
		DealPrice:     1000,
		EndTime:       endTime,
		EndTimeUnixMS: endTime.UnixMilli(),
		BidIncrement:  100,
	}

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price:          1100,
		IdempotencyKey: "repair_policy_fields",
		UserName:       "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed after policy repair: %v", err)
	}
	if store.findItemWithRuleCalls != 1 {
		t.Fatalf("expected one FindItemWithRule call for policy repair, got %d", store.findItemWithRuleCalls)
	}
	if fc.initCalls != 0 {
		t.Fatalf("expected no full InitAuctionState call for policy repair, got %d", fc.initCalls)
	}
	if fc.hotFieldUpdates != 1 {
		t.Fatalf("expected one hot field update for policy repair, got %d", fc.hotFieldUpdates)
	}
	if fc.lastBidLuaArgs == nil {
		t.Fatal("expected PlaceBidLua to be called")
	}
	if fc.lastBidLuaArgs.ExtendTriggerSec != testPolicy.ExtendTriggerSec ||
		fc.lastBidLuaArgs.AutoExtendSec != testPolicy.AutoExtendSec ||
		fc.lastBidLuaArgs.MaxExtendCount != testPolicy.MaxExtendCount ||
		fc.lastBidLuaArgs.MaxTotalExtendSec != testPolicy.MaxTotalExtendSec {
		t.Fatalf("expected repaired policy args %+v, got %+v", testPolicy, fc.lastBidLuaArgs)
	}
}

func TestPlaceBidRejectsNonOngoingItem(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_1")

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{Price: 100, IdempotencyKey: "k1"})
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request for non-ongoing item, got %v", err)
	}
}

func TestPlaceBidRejectsPriceTooLow(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.bidLuaCode = 3
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{Price: 50, IdempotencyKey: "k1"})
	if err == nil {
		t.Fatal("expected error for price too low")
	}
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40003 {
		t.Fatalf("expected code 40003, got %v", err)
	}
}

func TestPlaceBidRejectsInvalidIncrement(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.bidLuaCode = 4
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{Price: 150, IdempotencyKey: "k1"})
	if err == nil {
		t.Fatal("expected error for invalid increment")
	}
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40004 {
		t.Fatalf("expected code 40004, got %v", err)
	}
}

func TestPlaceBidRejectsEndedAuction(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.bidLuaCode = 2
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{Price: 100, IdempotencyKey: "k1"})
	if err == nil {
		t.Fatal("expected error for ended auction")
	}
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40002 {
		t.Fatalf("expected code 40002, got %v", err)
	}
}

func TestPlaceBidIdempotent(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	if _, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "idem_dup", UserName: "Alice",
	}); err != nil {
		t.Fatalf("first bid failed: %v", err)
	}
	// Force idempotency code on second call (fakeCache returns code=1, skips BidLog write)
	fc.bidLuaCode = 1
	if _, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "idem_dup", UserName: "Alice",
	}); err != nil {
		t.Fatalf("idempotent bid should not fail: %v", err)
	}
	// BidLog must not be written a second time
	count := 0
	for _, l := range store.bidLogs {
		if l.ItemID == itemID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 bid log after idempotent retry, got %d", count)
	}
}

func TestPlaceBidIdempotentRejectsEndedHotState(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	svc.now = func() time.Time { return now }
	endTime := now.Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	if _, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "idem_ended", UserName: "Alice",
	}); err != nil {
		t.Fatalf("first bid failed: %v", err)
	}
	endedAt := now.Add(time.Second).UnixMilli()
	fc.states[itemID].Status = "ended"
	fc.states[itemID].EndedAtUnixMS = endedAt
	fc.states[itemID].EndReason = "time_expired"

	fc.bidLuaCode = 1
	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "idem_ended", UserName: "Alice",
	})
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request for ended hot state, got %v", err)
	}
}

func TestPlaceBidPriceCapEndsAuction(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 500, endTime)

	result, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{Price: 500, IdempotencyKey: "idem_cap"})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if result.Status != "ended" {
		t.Fatalf("expected status ended when price cap reached, got %q", result.Status)
	}
	item := store.items[itemID]
	if item.Status != itemmodel.ItemEnded {
		t.Fatalf("expected item status ended in MySQL, got %q", item.Status)
	}
	if item.WinnerID != "user_1" {
		t.Fatalf("expected winner user_1, got %q", item.WinnerID)
	}
	if item.DealPrice != 500 {
		t.Fatalf("expected deal_price 500, got %d", item.DealPrice)
	}
	if got := store.roomCurrentItems["room_1"]; got != "" {
		t.Fatalf("expected MySQL room current item cleared, got %q", got)
	}
	if got := fc.roomCurrent["room_1"]; got != "" {
		t.Fatalf("expected Redis room current item cleared, got %q", got)
	}
	state := fc.states[itemID]
	if state == nil {
		t.Fatal("expected final auction state snapshot to remain in cache")
	}
	if state.Status != "ended" {
		t.Fatalf("expected redis state status ended, got %q", state.Status)
	}
	if state.LeaderUserID != "user_1" || state.DealPrice != 500 {
		t.Fatalf("expected final leader/deal user_1/500, got %q/%d", state.LeaderUserID, state.DealPrice)
	}
	if state.EndReason != "price_cap" {
		t.Fatalf("expected end_reason price_cap, got %q", state.EndReason)
	}
	if got := fc.stateTTLs[itemID]; got != itemcache.FinalSnapshotTTL {
		t.Fatalf("expected final snapshot TTL %s, got %s", itemcache.FinalSnapshotTTL, got)
	}
	if _, ok := fc.ending[itemID]; ok {
		t.Fatal("expected auction end unscheduled from cache")
	}
	for _, id := range fc.queues["room_1"] {
		if id == itemID {
			t.Fatal("expected item removed from room queue")
		}
	}
}

type fakeDepositChecker struct {
	paid  bool
	err   error
	calls int
}

func (f *fakeDepositChecker) HasPaidDeposit(_ context.Context, itemID, userID string, requiredAmount int64) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.paid, nil
}

func TestPlaceBidSkipsDepositCheckWhenNotRequired(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	deposits := &fakeDepositChecker{paid: false}
	svc := NewService(store, testPolicy, fc, nil, deposits, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "no_deposit_required", UserName: "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if deposits.calls != 0 {
		t.Fatalf("expected deposit checker not to be called, got %d calls", deposits.calls)
	}
}

func TestPlaceBidRejectsMissingDepositBeforeRedis(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	deposits := &fakeDepositChecker{paid: false}
	svc := NewService(store, testPolicy, fc, nil, deposits, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)
	rule := store.rules[store.items[itemID].RuleID]
	rule.DepositAmount = 5000
	fc.states[itemID].DepositAmount = 5000

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "missing_deposit", UserName: "Alice",
	})
	if err == nil {
		t.Fatal("expected deposit required error")
	}
	var ce *errorx.CodeError
	if !errors.As(err, &ce) || ce.Code != 40005 {
		t.Fatalf("expected code 40005, got %v", err)
	}
	if deposits.calls != 1 {
		t.Fatalf("expected one deposit checker call, got %d", deposits.calls)
	}
	if len(store.bidLogs) != 0 {
		t.Fatalf("expected no bid logs, got %d", len(store.bidLogs))
	}
	if len(fc.ranking[itemID]) != 0 {
		t.Fatalf("expected Redis fake ranking not to record bids, got %d", len(fc.ranking[itemID]))
	}
}

func TestPlaceBidAllowsPaidDeposit(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	deposits := &fakeDepositChecker{paid: true}
	svc := NewService(store, testPolicy, fc, nil, deposits, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)
	rule := store.rules[store.items[itemID].RuleID]
	rule.DepositAmount = 5000
	fc.states[itemID].DepositAmount = 5000

	result, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "paid_deposit", UserName: "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	if result.CurrentPrice != 100 {
		t.Fatalf("expected current price 100, got %d", result.CurrentPrice)
	}
	if deposits.calls != 1 {
		t.Fatalf("expected one deposit checker call, got %d", deposits.calls)
	}
	if len(store.bidLogs) != 1 {
		t.Fatalf("expected one bid log, got %d", len(store.bidLogs))
	}
}

func TestGetRankingFromCache(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	users := []struct {
		user  *usermodel.User
		price int64
		idem  string
	}{
		{&usermodel.User{ID: "u1", Name: "Alice"}, 300, "k1"},
		{&usermodel.User{ID: "u3", Name: "Carol"}, 400, "k3"},
		{&usermodel.User{ID: "u2", Name: "Bob"}, 500, "k2"},
	}
	for _, u := range users {
		if _, err := svc.PlaceBid(context.Background(), u.user, itemID, itemdto.PlaceBidInput{
			Price: u.price, IdempotencyKey: u.idem, UserName: u.user.Name,
		}); err != nil {
			t.Fatalf("PlaceBid for %s failed: %v", u.user.ID, err)
		}
	}

	result, err := svc.GetRanking(context.Background(), itemID, 1, 10)
	if err != nil {
		t.Fatalf("GetRanking failed: %v", err)
	}
	if len(result.List) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result.List))
	}
	if result.List[0].UserID != "u2" || result.List[0].Price != 500 {
		t.Fatalf("expected rank 1 = u2/500, got %+v", result.List[0])
	}
	if result.List[0].Rank != 1 {
		t.Fatalf("expected rank 1, got %d", result.List[0].Rank)
	}
	if result.List[1].UserID != "u3" || result.List[1].Price != 400 {
		t.Fatalf("expected rank 2 = u3/400, got %+v", result.List[1])
	}
	if result.List[2].UserID != "u1" || result.List[2].Price != 300 {
		t.Fatalf("expected rank 3 = u1/300, got %+v", result.List[2])
	}
}

func TestGetRankingFallsBackToMySQL(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	// Seed bid logs directly in fakeStore (simulate Redis miss)
	store.bidLogs = append(store.bidLogs,
		&itemmodel.BidLog{ID: "b1", ItemID: itemID, RoomID: "room_1", UserID: "u1", Price: 200},
		&itemmodel.BidLog{ID: "b2", ItemID: itemID, RoomID: "room_1", UserID: "u2", Price: 300},
	)
	// No bids placed via service so fc.ranking[itemID] was never populated — cache miss is natural.

	result, err := svc.GetRanking(context.Background(), itemID, 1, 10)
	if err != nil {
		t.Fatalf("GetRanking fallback failed: %v", err)
	}
	if len(result.List) != 2 {
		t.Fatalf("expected 2 entries from MySQL fallback, got %d", len(result.List))
	}
	if result.List[0].Price != 300 {
		t.Fatalf("expected highest price first, got %d", result.List[0].Price)
	}
}

func TestGetRankingPagination(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc, nil, nil, nil)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	for i := 1; i <= 5; i++ {
		u := &usermodel.User{ID: fmt.Sprintf("u%d", i), Name: fmt.Sprintf("User%d", i)}
		_, err := svc.PlaceBid(context.Background(), u, itemID, itemdto.PlaceBidInput{
			Price:          int64(i * 100),
			IdempotencyKey: fmt.Sprintf("k%d", i),
			UserName:       u.Name,
		})
		if err != nil {
			t.Fatalf("PlaceBid failed: %v", err)
		}
	}

	r, err := svc.GetRanking(context.Background(), itemID, 1, 2)
	if err != nil {
		t.Fatalf("GetRanking page 1 failed: %v", err)
	}
	if len(r.List) != 2 {
		t.Fatalf("expected 2 entries on page 1, got %d", len(r.List))
	}
	if r.List[0].Rank != 1 || r.List[1].Rank != 2 {
		t.Fatalf("expected ranks 1,2 on page 1, got %d,%d", r.List[0].Rank, r.List[1].Rank)
	}

	r2, err := svc.GetRanking(context.Background(), itemID, 2, 2)
	if err != nil {
		t.Fatalf("GetRanking page 2 failed: %v", err)
	}
	if len(r2.List) != 2 {
		t.Fatalf("expected 2 entries on page 2, got %d", len(r2.List))
	}
	if r2.List[0].Rank != 3 {
		t.Fatalf("expected rank 3 on page 2, got %d", r2.List[0].Rank)
	}
}

type fakeBroadcaster struct {
	mu       sync.Mutex
	fanouts  []fakeFanout
	unicasts []fakeUnicast
}
type fakeFanout struct {
	topic string
	event wsevent.Event
}
type fakeUnicast struct {
	addr  string
	event wsevent.Event
}

func (f *fakeBroadcaster) Fanout(topic string, event wsevent.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fanouts = append(f.fanouts, fakeFanout{topic, event})
	return nil
}
func (f *fakeBroadcaster) Unicast(addr string, event wsevent.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unicasts = append(f.unicasts, fakeUnicast{addr, event})
	return nil
}

func (f *fakeBroadcaster) fanoutSnapshot() []fakeFanout {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeFanout(nil), f.fanouts...)
}

func (f *fakeBroadcaster) unicastSnapshot() []fakeUnicast {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeUnicast(nil), f.unicasts...)
}

func (f *fakeBroadcaster) resetUnicasts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unicasts = nil
}

func waitForBidFanouts(t *testing.T, fb *fakeBroadcaster, want int) []fakeFanout {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fanouts := fb.fanoutSnapshot()
		count := 0
		for _, f := range fanouts {
			if f.event.Type == itemdto.EventBidSuccess {
				count++
			}
		}
		if count >= want {
			return fanouts
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("expected at least %d bid_success fanouts, got %v", want, fb.fanoutSnapshot())
	return nil
}

func TestPlaceBidBroadcastsBidSuccess(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	svc.bidBroadcastDelay = time.Millisecond
	now := time.Date(2026, 6, 3, 16, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	endTime := now.Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(context.Background(), bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "k1", UserName: "Alice",
	})
	if err != nil {
		t.Fatalf("PlaceBid failed: %v", err)
	}
	found := false
	for _, f := range waitForBidFanouts(t, fb, 1) {
		if f.event.Type == itemdto.EventBidSuccess {
			payload, ok := f.event.Payload.(itemdto.BidSuccessPayload)
			if !ok {
				t.Fatalf("expected BidSuccessPayload, got %T", f.event.Payload)
			}
			if payload.ServerTimeUnixMS != now.UnixMilli() {
				t.Fatalf("expected server_time_unix_ms %d, got %d", now.UnixMilli(), payload.ServerTimeUnixMS)
			}
			if payload.EndTimeUnixMS != endTime.UnixMilli() {
				t.Fatalf("expected end_time_unix_ms %d, got %d", endTime.UnixMilli(), payload.EndTimeUnixMS)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected bid_success fanout, got: %v", fb.fanouts)
	}
}

func TestPlaceBidBroadcastsUserOutbid(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	user1 := &usermodel.User{ID: "user_1", Name: "Alice", Identity: usermodel.IdentityUser}
	user2 := &usermodel.User{ID: "user_2", Name: "Bob", Identity: usermodel.IdentityUser}

	_, _ = svc.PlaceBid(context.Background(), user1, itemID, itemdto.PlaceBidInput{Price: 100, IdempotencyKey: "k1", UserName: "Alice"})
	fb.resetUnicasts()

	_, err := svc.PlaceBid(context.Background(), user2, itemID, itemdto.PlaceBidInput{Price: 200, IdempotencyKey: "k2", UserName: "Bob"})
	if err != nil {
		t.Fatalf("second PlaceBid failed: %v", err)
	}

	found := false
	for _, u := range fb.unicastSnapshot() {
		if u.event.Type == itemdto.EventUserOutbid && u.addr == wsevent.UserAddr("user_1") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected user_outbid unicast to user_1, got: %v", fb.unicasts)
	}
}

func TestPlaceBidCoalescesBidSuccessFanout(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fb := &fakeBroadcaster{}
	svc := NewService(store, testPolicy, fc, nil, nil, fb)
	svc.bidBroadcastDelay = 10 * time.Millisecond
	now := time.Date(2026, 6, 3, 16, 5, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	endTime := now.Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	for i, price := range []int64{100, 200, 300} {
		user := &usermodel.User{ID: fmt.Sprintf("user_%d", i+1), Name: fmt.Sprintf("User%d", i+1), Identity: usermodel.IdentityUser}
		_, err := svc.PlaceBid(context.Background(), user, itemID, itemdto.PlaceBidInput{
			Price: price, IdempotencyKey: fmt.Sprintf("coalesce_%d", i), UserName: user.Name,
		})
		if err != nil {
			t.Fatalf("PlaceBid %d failed: %v", i, err)
		}
	}

	time.Sleep(50 * time.Millisecond)
	var bidFanouts []fakeFanout
	for _, f := range fb.fanoutSnapshot() {
		if f.event.Type == itemdto.EventBidSuccess {
			bidFanouts = append(bidFanouts, f)
		}
	}
	if len(bidFanouts) != 1 {
		t.Fatalf("expected coalesced bid_success fanouts = 1, got %d (%+v)", len(bidFanouts), bidFanouts)
	}
	payload, ok := bidFanouts[0].event.Payload.(itemdto.BidSuccessPayload)
	if !ok {
		t.Fatalf("expected BidSuccessPayload, got %T", bidFanouts[0].event.Payload)
	}
	if payload.CurrentPrice != 300 || payload.LeaderUserID != "user_3" {
		t.Fatalf("expected latest bid payload user_3/300, got %+v", payload)
	}
	if payload.ServerTimeUnixMS != now.UnixMilli() || payload.EndTimeUnixMS != endTime.UnixMilli() {
		t.Fatalf("expected clock fields server=%d end=%d, got %+v", now.UnixMilli(), endTime.UnixMilli(), payload)
	}
}
