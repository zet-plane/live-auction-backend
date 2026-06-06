package bus

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type fakePublisher struct {
	channel string
	payload string
	err     error
}

func (p *fakePublisher) Publish(_ context.Context, channel, payload string) error {
	p.channel = channel
	p.payload = payload
	return p.err
}

func TestBroadcasterPublishesRoomFanoutEnvelope(t *testing.T) {
	pub := &fakePublisher{}
	b := NewBroadcaster(pub, Options{PodID: "pod_a", NewEventID: func() string { return "evt_1" }})

	err := b.Fanout(wsevent.RoomTopic("room_1"), wsevent.Event{
		Type: "bid_success",
		Payload: map[string]any{
			"item_id":         "item_1",
			"auction_version": float64(7),
		},
	})
	if err != nil {
		t.Fatalf("Fanout returned error: %v", err)
	}
	if pub.channel != ChannelRoom {
		t.Fatalf("channel = %q, want %q", pub.channel, ChannelRoom)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(pub.payload), &env); err != nil {
		t.Fatalf("payload is not envelope json: %v", err)
	}
	if env.EventID != "evt_1" || env.Scope != ScopeRoom || env.Target != "room_1" || env.Type != "bid_success" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if env.AuctionVersion != 7 {
		t.Fatalf("auction_version = %d, want 7", env.AuctionVersion)
	}
	if env.SourcePod != "pod_a" {
		t.Fatalf("source pod = %q, want pod_a", env.SourcePod)
	}
}

func TestBroadcasterPublishesUserUnicastEnvelope(t *testing.T) {
	pub := &fakePublisher{}
	b := NewBroadcaster(pub, Options{PodID: "pod_b", NewEventID: func() string { return "evt_2" }})

	err := b.Unicast(wsevent.UserAddr("user_1"), wsevent.Event{Type: "order_created"})
	if err != nil {
		t.Fatalf("Unicast returned error: %v", err)
	}
	if pub.channel != ChannelUser {
		t.Fatalf("channel = %q, want %q", pub.channel, ChannelUser)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(pub.payload), &env); err != nil {
		t.Fatalf("payload is not envelope json: %v", err)
	}
	if env.EventID != "evt_2" || env.Scope != ScopeUser || env.Target != "user_1" || env.Type != "order_created" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

type fakeDispatcher struct {
	roomID string
	userID string
	event  wsevent.Event
}

func (d *fakeDispatcher) SendToRoom(roomID string, event wsevent.Event) {
	d.roomID = roomID
	d.event = event
}

func (d *fakeDispatcher) SendToUser(userID string, event wsevent.Event) {
	d.userID = userID
	d.event = event
}

func TestSubscriberDispatchesRoomEnvelopeLocally(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	s := NewSubscriber(dispatcher)
	raw := `{"event_id":"evt_1","scope":"room","target":"room_1","type":"bid_success","payload":{"item_id":"item_1"},"source_pod":"pod_a","created_at_unix_ms":1}`

	if err := s.DispatchPayload([]byte(raw)); err != nil {
		t.Fatalf("DispatchPayload returned error: %v", err)
	}
	if dispatcher.roomID != "room_1" {
		t.Fatalf("roomID = %q, want room_1", dispatcher.roomID)
	}
	if dispatcher.event.Type != "bid_success" {
		t.Fatalf("event type = %q, want bid_success", dispatcher.event.Type)
	}
}

func TestSubscriberDispatchesUserEnvelopeLocally(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	s := NewSubscriber(dispatcher)
	raw := `{"event_id":"evt_2","scope":"user","target":"user_1","type":"order_created","payload":{"order_id":"order_1"},"source_pod":"pod_b","created_at_unix_ms":1}`

	if err := s.DispatchPayload([]byte(raw)); err != nil {
		t.Fatalf("DispatchPayload returned error: %v", err)
	}
	if dispatcher.userID != "user_1" {
		t.Fatalf("userID = %q, want user_1", dispatcher.userID)
	}
	if dispatcher.event.Type != "order_created" {
		t.Fatalf("event type = %q, want order_created", dispatcher.event.Type)
	}
}

func TestSubscriberRejectsMalformedEnvelope(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	s := NewSubscriber(dispatcher)

	if err := s.DispatchPayload([]byte(`{"scope":"room","target":"","type":"bid_success"}`)); err == nil {
		t.Fatal("expected malformed envelope error")
	}
	if dispatcher.roomID != "" || dispatcher.userID != "" {
		t.Fatalf("malformed envelope should not dispatch: %+v", dispatcher)
	}
}

func TestSubscriberRecordsMetricForInvalidJSONPayload(t *testing.T) {
	rec := &captureBusRecorder{}
	observability.SetDefaultRecorder(rec)
	t.Cleanup(func() { observability.SetDefaultRecorder(nil) })

	dispatcher := &fakeDispatcher{}
	s := NewSubscriber(dispatcher)

	if err := s.DispatchPayload([]byte(`{`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if dispatcher.roomID != "" || dispatcher.userID != "" {
		t.Fatalf("invalid JSON should not dispatch: %+v", dispatcher)
	}
	if len(rec.events) != 1 {
		t.Fatalf("event bus metrics = %d, want 1", len(rec.events))
	}
	got := rec.events[0]
	if got.Action != "dispatch" || got.Result != "error" {
		t.Fatalf("metric = %+v, want dispatch/error", got)
	}
}

type captureBusRecorder struct {
	observability.NoopRecorder
	events []observability.WSEventBusMetric
}

func (r *captureBusRecorder) WSEventBus(_ context.Context, m observability.WSEventBusMetric) {
	r.events = append(r.events, m)
}
