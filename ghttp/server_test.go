package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewServer 测试创建服务器。
// TestNewServer tests creating a server.
func TestNewServer(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	if s.router == nil {
		t.Error("router is nil")
	}
	if len(s.middleware) != 0 {
		t.Errorf("expected no global middleware, got %d", len(s.middleware))
	}
	if s.started.Load() {
		t.Error("server should not be started")
	}
}

// TestServerWithOptions 测试服务器选项。
// TestServerWithOptions tests server options.
func TestServerWithOptions(t *testing.T) {
	s := NewServer(
		WithAddr(":9090"),
		WithReadTimeout(30),
		WithWriteTimeout(30),
	)
	if s.config.addr != ":9090" {
		t.Errorf("expected addr :9090, got %s", s.config.addr)
	}
}

// TestServerUse 测试全局中间件。
// TestServerUse tests global middleware.
func TestServerUse(t *testing.T) {
	s := NewServer()
	middleware := func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			return next(ctx, req, resp)
		}
	}

	s.Use(middleware)
	if len(s.middleware) != 1 {
		t.Errorf("expected 1 middleware, got %d", len(s.middleware))
	}
}

// TestServerRegister 测试路由注册。
// TestServerRegister tests route registration.
func TestServerRegister(t *testing.T) {
	s := NewServer()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("OK"))
		return nil
	}

	route := Route{
		Method:          http.MethodGet,
		Path:            "/test",
		compiledHandler: handler,
	}

	err := s.Register(route)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// 验证路由已注册
	// Verify route is registered
	h, _, _ := s.router.match(http.MethodGet, "/test")
	if h == nil {
		t.Error("route not registered")
	}
}

// TestServerRegisterDuplicate 测试重复路由注册。
// TestServerRegisterDuplicate tests duplicate route registration.
func TestServerRegisterDuplicate(t *testing.T) {
	s := NewServer()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	}

	route := Route{
		Method:          http.MethodGet,
		Path:            "/test",
		compiledHandler: handler,
	}

	// 第一次注册成功
	// First registration succeeds
	err := s.Register(route)
	if err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	// 第二次注册应失败
	// Second registration should fail
	err = s.Register(route)
	if err == nil {
		t.Error("expected error for duplicate route, got nil")
	}
}

// TestServerRegisterAfterStart 测试启动后禁止注册。
// TestServerRegisterAfterStart tests registration is disallowed after start.
func TestServerRegisterAfterStart(t *testing.T) {
	s := NewServer()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return nil
	}

	route := Route{
		Method:          http.MethodGet,
		Path:            "/test",
		compiledHandler: handler,
	}

	// 启动服务器（通过 ServeHTTP）
	// Start server (via ServeHTTP)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	s.ServeHTTP(w, r)

	// 启动后注册应失败
	// Registration after start should fail
	err := s.Register(route)
	if !errors.Is(err, ErrRegistrationAfterStart) {
		t.Errorf("expected ErrRegistrationAfterStart, got %v", err)
	}
}

// TestServerGroup 测试路由分组。
// TestServerGroup tests route groups.
func TestServerGroup(t *testing.T) {
	s := NewServer()

	g := s.Group("/api")
	if g == nil {
		t.Fatal("Group returned nil")
	}
	if g.prefix != "/api" {
		t.Errorf("expected prefix /api, got %s", g.prefix)
	}
	if g.server != s {
		t.Error("group server mismatch")
	}
}

// TestServerServeHTTP 测试 HTTP 请求处理。
// TestServerServeHTTP tests HTTP request handling.
func TestServerServeHTTP(t *testing.T) {
	s := NewServer()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("Hello"))
		return nil
	}

	route := Route{
		Method:          http.MethodGet,
		Path:            "/hello",
		compiledHandler: handler,
	}

	err := s.Register(route)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hello", nil)
	s.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "Hello" {
		t.Errorf("expected body 'Hello', got '%s'", w.Body.String())
	}
}

// TestServerServeHTTP404 测试 404 响应。
// TestServerServeHTTP404 tests 404 response.
func TestServerServeHTTP404(t *testing.T) {
	s := NewServer()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	s.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

// TestMiddlewareExecution 测试中间件执行顺序。
// TestMiddlewareExecution tests middleware execution order.
func TestMiddlewareExecution(t *testing.T) {
	s := NewServer()

	var order []int

	middleware1 := func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			order = append(order, 1)
			err := next(ctx, req, resp)
			order = append(order, 4)
			return err
		}
	}

	middleware2 := func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			order = append(order, 2)
			err := next(ctx, req, resp)
			order = append(order, 3)
			return err
		}
	}

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		order = append(order, 99)
		resp.Write([]byte("OK"))
		return nil
	}

	s.Use(middleware1, middleware2)

	route := Route{
		Method:          http.MethodGet,
		Path:            "/test",
		compiledHandler: handler,
	}

	err := s.Register(route)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	s.ServeHTTP(w, r)

	// 期望顺序：1 -> 2 -> 99 -> 3 -> 4
	// Expected order: 1 -> 2 -> 99 -> 3 -> 4
	expected := []int{1, 2, 99, 3, 4}
	if len(order) != len(expected) {
		t.Fatalf("expected order length %d, got %d", len(expected), len(order))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d]: expected %d, got %d", i, v, order[i])
		}
	}
}

// TestErrorHandling 测试错误处理。
// TestErrorHandling tests error handling.
func TestErrorHandling(t *testing.T) {
	s := NewServer()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return errors.New("test error")
	}

	route := Route{
		Method:          http.MethodGet,
		Path:            "/error",
		compiledHandler: handler,
	}

	err := s.Register(route)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/error", nil)
	s.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

// TestHTTPError 测试 HTTPError 处理。
// TestHTTPError tests HTTPError handling.
func TestHTTPError(t *testing.T) {
	s := NewServer()

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return BadRequest("invalid input")
	}

	route := Route{
		Method:          http.MethodGet,
		Path:            "/bad",
		compiledHandler: handler,
	}

	err := s.Register(route)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/bad", nil)
	s.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
	if w.Body.String() != `{"error":"invalid input","status":400}` {
		t.Errorf("unexpected body '%s'", w.Body.String())
	}
}
