package gormv2

import (
	"context"
	"time"

	"github.com/zet-plane/live-auction-backend/pkg/logx"
	gormlogger "gorm.io/gorm/logger"
)

type Logger struct {
	LogLevel                  gormlogger.LogLevel
	SlowThreshold             time.Duration
	IgnoreRecordNotFoundError bool
}

func NewLogger() Logger {
	return Logger{
		LogLevel:                  gormlogger.Info,
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
		logx.Infof("[GORM] "+msg, args...)
	}
}

func (l Logger) Warn(_ context.Context, msg string, args ...any) {
	if l.LogLevel >= gormlogger.Warn {
		logx.Warnf("[GORM] "+msg, args...)
	}
}

func (l Logger) Error(_ context.Context, msg string, args ...any) {
	if l.LogLevel >= gormlogger.Error {
		logx.Errorf("[GORM] "+msg, args...)
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
		logx.Errorw("[GORM] query error", "sql", sql, "rows", rows, "elapsed", elapsed, "err", err)
	case l.SlowThreshold > 0 && elapsed > l.SlowThreshold && l.LogLevel >= gormlogger.Warn:
		logx.Warnw("[GORM] slow query", "sql", sql, "rows", rows, "elapsed", elapsed)
	case l.LogLevel >= gormlogger.Info:
		logx.Infow("[GORM] query", "sql", sql, "rows", rows, "elapsed", elapsed)
	}
}
