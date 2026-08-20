package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStage0EndToEnd 验收阶段 0 骨架:一条硬编码路由能匹配并返回 200。
// TestStage0EndToEnd accepts the stage-0 skeleton: one hardcoded route matches
// and returns 200.
func TestStage0EndToEnd(t *testing.T) {
	m := New()
	if err := m.RawHandle(http.MethodGet, "/ping", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		_, err := io.WriteString(resp, "pong")
		return err
	}); err != nil {
		t.Fatalf("RawHandle: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "pong" {
		t.Fatalf("body = %q, want %q", got, "pong")
	}
}

// TestStage0NotFound 验收未命中返回 404。
// TestStage0NotFound accepts that a miss returns 404.
func TestStage0NotFound(t *testing.T) {
	m := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestStage0DuplicateRoute 验收重复注册报错。
// TestStage0DuplicateRoute accepts that duplicate registration errors.
func TestStage0DuplicateRoute(t *testing.T) {
	m := New()
	fn := func(_ context.Context, _ *Request, _ *Response) error { return nil }
	if err := m.RawHandle(http.MethodGet, "/x", fn); err != nil {
		t.Fatalf("first RawHandle: %v", err)
	}
	if err := m.RawHandle(http.MethodGet, "/x", fn); err != ErrDuplicateRoute {
		t.Fatalf("dup err = %v, want %v", err, ErrDuplicateRoute)
	}
}
