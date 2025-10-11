package glog

import (
	"context"
	"io"
)

// Level 日志级别
type Level int8

const (
	DebugLevel Level = iota - 1
	InfoLevel
	WarnLevel
	ErrorLevel
	DPanicLevel
	PanicLevel
	FatalLevel
)

type Encoding string

const (
	JSONEncoding    Encoding = "json"
	ConsoleEncoding          = "console"
)

// OutputType 输出类型
type OutputType string

const (
	StdoutOutput OutputType = "stdout"
	StderrOutput OutputType = "stderr"
	FileOutput   OutputType = "file"
)

// Field 结构化日志字段
type Field struct {
	Key   string
	Value interface{}
}

// CoreLogger 核心日志接口
type CoreLogger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	DPanic(msg string, fields ...Field)
	Panic(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)

	Debugw(msg string, keysAndValues ...interface{})
	Infow(msg string, keysAndValues ...interface{})
	Warnw(msg string, keysAndValues ...interface{})
	Errorw(msg string, keysAndValues ...interface{})
	DPanicw(msg string, keysAndValues ...interface{})
	Panicw(msg string, keysAndValues ...interface{})
	Fatalw(msg string, keysAndValues ...interface{})

	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	DPanicf(format string, args ...interface{})
	Panicf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})

	DebugContext(ctx context.Context, msg string, fields ...Field)
	InfoContext(ctx context.Context, msg string, fields ...Field)
	WarnContext(ctx context.Context, msg string, fields ...Field)
	ErrorContext(ctx context.Context, msg string, fields ...Field)

	WithCallerSkip(skip int) CoreLogger
	WithOptions(opts ...Option) CoreLogger
	With(fields ...Field) CoreLogger

	Module(name string) CoreLogger

	Sync() error
}

// Logger 扩展日志接口，添加配置和管理功能
type Logger interface {
	CoreLogger

	SetLevel(lvl Level)
	GetLevel() Level

	SetOutput(w io.Writer)
	AddOutput(w io.Writer)

	Rotate() error

	Clone() Logger
}

// Option 配置选项
type Option interface {
	Apply(*Config)
}

// EncoderConfig 编码器配置
type EncoderConfig struct {
	MessageKey     string
	LevelKey       string
	TimeKey        string
	NameKey        string
	CallerKey      string
	FunctionKey    string
	StacktraceKey  string
	EncodeLevel    string
	EncodeTime     string
	EncodeDuration string
}

// Config 日志配置
type Config struct {
	CallerSkip        int
	Level             Level
	Development       bool
	DisableCaller     bool
	DisableStacktrace bool
	Sampling          *SamplingConfig
	Encoding          Encoding
	EncoderConfig     EncoderConfig
	OutputPaths       []OutputType
	ErrorOutputPaths  []string
	InitialFields     map[string]interface{}

	RotationConfig *RotationConfig
}

// SamplingConfig 采样配置
type SamplingConfig struct {
	Initial    int
	Thereafter int
}

// RotationConfig 轮转配置
type RotationConfig struct {
	MaxSize    int // MB
	MaxAge     int // days
	MaxBackups int
	LocalTime  bool
	Compress   bool
}

// OtelConfig OpenTelemetry配置
type OtelConfig struct {
	Enabled     bool
	ServiceName string
	Endpoint    string
}
