package observability

import (
	"context"
	"testing"
	"time"

	"github.com/zet-plane/live-auction-backend/config"
)

func TestSetupDisabledReturnsNoopShutdown(t *testing.T) {
	shutdown, err := Setup(context.Background(), config.Observability{Enabled: false})
	if err != nil {
		t.Fatalf("Setup disabled returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown is nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func TestNormalizeConfigDefaults(t *testing.T) {
	cfg := NormalizeConfig(config.Observability{})
	if cfg.ServiceName != "live-auction-backend" {
		t.Fatalf("service name = %q", cfg.ServiceName)
	}
	if cfg.Environment != "local" {
		t.Fatalf("environment = %q", cfg.Environment)
	}
	if cfg.OTLPEndpoint != "127.0.0.1:4317" {
		t.Fatalf("endpoint = %q", cfg.OTLPEndpoint)
	}
	if cfg.TraceSampleRatio != 1 {
		t.Fatalf("sample ratio = %v", cfg.TraceSampleRatio)
	}
	if metricsInterval(cfg) != 15*time.Second {
		t.Fatalf("metrics interval = %v, want 15s", metricsInterval(cfg))
	}
}

func TestResourceMergesWithDefaultResource(t *testing.T) {
	res, err := newResource(config.Observability{
		ServiceName:    "live-auction-backend",
		ServiceVersion: "0.1.0",
		Environment:    "local",
	})
	if err != nil {
		t.Fatalf("newResource returned error: %v", err)
	}
	if res == nil {
		t.Fatal("resource is nil")
	}
}
