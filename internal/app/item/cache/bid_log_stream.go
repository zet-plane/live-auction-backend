package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const BidLogStreamName = "auction:bid_log:stream"
const BidLogDeadStreamName = "auction:bid_log:dead"
const BidLogConsumerGroup = "bid-log-writers"

func (c *RedisCache) AppendBidLogEvent(ctx context.Context, event BidLogEvent) error {
	return c.client.XAdd(ctx, &redis.XAddArgs{
		Stream: BidLogStreamName,
		Values: map[string]any{
			"bid_id":             event.BidID,
			"item_id":            event.ItemID,
			"room_id":            event.RoomID,
			"user_id":            event.UserID,
			"price":              strconv.FormatInt(event.Price, 10),
			"created_at_unix_ms": strconv.FormatInt(event.CreatedAtUnixMS, 10),
		},
	}).Err()
}

type BidLogStreamReader struct {
	client   *redis.Client
	consumer string
}

func NewBidLogStreamReader(client *redis.Client, consumer string) *BidLogStreamReader {
	return &BidLogStreamReader{client: client, consumer: consumer}
}

func (r *BidLogStreamReader) EnsureGroup(ctx context.Context) error {
	err := r.client.XGroupCreateMkStream(ctx, BidLogStreamName, BidLogConsumerGroup, "0").Err()
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

	messages := make([]BidLogStreamMessage, 0)
	for _, stream := range streams {
		for _, message := range stream.Messages {
			event, err := bidLogEventFromStreamValues(message.Values)
			if err != nil {
				return nil, fmt.Errorf("parse bid log stream message %s: %w", message.ID, err)
			}
			messages = append(messages, BidLogStreamMessage{ID: message.ID, Event: event})
		}
	}
	return messages, nil
}

func (r *BidLogStreamReader) Ack(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.client.XAck(ctx, BidLogStreamName, BidLogConsumerGroup, ids...).Err()
}

func bidLogEventFromStreamValues(values map[string]any) (BidLogEvent, error) {
	price, err := requiredStreamInt64(values, "price")
	if err != nil {
		return BidLogEvent{}, err
	}
	createdAtUnixMS, err := requiredStreamInt64(values, "created_at_unix_ms")
	if err != nil {
		return BidLogEvent{}, err
	}
	bidID, err := requiredStreamString(values, "bid_id")
	if err != nil {
		return BidLogEvent{}, err
	}
	itemID, err := requiredStreamString(values, "item_id")
	if err != nil {
		return BidLogEvent{}, err
	}
	roomID, err := requiredStreamString(values, "room_id")
	if err != nil {
		return BidLogEvent{}, err
	}
	userID, err := requiredStreamString(values, "user_id")
	if err != nil {
		return BidLogEvent{}, err
	}
	return BidLogEvent{
		BidID:           bidID,
		ItemID:          itemID,
		RoomID:          roomID,
		UserID:          userID,
		Price:           price,
		CreatedAtUnixMS: createdAtUnixMS,
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
