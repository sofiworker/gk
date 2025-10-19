package gserver

import (
	"fmt"
	"log"
	"os"
	"sync"
)

type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

type stdLogger struct {
	base *log.Logger
	once sync.Once
}

func newStdLogger() Logger {
	return &stdLogger{
		base: log.New(os.Stdout, "[gserver] ", log.LstdFlags|log.Lmicroseconds),
	}
}

func (l *stdLogger) logf(level string, format string, args ...interface{}) {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.base == nil {
			l.base = log.New(os.Stdout, "[gserver] ", log.LstdFlags|log.Lmicroseconds)
		}
	})
	if l.base == nil {
		return
	}
	if len(args) == 0 {
		l.base.Printf("[%s] %s", level, format)
		return
	}
	l.base.Printf("[%s] %s", level, fmt.Sprintf(format, args...))
}

func (l *stdLogger) Debugf(format string, args ...interface{}) {
	l.logf("DEBUG", format, args...)
}

func (l *stdLogger) Infof(format string, args ...interface{}) {
	l.logf("INFO", format, args...)
}

func (l *stdLogger) Warnf(format string, args ...interface{}) {
	l.logf("WARN", format, args...)
}

func (l *stdLogger) Errorf(format string, args ...interface{}) {
	l.logf("ERROR", format, args...)
}

type noopLogger struct{}

func (noopLogger) Debugf(string, ...interface{}) {}
func (noopLogger) Infof(string, ...interface{})  {}
func (noopLogger) Warnf(string, ...interface{})  {}
func (noopLogger) Errorf(string, ...interface{}) {}

func NewNoopLogger() Logger {
	return noopLogger{}
}
