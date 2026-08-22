package ghttp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestNewServerDefaults 验证 Server 构造的默认配置（IdleTimeout=60s）。
func TestNewServerDefaults(t *testing.T) {
	m := New()
	s := NewServer(m)
	if s.IsStarted() || s.IsClosed() {
		t.Fatal("new server should be neither started nor closed")
	}
	h := s.unwrap()
	if h.IdleTimeout != 60*time.Second {
		t.Errorf("default IdleTimeout=%v, want 60s", h.IdleTimeout)
	}
}

// TestNewServerOpts 验证 WithXxx 选项正确应用。
func TestNewServerOpts(t *testing.T) {
	m := New()
	dur := 5 * time.Second
	opts := []ServerOption{
		WithAddr(":1999"),
		WithReadTimeout(dur),
		WithWriteTimeout(dur),
		WithMaxHeaderBytes(8 << 10),
	}
	s := NewServer(m, opts...)
	h := s.unwrap()
	if h.Addr != ":1999" {
		t.Errorf("Addr=%q, want \":1999\"", h.Addr)
	}
	if h.ReadTimeout != dur {
		t.Errorf("ReadTimeout=%v, want %v", h.ReadTimeout, dur)
	}
	if h.WriteTimeout != dur {
		t.Errorf("WriteTimeout=%v, want %v", h.WriteTimeout, dur)
	}
	if h.MaxHeaderBytes != 8<<10 {
		t.Errorf("MaxHeaderBytes=%d, want %d", h.MaxHeaderBytes, 8<<10)
	}
}

// TestServerLifecycle 验证状态转换：started → shutdown 或 close。
func TestServerLifecycle(t *testing.T) {
	m := New()
	s := NewServer(m)

	// Double-start should error
	ctx := context.Background()
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.markStarted()
	}()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown failed: %v", err)
	}

	// After close, markStarted should reject
	if err := s.markStarted(); err == nil {
		t.Error("markStarted after close should fail")
	}
}

// TestIsStarted_IsClosed 报告方法正确性。
func TestIsStarted_IsClosed(t *testing.T) {
	m := New()
	s := NewServer(m)
	if s.IsStarted() {
		t.Error("fresh server IsStarted should be false")
	}
	if s.IsClosed() {
		t.Error("fresh server IsClosed should be false")
	}

	_ = s.markStarted()
	if !s.IsStarted() {
		t.Error("after start IsStarted should be true")
	}

	ctx := context.Background()
	_ = s.Shutdown(ctx)
	if !s.IsClosed() {
		t.Error("after shutdown IsClosed should be true")
	}
}

// TestShutdownIdempotent 验证 Shutdown 幂等性。
func TestShutdownIdempotent(t *testing.T) {
	m := New()
	s := NewServer(m)
	ctx := context.Background()

	_ = s.markStarted()
	_ = s.Shutdown(ctx) // first call
	_ = s.Shutdown(ctx) // second call should not panic or error
}

// TestSentinelErrors_TLS 验证缺证书 TLS 失败时,用户侧可用 errors.Is(ErrTLSConfig) 判定。
// TestSentinelErrors_TLS verifies that a TLS failure with no certs can be detected
// via errors.Is(ErrTLSConfig).
func TestSentinelErrors_TLS(t *testing.T) {
	m := New()
	srv := NewServer(m)
	if err := srv.ListenAndServeTLS("", ""); !errors.Is(err, ErrTLSConfig) {
		t.Errorf("ListenAndServeTLS err=%v, want errors.Is ErrTLSConfig", err)
	}
}

// TestSentinelErrors_DoubleStart 验证运行中重复启动可经 errors.Is(ErrServerStarted) 判定。
// 第一个 ListenAndServe 在后台阻塞,其间从主 goroutine 再次启动应被拒。
// TestSentinelErrors_DoubleStart verifies a double start while running is detectable
// via errors.Is(ErrServerStarted). The first ListenAndServe blocks in the
// background; a second start from the main goroutine must be rejected.
func TestSentinelErrors_DoubleStart(t *testing.T) {
	m := New()
	srv := NewServer(m, WithAddr("127.0.0.1:0"))
	go func() { _ = srv.ListenAndServe() }()
	time.Sleep(100 * time.Millisecond) // 等启动 / wait for start

	// 运行中再次启动,应被拒为 ErrServerStarted。
	// Starting while running must be rejected as ErrServerStarted.
	if err := srv.ListenAndServe(); !errors.Is(err, ErrServerStarted) {
		t.Errorf("double start err=%v, want errors.Is ErrServerStarted", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// TestSentinelErrors_RestartAfterClose 验证关闭后再启动返回 ErrServerNotStartable。
// TestSentinelErrors_RestartAfterClose verifies starting after close returns
// ErrServerNotStartable.
func TestSentinelErrors_RestartAfterClose(t *testing.T) {
	m := New()
	srv := NewServer(m, WithAddr("127.0.0.1:0"))
	go func() { _ = srv.ListenAndServe() }()
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if err := srv.ListenAndServe(); !errors.Is(err, ErrServerNotStartable) {
		t.Errorf("restart after close err=%v, want errors.Is ErrServerNotStartable", err)
	}
}

// TestSentinelErrors_EngineNotStarted 验证未经 Run 就 Shutdown 返回 ErrEngineNotStarted。
// TestSentinelErrors_EngineNotStarted verifies Shutdown before Run returns
// ErrEngineNotStarted.
func TestSentinelErrors_EngineNotStarted(t *testing.T) {
	m := New()
	if err := m.Shutdown(context.Background()); !errors.Is(err, ErrEngineNotStarted) {
		t.Errorf("Shutdown err=%v, want errors.Is ErrEngineNotStarted", err)
	}
}
