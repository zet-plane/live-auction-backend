package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flamego/flamego"
)

func TestRegisterSwaggerRoutesServesIndex(t *testing.T) {
	f := flamego.New()
	registerSwaggerRoutes(f)

	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("expected swagger index route to be mounted, got status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil)
	rec = httptest.NewRecorder()
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected swagger JSON route to return 200, got status %d", rec.Code)
	}
	var body struct {
		Swagger string `json:"swagger"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected generated swagger JSON, got decode error: %v", err)
	}
	if body.Swagger != "2.0" {
		t.Fatalf("expected swagger 2.0 JSON, got %q", body.Swagger)
	}
}
