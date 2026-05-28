# Observability Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first observability foundation for live-auction-backend and replace existing `logx.Track` service operation tracking with `observability.Track(ctx, ...)`.

**Architecture:** The backend initializes OpenTelemetry at server startup and exports traces and metrics through OTLP to a local collector. `pkg/logx` remains the low-level zap facade, while `internal/core/observability` owns provider setup, request middleware, metric recorders, trace-aware JSON logging helpers, cron wrappers, and the new business operation tracker that replaces `logx.Track`.

**Tech Stack:** Go 1.23, Flamego, GORM, go-redis, zap, OpenTelemetry Go SDK, OTLP gRPC exporters, Redis OTel hook, Docker Compose, OpenTelemetry Collector, Prometheus, Tempo, Loki, Promtail, Grafana.

---

## Supersedes

This plan supersedes `docs/superpowers/plans/2026-05-25-observability.md`. That older plan instruments business spans directly in service methods; this plan routes business operation tracking through `observability.Track(ctx, ...)` as required by `docs/superpowers/specs/2026-05-25-observability-design.md`.

## File Map

- Modify `go.mod`, `go.sum`: add OpenTelemetry SDK/exporter and Redis instrumentation dependencies.
- Modify `config/vars.go`: add observability config structs.
- Modify `config/config.go`: add `ObservabilityMetricsInterval()`.
- Modify `config.yaml.example`: add local observability defaults.
- Create `internal/core/observability/provider.go`: configure OTel resource, tracer provider, meter provider, propagators, and shutdown.
- Create `internal/core/observability/metrics.go`: define low-cardinality metric types, recorder interface, default recorder, and OTel recorder.
- Create `internal/core/observability/trace.go`: implement `Track(ctx, op, fields...)` replacement for `logx.Track`.
- Create `internal/core/observability/log.go`: expose trace context zap fields and JSON logger config.
- Create `internal/core/observability/http.go`: Flamego middleware for server spans and HTTP metrics.
- Create `internal/core/observability/cron.go`: cron wrapper that creates root spans and metrics.
- Create tests under `internal/core/observability/*_test.go`: provider, recorder, tracker, log helper, HTTP middleware, cron wrapper.
- Modify `pkg/logx/logp.go`: add JSON config helper and optional context-aware logging helpers.
- Modify `cmd/server/server.go`: initialize observability, set default recorder, install HTTP middleware, shut down providers.
- Modify `internal/core/cache/cache.go`: install go-redis tracing and metrics hook.
- Modify `internal/middleware/gormv2/logger.go`: record DB spans/metrics and restrict raw SQL logging by mode.
- Modify `internal/app/*/handler/*.go`: pass `req.Context()` to service methods that now require context.
- Modify `internal/app/*/service/*.go`: replace all `logx.Track` calls with `observability.Track(ctx, ...)`.
- Modify `internal/app/item/cache/bid.go`: record `redis.place_bid_lua` span and Lua result metrics.
- Modify `internal/app/item/init.go`, `internal/app/order/init.go`: wrap cron jobs.
- Modify `docker-compose.yml`: add observability services.
- Create `deploy/observability/*.yaml`: collector, Prometheus, Tempo, Loki, Promtail, Grafana datasources and dashboards.
- Create `docs/observability/local-run.md`: local verification runbook.

---

### Task 1: Dependencies And Config

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `config/vars.go`
- Modify: `config/config.go`
- Modify: `config.yaml.example`
- Test: `config/config_test.go`

- [ ] **Step 1: Add dependencies**

Run:

```bash
rtk go get go.opentelemetry.io/otel@latest
rtk go get go.opentelemetry.io/otel/sdk@latest
rtk go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@latest
rtk go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@latest
rtk go get github.com/redis/go-redis/extra/redisotel/v9@latest
rtk go mod tidy
```

Expected: `go.mod` contains OpenTelemetry packages and `github.com/redis/go-redis/extra/redisotel/v9`.

- [ ] **Step 2: Write config test**

Create or extend `config/config_test.go`:

```go
package config

import (
	"testing"
	"time"
)

func TestObservabilityMetricsInterval(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.ObservabilityMetricsInterval(); got != 15*time.Second {
		t.Fatalf("default interval = %v, want 15s", got)
	}

	cfg.Observability.MetricsInterval = "30s"
	if got := cfg.ObservabilityMetricsInterval(); got != 30*time.Second {
		t.Fatalf("configured interval = %v, want 30s", got)
	}

	cfg.Observability.MetricsInterval = "bad"
	if got := cfg.ObservabilityMetricsInterval(); got != 15*time.Second {
		t.Fatalf("bad interval fallback = %v, want 15s", got)
	}
}
```

- [ ] **Step 3: Run the failing config test**

Run:

```bash
rtk go test ./config -run TestObservabilityMetricsInterval -count=1
```

Expected: FAIL because `Observability` or `ObservabilityMetricsInterval` is not defined.

- [ ] **Step 4: Add config structs and helper**

Modify `config/vars.go`:

```go
type GlobalConfig struct {
	Mode          string        `yaml:"mode"          mapstructure:"mode"`
	App           App           `yaml:"app"           mapstructure:"app"`
	HTTP          HTTP          `yaml:"http"          mapstructure:"http"`
	Database      Database      `yaml:"database"      mapstructure:"database"`
	Redis         Redis         `yaml:"redis"         mapstructure:"redis"`
	Auth          Auth          `yaml:"auth"          mapstructure:"auth"`
	Auction       Auction       `yaml:"auction"       mapstructure:"auction"`
	Observability Observability `yaml:"observability" mapstructure:"observability"`
}

type Observability struct {
	Enabled          bool              `yaml:"enabled"             mapstructure:"enabled"`
	ServiceName      string            `yaml:"service_name"        mapstructure:"service_name"`
	ServiceVersion   string            `yaml:"service_version"     mapstructure:"service_version"`
	Environment      string            `yaml:"environment"         mapstructure:"environment"`
	OTLPEndpoint     string            `yaml:"otlp_endpoint"       mapstructure:"otlp_endpoint"`
	OTLPInsecure     bool              `yaml:"otlp_insecure"       mapstructure:"otlp_insecure"`
	TraceSampleRatio float64           `yaml:"trace_sample_ratio"  mapstructure:"trace_sample_ratio"`
	MetricsInterval  string            `yaml:"metrics_interval"    mapstructure:"metrics_interval"`
	Logs             ObservabilityLogs `yaml:"logs"                mapstructure:"logs"`
}

type ObservabilityLogs struct {
	Format              string `yaml:"format"                mapstructure:"format"`
	Output              string `yaml:"output"                mapstructure:"output"`
	IncludeTraceContext bool   `yaml:"include_trace_context" mapstructure:"include_trace_context"`
}
```

Add to `config/config.go`:

```go
func (c *GlobalConfig) ObservabilityMetricsInterval() time.Duration {
	return parseDuration(c.Observability.MetricsInterval, 15*time.Second)
}
```

- [ ] **Step 5: Add YAML defaults**

Append to `config.yaml.example`:

```yaml
observability:
  enabled: true
  service_name: live-auction-backend
  service_version: 0.1.0
  environment: local
  otlp_endpoint: 127.0.0.1:4317
  otlp_insecure: true
  trace_sample_ratio: 1.0
  metrics_interval: 15s
  logs:
    format: json
    output: stdout
    include_trace_context: true
```

- [ ] **Step 6: Verify and commit**

Run:

```bash
rtk go test ./config -count=1
rtk git add go.mod go.sum config/vars.go config/config.go config/config_test.go config.yaml.example
rtk git commit -m "feat: add observability config"
```

Expected: config tests pass and commit succeeds.

---

### Task 2: Provider And Recorder

**Files:**
- Create: `internal/core/observability/provider.go`
- Create: `internal/core/observability/metrics.go`
- Create: `internal/core/observability/provider_test.go`
- Create: `internal/core/observability/metrics_test.go`

- [ ] **Step 1: Write provider tests**

Create `internal/core/observability/provider_test.go`:

```go
package observability

import (
	"context"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/config"
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
```

- [ ] **Step 2: Write recorder tests**

Create `internal/core/observability/metrics_test.go`:

```go
package observability

import (
	"context"
	"testing"
	"time"
)

func TestDefaultRecorderCanBeSetAndReset(t *testing.T) {
	SetDefaultRecorder(nil)
	DefaultRecorder().HTTPRequest(context.Background(), HTTPRequestMetric{
		Route: "/api/v1/health", Method: "GET", Status: 200, Duration: time.Millisecond,
	})

	rec := &captureRecorder{}
	SetDefaultRecorder(rec)
	DefaultRecorder().HTTPRequest(context.Background(), HTTPRequestMetric{
		Route: "/api/v1/items/{item_id}/bids", Method: "POST", Status: 201, Duration: time.Millisecond,
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

type captureRecorder struct {
	NoopRecorder
	http HTTPRequestMetric
}

func (r *captureRecorder) HTTPRequest(_ context.Context, m HTTPRequestMetric) {
	r.http = m
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
rtk go test ./internal/core/observability -run 'TestSetupDisabled|TestNormalizeConfig|TestDefaultRecorder|TestSafeReason' -count=1
```

Expected: FAIL because `internal/core/observability` does not exist.

- [ ] **Step 4: Create provider**

Create `internal/core/observability/provider.go` with these exported APIs:

```go
package observability

import (
	"context"
	"errors"
	"time"

	"github.com/zet-plane/live-auction-backend/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Shutdown func(context.Context) error

func NormalizeConfig(cfg config.Observability) config.Observability {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "live-auction-backend"
	}
	if cfg.Environment == "" {
		cfg.Environment = "local"
	}
	if cfg.OTLPEndpoint == "" {
		cfg.OTLPEndpoint = "127.0.0.1:4317"
	}
	if cfg.TraceSampleRatio <= 0 || cfg.TraceSampleRatio > 1 {
		cfg.TraceSampleRatio = 1
	}
	if cfg.MetricsInterval == "" {
		cfg.MetricsInterval = "15s"
	}
	return cfg
}

func Setup(ctx context.Context, cfg config.Observability) (Shutdown, error) {
	cfg = NormalizeConfig(cfg)
	if !cfg.Enabled {
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		otel.SetMeterProvider(metric.NewMeterProvider())
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironment(cfg.Environment),
	))
	if err != nil {
		return nil, err
	}

	traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint)}
	metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
		metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
	}

	traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
	if err != nil {
		return nil, err
	}
	metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.TraceSampleRatio)),
		sdktrace.WithBatcher(traceExporter),
	)
	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(metricsInterval(cfg)))),
	)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

func metricsInterval(cfg config.Observability) time.Duration {
	d, err := time.ParseDuration(cfg.MetricsInterval)
	if err != nil || d <= 0 {
		return 15 * time.Second
	}
	return d
}
```

- [ ] **Step 5: Create recorder**

Create `internal/core/observability/metrics.go` with these types and methods:

```go
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
	Route string
	Method string
	Status int
	Duration time.Duration
}

type RedisLuaMetric struct {
	Code string
	Duration time.Duration
}

type DBQueryMetric struct {
	Operation string
	Table string
	Result string
	Slow bool
	Duration time.Duration
}

type CronMetric struct {
	Name string
	Result string
	Duration time.Duration
}

type BidMetric struct {
	Result string
	Reason string
	Amount int64
	Duration time.Duration
}

type OrderMetric struct {
	Result string
}

type NoopRecorder struct{}

func (NoopRecorder) HTTPRequest(context.Context, HTTPRequestMetric) {}
func (NoopRecorder) RedisLua(context.Context, RedisLuaMetric) {}
func (NoopRecorder) DBQuery(context.Context, DBQueryMetric) {}
func (NoopRecorder) Cron(context.Context, CronMetric) {}
func (NoopRecorder) Bid(context.Context, BidMetric) {}
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
	httpCount metric.Int64Counter
	httpDuration metric.Float64Histogram
	redisLuaCount metric.Int64Counter
	redisLuaDuration metric.Float64Histogram
	dbCount metric.Int64Counter
	dbDuration metric.Float64Histogram
	cronCount metric.Int64Counter
	cronDuration metric.Float64Histogram
	bidCount metric.Int64Counter
	bidAmount metric.Int64Histogram
	bidDuration metric.Float64Histogram
	orderCount metric.Int64Counter
}

func NewRecorder() (*OTelRecorder, error) {
	meter := otel.Meter("github.com/zet-plane/live-auction-backend")
	httpCount, err := meter.Int64Counter("http.server.request.count")
	if err != nil { return nil, err }
	httpDuration, err := meter.Float64Histogram("http.server.request.duration")
	if err != nil { return nil, err }
	redisLuaCount, err := meter.Int64Counter("auction.place_bid.lua.result.count")
	if err != nil { return nil, err }
	redisLuaDuration, err := meter.Float64Histogram("auction.place_bid.lua.duration")
	if err != nil { return nil, err }
	dbCount, err := meter.Int64Counter("db.client.operation.count")
	if err != nil { return nil, err }
	dbDuration, err := meter.Float64Histogram("db.client.operation.duration")
	if err != nil { return nil, err }
	cronCount, err := meter.Int64Counter("cron.job.run.count")
	if err != nil { return nil, err }
	cronDuration, err := meter.Float64Histogram("cron.job.duration")
	if err != nil { return nil, err }
	bidCount, err := meter.Int64Counter("auction.bid.count")
	if err != nil { return nil, err }
	bidAmount, err := meter.Int64Histogram("auction.bid.amount")
	if err != nil { return nil, err }
	bidDuration, err := meter.Float64Histogram("auction.bid.duration")
	if err != nil { return nil, err }
	orderCount, err := meter.Int64Counter("order.auction_create.count")
	if err != nil { return nil, err }
	return &OTelRecorder{httpCount: httpCount, httpDuration: httpDuration, redisLuaCount: redisLuaCount, redisLuaDuration: redisLuaDuration, dbCount: dbCount, dbDuration: dbDuration, cronCount: cronCount, cronDuration: cronDuration, bidCount: bidCount, bidAmount: bidAmount, bidDuration: bidDuration, orderCount: orderCount}, nil
}

func (r *OTelRecorder) HTTPRequest(ctx context.Context, m HTTPRequestMetric) {
	opts := metric.WithAttributes(attribute.String("http.route", m.Route), attribute.String("http.method", m.Method), attribute.Int("http.status_code", m.Status))
	r.httpCount.Add(ctx, 1, opts)
	r.httpDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) RedisLua(ctx context.Context, m RedisLuaMetric) {
	opts := metric.WithAttributes(attribute.String("code", m.Code))
	r.redisLuaCount.Add(ctx, 1, opts)
	r.redisLuaDuration.Record(ctx, m.Duration.Seconds(), opts)
}

func (r *OTelRecorder) DBQuery(ctx context.Context, m DBQueryMetric) {
	opts := metric.WithAttributes(attribute.String("db.operation", m.Operation), attribute.String("db.sql.table", m.Table), attribute.String("result", m.Result), attribute.Bool("slow", m.Slow))
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
```

- [ ] **Step 6: Verify and commit**

Run:

```bash
rtk go test ./internal/core/observability -count=1
rtk git add internal/core/observability/provider.go internal/core/observability/metrics.go internal/core/observability/provider_test.go internal/core/observability/metrics_test.go
rtk git commit -m "feat: add observability provider and recorder"
```

Expected: observability tests pass and commit succeeds.

---

### Task 3: Business Operation Tracker

**Files:**
- Create: `internal/core/observability/trace.go`
- Create: `internal/core/observability/trace_test.go`
- Create: `internal/core/observability/log.go`
- Create: `internal/core/observability/log_test.go`
- Modify: `pkg/logx/logp.go`

- [ ] **Step 1: Write tracker tests**

Create `internal/core/observability/trace_test.go`:

```go
package observability

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTrackCreatesSpanAndRecordsBidMetric(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(old)

	rec := &trackCaptureRecorder{}
	SetDefaultRecorder(rec)
	defer SetDefaultRecorder(nil)

	var err error
	finish := Track(context.Background(), "auction.place_bid", "item_id", "item_1", "amount", int64(1100))
	finish(&err, "result", "success", "reason", "accepted")

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count = %d, want 1", len(spans))
	}
	if spans[0].Name != "auction.place_bid" {
		t.Fatalf("span name = %q", spans[0].Name)
	}
	if rec.bid.Result != "success" || rec.bid.Reason != "accepted" || rec.bid.Amount != 1100 {
		t.Fatalf("bid metric = %+v", rec.bid)
	}
}

func TestTrackMarksError(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(old)

	err := errors.New("boom")
	finish := Track(context.Background(), "room.start", "room_id", "room_1")
	finish(&err, "result", "error", "reason", "internal")

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count = %d, want 1", len(spans))
	}
	if len(spans[0].Events) == 0 {
		t.Fatal("expected recorded error event")
	}
}

type trackCaptureRecorder struct {
	NoopRecorder
	bid BidMetric
}

func (r *trackCaptureRecorder) Bid(_ context.Context, m BidMetric) {
	r.bid = m
}
```

- [ ] **Step 2: Write log helper tests**

Create `internal/core/observability/log_test.go`:

```go
package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceFieldsWithoutSpan(t *testing.T) {
	if fields := TraceFields(context.Background()); len(fields) != 0 {
		t.Fatalf("fields = %v, want empty", fields)
	}
}

func TestTraceFieldsWithSpanContext(t *testing.T) {
	tid, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	sid, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled}))
	fields := TraceFields(ctx)
	if len(fields) != 2 {
		t.Fatalf("fields len = %d, want 2", len(fields))
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
rtk go test ./internal/core/observability -run 'TestTrack|TestTraceFields' -count=1
```

Expected: FAIL because `Track` and `TraceFields` are not implemented.

- [ ] **Step 4: Implement trace helper**

Create `internal/core/observability/trace.go`:

```go
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
			case int64:
				return v
			case int:
				return int64(v)
			}
		}
	}
	return fallback
}
```

- [ ] **Step 5: Implement log helper and JSON config**

Create `internal/core/observability/log.go`:

```go
package observability

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func TraceFields(ctx context.Context) []zap.Field {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	}
}
```

Add to `pkg/logx/logp.go`:

```go
func JSONConfig() *zap.Config {
	cfg := defaultConfig()
	cfg.Encoding = "json"
	cfg.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	return cfg
}
```

- [ ] **Step 6: Verify and commit**

Run:

```bash
rtk go test ./internal/core/observability ./pkg/logx -count=1
rtk git add internal/core/observability/trace.go internal/core/observability/trace_test.go internal/core/observability/log.go internal/core/observability/log_test.go pkg/logx/logp.go
rtk git commit -m "feat: replace business tracking foundation"
```

Expected: tests pass and commit succeeds.

---

### Task 4: HTTP Middleware And Server Wiring

**Files:**
- Create: `internal/core/observability/http.go`
- Create: `internal/core/observability/http_test.go`
- Modify: `cmd/server/server.go`

- [ ] **Step 1: Write HTTP middleware test**

Create `internal/core/observability/http_test.go`:

```go
package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flamego/flamego"
)

type httpCaptureRecorder struct {
	NoopRecorder
	http HTTPRequestMetric
}

func (r *httpCaptureRecorder) HTTPRequest(_ context.Context, m HTTPRequestMetric) {
	r.http = m
}

func TestHTTPMiddlewareRecordsRequest(t *testing.T) {
	rec := &httpCaptureRecorder{}
	f := flamego.New()
	f.Use(HTTPMiddleware(rec))
	f.Get("/api/v1/health", func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)

	if rec.http.Route != "/api/v1/health" {
		t.Fatalf("route = %q", rec.http.Route)
	}
	if rec.http.Method != http.MethodGet {
		t.Fatalf("method = %q", rec.http.Method)
	}
	if rec.http.Status != http.StatusAccepted {
		t.Fatalf("status = %d", rec.http.Status)
	}
	if rec.http.Duration <= 0 || rec.http.Duration > time.Minute {
		t.Fatalf("duration = %v", rec.http.Duration)
	}
}
```

- [ ] **Step 2: Run failing test**

Run:

```bash
rtk go test ./internal/core/observability -run TestHTTPMiddlewareRecordsRequest -count=1
```

Expected: FAIL because `HTTPMiddleware` is not implemented.

- [ ] **Step 3: Implement HTTP middleware**

Create `internal/core/observability/http.go`:

```go
package observability

import (
	"net/http"
	"time"

	"github.com/flamego/flamego"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func HTTPMiddleware(rec Recorder) flamego.Handler {
	if rec == nil {
		rec = NoopRecorder{}
	}
	tracer := otel.Tracer("github.com/zet-plane/live-auction-backend/http")
	return func(c flamego.Context, req *http.Request) {
		route := routePattern(req)
		ctx, span := tracer.Start(req.Context(), req.Method+" "+route)
		*req = *req.WithContext(ctx)
		start := time.Now()

		c.Next()

		status := c.ResponseWriter().Status()
		duration := time.Since(start)
		span.SetAttributes(attribute.String("http.method", req.Method), attribute.String("http.route", route), attribute.Int("http.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		rec.HTTPRequest(ctx, HTTPRequestMetric{Route: route, Method: req.Method, Status: status, Duration: duration})
		span.End()
	}
}

func routePattern(req *http.Request) string {
	if req == nil || req.URL == nil {
		return "unknown"
	}
	return req.URL.Path
}
```

- [ ] **Step 4: Wire server startup**

Modify `cmd/server/server.go`:

```go
config.LoadConfig(configPath)
cfg := config.GetConfig()
if cfg.Observability.Logs.Format == "json" {
	logx.SetUp(logx.WithZapConfig(logx.JSONConfig()))
} else {
	logx.SetUp()
}
```

In `Run`, initialize provider and recorder before DB/cache setup:

```go
shutdown, err := observability.Setup(context.Background(), cfg.Observability)
if err != nil {
	logx.Errorf("observability setup failed: %v", err)
	shutdown = func(context.Context) error { return nil }
}
defer func() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		logx.Errorf("observability shutdown failed: %v", err)
	}
}()

rec, err := observability.NewRecorder()
if err != nil {
	logx.Errorf("observability recorder setup failed: %v", err)
	observability.SetDefaultRecorder(nil)
} else {
	observability.SetDefaultRecorder(rec)
}
```

Add middleware to `buildEngine` after recovery:

```go
observability.HTTPMiddleware(observability.DefaultRecorder()),
gw.RequestLog(),
```

- [ ] **Step 5: Verify and commit**

Run:

```bash
rtk go test ./internal/core/observability ./cmd/server -count=1
rtk git add internal/core/observability/http.go internal/core/observability/http_test.go cmd/server/server.go
rtk git commit -m "feat: wire http observability"
```

Expected: tests pass and commit succeeds.

---

### Task 5: Context Propagation And `logx.Track` Replacement

**Files:**
- Modify: `internal/app/deposit/service/service.go`
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/order/service/service.go`
- Modify: `internal/app/order/service/cron.go`
- Modify: `internal/app/room/service/service.go`
- Modify: `internal/app/user/service/service.go`
- Modify: handlers under `internal/app/*/handler/*.go`
- Modify: service tests under `internal/app/*/service/*_test.go`

- [ ] **Step 1: List current `logx.Track` call sites**

Run:

```bash
rtk rg -n "logx\\.Track" internal/app
```

Expected: output includes service methods in user, room, deposit, item, and order packages.

- [ ] **Step 2: Change public service signatures to accept context**

Apply these signature changes:

```go
func (s *Service) Register(ctx context.Context, input dto.RegisterInput) (result *dto.LoginResult, err error)
func (s *Service) Login(ctx context.Context, account string, password string) (result *dto.LoginResult, err error)
func (s *Service) Authenticate(ctx context.Context, token string) (result *model.User, err error)
func (s *Service) UpdateProfile(ctx context.Context, u *model.User, input dto.UpdateProfileInput) (err error)
func (s *Service) DeleteMe(ctx context.Context, u *model.User) (err error)
func (s *Service) ActivateRoom(ctx context.Context, current *usermodel.User, input dto.CreateRoomInput) (result *dto.MerchantRoomDTO, err error)
func (s *Service) GetMerchantRoom(ctx context.Context, current *usermodel.User) (result *dto.MerchantRoomDTO, err error)
func (s *Service) StartRoom(ctx context.Context, current *usermodel.User, roomID string) (err error)
func (s *Service) EndRoom(ctx context.Context, current *usermodel.User, roomID string) (err error)
func (s *Service) GetRoom(ctx context.Context, roomID string) (result *dto.RoomDetailDTO, err error)
func (s *Service) ListRooms(ctx context.Context, statusFilter model.RoomStatus) (result []*dto.RoomDetailDTO, err error)
func (s *Service) PayDeposit(ctx context.Context, current *usermodel.User, itemID string) (result *dto.DepositDetail, err error)
func (s *Service) GetMyDeposit(ctx context.Context, current *usermodel.User, itemID string) (result *dto.DepositDetail, err error)
func (s *Service) HasPaidDeposit(ctx context.Context, itemID, userID string, requiredAmount int64) (ok bool, err error)
func (s *Service) CreateItem(ctx context.Context, current *usermodel.User, input dto.CreateItemInput) (result *dto.CreateItemResult, err error)
func (s *Service) ListItems(ctx context.Context, query dto.ListItemsInput) (result *dto.ItemListResult, err error)
func (s *Service) ListMerchantItems(ctx context.Context, current *usermodel.User, query dto.ListItemsInput) (result *dto.MerchantItemListResult, err error)
func (s *Service) GetItem(ctx context.Context, itemID string) (result *dto.ItemDetailDTO, err error)
func (s *Service) UpdateItem(ctx context.Context, current *usermodel.User, itemID string, input dto.CreateItemInput) (err error)
func (s *Service) DeleteItem(ctx context.Context, current *usermodel.User, itemID string) (err error)
func (s *Service) PublishItem(ctx context.Context, current *usermodel.User, itemID string) (err error)
func (s *Service) StartItem(ctx context.Context, current *usermodel.User, itemID string) (err error)
func (s *Service) CancelItem(ctx context.Context, current *usermodel.User, itemID string) (err error)
func (s *Service) EndExpiredAuctions(ctx context.Context)
func (s *Service) PlaceBid(ctx context.Context, current *usermodel.User, itemID string, input dto.PlaceBidInput) (result *dto.PlaceBidResult, err error)
func (s *Service) GetRanking(ctx context.Context, itemID string, page, pageSize int) (result *dto.RankingResult, err error)
func (s *Service) CreateOrder(ctx context.Context, itemID, userID string, price int64) (result *model.Order, err error)
func (s *Service) Pay(ctx context.Context, current *usermodel.User, orderID string) (err error)
func (s *Service) Cancel(ctx context.Context, current *usermodel.User, orderID string) (err error)
func (s *Service) ListOrders(ctx context.Context, current *usermodel.User, input dto.ListOrdersInput) (result *dto.ListOrdersResult, err error)
func (s *Service) GetOrder(ctx context.Context, current *usermodel.User, orderID string) (result *dto.OrderDetail, err error)
func (s *Service) ScanExpiredOrders(ctx context.Context)
func (s *Service) ScanCompensation(ctx context.Context)
```

- [ ] **Step 3: Update handlers to pass request context**

In each handler that has `req *http.Request` available, pass `req.Context()` to the service. For handlers that do not currently receive `*http.Request`, add it to the Flamego handler signature.

Example for `internal/app/item/handler/bid.go`:

```go
func PlaceBid(r flamego.Render, req *http.Request, c flamego.Context, current *usermodel.User, body dto.PlaceBidRequest, errs binding.Errors) {
	if web.BindingErrors(r, errs) {
		return
	}
	result, err := svc.PlaceBid(req.Context(), current, c.Param("item_id"), body.Input(current.Name))
	if err != nil {
		logx.Warnw("PlaceBid failed", "user_id", current.ID, "item_id", c.Param("item_id"), "price", body.Price, "err", err)
		response.Error(r, err)
		return
	}
	response.OK(r, result)
}
```

- [ ] **Step 4: Replace `logx.Track` calls**

Use this mapping:

```text
user.Register                    -> user.register
user.Login                       -> user.login
user.Authenticate                -> user.authenticate
user.UpdateProfile               -> user.update_profile
user.DeleteMe                    -> user.delete_me
room.ActivateRoom                -> room.activate
room.GetMerchantRoom             -> room.get_merchant_room
room.StartRoom                   -> room.start
room.EndRoom                     -> room.end
room.GetRoom                     -> room.get
room.ListRooms                   -> room.list
deposit.PayDeposit               -> deposit.pay
deposit.GetMyDeposit             -> deposit.get_my
deposit.HasPaidDeposit           -> deposit.check
item.CreateItem                  -> item.create
item.ListItems                   -> item.list
item.ListMerchantItems           -> item.list_merchant
item.GetItem                     -> item.get
item.UpdateItem                  -> item.update
item.DeleteItem                  -> item.delete
item.PublishItem                 -> item.publish
item.StartItem                   -> item.start
item.CancelItem                  -> item.cancel
item.EndExpiredAuctions          -> auction.end_expired
item.PlaceBid                    -> auction.place_bid
item.GetRanking                  -> auction.get_ranking
order.CreateOrder                -> order.create_from_auction
order.Pay                        -> order.pay
order.Cancel                     -> order.cancel
order.ListOrders                 -> order.list
order.GetOrder                   -> order.get
order.ScanExpiredOrders          -> order.scan_expired
order.ScanCompensation           -> order.compensation_scan
```

Example replacement in `PlaceBid`:

```go
finish := observability.Track(ctx, "auction.place_bid",
	"user_id", userID(current),
	"item_id", itemID,
	"price", input.Price,
)
defer func() {
	finish(&err, "bid_id", bidID, "status", status, "result", bidResult, "reason", bidReason)
}()
```

Use fixed `result` and `reason` values:

```text
result: success | idempotent | rejected | error
reason: accepted | idempotency_key | item_not_ongoing | deposit_required | auction_ended | price_too_low | invalid_bid_increment | internal | redis_error | db_error
```

- [ ] **Step 5: Replace `context.Background()` inside service/cache calls**

In service methods, replace business-operation Redis calls with the propagated `ctx`.

Example:

```go
luaResult, err := s.cache.PlaceBidLua(ctx, item.ID, itemcache.BidLuaArgs{...})
```

Keep tests deterministic by passing `context.Background()` from test call sites.

- [ ] **Step 6: Verify no `logx.Track` remains**

Run:

```bash
rtk rg "logx\\.Track" internal/app pkg
```

Expected: no output.

- [ ] **Step 7: Run service tests and commit**

Run:

```bash
rtk go test ./internal/app/user/service ./internal/app/room/service ./internal/app/deposit/service ./internal/app/item/service ./internal/app/order/service -count=1
rtk git add internal/app pkg
rtk git commit -m "feat: replace service tracking with observability"
```

Expected: service tests pass and commit succeeds.

---

### Task 6: Redis, GORM, And Cron Instrumentation

**Files:**
- Modify: `internal/core/cache/cache.go`
- Modify: `internal/app/item/cache/bid.go`
- Modify: `internal/middleware/gormv2/logger.go`
- Modify: `internal/middleware/gormv2/logger_test.go`
- Create: `internal/core/observability/cron.go`
- Create: `internal/core/observability/cron_test.go`
- Modify: `internal/app/item/init.go`
- Modify: `internal/app/order/init.go`

- [ ] **Step 1: Instrument Redis client**

Modify `internal/core/cache/cache.go`:

```go
import "github.com/redis/go-redis/extra/redisotel/v9"
```

After `redis.NewClient(...)`:

```go
if err := redisotel.InstrumentTracing(client); err != nil {
	return nil, fmt.Errorf("instrument redis tracing: %w", err)
}
if err := redisotel.InstrumentMetrics(client); err != nil {
	return nil, fmt.Errorf("instrument redis metrics: %w", err)
}
```

- [ ] **Step 2: Instrument Lua execution**

In `internal/app/item/cache/bid.go`, wrap `PlaceBidLua` with:

```go
ctx, span := otel.Tracer("github.com/zet-plane/live-auction-backend/redis").Start(ctx, "redis.place_bid_lua")
defer span.End()
start := time.Now()
```

On Redis error:

```go
span.RecordError(err)
span.SetStatus(codes.Error, err.Error())
observability.DefaultRecorder().RedisLua(ctx, observability.RedisLuaMetric{Code: "error", Duration: time.Since(start)})
return nil, err
```

Before returning the parsed `BidLuaResult`:

```go
span.SetAttributes(attribute.String("auction.item_id", itemID), attribute.Int("auction.lua.code", result.Code))
observability.DefaultRecorder().RedisLua(ctx, observability.RedisLuaMetric{Code: strconv.Itoa(result.Code), Duration: time.Since(start)})
return result, nil
```

- [ ] **Step 3: Add GORM SQL parser test**

Extend `internal/middleware/gormv2/logger_test.go`:

```go
func TestSQLMetadata(t *testing.T) {
	cases := []struct {
		sql string
		op string
		table string
	}{
		{"SELECT * FROM auction_items WHERE id = ?", "SELECT", "auction_items"},
		{"INSERT INTO bid_logs (`id`) VALUES (?)", "INSERT", "bid_logs"},
		{"UPDATE orders SET status = ?", "UPDATE", "orders"},
		{"DELETE FROM deposits WHERE id = ?", "DELETE", "deposits"},
	}
	for _, tt := range cases {
		if got := operationFromSQL(tt.sql); got != tt.op {
			t.Fatalf("operationFromSQL(%q) = %q, want %q", tt.sql, got, tt.op)
		}
		if got := tableFromSQL(tt.sql); got != tt.table {
			t.Fatalf("tableFromSQL(%q) = %q, want %q", tt.sql, got, tt.table)
		}
	}
}
```

- [ ] **Step 4: Implement GORM metadata and metrics**

In `internal/middleware/gormv2/logger.go`, update `Trace(ctx context.Context, ...)` to create a span named `mysql.query`, record `db.system=mysql`, `db.operation`, `db.sql.table`, and call:

```go
observability.DefaultRecorder().DBQuery(ctx, observability.DBQueryMetric{
	Operation: operation,
	Table: table,
	Result: result,
	Slow: l.SlowThreshold > 0 && elapsed > l.SlowThreshold,
	Duration: elapsed,
})
```

Add helpers:

```go
func operationFromSQL(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "unknown"
	}
	return strings.ToUpper(fields[0])
}

func tableFromSQL(sql string) string {
	clean := strings.ReplaceAll(sql, "`", "")
	fields := strings.Fields(clean)
	for i, f := range fields {
		upper := strings.ToUpper(f)
		if (upper == "FROM" || upper == "INTO" || upper == "UPDATE") && i+1 < len(fields) {
			return strings.Trim(fields[i+1], ",")
		}
	}
	if len(fields) > 2 && strings.ToUpper(fields[0]) == "DELETE" && strings.ToUpper(fields[1]) == "FROM" {
		return strings.Trim(fields[2], ",")
	}
	return "unknown"
}
```

- [ ] **Step 5: Add cron wrapper**

Create `internal/core/observability/cron.go`:

```go
package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func WrapCron(name string, fn func(context.Context)) func() {
	return func() {
		ctx, span := otel.Tracer("github.com/zet-plane/live-auction-backend/cron").Start(context.Background(), "cron."+name)
		start := time.Now()
		fn(ctx)
		duration := time.Since(start)
		span.SetAttributes(attribute.String("cron.job", name), attribute.String("cron.result", "success"))
		DefaultRecorder().Cron(ctx, CronMetric{Name: name, Result: "success", Duration: duration})
		span.End()
	}
}
```

Create `internal/core/observability/cron_test.go`:

```go
package observability

import (
	"context"
	"testing"
)

func TestWrapCronRecordsSuccess(t *testing.T) {
	rec := &cronCaptureRecorder{}
	SetDefaultRecorder(rec)
	defer SetDefaultRecorder(nil)
	WrapCron("item.end_expired_auctions", func(context.Context) {})()
	if rec.metric.Name != "item.end_expired_auctions" {
		t.Fatalf("name = %q", rec.metric.Name)
	}
	if rec.metric.Result != "success" {
		t.Fatalf("result = %q", rec.metric.Result)
	}
}

type cronCaptureRecorder struct {
	NoopRecorder
	metric CronMetric
}

func (r *cronCaptureRecorder) Cron(_ context.Context, m CronMetric) {
	r.metric = m
}
```

- [ ] **Step 6: Wrap registered cron jobs**

In `internal/app/item/init.go`:

```go
engine.Cron.AddFunc("@every 1m", observability.WrapCron("item.end_expired_auctions", svc.EndExpiredAuctions))
```

In `internal/app/order/init.go`:

```go
engine.Cron.AddFunc("@every 5m", observability.WrapCron("order.scan_expired_orders", Svc.ScanExpiredOrders))
engine.Cron.AddFunc("@every 10m", observability.WrapCron("order.scan_compensation", Svc.ScanCompensation))
```

- [ ] **Step 7: Verify and commit**

Run:

```bash
rtk go test ./internal/core/observability ./internal/core/cache ./internal/app/item/cache ./internal/middleware/gormv2 ./internal/app/item/... ./internal/app/order/... -count=1
rtk git add internal/core/cache/cache.go internal/app/item/cache/bid.go internal/middleware/gormv2/logger.go internal/middleware/gormv2/logger_test.go internal/core/observability/cron.go internal/core/observability/cron_test.go internal/app/item/init.go internal/app/order/init.go
rtk git commit -m "feat: instrument redis gorm and cron"
```

Expected: tests pass and commit succeeds.

---

### Task 7: Local Observability Stack

**Files:**
- Modify: `docker-compose.yml`
- Create: `deploy/observability/otel-collector.yaml`
- Create: `deploy/observability/prometheus.yaml`
- Create: `deploy/observability/tempo.yaml`
- Create: `deploy/observability/loki.yaml`
- Create: `deploy/observability/promtail.yaml`
- Create: `deploy/observability/grafana/datasources/prometheus.yaml`
- Create: `deploy/observability/grafana/datasources/tempo.yaml`
- Create: `deploy/observability/grafana/datasources/loki.yaml`

- [ ] **Step 1: Add collector config**

Create `deploy/observability/otel-collector.yaml`:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

exporters:
  prometheus:
    endpoint: 0.0.0.0:8889
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp/tempo]
    metrics:
      receivers: [otlp]
      exporters: [prometheus]
```

- [ ] **Step 2: Add Prometheus, Tempo, Loki, Promtail configs**

Create `deploy/observability/prometheus.yaml`:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: otel-collector
    static_configs:
      - targets: ["otel-collector:8889"]
```

Create `deploy/observability/tempo.yaml`:

```yaml
server:
  http_listen_port: 3200

distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317

storage:
  trace:
    backend: local
    local:
      path: /tmp/tempo/traces
```

Create `deploy/observability/loki.yaml`:

```yaml
auth_enabled: false
server:
  http_listen_port: 3100
common:
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory
schema_config:
  configs:
    - from: 2024-01-01
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h
```

Create `deploy/observability/promtail.yaml`:

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0
positions:
  filename: /tmp/positions.yaml
clients:
  - url: http://loki:3100/loki/api/v1/push
scrape_configs:
  - job_name: live-auction-backend
    static_configs:
      - targets: [localhost]
        labels:
          service_name: live-auction-backend
          environment: local
          __path__: /var/log/live-auction/*.log
```

- [ ] **Step 3: Add Grafana datasources**

Create datasource files:

```yaml
# deploy/observability/grafana/datasources/prometheus.yaml
apiVersion: 1
datasources:
  - name: Prometheus
    uid: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
```

```yaml
# deploy/observability/grafana/datasources/tempo.yaml
apiVersion: 1
datasources:
  - name: Tempo
    uid: Tempo
    type: tempo
    access: proxy
    url: http://tempo:3200
    jsonData:
      tracesToLogsV2:
        datasourceUid: Loki
        filterByTraceID: true
```

```yaml
# deploy/observability/grafana/datasources/loki.yaml
apiVersion: 1
datasources:
  - name: Loki
    uid: Loki
    type: loki
    access: proxy
    url: http://loki:3100
```

- [ ] **Step 4: Add compose services**

Add services to `docker-compose.yml`:

```yaml
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.103.0
    command: ["--config=/etc/otel-collector.yaml"]
    volumes:
      - ./deploy/observability/otel-collector.yaml:/etc/otel-collector.yaml:ro
    ports:
      - "4317:4317"
      - "4318:4318"
      - "8889:8889"

  prometheus:
    image: prom/prometheus:v2.53.0
    command: ["--config.file=/etc/prometheus/prometheus.yaml"]
    volumes:
      - ./deploy/observability/prometheus.yaml:/etc/prometheus/prometheus.yaml:ro
    ports:
      - "9090:9090"

  tempo:
    image: grafana/tempo:2.5.0
    command: ["-config.file=/etc/tempo.yaml"]
    volumes:
      - ./deploy/observability/tempo.yaml:/etc/tempo.yaml:ro
    ports:
      - "3200:3200"

  loki:
    image: grafana/loki:3.1.0
    command: ["-config.file=/etc/loki.yaml"]
    volumes:
      - ./deploy/observability/loki.yaml:/etc/loki.yaml:ro
    ports:
      - "3100:3100"

  promtail:
    image: grafana/promtail:3.1.0
    command: ["-config.file=/etc/promtail.yaml"]
    volumes:
      - ./deploy/observability/promtail.yaml:/etc/promtail.yaml:ro
      - ./logs:/var/log/live-auction:ro
    depends_on:
      - loki

  grafana:
    image: grafana/grafana:11.1.0
    volumes:
      - ./deploy/observability/grafana/datasources:/etc/grafana/provisioning/datasources:ro
    ports:
      - "3000:3000"
    depends_on:
      - prometheus
      - tempo
      - loki
```

- [ ] **Step 5: Verify and commit**

Run:

```bash
rtk docker compose config
rtk git add docker-compose.yml deploy/observability
rtk git commit -m "chore: add local observability stack"
```

Expected: compose config validates and commit succeeds.

---

### Task 8: Dashboards, Runbook, And Final Verification

**Files:**
- Create: `deploy/observability/grafana/dashboards/dashboard-provider.yaml`
- Create: `deploy/observability/grafana/dashboards/live-auction-overview.json`
- Create: `deploy/observability/grafana/dashboards/live-auction-bidding.json`
- Create: `deploy/observability/grafana/dashboards/live-auction-logs.json`
- Create: `docs/observability/local-run.md`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Add dashboard provider**

Create `deploy/observability/grafana/dashboards/dashboard-provider.yaml`:

```yaml
apiVersion: 1
providers:
  - name: live-auction
    orgId: 1
    folder: Live Auction
    type: file
    disableDeletion: false
    editable: true
    options:
      path: /var/lib/grafana/dashboards
```

Add Grafana volumes:

```yaml
      - ./deploy/observability/grafana/dashboards:/var/lib/grafana/dashboards:ro
      - ./deploy/observability/grafana/dashboards/dashboard-provider.yaml:/etc/grafana/provisioning/dashboards/dashboard-provider.yaml:ro
```

- [ ] **Step 2: Add dashboards**

Create `deploy/observability/grafana/dashboards/live-auction-overview.json`:

```json
{"title":"Live Auction Overview","schemaVersion":39,"version":1,"refresh":"10s","panels":[{"type":"timeseries","title":"HTTP Requests","targets":[{"datasource":{"type":"prometheus","uid":"Prometheus"},"expr":"sum(rate(http_server_request_count[1m])) by (http_route, http_method)"}],"gridPos":{"x":0,"y":0,"w":12,"h":8}},{"type":"timeseries","title":"DB Query Duration","targets":[{"datasource":{"type":"prometheus","uid":"Prometheus"},"expr":"histogram_quantile(0.95, sum(rate(db_client_operation_duration_bucket[5m])) by (le, db_operation, db_sql_table))"}],"gridPos":{"x":12,"y":0,"w":12,"h":8}}]}
```

Create `deploy/observability/grafana/dashboards/live-auction-bidding.json`:

```json
{"title":"Live Auction Bidding","schemaVersion":39,"version":1,"refresh":"10s","panels":[{"type":"timeseries","title":"Bid Results","targets":[{"datasource":{"type":"prometheus","uid":"Prometheus"},"expr":"sum(rate(auction_bid_count[1m])) by (result, reason)"}],"gridPos":{"x":0,"y":0,"w":12,"h":8}},{"type":"timeseries","title":"Lua Result Codes","targets":[{"datasource":{"type":"prometheus","uid":"Prometheus"},"expr":"sum(rate(auction_place_bid_lua_result_count[1m])) by (code)"}],"gridPos":{"x":12,"y":0,"w":12,"h":8}}]}
```

Create `deploy/observability/grafana/dashboards/live-auction-logs.json`:

```json
{"title":"Live Auction Logs","schemaVersion":39,"version":1,"refresh":"10s","panels":[{"type":"logs","title":"Application Logs","targets":[{"datasource":{"type":"loki","uid":"Loki"},"expr":"{service_name=\"live-auction-backend\"}"}],"gridPos":{"x":0,"y":0,"w":24,"h":12}}]}
```

- [ ] **Step 3: Add local runbook**

Create `docs/observability/local-run.md`:

````markdown
# Local Observability Runbook

## Start

```bash
rtk docker compose up -d mysql redis otel-collector prometheus tempo loki promtail grafana
rtk go run main.go server -c config.yaml
```

## Verify

```bash
rtk curl http://127.0.0.1:8080/api/v1/health
```

Prometheus checks:

```text
http_server_request_count
auction_bid_count
auction_place_bid_lua_result_count
db_client_operation_count
cron_job_run_count
```

Grafana checks:

- Open `http://127.0.0.1:3000`.
- Confirm Prometheus, Tempo, and Loki datasources are provisioned.
- Confirm `Live Auction Overview`, `Live Auction Bidding`, and `Live Auction Logs` dashboards are visible.
- After a bid request, confirm Tempo contains `auction.place_bid`.
- In Loki, query `{service_name="live-auction-backend"}` and filter by `trace_id`.
````

- [ ] **Step 4: Final verification**

Run:

```bash
rtk go test ./...
rtk docker compose config
rtk rg "logx\\.Track" internal/app pkg
```

Expected:

```text
go test ./... passes
docker compose config exits 0
rg "logx\\.Track" internal/app pkg prints no matches
```

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add deploy/observability/grafana docker-compose.yml docs/observability/local-run.md
rtk git commit -m "docs: add observability dashboards and runbook"
```

Expected: commit succeeds.

---

## Self-Review Notes

- Spec coverage: config, provider, HTTP, MySQL, Redis, cron, auction business path, JSON logs, local compose, Grafana, and verification are covered.
- `logx.Track` replacement: Task 3 creates `observability.Track`; Task 5 migrates all service call sites and verifies no `logx.Track` remains.
- High-cardinality control: IDs are span/log attributes only; metrics use operation, route, result, reason, code, and job labels.
- Unit-test boundary: default Go tests use noop/in-memory recorders and do not require MySQL, Redis, Prometheus, Tempo, Loki, or Grafana.
