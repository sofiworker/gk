package v2

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	root "github.com/sofiworker/gk/ghttp"
)

// Server 组合 v2 路由注册器与 HTTP 监听生命周期；零值不可用。
// Server combines the v2 route registrar with an HTTP listener lifecycle; its zero value is invalid.
type Server struct {
	backend *root.Server
	api     *API
	mu      sync.RWMutex
	routes  []Route
}

type serverRegistrar struct{ server *Server }

func (r serverRegistrar) RawHandle(method, path string, handler root.RawHandlerFunc) error {
	return r.server.backend.RawHandle(method, path, handler)
}

func (r serverRegistrar) registerRoute(route Route) error {
	err := r.RawHandle(route.Method, route.Path, func(ctx context.Context, req *Request, resp *Response) error {
		return route.Serve(ctx, req, resp)
	})
	if err != nil {
		return err
	}
	r.server.mu.Lock()
	r.server.routes = append(r.server.routes, route)
	r.server.mu.Unlock()
	return nil
}

// ServerOption 在构造时配置监听与错误处理。
// ServerOption configures listening and error handling at construction time.
type ServerOption func(*serverSettings)

type serverSettings struct{ options []root.Option }

// Middleware 包裹端点处理器。
// Middleware wraps an endpoint handler.
type Middleware = root.Middleware

// ErrServerStarted 表示服务已经启动。
// ErrServerStarted indicates the server has already started.
var ErrServerStarted = root.ErrServerStarted

// ErrServerNotStartable 表示服务已经关闭，不能再次启动。
// ErrServerNotStartable indicates the server has closed and cannot start again.
var ErrServerNotStartable = root.ErrServerNotStartable

// ErrServerClosed 表示监听服务正常关闭。
// ErrServerClosed indicates the serving loop stopped after a normal shutdown.
var ErrServerClosed = root.ErrServerClosed

// ErrTLSConfig 表示 HTTPS 缺少证书配置。
// ErrTLSConfig indicates missing HTTPS certificate configuration.
var ErrTLSConfig = root.ErrTLSConfig

// NewServer 创建独立的 v2 HTTP 服务。
// NewServer creates a standalone v2 HTTP server.
func NewServer(opts ...ServerOption) *Server {
	settings := serverSettings{}
	for _, opt := range opts {
		if opt != nil {
			opt(&settings)
		}
	}
	backend := root.New(settings.options...)
	s := &Server{backend: backend}
	s.api = New(serverRegistrar{server: s})
	return s
}

// Register 校验并挂载一批 v2 路由。
// Register validates and mounts a batch of v2 routes.
func (s *Server) Register(routes ...Route) error { return s.api.Register(routes...) }

// Group 创建继承服务级默认配置的路由组。
// Group creates a route group inheriting server-level defaults.
func (s *Server) Group(prefix string, opts ...GroupOption) *Group {
	return s.api.Group(prefix, opts...)
}

// With 设置随后注册路由的默认选项。
// With sets defaults for routes registered subsequently.
func (s *Server) With(opts ...Option) *Server { s.api.With(opts...); return s }

// Use 添加全局中间件；应在注册路由和启动服务前调用。
// Use adds global middleware; call it before registering routes and serving.
func (s *Server) Use(middleware ...Middleware) *Server {
	s.backend.Use(middleware...)
	return s
}

// ServeHTTP 处理单个 HTTP 请求。
// ServeHTTP handles one HTTP request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.backend.ServeHTTP(w, r) }

// Run 阻塞监听 HTTP 地址；正常关闭返回 ErrServerClosed。
// Run blocks serving an HTTP address; a clean shutdown returns ErrServerClosed.
func (s *Server) Run(addr string) error { return s.backend.Run(addr) }

// RunTLS 阻塞监听 HTTPS 地址。
// RunTLS blocks serving an HTTPS address.
func (s *Server) RunTLS(addr, certFile, keyFile string) error {
	return s.backend.RunTLS(addr, certFile, keyFile)
}

// Serve 在已有监听器上阻塞服务。
// Serve blocks serving on an existing listener.
func (s *Server) Serve(listener net.Listener) error { return s.backend.Serve(listener) }

// ServeTLS 在已有监听器上阻塞服务 HTTPS；缺少 TLSConfig 证书时需指定证书文件。
// ServeTLS blocks serving HTTPS on an existing listener; provide certificate files unless TLSConfig has certificates.
func (s *Server) ServeTLS(listener net.Listener, certFile, keyFile string) error {
	return s.backend.ServeTLS(listener, certFile, keyFile)
}

// Shutdown 等待活动请求完成，直到 context 到期。
// Shutdown waits for active requests to finish until the context expires.
func (s *Server) Shutdown(ctx context.Context) error { return s.backend.Shutdown(ctx) }

// Close 立即停止服务。
// Close stops the server immediately.
func (s *Server) Close() error { return s.backend.Close() }

// WithAddr 设置 Run/RunTLS 的默认监听地址。
// WithAddr sets the default address for Run and RunTLS.
func WithAddr(addr string) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithAddr(addr)) }
}

// WithReadTimeout 设置整个请求的读取时限。
// WithReadTimeout sets the deadline for reading an entire request.
func WithReadTimeout(timeout time.Duration) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithReadTimeout(timeout)) }
}

// WithReadHeaderTimeout 设置请求头读取时限。
// WithReadHeaderTimeout sets the deadline for reading request headers.
func WithReadHeaderTimeout(timeout time.Duration) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithReadHeaderTimeout(timeout)) }
}

// WithWriteTimeout 设置响应写入时限。
// WithWriteTimeout sets the deadline for writing a response.
func WithWriteTimeout(timeout time.Duration) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithWriteTimeout(timeout)) }
}

// WithIdleTimeout 设置长连接空闲时限。
// WithIdleTimeout sets the keep-alive idle timeout.
func WithIdleTimeout(timeout time.Duration) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithIdleTimeout(timeout)) }
}

// WithMaxHeaderBytes 设置允许解析的最大请求头字节数。
// WithMaxHeaderBytes limits parsed request header bytes.
func WithMaxHeaderBytes(limit int) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithMaxHeaderBytes(limit)) }
}

// WithTLSConfig 配置 TLS 证书及连接策略。
// WithTLSConfig configures TLS certificates and connection policy.
func WithTLSConfig(cfg *tls.Config) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithTLSConfig(cfg)) }
}

// WithBaseContext 设置入站连接的基础 context。
// WithBaseContext sets the base context for incoming connections.
func WithBaseContext(fn func(net.Listener) context.Context) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithBaseContext(fn)) }
}

// WithErrorHandler 观察处理错误及客户端实际收到的状态码。
// WithErrorHandler observes handler errors and the status received by the client.
func WithErrorHandler(handler func(*http.Request, int, error)) ServerOption {
	return func(s *serverSettings) { s.options = append(s.options, root.WithErrorHook(handler)) }
}
