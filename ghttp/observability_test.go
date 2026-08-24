package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func obsRawHandler(fn func(req *Request, resp *Response)) RawHandlerFunc {
	return func(_ context.Context, req *Request, resp *Response) error {
		fn(req, resp)
		if !resp.Written() {
			resp.WriteHeader(http.StatusOK)
		}
		return nil
	}
}

// TestMatchedRoute 覆盖各类路由命中后的模板还原。
func TestMatchedRoute(t *testing.T) {
	s := New()
	h := obsRawHandler(func(req *Request, resp *Response) {
		resp.Header().Set("X-Route", req.MatchedRoute())
	})
	s.RawHandle(http.MethodGet, "/static/path", h)
	s.RawHandle(http.MethodGet, "/users/{id}", h)
	s.RawHandle(http.MethodGet, "/users/{id}/posts/{pid}", h)
	s.RawHandle(http.MethodGet, "/files/{path...}", h)
	s.RawHandle(http.MethodGet, "/a/b", h)
	s.RawHandle(http.MethodGet, "/a/{x}", h)

	tests := []struct{ path, want string }{
		{"/static/path", "/static/path"},
		{"/users/42", "/users/:id"},
		{"/users/42/posts/7", "/users/:id/posts/:pid"},
		{"/files/a/b/c.txt", "/files/*path"},
		{"/a/b", "/a/b"},
		{"/a/zzz", "/a/:x"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if got := w.Header().Get("X-Route"); got != tt.want {
				t.Errorf("MatchedRoute for %s: got %q want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestMatchedRouteWithGlobalMiddleware 验证有全局中间件(chained 路径)时 MatchedRoute 也可见。
func TestMatchedRouteWithGlobalMiddleware(t *testing.T) {
	s := New()
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error { return next(ctx, req, resp) }
	})
	var seenInMW string
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			err := next(ctx, req, resp)
			seenInMW = req.MatchedRoute()
			return err
		}
	})
	s.RawHandle(http.MethodGet, "/users/{id}", obsRawHandler(func(req *Request, resp *Response) {}))
	r := httptest.NewRequest(http.MethodGet, "/users/9", nil)
	s.ServeHTTP(httptest.NewRecorder(), r)
	if seenInMW != "/users/:id" {
		t.Errorf("middleware saw route %q want /users/:id", seenInMW)
	}
}

// TestMatchedRouteMissEmpty 验证未命中时 MatchedRoute 为空。
func TestMatchedRouteMissEmpty(t *testing.T) {
	s := New()
	var seen = "unset"
	s.Use(func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			err := next(ctx, req, resp)
			seen = req.MatchedRoute()
			return err
		}
	})
	s.RawHandle(http.MethodGet, "/exists", obsRawHandler(func(req *Request, resp *Response) {}))
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))
	if seen != "" {
		t.Errorf("miss MatchedRoute got %q want empty", seen)
	}
}

// TestClientIP 覆盖可信代理模型的关键分支。
func TestClientIP(t *testing.T) {
	handler := func(s *Server) {
		s.RawHandle(http.MethodGet, "/ip", obsRawHandler(func(req *Request, resp *Response) {
			resp.Header().Set("X-ClientIP", req.ClientIP())
			resp.Header().Set("X-RemoteIP", req.RemoteIP())
		}))
	}
	call := func(s *Server, remote string, headers map[string]string) (clientIP, remoteIP string) {
		r := httptest.NewRequest(http.MethodGet, "/ip", nil)
		r.RemoteAddr = remote
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Header().Get("X-ClientIP"), w.Header().Get("X-RemoteIP")
	}

	t.Run("no trusted proxy ignores XFF", func(t *testing.T) {
		s := New()
		handler(s)
		ip, _ := call(s, "10.0.0.5:1234", map[string]string{"X-Forwarded-For": "1.2.3.4"})
		if ip != "10.0.0.5" {
			t.Errorf("got %q want 10.0.0.5", ip)
		}
	})
	t.Run("trusted peer walks XFF", func(t *testing.T) {
		s := New(WithTrustedProxies("10.0.0.0/8"))
		handler(s)
		ip, _ := call(s, "10.0.0.5:1234", map[string]string{"X-Forwarded-For": "203.0.113.7, 10.0.0.9"})
		if ip != "203.0.113.7" {
			t.Errorf("got %q want 203.0.113.7", ip)
		}
	})
	t.Run("untrusted peer ignores XFF", func(t *testing.T) {
		s := New(WithTrustedProxies("10.0.0.0/8"))
		handler(s)
		ip, _ := call(s, "8.8.8.8:1234", map[string]string{"X-Forwarded-For": "203.0.113.7"})
		if ip != "8.8.8.8" {
			t.Errorf("got %q want 8.8.8.8", ip)
		}
	})
	t.Run("custom forwarded header", func(t *testing.T) {
		s := New(WithTrustedProxies("10.0.0.0/8"), WithForwardedHeaders("X-Real-IP"))
		handler(s)
		ip, _ := call(s, "10.0.0.5:1234", map[string]string{"X-Real-IP": "203.0.113.99"})
		if ip != "203.0.113.99" {
			t.Errorf("got %q want 203.0.113.99", ip)
		}
	})
	t.Run("bare trusted ip and portless remote", func(t *testing.T) {
		s := New(WithTrustedProxies("192.168.1.1"))
		handler(s)
		ip, remoteIP := call(s, "192.168.1.1", map[string]string{"X-Forwarded-For": "203.0.113.1"})
		if ip != "203.0.113.1" {
			t.Errorf("clientIP got %q want 203.0.113.1", ip)
		}
		if remoteIP != "192.168.1.1" {
			t.Errorf("remoteIP got %q want 192.168.1.1", remoteIP)
		}
	})
}

// TestParseTrustedProxies 直接单测 CIDR/裸 IP 解析与错误处理。
func TestParseTrustedProxies(t *testing.T) {
	ok, err := parseTrustedProxies([]string{"10.0.0.0/8", "192.168.1.1", "  ", "::1"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(ok) != 3 {
		t.Errorf("got %d prefixes want 3", len(ok))
	}
	if _, err := parseTrustedProxies([]string{"not-an-ip"}); err == nil {
		t.Error("expected error for invalid entry")
	}
}

// TestWithTrustedProxiesPanicsOnInvalid 验证非法 CIDR 在注册期 panic。
func TestWithTrustedProxiesPanicsOnInvalid(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for invalid CIDR")
		}
	}()
	New(WithTrustedProxies("999.999.0.0/8"))
}

// TestAccessLogFields 验证结构化 AccessLog 的字段(Route/Status/BytesOut/ClientIP/Err)。
func TestAccessLogFields(t *testing.T) {
	var got AccessLog
	s := New(WithTrustedProxies("10.0.0.0/8"))
	s.Use(LoggerWith(func(a AccessLog) { got = a }))
	s.RawHandle(http.MethodGet, "/users/{id}", obsRawHandler(func(req *Request, resp *Response) {
		_, _ = resp.WriteString("hello-body")
	}))
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	r.RemoteAddr = "10.0.0.5:1111"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	s.ServeHTTP(httptest.NewRecorder(), r)

	if got.Method != http.MethodGet {
		t.Errorf("Method got %q", got.Method)
	}
	if got.Path != "/users/42" {
		t.Errorf("Path got %q want /users/42", got.Path)
	}
	if got.Route != "/users/:id" {
		t.Errorf("Route got %q want /users/:id", got.Route)
	}
	if got.Status != http.StatusOK {
		t.Errorf("Status got %d want 200", got.Status)
	}
	if got.BytesOut != len("hello-body") {
		t.Errorf("BytesOut got %d want %d", got.BytesOut, len("hello-body"))
	}
	if got.ClientIP != "203.0.113.7" {
		t.Errorf("ClientIP got %q want 203.0.113.7", got.ClientIP)
	}
	if got.Err != nil {
		t.Errorf("Err got %v want nil", got.Err)
	}
}

// TestResponseBytesOut 验证 BytesOut 累计多次写入。
func TestResponseBytesOut(t *testing.T) {
	s := New()
	s.RawHandle(http.MethodGet, "/multi", obsRawHandler(func(req *Request, resp *Response) {
		_, _ = resp.Write([]byte("abc"))
		_, _ = resp.WriteString("de")
	}))
	var logged AccessLog
	s2 := New()
	s2.Use(LoggerWith(func(a AccessLog) { logged = a }))
	s2.RawHandle(http.MethodGet, "/multi", obsRawHandler(func(req *Request, resp *Response) {
		_, _ = resp.Write([]byte("abc"))
		_, _ = resp.WriteString("de")
	}))
	s2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/multi", nil))
	if logged.BytesOut != 5 {
		t.Errorf("BytesOut got %d want 5", logged.BytesOut)
	}
}

// TestMatchedRouteAfterSplit 保护指针式 fullPath 在 radix 边分裂后仍指向正确模板。
func TestMatchedRouteAfterSplit(t *testing.T) {
	s := New()
	h := obsRawHandler(func(req *Request, resp *Response) {
		resp.Header().Set("X-Route", req.MatchedRoute())
	})
	// 故意制造节点分裂:共享前缀 /api/v1/user 与 /api/v1/users
	s.RawHandle(http.MethodGet, "/api/v1/users", h)
	s.RawHandle(http.MethodGet, "/api/v1/user", h)
	s.RawHandle(http.MethodGet, "/api/v2/{id}", h)
	s.RawHandle(http.MethodGet, "/api", h)

	tests := []struct{ path, want string }{
		{"/api/v1/users", "/api/v1/users"},
		{"/api/v1/user", "/api/v1/user"},
		{"/api/v2/42", "/api/v2/:id"},
		{"/api", "/api"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, tt.path, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if got := w.Header().Get("X-Route"); got != tt.want {
			t.Errorf("%s → %q want %q", tt.path, got, tt.want)
		}
	}
}
