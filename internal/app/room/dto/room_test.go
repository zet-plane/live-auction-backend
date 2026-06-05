package dto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/pkg/errorx"
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

func TestRoomFeedCursorRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 6, 5, 10, 30, 45, 123456000, time.UTC)
	cursor := RoomFeedCursor{CreatedAt: createdAt, ID: "room_abc"}

	encoded, err := EncodeRoomFeedCursor(cursor)
	if err != nil {
		t.Fatalf("EncodeRoomFeedCursor: %v", err)
	}
	if encoded == "" {
		t.Fatal("expected non-empty cursor")
	}
	if strings.Contains(encoded, "=") {
		t.Fatalf("expected raw base64 cursor without padding, got %q", encoded)
	}
	if _, err := base64.RawURLEncoding.DecodeString(encoded); err != nil {
		t.Fatalf("expected cursor to decode with RawURLEncoding: %v", err)
	}

	decoded, err := DecodeRoomFeedCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeRoomFeedCursor: %v", err)
	}
	if !decoded.CreatedAt.Equal(createdAt) || decoded.ID != cursor.ID {
		t.Fatalf("decoded cursor mismatch: %+v", decoded)
	}
}

func TestDecodeRoomFeedCursorRejectsInvalidValue(t *testing.T) {
	if _, err := DecodeRoomFeedCursor("not-base64"); !errors.Is(err, errorx.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestDecodeRoomFeedCursorRejectsMissingFields(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "omitted id",
			raw:  `{"created_at":"2026-06-05T10:30:45Z"}`,
		},
		{
			name: "omitted created_at",
			raw:  `{"id":"room_abc"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := base64.RawURLEncoding.EncodeToString([]byte(tt.raw))

			if _, err := DecodeRoomFeedCursor(encoded); !errors.Is(err, errorx.ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
		})
	}
}

func TestNormalizeRoomFeedInput(t *testing.T) {
	input := NormalizeRoomFeedInput(RoomFeedInput{Limit: 0})
	if input.Limit != RoomFeedDefaultLimit {
		t.Fatalf("expected default limit %d, got %d", RoomFeedDefaultLimit, input.Limit)
	}

	input = NormalizeRoomFeedInput(RoomFeedInput{Limit: RoomFeedMaxLimit + 1})
	if input.Limit != RoomFeedMaxLimit {
		t.Fatalf("expected max limit %d, got %d", RoomFeedMaxLimit, input.Limit)
	}

	input = NormalizeRoomFeedInput(RoomFeedInput{Cursor: "  abc  ", Limit: 3})
	if input.Cursor != "abc" || input.Limit != 3 {
		t.Fatalf("expected trimmed cursor and preserved limit, got %+v", input)
	}
}
