package bus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type LocalDispatcher interface {
	SendToRoom(roomID string, event wsevent.Event)
	SendToUser(userID string, event wsevent.Event)
}

type Subscriber struct {
	dispatcher LocalDispatcher
}

func NewSubscriber(dispatcher LocalDispatcher) *Subscriber {
	return &Subscriber{dispatcher: dispatcher}
}

func (s *Subscriber) DispatchPayload(raw []byte) error {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if env.Target == "" || env.Type == "" {
		recordBus("dispatch", "error", env.Scope, env.Type)
		return fmt.Errorf("invalid websocket bus envelope")
	}
	event := wsevent.Event{Type: env.Type, Payload: json.RawMessage(env.Payload)}
	switch env.Scope {
	case ScopeRoom:
		s.dispatcher.SendToRoom(env.Target, event)
	case ScopeUser:
		s.dispatcher.SendToUser(env.Target, event)
	default:
		recordBus("dispatch", "error", env.Scope, env.Type)
		return fmt.Errorf("unknown websocket bus scope: %s", env.Scope)
	}
	recordBus("dispatch", "success", env.Scope, env.Type)
	return nil
}

func (s *Subscriber) Run(ctx context.Context, client *redis.Client) {
	pubsub := client.Subscribe(ctx, ChannelRoom, ChannelUser)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := s.DispatchPayload([]byte(msg.Payload)); err != nil {
				logx.Warnw("ws bus dispatch failed", "channel", msg.Channel, "err", err)
			}
		}
	}
}
