package observability

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

func TestNoopRecorderAcceptsWSEventBusMetric(t *testing.T) {
	var rec Recorder = NoopRecorder{}
	rec.WSEventBus(context.Background(), WSEventBusMetric{
		Action:    "publish",
		Result:    "success",
		Scope:     "room",
		EventType: "bid_success",
	})
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

func TestCronMetricsAvoidPrometheusReservedJobLabel(t *testing.T) {
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
	rec.Cron(ctx, CronMetric{Name: "auction.settle_due", Result: "success", Duration: 10 * time.Millisecond})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	for _, name := range []string{"cron.job.run.count", "cron.job.duration"} {
		attrs := metricAttributes(t, rm, name)
		if got := attrs["cron_job"]; got != "auction.settle_due" {
			t.Fatalf("%s cron_job attribute = %q, want auction.settle_due", name, got)
		}
		if _, ok := attrs["job"]; ok {
			t.Fatalf("%s should not use reserved Prometheus job attribute", name)
		}
	}
}

func TestWSConnectionLifecycleMetricsIncludeStreamAndResult(t *testing.T) {
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
	rec.WSConnectionLifecycle(ctx, WSConnectionLifecycleMetric{
		Stream: "control",
		Result: "accepted",
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	attrs := metricAttributes(t, rm, "ws_connection_lifecycle")
	if attrs["stream"] == "" || attrs["result"] == "" {
		t.Fatalf("expected stream and result attrs, got %#v", attrs)
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

func metricAttributes(t *testing.T, rm metricdata.ResourceMetrics, name string) map[string]string {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				if len(data.DataPoints) != 1 {
					t.Fatalf("%s datapoints = %d, want 1", name, len(data.DataPoints))
				}
				return attributesMap(data.DataPoints[0].Attributes.ToSlice())
			case metricdata.Histogram[float64]:
				if len(data.DataPoints) != 1 {
					t.Fatalf("%s datapoints = %d, want 1", name, len(data.DataPoints))
				}
				return attributesMap(data.DataPoints[0].Attributes.ToSlice())
			default:
				t.Fatalf("%s data type = %T, want Sum[int64] or Histogram[float64]", name, m.Data)
			}
		}
	}
	t.Fatalf("metric %s not found in collected data", name)
	return nil
}

func attributesMap(attrs []attribute.KeyValue) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		out[string(attr.Key)] = attr.Value.AsString()
	}
	return out
}

type captureRecorder struct {
	NoopRecorder
	http HTTPRequestMetric
}

func (r *captureRecorder) HTTPRequest(_ context.Context, m HTTPRequestMetric) {
	r.http = m
}
