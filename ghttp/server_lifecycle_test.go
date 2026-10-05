package ghttp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// lifecycleTimeout 是生命周期测试的统一等待上限。
// lifecycleTimeout is the common wait limit for lifecycle tests.
const lifecycleTimeout = 5 * time.Second

// newLifecycleServer 创建带 /ping 路由的服务器。
// ready 在首次请求到达处理器时关闭：处理器 goroutine 由 Serve 循环在写入
// httpServer 之后启动，因此等待 ready 可建立 happens-before，避免 -race 误报。
// newLifecycleServer creates a server with a /ping route. ready is closed when the
// first request reaches the handler: the handler goroutine is spawned by the serve
// loop after httpServer is assigned, so waiting on ready establishes happens-before
// and avoids -race reports.
func newLifecycleServer(t *testing.T, opts ...ServerOption) (*Server, <-chan struct{}) {
	t.Helper()
	s := NewServer(opts...)
	ready := make(chan struct{})
	var once sync.Once
	route := Raw(http.MethodGet, "/ping", func(ctx context.Context, req *Request, resp *Response) error {
		once.Do(func() { close(ready) })
		_, err := resp.Write([]byte("pong"))
		return err
	})
	if err := s.Register(route); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	return s, ready
}

// writeSelfSignedCert 生成自签名 ECDSA 证书并写入 dir。
// writeSelfSignedCert generates a self-signed ECDSA certificate into dir.
func writeSelfSignedCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

// lifecycleClient 返回跳过证书校验、禁用 keep-alive 的客户端。
// lifecycleClient returns a client that skips cert verification and disables keep-alive.
func lifecycleClient() *http.Client {
	return &http.Client{
		Timeout: lifecycleTimeout,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test only
			DisableKeepAlives: true,
		},
	}
}

// getBody 发送 GET 请求并返回响应体。
// getBody sends a GET request and returns the body.
func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := lifecycleClient().Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	return string(b)
}

// waitReady 等待 ready 关闭。
// waitReady waits for ready to be closed.
func waitReady(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(lifecycleTimeout):
		t.Fatal("timeout waiting for handler")
	}
}

// waitServeResult 等待服务 goroutine 返回。
// waitServeResult waits for the serving goroutine to return.
func waitServeResult(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(lifecycleTimeout):
		t.Fatal("timeout waiting for server to return")
		return nil
	}
}

// freeAddr 获取一个空闲的本地地址。
// freeAddr obtains a free local address.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// TestLifecycleShutdownCloseNotStarted 测试未启动时 Shutdown/Close 返回 nil。
// TestLifecycleShutdownCloseNotStarted tests Shutdown/Close return nil when not started.
func TestLifecycleShutdownCloseNotStarted(t *testing.T) {
	tests := []struct {
		name string
		fn   func(s *Server) error
	}{
		{"Shutdown", func(s *Server) error { return s.Shutdown(context.Background()) }},
		{"Close", func(s *Server) error { return s.Close() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(NewServer()); err != nil {
				t.Errorf("expected nil, got %v", err)
			}
		})
	}
}

// TestLifecycleServeShutdown 测试 Serve + Shutdown。
// TestLifecycleServeShutdown tests Serve + Shutdown.
func TestLifecycleServeShutdown(t *testing.T) {
	s, ready := newLifecycleServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(ln) }()

	if got := getBody(t, "http://"+ln.Addr().String()+"/ping"); got != "pong" {
		t.Errorf("expected pong, got %q", got)
	}
	waitReady(t, ready)

	ctx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
	if err := waitServeResult(t, errCh); !errors.Is(err, ErrServerClosed) {
		t.Errorf("expected ErrServerClosed, got %v", err)
	}
}

// TestLifecycleServeClose 测试 Serve + Close。
// TestLifecycleServeClose tests Serve + Close.
func TestLifecycleServeClose(t *testing.T) {
	s, ready := newLifecycleServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(ln) }()

	getBody(t, "http://"+ln.Addr().String()+"/ping")
	waitReady(t, ready)

	if err := s.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if err := waitServeResult(t, errCh); !errors.Is(err, ErrServerClosed) {
		t.Errorf("expected ErrServerClosed, got %v", err)
	}
}

// TestLifecycleServeUseIgnoredAfterStart 测试启动后 Use 被忽略。
// TestLifecycleServeUseIgnoredAfterStart tests Use is ignored after start.
func TestLifecycleServeUseIgnoredAfterStart(t *testing.T) {
	s, ready := newLifecycleServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(ln) }()

	getBody(t, "http://"+ln.Addr().String()+"/ping")
	waitReady(t, ready)

	if !s.started.Load() {
		t.Fatal("expected server to be started")
	}
	called := false
	mw := func(next Handler) Handler {
		called = true
		return next
	}
	if got := s.Use(mw); got != s {
		t.Error("Use should return the same server")
	}
	if len(s.middleware) != 0 {
		t.Errorf("expected middleware ignored after start, got %d", len(s.middleware))
	}
	getBody(t, "http://"+ln.Addr().String()+"/ping")
	if called {
		t.Error("middleware added after start must not be applied")
	}

	_ = s.Close()
	waitServeResult(t, errCh)
}

// TestLifecycleServeTLS 测试 ServeTLS（含 WithTLSConfig）。
// TestLifecycleServeTLS tests ServeTLS (with WithTLSConfig).
func TestLifecycleServeTLS(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t, t.TempDir())
	s, ready := newLifecycleServer(t, WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.ServeTLS(ln, certFile, keyFile) }()

	if got := getBody(t, "https://"+ln.Addr().String()+"/ping"); got != "pong" {
		t.Errorf("expected pong, got %q", got)
	}
	waitReady(t, ready)

	ctx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
	if err := waitServeResult(t, errCh); !errors.Is(err, ErrServerClosed) {
		t.Errorf("expected ErrServerClosed, got %v", err)
	}
}

// TestLifecycleServeTLSBadCert 测试证书文件不存在时 ServeTLS 返回错误。
// TestLifecycleServeTLSBadCert tests ServeTLS returns an error for missing cert files.
func TestLifecycleServeTLSBadCert(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	s := NewServer()
	missing := filepath.Join(t.TempDir(), "missing.pem")
	if err := s.ServeTLS(ln, missing, missing); err == nil || errors.Is(err, ErrServerClosed) {
		t.Errorf("expected cert load error, got %v", err)
	}
}

// TestLifecycleRun 测试 Run + Shutdown。
// TestLifecycleRun tests Run + Shutdown.
func TestLifecycleRun(t *testing.T) {
	s, ready := newLifecycleServer(t)
	addr := freeAddr(t)
	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(addr) }()

	var body string
	deadline := time.Now().Add(lifecycleTimeout)
	for {
		resp, err := lifecycleClient().Get("http://" + addr + "/ping")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(b)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not come up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if body != "pong" {
		t.Errorf("expected pong, got %q", body)
	}
	waitReady(t, ready)

	ctx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
	if err := waitServeResult(t, errCh); !errors.Is(err, ErrServerClosed) {
		t.Errorf("expected ErrServerClosed, got %v", err)
	}
}

// TestLifecycleRunTLS 测试 RunTLS + Shutdown。
// TestLifecycleRunTLS tests RunTLS + Shutdown.
func TestLifecycleRunTLS(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t, t.TempDir())
	s, ready := newLifecycleServer(t)
	addr := freeAddr(t)
	errCh := make(chan error, 1)
	go func() { errCh <- s.RunTLS(addr, certFile, keyFile) }()

	var body string
	deadline := time.Now().Add(lifecycleTimeout)
	for {
		resp, err := lifecycleClient().Get("https://" + addr + "/ping")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(b)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not come up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if body != "pong" {
		t.Errorf("expected pong, got %q", body)
	}
	waitReady(t, ready)

	ctx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
	if err := waitServeResult(t, errCh); !errors.Is(err, ErrServerClosed) {
		t.Errorf("expected ErrServerClosed, got %v", err)
	}
}

// TestLifecycleRunErrors 测试 Run/RunTLS 的错误路径（立即返回，无竞态）。
// TestLifecycleRunErrors tests Run/RunTLS error paths (return immediately, race-free).
func TestLifecycleRunErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pem")
	tests := []struct {
		name string
		fn   func(s *Server) error
	}{
		{"Run invalid addr", func(s *Server) error { return s.Run("127.0.0.1:-1") }},
		{"RunTLS missing cert", func(s *Server) error { return s.RunTLS("127.0.0.1:0", missing, missing) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer()
			err := tt.fn(s)
			if err == nil || errors.Is(err, ErrServerClosed) {
				t.Errorf("expected non-closed error, got %v", err)
			}
			if !s.started.Load() {
				t.Error("expected started flag set")
			}
		})
	}
}

// TestLifecycleCloseAfterRunError 测试启动失败后 Shutdown/Close 仍可安全调用。
// TestLifecycleCloseAfterRunError tests Shutdown/Close are safe after a failed start.
func TestLifecycleCloseAfterRunError(t *testing.T) {
	s := NewServer()
	if err := s.Run("127.0.0.1:-1"); err == nil {
		t.Fatal("expected error")
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestLifecycleErrorHandler 测试自定义/默认错误处理器分支。
// TestLifecycleErrorHandler tests custom/default error handler branches.
func TestLifecycleErrorHandler(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name     string
		opts     func(called *error) []ServerOption
		wantCode int
		wantBody string
		wantErr  bool
	}{
		{
			name: "custom handler",
			opts: func(called *error) []ServerOption {
				return []ServerOption{WithErrorHandler(func(_ context.Context, _ *Request, resp *Response, err error) {
					*called = err
					resp.WriteHeader(http.StatusTeapot)
					resp.Write([]byte("custom"))
				})}
			},
			wantCode: http.StatusTeapot,
			wantBody: "custom",
			wantErr:  true,
		},
		{
			name:     "nil handler falls back to default",
			opts:     func(*error) []ServerOption { return []ServerOption{WithErrorHandler(nil)} },
			wantCode: http.StatusInternalServerError,
			wantBody: `{"error":"Internal Server Error","status":500}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called error
			s := NewServer(tt.opts(&called)...)
			route := Raw(http.MethodGet, "/err", func(context.Context, *Request, *Response) error { return boom })
			if err := s.Register(route); err != nil {
				t.Fatalf("Register failed: %v", err)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/err", nil))
			if w.Code != tt.wantCode {
				t.Errorf("expected %d, got %d", tt.wantCode, w.Code)
			}
			if w.Body.String() != tt.wantBody {
				t.Errorf("expected body %q, got %q", tt.wantBody, w.Body.String())
			}
			if tt.wantErr && !errors.Is(called, boom) {
				t.Errorf("custom handler got err %v", called)
			}
		})
	}
}

// TestLifecycleStartAfterShutdown 测试 Shutdown/Close 之后再启动会返回 ErrServerClosed，避免"关不掉"的竞态窗口。
// TestLifecycleStartAfterShutdown tests that starting after Shutdown/Close returns ErrServerClosed,
// closing the window where a server could start after being shut down.
func TestLifecycleStartAfterShutdown(t *testing.T) {
	tests := []struct {
		name  string
		close func(*Server) error
	}{
		{"shutdown", func(s *Server) error { return s.Shutdown(context.Background()) }},
		{"close", func(s *Server) error { return s.Close() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer()
			if err := tt.close(s); err != nil {
				t.Fatalf("close before start: %v", err)
			}
			if err := s.Run("127.0.0.1:0"); !errors.Is(err, ErrServerClosed) {
				t.Errorf("Run after close = %v, want ErrServerClosed", err)
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer ln.Close()
			if err := s.Serve(ln); !errors.Is(err, ErrServerClosed) {
				t.Errorf("Serve after close = %v, want ErrServerClosed", err)
			}
		})
	}
}
