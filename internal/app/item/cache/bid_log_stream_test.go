package cache

import (
	"errors"
	"reflect"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestNewBidLogXAddArgsUsesApproximateMaxLen(t *testing.T) {
	args := newBidLogXAddArgs(BidLogEvent{
		BidID:           "bid_1",
		ItemID:          "item_1",
		RoomID:          "room_1",
		UserID:          "user_1",
		Price:           1200,
		CreatedAtUnixMS: 1780560000123,
	})

	if args.Stream != BidLogStreamName {
		t.Fatalf("expected stream %q, got %q", BidLogStreamName, args.Stream)
	}
	if args.MaxLen != BidLogStreamMaxLen || !args.Approx {
		t.Fatalf("expected approximate maxlen %d, got maxlen=%d approx=%v", BidLogStreamMaxLen, args.MaxLen, args.Approx)
	}
}

func TestBidLogEventFromStreamValuesParsesEvent(t *testing.T) {
	event, err := bidLogEventFromStreamValues(map[string]any{
		"bid_id":             "bid_1",
		"item_id":            "item_1",
		"room_id":            "room_1",
		"user_id":            "user_1",
		"price":              "1200",
		"created_at_unix_ms": "1780560000123",
	})
	if err != nil {
		t.Fatalf("bidLogEventFromStreamValues returned error: %v", err)
	}
	if event.BidID != "bid_1" || event.ItemID != "item_1" || event.RoomID != "room_1" || event.UserID != "user_1" {
		t.Fatalf("string fields not copied: %+v", event)
	}
	if event.Price != 1200 || event.CreatedAtUnixMS != 1780560000123 {
		t.Fatalf("numeric fields not parsed: %+v", event)
	}
}

func TestBidLogEventFromStreamValuesRejectsInvalidNumericField(t *testing.T) {
	_, err := bidLogEventFromStreamValues(map[string]any{
		"bid_id":             "bid_1",
		"item_id":            "item_1",
		"room_id":            "room_1",
		"user_id":            "user_1",
		"price":              "not-a-number",
		"created_at_unix_ms": "1780560000123",
	})
	if err == nil {
		t.Fatal("expected invalid numeric field error")
	}
}

func TestBidLogEventFromStreamValuesRejectsMissingNumericField(t *testing.T) {
	_, err := bidLogEventFromStreamValues(map[string]any{
		"bid_id":             "bid_1",
		"item_id":            "item_1",
		"room_id":            "room_1",
		"user_id":            "user_1",
		"created_at_unix_ms": "1780560000123",
	})
	if err == nil {
		t.Fatal("expected missing numeric field error")
	}
}

func TestParseBidLogStreamMessagesDeadLettersMalformedAndReturnsValid(t *testing.T) {
	var deadLettered []string
	messages, err := parseBidLogStreamMessages([]redis.XMessage{
		{
			ID: "stream-1",
			Values: map[string]any{
				"bid_id":             "bid_1",
				"item_id":            "item_1",
				"room_id":            "room_1",
				"user_id":            "user_1",
				"price":              "1200",
				"created_at_unix_ms": "1780560000123",
			},
		},
		{
			ID: "stream-bad",
			Values: map[string]any{
				"bid_id":             "bid_bad",
				"item_id":            "item_1",
				"room_id":            "room_1",
				"user_id":            "user_bad",
				"price":              "bad-price",
				"created_at_unix_ms": "1780560000123",
			},
		},
		{
			ID: "stream-2",
			Values: map[string]any{
				"bid_id":             "bid_2",
				"item_id":            "item_1",
				"room_id":            "room_1",
				"user_id":            "user_2",
				"price":              "1400",
				"created_at_unix_ms": "1780560001123",
			},
		},
	}, func(message redis.XMessage, _ error) error {
		deadLettered = append(deadLettered, message.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("parseBidLogStreamMessages returned error: %v", err)
	}
	if !reflect.DeepEqual(deadLettered, []string{"stream-bad"}) {
		t.Fatalf("expected malformed message to be dead-lettered, got %#v", deadLettered)
	}
	if len(messages) != 2 || messages[0].ID != "stream-1" || messages[1].ID != "stream-2" {
		t.Fatalf("expected valid messages to be returned, got %#v", messages)
	}
}

func TestParseBidLogStreamMessagesReturnsDeadLetterError(t *testing.T) {
	deadLetterErr := errors.New("dead letter unavailable")
	_, err := parseBidLogStreamMessages([]redis.XMessage{
		{
			ID: "stream-bad",
			Values: map[string]any{
				"bid_id":             "bid_bad",
				"item_id":            "item_1",
				"room_id":            "room_1",
				"user_id":            "user_bad",
				"price":              "bad-price",
				"created_at_unix_ms": "1780560000123",
			},
		},
	}, func(redis.XMessage, error) error {
		return deadLetterErr
	})
	if !errors.Is(err, deadLetterErr) {
		t.Fatalf("expected dead-letter error, got %v", err)
	}
}
