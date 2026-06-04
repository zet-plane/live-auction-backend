package cache

import (
	"context"
	"strconv"

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
