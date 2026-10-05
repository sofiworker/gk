package ghttp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// mockResponseWriter 是用于基准测试的轻量级 ResponseWriter。
// mockResponseWriter is a lightweight ResponseWriter for benchmarking.
type mockResponseWriter struct {
	header     http.Header
	statusCode int
	written    int
}

func (m *mockResponseWriter) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *mockResponseWriter) Write(b []byte) (int, error) {
	m.written += len(b)
	return len(b), nil
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}

func (m *mockResponseWriter) reset() {
	m.header = nil
	m.statusCode = 0
	m.written = 0
}

// TestRecovery 测试 Recovery 中间件。
// TestRecovery tests Recovery middleware.
func TestRecovery(t *testing.T) {
	s := NewServer()
	s.Use(Recovery())

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		panic("test panic")
	}

	err := s.Register(Route{
		Method:          http.MethodGet,
		Path:            "/panic",
		compiledHandler: handler,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/panic", nil)
	s.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	body := w.Body.String()
	if body != `{"error":"internal server error","status":500}` {
		t.Errorf("unexpected body '%s'", body)
	}
}

// TestRecoveryCustomHandler 测试自定义 Recovery 处理器。
// TestRecoveryCustomHandler tests custom Recovery handler.
func TestRecoveryCustomHandler(t *testing.T) {
	s := NewServer()

	customHandlerCalled := false
	customHandler := func(ctx context.Context, req *Request, resp *Response, rec any) {
		customHandlerCalled = true
		resp.WriteHeader(http.StatusServiceUnavailable)
		resp.Write([]byte("custom recovery"))
	}

	s.Use(RecoveryWithHandler(io.Discard, customHandler))

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		panic("test panic")
	}

	err := s.Register(Route{
		Method:          http.MethodGet,
		Path:            "/panic",
		compiledHandler: handler,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/panic", nil)
	s.ServeHTTP(w, r)

	if !customHandlerCalled {
		t.Error("custom handler was not called")
	}

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	body := w.Body.String()
	if body != "custom recovery" {
		t.Errorf("expected 'custom recovery', got '%s'", body)
	}
}

// TestLogger 测试 Logger 中间件。
// TestLogger tests Logger middleware.
func TestLogger(t *testing.T) {
	s := NewServer()

	var logBuf bytes.Buffer
	s.Use(AccessLogWithWriter(&logBuf))

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("OK"))
		return nil
	}

	err := s.Register(Route{
		Method:          http.MethodGet,
		Path:            "/test",
		compiledHandler: handler,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	s.ServeHTTP(w, r)

	logOutput := logBuf.String()
	if logOutput == "" {
		t.Error("logger did not write any output")
	}

	// 验证日志包含关键信息
	// Verify log contains key information
	if !strings.Contains(logOutput, "GET") {
		t.Error("log should contain HTTP method")
	}
	if !strings.Contains(logOutput, "/test") {
		t.Error("log should contain path")
	}
	if !strings.Contains(logOutput, "200") {
		t.Error("log should contain status code")
	}
}

// TestLoggerSkipPaths 测试 Logger 跳过指定路径。
// TestLoggerSkipPaths tests Logger skipping specified paths.
func TestLoggerSkipPaths(t *testing.T) {
	s := NewServer()

	var logBuf bytes.Buffer
	s.Use(AccessLogWithConfig(AccessLogConfig{
		Output:    &logBuf,
		SkipPaths: []string{"/health"},
	}))

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("OK"))
		return nil
	}

	s.Register(Route{Method: http.MethodGet, Path: "/health", compiledHandler: handler})
	s.Register(Route{Method: http.MethodGet, Path: "/test", compiledHandler: handler})

	// 请求 /health（应跳过日志）
	// Request /health (should skip logging)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	s.ServeHTTP(w, r)

	if logBuf.Len() > 0 {
		t.Error("logger should skip /health path")
	}

	// 请求 /test（应记录日志）
	// Request /test (should log)
	logBuf.Reset()
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/test", nil)
	s.ServeHTTP(w, r)

	if logBuf.Len() == 0 {
		t.Error("logger should log /test path")
	}
}

// TestLoggerWithError 测试 Logger 记录错误。
// TestLoggerWithError tests Logger logging errors.
func TestLoggerWithError(t *testing.T) {
	s := NewServer()

	var logBuf bytes.Buffer
	s.Use(AccessLogWithWriter(&logBuf))

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return BadRequest("test error")
	}

	err := s.Register(Route{
		Method:          http.MethodGet,
		Path:            "/error",
		compiledHandler: handler,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/error", nil)
	s.ServeHTTP(w, r)

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "error:") {
		t.Error("log should contain error message")
	}
}

// TestLoggerLineInjection 测试错误文本与 panic 值中的换行被转义，无法伪造额外日志行。
// TestLoggerLineInjection tests that newlines in error text and panic values are escaped
// so they cannot forge extra log lines.
func TestLoggerLineInjection(t *testing.T) {
	const forged = "x\n2026/01/01 - 00:00:00 | 200 | admin login ok"

	tests := []struct {
		name    string
		handler RawHandlerFunc
		wrap    func(io.Writer) Middleware
	}{
		{
			name: "logger error text",
			handler: func(ctx context.Context, req *Request, resp *Response) error {
				return BadRequest(req.Raw.URL.Query().Get("q"))
			},
			wrap: AccessLogWithWriter,
		},
		{
			name: "recovery panic value",
			handler: func(ctx context.Context, req *Request, resp *Response) error {
				panic(req.Raw.URL.Query().Get("q"))
			},
			wrap: RecoveryWithWriter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			s := NewServer()
			s.Use(tt.wrap(&logBuf))
			if err := s.Register(Raw(http.MethodGet, "/e", tt.handler)); err != nil {
				t.Fatalf("Register failed: %v", err)
			}

			r := httptest.NewRequest(http.MethodGet, "/e", nil)
			r.URL.RawQuery = "q=" + url.QueryEscape(forged)
			s.ServeHTTP(httptest.NewRecorder(), r)

			if strings.Contains(logBuf.String(), "\n2026/01/01") {
				t.Fatalf("forged log line written:\n%s", logBuf.String())
			}
			if !strings.Contains(logBuf.String(), `x\n2026/01/01`) {
				t.Errorf("escaped value missing from log:\n%s", logBuf.String())
			}
		})
	}
}

// TestMultipleMiddleware 测试多个中间件组合。
// TestMultipleMiddleware tests multiple middleware combination.
func TestMultipleMiddleware(t *testing.T) {
	s := NewServer()

	var logBuf bytes.Buffer
	s.Use(Recovery(), AccessLogWithWriter(&logBuf))

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("OK"))
		return nil
	}

	err := s.Register(Route{
		Method:          http.MethodGet,
		Path:            "/test",
		compiledHandler: handler,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	s.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if logBuf.Len() == 0 {
		t.Error("logger should have logged the request")
	}
}

// BenchmarkRecovery 测试 Recovery 中间件性能。
// BenchmarkRecovery benchmarks Recovery middleware performance.
func BenchmarkRecovery(b *testing.B) {
	s := NewServer()
	s.Use(Recovery())

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("OK"))
		return nil
	}

	s.Register(Route{
		Method:          http.MethodGet,
		Path:            "/test",
		compiledHandler: handler,
	})

	w := &mockResponseWriter{}
	r, _ := http.NewRequest(http.MethodGet, "/test", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w.reset()
		s.ServeHTTP(w, r)
	}
}

// BenchmarkLogger 测试 Logger 中间件性能。
// BenchmarkLogger benchmarks Logger middleware performance.
func BenchmarkLogger(b *testing.B) {
	s := NewServer()
	s.Use(AccessLogWithWriter(io.Discard))

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		resp.Write([]byte("OK"))
		return nil
	}

	s.Register(Route{
		Method:          http.MethodGet,
		Path:            "/test",
		compiledHandler: handler,
	})

	w := &mockResponseWriter{}
	r, _ := http.NewRequest(http.MethodGet, "/test", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w.reset()
		s.ServeHTTP(w, r)
	}
}
