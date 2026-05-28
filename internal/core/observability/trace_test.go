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
