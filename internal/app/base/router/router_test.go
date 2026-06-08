package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flamego/flamego"
)

func TestUploadSignRouteRequiresAuthorization(t *testing.T) {
	f := flamego.New()
	f.Use(flamego.Renderer())
	RegisterRoutes(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/base/uploads/images/sign", strings.NewReader(`{"filename":"a.png","content_type":"image/png","size":1,"usage":"item"}`))
	req.Header.Set("Content-Type", "application/json")
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}
