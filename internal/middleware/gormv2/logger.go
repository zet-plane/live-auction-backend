package gormv2

import (
	"context"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/internal/core/observability"
	"github.com/zet-plane/live-auction-backend/pkg/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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

func (l Logger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rows int64), err error) {
	if l.LogLevel <= gormlogger.Silent {
		return
	}
	ctx, span := otel.Tracer("github.com/zet-plane/live-auction-backend/gorm").Start(ctx, "mysql.query")
	defer span.End()
	elapsed := time.Since(begin)
	sql, rows := fc()
	operation := operationFromSQL(sql)
	table := tableFromSQL(sql)
	result := "success"
	if err != nil {
		result = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.SetAttributes(
		attribute.String("db.system", "mysql"),
		attribute.String("db.operation", operation),
		attribute.String("db.sql.table", table),
		attribute.Int64("db.rows_affected", rows),
	)
	observability.DefaultRecorder().DBQuery(ctx, observability.DBQueryMetric{
		Operation: operation,
		Table:     table,
		Result:    result,
		Slow:      l.SlowThreshold > 0 && elapsed > l.SlowThreshold,
		Duration:  elapsed,
	})
	switch {
	case err != nil && l.LogLevel >= gormlogger.Error:
		logx.Errorw("[GORM] query error", "sql", sql, "rows", rows, "elapsed", elapsed, "err", err)
	case l.SlowThreshold > 0 && elapsed > l.SlowThreshold && l.LogLevel >= gormlogger.Warn:
		logx.Warnw("[GORM] slow query", "sql", sql, "rows", rows, "elapsed", elapsed)
	case l.LogLevel >= gormlogger.Info:
		logx.Infow("[GORM] query", "sql", sql, "rows", rows, "elapsed", elapsed)
	}
}

func operationFromSQL(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "unknown"
	}
	return strings.ToUpper(fields[0])
}

func tableFromSQL(sql string) string {
	clean := strings.ReplaceAll(sql, "`", "")
	fields := strings.Fields(clean)
	for i, f := range fields {
		upper := strings.ToUpper(f)
		if (upper == "FROM" || upper == "INTO" || upper == "UPDATE") && i+1 < len(fields) {
			return strings.Trim(fields[i+1], ",")
		}
	}
	if len(fields) > 2 && strings.ToUpper(fields[0]) == "DELETE" && strings.ToUpper(fields[1]) == "FROM" {
		return strings.Trim(fields[2], ",")
	}
	return "unknown"
}
