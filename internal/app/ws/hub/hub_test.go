package hub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

func TestRegisterSameUserRoomReplacesOldConnection(t *testing.T) {
	h := NewHub(nil)
	oldConn := newTestConn("user_1", "room_1")
	oldConn.id = "old_conn"
	newConn := newTestConn("user_1", "room_1")
	newConn.id = "new_conn"

	h.Register(oldConn)
	h.Register(newConn)

	h.mu.RLock()
	room := h.rooms["room_1"]
	_, oldStillIndexed := room["old_conn"]
	_, newIndexed := room["new_conn"]
	h.mu.RUnlock()
	if oldStillIndexed {
		t.Fatal("expected old same-user room connection to be replaced")
	}
	if !newIndexed {
		t.Fatal("expected new connection to be indexed")
	}
	if !oldConn.isClosed() {
		t.Fatal("expected old connection to be closed after replacement")
	}

	h.SendToRoom("room_1", wsevent.Event{Type: "room_event"})
	if len(newConn.send) != 1 {
		t.Fatalf("expected new connection to receive room event, got %d", len(newConn.send))
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

type fakePresenceStore struct {
	mu     sync.Mutex
	joins  []string
	leaves []string
}

func (s *fakePresenceStore) JoinRoom(_ context.Context, roomID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.joins = append(s.joins, roomID+"/"+userID)
	return nil
}

func (s *fakePresenceStore) LeaveRoom(_ context.Context, roomID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leaves = append(s.leaves, roomID+"/"+userID)
	return nil
}

func (s *fakePresenceStore) joinCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.joins)
}

func (s *fakePresenceStore) leaveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.leaves)
}

type fakeSocket struct {
	mu                sync.Mutex
	pingCh            chan struct{}
	pongHandler       func(string) error
	readDeadlineCalls int
	closeOnce         sync.Once
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{pingCh: make(chan struct{})}
}

func (s *fakeSocket) SetReadDeadline(time.Time) error {
	s.mu.Lock()
	s.readDeadlineCalls++
	s.mu.Unlock()
	return nil
}

func (s *fakeSocket) SetPongHandler(handler func(string) error) {
	s.mu.Lock()
	s.pongHandler = handler
	s.mu.Unlock()
}

func (s *fakeSocket) ReadMessage() (int, []byte, error) {
	return 0, nil, errors.New("fake socket closed")
}

func (s *fakeSocket) SetWriteDeadline(time.Time) error { return nil }

func (s *fakeSocket) WriteJSON(any) error { return nil }

func (s *fakeSocket) WriteControl(messageType int, _ []byte, _ time.Time) error {
	if messageType == websocket.PingMessage {
		s.closeOnce.Do(func() {
			close(s.pingCh)
		})
	}
	return nil
}

func (s *fakeSocket) Close() error { return nil }

func (s *fakeSocket) currentPongHandler() func(string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pongHandler
}

func (s *fakeSocket) readDeadlineCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readDeadlineCalls
}

func TestCloseConnRemovesPresenceOnce(t *testing.T) {
	h := NewHub(nil)
	presence := &fakePresenceStore{}
	h.presence = presence
	c := newTestConn("user_1", "room_1")
	h.Register(c)
	waitFor(t, func() bool { return presence.joinCount() == 1 })

	h.closeConn(c)
	h.closeConn(c)

	waitFor(t, func() bool { return presence.leaveCount() == 1 })
	time.Sleep(20 * time.Millisecond)
	if got := presence.leaveCount(); got != 1 {
		t.Fatalf("expected one presence leave after duplicate close, got %d", got)
	}
}

func TestClosingReplacedConnectionDoesNotRemoveNewPresence(t *testing.T) {
	h := NewHub(nil)
	presence := &fakePresenceStore{}
	h.presence = presence
	oldConn := newTestConn("user_1", "room_1")
	oldConn.id = "old_conn"
	newConn := newTestConn("user_1", "room_1")
	newConn.id = "new_conn"

	h.Register(oldConn)
	waitFor(t, func() bool { return presence.joinCount() == 1 })
	h.Register(newConn)
	waitFor(t, func() bool { return presence.joinCount() == 2 })

	h.closeConn(oldConn)
	time.Sleep(20 * time.Millisecond)
	if got := presence.leaveCount(); got != 0 {
		t.Fatalf("expected replaced old connection not to remove online presence, got %d leaves", got)
	}

	h.closeConn(newConn)
	waitFor(t, func() bool { return presence.leaveCount() == 1 })
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

func TestSendToRoomDeliversEvent(t *testing.T) {
	h := NewHub(nil)
	c1 := newTestConn("user_1", "room_1")
	c2 := newTestConn("user_2", "room_2")
	h.Register(c1)
	h.Register(c2)

	evt := wsevent.Event{Type: "direct_room_event"}
	h.SendToRoom("room_1", evt)

	if len(c1.send) != 1 {
		t.Fatalf("c1 should receive 1 event, got %d", len(c1.send))
	}
	if got := <-c1.send; got.Type != "direct_room_event" {
		t.Fatalf("expected direct_room_event, got %q", got.Type)
	}
	if len(c2.send) != 0 {
		t.Fatalf("c2 (different room) should receive 0 events, got %d", len(c2.send))
	}
}

type fakeSnapshotProvider struct {
	event wsevent.Event
	ok    bool
	err   error
	calls []string
}

func (p *fakeSnapshotProvider) SnapshotForRoom(_ context.Context, roomID string) (*wsevent.Event, bool, error) {
	p.calls = append(p.calls, roomID)
	if p.err != nil || !p.ok {
		return nil, p.ok, p.err
	}
	return &p.event, true, nil
}

func TestRegisterDeliversSnapshotWhenProviderHasOne(t *testing.T) {
	h := NewHub(nil)
	provider := &fakeSnapshotProvider{
		event: wsevent.Event{Type: "auction_snapshot"},
		ok:    true,
	}
	h.SetSnapshotProvider(provider)

	c := newTestConn("user_1", "room_1")
	h.Register(c)

	if len(provider.calls) != 1 || provider.calls[0] != "room_1" {
		t.Fatalf("expected snapshot provider called for room_1, got %v", provider.calls)
	}
	if len(c.send) != 1 {
		t.Fatalf("expected snapshot delivered to new conn, got %d events", len(c.send))
	}
	if got := <-c.send; got.Type != "auction_snapshot" {
		t.Fatalf("expected auction_snapshot, got %q", got.Type)
	}
}

func TestStartWriteLoopSendsServerControlPing(t *testing.T) {
	oldPingInterval := pingInterval
	pingInterval = 10 * time.Millisecond
	t.Cleanup(func() { pingInterval = oldPingInterval })

	h := NewHub(nil)
	ws := newFakeSocket()
	conn := NewConn("conn_1", "user_1", "room_1", ws, h)
	h.Register(conn)
	go conn.StartWriteLoop()
	defer conn.close()

	select {
	case <-ws.pingCh:
	case <-time.After(time.Second):
		t.Fatal("expected server to send websocket control ping")
	}
}

func TestStartReadLoopConfiguresPongDeadlineRefresh(t *testing.T) {
	h := NewHub(nil)
	ws := newFakeSocket()
	conn := NewConn("conn_1", "user_1", "room_1", ws, h)
	h.Register(conn)

	conn.StartReadLoop()

	handler := ws.currentPongHandler()
	if handler == nil {
		t.Fatal("expected pong handler to be configured")
	}
	before := ws.readDeadlineCount()
	if err := handler(""); err != nil {
		t.Fatalf("pong handler returned error: %v", err)
	}
	if got := ws.readDeadlineCount(); got != before+1 {
		t.Fatalf("expected pong handler to refresh read deadline, before=%d after=%d", before, got)
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

func TestCloseConnIsIdempotentAndClosedConnDeliveryDoesNotPanic(t *testing.T) {
	h := NewHub(nil)
	c := newTestConn("user_1", "room_1")
	h.Register(c)

	h.closeConn(c)
	assertNotPanics(t, func() {
		h.closeConn(c)
	})

	h.mu.Lock()
	h.rooms["room_1"] = map[string]*Conn{c.id: c}
	h.users["user_1"] = []*Conn{c}
	h.mu.Unlock()

	assertNotPanics(t, func() {
		h.SendToRoom("room_1", wsevent.Event{Type: "after_close_room"})
	})
	assertNotPanics(t, func() {
		_ = h.Unicast(wsevent.UserAddr("user_1"), wsevent.Event{Type: "after_close_user"})
	})

	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, ok := h.rooms["room_1"][c.id]; ok {
		t.Fatal("expected closed conn removed from room index")
	}
	if len(h.users["user_1"]) != 0 {
		t.Fatalf("expected closed conn removed from user index, got %d", len(h.users["user_1"]))
	}
}

func TestConcurrentSendAndCloseDoesNotPanic(t *testing.T) {
	h := NewHub(nil)
	c := newTestConn("user_1", "room_1")
	h.Register(c)

	var wg sync.WaitGroup
	panicCh := make(chan any, 64)
	run := func(fn func()) {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicCh <- r
			}
		}()
		fn()
	}

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go run(func() {
			for j := 0; j < 100; j++ {
				h.SendToRoom("room_1", wsevent.Event{Type: "room_event"})
				_ = h.Unicast(wsevent.UserAddr("user_1"), wsevent.Event{Type: "user_event"})
			}
		})
	}
	wg.Add(1)
	go run(func() {
		for i := 0; i < 100; i++ {
			h.closeConn(c)
		}
	})

	wg.Wait()
	close(panicCh)
	for p := range panicCh {
		t.Fatalf("unexpected panic during concurrent send/close: %v", p)
	}
}

func assertNotPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
