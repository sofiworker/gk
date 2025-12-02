package gserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubLogger struct {
	infos  []string
	errors []string
}

func (s *stubLogger) Debugf(format string, args ...interface{}) {}
func (s *stubLogger) Infof(format string, args ...interface{}) {
	s.infos = append(s.infos, fmt.Sprintf(format, args...))
}
func (s *stubLogger) Warnf(format string, args ...interface{}) {}
func (s *stubLogger) Errorf(format string, args ...interface{}) {
	s.errors = append(s.errors, fmt.Sprintf(format, args...))
}

func TestRequestLogger(t *testing.T) {
	logger := &stubLogger{}
	server := NewServer(WithLogger(logger))
	server.Use(RequestLogger())
	server.GET("/ping", func(c *Context) {
		c.String(http.StatusOK, "pong")
	})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(logger.infos) == 0 {
		t.Fatalf("expected info logs to be written")
	}
	if !strings.Contains(logger.infos[0], "GET /ping") {
		t.Fatalf("unexpected log content %q", logger.infos[0])
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	logger := &stubLogger{}
	server := NewServer(WithLogger(logger))
	server.Use(Recovery())
	server.GET("/panic", func(c *Context) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if len(logger.errors) == 0 {
		t.Fatalf("expected error log for panic")
	}
}
