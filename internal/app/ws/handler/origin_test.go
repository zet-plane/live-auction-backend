package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zet-plane/live-auction-backend/internal/middleware/web"
)

func TestConfigureOriginCheckerRejectsUnconfiguredWebSocketOrigin(t *testing.T) {
	ConfigureOriginChecker(web.NewOriginPolicy("release", []string{"https://app.example.com"}))
	t.Cleanup(func() { ConfigureOriginChecker(web.NewOriginPolicy("debug", nil)) })

	req := httptest.NewRequest(http.MethodGet, "/ws/v1/rooms/room_1", nil)
	req.Header.Set("Origin", "https://evil.example.com")

	if upgrader.CheckOrigin(req) {
		t.Fatal("unconfigured websocket origin should be rejected")
	}
}

func TestConfigureOriginCheckerAllowsConfiguredWebSocketOrigin(t *testing.T) {
	ConfigureOriginChecker(web.NewOriginPolicy("release", []string{"https://app.example.com"}))
	t.Cleanup(func() { ConfigureOriginChecker(web.NewOriginPolicy("debug", nil)) })

	req := httptest.NewRequest(http.MethodGet, "/ws/v1/rooms/room_1", nil)
	req.Header.Set("Origin", "https://app.example.com")

	if !upgrader.CheckOrigin(req) {
		t.Fatal("configured websocket origin should be allowed")
	}
}

func TestConfigureOriginCheckerWildcardAllowsAnyWebSocketOrigin(t *testing.T) {
	ConfigureOriginChecker(web.NewOriginPolicy("release", []string{"*"}))
	t.Cleanup(func() { ConfigureOriginChecker(web.NewOriginPolicy("debug", nil)) })

	req := httptest.NewRequest(http.MethodGet, "/ws/v1/rooms/room_1", nil)
	req.Header.Set("Origin", "https://any.example.com")

	if !upgrader.CheckOrigin(req) {
		t.Fatal("wildcard websocket origin should be allowed")
	}
}
