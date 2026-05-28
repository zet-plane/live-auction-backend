package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func Track(ctx context.Context, op string, fields ...any) func(*error, ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	ctx, span := otel.Tracer("github.com/zet-plane/live-auction-backend/business").Start(ctx, op)
	span.SetAttributes(attributesFromKV(fields)...)
	logx.Infow("operation started", append([]any{"op", op}, fields...)...)

	return func(errp *error, extra ...any) {
		duration := time.Since(start)
		span.SetAttributes(attributesFromKV(extra)...)
		span.SetAttributes(attribute.Float64("duration_ms", float64(duration.Milliseconds())))

		kv := append([]any{"op", op}, fields...)
		kv = append(kv, extra...)
		kv = append(kv, "elapsed", duration)

		result := stringValue(extra, "result", "success")
		reason := stringValue(extra, "reason", "none")
		amount := int64Value(fields, "amount", int64Value(fields, "price", 0))
		recordOperationMetric(ctx, op, result, reason, amount, duration)

		if errp != nil && *errp != nil {
			span.RecordError(*errp)
			span.SetStatus(codes.Error, (*errp).Error())
			kv = append(kv, "err", *errp)
			logx.Warnw("operation failed", kv...)
			span.End()
			return
		}

		span.SetStatus(codes.Ok, "")
		logx.Infow("operation completed", kv...)
		span.End()
	}
}

func recordOperationMetric(ctx context.Context, op, result, reason string, amount int64, duration time.Duration) {
	switch op {
	case "auction.place_bid":
		DefaultRecorder().Bid(ctx, BidMetric{Result: result, Reason: reason, Amount: amount, Duration: duration})
	case "order.create_from_auction":
		DefaultRecorder().OrderAuctionCreate(ctx, OrderMetric{Result: result})
	}
}

func attributesFromKV(kv []any) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok || key == "" {
			continue
		}
		switch v := kv[i+1].(type) {
		case string:
			attrs = append(attrs, attribute.String(key, v))
		case int:
			attrs = append(attrs, attribute.Int(key, v))
		case int64:
			attrs = append(attrs, attribute.Int64(key, v))
		case bool:
			attrs = append(attrs, attribute.Bool(key, v))
		default:
			attrs = append(attrs, attribute.String(key, fmt.Sprint(v)))
		}
	}
	return attrs
}

func stringValue(kv []any, key, fallback string) string {
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok && k == key {
			if v, ok := kv[i+1].(string); ok && v != "" {
				return v
			}
		}
	}
	return fallback
}

func int64Value(kv []any, key string, fallback int64) int64 {
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok && k == key {
			switch v := kv[i+1].(type) {
			case int:
				return int64(v)
			case int64:
				return v
			}
		}
	}
	return fallback
}
