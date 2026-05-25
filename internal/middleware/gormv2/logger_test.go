package gormv2

import (
	"testing"

	gormlogger "gorm.io/gorm/logger"
)

func TestNewLoggerDefaultsToInfoLevel(t *testing.T) {
	logger := NewLogger()
	if logger.LogLevel != gormlogger.Info {
		t.Fatalf("LogLevel = %v, want %v", logger.LogLevel, gormlogger.Info)
	}
}
