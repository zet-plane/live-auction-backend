package bus

import (
	"context"
	"encoding/json"
	"testing"

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
