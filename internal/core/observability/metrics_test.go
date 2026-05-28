package observability

import (
	"context"
	"testing"
	"time"
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

type captureRecorder struct {
	NoopRecorder
	http HTTPRequestMetric
}

func (r *captureRecorder) HTTPRequest(_ context.Context, m HTTPRequestMetric) {
	r.http = m
}
