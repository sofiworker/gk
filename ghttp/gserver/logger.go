package gserver

import (
	"github.com/sofiworker/gk/glog"
)

type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

type logger struct {
	base glog.Logger
}

func newLogger() Logger {
	return &logger{
		base: glog.Default(),
	}
}

func (l *logger) Debugf(format string, args ...interface{}) {
	l.base.Debugf(format, args...)
}

func (l *logger) Infof(format string, args ...interface{}) {
	l.base.Infof(format, args...)
}

func (l *logger) Warnf(format string, args ...interface{}) {
	l.base.Warnf(format, args...)
}

func (l *logger) Errorf(format string, args ...interface{}) {
	l.base.Errorf(format, args...)
}
