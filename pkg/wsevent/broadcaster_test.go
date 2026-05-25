package wsevent_test

import (
	"testing"

	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

func TestRoomTopic(t *testing.T) {
	if got := wsevent.RoomTopic("room_123"); got != "room:room_123" {
		t.Errorf("RoomTopic = %q, want %q", got, "room:room_123")
	}
}

func TestUserAddr(t *testing.T) {
	if got := wsevent.UserAddr("user_456"); got != "user:user_456" {
		t.Errorf("UserAddr = %q, want %q", got, "user:user_456")
	}
}

func TestEventJSON(t *testing.T) {
	// 确认 Event.Payload 为 any，可接受任意 struct
	evt := wsevent.Event{Type: "ping", Payload: map[string]string{"k": "v"}}
	if evt.Type != "ping" {
		t.Errorf("unexpected type %q", evt.Type)
	}
}
