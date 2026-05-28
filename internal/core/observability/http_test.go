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
