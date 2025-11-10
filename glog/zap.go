package glog

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type zapLogger struct {
	l           *zap.Logger
	atomicLevel zap.AtomicLevel
	config      *Config
}

func newZapLogger(config *Config) (GLogger, error) {
	writers, err := buildWriters(config)
	if err != nil {
		return nil, err
	}

	syncers := make([]zapcore.WriteSyncer, 0, len(writers))
	for _, writer := range writers {
		syncers = append(syncers, zapcore.AddSync(writer))
	}
	writeSyncer := zapcore.NewMultiWriteSyncer(syncers...)

	encoder := buildEncoder(config)
	atomicLevel := zap.NewAtomicLevelAt(zapcore.Level(config.Level))

	core := zapcore.NewCore(encoder, writeSyncer, atomicLevel)

	options := buildOptions(config)
	l := zap.New(core, options...)

	configCopy := *config
	if config.RotationConfig != nil {
		rotationCopy := *config.RotationConfig
		configCopy.RotationConfig = &rotationCopy
	}
	if config.InitialFields != nil {
		fieldsCopy := make(map[string]interface{})
		for k, v := range config.InitialFields {
			fieldsCopy[k] = v
		}
		configCopy.InitialFields = fieldsCopy
	}
	if config.TimeFormat != "" {
		configCopy.TimeFormat = config.TimeFormat
	}

	return &zapLogger{
		l:           l,
		atomicLevel: atomicLevel,
		config:      &configCopy,
	}, nil
}

func (l *zapLogger) Config() *Config {
	return l.config
}

func (l *zapLogger) SetLevel(level Level) {
	l.atomicLevel.SetLevel(zapcore.Level(level))
	l.config.Level = level
}

func (l *zapLogger) With(args ...interface{}) GLogger {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		l.l.Warn("glog: failed to create structured logger with provided fields", zap.Error(err))
		return &zapLogger{l: l.l, atomicLevel: l.atomicLevel, config: l.config}
	}
	return &zapLogger{l: l.l.With(fields...), atomicLevel: l.atomicLevel, config: l.config}
}

func (l *zapLogger) Debug(msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	l.l.Debug(msg, fields...)
}
func (l *zapLogger) Info(msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	l.l.Info(msg, fields...)
}
func (l *zapLogger) Warn(msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	l.l.Warn(msg, fields...)
}
func (l *zapLogger) Error(msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	l.l.Error(msg, fields...)
}
func (l *zapLogger) Fatal(msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	l.l.Fatal(msg, fields...)
}

func (l *zapLogger) Debugf(format string, v ...interface{}) { l.l.Sugar().Debugf(format, v...) }
func (l *zapLogger) Infof(format string, v ...interface{})  { l.l.Sugar().Infof(format, v...) }
func (l *zapLogger) Warnf(format string, v ...interface{})  { l.l.Sugar().Warnf(format, v...) }
func (l *zapLogger) Errorf(format string, v ...interface{}) { l.l.Sugar().Errorf(format, v...) }

func (l *zapLogger) DebugContext(ctx context.Context, msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	fields = append(fields, l.traceContextFields(ctx)...)
	l.l.Debug(msg, fields...)
}
func (l *zapLogger) InfoContext(ctx context.Context, msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	fields = append(fields, l.traceContextFields(ctx)...)
	l.l.Info(msg, fields...)
}
func (l *zapLogger) WarnContext(ctx context.Context, msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	fields = append(fields, l.traceContextFields(ctx)...)
	l.l.Warn(msg, fields...)
}
func (l *zapLogger) ErrorContext(ctx context.Context, msg string, args ...interface{}) {
	fields, err := l.argsToZapFields(args...)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	fields = append(fields, l.traceContextFields(ctx)...)
	l.l.Error(msg, fields...)
}

func (l *zapLogger) Sync() error {
	return l.l.Sync()
}

func (l *zapLogger) argsToZapFields(args ...interface{}) ([]zap.Field, error) {
	if len(args)%2 != 0 {
		return nil, ErrInvalidKeyValuePairs
	}
	fields := make([]zap.Field, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			return nil, ErrKeyNotString
		}
		fields = append(fields, zap.Any(key, args[i+1]))
	}
	return fields, nil
}

func (l *zapLogger) traceContextFields(ctx context.Context) []zap.Field {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return nil
	}
	spanCtx := span.SpanContext()
	if !spanCtx.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", spanCtx.TraceID().String()),
		zap.String("span_id", spanCtx.SpanID().String()),
	}
}

func buildEncoder(config *Config) zapcore.Encoder {
	var zapEncoderConfig zapcore.EncoderConfig
	if config.Development {
		zapEncoderConfig = zap.NewDevelopmentEncoderConfig()
	} else {
		zapEncoderConfig = zap.NewProductionEncoderConfig()
	}
	if config.EncoderConfig.MessageKey != "" {
		zapEncoderConfig.MessageKey = config.EncoderConfig.MessageKey
	}
	if config.EncoderConfig.LevelKey != "" {
		zapEncoderConfig.LevelKey = config.EncoderConfig.LevelKey
	}
	if config.EncoderConfig.TimeKey != "" {
		zapEncoderConfig.TimeKey = config.EncoderConfig.TimeKey
	}
	if config.EncoderConfig.CallerKey != "" {
		zapEncoderConfig.CallerKey = config.EncoderConfig.CallerKey
	}
	if config.EncoderConfig.StacktraceKey != "" {
		zapEncoderConfig.StacktraceKey = config.EncoderConfig.StacktraceKey
	}
	zapEncoderConfig.EncodeTime = zapcore.EpochMillisTimeEncoder
	if config.TimeFormat != "" {
		zapEncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(config.TimeFormat)
	}
	if config.Encoding == JSONEncoding {
		return zapcore.NewJSONEncoder(zapEncoderConfig)
	}
	return zapcore.NewConsoleEncoder(zapEncoderConfig)
}

func buildOptions(config *Config) []zap.Option {
	var opts []zap.Option
	opts = append(opts, zap.AddCallerSkip(1))
	if !config.DisableCaller {
		opts = append(opts, zap.AddCaller())
	}
	stackLevel := zap.ErrorLevel
	if config.Development {
		stackLevel = zap.WarnLevel
	}
	if !config.DisableStacktrace {
		opts = append(opts, zap.AddStacktrace(stackLevel))
	}
	if len(config.InitialFields) > 0 {
		fields := make([]zap.Field, 0, len(config.InitialFields))
		for k, v := range config.InitialFields {
			fields = append(fields, zap.Any(k, v))
		}
		opts = append(opts, zap.Fields(fields...))
	}
	return opts
}
