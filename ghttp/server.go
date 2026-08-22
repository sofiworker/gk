package ghttp

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server 是包裹 *Engine 的生产级 HTTP Server：管理监听生命周期、优雅关闭与超时配置。
// 零值不可用，必须经 NewServer 构造。多数场景可直接用 Engine.Run/RunTLS 一步式启动；
// 仅在需要精细控制底层 http.Server（复用监听器、ServeTLS、Close 等）时才显式用 Server。
// Server is a production-grade HTTP server wrapping an *Engine: it manages the
// listen lifecycle, graceful shutdown, and timeout configuration. The zero value
// is unusable; construct it via NewServer. Most cases can just use Engine.Run/RunTLS
// one-liners; use Server explicitly only for fine-grained control of the underlying
// http.Server (listener reuse, ServeTLS, Close, etc.).
type Server struct {
	httpSrv *http.Server

	mu      sync.Mutex
	started bool
	closed  bool
}

// ServerOption 以函数式选项配置 Server（对齐仓库 WithXxx 约定）。
// ServerOption configures a Server via functional options (matching the repo's
// WithXxx convention).
type ServerOption func(*Server)

// NewServer 从 *Engine 构造生产级 Server，opts 按顺序应用。默认无读写超时，
// IdleTimeout=60s（长连接闲置回收），MaxHeaderBytes 用标准库默认。
// NewServer builds a production Server from an *Engine, applying opts in order.
// Defaults: no read/write timeout, IdleTimeout=60s (idle keep-alive reaping),
// standard-library default MaxHeaderBytes.
func NewServer(e *Engine, opts ...ServerOption) *Server {
	s := &Server{
		httpSrv: &http.Server{
			Handler:     e,
			IdleTimeout: 60 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithAddr 设置监听地址（形如 ":8080" 或 "127.0.0.1:8080"）。
// WithAddr sets the listen address (e.g. ":8080" or "127.0.0.1:8080").
func WithAddr(addr string) ServerOption {
	return func(s *Server) { s.httpSrv.Addr = addr }
}

// WithReadTimeout 设置读取整个请求（含 body）的最大时长；<=0 表示无超时。
// WithReadTimeout caps reading the entire request (including body); <=0 disables.
func WithReadTimeout(d time.Duration) ServerOption {
	return func(s *Server) { s.httpSrv.ReadTimeout = d }
}

// WithReadHeaderTimeout 设置读取请求头的最大时长；<=0 表示回退到 ReadTimeout。
// WithReadHeaderTimeout caps reading request headers; <=0 falls back to ReadTimeout.
func WithReadHeaderTimeout(d time.Duration) ServerOption {
	return func(s *Server) { s.httpSrv.ReadHeaderTimeout = d }
}

// WithWriteTimeout 设置写响应的最大时长；<=0 表示无超时。
// WithWriteTimeout caps writing the response; <=0 disables.
func WithWriteTimeout(d time.Duration) ServerOption {
	return func(s *Server) { s.httpSrv.WriteTimeout = d }
}

// WithIdleTimeout 设置 keep-alive 连接的空闲最大时长；<=0 表示无超时。
// WithIdleTimeout caps idle time for keep-alive connections; <=0 disables.
func WithIdleTimeout(d time.Duration) ServerOption {
	return func(s *Server) { s.httpSrv.IdleTimeout = d }
}

// WithMaxHeaderBytes 设置请求头（含请求行）解析的最大字节数；<=0 用标准库默认。
// WithMaxHeaderBytes caps bytes parsed for request headers (incl. request line);
// <=0 uses the standard-library default.
func WithMaxHeaderBytes(n int) ServerOption {
	return func(s *Server) {
		if n > 0 {
			s.httpSrv.MaxHeaderBytes = n
		}
	}
}

// WithTLSConfig 直接注入 *tls.Config（用于 mTLS、自定义 CA、ALPN 等高级场景）。
// WithTLSConfig injects a *tls.Config directly (for mTLS, custom CA, ALPN, etc.).
func WithTLSConfig(cfg *tls.Config) ServerOption {
	return func(s *Server) { s.httpSrv.TLSConfig = cfg }
}

// WithBaseContext 设置所有入站请求的根 context 构造器，便于注入全局取消或值。
// WithBaseContext sets the base-context constructor for all inbound requests,
// useful for injecting global cancellation or values.
func WithBaseContext(fn func(net.Listener) context.Context) ServerOption {
	return func(s *Server) { s.httpSrv.BaseContext = fn }
}

// ListenAndServe 在配置地址上阻塞式启动明文 HTTP 服务。正常关闭时返回
// ErrServerClosed（可 errors.Is 判定）；地址为空则默认 ":8080"。
// ListenAndServe starts a plaintext HTTP server on the configured address and
// blocks. Returns ErrServerClosed on a clean shutdown (test via errors.Is);
// an empty address defaults to ":8080".
func (s *Server) ListenAndServe() error {
	if err := s.markStarted(); err != nil {
		return err
	}
	if s.httpSrv.Addr == "" {
		s.httpSrv.Addr = ":8080"
	}
	return s.httpSrv.ListenAndServe()
}

// ListenAndServeTLS 在配置地址上阻塞式启动 HTTPS 服务。若已注入 TLSConfig，
// certFile/keyFile 可为空；否则二者必填。
// ListenAndServeTLS starts an HTTPS server on the configured address and blocks.
// certFile/keyFile may be empty if a TLSConfig was injected; otherwise both are
// required.
func (s *Server) ListenAndServeTLS(certFile, keyFile string) error {
	if s.httpSrv.TLSConfig == nil && (certFile == "" || keyFile == "") {
		return ErrTLSConfig
	}
	if err := s.markStarted(); err != nil {
		return err
	}
	if s.httpSrv.Addr == "" {
		s.httpSrv.Addr = ":8443"
	}
	return s.httpSrv.ListenAndServeTLS(certFile, keyFile)
}

// Serve 在已有 net.Listener 上启动服务，供自定义监听器（Unix socket、限流监听等）
// 或测试注入使用。
// Serve runs the server on an existing net.Listener, for custom listeners (Unix
// socket, throttled listener, etc.) or test injection.
func (s *Server) Serve(l net.Listener) error {
	if err := s.markStarted(); err != nil {
		return err
	}
	return s.httpSrv.Serve(l)
}

// ServeTLS 在已有 net.Listener 上启动 HTTPS 服务。若已注入 TLSConfig（含证书），
// certFile/keyFile 可为空；否则二者必填。供自定义监听器或测试注入使用。
// ServeTLS runs an HTTPS server on an existing net.Listener. certFile/keyFile may
// be empty if a TLSConfig with certificates was injected; otherwise both are
// required. For custom listeners or test injection.
func (s *Server) ServeTLS(l net.Listener, certFile, keyFile string) error {
	hasConfigCert := s.httpSrv.TLSConfig != nil && len(s.httpSrv.TLSConfig.Certificates) > 0
	if !hasConfigCert && (certFile == "" || keyFile == "") {
		return ErrTLSConfig
	}
	if err := s.markStarted(); err != nil {
		return err
	}
	return s.httpSrv.ServeTLS(l, certFile, keyFile)
}

// Shutdown 优雅关闭：先关监听器拒绝新连接，再等待进行中请求完成，直到 ctx 到期。
// 幂等；ctx 到期时返回其错误。
// Shutdown gracefully stops the server: it closes listeners to reject new
// connections, then waits for in-flight requests until ctx expires. Idempotent;
// returns ctx's error if it expires first.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	return s.httpSrv.Shutdown(ctx)
}

// Close 立即关闭：强制中断所有活动连接，不等待进行中请求。仅用于紧急停止。
// Close stops immediately, forcibly interrupting all active connections without
// waiting for in-flight requests. Use only for emergency stops.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	return s.httpSrv.Close()
}

// markStarted 原子地把 Server 标记为已启动，重复启动或关闭后启动均报错。
// markStarted atomically flags the server started, rejecting a double start or a
// start after close.
func (s *Server) markStarted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrServerNotStartable
	}
	if s.started {
		return ErrServerStarted
	}
	s.started = true
	return nil
}

// IsStarted 报告 Server 是否已启动。
// IsStarted reports whether the server has started.
func (s *Server) IsStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// IsClosed 报告 Server 是否已关闭。
// IsClosed reports whether the server has been shut down.
func (s *Server) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// unwrap 暴露底层 *http.Server，仅供同包测试断言配置用。
// unwrap exposes the underlying *http.Server for same-package test assertions.
func (s *Server) unwrap() *http.Server {
	return s.httpSrv
}
