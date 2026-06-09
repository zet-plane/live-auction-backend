package hub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

func newTestConn(userID, roomID string) *Conn {
	return &Conn{
		id:     "conn_" + userID,
		userID: userID,
		roomID: roomID,
		stream: streamAll,
		high:   make(chan wsevent.Event, 8),
		send:   make(chan wsevent.Event, 8),

		timeSyncNotify: make(chan struct{}, 1),
	}
}

func TestClassifyEventLane(t *testing.T) {
	tests := []struct {
		eventType string
		want      eventLane
	}{
		{eventType: "time_sync", want: laneLatest},
		{eventType: "user_outbid", want: laneHigh},
		{eventType: "auction_extended", want: laneHigh},
		{eventType: "auction_ended", want: laneHigh},
		{eventType: "auction_cancelled", want: laneHigh},
		{eventType: "order_created", want: laneHigh},
		{eventType: "auction_started", want: laneHigh},
		{eventType: "auction_snapshot", want: laneHigh},
		{eventType: "bid_success", want: laneNormal},
		{eventType: "unknown_event", want: laneNormal},
	}

	for _, tt := range tests {
		if got := classifyEventLane(tt.eventType); got != tt.want {
			t.Fatalf("classifyEventLane(%q) = %q, want %q", tt.eventType, got, tt.want)
		}
	}
}

func TestParseConnStream(t *testing.T) {
	tests := []struct {
		raw  string
		want connStream
	}{
		{"", streamAll},
		{"all", streamAll},
		{"control", streamControl},
		{"market", streamMarket},
		{"user", streamControl},
		{"bad", streamAll},
	}
	for _, tt := range tests {
		if got := parseConnStream(tt.raw); got != tt.want {
			t.Fatalf("parseConnStream(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestClassifyEventStream(t *testing.T) {
	tests := []struct {
		eventType string
		want      connStream
	}{
		{"time_sync", streamControl},
		{"auction_snapshot", streamControl},
		{"auction_started", streamControl},
		{"auction_extended", streamControl},
		{"auction_ended", streamControl},
		{"auction_cancelled", streamControl},
		{"bid_success", streamMarket},
		{"user_outbid", streamControl},
		{"order_created", streamControl},
		{"unknown_event", streamMarket},
	}
	for _, tt := range tests {
		if got := classifyEventStream(tt.eventType); got != tt.want {
			t.Fatalf("classifyEventStream(%q) = %q, want %q", tt.eventType, got, tt.want)
		}
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

func TestRegisterAllowsSameUserSameRoomDifferentStreams(t *testing.T) {
	h := NewHub(nil)
	control := NewConnWithStream("conn_control", "user_1", "room_1", newFakeSocket(), h, streamControl)
	market := NewConnWithStream("conn_market", "user_1", "room_1", newFakeSocket(), h, streamMarket)

	h.Register(control)
	h.Register(market)

	h.mu.RLock()
	defer h.mu.RUnlock()
	if got := len(h.rooms["room_1"]); got != 2 {
		t.Fatalf("room connection count = %d, want 2", got)
	}
	if got := len(h.users["user_1"]); got != 2 {
		t.Fatalf("user connection count = %d, want 2", got)
	}
}

func TestRegisterReplacesSameUserSameRoomSameStream(t *testing.T) {
	h := NewHub(nil)
	first := NewConnWithStream("conn_1", "user_1", "room_1", newFakeSocket(), h, streamMarket)
	second := NewConnWithStream("conn_2", "user_1", "room_1", newFakeSocket(), h, streamMarket)

	h.Register(first)
	h.Register(second)

	h.mu.RLock()
	defer h.mu.RUnlock()
	if got := len(h.rooms["room_1"]); got != 1 {
		t.Fatalf("room connection count = %d, want 1", got)
	}
	if _, ok := h.rooms["room_1"]["conn_2"]; !ok {
		t.Fatalf("replacement connection not registered")
	}
	if !first.isClosed() {
		t.Fatalf("expected first same-stream connection to be closed after replacement")
	}
}

func TestRegisterRecordsStreamLifecycleMetric(t *testing.T) {
	rec := &captureWSRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

	h := NewHub(nil)
	conn := NewConnWithStream("conn_control", "user_1", "room_1", newFakeSocket(), h, streamControl)

	h.Register(conn)
	h.Remove(conn)

	if got := rec.lifecycleCount("control", "accepted", ""); got != 1 {
		t.Fatalf("accepted lifecycle count = %d, want 1", got)
	}
	if got := rec.lifecycleCount("control", "closed", "normal"); got != 1 {
		t.Fatalf("closed lifecycle count = %d, want 1", got)
	}
}

func TestRegisterRecordsReplacedStreamLifecycleMetric(t *testing.T) {
	rec := &captureWSRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

	h := NewHub(nil)
	first := NewConnWithStream("conn_1", "user_1", "room_1", newFakeSocket(), h, streamMarket)
	second := NewConnWithStream("conn_2", "user_1", "room_1", newFakeSocket(), h, streamMarket)

	h.Register(first)
	h.Register(second)

	if got := rec.lifecycleCount("market", "closed", "replaced"); got != 1 {
		t.Fatalf("replaced lifecycle count = %d, want 1", got)
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

func TestRemoveLeavesPresenceAfterLastSameUserRoomStreamConnection(t *testing.T) {
	h := NewHub(nil)
	presence := &fakePresenceStore{leaveCh: make(chan struct{}, 2)}
	h.presence = presence
	control := NewConnWithStream("conn_control", "user_1", "room_1", newFakeSocket(), h, streamControl)
	market := NewConnWithStream("conn_market", "user_1", "room_1", newFakeSocket(), h, streamMarket)

	h.Register(control)
	h.Register(market)
	waitFor(t, func() bool { return presence.joinCount() == 2 })

	h.Remove(control)
	select {
	case <-presence.leaveCh:
		t.Fatalf("presence leave should wait until the last same-user room stream closes")
	case <-time.After(30 * time.Millisecond):
	}

	h.Remove(market)
	waitFor(t, func() bool { return presence.leaveCount() == 1 })
	if got := presence.leaveCount(); got != 1 {
		t.Fatalf("presence leave count after last removal = %d, want 1", got)
	}
}

type fakePresenceStore struct {
	mu       sync.Mutex
	joins    []string
	leaves   []string
	leaveCh  chan struct{}
	joinErr  error
	leaveErr error
}

func (s *fakePresenceStore) JoinRoom(_ context.Context, roomID, userID string) error {
	if s.joinErr != nil {
		return s.joinErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.joins = append(s.joins, roomID+"/"+userID)
	return nil
}

func (s *fakePresenceStore) LeaveRoom(_ context.Context, roomID, userID string) error {
	if s.leaveErr != nil {
		return s.leaveErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leaves = append(s.leaves, roomID+"/"+userID)
	if s.leaveCh != nil {
		select {
		case s.leaveCh <- struct{}{}:
		default:
		}
	}
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
	writeJSONErr      error
	writeJSONCh       chan struct{}
	writes            []wsevent.Event
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

func (s *fakeSocket) WriteJSON(v any) error {
	s.mu.Lock()
	err := s.writeJSONErr
	ch := s.writeJSONCh
	if event, ok := v.(wsevent.Event); ok {
		s.writes = append(s.writes, event)
	}
	s.mu.Unlock()
	if ch != nil {
		close(ch)
	}
	return err
}

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

func (s *fakeSocket) writtenEvents() []wsevent.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]wsevent.Event(nil), s.writes...)
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

func TestPresenceFailureMarksDegraded(t *testing.T) {
	SetPresenceStatusForTest("ok")
	t.Cleanup(func() { SetPresenceStatusForTest("ok") })

	h := NewHub(nil)
	presence := &fakePresenceStore{joinErr: errors.New("boom")}
	h.presence = presence
	c := newTestConn("user_1", "room_1")

	h.Register(c)
	waitFor(t, func() bool { return PresenceStatus() == "degraded" })
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

func TestFanoutDeliversOnlyToMatchingRoomStreams(t *testing.T) {
	h := NewHub(nil)
	controlWS := newFakeSocket()
	marketWS := newFakeSocket()
	allWS := newFakeSocket()
	control := NewConnWithStream("conn_control", "user_1", "room_1", controlWS, h, streamControl)
	market := NewConnWithStream("conn_market", "user_2", "room_1", marketWS, h, streamMarket)
	all := NewConnWithStream("conn_all", "user_3", "room_1", allWS, h, streamAll)
	h.Register(control)
	h.Register(market)
	h.Register(all)

	h.SendToRoom("room_1", wsevent.Event{Type: "time_sync"})

	if len(controlWS.writtenEvents()) != 0 || len(marketWS.writtenEvents()) != 0 || len(allWS.writtenEvents()) != 0 {
		t.Fatalf("events should be queued but not written until write loop starts")
	}
	if len(control.high) != 0 {
		t.Fatalf("time_sync must not enter high queue")
	}
	if market.latestTimeSync != nil {
		t.Fatalf("market stream should not receive control time_sync")
	}
	if all.latestTimeSync == nil {
		t.Fatalf("all stream should receive time_sync for backward compatibility")
	}
	if control.latestTimeSync == nil {
		t.Fatalf("control stream should receive time_sync")
	}
}

func TestFanoutDeliversMarketEventsOnlyToMatchingRoomStreams(t *testing.T) {
	h := NewHub(nil)
	control := NewConnWithStream("conn_control", "user_1", "room_1", newFakeSocket(), h, streamControl)
	market := NewConnWithStream("conn_market", "user_2", "room_1", newFakeSocket(), h, streamMarket)
	all := NewConnWithStream("conn_all", "user_3", "room_1", newFakeSocket(), h, streamAll)
	h.Register(control)
	h.Register(market)
	h.Register(all)

	h.SendToRoom("room_1", wsevent.Event{Type: "bid_success"})

	if len(control.send) != 0 {
		t.Fatalf("control stream should not receive market bid_success")
	}
	if len(market.send) != 1 {
		t.Fatalf("market stream should receive bid_success, got %d events", len(market.send))
	}
	if len(all.send) != 1 {
		t.Fatalf("all stream should receive bid_success for backward compatibility, got %d events", len(all.send))
	}
}

func TestFanoutRecordsBroadcastAndDeliveryMetrics(t *testing.T) {
	rec := &captureWSRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

	h := NewHub(nil)
	c1 := newTestConn("user_1", "room_1")
	c2 := newTestConn("user_2", "room_1")
	h.Register(c1)
	h.Register(c2)

	if err := h.Fanout(wsevent.RoomTopic("room_1"), wsevent.Event{Type: "bid_success"}); err != nil {
		t.Fatalf("Fanout error: %v", err)
	}

	if got := rec.connectionDelta(); got != 2 {
		t.Fatalf("connection delta = %d, want 2", got)
	}
	broadcast := rec.lastBroadcast()
	if broadcast.Mode != "fanout" || broadcast.Result != "success" || broadcast.Recipients != 2 {
		t.Fatalf("broadcast metric = %+v", broadcast)
	}
	if broadcast.EventType != "bid_success" {
		t.Fatalf("broadcast event type = %q, want bid_success", broadcast.EventType)
	}
	if got := rec.deliveryCount("success"); got != 2 {
		t.Fatalf("success deliveries = %d, want 2", got)
	}
	delivery := rec.lastDelivery()
	if delivery.EventType != "bid_success" || delivery.QueueCap != 8 || delivery.QueueLen != 1 {
		t.Fatalf("delivery metric = %+v", delivery)
	}
}

func TestFullChannelRecordsDroppedDeliveryMetric(t *testing.T) {
	rec := &captureWSRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

	h := NewHub(nil)
	c := &Conn{
		id:     "slow_conn",
		userID: "user_slow",
		roomID: "room_1",
		stream: streamAll,
		send:   make(chan wsevent.Event, 1),
	}
	h.Register(c)
	c.send <- wsevent.Event{Type: "fill"}

	h.SendToRoom("room_1", wsevent.Event{Type: "overflow"})

	if got := rec.deliveryCount("dropped"); got != 1 {
		t.Fatalf("dropped deliveries = %d, want 1", got)
	}
	delivery := rec.lastDelivery()
	if delivery.EventType != "overflow" || delivery.Reason != "send_queue_full" || delivery.QueueLen != 1 || delivery.QueueCap != 1 {
		t.Fatalf("dropped delivery metric = %+v", delivery)
	}
	broadcast := rec.lastBroadcast()
	if broadcast.Mode != "fanout" || broadcast.Result != "dropped" || broadcast.Recipients != 0 {
		t.Fatalf("broadcast metric = %+v", broadcast)
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
	if len(c.high) != 1 {
		t.Fatalf("expected snapshot delivered to high queue, got %d events", len(c.high))
	}
	if got := <-c.high; got.Type != "auction_snapshot" {
		t.Fatalf("expected auction_snapshot, got %q", got.Type)
	}
}

func TestRegisterDeliversSnapshotOnlyToMatchingStreams(t *testing.T) {
	h := NewHub(nil)
	provider := &fakeSnapshotProvider{
		event: wsevent.Event{Type: "auction_snapshot"},
		ok:    true,
	}
	h.SetSnapshotProvider(provider)

	market := NewConnWithStream("conn_market", "user_1", "room_1", newFakeSocket(), h, streamMarket)
	control := NewConnWithStream("conn_control", "user_2", "room_1", newFakeSocket(), h, streamControl)
	all := NewConnWithStream("conn_all", "user_3", "room_1", newFakeSocket(), h, streamAll)
	h.Register(market)
	h.Register(control)
	h.Register(all)

	if len(provider.calls) != 2 {
		t.Fatalf("snapshot provider should skip market stream and run for control/all only, got calls %v", provider.calls)
	}
	if len(market.high) != 0 || len(market.send) != 0 || market.latestTimeSync != nil {
		t.Fatalf("market stream should not receive control auction_snapshot")
	}
	if len(control.high) != 1 {
		t.Fatalf("control stream should receive auction_snapshot, got %d events", len(control.high))
	}
	if len(all.high) != 1 {
		t.Fatalf("all stream should receive auction_snapshot for backward compatibility, got %d events", len(all.high))
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

func TestStartWriteLoopRecordsSocketWriteMetrics(t *testing.T) {
	rec := &captureWSRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

	h := NewHub(nil)
	ws := newFakeSocket()
	ws.writeJSONErr = errors.New("i/o timeout")
	ws.writeJSONCh = make(chan struct{})
	conn := NewConn("conn_1", "user_1", "room_1", ws, h)
	h.Register(conn)
	go conn.StartWriteLoop()

	if !conn.enqueue(wsevent.Event{Type: "bid_success"}) {
		t.Fatal("expected enqueue to succeed")
	}

	select {
	case <-ws.writeJSONCh:
	case <-time.After(time.Second):
		t.Fatal("expected write loop to call WriteJSON")
	}
	waitFor(t, func() bool { return rec.writeCount("failed") == 1 && rec.closeReasonCount("write_json_timeout") == 1 })

	write := rec.lastWrite()
	if write.EventType != "bid_success" || write.Reason != "timeout" || write.QueueCap != sendBufSize {
		t.Fatalf("write metric = %+v", write)
	}
}

func TestHighPriorityEventWritesBeforeNormalQueue(t *testing.T) {
	h := NewHub(nil)
	ws := newFakeSocket()
	conn := NewConn("conn_1", "user_1", "room_1", ws, h)
	h.Register(conn)

	for i := 0; i < 8; i++ {
		h.SendToRoom("room_1", wsevent.Event{Type: "bid_success", Payload: map[string]any{"seq": i}})
	}
	h.SendToRoom("room_1", wsevent.Event{Type: "auction_ended", Payload: map[string]any{"seq": 99}})

	go conn.StartWriteLoop()
	t.Cleanup(conn.close)

	waitFor(t, func() bool { return len(ws.writtenEvents()) >= 1 })
	first := ws.writtenEvents()[0]
	if first.Type != "auction_ended" {
		t.Fatalf("expected high priority event first, got %+v", first)
	}
}

func TestTimeSyncDeliveryKeepsOnlyLatestEvent(t *testing.T) {
	h := NewHub(nil)
	ws := newFakeSocket()
	conn := NewConn("conn_1", "user_1", "room_1", ws, h)
	h.Register(conn)

	h.SendToRoom("room_1", wsevent.Event{Type: "time_sync", Payload: map[string]any{"seq": 1}})
	h.SendToRoom("room_1", wsevent.Event{Type: "time_sync", Payload: map[string]any{"seq": 2}})

	go conn.StartWriteLoop()
	t.Cleanup(conn.close)

	waitFor(t, func() bool { return len(ws.writtenEvents()) >= 1 })
	first := ws.writtenEvents()[0]
	if first.Type != "time_sync" {
		t.Fatalf("expected time_sync first, got %+v", first)
	}
	payload := first.Payload.(map[string]any)
	if payload["seq"] != 2 {
		t.Fatalf("expected latest time_sync seq=2, got %#v", payload)
	}
}

func TestTimeSyncOverwriteAndWriteMetrics(t *testing.T) {
	rec := &captureWSRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

	h := NewHub(nil)
	ws := newFakeSocket()
	conn := NewConn("conn_1", "user_1", "room_1", ws, h)
	h.Register(conn)

	h.SendToRoom("room_1", wsevent.Event{Type: "time_sync", Payload: map[string]any{"seq": 1}})
	h.SendToRoom("room_1", wsevent.Event{Type: "time_sync", Payload: map[string]any{"seq": 2}})

	if got := rec.timeSyncCount("overwrite", "success"); got != 1 {
		t.Fatalf("expected one overwrite metric, got %d", got)
	}

	go conn.StartWriteLoop()
	t.Cleanup(conn.close)

	waitFor(t, func() bool { return rec.timeSyncCount("write", "success") == 1 })
	metric := rec.lastTimeSync()
	if metric.WriteLag <= 0 {
		t.Fatalf("expected positive time_sync write lag, got %s", metric.WriteLag)
	}
}

func TestHighPriorityEventWritesBeforeTimeSync(t *testing.T) {
	h := NewHub(nil)
	ws := newFakeSocket()
	conn := NewConn("conn_1", "user_1", "room_1", ws, h)
	h.Register(conn)

	h.SendToRoom("room_1", wsevent.Event{Type: "time_sync", Payload: map[string]any{"seq": 1}})
	h.SendToRoom("room_1", wsevent.Event{Type: "auction_cancelled", Payload: map[string]any{"seq": 2}})

	go conn.StartWriteLoop()
	t.Cleanup(conn.close)

	waitFor(t, func() bool { return len(ws.writtenEvents()) >= 1 })
	first := ws.writtenEvents()[0]
	if first.Type != "auction_cancelled" {
		t.Fatalf("expected high priority event before time_sync, got %+v", first)
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

	if len(c1.high) != 1 {
		t.Errorf("c1 should receive 1 high priority event, got %d", len(c1.high))
	}
	if len(c2.send) != 0 {
		t.Errorf("c2 should not receive event, got %d", len(c2.send))
	}
}

func TestSendToUserDeliversToLocalUserConnections(t *testing.T) {
	h := NewHub(nil)
	targetRoomConn := newTestConn("user_1", "room_1")
	otherRoomConn := newTestConn("user_1", "room_2")
	otherUserConn := newTestConn("user_2", "room_1")

	h.Register(targetRoomConn)
	h.Register(otherRoomConn)
	h.Register(otherUserConn)

	h.SendToUser("user_1", wsevent.Event{Type: "order_created"})

	if got := len(targetRoomConn.high); got != 1 {
		t.Fatalf("target room user high queue len = %d, want 1", got)
	}
	if got := len(otherRoomConn.high); got != 1 {
		t.Fatalf("other room same user high queue len = %d, want 1", got)
	}
	if got := len(otherUserConn.high); got != 0 {
		t.Fatalf("other user high queue len = %d, want 0", got)
	}
}

func TestUnicastDeliversOnlyToMatchingUserStreams(t *testing.T) {
	h := NewHub(nil)
	control := NewConnWithStream("conn_control", "user_1", "room_1", newFakeSocket(), h, streamControl)
	market := NewConnWithStream("conn_market", "user_1", "room_1", newFakeSocket(), h, streamMarket)
	all := NewConnWithStream("conn_all", "user_1", "room_1", newFakeSocket(), h, streamAll)
	h.Register(control)
	h.Register(market)
	h.Register(all)

	if err := h.Unicast(wsevent.UserAddr("user_1"), wsevent.Event{Type: "user_outbid"}); err != nil {
		t.Fatalf("Unicast error: %v", err)
	}

	if len(control.high) != 1 {
		t.Fatalf("control stream should receive user_outbid, got %d events", len(control.high))
	}
	if len(all.high) != 1 {
		t.Fatalf("all stream should receive user_outbid for backward compatibility, got %d events", len(all.high))
	}
	if len(market.high) != 0 || len(market.send) != 0 || market.latestTimeSync != nil {
		t.Fatalf("market stream should not receive control user_outbid")
	}
}

func TestSlowConnectionIsClosedOnFullChannel(t *testing.T) {
	h := NewHub(nil)
	// send channel 容量为 1，填满后下次 Fanout 应触发 closeConn
	c := &Conn{
		id:     "slow_conn",
		userID: "user_slow",
		roomID: "room_1",
		stream: streamAll,
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

type captureWSRecorder struct {
	observability.NoopRecorder
	mu          sync.Mutex
	connections []observability.WSConnectionMetric
	broadcasts  []observability.WSBroadcastMetric
	deliveries  []observability.WSDeliveryMetric
	writes      []observability.WSWriteMetric
	timeSyncs   []observability.WSTimeSyncMetric
	lifecycles  []observability.WSConnectionLifecycleMetric
}

func (r *captureWSRecorder) WSConnection(_ context.Context, m observability.WSConnectionMetric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connections = append(r.connections, m)
}

func (r *captureWSRecorder) WSBroadcast(_ context.Context, m observability.WSBroadcastMetric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.broadcasts = append(r.broadcasts, m)
}

func (r *captureWSRecorder) WSDelivery(_ context.Context, m observability.WSDeliveryMetric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveries = append(r.deliveries, m)
}

func (r *captureWSRecorder) WSWrite(_ context.Context, m observability.WSWriteMetric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, m)
}

func (r *captureWSRecorder) WSTimeSync(_ context.Context, m observability.WSTimeSyncMetric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timeSyncs = append(r.timeSyncs, m)
}

func (r *captureWSRecorder) WSConnectionLifecycle(_ context.Context, m observability.WSConnectionLifecycleMetric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lifecycles = append(r.lifecycles, m)
}

func (r *captureWSRecorder) connectionDelta() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int64
	for _, m := range r.connections {
		total += m.ActiveDelta
	}
	return total
}

func (r *captureWSRecorder) lastBroadcast() observability.WSBroadcastMetric {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.broadcasts) == 0 {
		return observability.WSBroadcastMetric{}
	}
	return r.broadcasts[len(r.broadcasts)-1]
}

func (r *captureWSRecorder) lastDelivery() observability.WSDeliveryMetric {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.deliveries) == 0 {
		return observability.WSDeliveryMetric{}
	}
	return r.deliveries[len(r.deliveries)-1]
}

func (r *captureWSRecorder) deliveryCount(result string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int
	for _, m := range r.deliveries {
		if m.Result == result {
			total++
		}
	}
	return total
}

func (r *captureWSRecorder) lastWrite() observability.WSWriteMetric {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.writes) == 0 {
		return observability.WSWriteMetric{}
	}
	return r.writes[len(r.writes)-1]
}

func (r *captureWSRecorder) writeCount(result string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int
	for _, m := range r.writes {
		if m.Result == result {
			total++
		}
	}
	return total
}

func (r *captureWSRecorder) closeReasonCount(reason string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int
	for _, m := range r.connections {
		if m.Action == "close" && m.Reason == reason {
			total++
		}
	}
	return total
}

func (r *captureWSRecorder) timeSyncCount(action, result string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int
	for _, m := range r.timeSyncs {
		if m.Action == action && m.Result == result {
			total++
		}
	}
	return total
}

func (r *captureWSRecorder) lifecycleCount(stream, result, reason string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int
	for _, m := range r.lifecycles {
		if m.Stream == stream && m.Result == result && m.Reason == reason {
			total++
		}
	}
	return total
}

func (r *captureWSRecorder) lastTimeSync() observability.WSTimeSyncMetric {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.timeSyncs) == 0 {
		return observability.WSTimeSyncMetric{}
	}
	return r.timeSyncs[len(r.timeSyncs)-1]
}
