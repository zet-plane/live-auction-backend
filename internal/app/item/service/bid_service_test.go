package service

import (
	"errors"
	"testing"
	"time"

	itemdto "github.com/zet-plane/live-auction-backend/internal/app/item/dto"
	itemmodel "github.com/zet-plane/live-auction-backend/internal/app/item/model"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
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
	result, err := svc.CreateItem(
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
	if err := svc.PublishItem(merchant, result.ItemID); err != nil {
		t.Fatalf("PublishItem failed: %v", err)
	}
	if err := svc.StartItem(merchant, result.ItemID); err != nil {
		t.Fatalf("StartItem failed: %v", err)
	}
	return result.ItemID
}

func TestPlaceBidSucceeds(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	result, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
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

func TestPlaceBidRejectsNonOngoingItem(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	itemID := seedPublishedItem(t, svc, "merchant_1", "room_1")

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 100, IdempotencyKey: "k1"})
	if !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected invalid request for non-ongoing item, got %v", err)
	}
}

func TestPlaceBidRejectsPriceTooLow(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	fc.bidLuaCode = 3
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 50, IdempotencyKey: "k1"})
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
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 150, IdempotencyKey: "k1"})
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
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	_, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 100, IdempotencyKey: "k1"})
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
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 0, endTime)

	if _, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
		Price: 100, IdempotencyKey: "idem_dup", UserName: "Alice",
	}); err != nil {
		t.Fatalf("first bid failed: %v", err)
	}
	// Force idempotency code on second call (fakeCache returns code=1, skips BidLog write)
	fc.bidLuaCode = 1
	if _, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{
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

func TestPlaceBidPriceCapEndsAuction(t *testing.T) {
	store := newFakeStore()
	fc := newFakeCache()
	svc := NewService(store, testPolicy, fc)
	endTime := time.Now().Add(5 * time.Minute)
	itemID := seedOngoingItem(t, svc, "merchant_1", "room_1", 0, 100, 500, endTime)

	result, err := svc.PlaceBid(bidder, itemID, itemdto.PlaceBidInput{Price: 500, IdempotencyKey: "idem_cap"})
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
}
