package ghttp

import (
	"context"
)

// ===========================================================================
// Engine 一体化便捷入口:让 Engine 像 gin 的 Engine 一样自带 Run/RunTLS/Shutdown,
// 无需显式 NewServer 两步式。内部仍复用 Server(即 *http.Server 的生产封装),因此
// 超时/TLS/BaseContext 等选项与 NewServer 完全一致。需要精细控制底层 http.Server
// 时,仍可显式 NewServer(engine, opts...) 走高级路径。
// Engine's all-in-one convenience entrypoints: like gin's Engine, Engine carries
// Run/RunTLS/Shutdown directly, avoiding the two-step NewServer. Internally it
// reuses Server (the production wrapper over *http.Server), so timeout/TLS/
// BaseContext options match NewServer exactly. For fine-grained control of the
// underlying http.Server, still call NewServer(engine, opts...) for the advanced
// path.
// ===========================================================================

// Run 在 addr 上阻塞式启动明文 HTTP 服务(gin 风格一步式)。addr 为空默认 ":8080"。
// opts 透传给内部 Server(超时、MaxHeaderBytes、BaseContext 等)。正常关闭返回
// ErrServerClosed。启动后可从另一 goroutine 调 Shutdown 优雅停止。
// Run starts a plaintext HTTP server on addr and blocks (gin-style one-liner). An
// empty addr defaults to ":8080". opts pass through to the internal Server
// (timeouts, MaxHeaderBytes, BaseContext, ...). A clean shutdown returns
// ErrServerClosed. After starting, call Shutdown from another goroutine to stop.
func (e *Engine) Run(addr string, opts ...ServerOption) error {
	return e.newBoundServer(addr, opts).ListenAndServe()
}

// RunTLS 在 addr 上阻塞式启动 HTTPS 服务。addr 为空默认 ":8443"。若经 opts 注入了
// WithTLSConfig,certFile/keyFile 可为空;否则二者必填。
// RunTLS starts an HTTPS server on addr and blocks. An empty addr defaults to
// ":8443". If a WithTLSConfig was injected via opts, certFile/keyFile may be empty;
// otherwise both are required.
func (e *Engine) RunTLS(addr, certFile, keyFile string, opts ...ServerOption) error {
	return e.newBoundServer(addr, opts).ListenAndServeTLS(certFile, keyFile)
}

// newBoundServer 构造以本 Engine 为 Handler、绑定 addr 的 Server,并登记为 activeServer
// 供 Shutdown 使用。addr 非空时以 WithAddr 前置覆盖(用户 opts 仍可再覆盖)。
// newBoundServer builds a Server with this Engine as Handler bound to addr, and
// records it as activeServer for Shutdown. A non-empty addr is prepended via
// WithAddr (user opts may still override it).
func (e *Engine) newBoundServer(addr string, opts []ServerOption) *Server {
	all := opts
	if addr != "" {
		all = append([]ServerOption{WithAddr(addr)}, opts...)
	}
	srv := NewServer(e, all...)
	e.srvMu.Lock()
	e.activeServer = srv
	e.srvMu.Unlock()
	return srv
}

// Shutdown 优雅关闭由 Run/RunTLS 启动的服务:关监听、等待进行中请求完成直至 ctx 到期。
// 未经 Run/RunTLS 启动则返回错误。幂等。
// Shutdown gracefully stops a server started by Run/RunTLS: close listeners and
// wait for in-flight requests until ctx expires. Returns an error if not started
// via Run/RunTLS. Idempotent.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.srvMu.Lock()
	srv := e.activeServer
	e.srvMu.Unlock()
	if srv == nil {
		return ErrEngineNotStarted
	}
	return srv.Shutdown(ctx)
}
