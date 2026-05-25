package logx

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// SinkCore wraps a zap core and fans log entries out to additional Sinks.
type SinkCore struct {
	core zapcore.Core
	sink []Sink
}

func NewCoreX(l *zap.Logger, sink []Sink) *SinkCore {
	return &SinkCore{
		core: l.Core(),
		sink: sink,
	}
}

func (c *SinkCore) Enabled(level zapcore.Level) bool {
	return c.core.Enabled(level)
}

func (c *SinkCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *SinkCore) With(fields []zapcore.Field) zapcore.Core {
	return c.core.With(fields)
}

func (c *SinkCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	for _, s := range c.sink {
		s.Write(ent, fields)
	}
	return c.core.Write(ent, fields)
}

func (c *SinkCore) Sync() error {
	return c.core.Sync()
}
