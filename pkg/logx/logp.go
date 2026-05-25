package logx

import (
	"sync"

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

// Package-level logging functions — these call the global root logger.

func Debug(v ...any)                       { rootLogger.logger.Sugar().Debug(v...) }
func Info(v ...any)                        { rootLogger.logger.Sugar().Info(v...) }
func Warn(v ...any)                        { rootLogger.logger.Sugar().Warn(v...) }
func Error(v ...any)                       { rootLogger.logger.Sugar().Error(v...) }
func Panic(v ...any)                       { rootLogger.logger.Sugar().Panic(v...) }
func Fatal(v ...any)                       { rootLogger.logger.Sugar().Fatal(v...) }
func Debugf(format string, v ...any)       { rootLogger.logger.Sugar().Debugf(format, v...) }
func Infof(format string, v ...any)        { rootLogger.logger.Sugar().Infof(format, v...) }
func Warnf(format string, v ...any)        { rootLogger.logger.Sugar().Warnf(format, v...) }
func Errorf(format string, v ...any)       { rootLogger.logger.Sugar().Errorf(format, v...) }
func Panicf(format string, v ...any)       { rootLogger.logger.Sugar().Panicf(format, v...) }
func Fatalf(format string, v ...any)       { rootLogger.logger.Sugar().Fatalf(format, v...) }
func Debugw(msg string, kv ...any)         { rootLogger.logger.Sugar().Debugw(msg, kv...) }
func Infow(msg string, kv ...any)          { rootLogger.logger.Sugar().Infow(msg, kv...) }
func Warnw(msg string, kv ...any)          { rootLogger.logger.Sugar().Warnw(msg, kv...) }
func Errorw(msg string, kv ...any)         { rootLogger.logger.Sugar().Errorw(msg, kv...) }
func Panicw(msg string, kv ...any)         { rootLogger.logger.Sugar().Panicw(msg, kv...) }
func Fatalw(msg string, kv ...any)         { rootLogger.logger.Sugar().Fatalw(msg, kv...) }
