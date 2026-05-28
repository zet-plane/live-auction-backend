package config

import (
	"testing"
	"time"
)

func TestObservabilityMetricsInterval(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.ObservabilityMetricsInterval(); got != 15*time.Second {
		t.Fatalf("default interval = %v, want 15s", got)
	}

	cfg.Observability.MetricsInterval = "30s"
	if got := cfg.ObservabilityMetricsInterval(); got != 30*time.Second {
		t.Fatalf("configured interval = %v, want 30s", got)
	}

	cfg.Observability.MetricsInterval = "bad"
	if got := cfg.ObservabilityMetricsInterval(); got != 15*time.Second {
		t.Fatalf("bad interval fallback = %v, want 15s", got)
	}
}
