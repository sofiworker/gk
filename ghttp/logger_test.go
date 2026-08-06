package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testLogger struct {
	lastLevel string
	lastMsg   string
	lastCtx   context.Context
	lastArgs  []interface{}
}

func (l *testLogger) DebugContext(ctx context.Context, msg string, args ...interface{}) {
	l.lastLevel = "debug"
	l.lastCtx = ctx
	l.lastMsg = msg
	l.lastArgs = args
}

func (l *testLogger) InfoContext(ctx context.Context, msg string, args ...interface{}) {
	l.lastLevel = "info"
	l.lastCtx = ctx
	l.lastMsg = msg
	l.lastArgs = args
}

func (l *testLogger) WarnContext(ctx context.Context, msg string, args ...interface{}) {
	l.lastLevel = "warn"
	l.lastCtx = ctx
	l.lastMsg = msg
	l.lastArgs = args
}

func (l *testLogger) ErrorContext(ctx context.Context, msg string, args ...interface{}) {
	l.lastLevel = "error"
	l.lastCtx = ctx
	l.lastMsg = msg
	l.lastArgs = args
}

func TestLoggerInterface(t *testing.T) {
	var _ Logger = &testLogger{}
}

func TestLoggerLevels(t *testing.T) {
	tl := &testLogger{}
	ctx := context.Background()

	tl.DebugContext(ctx, "debug", "n", 1)
	if tl.lastLevel != "debug" {
		t.Errorf("expected debug, got %s", tl.lastLevel)
	}

	tl.InfoContext(ctx, "info", "n", 2)
	if tl.lastLevel != "info" {
		t.Errorf("expected info, got %s", tl.lastLevel)
	}

	tl.WarnContext(ctx, "warn", "n", 3)
	if tl.lastLevel != "warn" {
		t.Errorf("expected warn, got %s", tl.lastLevel)
	}

	tl.ErrorContext(ctx, "error", "n", 4)
	if tl.lastLevel != "error" {
		t.Errorf("expected error, got %s", tl.lastLevel)
	}
}

func TestRequestLoggerUsesInjectedLogger(t *testing.T) {
	tl := &testLogger{}
	app := New(WithLogger(tl), WithProduces(MIMEJSON))
	app.Use(RequestLogger())

	Route[struct{}, struct{}](app).GET("/log").To(func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, nil
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/log", nil))

	if tl.lastLevel != "info" {
		t.Fatalf("lastLevel = %q, want info", tl.lastLevel)
	}
	if tl.lastCtx == nil {
		t.Fatal("expected request context")
	}
	if tl.lastMsg != "http request" {
		t.Fatalf("lastMsg = %q, want http request", tl.lastMsg)
	}
	if !hasLogArg(tl.lastArgs, "method", http.MethodGet) {
		t.Fatalf("lastArgs = %#v, want method", tl.lastArgs)
	}
	if !hasLogArg(tl.lastArgs, "path", "/log") {
		t.Fatalf("lastArgs = %#v, want path", tl.lastArgs)
	}
	if !hasLogArg(tl.lastArgs, "status", http.StatusOK) {
		t.Fatalf("lastArgs = %#v, want status", tl.lastArgs)
	}
	if !hasLogKey(tl.lastArgs, "size") {
		t.Fatalf("lastArgs = %#v, want size", tl.lastArgs)
	}
	if !hasLogKey(tl.lastArgs, "duration") {
		t.Fatalf("lastArgs = %#v, want duration", tl.lastArgs)
	}
}

func TestRequestLoggerWithoutInjectedLoggerIsNoop(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(RequestLogger())

	Route[struct{}, struct{}](app).GET("/log").To(func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, nil
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/log", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRecovererUsesInjectedLogger(t *testing.T) {
	tl := &testLogger{}
	app := New(WithLogger(tl), WithProduces(MIMEJSON))
	app.Use(Recoverer())

	Route[struct{}, struct{}](app).GET("/panic").To(func(context.Context, struct{}) (struct{}, error) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if tl.lastLevel != "error" {
		t.Fatalf("lastLevel = %q, want error", tl.lastLevel)
	}
	if tl.lastMsg != "panic recovered" {
		t.Fatalf("lastMsg = %q, want panic recovered", tl.lastMsg)
	}
	if !hasLogArg(tl.lastArgs, "panic", "boom") {
		t.Fatalf("lastArgs = %#v, want panic", tl.lastArgs)
	}
}

func hasLogArg(args []interface{}, key string, value interface{}) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func hasLogKey(args []interface{}, key string) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return true
		}
	}
	return false
}
