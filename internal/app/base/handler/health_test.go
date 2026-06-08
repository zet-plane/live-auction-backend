package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flamego/binding"
	"github.com/flamego/flamego"
	"github.com/zet-plane/live-auction-backend/internal/app/base/dto"
	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
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
	prevDB, prevCache, prevUploadSvc := db, cache, uploadSvc
	t.Cleanup(func() { Init(prevDB, prevCache, prevUploadSvc) })
	Init(nil, nil, nil)
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

func TestSignImageUploadWithoutServiceReturnsInternal(t *testing.T) {
	prevDB, prevCache, prevUploadSvc := db, cache, uploadSvc
	t.Cleanup(func() { Init(prevDB, prevCache, prevUploadSvc) })
	Init(nil, nil, nil)
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Use(func(c flamego.Context) {
		c.Map(&usermodel.User{ID: "user_123"})
		c.Next()
	})
	f.Post("/api/v1/base/uploads/images/sign", binding.JSON(dto.SignImageUploadRequest{}), SignImageUpload)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/base/uploads/images/sign", strings.NewReader(`{"filename":"a.png","content_type":"image/png","size":1,"usage":"item"}`))
	req.Header.Set("Content-Type", "application/json")
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSignImageUploadBindingErrorReturnsBadRequest(t *testing.T) {
	f := flamego.New()
	f.Use(flamego.Renderer())
	f.Use(func(c flamego.Context) {
		c.Map(&usermodel.User{ID: "user_123"})
		c.Next()
	})
	f.Post("/api/v1/base/uploads/images/sign", binding.JSON(dto.SignImageUploadRequest{}), SignImageUpload)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/base/uploads/images/sign", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
