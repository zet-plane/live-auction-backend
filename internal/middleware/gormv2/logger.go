package gormv2

import (
	"context"
	"fmt"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

type Logger struct {
	LogLevel                  gormlogger.LogLevel
	SlowThreshold             time.Duration
	IgnoreRecordNotFoundError bool
}

func NewLogger() Logger {
	return Logger{
		LogLevel:                  gormlogger.Warn,
		SlowThreshold:             100 * time.Millisecond,
		IgnoreRecordNotFoundError: true,
	}
}

func (l Logger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	l.LogLevel = level
	return l
}

func (l Logger) Info(_ context.Context, msg string, args ...any) {
	if l.LogLevel >= gormlogger.Info {
		fmt.Printf("[GORM] "+msg+"\n", args...)
	}
}

func (l Logger) Warn(_ context.Context, msg string, args ...any) {
	if l.LogLevel >= gormlogger.Warn {
		fmt.Printf("[GORM WARN] "+msg+"\n", args...)
	}
}

func (l Logger) Error(_ context.Context, msg string, args ...any) {
	if l.LogLevel >= gormlogger.Error {
		fmt.Printf("[GORM ERROR] "+msg+"\n", args...)
	}
}

func (l Logger) Trace(_ context.Context, begin time.Time, fc func() (sql string, rows int64), err error) {
	if l.LogLevel <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()
	switch {
	case err != nil && l.LogLevel >= gormlogger.Error:
		fmt.Printf("[GORM ERROR] %s | %d rows | %v | err=%v\n", sql, rows, elapsed, err)
	case l.SlowThreshold > 0 && elapsed > l.SlowThreshold && l.LogLevel >= gormlogger.Warn:
		fmt.Printf("[GORM SLOW] %s | %d rows | %v\n", sql, rows, elapsed)
	case l.LogLevel >= gormlogger.Info:
		fmt.Printf("[GORM] %s | %d rows | %v\n", sql, rows, elapsed)
	}
}
