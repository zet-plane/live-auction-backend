package cache

import "testing"

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
