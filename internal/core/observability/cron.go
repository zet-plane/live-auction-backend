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
