package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Recorder interface {
	HTTPRequest(context.Context, HTTPRequestMetric)
	RedisLua(context.Context, RedisLuaMetric)
	DBQuery(context.Context, DBQueryMetric)
	Cron(context.Context, CronMetric)
	Bid(context.Context, BidMetric)
	OrderAuctionCreate(context.Context, OrderMetric)
}

type HTTPRequestMetric struct {
	Route    string
	Method   string
	Status   int
	Duration time.Duration
}

type RedisLuaMetric struct {
	Code     string
	Duration time.Duration
}

type DBQueryMetric struct {
	Operation string
	Table     string
	Result    string
	Slow      bool
	Duration  time.Duration
}

type CronMetric struct {
	Name     string
	Result   string
	Duration time.Duration
}

type BidMetric struct {
	Result   string
	Reason   string
	Amount   int64
	Duration time.Duration
}

type OrderMetric struct {
	Result string
}

type NoopRecorder struct{}

func (NoopRecorder) HTTPRequest(context.Context, HTTPRequestMetric)  {}
func (NoopRecorder) RedisLua(context.Context, RedisLuaMetric)        {}
func (NoopRecorder) DBQuery(context.Context, DBQueryMetric)          {}
func (NoopRecorder) Cron(context.Context, CronMetric)                {}
func (NoopRecorder) Bid(context.Context, BidMetric)                  {}
func (NoopRecorder) OrderAuctionCreate(context.Context, OrderMetric) {}

var defaultRecorder Recorder = NoopRecorder{}

func SetDefaultRecorder(rec Recorder) {
	if rec == nil {
		defaultRecorder = NoopRecorder{}
		return
	}
	defaultRecorder = rec
}

func DefaultRecorder() Recorder {
	return defaultRecorder
}

type OTelRecorder struct {
	httpCount        metric.Int64Counter
	httpDuration     metric.Float64Histogram
	redisLuaCount    metric.Int64Counter
	redisLuaDuration metric.Float64Histogram
	dbCount          metric.Int64Counter
	dbDuration       metric.Float64Histogram
	cronCount        metric.Int64Counter
	cronDuration     metric.Float64Histogram
	bidCount         metric.Int64Counter
	bidAmount        metric.Int64Histogram
	bidDuration      metric.Float64Histogram
	orderCount       metric.Int64Counter
}

func NewRecorder() (*OTelRecorder, error) {
	meter := otel.Meter("github.com/zet-plane/live-auction-backend")
	httpCount, err := meter.Int64Counter("http.server.request.count")
	if err != nil {
		return nil, err
	}
	httpDuration, err := meter.Float64Histogram("http.server.request.duration")
	if err != nil {
		return nil, err
	}
	redisLuaCount, err := meter.Int64Counter("auction.place_bid.lua.result.count")
	if err != nil {
		return nil, err
	}
	redisLuaDuration, err := meter.Float64Histogram("auction.place_bid.lua.duration")
	if err != nil {
		return nil, err
	}
	dbCount, err := meter.Int64Counter("db.client.operation.count")
	if err != nil {
		return nil, err
	}
	dbDuration, err := meter.Float64Histogram("db.client.operation.duration")
	if err != nil {
		return nil, err
	}
	cronCount, err := meter.Int64Counter("cron.job.run.count")
	if err != nil {
		return nil, err
	}
	cronDuration, err := meter.Float64Histogram("cron.job.duration")
	if err != nil {
		return nil, err
	}
	bidCount, err := meter.Int64Counter("auction.bid.count")
	if err != nil {
		return nil, err
	}
	bidAmount, err := meter.Int64Histogram("auction.bid.amount")
	if err != nil {
		return nil, err
	}
	bidDuration, err := meter.Float64Histogram("auction.bid.duration")
	if err != nil {
		return nil, err
	}
	orderCount, err := meter.Int64Counter("order.auction_create.count")
	if err != nil {
		return nil, err
	}
	return &OTelRecorder{
		httpCount:        httpCount,
		httpDuration:     httpDuration,
		redisLuaCount:    redisLuaCount,
		redisLuaDuration: redisLuaDuration,
		dbCount:          dbCount,
		dbDuration:       dbDuration,
		cronCount:        cronCount,
		cronDuration:     cronDuration,
		bidCount:         bidCount,
		bidAmount:        bidAmount,
		bidDuration:      bidDuration,
		orderCount:       orderCount,
	}, nil
}

func (r *OTelRecorder) HTTPRequest(ctx context.Context, m HTTPRequestMetric) {
	opts := metric.WithAttributes(
		attribute.String("http.route", m.Route),
		attribute.String("http.method", m.Method),
		attribute.Int("http.status_code", m.Status),
	)
	r.httpCount.Add(ctx, 1, opts)
	r.httpDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) RedisLua(ctx context.Context, m RedisLuaMetric) {
	opts := metric.WithAttributes(attribute.String("code", m.Code))
	r.redisLuaCount.Add(ctx, 1, opts)
	r.redisLuaDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) DBQuery(ctx context.Context, m DBQueryMetric) {
	opts := metric.WithAttributes(
		attribute.String("db.operation", m.Operation),
		attribute.String("db.sql.table", m.Table),
		attribute.String("result", m.Result),
		attribute.Bool("slow", m.Slow),
	)
	r.dbCount.Add(ctx, 1, opts)
	r.dbDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) Cron(ctx context.Context, m CronMetric) {
	opts := metric.WithAttributes(attribute.String("job", m.Name), attribute.String("result", m.Result))
	r.cronCount.Add(ctx, 1, opts)
	r.cronDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) Bid(ctx context.Context, m BidMetric) {
	opts := metric.WithAttributes(attribute.String("result", m.Result), attribute.String("reason", SafeReason(m.Reason)))
	r.bidCount.Add(ctx, 1, opts)
	r.bidAmount.Record(ctx, m.Amount, opts)
	r.bidDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) OrderAuctionCreate(ctx context.Context, m OrderMetric) {
	r.orderCount.Add(ctx, 1, metric.WithAttributes(attribute.String("result", m.Result)))
}

func SafeReason(reason string) string {
	if reason == "" {
		return "none"
	}
	return reason
}
