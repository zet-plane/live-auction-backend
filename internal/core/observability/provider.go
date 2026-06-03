package observability

import (
	"context"
	"errors"
	"time"

	"github.com/zet-plane/live-auction-backend/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

	res, err := newResource(cfg)
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
	mp := newMeterProvider(
		res,
		metric.NewPeriodicReader(metricExporter, metric.WithInterval(metricsInterval(cfg))),
	)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

func newResource(cfg config.Observability) (*resource.Resource, error) {
	return resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		attribute.String("deployment.environment", cfg.Environment),
	))
}

func newMeterProvider(res *resource.Resource, reader metric.Reader) *metric.MeterProvider {
	opts := []metric.Option{
		metric.WithReader(reader),
		metric.WithView(durationHistogramViews()...),
	}
	if res != nil {
		opts = append(opts, metric.WithResource(res))
	}
	return metric.NewMeterProvider(opts...)
}

func durationHistogramViews() []metric.View {
	aggregation := metric.AggregationExplicitBucketHistogram{
		Boundaries: durationHistogramBoundaries(),
		NoMinMax:   true,
	}
	names := []string{
		"http.server.request.duration",
		"db.client.operation.duration",
		"auction.place_bid.lua.duration",
		"cron.job.duration",
		"auction.bid.duration",
		"ws.broadcast.duration",
		"ws.delivery.duration",
		"ws.write.duration",
	}
	views := make([]metric.View, 0, len(names))
	for _, name := range names {
		views = append(views, metric.NewView(
			metric.Instrument{Name: name},
			metric.Stream{Aggregation: aggregation},
		))
	}
	return views
}

func durationHistogramBoundaries() []float64 {
	return []float64{
		0.001,
		0.002,
		0.005,
		0.01,
		0.025,
		0.05,
		0.1,
		0.25,
		0.5,
		1,
		2.5,
		5,
		10,
	}
}

func metricsInterval(cfg config.Observability) time.Duration {
	d, err := time.ParseDuration(cfg.MetricsInterval)
	if err != nil || d <= 0 {
		return 15 * time.Second
	}
	return d
}
