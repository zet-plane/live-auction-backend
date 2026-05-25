package hub

import (
	"testing"

	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

func newTestConn(userID, roomID string) *Conn {
	return &Conn{
		id:     "conn_" + userID,
		userID: userID,
		roomID: roomID,
		send:   make(chan wsevent.Event, 8),
	}
}

func TestRegisterAddsToIndexes(t *testing.T) {
	h := NewHub(nil)
	c := newTestConn("user_1", "room_1")
	h.Register(c)

	h.mu.RLock()
	defer h.mu.RUnlock()

	if _, ok := h.rooms["room_1"]["conn_user_1"]; !ok {
		t.Error("expected conn in rooms index")
	}
	found := false
	for _, uc := range h.users["user_1"] {
		if uc.id == "conn_user_1" {
			found = true
		}
	}
	if !found {
		t.Error("expected conn in users index")
	}
}

func TestRemoveCleansIndexes(t *testing.T) {
	h := NewHub(nil)
	c := newTestConn("user_1", "room_1")
	h.Register(c)
	h.Remove(c)

	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.rooms["room_1"]) != 0 {
		t.Error("expected rooms index cleared")
	}
	if len(h.users["user_1"]) != 0 {
		t.Error("expected users index cleared")
	}
}

func TestFanoutDeliversToRoom(t *testing.T) {
	h := NewHub(nil)
	c1 := newTestConn("user_1", "room_1")
	c2 := newTestConn("user_2", "room_1")
	c3 := newTestConn("user_3", "room_2") // 不同房间
	h.Register(c1)
	h.Register(c2)
	h.Register(c3)

	evt := wsevent.Event{Type: "test_event"}
	if err := h.Fanout(wsevent.RoomTopic("room_1"), evt); err != nil {
		t.Fatalf("Fanout error: %v", err)
	}

	if len(c1.send) != 1 {
		t.Errorf("c1 should receive 1 event, got %d", len(c1.send))
	}
	if len(c2.send) != 1 {
		t.Errorf("c2 should receive 1 event, got %d", len(c2.send))
	}
	if len(c3.send) != 0 {
		t.Errorf("c3 (different room) should receive 0 events, got %d", len(c3.send))
	}
}

func TestUnicastDeliversToUser(t *testing.T) {
	h := NewHub(nil)
	c1 := newTestConn("user_1", "room_1")
	c2 := newTestConn("user_2", "room_1")
	h.Register(c1)
	h.Register(c2)

	evt := wsevent.Event{Type: "user_outbid"}
	if err := h.Unicast(wsevent.UserAddr("user_1"), evt); err != nil {
		t.Fatalf("Unicast error: %v", err)
	}

	if len(c1.send) != 1 {
		t.Errorf("c1 should receive 1 event, got %d", len(c1.send))
	}
	if len(c2.send) != 0 {
		t.Errorf("c2 should not receive event, got %d", len(c2.send))
	}
}

func TestSlowConnectionIsClosedOnFullChannel(t *testing.T) {
	h := NewHub(nil)
	// send channel 容量为 1，填满后下次 Fanout 应触发 closeConn
	c := &Conn{
		id:     "slow_conn",
		userID: "user_slow",
		roomID: "room_1",
		send:   make(chan wsevent.Event, 1), // 容量1
	}
	h.Register(c)
	c.send <- wsevent.Event{Type: "fill"} // 填满

	h.Fanout(wsevent.RoomTopic("room_1"), wsevent.Event{Type: "overflow"})

	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, ok := h.rooms["room_1"]["slow_conn"]; ok {
		t.Error("slow connection should have been removed from rooms index")
	}
}
