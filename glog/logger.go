package glog

import "context"

// Logger 定义了一个最小化的、可嵌入的日志接口。
type Logger interface {
	Debugf(format string, v ...interface{})
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

// GLogger 是一个功能完备的日志接口，主要用于应用层代码。
type GLogger interface {
	Logger

	With(args ...interface{}) GLogger
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Fatal(msg string, args ...interface{})
	DebugContext(ctx context.Context, msg string, args ...interface{})
	InfoContext(ctx context.Context, msg string, args ...interface{})
	WarnContext(ctx context.Context, msg string, args ...interface{})
	ErrorContext(ctx context.Context, msg string, args ...interface{})

	// SetLevel 动态地改变日志记录器的级别。
	SetLevel(level Level)

	// Config 返回创建此 logger 时使用的配置。
	Config() *Config

	// Sync 将所有缓冲的日志条目刷写到磁盘。
	Sync() error
}
