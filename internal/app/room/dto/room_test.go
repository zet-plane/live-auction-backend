package dto

import (
	"encoding/json"
	"testing"
)

func TestRoomDetailDTOIncludesEmptyCurrentItemID(t *testing.T) {
	body, err := json.Marshal(RoomDetailDTO{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := got["current_item_id"]; !ok {
		t.Fatalf("expected current_item_id key in JSON, got %s", body)
	}
}
