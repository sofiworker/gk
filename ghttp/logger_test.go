package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRequestLoggerUsesInjectedLogger(t *testing.T) {
	tl := &testLogger{}
	app := New(WithLogger(tl))
	app.Use(RequestLogger())

	if err := Route[struct{}, struct{}](app).GET("/log").To(func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/log", nil))

	if tl.lastLevel != "info" {
		t.Fatalf("lastLevel = %q, want info", tl.lastLevel)
	}
	if !strings.Contains(tl.lastMsg, "%s %s %s") {
		t.Fatalf("lastMsg = %q, want request log format", tl.lastMsg)
	}
}

func TestRequestLoggerWithoutInjectedLoggerIsNoop(t *testing.T) {
	app := New()
	app.Use(RequestLogger())

	if err := Route[struct{}, struct{}](app).GET("/log").To(func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/log", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRecovererUsesInjectedLogger(t *testing.T) {
	tl := &testLogger{}
	app := New(WithLogger(tl))
	app.Use(Recoverer())

	if err := Route[struct{}, struct{}](app).GET("/panic").To(func(context.Context, *struct{}) (*struct{}, error) {
		panic("boom")
	}); err != nil {
		t.Fatalf("To failed: %v", err)
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if tl.lastLevel != "error" {
		t.Fatalf("lastLevel = %q, want error", tl.lastLevel)
	}
}
