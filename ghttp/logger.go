package ghttp

import "context"

// Logger 是 ghttp 使用的日志接口。
// Logger is the logging interface used by ghttp.
type Logger interface {
	DebugContext(ctx context.Context, msg string, args ...interface{})
	InfoContext(ctx context.Context, msg string, args ...interface{})
	WarnContext(ctx context.Context, msg string, args ...interface{})
	ErrorContext(ctx context.Context, msg string, args ...interface{})
}
