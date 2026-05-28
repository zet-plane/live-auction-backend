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
