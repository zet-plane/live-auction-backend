package bus

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/pkg/snowflake"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

const (
	ChannelRoom = "ws:event:room"
	ChannelUser = "ws:event:user"

	ScopeRoom = "room"
	ScopeUser = "user"
)

type Envelope struct {
	EventID         string          `json:"event_id"`
	Scope           string          `json:"scope"`
	Target          string          `json:"target"`
	Type            string          `json:"type"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	AuctionVersion  int64           `json:"auction_version,omitempty"`
	SourcePod       string          `json:"source_pod"`
	CreatedAtUnixMS int64           `json:"created_at_unix_ms"`
}

func newEventID() string {
	return "evt_" + snowflake.MakeUUID()
}

func envelopeFromEvent(scope, target string, event wsevent.Event, sourcePod string, eventID func() string, now func() time.Time) (Envelope, error) {
	raw, err := json.Marshal(event.Payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		EventID:         eventID(),
		Scope:           scope,
		Target:          target,
		Type:            event.Type,
		Payload:         raw,
		AuctionVersion:  auctionVersionFromJSON(raw),
		SourcePod:       sourcePod,
		CreatedAtUnixMS: now().UnixMilli(),
	}, nil
}

func topicTarget(topic string) (string, error) {
	target := strings.TrimPrefix(topic, "room:")
	if target == "" || target == topic {
		return "", fmt.Errorf("invalid room topic: %q", topic)
	}
	return target, nil
}

func userTarget(addr string) (string, error) {
	target := strings.TrimPrefix(addr, "user:")
	if target == "" || target == addr {
		return "", fmt.Errorf("invalid user address: %q", addr)
	}
	return target, nil
}

func auctionVersionFromJSON(raw []byte) int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return 0
	}
	switch v := fields["auction_version"].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		return 0
	}
}
