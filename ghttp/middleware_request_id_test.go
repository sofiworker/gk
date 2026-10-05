package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ridServe 用给定中间件注册 GET /t 并发出请求。
// ridServe registers GET /t with the given middleware and performs a request.
func ridServe(t *testing.T, mw Middleware, h Handler, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	s := NewServer()
	s.Use(mw)
	if err := s.Register(Route{Method: http.MethodGet, Path: "/t", compiledHandler: h}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/t", nil)
	if mutate != nil {
		mutate(r)
	}
	s.ServeHTTP(w, r)
	return w
}

func TestRequestID(t *testing.T) {
	var seen, seenRaw string
	h := func(ctx context.Context, req *Request, resp *Response) error {
		seen = RequestIDFrom(ctx)
		seenRaw = RequestIDFrom(req.Context())
		return nil
	}
	tests := []struct {
		name  string
		in    string
		reuse bool
	}{
		{"reuse valid", "abc-123_X.y", true},
		{"too long", strings.Repeat("a", 129), false},
		{"bad chars", "a b", false},
		{"semicolon", "a;b", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := ridServe(t, RequestID(), h, func(r *http.Request) {
				if tt.in != "" {
					r.Header["X-Request-Id"] = []string{tt.in}
				}
			})
			got := w.Header().Get(HeaderRequestID)
			if tt.reuse && got != tt.in {
				t.Fatalf("got %q want reuse %q", got, tt.in)
			}
			if !tt.reuse && (got == tt.in || len(got) != 32) {
				t.Fatalf("expected generated 32 hex, got %q", got)
			}
			if seen != got || seenRaw != got {
				t.Fatalf("ctx id %q / raw %q != header %q", seen, seenRaw, got)
			}
		})
	}
}

func TestRequestIDOptions(t *testing.T) {
	w := ridServe(t, RequestID(WithRequestIDHeader("X-Trace"), WithRequestIDGenerator(func() string { return "fixed" }), WithRequestIDHeader(""), WithRequestIDGenerator(nil)),
		func(context.Context, *Request, *Response) error { return nil }, nil)
	if w.Header().Get("X-Trace") != "fixed" {
		t.Fatalf("headers: %v", w.Header())
	}
	if RequestIDFrom(context.Background()) != "" {
		t.Fatal("expected empty")
	}
}
