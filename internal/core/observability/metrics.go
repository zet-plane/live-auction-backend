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
	BidBroadcast(context.Context, BidBroadcastMetric)
	BidHotState(context.Context, BidHotStateMetric)
	BidLogStream(context.Context, BidLogStreamMetric)
	BidLogWorker(context.Context, BidLogWorkerMetric)
	WSConnection(context.Context, WSConnectionMetric)
	WSConnectionLifecycle(context.Context, WSConnectionLifecycleMetric)
	WSBroadcast(context.Context, WSBroadcastMetric)
	WSDelivery(context.Context, WSDeliveryMetric)
	WSWrite(context.Context, WSWriteMetric)
	WSTimeSync(context.Context, WSTimeSyncMetric)
	WSEventBus(context.Context, WSEventBusMetric)
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

type BidBroadcastMetric struct {
	Action    string
	Result    string
	EventType string
	Bids      int64
	Pending   int64
	Duration  time.Duration
}

type BidHotStateMetric struct {
	Result   string
	Duration time.Duration
}

type BidLogStreamMetric struct {
	Result   string
	Duration time.Duration
}

type BidLogWorkerMetric struct {
	Result    string
	BatchSize int64
	Duration  time.Duration
}

type WSConnectionMetric struct {
	Action      string
	Result      string
	Reason      string
	ActiveDelta int64
}

type WSConnectionLifecycleMetric struct {
	Stream string
	Result string
	Reason string
}

type WSBroadcastMetric struct {
	Mode       string
	Result     string
	EventType  string
	Recipients int64
	Duration   time.Duration
}

type WSDeliveryMetric struct {
	Result    string
	Reason    string
	EventType string
	QueueLen  int64
	QueueCap  int64
	Duration  time.Duration
}

type WSWriteMetric struct {
	Result    string
	Reason    string
	EventType string
	QueueLen  int64
	QueueCap  int64
	Duration  time.Duration
}

type WSTimeSyncMetric struct {
	Action   string
	Result   string
	WriteLag time.Duration
}

type WSEventBusMetric struct {
	Action    string
	Result    string
	Scope     string
	EventType string
}

type OrderMetric struct {
	Result string
}

type NoopRecorder struct{}

func (NoopRecorder) HTTPRequest(context.Context, HTTPRequestMetric)                     {}
func (NoopRecorder) RedisLua(context.Context, RedisLuaMetric)                           {}
func (NoopRecorder) DBQuery(context.Context, DBQueryMetric)                             {}
func (NoopRecorder) Cron(context.Context, CronMetric)                                   {}
func (NoopRecorder) Bid(context.Context, BidMetric)                                     {}
func (NoopRecorder) BidBroadcast(context.Context, BidBroadcastMetric)                   {}
func (NoopRecorder) BidHotState(context.Context, BidHotStateMetric)                     {}
func (NoopRecorder) BidLogStream(context.Context, BidLogStreamMetric)                   {}
func (NoopRecorder) BidLogWorker(context.Context, BidLogWorkerMetric)                   {}
func (NoopRecorder) WSConnection(context.Context, WSConnectionMetric)                   {}
func (NoopRecorder) WSConnectionLifecycle(context.Context, WSConnectionLifecycleMetric) {}
func (NoopRecorder) WSBroadcast(context.Context, WSBroadcastMetric)                     {}
func (NoopRecorder) WSDelivery(context.Context, WSDeliveryMetric)                       {}
func (NoopRecorder) WSWrite(context.Context, WSWriteMetric)                             {}
func (NoopRecorder) WSTimeSync(context.Context, WSTimeSyncMetric)                       {}
func (NoopRecorder) WSEventBus(context.Context, WSEventBusMetric)                       {}
func (NoopRecorder) OrderAuctionCreate(context.Context, OrderMetric)                    {}

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
	httpCount             metric.Int64Counter
	httpDuration          metric.Float64Histogram
	redisLuaCount         metric.Int64Counter
	redisLuaDuration      metric.Float64Histogram
	dbCount               metric.Int64Counter
	dbDuration            metric.Float64Histogram
	cronCount             metric.Int64Counter
	cronDuration          metric.Float64Histogram
	bidCount              metric.Int64Counter
	bidAmount             metric.Int64Histogram
	bidDuration           metric.Float64Histogram
	bidBroadcastCount     metric.Int64Counter
	bidBroadcastBids      metric.Int64Histogram
	bidBroadcastPending   metric.Int64Histogram
	bidBroadcastDuration  metric.Float64Histogram
	bidHotStateCount      metric.Int64Counter
	bidHotStateDuration   metric.Float64Histogram
	bidLogStreamCount     metric.Int64Counter
	bidLogStreamDuration  metric.Float64Histogram
	bidLogWorkerCount     metric.Int64Counter
	bidLogWorkerBatchSize metric.Int64Histogram
	bidLogWorkerDuration  metric.Float64Histogram
	wsConnectionCount     metric.Int64Counter
	wsConnectionActive    metric.Int64UpDownCounter
	wsConnectionLifecycle metric.Int64Counter
	wsBroadcastCount      metric.Int64Counter
	wsBroadcastTargets    metric.Int64Histogram
	wsBroadcastDuration   metric.Float64Histogram
	wsDeliveryCount       metric.Int64Counter
	wsDeliveryDuration    metric.Float64Histogram
	wsWriteCount          metric.Int64Counter
	wsWriteDuration       metric.Float64Histogram
	wsSendQueueDepth      metric.Int64Histogram
	wsTimeSyncCount       metric.Int64Counter
	wsTimeSyncWriteLag    metric.Float64Histogram
	wsEventBusCount       metric.Int64Counter
	orderCount            metric.Int64Counter
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
	bidBroadcastCount, err := meter.Int64Counter("auction.bid_broadcast.count")
	if err != nil {
		return nil, err
	}
	bidBroadcastBids, err := meter.Int64Histogram("auction.bid_broadcast.bids")
	if err != nil {
		return nil, err
	}
	bidBroadcastPending, err := meter.Int64Histogram("auction.bid_broadcast.pending")
	if err != nil {
		return nil, err
	}
	bidBroadcastDuration, err := meter.Float64Histogram("auction.bid_broadcast.duration")
	if err != nil {
		return nil, err
	}
	bidHotStateCount, err := meter.Int64Counter("auction.hot_state.lookup.count")
	if err != nil {
		return nil, err
	}
	bidHotStateDuration, err := meter.Float64Histogram("auction.hot_state.lookup.duration")
	if err != nil {
		return nil, err
	}
	bidLogStreamCount, err := meter.Int64Counter("auction.bid_log.stream.append.count")
	if err != nil {
		return nil, err
	}
	bidLogStreamDuration, err := meter.Float64Histogram("auction.bid_log.stream.append.duration")
	if err != nil {
		return nil, err
	}
	bidLogWorkerCount, err := meter.Int64Counter("auction.bid_log.worker.batch.count")
	if err != nil {
		return nil, err
	}
	bidLogWorkerBatchSize, err := meter.Int64Histogram("auction.bid_log.worker.batch.size")
	if err != nil {
		return nil, err
	}
	bidLogWorkerDuration, err := meter.Float64Histogram("auction.bid_log.worker.persist.duration")
	if err != nil {
		return nil, err
	}
	wsConnectionCount, err := meter.Int64Counter("ws.connection.count")
	if err != nil {
		return nil, err
	}
	wsConnectionActive, err := meter.Int64UpDownCounter("ws.connection.active")
	if err != nil {
		return nil, err
	}
	wsConnectionLifecycle, err := meter.Int64Counter("ws_connection_lifecycle")
	if err != nil {
		return nil, err
	}
	wsBroadcastCount, err := meter.Int64Counter("ws.broadcast.count")
	if err != nil {
		return nil, err
	}
	wsBroadcastTargets, err := meter.Int64Histogram("ws.broadcast.recipients")
	if err != nil {
		return nil, err
	}
	wsBroadcastDuration, err := meter.Float64Histogram("ws.broadcast.duration")
	if err != nil {
		return nil, err
	}
	wsDeliveryCount, err := meter.Int64Counter("ws.delivery.count")
	if err != nil {
		return nil, err
	}
	wsDeliveryDuration, err := meter.Float64Histogram("ws.delivery.duration")
	if err != nil {
		return nil, err
	}
	wsWriteCount, err := meter.Int64Counter("ws.write.count")
	if err != nil {
		return nil, err
	}
	wsWriteDuration, err := meter.Float64Histogram("ws.write.duration")
	if err != nil {
		return nil, err
	}
	wsSendQueueDepth, err := meter.Int64Histogram("ws.send_queue.depth")
	if err != nil {
		return nil, err
	}
	wsTimeSyncCount, err := meter.Int64Counter("ws.time_sync.count")
	if err != nil {
		return nil, err
	}
	wsTimeSyncWriteLag, err := meter.Float64Histogram("ws.time_sync.write_lag.duration")
	if err != nil {
		return nil, err
	}
	wsEventBusCount, err := meter.Int64Counter("live_auction_ws_event_bus_total")
	if err != nil {
		return nil, err
	}
	orderCount, err := meter.Int64Counter("order.auction_create.count")
	if err != nil {
		return nil, err
	}
	return &OTelRecorder{
		httpCount:             httpCount,
		httpDuration:          httpDuration,
		redisLuaCount:         redisLuaCount,
		redisLuaDuration:      redisLuaDuration,
		dbCount:               dbCount,
		dbDuration:            dbDuration,
		cronCount:             cronCount,
		cronDuration:          cronDuration,
		bidCount:              bidCount,
		bidAmount:             bidAmount,
		bidDuration:           bidDuration,
		bidBroadcastCount:     bidBroadcastCount,
		bidBroadcastBids:      bidBroadcastBids,
		bidBroadcastPending:   bidBroadcastPending,
		bidBroadcastDuration:  bidBroadcastDuration,
		bidHotStateCount:      bidHotStateCount,
		bidHotStateDuration:   bidHotStateDuration,
		bidLogStreamCount:     bidLogStreamCount,
		bidLogStreamDuration:  bidLogStreamDuration,
		bidLogWorkerCount:     bidLogWorkerCount,
		bidLogWorkerBatchSize: bidLogWorkerBatchSize,
		bidLogWorkerDuration:  bidLogWorkerDuration,
		wsConnectionCount:     wsConnectionCount,
		wsConnectionActive:    wsConnectionActive,
		wsConnectionLifecycle: wsConnectionLifecycle,
		wsBroadcastCount:      wsBroadcastCount,
		wsBroadcastTargets:    wsBroadcastTargets,
		wsBroadcastDuration:   wsBroadcastDuration,
		wsDeliveryCount:       wsDeliveryCount,
		wsDeliveryDuration:    wsDeliveryDuration,
		wsWriteCount:          wsWriteCount,
		wsWriteDuration:       wsWriteDuration,
		wsSendQueueDepth:      wsSendQueueDepth,
		wsTimeSyncCount:       wsTimeSyncCount,
		wsTimeSyncWriteLag:    wsTimeSyncWriteLag,
		wsEventBusCount:       wsEventBusCount,
		orderCount:            orderCount,
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
	opts := metric.WithAttributes(attribute.String("cron_job", m.Name), attribute.String("result", m.Result))
	r.cronCount.Add(ctx, 1, opts)
	r.cronDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) Bid(ctx context.Context, m BidMetric) {
	opts := metric.WithAttributes(attribute.String("result", m.Result), attribute.String("reason", SafeReason(m.Reason)))
	r.bidCount.Add(ctx, 1, opts)
	r.bidAmount.Record(ctx, m.Amount, opts)
	r.bidDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) BidBroadcast(ctx context.Context, m BidBroadcastMetric) {
	opts := metric.WithAttributes(
		attribute.String("action", SafeReason(m.Action)),
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("event_type", SafeReason(m.EventType)),
	)
	r.bidBroadcastCount.Add(ctx, 1, opts)
	r.bidBroadcastBids.Record(ctx, m.Bids, opts)
	r.bidBroadcastPending.Record(ctx, m.Pending, opts)
	r.bidBroadcastDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) BidHotState(ctx context.Context, m BidHotStateMetric) {
	opts := metric.WithAttributes(attribute.String("result", SafeReason(m.Result)))
	r.bidHotStateCount.Add(ctx, 1, opts)
	r.bidHotStateDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) BidLogStream(ctx context.Context, m BidLogStreamMetric) {
	opts := metric.WithAttributes(attribute.String("result", SafeReason(m.Result)))
	r.bidLogStreamCount.Add(ctx, 1, opts)
	r.bidLogStreamDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) BidLogWorker(ctx context.Context, m BidLogWorkerMetric) {
	opts := metric.WithAttributes(attribute.String("result", SafeReason(m.Result)))
	r.bidLogWorkerCount.Add(ctx, 1, opts)
	r.bidLogWorkerBatchSize.Record(ctx, m.BatchSize, opts)
	r.bidLogWorkerDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) WSConnection(ctx context.Context, m WSConnectionMetric) {
	opts := metric.WithAttributes(
		attribute.String("action", SafeReason(m.Action)),
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("reason", SafeReason(m.Reason)),
	)
	r.wsConnectionCount.Add(ctx, 1, opts)
	if m.ActiveDelta != 0 {
		r.wsConnectionActive.Add(ctx, m.ActiveDelta, opts)
	}
}

func (r *OTelRecorder) WSConnectionLifecycle(ctx context.Context, m WSConnectionLifecycleMetric) {
	r.wsConnectionLifecycle.Add(ctx, 1, metric.WithAttributes(
		attribute.String("stream", SafeReason(m.Stream)),
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("reason", SafeReason(m.Reason)),
	))
}

func (r *OTelRecorder) WSBroadcast(ctx context.Context, m WSBroadcastMetric) {
	opts := metric.WithAttributes(
		attribute.String("mode", SafeReason(m.Mode)),
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("event_type", SafeReason(m.EventType)),
	)
	r.wsBroadcastCount.Add(ctx, 1, opts)
	r.wsBroadcastTargets.Record(ctx, m.Recipients, opts)
	r.wsBroadcastDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) WSDelivery(ctx context.Context, m WSDeliveryMetric) {
	opts := metric.WithAttributes(
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("reason", SafeReason(m.Reason)),
		attribute.String("event_type", SafeReason(m.EventType)),
	)
	r.wsDeliveryCount.Add(ctx, 1, opts)
	r.wsDeliveryDuration.Record(ctx, m.Duration.Seconds(), opts)
	r.wsSendQueueDepth.Record(ctx, m.QueueLen, opts)
}

func (r *OTelRecorder) WSWrite(ctx context.Context, m WSWriteMetric) {
	opts := metric.WithAttributes(
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("reason", SafeReason(m.Reason)),
		attribute.String("event_type", SafeReason(m.EventType)),
	)
	r.wsWriteCount.Add(ctx, 1, opts)
	r.wsWriteDuration.Record(ctx, m.Duration.Seconds(), opts)
	r.wsSendQueueDepth.Record(ctx, m.QueueLen, opts)
}

func (r *OTelRecorder) WSTimeSync(ctx context.Context, m WSTimeSyncMetric) {
	opts := metric.WithAttributes(
		attribute.String("action", SafeReason(m.Action)),
		attribute.String("result", SafeReason(m.Result)),
	)
	r.wsTimeSyncCount.Add(ctx, 1, opts)
	if m.WriteLag != 0 {
		r.wsTimeSyncWriteLag.Record(ctx, m.WriteLag.Seconds(), opts)
	}
}

func (r *OTelRecorder) WSEventBus(ctx context.Context, m WSEventBusMetric) {
	r.wsEventBusCount.Add(ctx, 1, metric.WithAttributes(
		attribute.String("action", SafeReason(m.Action)),
		attribute.String("result", SafeReason(m.Result)),
		attribute.String("scope", SafeReason(m.Scope)),
		attribute.String("event_type", SafeReason(m.EventType)),
	))
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
