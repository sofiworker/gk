package ghttp

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// Run 启动服务器并监听 addr；addr 为空时使用 WithAddr 的地址。
// Run starts the server listening on addr; an empty addr uses the WithAddr address.
func (s *Server) Run(addr string) error {
	hs, err := s.prepareHTTPServer(s.resolveAddr(addr), false)
	if err != nil {
		return err
	}
	return hs.ListenAndServe()
}

// RunTLS 启动 HTTPS 服务器；addr 为空时使用 WithAddr 的地址。
// RunTLS starts the HTTPS server; an empty addr uses the WithAddr address.
func (s *Server) RunTLS(addr, certFile, keyFile string) error {
	hs, err := s.prepareHTTPServer(s.resolveAddr(addr), true)
	if err != nil {
		return err
	}
	return hs.ListenAndServeTLS(certFile, keyFile)
}

// Serve 使用给定的 listener 提供服务。
// Serve serves using the given listener.
func (s *Server) Serve(listener net.Listener) error {
	hs, err := s.prepareHTTPServer("", false)
	if err != nil {
		return err
	}
	return hs.Serve(listener)
}

// ServeTLS 使用给定的 listener 提供 HTTPS 服务。
// ServeTLS serves HTTPS using the given listener.
func (s *Server) ServeTLS(listener net.Listener, certFile, keyFile string) error {
	hs, err := s.prepareHTTPServer("", true)
	if err != nil {
		return err
	}
	return hs.ServeTLS(listener, certFile, keyFile)
}

// RunContext 监听 addr 直到 ctx 结束，然后优雅关闭：最多等待 WithShutdownTimeout 让在途请求完成。
// 正常关闭返回 nil。常与 signal.NotifyContext 搭配使用。
// RunContext listens on addr until ctx ends, then shuts down gracefully, waiting at most
// WithShutdownTimeout for in-flight requests. A clean shutdown returns nil. Pair it with
// signal.NotifyContext.
func (s *Server) RunContext(ctx context.Context, addr string) error {
	hs, err := s.prepareHTTPServer(s.resolveAddr(addr), false)
	if err != nil {
		return err
	}
	return s.serveUntil(ctx, hs.ListenAndServe)
}

// ServeContext 在 listener 上提供服务直到 ctx 结束，然后优雅关闭，语义同 RunContext。
// ServeContext serves on listener until ctx ends, then shuts down gracefully, like RunContext.
func (s *Server) ServeContext(ctx context.Context, listener net.Listener) error {
	hs, err := s.prepareHTTPServer("", false)
	if err != nil {
		return err
	}
	return s.serveUntil(ctx, func() error { return hs.Serve(listener) })
}

// serveUntil 运行 serve，ctx 结束时触发优雅关闭并等待 serve 返回。
// serveUntil runs serve and, when ctx ends, triggers a graceful shutdown and waits for
// serve to return.
func (s *Server) serveUntil(ctx context.Context, serve func() error) error {
	errCh := make(chan error, 1)
	go func() { errCh <- serve() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.config.shutdownTimeout)
	defer cancel()
	shutdownErr := s.Shutdown(sctx)
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}

// resolveAddr 返回实际监听地址：addr 为空时使用配置值。
// resolveAddr returns the address to listen on, falling back to the configured one.
func (s *Server) resolveAddr(addr string) string {
	if addr == "" {
		return s.config.addr
	}
	return addr
}

// prepareHTTPServer 固化处理链，并在锁内创建底层 http.Server；已关闭时返回 ErrServerClosed。
// prepareHTTPServer freezes the handler chain and creates the underlying http.Server
// under lock; returns ErrServerClosed once closed.
func (s *Server) prepareHTTPServer(addr string, withTLS bool) (*http.Server, error) {
	s.frozenHandler()

	s.srvMu.Lock()
	defer s.srvMu.Unlock()

	if s.closed {
		return nil, ErrServerClosed
	}

	hs := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadTimeout:       s.config.readTimeout,
		ReadHeaderTimeout: s.config.readHeaderTimeout,
		WriteTimeout:      s.config.writeTimeout,
		IdleTimeout:       s.config.idleTimeout,
		MaxHeaderBytes:    s.config.maxHeaderBytes,
		BaseContext:       s.config.baseContext,
		ErrorLog:          errorLogFor(s.config),
		Protocols:         s.config.protocols,
		HTTP2:             s.config.http2,
	}
	if withTLS {
		hs.TLSConfig = s.config.tlsConfig
	}
	s.httpServer = hs
	return hs, nil
}

// markClosed 标记服务器已关闭并返回当前底层 http.Server（可能为 nil）。
// markClosed marks the server closed and returns the current underlying http.Server (may be nil).
func (s *Server) markClosed() *http.Server {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	s.closed = true
	return s.httpServer
}

// Shutdown 优雅关闭服务器：停止接收新连接并等待在途请求完成或 ctx 结束。
// 之后的 Run/Serve 调用返回 ErrServerClosed。
// Shutdown gracefully stops the server: it stops accepting connections and waits for
// in-flight requests or ctx. Later Run/Serve calls return ErrServerClosed.
//
// 在途请求结束（或 ctx 结束）后按注册的逆序执行 OnShutdown 钩子，钩子只执行一次；
// 返回值合并 http.Server.Shutdown 与钩子的错误。
// After in-flight requests finish (or ctx ends) the OnShutdown hooks run once in reverse
// registration order; the result joins the http.Server.Shutdown and hook errors.
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	if hs := s.markClosed(); hs != nil {
		err = hs.Shutdown(ctx)
	}
	return errors.Join(err, s.runShutdownHooks(ctx))
}

// OnShutdown 注册关闭钩子（如关闭数据库连接池），在 Shutdown/Close 时、在途请求结束后按注册的
// 逆序执行，类似 defer。钩子应尊重 ctx（Close 时传入 context.Background()）。
// 在已执行过关闭后注册的钩子不会再被调用。nil 被忽略。
// OnShutdown registers a shutdown hook (e.g. closing a database pool). Hooks run on
// Shutdown/Close after in-flight requests finish, in reverse registration order like
// defer. Hooks should honor ctx (Close passes context.Background()). Hooks registered
// after shutdown already ran are never called. nil is ignored.
func (s *Server) OnShutdown(fn func(ctx context.Context) error) *Server {
	if fn == nil {
		return s
	}
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	s.hooks = append(s.hooks, fn)
	return s
}

// runShutdownHooks 逆序执行关闭钩子一次，并缓存合并后的错误。
// runShutdownHooks runs the hooks once in reverse order and caches the joined error.
func (s *Server) runShutdownHooks(ctx context.Context) error {
	s.hooksOnce.Do(func() {
		s.srvMu.Lock()
		hooks := append([]func(context.Context) error(nil), s.hooks...)
		s.srvMu.Unlock()
		errs := make([]error, 0, len(hooks))
		for i := len(hooks) - 1; i >= 0; i-- {
			errs = append(errs, hooks[i](ctx))
		}
		s.hooksErr = errors.Join(errs...)
	})
	return s.hooksErr
}

// Close 立即关闭服务器。之后的 Run/Serve 调用返回 ErrServerClosed。
// Close immediately closes the server. Later Run/Serve calls return ErrServerClosed.
//
// 之后执行 OnShutdown 钩子（使用 context.Background()），返回值合并两者的错误。
// The OnShutdown hooks then run with context.Background(); the result joins both errors.
func (s *Server) Close() error {
	var err error
	if hs := s.markClosed(); hs != nil {
		err = hs.Close()
	}
	return errors.Join(err, s.runShutdownHooks(context.Background()))
}
