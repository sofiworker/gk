package ghttp

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"
)

// ——— BasicAuth ———

func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func TestBasicAuth(t *testing.T) {
	s := New()
	s.Use(BasicAuth("My Realm", map[string]string{"alice": "secret", "bob": "pw"}))
	s.RawHandle(http.MethodGet, "/private", func(ctx context.Context, req *Request, resp *Response) error {
		resp.Header().Set("X-User", BasicAuthUser(ctx))
		resp.WriteHeader(http.StatusOK)
		return nil
	})

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantUser   string
	}{
		{"valid alice", basicAuthHeader("alice", "secret"), 200, "alice"},
		{"valid bob", basicAuthHeader("bob", "pw"), 200, "bob"},
		{"wrong password", basicAuthHeader("alice", "wrong"), 401, ""},
		{"unknown user", basicAuthHeader("carol", "x"), 401, ""},
		{"no header", "", 401, ""},
		{"malformed base64", "Basic !!!notbase64", 401, ""},
		{"no colon", "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")), 401, ""},
		{"wrong scheme", "Bearer token", 401, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/private", nil)
			if tt.authHeader != "" {
				r.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tt.wantStatus {
				t.Errorf("status %d want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == 401 {
				if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="My Realm"` {
					t.Errorf("WWW-Authenticate %q", got)
				}
			}
			if tt.wantUser != "" && w.Header().Get("X-User") != tt.wantUser {
				t.Errorf("X-User %q want %q", w.Header().Get("X-User"), tt.wantUser)
			}
		})
	}
}

func TestBasicAuthSchemeCaseInsensitive(t *testing.T) {
	s := New()
	s.Use(BasicAuth("R", map[string]string{"u": "p"}))
	s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(200)
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	// scheme 名大小写不敏感（RFC 7617）
	r.Header.Set("Authorization", "basic "+base64.StdEncoding.EncodeToString([]byte("u:p")))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("lowercase scheme: status %d want 200", w.Code)
	}
}

func TestBasicAuthEmptyRealmDefault(t *testing.T) {
	s := New()
	s.Use(BasicAuth("", map[string]string{"u": "p"}))
	s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(200)
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="Restricted"` {
		t.Errorf("default realm challenge %q", got)
	}
}

func TestParseBasicAuthUnit(t *testing.T) {
	u, p, ok := parseBasicAuth(basicAuthHeader("user", "pa:ss"))
	if !ok || u != "user" || p != "pa:ss" {
		t.Errorf("parse got (%q,%q,%v); password with colon must survive", u, p, ok)
	}
	if _, _, ok := parseBasicAuth("Basic"); ok {
		t.Error("too-short header should fail")
	}
}

// ——— RunGraceful ———

// TestRunGracefulListenFailure 监听失败(非法地址)应在启动阶段即返回错误,不进入信号等待。
func TestRunGracefulListenFailure(t *testing.T) {
	s := New()
	done := make(chan error, 1)
	go func() { done <- s.RunGraceful("256.256.256.256:99999") }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected listen error, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Error("RunGraceful did not return on listen failure")
	}
}

// TestRunGracefulSignalDrains 用真实监听 + 自发信号验证完整编排:摘流→排水→返回 nil。
func TestRunGracefulSignalDrains(t *testing.T) {
	// 先占一个真实端口拿到地址,再关掉让 RunGraceful 用（缩小竞态窗口）。
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	gate, checker := NewReadinessGate("test")
	gate.Set(true, nil)
	s := New()
	Ready(s, "/readyz", time.Second, checker) // 挂就绪探针，便于验证摘流
	s.RawHandle(http.MethodGet, "/ping", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(200)
		return nil
	})

	done := make(chan error, 1)
	go func() {
		done <- s.RunGraceful(addr, WithReadinessGate(gate), WithShutdownTimeout(2*time.Second))
	}()

	// 等服务起来
	waitServing(t, addr, 2*time.Second)

	// 自发 SIGTERM 触发优雅退出
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find self process: %v", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send signal: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("RunGraceful returned %v want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunGraceful did not shut down after SIGTERM")
	}
	// 摘流后门闸应为未就绪
	if err := checker.Check(context.Background()); err == nil {
		t.Error("readiness gate should be not-ready after graceful stop")
	}
}

func waitServing(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not start within %s", addr, timeout)
}

// TestWithShutdownTimeoutDefaults 验证选项默认与非正值回退。
func TestGracefulOptionDefaults(t *testing.T) {
	var cfg gracefulConfig
	cfg.shutdownTimeout = DefaultShutdownTimeout
	WithShutdownTimeout(-1)(&cfg)
	// -1 经 RunGraceful 内的回退逻辑应变默认；此处仅验证 setter 本身写入了 -1
	if cfg.shutdownTimeout != -1 {
		t.Errorf("setter wrote %v", cfg.shutdownTimeout)
	}
	WithDrainDelay(3 * time.Second)(&cfg)
	if cfg.drainDelay != 3*time.Second {
		t.Errorf("drainDelay %v", cfg.drainDelay)
	}
	WithSignals(syscall.SIGUSR1)(&cfg)
	if len(cfg.signals) != 1 || cfg.signals[0] != syscall.SIGUSR1 {
		t.Errorf("signals %v", cfg.signals)
	}
	g, _ := NewReadinessGate("x")
	WithReadinessGate(g)(&cfg)
	if cfg.gate != g {
		t.Error("gate not set")
	}
}
