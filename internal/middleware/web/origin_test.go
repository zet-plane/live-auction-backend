package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flamego/flamego"
)

func TestOriginPolicyDefaultsAllowLocalhostOutsideRelease(t *testing.T) {
	policy := NewOriginPolicy("debug", nil)

	if !policy.Allows("http://localhost:5173") {
		t.Fatal("localhost origin should be allowed in debug mode")
	}
	if !policy.Allows("http://127.0.0.1:5173") {
		t.Fatal("127.0.0.1 origin should be allowed in debug mode")
	}
	if policy.Allows("https://example.com") {
		t.Fatal("unconfigured remote origin should be rejected in debug mode")
	}
}

func TestOriginPolicyDefaultsRejectRemoteOriginsInRelease(t *testing.T) {
	policy := NewOriginPolicy("release", nil)

	if policy.Allows("http://localhost:5173") {
		t.Fatal("release mode should not allow localhost without explicit config")
	}
	if policy.Allows("https://example.com") {
		t.Fatal("release mode should not allow remote origins without explicit config")
	}
}

func TestOriginPolicyAllowsConfiguredOriginsExactly(t *testing.T) {
	policy := NewOriginPolicy("release", []string{"https://app.example.com", "http://localhost:5173"})

	if !policy.Allows("https://app.example.com") {
		t.Fatal("configured production origin should be allowed")
	}
	if !policy.Allows("http://localhost:5173") {
		t.Fatal("configured localhost origin should be allowed")
	}
	if policy.Allows("https://evil.example.com") {
		t.Fatal("unconfigured origin should be rejected")
	}
}

func TestOriginPolicyAllowsConfiguredLocalhostWildcardPorts(t *testing.T) {
	policy := NewOriginPolicy("release", []string{"https://app.example.com", "http://localhost:*", "http://127.0.0.1:*"})

	if !policy.Allows("http://localhost:5173") {
		t.Fatal("localhost wildcard should allow Vite port")
	}
	if !policy.Allows("http://localhost:3000") {
		t.Fatal("localhost wildcard should allow another local port")
	}
	if !policy.Allows("http://127.0.0.1:8080") {
		t.Fatal("127.0.0.1 wildcard should allow local loopback port")
	}
	if policy.Allows("http://evil.example.com:5173") {
		t.Fatal("localhost wildcard should not allow remote hosts")
	}
	if policy.Allows("https://localhost:5173") {
		t.Fatal("http localhost wildcard should not allow https localhost")
	}
}

func TestOriginPolicyWildcardAllowsAnyOrigin(t *testing.T) {
	policy := NewOriginPolicy("release", []string{"*"})

	if !policy.Allows("https://app.example.com") {
		t.Fatal("wildcard origin should allow configured app origin")
	}
	if !policy.Allows("https://evil.example.com") {
		t.Fatal("wildcard origin should allow any remote origin")
	}
	if !policy.Allows("http://localhost:5173") {
		t.Fatal("wildcard origin should allow localhost origin")
	}
}

func TestCORSMiddlewareAllowsConfiguredPreflight(t *testing.T) {
	f := flamego.New()
	f.Use(CORSMiddleware(NewOriginPolicy("release", []string{"https://app.example.com"})))
	f.Get("/api/v1/health", func() string { return "ok" })

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	rec := httptest.NewRecorder()

	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "authorization,content-type" {
		t.Fatalf("allow headers = %q", got)
	}
}

func TestCORSMiddlewareWildcardEchoesRequestOrigin(t *testing.T) {
	f := flamego.New()
	f.Use(CORSMiddleware(NewOriginPolicy("release", []string{"*"})))
	f.Get("/api/v1/health", func() string { return "ok" })

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	req.Header.Set("Origin", "https://any.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	rec := httptest.NewRecorder()

	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://any.example.com" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q", got)
	}
}

func TestCORSMiddlewareRejectsUnconfiguredPreflight(t *testing.T) {
	f := flamego.New()
	f.Use(CORSMiddleware(NewOriginPolicy("release", []string{"https://app.example.com"})))
	f.Get("/api/v1/health", func() string { return "ok" })

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()

	f.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("rejected request allow origin = %q, want empty", got)
	}
}
