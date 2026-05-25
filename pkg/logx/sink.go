package logx

import "go.uber.org/zap/zapcore"

// Sink is a secondary log destination (e.g. remote log service).
type Sink interface {
	Open()
	Close()
	Write(ent zapcore.Entry, fields []zapcore.Field)
}
