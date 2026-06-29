package ghttp

import (
	"testing"
)

type testLogger struct {
	lastLevel string
	lastMsg   string
}

func (l *testLogger) Debugf(format string, args ...interface{}) {
	l.lastLevel = "debug"
	l.lastMsg = format
}

func (l *testLogger) Infof(format string, args ...interface{}) {
	l.lastLevel = "info"
	l.lastMsg = format
}

func (l *testLogger) Warnf(format string, args ...interface{}) {
	l.lastLevel = "warn"
	l.lastMsg = format
}

func (l *testLogger) Errorf(format string, args ...interface{}) {
	l.lastLevel = "error"
	l.lastMsg = format
}

func TestLoggerInterface(t *testing.T) {
	var l Logger = &testLogger{}
	if l == nil {
		t.Fatal("Logger interface should be assignable")
	}
}

func TestLoggerLevels(t *testing.T) {
	tl := &testLogger{}

	tl.Debugf("debug %d", 1)
	if tl.lastLevel != "debug" {
		t.Errorf("expected debug, got %s", tl.lastLevel)
	}

	tl.Infof("info %d", 2)
	if tl.lastLevel != "info" {
		t.Errorf("expected info, got %s", tl.lastLevel)
	}

	tl.Warnf("warn %d", 3)
	if tl.lastLevel != "warn" {
		t.Errorf("expected warn, got %s", tl.lastLevel)
	}

	tl.Errorf("error %d", 4)
	if tl.lastLevel != "error" {
		t.Errorf("expected error, got %s", tl.lastLevel)
	}
}
