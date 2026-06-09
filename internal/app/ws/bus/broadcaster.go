package bus

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/errorx"
	"github.com/zet-plane/live-auction-backend/pkg/wsevent"
)

type Publisher interface {
	Publish(ctx context.Context, channel, payload string) error
}

type RedisPublisher struct {
	client *redis.Client
}

func NewRedisPublisher(client *redis.Client) *RedisPublisher {
	return &RedisPublisher{client: client}
}

func (p *RedisPublisher) Publish(ctx context.Context, channel, payload string) error {
	return p.client.Publish(ctx, channel, payload).Err()
}

type ActiveRedisProvider interface {
	ActiveRedis() (*redis.Client, availability.Snapshot, bool)
}

var ErrEventBusUnavailable = errorx.New(http.StatusServiceUnavailable, 50302, "websocket event bus temporarily unavailable")

type ActiveRedisPublisher struct {
	provider ActiveRedisProvider
}

func NewActiveRedisPublisher(provider ActiveRedisProvider) *ActiveRedisPublisher {
	return &ActiveRedisPublisher{provider: provider}
}

func (p *ActiveRedisPublisher) Publish(ctx context.Context, channel, payload string) error {
	client, _, ok := p.provider.ActiveRedis()
	if !ok {
		return ErrEventBusUnavailable
	}
	return client.Publish(ctx, channel, payload).Err()
}

type Options struct {
	PodID      string
	NewEventID func() string
	Now        func() time.Time
}

type Broadcaster struct {
	publisher  Publisher
	podID      string
	newEventID func() string
	now        func() time.Time
}

func NewBroadcaster(publisher Publisher, opts Options) *Broadcaster {
	if opts.NewEventID == nil {
		opts.NewEventID = newEventID
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Broadcaster{
		publisher:  publisher,
		podID:      opts.PodID,
		newEventID: opts.NewEventID,
		now:        opts.Now,
	}
}

func (b *Broadcaster) Fanout(topic string, event wsevent.Event) error {
	target, err := topicTarget(topic)
	if err != nil {
		return err
	}
	env, err := envelopeFromEvent(ScopeRoom, target, event, b.podID, b.newEventID, b.now)
	if err != nil {
		return err
	}
	return b.publish(ChannelRoom, env)
}

func (b *Broadcaster) Unicast(addr string, event wsevent.Event) error {
	target, err := userTarget(addr)
	if err != nil {
		return err
	}
	env, err := envelopeFromEvent(ScopeUser, target, event, b.podID, b.newEventID, b.now)
	if err != nil {
		return err
	}
	return b.publish(ChannelUser, env)
}

func (b *Broadcaster) publish(channel string, env Envelope) error {
	raw, err := json.Marshal(env)
	if err != nil {
		recordBus("publish", "error", env.Scope, env.Type)
		return err
	}
	err = b.publisher.Publish(context.Background(), channel, string(raw))
	if err != nil {
		recordBus("publish", "error", env.Scope, env.Type)
		return err
	}
	recordBus("publish", "success", env.Scope, env.Type)
	return nil
}

func recordBus(action, result, scope, eventType string) {
	observability.DefaultRecorder().WSEventBus(context.Background(), observability.WSEventBusMetric{
		Action:    action,
		Result:    result,
		Scope:     scope,
		EventType: eventType,
	})
}
