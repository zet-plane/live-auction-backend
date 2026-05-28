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
		span.SetAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.route", route),
			attribute.Int("http.status_code", status),
		)
		if status >= http.StatusInternalServerError {
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
