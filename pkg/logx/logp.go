package logx

import (
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Option func(*LogP)

type LogP struct {
	logger  *zap.Logger
	zapConf *zap.Config
	sinks   []Sink
}

var (
	rootLogger *LogP
	setupOnce  sync.Once
	nopLogger  = zap.NewNop().Sugar()
)

func WithZapConfig(config *zap.Config) Option {
	return func(p *LogP) {
		p.zapConf = config
	}
}

func WithSink(s ...Sink) Option {
	return func(p *LogP) {
		p.sinks = append(p.sinks, s...)
	}
}

// SetUp initialises the global logger. Safe to call multiple times; only the
// first call takes effect.
func SetUp(opts ...Option) *LogP {
	setupOnce.Do(func() {
		rootLogger = &LogP{}
		for _, opt := range opts {
			opt(rootLogger)
		}
		if rootLogger.zapConf == nil {
			rootLogger.zapConf = defaultConfig()
		}
		log, err := rootLogger.zapConf.Build(
			zap.AddCallerSkip(1),
			zap.AddStacktrace(zapcore.DPanicLevel),
		)
		if err != nil {
			panic(err)
		}
		rootLogger.logger = log
		if len(rootLogger.sinks) > 0 {
			for _, s := range rootLogger.sinks {
				s.Open()
			}
			core := NewCoreX(rootLogger.logger, rootLogger.sinks)
			rootLogger.logger = zap.New(
				core,
				zap.AddCaller(),
				zap.AddCallerSkip(1),
				zap.AddStacktrace(zapcore.DPanicLevel),
			)
		}
	})
	return rootLogger
}

// Stop flushes buffered log entries and closes all sinks. Call at shutdown.
func Stop() {
	if rootLogger == nil {
		return
	}
	for _, s := range rootLogger.sinks {
		s.Close()
	}
	_ = rootLogger.logger.Sync()
}

// SetLevel changes the log level at runtime.
func SetLevel(level zapcore.Level) {
	if rootLogger == nil {
		SetUp()
	}
	rootLogger.zapConf.Level.SetLevel(level)
}

func defaultConfig() *zap.Config {
	return &zap.Config{
		Level:            zap.NewAtomicLevelAt(zapcore.InfoLevel),
		Development:      false,
		Encoding:         "console",
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.CapitalColorLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
	}
}

func sugar() *zap.SugaredLogger {
	if rootLogger == nil || rootLogger.logger == nil {
		return nopLogger
	}
	return rootLogger.logger.Sugar()
}

// Track logs a service operation start and returns a completion logger.
func Track(op string, fields ...any) func(*error, ...any) {
	start := time.Now()
	base := append([]any{"op", op}, fields...)
	Infow("service started", base...)
	return func(errp *error, extra ...any) {
		kv := make([]any, 0, len(base)+len(extra)+4)
		kv = append(kv, base...)
		kv = append(kv, extra...)
		kv = append(kv, "elapsed", time.Since(start))
		if errp != nil && *errp != nil {
			kv = append(kv, "err", *errp)
			Warnw("service failed", kv...)
			return
		}
		Infow("service completed", kv...)
	}
}

// Package-level logging functions — these call the global root logger.

func Debug(v ...any)                 { sugar().Debug(v...) }
func Info(v ...any)                  { sugar().Info(v...) }
func Warn(v ...any)                  { sugar().Warn(v...) }
func Error(v ...any)                 { sugar().Error(v...) }
func Panic(v ...any)                 { sugar().Panic(v...) }
func Fatal(v ...any)                 { sugar().Fatal(v...) }
func Debugf(format string, v ...any) { sugar().Debugf(format, v...) }
func Infof(format string, v ...any)  { sugar().Infof(format, v...) }
func Warnf(format string, v ...any)  { sugar().Warnf(format, v...) }
func Errorf(format string, v ...any) { sugar().Errorf(format, v...) }
func Panicf(format string, v ...any) { sugar().Panicf(format, v...) }
func Fatalf(format string, v ...any) { sugar().Fatalf(format, v...) }
func Debugw(msg string, kv ...any)   { sugar().Debugw(msg, kv...) }
func Infow(msg string, kv ...any)    { sugar().Infow(msg, kv...) }
func Warnw(msg string, kv ...any)    { sugar().Warnw(msg, kv...) }
func Errorw(msg string, kv ...any)   { sugar().Errorw(msg, kv...) }
func Panicw(msg string, kv ...any)   { sugar().Panicw(msg, kv...) }
func Fatalw(msg string, kv ...any)   { sugar().Fatalw(msg, kv...) }
