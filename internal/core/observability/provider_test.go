package observability

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/config"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSetupDisabledReturnsNoopShutdown(t *testing.T) {
	shutdown, err := Setup(context.Background(), config.Observability{Enabled: false})
	if err != nil {
		t.Fatalf("Setup disabled returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown is nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func TestNormalizeConfigDefaults(t *testing.T) {
	cfg := NormalizeConfig(config.Observability{})
	if cfg.ServiceName != "live-auction-backend" {
		t.Fatalf("service name = %q", cfg.ServiceName)
	}
	if cfg.Environment != "local" {
		t.Fatalf("environment = %q", cfg.Environment)
	}
	if cfg.OTLPEndpoint != "127.0.0.1:4317" {
		t.Fatalf("endpoint = %q", cfg.OTLPEndpoint)
	}
	if cfg.TraceSampleRatio != 1 {
		t.Fatalf("sample ratio = %v", cfg.TraceSampleRatio)
	}
	if metricsInterval(cfg) != 15*time.Second {
		t.Fatalf("metrics interval = %v, want 15s", metricsInterval(cfg))
	}
}

func TestResourceMergesWithDefaultResource(t *testing.T) {
	res, err := newResource(config.Observability{
		ServiceName:    "live-auction-backend",
		ServiceVersion: "0.1.0",
		Environment:    "local",
	})
	if err != nil {
		t.Fatalf("newResource returned error: %v", err)
	}
	if res == nil {
		t.Fatal("resource is nil")
	}
}

func TestMeterProviderUsesSubSecondDurationBuckets(t *testing.T) {
	reader := metric.NewManualReader()
	mp := newMeterProvider(nil, reader)

	for _, name := range []string{
		"http.server.request.duration",
		"db.client.operation.duration",
	} {
		t.Run(name, func(t *testing.T) {
			histogram, err := mp.Meter("test").Float64Histogram(name)
			if err != nil {
				t.Fatalf("Float64Histogram returned error: %v", err)
			}
			histogram.Record(context.Background(), 0.012)

			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect returned error: %v", err)
			}
			got := histogramBounds(t, rm, name)
			want := durationHistogramBoundaries()
			if !slices.Equal(got, want) {
				t.Fatalf("bounds = %v, want %v", got, want)
			}
		})
	}
}

func TestRuntimeMetricsCollectsDBPoolStats(t *testing.T) {
	reader := metric.NewManualReader()
	mp := newMeterProvider(nil, reader)
	cleanup, err := RegisterRuntimeMetrics(mp, fakeDBStatsProvider{})
	if err != nil {
		t.Fatalf("RegisterRuntimeMetrics returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Fatalf("cleanup returned error: %v", err)
		}
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if got := int64GaugeValue(t, rm, "db.client.connections.open"); got != 7 {
		t.Fatalf("open connections = %d, want 7", got)
	}
	if got := int64GaugeValue(t, rm, "db.client.connections.idle"); got != 2 {
		t.Fatalf("idle connections = %d, want 2", got)
	}
	if got := int64GaugeValue(t, rm, "db.client.connections.in_use"); got != 5 {
		t.Fatalf("in-use connections = %d, want 5", got)
	}
}

func histogramBounds(t *testing.T, rm metricdata.ResourceMetrics, name string) []float64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			data, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Histogram[float64]", name, m.Data)
			}
			if len(data.DataPoints) != 1 {
				t.Fatalf("%s datapoints = %d, want 1", name, len(data.DataPoints))
			}
			return data.DataPoints[0].Bounds
		}
	}
	t.Fatalf("metric %s not found in collected data", name)
	return nil
}

func int64GaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			data, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Gauge[int64]", name, m.Data)
			}
			if len(data.DataPoints) != 1 {
				t.Fatalf("%s datapoints = %d, want 1", name, len(data.DataPoints))
			}
			return data.DataPoints[0].Value
		}
	}
	t.Fatalf("metric %s not found in collected data", name)
	return 0
}

type fakeDBStatsProvider struct{}

func (fakeDBStatsProvider) Stats() DBStats {
	return DBStats{
		OpenConnections: 7,
		InUse:           5,
		Idle:            2,
		WaitCount:       11,
		WaitDuration:    120 * time.Millisecond,
		MaxOpen:         20,
	}
}
