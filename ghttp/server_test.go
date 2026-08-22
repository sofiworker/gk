package ghttp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestNewDefaults 验证 New 的默认监听配置（IdleTimeout=60s）与初始生命周期状态。
func TestNewDefaults(t *testing.T) {
	s := New()
	if s.IsStarted() || s.IsClosed() {
		t.Fatal("new server should be neither started nor closed")
	}
	h := s.unwrap()
	if h.IdleTimeout != 60*time.Second {
		t.Errorf("default IdleTimeout=%v, want 60s", h.IdleTimeout)
	}
	// Handler 必须直指内部 mux（零偏移嵌入），保证请求路径无提升包装。
	if h.Handler != &s.mux {
		t.Error("http.Server.Handler should point at the embedded mux")
	}
}

// TestNewOptions 验证 WithXxx 选项正确应用。
func TestNewOptions(t *testing.T) {
	dur := 5 * time.Second
	s := New(
		WithAddr(":1999"),
		WithReadTimeout(dur),
		WithWriteTimeout(dur),
		WithMaxHeaderBytes(8<<10),
		WithStrictPath(true),
	)
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
	if !s.strictPath {
		t.Error("WithStrictPath(true) should set strictPath")
	}
}

// TestServerLifecycle 验证状态转换：started → shutdown 后不可再启动。
func TestServerLifecycle(t *testing.T) {
	s := New()
	if err := s.markStarted(); err != nil {
		t.Fatalf("first markStarted: %v", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown failed: %v", err)
	}
	if err := s.markStarted(); err == nil {
		t.Error("markStarted after close should fail")
	}
}

// TestIsStarted_IsClosed 验证三态生命周期的报告方法:idle → running → closed 逐态互斥,
// 不存在"既在运行又已关闭"的矛盾组合。
// TestIsStarted_IsClosed verifies the three-state lifecycle reporters: idle →
// running → closed are mutually exclusive, with no contradictory "running and
// closed" combination.
func TestIsStarted_IsClosed(t *testing.T) {
	s := New()
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
	if s.IsClosed() {
		t.Error("while running IsClosed should be false")
	}

	_ = s.Shutdown(context.Background())
	if !s.IsClosed() {
		t.Error("after shutdown IsClosed should be true")
	}
	// 三态语义:关闭后不再"正在运行"。
	// Three-state semantics: no longer "running" once closed.
	if s.IsStarted() {
		t.Error("after shutdown IsStarted should be false (running and closed are exclusive)")
	}
}

// TestServerState_TransitionsAreTerminal 验证 closed 是终态:关闭后既不能启动,
// 也不会被后续 Shutdown/Close 退回其他状态。
// TestServerState_TransitionsAreTerminal verifies closed is terminal: once closed the
// server cannot start, and later Shutdown/Close calls never move it back.
func TestServerState_TransitionsAreTerminal(t *testing.T) {
	s := New()
	_ = s.markStarted()
	_ = s.Shutdown(context.Background())

	if err := s.markStarted(); !errors.Is(err, ErrServerNotStartable) {
		t.Errorf("markStarted after close = %v, want ErrServerNotStartable", err)
	}
	_ = s.Close()
	if !s.IsClosed() || s.IsStarted() {
		t.Error("Close after Shutdown should keep the server closed")
	}
	if err := s.markStarted(); !errors.Is(err, ErrServerNotStartable) {
		t.Errorf("markStarted after Close = %v, want ErrServerNotStartable", err)
	}
}

// TestShutdownIdempotent 验证 Shutdown 幂等性，且未启动时调用也安全。
func TestShutdownIdempotent(t *testing.T) {
	s := New()
	ctx := context.Background()

	// 未启动就 Shutdown 应安全返回（无监听器可关）。
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown before start: %v, want nil", err)
	}

	s2 := New()
	_ = s2.markStarted()
	_ = s2.Shutdown(ctx)
	_ = s2.Shutdown(ctx) // 第二次不应 panic 或报错
}

// TestSentinelErrors_TLS 验证缺证书 TLS 失败时,用户侧可用 errors.Is(ErrTLSConfig) 判定。
// TestSentinelErrors_TLS verifies that a TLS failure with no certs can be detected
// via errors.Is(ErrTLSConfig).
func TestSentinelErrors_TLS(t *testing.T) {
	s := New()
	if err := s.RunTLS("", "", ""); !errors.Is(err, ErrTLSConfig) {
		t.Errorf("RunTLS err=%v, want errors.Is ErrTLSConfig", err)
	}
}

// TestSentinelErrors_DoubleStart 验证运行中重复启动可经 errors.Is(ErrServerStarted) 判定。
// 第一个 Run 在后台阻塞,其间从主 goroutine 再次启动应被拒。
// TestSentinelErrors_DoubleStart verifies a double start while running is detectable
// via errors.Is(ErrServerStarted). The first Run blocks in the background; a second
// start from the main goroutine must be rejected.
func TestSentinelErrors_DoubleStart(t *testing.T) {
	s := New()
	go func() { _ = s.Run("127.0.0.1:0") }()
	time.Sleep(100 * time.Millisecond) // 等启动 / wait for start

	if err := s.Run("127.0.0.1:0"); !errors.Is(err, ErrServerStarted) {
		t.Errorf("double start err=%v, want errors.Is ErrServerStarted", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// TestSentinelErrors_RestartAfterClose 验证关闭后再启动返回 ErrServerNotStartable。
// TestSentinelErrors_RestartAfterClose verifies starting after close returns
// ErrServerNotStartable.
func TestSentinelErrors_RestartAfterClose(t *testing.T) {
	s := New()
	go func() { _ = s.Run("127.0.0.1:0") }()
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if err := s.Run("127.0.0.1:0"); !errors.Is(err, ErrServerNotStartable) {
		t.Errorf("restart after close err=%v, want errors.Is ErrServerNotStartable", err)
	}
}

// TestEndRun_ListenFailureFallsBackToIdle 验证启动失败后 Server 退回未启动状态：否则 state
// 会永久停在 running，IsStarted 谎报正在服务，且换端口重试会被 ErrServerStarted 挡回。
func TestEndRun_ListenFailureFallsBackToIdle(t *testing.T) {
	s := New()
	if err := s.markStarted(); err != nil {
		t.Fatal(err)
	}

	bindErr := errors.New("listen tcp :80: bind: permission denied")
	if got := s.endRun(bindErr); !errors.Is(got, bindErr) {
		t.Fatalf("endRun() = %v, want the original bind error", got)
	}
	if s.IsStarted() {
		t.Error("IsStarted() = true after a listen failure, want false")
	}
	if s.IsClosed() {
		t.Error("IsClosed() = true after a listen failure, want false (failure is not terminal)")
	}
	// 关键：启动失败不是终态，必须能换地址重试。
	if err := s.markStarted(); err != nil {
		t.Errorf("cannot restart after a listen failure: %v", err)
	}
}

// TestEndRun_PreservesClosedStateOnCleanShutdown 验证正常关闭路径不被回退覆盖：Shutdown 已
// 把状态推进到 closed，底层随即返回 ErrServerClosed，此时不得回退到可启动状态。
func TestEndRun_PreservesClosedStateOnCleanShutdown(t *testing.T) {
	s := New()
	if err := s.markStarted(); err != nil {
		t.Fatal(err)
	}
	s.markClosed()

	if got := s.endRun(ErrServerClosed); !errors.Is(got, ErrServerClosed) {
		t.Fatalf("endRun() = %v, want ErrServerClosed", got)
	}
	if !s.IsClosed() {
		t.Error("IsClosed() = false after a clean shutdown, want true (closed is terminal)")
	}
	if err := s.markStarted(); !errors.Is(err, ErrServerNotStartable) {
		t.Errorf("markStarted() = %v, want ErrServerNotStartable", err)
	}
}

// TestEndRun_RollbackDoesNotResurrectClosedServer 覆盖回退分支里的复合判定：并发 Shutdown
// 抢先推进到终态后，即便底层报的是非 ErrServerClosed 的错误，回退也不得把终态改回 idle
// ——那才是真正的状态污染（把已关闭的 Server 变回可启动）。
func TestEndRun_RollbackDoesNotResurrectClosedServer(t *testing.T) {
	s := New()
	if err := s.markStarted(); err != nil {
		t.Fatal(err)
	}
	s.markClosed() // 模拟并发 Shutdown 先落笔

	_ = s.endRun(errors.New("use of closed network connection"))

	if !s.IsClosed() {
		t.Error("IsClosed() = false, want true (rollback must not overwrite the terminal state)")
	}
	if err := s.markStarted(); !errors.Is(err, ErrServerNotStartable) {
		t.Errorf("markStarted() = %v, want ErrServerNotStartable", err)
	}
}
