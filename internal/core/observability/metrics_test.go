package observability

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestDefaultRecorderCanBeSetAndReset(t *testing.T) {
	SetDefaultRecorder(nil)
	DefaultRecorder().HTTPRequest(context.Background(), HTTPRequestMetric{
		Route:    "/api/v1/health",
		Method:   "GET",
		Status:   200,
		Duration: time.Millisecond,
	})

	rec := &captureRecorder{}
	SetDefaultRecorder(rec)
	DefaultRecorder().HTTPRequest(context.Background(), HTTPRequestMetric{
		Route:    "/api/v1/items/{item_id}/bids",
		Method:   "POST",
		Status:   201,
		Duration: time.Millisecond,
	})
	if rec.http.Route != "/api/v1/items/{item_id}/bids" {
		t.Fatalf("captured route = %q", rec.http.Route)
	}
}

func TestSafeReasonNormalizesEmpty(t *testing.T) {
	if got := SafeReason(""); got != "none" {
		t.Fatalf("empty reason = %q", got)
	}
	if got := SafeReason("price_too_low"); got != "price_too_low" {
		t.Fatalf("reason = %q", got)
	}
}

func TestBidHotPathMetricsAreRecorded(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	oldProvider := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() {
		otel.SetMeterProvider(oldProvider)
	})

	rec, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder returned error: %v", err)
	}
	rec.BidHotState(ctx, BidHotStateMetric{Result: "hit", Duration: 10 * time.Millisecond})
	rec.BidLogStream(ctx, BidLogStreamMetric{Result: "success", Duration: 20 * time.Millisecond})
	rec.BidLogWorker(ctx, BidLogWorkerMetric{Result: "error", BatchSize: 3, Duration: 30 * time.Millisecond})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	names := collectedMetricNames(rm)
	for _, name := range []string{
		"auction.hot_state.lookup.count",
		"auction.hot_state.lookup.duration",
		"auction.bid_log.stream.append.count",
		"auction.bid_log.stream.append.duration",
		"auction.bid_log.worker.batch.count",
		"auction.bid_log.worker.batch.size",
		"auction.bid_log.worker.persist.duration",
	} {
		if !names[name] {
			t.Fatalf("metric %q was not collected", name)
		}
	}
}

func collectedMetricNames(rm metricdata.ResourceMetrics) map[string]bool {
	names := make(map[string]bool)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	return names
}

type captureRecorder struct {
	NoopRecorder
	http HTTPRequestMetric
}

func (r *captureRecorder) HTTPRequest(_ context.Context, m HTTPRequestMetric) {
	r.http = m
}
