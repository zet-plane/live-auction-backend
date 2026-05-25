# Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local,准生产可用的 OpenTelemetry observability stack for live-auction-backend with metrics, traces, Loki logs, and Grafana dashboards.

**Architecture:** The backend initializes OpenTelemetry once during server startup, exports traces and metrics to an OpenTelemetry Collector, and emits JSON logs with trace context for Promtail/Loki. HTTP, GORM, Redis, cron, and auction business paths get focused instrumentation while ordinary unit tests use noop or in-memory providers.

**Tech Stack:** Go 1.23, Flamego, GORM, go-redis, zap, OpenTelemetry Go SDK, OTLP gRPC exporters, OpenTelemetry Collector, Prometheus, Tempo, Loki, Promtail, Grafana, docker-compose.

---

## File Map

- Modify `go.mod`, `go.sum`: add OpenTelemetry, OTLP, Redis OTel, and test helper dependencies.
- Modify `config/vars.go`: add `Observability` and `ObservabilityLogs` config structs.
- Modify `config/config.go`: add duration helper for metrics interval.
- Modify `config.yaml.example`: add local observability defaults.
- Create `internal/core/observability/provider.go`: initialize tracer provider, meter provider, resource, and shutdown.
- Create `internal/core/observability/metrics.go`: define metric recorder interfaces and instruments.
- Create `internal/core/observability/http.go`: Flamego middleware for request spans, route-safe metrics, and trace context mapping.
- Create `internal/core/observability/log.go`: zap helpers for trace context fields and JSON config.
- Create `internal/core/observability/cron.go`: cron wrapper with spans and metrics.
- Create `internal/core/observability/provider_test.go`: noop and config behavior tests.
- Create `internal/core/observability/http_test.go`: middleware route/status tests.
- Create `internal/core/observability/cron_test.go`: cron wrapper success/failure tests.
- Modify `pkg/logx/logp.go`: allow JSON config and trace-aware field injection.
- Modify `cmd/server/server.go`: initialize observability, install middleware, shutdown provider.
- Modify `internal/core/database/database.go`: pass context-aware GORM logger and record DB stats.
- Modify `internal/middleware/gormv2/logger.go`: add spans and metrics around queries.
- Modify `internal/core/cache/cache.go`: install go-redis OTel instrumentation.
- Modify `internal/app/item/cache/bid.go`: add `redis.place_bid_lua` span and Lua result metric.
- Modify `internal/app/item/init.go`: wrap `EndExpiredAuctions` cron job.
- Modify `internal/app/order/init.go`: wrap order cron jobs.
- Modify `internal/app/item/service/bid_service.go`: add `auction.place_bid` trace and business metrics.
- Modify `internal/app/item/service/service.go`: add `item.start`, `auction.end_expired`, and active item metrics.
- Modify `internal/app/order/service/service.go`: add `order.create_from_auction` metrics.
- Modify `internal/app/order/service/cron.go`: add compensation metrics.
- Modify `docker-compose.yml`: add observability services.
- Create `deploy/observability/otel-collector.yaml`.
- Create `deploy/observability/prometheus.yaml`.
- Create `deploy/observability/tempo.yaml`.
- Create `deploy/observability/loki.yaml`.
- Create `deploy/observability/promtail.yaml`.
- Create `deploy/observability/grafana/datasources/prometheus.yaml`.
- Create `deploy/observability/grafana/datasources/tempo.yaml`.
- Create `deploy/observability/grafana/datasources/loki.yaml`.
- Create `deploy/observability/grafana/dashboards/live-auction-overview.json`.
- Create `deploy/observability/grafana/dashboards/live-auction-bidding.json`.
- Create `deploy/observability/grafana/dashboards/live-auction-logs.json`.
- Create `docs/observability/local-run.md`: local verification guide.

---

## Task 1: Dependencies And Config

**Files:**
- Modify: `go.mod`
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
```

Expected: `go.mod` contains `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/sdk`, OTLP exporters, and `github.com/redis/go-redis/extra/redisotel/v9`.

- [ ] **Step 2: Write config test**

Add to `config/config_test.go`:

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
}
```

- [ ] **Step 3: Run config test and verify failure**

Run:

```bash
rtk go test ./config -run TestObservabilityMetricsInterval -count=1
```

Expected: FAIL with `cfg.Observability undefined` or `ObservabilityMetricsInterval undefined`.

- [ ] **Step 4: Add config structs**

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
	Enabled          bool              `yaml:"enabled"            mapstructure:"enabled"`
	ServiceName      string            `yaml:"service_name"       mapstructure:"service_name"`
	ServiceVersion   string            `yaml:"service_version"    mapstructure:"service_version"`
	Environment      string            `yaml:"environment"        mapstructure:"environment"`
	OTLPEndpoint     string            `yaml:"otlp_endpoint"      mapstructure:"otlp_endpoint"`
	OTLPInsecure     bool              `yaml:"otlp_insecure"      mapstructure:"otlp_insecure"`
	TraceSampleRatio float64           `yaml:"trace_sample_ratio" mapstructure:"trace_sample_ratio"`
	MetricsInterval  string            `yaml:"metrics_interval"   mapstructure:"metrics_interval"`
	Logs             ObservabilityLogs `yaml:"logs"               mapstructure:"logs"`
}

type ObservabilityLogs struct {
	Format              string `yaml:"format"                mapstructure:"format"`
	Output              string `yaml:"output"                mapstructure:"output"`
	IncludeTraceContext bool   `yaml:"include_trace_context" mapstructure:"include_trace_context"`
}
```

Modify `config/config.go`:

```go
func (c *GlobalConfig) ObservabilityMetricsInterval() time.Duration {
	return parseDuration(c.Observability.MetricsInterval, 15*time.Second)
}
```

- [ ] **Step 5: Add example YAML**

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

- [ ] **Step 6: Run config tests**

Run:

```bash
rtk go test ./config -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```bash
rtk git add go.mod go.sum config/vars.go config/config.go config/config_test.go config.yaml.example
rtk git commit -m "feat: add observability config"
```

Expected: commit succeeds.

---

## Task 2: Observability Provider

**Files:**
- Create: `internal/core/observability/provider.go`
- Create: `internal/core/observability/provider_test.go`

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
	if cfg.MetricsInterval != "15s" {
		t.Fatalf("metrics interval = %q", cfg.MetricsInterval)
	}
}

func TestMetricsIntervalFallsBack(t *testing.T) {
	got := metricsInterval(config.Observability{MetricsInterval: "bad"})
	if got != 15*time.Second {
		t.Fatalf("interval = %v, want 15s", got)
	}
}
```

- [ ] **Step 2: Run provider tests and verify failure**

Run:

```bash
rtk go test ./internal/core/observability -run 'TestSetupDisabled|TestNormalizeConfig|TestMetricsInterval' -count=1
```

Expected: FAIL because the package or functions do not exist.

- [ ] **Step 3: Create provider**

Create `internal/core/observability/provider.go`:

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

const (
	defaultServiceName     = "live-auction-backend"
	defaultEnvironment     = "local"
	defaultOTLPEndpoint    = "127.0.0.1:4317"
	defaultMetricsInterval = "15s"
)

func NormalizeConfig(cfg config.Observability) config.Observability {
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultServiceName
	}
	if cfg.Environment == "" {
		cfg.Environment = defaultEnvironment
	}
	if cfg.OTLPEndpoint == "" {
		cfg.OTLPEndpoint = defaultOTLPEndpoint
	}
	if cfg.TraceSampleRatio <= 0 || cfg.TraceSampleRatio > 1 {
		cfg.TraceSampleRatio = 1
	}
	if cfg.MetricsInterval == "" {
		cfg.MetricsInterval = defaultMetricsInterval
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

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
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

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.TraceSampleRatio)),
		sdktrace.WithBatcher(traceExporter),
	)
	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(metricsInterval(cfg)))),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return func(ctx context.Context) error {
		return errors.Join(
			tracerProvider.Shutdown(ctx),
			meterProvider.Shutdown(ctx),
		)
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

- [ ] **Step 4: Run provider tests**

Run:

```bash
rtk go test ./internal/core/observability -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add internal/core/observability/provider.go internal/core/observability/provider_test.go
rtk git commit -m "feat: initialize opentelemetry provider"
```

Expected: commit succeeds.

---

## Task 3: Metrics Recorder

**Files:**
- Create: `internal/core/observability/metrics.go`
- Create: `internal/core/observability/metrics_test.go`

- [ ] **Step 1: Write metrics tests**

Create `internal/core/observability/metrics_test.go`:

```go
package observability

import (
	"context"
	"testing"
	"time"
)

func TestNoopRecorderDoesNotPanic(t *testing.T) {
	r := NoopRecorder{}
	ctx := context.Background()
	r.HTTPRequest(ctx, HTTPRequestMetric{Route: "/api/v1/health", Method: "GET", Status: 200, Duration: time.Millisecond})
	r.RedisLua(ctx, RedisLuaMetric{Code: "0", Duration: time.Millisecond})
	r.Bid(ctx, BidMetric{Result: "success", Reason: "accepted", Amount: 1000, Duration: time.Millisecond})
	r.Cron(ctx, CronMetric{Name: "item.end_expired_auctions", Result: "success", Duration: time.Millisecond})
}

func TestSafeReasonNormalizesEmpty(t *testing.T) {
	if got := SafeReason(""); got != "none" {
		t.Fatalf("SafeReason empty = %q", got)
	}
	if got := SafeReason("price_too_low"); got != "price_too_low" {
		t.Fatalf("SafeReason value = %q", got)
	}
}
```

- [ ] **Step 2: Run metrics tests and verify failure**

Run:

```bash
rtk go test ./internal/core/observability -run 'TestNoopRecorder|TestSafeReason' -count=1
```

Expected: FAIL because metric types do not exist.

- [ ] **Step 3: Create metrics recorder**

Create `internal/core/observability/metrics.go`:

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
	RedisCommand(context.Context, RedisCommandMetric)
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

type RedisCommandMetric struct {
	Command  string
	Result   string
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

func (NoopRecorder) HTTPRequest(context.Context, HTTPRequestMetric)       {}
func (NoopRecorder) RedisCommand(context.Context, RedisCommandMetric)     {}
func (NoopRecorder) RedisLua(context.Context, RedisLuaMetric)             {}
func (NoopRecorder) DBQuery(context.Context, DBQueryMetric)               {}
func (NoopRecorder) Cron(context.Context, CronMetric)                     {}
func (NoopRecorder) Bid(context.Context, BidMetric)                       {}
func (NoopRecorder) OrderAuctionCreate(context.Context, OrderMetric)      {}

type OTelRecorder struct {
	httpCount       metric.Int64Counter
	httpDuration    metric.Float64Histogram
	redisLuaCount   metric.Int64Counter
	redisLuaDuration metric.Float64Histogram
	bidCount        metric.Int64Counter
	bidAmount       metric.Int64Histogram
	bidDuration     metric.Float64Histogram
	cronCount       metric.Int64Counter
	cronDuration    metric.Float64Histogram
	dbCount         metric.Int64Counter
	dbDuration      metric.Float64Histogram
	orderCount      metric.Int64Counter
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
	cronCount, err := meter.Int64Counter("cron.job.run.count")
	if err != nil {
		return nil, err
	}
	cronDuration, err := meter.Float64Histogram("cron.job.duration")
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
	orderCount, err := meter.Int64Counter("order.auction_create.count")
	if err != nil {
		return nil, err
	}
	return &OTelRecorder{
		httpCount: httpCount, httpDuration: httpDuration,
		redisLuaCount: redisLuaCount, redisLuaDuration: redisLuaDuration,
		bidCount: bidCount, bidAmount: bidAmount, bidDuration: bidDuration,
		cronCount: cronCount, cronDuration: cronDuration,
		dbCount: dbCount, dbDuration: dbDuration,
		orderCount: orderCount,
	}, nil
}

func (r *OTelRecorder) HTTPRequest(ctx context.Context, m HTTPRequestMetric) {
	attrs := metric.WithAttributes(attribute.String("http.route", m.Route), attribute.String("http.method", m.Method), attribute.Int("http.status_code", m.Status))
	r.httpCount.Add(ctx, 1, attrs)
	r.httpDuration.Record(ctx, m.Duration.Seconds(), attrs)
}

func (r *OTelRecorder) RedisCommand(context.Context, RedisCommandMetric) {}

func (r *OTelRecorder) RedisLua(ctx context.Context, m RedisLuaMetric) {
	attrs := metric.WithAttributes(attribute.String("code", m.Code))
	r.redisLuaCount.Add(ctx, 1, attrs)
	r.redisLuaDuration.Record(ctx, m.Duration.Seconds(), attrs)
}

func (r *OTelRecorder) DBQuery(ctx context.Context, m DBQueryMetric) {
	attrs := metric.WithAttributes(attribute.String("db.operation", m.Operation), attribute.String("db.sql.table", m.Table), attribute.String("result", m.Result), attribute.Bool("slow", m.Slow))
	r.dbCount.Add(ctx, 1, attrs)
	r.dbDuration.Record(ctx, m.Duration.Seconds(), attrs)
}

func (r *OTelRecorder) Cron(ctx context.Context, m CronMetric) {
	attrs := metric.WithAttributes(attribute.String("job", m.Name), attribute.String("result", m.Result))
	r.cronCount.Add(ctx, 1, attrs)
	r.cronDuration.Record(ctx, m.Duration.Seconds(), attrs)
}

func (r *OTelRecorder) Bid(ctx context.Context, m BidMetric) {
	attrs := metric.WithAttributes(attribute.String("result", m.Result), attribute.String("reason", SafeReason(m.Reason)))
	r.bidCount.Add(ctx, 1, attrs)
	r.bidAmount.Record(ctx, m.Amount, attrs)
	r.bidDuration.Record(ctx, m.Duration.Seconds(), attrs)
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

- [ ] **Step 4: Run metrics tests**

Run:

```bash
rtk go test ./internal/core/observability -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add internal/core/observability/metrics.go internal/core/observability/metrics_test.go
rtk git commit -m "feat: add observability metrics recorder"
```

Expected: commit succeeds.

---

## Task 4: HTTP Middleware And Trace Context

**Files:**
- Create: `internal/core/observability/http.go`
- Create: `internal/core/observability/http_test.go`
- Modify: `cmd/server/server.go`

- [ ] **Step 1: Write HTTP middleware tests**

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

type captureRecorder struct {
	NoopRecorder
	httpMetric HTTPRequestMetric
}

func (c *captureRecorder) HTTPRequest(_ context.Context, m HTTPRequestMetric) {
	c.httpMetric = m
}

func TestHTTPMiddlewareRecordsRouteTemplate(t *testing.T) {
	rec := &captureRecorder{}
	f := flamego.New()
	f.Use(HTTPMiddleware(rec))
	f.Get("/api/v1/items/{item_id}/bids", func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/item_123/bids", nil)
	w := httptest.NewRecorder()
	f.ServeHTTP(w, req)

	if rec.httpMetric.Route != "/api/v1/items/{item_id}/bids" {
		t.Fatalf("route = %q", rec.httpMetric.Route)
	}
	if rec.httpMetric.Status != http.StatusCreated {
		t.Fatalf("status = %d", rec.httpMetric.Status)
	}
	if rec.httpMetric.Duration <= 0 || rec.httpMetric.Duration > time.Minute {
		t.Fatalf("duration = %v", rec.httpMetric.Duration)
	}
}
```

- [ ] **Step 2: Run HTTP middleware test and verify failure**

Run:

```bash
rtk go test ./internal/core/observability -run TestHTTPMiddlewareRecordsRouteTemplate -count=1
```

Expected: FAIL because `HTTPMiddleware` does not exist.

- [ ] **Step 3: Create HTTP middleware**

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
		route := routePattern(c, req)
		ctx, span := tracer.Start(req.Context(), req.Method+" "+route)
		defer span.End()
		*req = *req.WithContext(ctx)

		start := time.Now()
		c.Next()
		status := c.ResponseWriter().Status()
		duration := time.Since(start)

		span.SetAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.route", route),
			attribute.Int("http.status_code", status),
		)
		if status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		rec.HTTPRequest(ctx, HTTPRequestMetric{Route: route, Method: req.Method, Status: status, Duration: duration})
	}
}

func routePattern(c flamego.Context, req *http.Request) string {
	if path := c.Param("route"); path != "" {
		return path
	}
	return req.URL.Path
}
```

- [ ] **Step 4: Wire middleware in server**

Modify `cmd/server/server.go`:

```go
shutdown, err := observability.Setup(context.Background(), cfg.Observability)
if err != nil {
	logx.Errorf("observability setup failed: %v", err)
	shutdown = func(context.Context) error { return nil }
}
rec, err := observability.NewRecorder()
if err != nil {
	logx.Errorf("observability recorder setup failed: %v", err)
	rec = nil
}
engine, err := buildEngine(cfg, db, rdb, rec)
```

Change `buildEngine` signature:

```go
func buildEngine(cfg *config.Config, db *gorm.DB, rdb *redis.Client, rec observability.Recorder) (*kernel.Engine, error)
```

Add middleware before `gw.RequestLog()`:

```go
observability.HTTPMiddleware(rec),
gw.RequestLog(),
```

Call shutdown after `run(engine)` returns:

```go
defer func() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		logx.Errorf("observability shutdown failed: %v", err)
	}
}()
```

- [ ] **Step 5: Run tests**

Run:

```bash
rtk go test ./internal/core/observability ./cmd/server -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
rtk git add internal/core/observability/http.go internal/core/observability/http_test.go cmd/server/server.go
rtk git commit -m "feat: add http observability middleware"
```

Expected: commit succeeds.

---

## Task 5: Logging With Trace Context

**Files:**
- Create: `internal/core/observability/log.go`
- Create: `internal/core/observability/log_test.go`
- Modify: `pkg/logx/logp.go`
- Modify: `cmd/server/server.go`

- [ ] **Step 1: Write log helper test**

Create `internal/core/observability/log_test.go`:

```go
package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceFieldsWithoutSpan(t *testing.T) {
	fields := TraceFields(context.Background())
	if len(fields) != 0 {
		t.Fatalf("fields = %v, want empty", fields)
	}
}

func TestTraceFieldsWithSpanContext(t *testing.T) {
	tid, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	sid, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled}))
	fields := TraceFields(ctx)
	if len(fields) != 2 {
		t.Fatalf("fields len = %d", len(fields))
	}
}
```

- [ ] **Step 2: Run log helper test and verify failure**

Run:

```bash
rtk go test ./internal/core/observability -run TestTraceFields -count=1
```

Expected: FAIL because `TraceFields` does not exist.

- [ ] **Step 3: Add trace log helper**

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

- [ ] **Step 4: Add JSON logging option**

Modify `pkg/logx/logp.go`:

```go
func JSONConfig() *zap.Config {
	cfg := defaultConfig()
	cfg.Encoding = "json"
	cfg.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	return cfg
}
```

In `cmd/server/server.go` `PreRun`, select config:

```go
config.LoadConfig(configPath)
cfg := config.GetConfig()
if cfg.Observability.Logs.Format == "json" {
	logx.SetUp(logx.WithZapConfig(logx.JSONConfig()))
	return
}
logx.SetUp()
```

- [ ] **Step 5: Run logging tests**

Run:

```bash
rtk go test ./pkg/logx ./internal/core/observability -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
rtk git add internal/core/observability/log.go internal/core/observability/log_test.go pkg/logx/logp.go cmd/server/server.go
rtk git commit -m "feat: add trace-aware json logging"
```

Expected: commit succeeds.

---

## Task 6: Redis And Lua Instrumentation

**Files:**
- Modify: `internal/core/cache/cache.go`
- Modify: `internal/app/item/cache/bid.go`
- Test: `internal/app/item/cache/bid_test.go`

- [ ] **Step 1: Add Redis client instrumentation**

Modify `internal/core/cache/cache.go`:

```go
import "github.com/redis/go-redis/extra/redisotel/v9"
```

After `redis.NewClient`:

```go
if err := redisotel.InstrumentTracing(client); err != nil {
	return nil, fmt.Errorf("instrument redis tracing: %w", err)
}
if err := redisotel.InstrumentMetrics(client); err != nil {
	return nil, fmt.Errorf("instrument redis metrics: %w", err)
}
```

- [ ] **Step 2: Add Lua span and metric**

Modify `internal/app/item/cache/bid.go` around `PlaceBidLua`:

```go
tracer := otel.Tracer("github.com/zet-plane/live-auction-backend/redis")
ctx, span := tracer.Start(ctx, "redis.place_bid_lua")
defer span.End()
start := time.Now()
```

After parsing result:

```go
result := &BidLuaResult{
	Code:             int(toI64(res[0])),
	BidID:            toStr(res[1]),
	CurrentPrice:     toI64(res[2]),
	LeaderUserID:     toStr(res[3]),
	EndTimeUnix:      toI64(res[4]),
	IsExtended:       toI64(res[5]) == 1,
	IsCapped:         toI64(res[6]) == 1,
	PrevLeaderUserID: toStr(res[7]),
}
span.SetAttributes(attribute.String("auction.item_id", itemID), attribute.Int("auction.lua.code", result.Code))
observability.DefaultRecorder().RedisLua(ctx, observability.RedisLuaMetric{Code: strconv.Itoa(result.Code), Duration: time.Since(start)})
return result, nil
```

On Redis error:

```go
span.RecordError(err)
span.SetStatus(codes.Error, err.Error())
observability.DefaultRecorder().RedisLua(ctx, observability.RedisLuaMetric{Code: "error", Duration: time.Since(start)})
return nil, err
```

Add imports:

```go
import (
	"time"

	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)
```

Add `DefaultRecorder` support in `metrics.go`:

```go
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
```

- [ ] **Step 3: Run cache tests**

Run:

```bash
rtk go test ./internal/app/item/cache ./internal/core/cache ./internal/core/observability -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

Run:

```bash
rtk git add internal/core/cache/cache.go internal/app/item/cache/bid.go internal/core/observability/metrics.go
rtk git commit -m "feat: instrument redis auction operations"
```

Expected: commit succeeds.

---

## Task 7: GORM Query Instrumentation

**Files:**
- Modify: `internal/middleware/gormv2/logger.go`
- Modify: `internal/middleware/gormv2/logger_test.go`

- [ ] **Step 1: Extend logger tests**

Add to `internal/middleware/gormv2/logger_test.go`:

```go
func TestTableFromSQL(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM auction_items WHERE id = ?": "auction_items",
		"INSERT INTO bid_logs (`id`) VALUES (?)":   "bid_logs",
		"UPDATE orders SET status = ?":             "orders",
		"DELETE FROM deposits WHERE id = ?":        "deposits",
	}
	for sql, want := range cases {
		if got := tableFromSQL(sql); got != want {
			t.Fatalf("tableFromSQL(%q) = %q, want %q", sql, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
rtk go test ./internal/middleware/gormv2 -run TestTableFromSQL -count=1
```

Expected: FAIL because `tableFromSQL` does not exist.

- [ ] **Step 3: Add trace and metric recording**

Modify `Trace` in `internal/middleware/gormv2/logger.go`:

```go
ctx, span := otel.Tracer("github.com/zet-plane/live-auction-backend/gorm").Start(ctx, "mysql.query")
defer span.End()
elapsed := time.Since(begin)
sql, rows := fc()
operation := operationFromSQL(sql)
table := tableFromSQL(sql)
result := "success"
if err != nil {
	result = "error"
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
span.SetAttributes(
	attribute.String("db.system", "mysql"),
	attribute.String("db.operation", operation),
	attribute.String("db.sql.table", table),
	attribute.Int64("db.rows_affected", rows),
)
observability.DefaultRecorder().DBQuery(ctx, observability.DBQueryMetric{
	Operation: operation,
	Table: table,
	Result: result,
	Slow: l.SlowThreshold > 0 && elapsed > l.SlowThreshold,
	Duration: elapsed,
})
```

Add helper functions:

```go
func operationFromSQL(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "unknown"
	}
	return strings.ToUpper(fields[0])
}

func tableFromSQL(sql string) string {
	fields := strings.Fields(strings.ReplaceAll(sql, "`", ""))
	for i, f := range fields {
		upper := strings.ToUpper(f)
		if (upper == "FROM" || upper == "INTO" || upper == "UPDATE") && i+1 < len(fields) {
			return strings.Trim(fields[i+1], ",")
		}
	}
	return "unknown"
}
```

- [ ] **Step 4: Run GORM logger tests**

Run:

```bash
rtk go test ./internal/middleware/gormv2 -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
rtk git add internal/middleware/gormv2/logger.go internal/middleware/gormv2/logger_test.go
rtk git commit -m "feat: instrument gorm queries"
```

Expected: commit succeeds.

---

## Task 8: Cron Wrapper

**Files:**
- Create: `internal/core/observability/cron.go`
- Create: `internal/core/observability/cron_test.go`
- Modify: `internal/app/item/init.go`
- Modify: `internal/app/order/init.go`

- [ ] **Step 1: Write cron wrapper tests**

Create `internal/core/observability/cron_test.go`:

```go
package observability

import (
	"context"
	"testing"
)

type cronCapture struct {
	NoopRecorder
	metric CronMetric
}

func (c *cronCapture) Cron(_ context.Context, m CronMetric) {
	c.metric = m
}

func TestWrapCronRecordsSuccess(t *testing.T) {
	rec := &cronCapture{}
	fn := WrapCron("item.end_expired_auctions", rec, func() {})
	fn()
	if rec.metric.Name != "item.end_expired_auctions" {
		t.Fatalf("job = %q", rec.metric.Name)
	}
	if rec.metric.Result != "success" {
		t.Fatalf("result = %q", rec.metric.Result)
	}
}
```

- [ ] **Step 2: Run cron test and verify failure**

Run:

```bash
rtk go test ./internal/core/observability -run TestWrapCronRecordsSuccess -count=1
```

Expected: FAIL because `WrapCron` does not exist.

- [ ] **Step 3: Create cron wrapper**

Create `internal/core/observability/cron.go`:

```go
package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func WrapCron(name string, rec Recorder, fn func()) func() {
	if rec == nil {
		rec = NoopRecorder{}
	}
	return func() {
		ctx, span := otel.Tracer("github.com/zet-plane/live-auction-backend/cron").Start(context.Background(), "cron."+name)
		defer span.End()
		start := time.Now()
		fn()
		duration := time.Since(start)
		span.SetAttributes(attribute.String("cron.job", name), attribute.String("cron.result", "success"))
		rec.Cron(ctx, CronMetric{Name: name, Result: "success", Duration: duration})
	}
}
```

- [ ] **Step 4: Wrap registered jobs**

Modify `internal/app/item/init.go`:

```go
engine.Cron.AddFunc("@every 1m", observability.WrapCron("item.end_expired_auctions", observability.DefaultRecorder(), svc.EndExpiredAuctions))
```

Modify `internal/app/order/init.go`:

```go
engine.Cron.AddFunc("@every 5m", observability.WrapCron("order.scan_expired_orders", observability.DefaultRecorder(), Svc.ScanExpiredOrders))
engine.Cron.AddFunc("@every 10m", observability.WrapCron("order.scan_compensation", observability.DefaultRecorder(), Svc.ScanCompensation))
```

- [ ] **Step 5: Run cron and module tests**

Run:

```bash
rtk go test ./internal/core/observability ./internal/app/item/... ./internal/app/order/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
rtk git add internal/core/observability/cron.go internal/core/observability/cron_test.go internal/app/item/init.go internal/app/order/init.go
rtk git commit -m "feat: instrument cron jobs"
```

Expected: commit succeeds.

---

## Task 9: Auction Business Spans And Metrics

**Files:**
- Modify: `internal/app/item/service/bid_service.go`
- Modify: `internal/app/item/service/bid_service_test.go`
- Modify: `internal/app/item/service/service.go`
- Modify: `internal/app/order/service/service.go`
- Modify: `internal/app/order/service/cron.go`

- [ ] **Step 1: Add recorder injection to item service**

Modify `internal/app/item/service/service.go` `Service` struct:

```go
type Service struct {
	store      dao.Store
	policy     dto.AuctionPolicy
	cache      itemcache.Cache
	orderSvc   *orderservice.Service
	depositSvc DepositChecker
	hub        EventPublisher
	recorder   observability.Recorder
	now        func() time.Time
}
```

Add method:

```go
func (s *Service) WithRecorder(rec observability.Recorder) *Service {
	if rec == nil {
		s.recorder = observability.NoopRecorder{}
		return s
	}
	s.recorder = rec
	return s
}
```

In `NewService`, set `recorder: observability.DefaultRecorder()`.

- [ ] **Step 2: Instrument PlaceBid**

Modify `PlaceBid` in `internal/app/item/service/bid_service.go`:

```go
start := time.Now()
ctx, span := otel.Tracer("github.com/zet-plane/live-auction-backend/auction").Start(context.Background(), "auction.place_bid")
defer span.End()
span.SetAttributes(attribute.String("auction.item_id", strings.TrimSpace(itemID)), attribute.String("user.id", current.ID))
recordBid := func(result, reason string, amount int64) {
	s.recorder.Bid(ctx, observability.BidMetric{Result: result, Reason: reason, Amount: amount, Duration: time.Since(start)})
}
```

Before each return:

```go
recordBid("error", "find_item_failed", input.Price)
recordBid("rejected", "item_not_ongoing", input.Price)
recordBid("rejected", "deposit_required", input.Price)
recordBid("error", "redis_error", input.Price)
recordBid("idempotent", "idempotency_key", input.Price)
recordBid("rejected", "auction_ended", input.Price)
recordBid("rejected", "price_too_low", input.Price)
recordBid("rejected", "invalid_bid_increment", input.Price)
recordBid("error", "bid_log_create_failed", input.Price)
recordBid("success", "accepted", input.Price)
```

When capped:

```go
span.SetAttributes(attribute.Bool("auction.price_cap_end", true))
```

- [ ] **Step 3: Add business metric test**

Add to `internal/app/item/service/bid_service_test.go`:

```go
type bidCaptureRecorder struct {
	observability.NoopRecorder
	bids []observability.BidMetric
}

func (r *bidCaptureRecorder) Bid(_ context.Context, m observability.BidMetric) {
	r.bids = append(r.bids, m)
}

func TestPlaceBidRecordsSuccessMetric(t *testing.T) {
	svc, store, cache := newStartedAuctionService(t)
	rec := &bidCaptureRecorder{}
	svc.WithRecorder(rec)
	bidder := &usermodel.User{ID: "user_metric", Name: "metric", Identity: usermodel.IdentityUser}
	_, err := svc.PlaceBid(bidder, store.item.ID, itemdto.PlaceBidInput{Price: 1100, IdempotencyKey: "metric_key", UserName: "metric"})
	if err != nil {
		t.Fatalf("PlaceBid returned error: %v", err)
	}
	if cache == nil {
		t.Fatal("cache is nil")
	}
	if len(rec.bids) != 1 {
		t.Fatalf("metric count = %d", len(rec.bids))
	}
	if rec.bids[0].Result != "success" || rec.bids[0].Reason != "accepted" {
		t.Fatalf("metric = %+v", rec.bids[0])
	}
}
```

- [ ] **Step 4: Instrument order creation**

Modify `internal/app/order/service/service.go` `CreateOrder`:

```go
ctx, span := otel.Tracer("github.com/zet-plane/live-auction-backend/order").Start(context.Background(), "order.create_from_auction")
defer span.End()
span.SetAttributes(attribute.String("auction.item_id", itemID), attribute.String("user.id", userID))
```

Record result:

```go
observability.DefaultRecorder().OrderAuctionCreate(ctx, observability.OrderMetric{Result: "success"})
observability.DefaultRecorder().OrderAuctionCreate(ctx, observability.OrderMetric{Result: "error"})
```

- [ ] **Step 5: Run service tests**

Run:

```bash
rtk go test ./internal/app/item/service ./internal/app/order/service -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
rtk git add internal/app/item/service/bid_service.go internal/app/item/service/bid_service_test.go internal/app/item/service/service.go internal/app/order/service/service.go internal/app/order/service/cron.go
rtk git commit -m "feat: instrument auction business flow"
```

Expected: commit succeeds.

---

## Task 10: Local Observability Stack

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

- [ ] **Step 1: Add OTel Collector config**

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
  logging:
    verbosity: basic

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp/tempo, logging]
    metrics:
      receivers: [otlp]
      exporters: [prometheus, logging]
```

- [ ] **Step 2: Add Prometheus config**

Create `deploy/observability/prometheus.yaml`:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: otel-collector
    static_configs:
      - targets: ["otel-collector:8889"]
```

- [ ] **Step 3: Add Tempo config**

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

- [ ] **Step 4: Add Loki and Promtail configs**

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
          job: live-auction-backend
          service_name: live-auction-backend
          environment: local
          __path__: /var/log/live-auction/*.log
```

- [ ] **Step 5: Add Grafana datasources**

Create `deploy/observability/grafana/datasources/prometheus.yaml`:

```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    uid: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
```

Create `deploy/observability/grafana/datasources/tempo.yaml`:

```yaml
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
        tags: ["service_name", "environment"]
        filterByTraceID: true
```

Create `deploy/observability/grafana/datasources/loki.yaml`:

```yaml
apiVersion: 1
datasources:
  - name: Loki
    uid: Loki
    type: loki
    access: proxy
    url: http://loki:3100
    jsonData:
      derivedFields:
        - name: trace_id
          matcherRegex: '"trace_id":"([a-f0-9]+)"'
          datasourceUid: Tempo
          url: '$${__value.raw}'
```

- [ ] **Step 6: Add compose services**

Modify `docker-compose.yml` by adding services:

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

- [ ] **Step 7: Validate compose config**

Run:

```bash
rtk docker compose config
```

Expected: command exits successfully and prints merged compose YAML.

- [ ] **Step 8: Commit**

Run:

```bash
rtk git add docker-compose.yml deploy/observability
rtk git commit -m "chore: add local observability stack"
```

Expected: commit succeeds.

---

## Task 11: Dashboards And Local Verification Guide

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

Modify Grafana service volumes:

```yaml
      - ./deploy/observability/grafana/dashboards:/var/lib/grafana/dashboards:ro
      - ./deploy/observability/grafana/dashboards/dashboard-provider.yaml:/etc/grafana/provisioning/dashboards/dashboard-provider.yaml:ro
```

- [ ] **Step 2: Add minimal overview dashboard**

Create `deploy/observability/grafana/dashboards/live-auction-overview.json`:

```json
{
  "title": "Live Auction Overview",
  "schemaVersion": 39,
  "version": 1,
  "refresh": "10s",
  "panels": [
    {
      "type": "timeseries",
      "title": "HTTP Requests",
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "Prometheus"},
          "expr": "sum(rate(http_server_request_count[1m])) by (http_route, http_method)"
        }
      ],
      "gridPos": {"x": 0, "y": 0, "w": 12, "h": 8}
    }
  ]
}
```

- [ ] **Step 3: Add bidding dashboard**

Create `deploy/observability/grafana/dashboards/live-auction-bidding.json`:

```json
{
  "title": "Live Auction Bidding",
  "schemaVersion": 39,
  "version": 1,
  "refresh": "10s",
  "panels": [
    {
      "type": "timeseries",
      "title": "Bid Results",
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "Prometheus"},
          "expr": "sum(rate(auction_bid_count[1m])) by (result, reason)"
        }
      ],
      "gridPos": {"x": 0, "y": 0, "w": 12, "h": 8}
    }
  ]
}
```

- [ ] **Step 4: Add logs dashboard**

Create `deploy/observability/grafana/dashboards/live-auction-logs.json`:

```json
{
  "title": "Live Auction Logs",
  "schemaVersion": 39,
  "version": 1,
  "refresh": "10s",
  "panels": [
    {
      "type": "logs",
      "title": "Application Logs",
      "targets": [
        {
          "datasource": {"type": "loki", "uid": "Loki"},
          "expr": "{service_name=\"live-auction-backend\"}"
        }
      ],
      "gridPos": {"x": 0, "y": 0, "w": 24, "h": 12}
    }
  ]
}
```

- [ ] **Step 5: Add local verification guide**

Create `docs/observability/local-run.md`:

````markdown
# Local Observability Runbook

## Start stack

```bash
rtk docker compose up -d mysql redis otel-collector prometheus tempo loki promtail grafana
rtk go run main.go server -c config.yaml
```

## Open

- Grafana: http://127.0.0.1:3000
- Prometheus: http://127.0.0.1:9090
- Tempo: http://127.0.0.1:3200
- Loki: http://127.0.0.1:3100

## Verify

1. Call `GET /api/v1/health`.
2. In Prometheus, query `http_server_request_count`.
3. Create a room, item, deposit, and bid through API calls.
4. In Prometheus, query `auction_bid_count`.
5. In Grafana Tempo, find a trace named `auction.place_bid`.
6. In Grafana Loki, query `{service_name="live-auction-backend"}` and filter by `trace_id`.
````

- [ ] **Step 6: Validate JSON files**

Run:

```bash
rtk go test ./...
```

Expected: PASS for Go tests. If external services are not running, unit tests still pass because they do not depend on Prometheus, Tempo, Loki, Grafana, MySQL, or Redis.

- [ ] **Step 7: Commit**

Run:

```bash
rtk git add deploy/observability/grafana docker-compose.yml docs/observability/local-run.md
rtk git commit -m "docs: add observability dashboards and runbook"
```

Expected: commit succeeds.

---

## Final Verification

- [ ] **Step 1: Run full unit suite**

Run:

```bash
rtk go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run compose config validation**

Run:

```bash
rtk docker compose config
```

Expected: PASS and merged YAML output.

- [ ] **Step 3: Start local observability stack**

Run:

```bash
rtk docker compose up -d mysql redis otel-collector prometheus tempo loki promtail grafana
```

Expected: all requested containers are running.

- [ ] **Step 4: Start backend**

Run:

```bash
rtk go run main.go server -c config.yaml
```

Expected: server starts on configured address and logs in JSON format.

- [ ] **Step 5: Smoke check health metrics**

Run:

```bash
rtk curl http://127.0.0.1:8080/api/v1/health
```

Expected: JSON response with `status: ok`.

- [ ] **Step 6: Verify observability UIs**

Open:

```text
http://127.0.0.1:3000
http://127.0.0.1:9090
http://127.0.0.1:3200
http://127.0.0.1:3100
```

Expected:

- Prometheus query `http_server_request_count` returns samples.
- Grafana shows provisioned datasources.
- Tempo receives traces after health and bid requests.
- Loki returns app logs with `trace_id` when a request span is active.

---

## Self-Review Notes

- Spec coverage: tasks cover config, provider, HTTP, MySQL, Redis, cron, auction business metrics, Loki logging, local compose, Grafana, and verification.
- Scope retained: no alerting, no production deployment automation, no default unit tests that require external services.
- High cardinality control: route templates are used for HTTP labels; IDs stay in logs/span attributes rather than Loki labels or metric labels.
