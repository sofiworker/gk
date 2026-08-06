package ghttp

import (
	"context"
	"log/slog"
)

// SlogLogger adapts a standard library *slog.Logger to the ghttp Logger
// interface.
type SlogLogger struct {
	logger *slog.Logger
}

// NewSlogLogger wraps a slog logger as a ghttp Logger. A nil logger returns nil.
func NewSlogLogger(logger *slog.Logger) Logger {
	if logger == nil {
		return nil
	}
	return &SlogLogger{logger: logger}
}

func (l *SlogLogger) DebugContext(ctx context.Context, msg string, args ...interface{}) {
	l.logger.DebugContext(ctx, msg, args...)
}

func (l *SlogLogger) InfoContext(ctx context.Context, msg string, args ...interface{}) {
	l.logger.InfoContext(ctx, msg, args...)
}

func (l *SlogLogger) WarnContext(ctx context.Context, msg string, args ...interface{}) {
	l.logger.WarnContext(ctx, msg, args...)
}

func (l *SlogLogger) ErrorContext(ctx context.Context, msg string, args ...interface{}) {
	l.logger.ErrorContext(ctx, msg, args...)
}
