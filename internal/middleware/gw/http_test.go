package gw

import (
	"net/url"
	"testing"
)

func TestSanitizeRequestURIRedactsSensitiveQueryValues(t *testing.T) {
	u, err := url.Parse("/ws/v1/rooms/room_123?ticket=secret-ticket&token=secret-token&page=1")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	got := sanitizeRequestURI(u)
	want := "/ws/v1/rooms/room_123?page=1&ticket=REDACTED&token=REDACTED"
	if got != want {
		t.Fatalf("sanitizeRequestURI() = %q, want %q", got, want)
	}
}

func TestSanitizeRequestURILeavesPathWithoutQueryUnchanged(t *testing.T) {
	u, err := url.Parse("/api/v1/rooms")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	got := sanitizeRequestURI(u)
	if got != "/api/v1/rooms" {
		t.Fatalf("sanitizeRequestURI() = %q, want path unchanged", got)
	}
}
