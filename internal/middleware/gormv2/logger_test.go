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

func TestSQLMetadata(t *testing.T) {
	cases := []struct {
		sql   string
		op    string
		table string
	}{
		{"SELECT * FROM auction_items WHERE id = ?", "SELECT", "auction_items"},
		{"INSERT INTO bid_logs (`id`) VALUES (?)", "INSERT", "bid_logs"},
		{"UPDATE orders SET status = ?", "UPDATE", "orders"},
		{"DELETE FROM deposits WHERE id = ?", "DELETE", "deposits"},
	}
	for _, tt := range cases {
		if got := operationFromSQL(tt.sql); got != tt.op {
			t.Fatalf("operationFromSQL(%q) = %q, want %q", tt.sql, got, tt.op)
		}
		if got := tableFromSQL(tt.sql); got != tt.table {
			t.Fatalf("tableFromSQL(%q) = %q, want %q", tt.sql, got, tt.table)
		}
	}
}
