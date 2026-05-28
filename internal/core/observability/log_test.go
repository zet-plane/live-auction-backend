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
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
	}))
	fields := TraceFields(ctx)
	if len(fields) != 2 {
		t.Fatalf("fields len = %d, want 2", len(fields))
	}
}
