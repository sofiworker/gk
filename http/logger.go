package ghttp

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"strings"
)

// Logger 是 ghttp 的日志契约：由调用方注入的最小接口。方法集与 ghttp/client 的 Logger 一致，
// 同一个实现（如 glog 适配器）可以同时服务 server 与 client。
// Logger is ghttp's logging contract: a minimal interface injected by the caller. Its
// method set matches ghttp/client's Logger, so one implementation (a glog adapter, say)
// serves both the server and the client.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// NewSlogLogger 把 *slog.Logger 适配为 Logger；l 为 nil 时使用 slog.Default()。
// NewSlogLogger adapts a *slog.Logger to Logger; nil uses slog.Default().
func NewSlogLogger(l *slog.Logger) Logger {
	if l == nil {
		l = slog.Default()
	}
	return slogLogger{l: l}
}

// slogLogger 是基于 slog 的 Logger 实现。
// slogLogger is the slog-backed Logger implementation.
type slogLogger struct{ l *slog.Logger }

func (s slogLogger) log(level slog.Level, format string, args []any) {
	ctx := context.Background()
	if !s.l.Enabled(ctx, level) {
		return
	}
	s.l.Log(ctx, level, fmt.Sprintf(format, args...))
}

// Debugf 实现 Logger。
// Debugf implements Logger.
func (s slogLogger) Debugf(format string, args ...any) { s.log(slog.LevelDebug, format, args) }

// Infof 实现 Logger。
// Infof implements Logger.
func (s slogLogger) Infof(format string, args ...any) { s.log(slog.LevelInfo, format, args) }

// Warnf 实现 Logger。
// Warnf implements Logger.
func (s slogLogger) Warnf(format string, args ...any) { s.log(slog.LevelWarn, format, args) }

// Errorf 实现 Logger。
// Errorf implements Logger.
func (s slogLogger) Errorf(format string, args ...any) { s.log(slog.LevelError, format, args) }

// NewStdLogger 把标准库 *log.Logger 适配为 Logger，级别以 "[LEVEL] " 前缀区分；
// l 为 nil 时使用 log.Default()。
// NewStdLogger adapts a standard *log.Logger to Logger, marking levels with a "[LEVEL] "
// prefix; nil uses log.Default().
func NewStdLogger(l *log.Logger) Logger {
	if l == nil {
		l = log.Default()
	}
	return stdLogger{l: l}
}

// stdLogger 是基于 *log.Logger 的 Logger 实现。
// stdLogger is the *log.Logger-backed Logger implementation.
type stdLogger struct{ l *log.Logger }

// Debugf 实现 Logger。
// Debugf implements Logger.
func (s stdLogger) Debugf(format string, args ...any) { s.l.Printf("[DEBUG] "+format, args...) }

// Infof 实现 Logger。
// Infof implements Logger.
func (s stdLogger) Infof(format string, args ...any) { s.l.Printf("[INFO] "+format, args...) }

// Warnf 实现 Logger。
// Warnf implements Logger.
func (s stdLogger) Warnf(format string, args ...any) { s.l.Printf("[WARN] "+format, args...) }

// Errorf 实现 Logger。
// Errorf implements Logger.
func (s stdLogger) Errorf(format string, args ...any) { s.l.Printf("[ERROR] "+format, args...) }

// NopLogger 返回丢弃所有日志的 Logger。
// NopLogger returns a Logger that discards everything.
func NopLogger() Logger { return nopLogger{} }

// nopLogger 丢弃所有日志。
// nopLogger discards everything.
type nopLogger struct{}

func (nopLogger) Debugf(string, ...any) {}
func (nopLogger) Infof(string, ...any)  {}
func (nopLogger) Warnf(string, ...any)  {}
func (nopLogger) Errorf(string, ...any) {}

// loggerWriter 把 net/http 写给 http.Server.ErrorLog 的每一行转发到 Logger.Errorf。
// loggerWriter forwards each line net/http writes to http.Server.ErrorLog to Logger.Errorf.
type loggerWriter struct{ l Logger }

// Write 实现 io.Writer。
// Write implements io.Writer.
func (w loggerWriter) Write(p []byte) (int, error) {
	w.l.Errorf("%s", strings.TrimRight(string(p), "\r\n"))
	return len(p), nil
}

// errorLogFor 返回交给 http.Server 的 ErrorLog：优先使用 WithErrorLog，其次把 WithLogger 适配过去。
// errorLogFor returns the http.Server ErrorLog: WithErrorLog first, otherwise WithLogger adapted.
func errorLogFor(c serverConfig) *log.Logger {
	if c.errorLog != nil {
		return c.errorLog
	}
	if c.logger != nil {
		return log.New(loggerWriter{l: c.logger}, "", 0)
	}
	return nil
}
