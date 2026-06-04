package item

import (
	"errors"
	"testing"
)

func TestBidLogConsumerNameUsesHostname(t *testing.T) {
	name := bidLogConsumerName(func() (string, error) {
		return "pod-abc123", nil
	})
	if name != "backend-pod-abc123" {
		t.Fatalf("expected hostname-based consumer name, got %q", name)
	}
}

func TestBidLogConsumerNameFallsBackWhenHostnameUnavailable(t *testing.T) {
	name := bidLogConsumerName(func() (string, error) {
		return "", errors.New("hostname unavailable")
	})
	if name != "backend-1" {
		t.Fatalf("expected fallback consumer name, got %q", name)
	}
}
