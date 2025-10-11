package glog

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// zapLogger
type zapLogger struct {
	zapLogger *zap.Logger
	config    *Config
	fields    []zap.Field
	mu        sync.RWMutex
}

// NewLogger 创建新的Logger实例
func NewLogger(config *Config) (Logger, error) {
	writers, err := buildWriters(config)
	if err != nil {
		return nil, err
	}

	// 使用多个writer
	var coreWriter io.Writer
	if len(writers) == 1 {
		coreWriter = writers[0]
	} else {
		coreWriter = io.MultiWriter(writers...)
	}

	zapConfig := buildZapConfig(config)

	core := zapcore.NewCore(
		buildEncoder(config),
		zapcore.AddSync(coreWriter),
		zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl >= zapcore.Level(config.Level)
		}),
	)

	options := buildOptions(zapConfig)
	options = append(options, zap.AddCallerSkip(config.CallerSkip))
	l := zap.New(core, options...)
	zap.AddCallerSkip(config.CallerSkip)

	return &zapLogger{
		zapLogger: l,
		config:    config,
	}, nil
}

func (l *zapLogger) WithCallerSkip(skip int) CoreLogger {
	// 创建新的zap logger，添加调用者跳过选项
	newZapLogger := l.zapLogger.WithOptions(zap.AddCallerSkip(skip))

	// 返回新的logger实例
	newLogger := *l
	newLogger.zapLogger = newZapLogger
	return &newLogger
}

func (l *zapLogger) Debug(msg string, fields ...Field) {
	l.zapLogger.Debug(msg, l.convertFields(fields)...)
}

func (l *zapLogger) Info(msg string, fields ...Field) {
	l.zapLogger.Info(msg, l.convertFields(fields)...)
}

func (l *zapLogger) Warn(msg string, fields ...Field) {
	l.zapLogger.Warn(msg, l.convertFields(fields)...)
}

func (l *zapLogger) Error(msg string, fields ...Field) {
	l.zapLogger.Error(msg, l.convertFields(fields)...)
}

func (l *zapLogger) DPanic(msg string, fields ...Field) {
	l.zapLogger.DPanic(msg, l.convertFields(fields)...)
}

func (l *zapLogger) Panic(msg string, fields ...Field) {
	l.zapLogger.Panic(msg, l.convertFields(fields)...)
}

func (l *zapLogger) Fatal(msg string, fields ...Field) {
	l.zapLogger.Fatal(msg, l.convertFields(fields)...)
}

func (l *zapLogger) Debugw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Debugw(msg, keysAndValues...)
}

func (l *zapLogger) Infow(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Infow(msg, keysAndValues...)
}

func (l *zapLogger) Warnw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Warnw(msg, keysAndValues...)
}

func (l *zapLogger) Errorw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Errorw(msg, keysAndValues...)
}

func (l *zapLogger) DPanicw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().DPanicw(msg, keysAndValues...)
}

func (l *zapLogger) Panicw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Panicw(msg, keysAndValues...)
}

func (l *zapLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Fatalw(msg, keysAndValues...)
}

// Debugf 使用格式化字符串记录 debug 级别日志
func (l *zapLogger) Debugf(format string, args ...interface{}) {
	if l.zapLogger.Core().Enabled(zapcore.DebugLevel) {
		l.zapLogger.Debug(fmt.Sprintf(format, args...))
	}
}

// Infof 使用格式化字符串记录 info 级别日志
func (l *zapLogger) Infof(format string, args ...interface{}) {
	if l.zapLogger.Core().Enabled(zapcore.InfoLevel) {
		l.zapLogger.Info(fmt.Sprintf(format, args...))
	}
}

// Warnf 使用格式化字符串记录 warn 级别日志
func (l *zapLogger) Warnf(format string, args ...interface{}) {
	if l.zapLogger.Core().Enabled(zapcore.WarnLevel) {
		l.zapLogger.Warn(fmt.Sprintf(format, args...))
	}
}

// Errorf 使用格式化字符串记录 error 级别日志
func (l *zapLogger) Errorf(format string, args ...interface{}) {
	if l.zapLogger.Core().Enabled(zapcore.ErrorLevel) {
		l.zapLogger.Error(fmt.Sprintf(format, args...))
	}
}

// DPanicf 使用格式化字符串记录 dpanic 级别日志
func (l *zapLogger) DPanicf(format string, args ...interface{}) {
	if l.zapLogger.Core().Enabled(zapcore.DPanicLevel) {
		l.zapLogger.DPanic(fmt.Sprintf(format, args...))
	}
}

// Panicf 使用格式化字符串记录 panic 级别日志
func (l *zapLogger) Panicf(format string, args ...interface{}) {
	if l.zapLogger.Core().Enabled(zapcore.PanicLevel) {
		l.zapLogger.Panic(fmt.Sprintf(format, args...))
	}
}

// Fatalf 使用格式化字符串记录 fatal 级别日志
func (l *zapLogger) Fatalf(format string, args ...interface{}) {
	if l.zapLogger.Core().Enabled(zapcore.FatalLevel) {
		l.zapLogger.Fatal(fmt.Sprintf(format, args...))
	}
}

func (l *zapLogger) DebugContext(ctx context.Context, msg string, fields ...Field) {
	fields = append(fields, l.traceContextFields(ctx)...)
	l.Debug(msg, fields...)
}

func (l *zapLogger) InfoContext(ctx context.Context, msg string, fields ...Field) {
	fields = append(fields, l.traceContextFields(ctx)...)
	l.Info(msg, fields...)
}

func (l *zapLogger) WarnContext(ctx context.Context, msg string, fields ...Field) {
	fields = append(fields, l.traceContextFields(ctx)...)
	l.Warn(msg, fields...)
}

func (l *zapLogger) ErrorContext(ctx context.Context, msg string, fields ...Field) {
	fields = append(fields, l.traceContextFields(ctx)...)
	l.Error(msg, fields...)
}

func (l *zapLogger) WithOptions(opts ...Option) CoreLogger {
	return l
}

func (l *zapLogger) With(fields ...Field) CoreLogger {
	newLogger := *l
	newLogger.fields = append(newLogger.fields, l.convertFields(fields)...)
	newLogger.zapLogger = l.zapLogger.With(l.convertFields(fields)...)
	return &newLogger
}

func (l *zapLogger) Sync() error {
	return l.zapLogger.Sync()
}

func (l *zapLogger) SetLevel(lvl Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.config.Level = lvl
	// 注意：实际应用中需要重新构建logger核心
}

func (l *zapLogger) GetLevel() Level {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config.Level
}

func (l *zapLogger) SetOutput(w io.Writer) {
	// 简化实现，实际需要重建logger核心
}

func (l *zapLogger) AddOutput(w io.Writer) {
	// 简化实现
}

func (l *zapLogger) Rotate() error {
	// 如果使用lumberjack，则调用其Rotate方法
	// 这里简化处理
	return nil
}

// Clone 创建一个新的logger实例，不影响原logger
func (l *zapLogger) Clone() Logger {
	// 创建配置的深拷贝
	configCopy := *l.config

	// 构建writers
	writers, err := buildWriters(&configCopy)
	if err != nil {
		// 如果构建失败，使用默认stdout
		writers = []io.Writer{os.Stdout}
	}

	// 使用多个writer
	var coreWriter io.Writer
	if len(writers) == 1 {
		coreWriter = writers[0]
	} else {
		coreWriter = io.MultiWriter(writers...)
	}

	// 创建新的zap logger实例
	zapConfig := buildZapConfig(&configCopy)
	core := zapcore.NewCore(
		buildEncoder(&configCopy),
		zapcore.AddSync(coreWriter),
		zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl >= zapcore.Level(configCopy.Level)
		}),
	)

	newZapLogger := zap.New(core, buildOptions(zapConfig)...)
	zap.AddCallerSkip(configCopy.CallerSkip)

	return &zapLogger{
		zapLogger: newZapLogger,
		config:    &configCopy,
		fields:    append([]zap.Field(nil), l.fields...), // 拷贝现有字段
	}
}

// Module 设置模块名称作为固定前缀
func (l *zapLogger) Module(name string) CoreLogger {
	moduleField := Field{Key: "module", Value: name}
	return l.With(moduleField)
}

func (l *zapLogger) convertFields(fields []Field) []zap.Field {
	zapFields := make([]zap.Field, len(fields))
	for i, f := range fields {
		zapFields[i] = zap.Any(f.Key, f.Value)
	}
	return zapFields
}

func (l *zapLogger) traceContextFields(ctx context.Context) []Field {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return nil
	}

	ctxFields := []Field{
		{Key: "trace_id", Value: span.SpanContext().TraceID()},
		{Key: "span_id", Value: span.SpanContext().SpanID()},
	}

	return ctxFields
}

func buildZapConfig(config *Config) zap.Config {
	zapConfig := zap.NewProductionConfig()
	if config.Development {
		zapConfig = zap.NewDevelopmentConfig()
	}

	zapConfig.Level = zap.NewAtomicLevelAt(zapcore.Level(config.Level))
	zapConfig.DisableCaller = config.DisableCaller
	zapConfig.DisableStacktrace = config.DisableStacktrace
	zapConfig.Encoding = string(config.Encoding)

	if config.EncoderConfig.MessageKey != "" {
		zapConfig.EncoderConfig.MessageKey = config.EncoderConfig.MessageKey
	}
	if config.EncoderConfig.LevelKey != "" {
		zapConfig.EncoderConfig.LevelKey = config.EncoderConfig.LevelKey
	}
	return zapConfig
}

func buildEncoder(config *Config) zapcore.Encoder {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	if config.Encoding == JSONEncoding {
		return zapcore.NewJSONEncoder(encoderConfig)
	}
	return zapcore.NewConsoleEncoder(encoderConfig)
}

func buildOptions(zapConfig zap.Config) []zap.Option {
	var opts []zap.Option

	if zapConfig.Development {
		opts = append(opts, zap.Development())
	}

	if !zapConfig.DisableCaller {
		opts = append(opts, zap.AddCaller())
	}

	stackLevel := zap.ErrorLevel
	if zapConfig.Development {
		stackLevel = zap.WarnLevel
	}
	if !zapConfig.DisableStacktrace {
		opts = append(opts, zap.AddStacktrace(zapcore.Level(stackLevel)))
	}

	if zapConfig.Sampling != nil {
		opts = append(opts, zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewSamplerWithOptions(
				core,
				time.Second,
				zapConfig.Sampling.Initial,
				zapConfig.Sampling.Thereafter,
			)
		}))
	}

	return opts
}
