package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flamego/flamego"
)

func TestLivezAlwaysReturnsOK(t *testing.T) {
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Get("/livez", Livez)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadyzWithoutDBReturnsServiceUnavailable(t *testing.T) {
	Init(nil, nil)
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Get("/readyz", Readyz)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}
