package ghttp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestResp() *Response { return &Response{Writer: httptest.NewRecorder()} }

func newTestReq() *http.Request { return httptest.NewRequest(http.MethodGet, "/t", nil) }

func TestObserve(t *testing.T) {
	tests := []struct {
		name       string
		h          Handler
		wantStatus int
		wantBytes  int64
		wantErr    bool
	}{
		{"ok", func(_ context.Context, _ *Request, resp *Response) error {
			_, _ = resp.Write([]byte("hello"))
			return nil
		}, 200, 5, false},
		{"error unwritten", func(context.Context, *Request, *Response) error { return ErrForbidden }, 403, 0, true},
		{"error written", func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(201)
			return ErrForbidden
		}, 201, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info RequestInfo
			ridServe(t, Observe(func(i RequestInfo) { info = i }), tt.h, nil)
			if info.Status != tt.wantStatus || info.BytesWritten != tt.wantBytes || (info.Err != nil) != tt.wantErr {
				t.Fatalf("info: %+v", info)
			}
			if info.Method != "GET" || info.Path != "/t" || info.Route != "/t" {
				t.Fatalf("info: %+v", info)
			}
		})
	}
}

func TestObserveUnmatchedAndNil(t *testing.T) {
	s := NewServer()
	var info RequestInfo
	s.Use(Observe(func(i RequestInfo) { info = i }))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if info.Route != "" || info.Status != 404 {
		t.Fatalf("info: %+v", info)
	}
	called := false
	h := Observe(nil)(func(context.Context, *Request, *Response) error { called = true; return nil })
	_ = h(context.Background(), &Request{Raw: newTestReq()}, newTestResp())
	if !called {
		t.Fatal("nil observe must pass through")
	}
}

func TestLoggerStatusOnError(t *testing.T) {
	var buf bytes.Buffer
	w := ridServe(t, AccessLogWithWriter(&buf), func(context.Context, *Request, *Response) error { return ErrForbidden }, nil)
	if w.Code != 403 || !strings.Contains(buf.String(), "403") {
		t.Fatalf("code %d log %q", w.Code, buf.String())
	}
}
