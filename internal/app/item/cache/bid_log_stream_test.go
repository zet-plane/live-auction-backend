package cache

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/zet-plane/live-auction-backend/internal/core/availability"
)

func TestNewBidLogXAddArgsDoesNotTrimAcceptedBidEvents(t *testing.T) {
	args := newBidLogXAddArgs(BidLogEvent{
		BidID:           "bid_1",
		ItemID:          "item_1",
		RoomID:          "room_1",
		UserID:          "user_1",
		Price:           1200,
		CreatedAtUnixMS: 1780560000123,
	})

	if args.Stream != BidLogStreamName {
		t.Fatalf("expected stream %q, got %q", BidLogStreamName, args.Stream)
	}
	if args.MaxLen != 0 || args.Approx {
		t.Fatalf("expected no stream trimming, got maxlen=%d approx=%v", args.MaxLen, args.Approx)
	}
}

func TestBidLuaScriptAppendsAcceptedBidLogWithoutTrimming(t *testing.T) {
	if !strings.Contains(bidLuaScript, "XADD") || !strings.Contains(bidLuaScript, BidLogStreamName) {
		t.Fatalf("expected bid lua script to append to %q", BidLogStreamName)
	}
	if !strings.Contains(bidLuaScript, bidLogFieldCreatedAtUnixMS) {
		t.Fatalf("expected bid lua script to include %q", bidLogFieldCreatedAtUnixMS)
	}
	if strings.Contains(bidLuaScript, "MAXLEN") {
		t.Fatal("expected bid lua accepted-event append not to use stream trimming")
	}
	xaddIndex := strings.Index(bidLuaScript, "XADD")
	setIndex := strings.Index(bidLuaScript, "SET', idem_key")
	if xaddIndex < 0 || setIndex < 0 || xaddIndex > setIndex {
		t.Fatal("expected bid lua script to append bid log before recording idempotency")
	}
}

func TestBidLuaScriptStoresAuctionVersionWithIdempotencyKey(t *testing.T) {
	if !strings.Contains(bidLuaScript, "bid_id .. '|' .. auction_version") {
		t.Fatal("expected bid lua script to store auction_version with idempotency value")
	}
	if !strings.Contains(bidLuaScript, "string.match(existing, '^([^|]+)|(%d+)$')") {
		t.Fatal("expected bid lua script to parse original idempotent bid version")
	}
}

func TestBidLogEventFromStreamValuesParsesEvent(t *testing.T) {
	event, err := bidLogEventFromStreamValues(map[string]any{
		"bid_id":             "bid_1",
		"item_id":            "item_1",
		"room_id":            "room_1",
		"user_id":            "user_1",
		"price":              "1200",
		"created_at_unix_ms": "1780560000123",
		"authority_epoch":    "7",
		"auction_version":    "3",
	})
	if err != nil {
		t.Fatalf("bidLogEventFromStreamValues returned error: %v", err)
	}
	if event.BidID != "bid_1" || event.ItemID != "item_1" || event.RoomID != "room_1" || event.UserID != "user_1" {
		t.Fatalf("string fields not copied: %+v", event)
	}
	if event.Price != 1200 || event.CreatedAtUnixMS != 1780560000123 {
		t.Fatalf("numeric fields not parsed: %+v", event)
	}
}

func TestParseBidLogStreamMessageIncludesAuthorityFields(t *testing.T) {
	messages := []redis.XMessage{{
		ID: "1-0",
		Values: map[string]any{
			"bid_id": "bid_1", "item_id": "item_1", "room_id": "room_1", "user_id": "user_1",
			"price": "1200", "created_at_unix_ms": "1710000000000",
			"authority_epoch": "7", "auction_version": "3", "idempotency_key": "idem_1",
		},
	}}
	got, err := parseBidLogStreamMessages(messages, nil)
	if err != nil {
		t.Fatalf("parseBidLogStreamMessages() error = %v", err)
	}
	if got[0].Event.AuthorityEpoch != 7 || got[0].Event.AuctionVersion != 3 || got[0].Event.IdempotencyKey != "idem_1" {
		t.Fatalf("event = %+v", got[0].Event)
	}
}

func TestBidLogEventFromStreamValuesRejectsInvalidNumericField(t *testing.T) {
	_, err := bidLogEventFromStreamValues(map[string]any{
		"bid_id":             "bid_1",
		"item_id":            "item_1",
		"room_id":            "room_1",
		"user_id":            "user_1",
		"price":              "not-a-number",
		"created_at_unix_ms": "1780560000123",
	})
	if err == nil {
		t.Fatal("expected invalid numeric field error")
	}
}

func TestBidLogEventFromStreamValuesRejectsMissingNumericField(t *testing.T) {
	_, err := bidLogEventFromStreamValues(map[string]any{
		"bid_id":             "bid_1",
		"item_id":            "item_1",
		"room_id":            "room_1",
		"user_id":            "user_1",
		"created_at_unix_ms": "1780560000123",
	})
	if err == nil {
		t.Fatal("expected missing numeric field error")
	}
}

func TestParseBidLogStreamMessagesDeadLettersMalformedAndReturnsValid(t *testing.T) {
	var deadLettered []string
	messages, err := parseBidLogStreamMessages([]redis.XMessage{
		{
			ID: "stream-1",
			Values: map[string]any{
				"bid_id":             "bid_1",
				"item_id":            "item_1",
				"room_id":            "room_1",
				"user_id":            "user_1",
				"price":              "1200",
				"created_at_unix_ms": "1780560000123",
				"authority_epoch":    "7",
				"auction_version":    "1",
			},
		},
		{
			ID: "stream-bad",
			Values: map[string]any{
				"bid_id":             "bid_bad",
				"item_id":            "item_1",
				"room_id":            "room_1",
				"user_id":            "user_bad",
				"price":              "bad-price",
				"created_at_unix_ms": "1780560000123",
			},
		},
		{
			ID: "stream-2",
			Values: map[string]any{
				"bid_id":             "bid_2",
				"item_id":            "item_1",
				"room_id":            "room_1",
				"user_id":            "user_2",
				"price":              "1400",
				"created_at_unix_ms": "1780560001123",
				"authority_epoch":    "7",
				"auction_version":    "2",
			},
		},
	}, func(message redis.XMessage, _ error) error {
		deadLettered = append(deadLettered, message.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("parseBidLogStreamMessages returned error: %v", err)
	}
	if !reflect.DeepEqual(deadLettered, []string{"stream-bad"}) {
		t.Fatalf("expected malformed message to be dead-lettered, got %#v", deadLettered)
	}
	if len(messages) != 2 || messages[0].ID != "stream-1" || messages[1].ID != "stream-2" {
		t.Fatalf("expected valid messages to be returned, got %#v", messages)
	}
}

func TestParseBidLogStreamMessagesReturnsDeadLetterError(t *testing.T) {
	deadLetterErr := errors.New("dead letter unavailable")
	_, err := parseBidLogStreamMessages([]redis.XMessage{
		{
			ID: "stream-bad",
			Values: map[string]any{
				"bid_id":             "bid_bad",
				"item_id":            "item_1",
				"room_id":            "room_1",
				"user_id":            "user_bad",
				"price":              "bad-price",
				"created_at_unix_ms": "1780560000123",
			},
		},
	}, func(redis.XMessage, error) error {
		return deadLetterErr
	})
	if !errors.Is(err, deadLetterErr) {
		t.Fatalf("expected dead-letter error, got %v", err)
	}
}

func TestActiveBidLogStreamReaderReadsFromCurrentActiveRedis(t *testing.T) {
	cloud, cloudHook := newBidLogStreamHookedClient()
	local, localHook := newBidLogStreamHookedClient()
	provider := &fakeBidLogActiveRedisProvider{client: local}
	reader := NewActiveBidLogStreamReader(provider, "consumer-1")

	messages, err := reader.Read(context.Background(), 10)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(messages) != 1 || messages[0].Event.BidID != "bid_local" {
		t.Fatalf("messages = %+v, want local bid", messages)
	}
	if got := localHook.count("xreadgroup"); got != 1 {
		t.Fatalf("local xreadgroup calls = %d, want 1", got)
	}
	if got := cloudHook.count("xreadgroup"); got != 0 {
		t.Fatalf("cloud xreadgroup calls = %d, want 0", got)
	}
	_ = cloud.Close()
	_ = local.Close()
}

func TestActiveBidLogStreamReaderAcksReadSourceAfterActiveRedisSwitches(t *testing.T) {
	cloud, cloudHook := newBidLogStreamHookedClient()
	local, localHook := newBidLogStreamHookedClient()
	provider := &fakeBidLogActiveRedisProvider{client: cloud}
	reader := NewActiveBidLogStreamReader(provider, "consumer-1")

	messages, err := reader.Read(context.Background(), 10)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(messages) != 1 || messages[0].ID == "" {
		t.Fatalf("messages = %+v, want one cloud message", messages)
	}

	provider.client = local
	if err := reader.Ack(context.Background(), []string{messages[0].ID}); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if got := cloudHook.count("xack"); got != 1 {
		t.Fatalf("cloud xack calls = %d, want 1", got)
	}
	if got := localHook.count("xack"); got != 0 {
		t.Fatalf("local xack calls = %d, want 0", got)
	}
	_ = cloud.Close()
	_ = local.Close()
}

type fakeBidLogActiveRedisProvider struct {
	client *redis.Client
}

func (p *fakeBidLogActiveRedisProvider) ActiveRedis() (*redis.Client, availability.Snapshot, bool) {
	return p.client, availability.Snapshot{Valid: true, ActiveRedis: availability.RedisLocal}, p.client != nil
}

type bidLogStreamHook struct {
	mu    sync.Mutex
	calls map[string]int
}

func newBidLogStreamHookedClient() (*redis.Client, *bidLogStreamHook) {
	hook := &bidLogStreamHook{calls: make(map[string]int)}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(hook)
	return client, hook
}

func (h *bidLogStreamHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *bidLogStreamHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *bidLogStreamHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		name := strings.ToLower(cmd.Name())
		h.mu.Lock()
		h.calls[name]++
		h.mu.Unlock()

		switch c := cmd.(type) {
		case *redis.StatusCmd:
			c.SetVal("OK")
		case *redis.XStreamSliceCmd:
			c.SetVal([]redis.XStream{{
				Stream: BidLogStreamName,
				Messages: []redis.XMessage{{
					ID:     "1-0",
					Values: bidLogStreamValues(BidLogEvent{BidID: "bid_local", ItemID: "item_1", RoomID: "room_1", UserID: "user_1", Price: 1200, CreatedAtUnixMS: 1780560000123, AuthorityEpoch: 0, AuctionVersion: 1}),
				}},
			}})
		case *redis.XAutoClaimCmd:
			c.SetVal(nil, "0-0")
		case *redis.IntCmd:
			c.SetVal(1)
		default:
			return next(ctx, cmd)
		}
		return nil
	}
}

func (h *bidLogStreamHook) count(command string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[command]
}
