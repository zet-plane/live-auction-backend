package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const BidLogStreamName = "auction:bid_log:stream"
const BidLogDeadStreamName = "auction:bid_log:dead"
const BidLogConsumerGroup = "bid-log-writers"
const BidLogDeadStreamMaxLen = 100000

const (
	bidLogFieldBidID           = "bid_id"
	bidLogFieldItemID          = "item_id"
	bidLogFieldRoomID          = "room_id"
	bidLogFieldUserID          = "user_id"
	bidLogFieldPrice           = "price"
	bidLogFieldCreatedAtUnixMS = "created_at_unix_ms"
	bidLogFieldAuthorityEpoch  = "authority_epoch"
	bidLogFieldAuctionVersion  = "auction_version"
	bidLogFieldIdempotencyKey  = "idempotency_key"
)

const bidLogPendingMinIdle = 30 * time.Second

func (c *RedisCache) AppendBidLogEvent(ctx context.Context, event BidLogEvent) error {
	return c.client.XAdd(ctx, newBidLogXAddArgs(event)).Err()
}

func newBidLogXAddArgs(event BidLogEvent) *redis.XAddArgs {
	return &redis.XAddArgs{
		Stream: BidLogStreamName,
		Values: bidLogStreamValues(event),
	}
}

func bidLogStreamValues(event BidLogEvent) map[string]any {
	return map[string]any{
		bidLogFieldBidID:           event.BidID,
		bidLogFieldItemID:          event.ItemID,
		bidLogFieldRoomID:          event.RoomID,
		bidLogFieldUserID:          event.UserID,
		bidLogFieldPrice:           strconv.FormatInt(event.Price, 10),
		bidLogFieldCreatedAtUnixMS: strconv.FormatInt(event.CreatedAtUnixMS, 10),
		bidLogFieldAuthorityEpoch:  strconv.FormatInt(event.AuthorityEpoch, 10),
		bidLogFieldAuctionVersion:  strconv.FormatInt(event.AuctionVersion, 10),
		bidLogFieldIdempotencyKey:  event.IdempotencyKey,
	}
}

type BidLogStreamReader struct {
	client   *redis.Client
	consumer string
}

func NewBidLogStreamReader(client *redis.Client, consumer string) *BidLogStreamReader {
	return &BidLogStreamReader{client: client, consumer: consumer}
}

func (r *BidLogStreamReader) EnsureGroup(ctx context.Context) error {
	return ensureBidLogGroup(ctx, r.client)
}

type ActiveBidLogStreamReader struct {
	provider activeRedisProvider
	consumer string

	mu             sync.Mutex
	ensuredClients map[*redis.Client]struct{}
	lastReadClient *redis.Client
}

func NewActiveBidLogStreamReader(provider activeRedisProvider, consumer string) *ActiveBidLogStreamReader {
	return &ActiveBidLogStreamReader{
		provider:       provider,
		consumer:       consumer,
		ensuredClients: make(map[*redis.Client]struct{}),
	}
}

func (r *ActiveBidLogStreamReader) EnsureGroup(ctx context.Context) error {
	client, err := r.activeClient()
	if err != nil {
		return err
	}
	return r.ensureGroup(ctx, client)
}

func (r *ActiveBidLogStreamReader) Read(ctx context.Context, count int) ([]BidLogStreamMessage, error) {
	reader, client, err := r.currentReader(ctx)
	if err != nil {
		return nil, err
	}
	messages, err := reader.Read(ctx, count)
	if err == nil {
		r.rememberReadClient(client)
	}
	return messages, err
}

func (r *ActiveBidLogStreamReader) ReadPending(ctx context.Context, count int) ([]BidLogStreamMessage, error) {
	reader, client, err := r.currentReader(ctx)
	if err != nil {
		return nil, err
	}
	messages, err := reader.ReadPending(ctx, count)
	if err == nil {
		r.rememberReadClient(client)
	}
	return messages, err
}

func (r *ActiveBidLogStreamReader) Ack(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	client := r.ackClient()
	if client == nil {
		var err error
		client, err = r.activeClient()
		if err != nil {
			return err
		}
	}
	if err := r.ensureGroup(ctx, client); err != nil {
		return err
	}
	return NewBidLogStreamReader(client, r.consumer).Ack(ctx, ids)
}

func (r *ActiveBidLogStreamReader) currentReader(ctx context.Context) (*BidLogStreamReader, *redis.Client, error) {
	client, err := r.activeClient()
	if err != nil {
		return nil, nil, err
	}
	if err := r.ensureGroup(ctx, client); err != nil {
		return nil, nil, err
	}
	return NewBidLogStreamReader(client, r.consumer), client, nil
}

func (r *ActiveBidLogStreamReader) activeClient() (*redis.Client, error) {
	if r == nil || r.provider == nil {
		return nil, errActiveRedisUnavailable
	}
	client, _, ok := r.provider.ActiveRedis()
	if !ok || client == nil {
		return nil, errActiveRedisUnavailable
	}
	return client, nil
}

func (r *ActiveBidLogStreamReader) ensureGroup(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return errActiveRedisUnavailable
	}
	r.mu.Lock()
	if _, ok := r.ensuredClients[client]; ok {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	if err := ensureBidLogGroup(ctx, client); err != nil {
		return err
	}

	r.mu.Lock()
	r.ensuredClients[client] = struct{}{}
	r.mu.Unlock()
	return nil
}

func (r *ActiveBidLogStreamReader) rememberReadClient(client *redis.Client) {
	r.mu.Lock()
	r.lastReadClient = client
	r.mu.Unlock()
}

func (r *ActiveBidLogStreamReader) ackClient() *redis.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastReadClient
}

func ensureBidLogGroup(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return errActiveRedisUnavailable
	}
	err := client.XGroupCreateMkStream(ctx, BidLogStreamName, BidLogConsumerGroup, "0").Err()
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (r *BidLogStreamReader) Read(ctx context.Context, count int) ([]BidLogStreamMessage, error) {
	if count <= 0 {
		count = 1
	}
	streams, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    BidLogConsumerGroup,
		Consumer: r.consumer,
		Streams:  []string{BidLogStreamName, ">"},
		Count:    int64(count),
		Block:    50 * time.Millisecond,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	return r.parseRedisMessages(ctx, redisStreamMessages(streams))
}

func (r *BidLogStreamReader) ReadPending(ctx context.Context, count int) ([]BidLogStreamMessage, error) {
	if count <= 0 {
		count = 1
	}
	messages, _, err := r.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   BidLogStreamName,
		Group:    BidLogConsumerGroup,
		Consumer: r.consumer,
		MinIdle:  bidLogPendingMinIdle,
		Start:    "0-0",
		Count:    int64(count),
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return r.parseRedisMessages(ctx, messages)
}

func (r *BidLogStreamReader) parseRedisMessages(ctx context.Context, messages []redis.XMessage) ([]BidLogStreamMessage, error) {
	return parseBidLogStreamMessages(messages, func(message redis.XMessage, parseErr error) error {
		return r.deadLetterAndAck(ctx, message, parseErr)
	})
}

func redisStreamMessages(streams []redis.XStream) []redis.XMessage {
	messages := make([]redis.XMessage, 0)
	for _, stream := range streams {
		messages = append(messages, stream.Messages...)
	}
	return messages
}

func (r *BidLogStreamReader) Ack(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.client.XAck(ctx, BidLogStreamName, BidLogConsumerGroup, ids...).Err()
}

func (r *BidLogStreamReader) deadLetterAndAck(ctx context.Context, message redis.XMessage, parseErr error) error {
	values := map[string]any{
		"stream_id": message.ID,
		"error":     parseErr.Error(),
	}
	for key, value := range message.Values {
		values["source_"+key] = fmt.Sprint(value)
	}
	if err := r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: BidLogDeadStreamName,
		MaxLen: BidLogDeadStreamMaxLen,
		Approx: true,
		Values: values,
	}).Err(); err != nil {
		return err
	}
	return r.Ack(ctx, []string{message.ID})
}

func parseBidLogStreamMessages(messages []redis.XMessage, deadLetter func(redis.XMessage, error) error) ([]BidLogStreamMessage, error) {
	result := make([]BidLogStreamMessage, 0, len(messages))
	for _, message := range messages {
		event, err := bidLogEventFromStreamValues(message.Values)
		if err != nil {
			parseErr := fmt.Errorf("parse bid log stream message %s: %w", message.ID, err)
			if deadLetter == nil {
				return nil, parseErr
			}
			if deadLetterErr := deadLetter(message, parseErr); deadLetterErr != nil {
				return nil, fmt.Errorf("dead-letter bid log stream message %s: %w", message.ID, deadLetterErr)
			}
			continue
		}
		result = append(result, BidLogStreamMessage{ID: message.ID, Event: event})
	}
	return result, nil
}

func bidLogEventFromStreamValues(values map[string]any) (BidLogEvent, error) {
	price, err := requiredStreamInt64(values, bidLogFieldPrice)
	if err != nil {
		return BidLogEvent{}, err
	}
	createdAtUnixMS, err := requiredStreamInt64(values, bidLogFieldCreatedAtUnixMS)
	if err != nil {
		return BidLogEvent{}, err
	}
	bidID, err := requiredStreamString(values, bidLogFieldBidID)
	if err != nil {
		return BidLogEvent{}, err
	}
	itemID, err := requiredStreamString(values, bidLogFieldItemID)
	if err != nil {
		return BidLogEvent{}, err
	}
	roomID, err := requiredStreamString(values, bidLogFieldRoomID)
	if err != nil {
		return BidLogEvent{}, err
	}
	userID, err := requiredStreamString(values, bidLogFieldUserID)
	if err != nil {
		return BidLogEvent{}, err
	}
	authorityEpoch, err := requiredStreamInt64(values, bidLogFieldAuthorityEpoch)
	if err != nil {
		return BidLogEvent{}, err
	}
	auctionVersion, err := requiredStreamInt64(values, bidLogFieldAuctionVersion)
	if err != nil {
		return BidLogEvent{}, err
	}
	idempotencyKey := optionalStreamString(values, bidLogFieldIdempotencyKey)
	return BidLogEvent{
		BidID:           bidID,
		ItemID:          itemID,
		RoomID:          roomID,
		UserID:          userID,
		Price:           price,
		CreatedAtUnixMS: createdAtUnixMS,
		AuthorityEpoch:  authorityEpoch,
		AuctionVersion:  auctionVersion,
		IdempotencyKey:  idempotencyKey,
	}, nil
}

func requiredStreamInt64(values map[string]any, field string) (int64, error) {
	raw, err := requiredStreamString(values, field)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", field, err)
	}
	return n, nil
}

func optionalStreamString(values map[string]any, field string) string {
	raw, ok := values[field]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func requiredStreamString(values map[string]any, field string) (string, error) {
	raw, ok := values[field]
	if !ok {
		return "", fmt.Errorf("missing %s", field)
	}
	switch v := raw.(type) {
	case string:
		if v == "" {
			return "", fmt.Errorf("missing %s", field)
		}
		return v, nil
	case []byte:
		if len(v) == 0 {
			return "", fmt.Errorf("missing %s", field)
		}
		return string(v), nil
	default:
		return "", fmt.Errorf("invalid %s type %T", field, raw)
	}
}
