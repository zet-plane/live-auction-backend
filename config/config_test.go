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

func TestStorageTOSUploadExpires(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.StorageTOSUploadExpires(); got != 10*time.Minute {
		t.Fatalf("default upload expires = %v, want 10m", got)
	}

	cfg.Storage.TOS.UploadExpires = "5m"
	if got := cfg.StorageTOSUploadExpires(); got != 5*time.Minute {
		t.Fatalf("configured upload expires = %v, want 5m", got)
	}

	cfg.Storage.TOS.UploadExpires = "bad"
	if got := cfg.StorageTOSUploadExpires(); got != 10*time.Minute {
		t.Fatalf("bad upload expires fallback = %v, want 10m", got)
	}
}

func TestStorageTOSImageMaxSizeBytes(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.StorageTOSImageMaxSizeBytes(); got != 10*1024*1024 {
		t.Fatalf("default max size = %d, want 10485760", got)
	}

	cfg.Storage.TOS.ImageMaxSizeBytes = 2 * 1024 * 1024
	if got := cfg.StorageTOSImageMaxSizeBytes(); got != 2*1024*1024 {
		t.Fatalf("configured max size = %d, want 2097152", got)
	}

	cfg.Storage.TOS.ImageMaxSizeBytes = -1
	if got := cfg.StorageTOSImageMaxSizeBytes(); got != 10*1024*1024 {
		t.Fatalf("negative max size fallback = %d, want 10485760", got)
	}
}

func TestAvailabilityDurationFallbacks(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.AvailabilityRedisProbeInterval(); got != time.Second {
		t.Fatalf("redis probe fallback = %v, want 1s", got)
	}
	if got := cfg.AvailabilityRedisFailoverThreshold(); got != 3*time.Second {
		t.Fatalf("redis failover fallback = %v, want 3s", got)
	}
	if got := cfg.MySQLBufferingWindow(); got != 10*time.Second {
		t.Fatalf("mysql buffering fallback = %v, want 10s", got)
	}
}
