package ghttp

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"time"
)

// 默认值。
// Defaults.
const (
	// DefaultMaxBodyBytes 是默认请求体上限：32 MiB。
	// DefaultMaxBodyBytes is the default request-body limit: 32 MiB.
	DefaultMaxBodyBytes int64 = 32 << 20

	// DefaultReadHeaderTimeout 是默认读请求头超时，用于抵御慢速请求头攻击。
	// DefaultReadHeaderTimeout is the default header read timeout, defending against
	// slow-header attacks.
	DefaultReadHeaderTimeout = 10 * time.Second

	// DefaultShutdownTimeout 是 RunContext/ServeContext 优雅关闭的默认等待上限。
	// DefaultShutdownTimeout is the default grace period for RunContext/ServeContext.
	DefaultShutdownTimeout = 30 * time.Second
)

// serverConfig 存储服务器配置。
// serverConfig stores server configuration.
type serverConfig struct {
	addr              string
	readTimeout       time.Duration
	readHeaderTimeout time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	shutdownTimeout   time.Duration
	maxHeaderBytes    int
	maxBodyBytes      int64
	strictPath        bool
	fixPath           bool
	tlsConfig         *tls.Config
	protocols         *http.Protocols
	http2             *http.HTTP2Config
	baseContext       func(net.Listener) context.Context
	errorLog          *log.Logger
	logger            Logger
	errorHandler      func(context.Context, *Request, *Response, error)
}

// defaultServerConfig 返回默认配置。
// defaultServerConfig returns the default configuration.
func defaultServerConfig() serverConfig {
	return serverConfig{
		addr:              ":8080",
		readTimeout:       15 * time.Second,
		readHeaderTimeout: DefaultReadHeaderTimeout,
		writeTimeout:      15 * time.Second,
		idleTimeout:       60 * time.Second,
		shutdownTimeout:   DefaultShutdownTimeout,
		maxHeaderBytes:    1 << 20,
		maxBodyBytes:      DefaultMaxBodyBytes,
		errorHandler:      defaultErrorHandler,
	}
}

// ServerOption 配置服务器选项。
// ServerOption configures server options.
type ServerOption func(*serverConfig)

// WithAddr 设置默认监听地址，Run("") 与 RunContext(ctx, "") 使用它。
// WithAddr sets the default listen address used by Run("") and RunContext(ctx, "").
func WithAddr(addr string) ServerOption {
	return func(c *serverConfig) { c.addr = addr }
}

// WithReadTimeout 设置读取整个请求（含 body）的超时。
// WithReadTimeout sets the timeout for reading the whole request, including the body.
func WithReadTimeout(timeout time.Duration) ServerOption {
	return func(c *serverConfig) { c.readTimeout = timeout }
}

// WithReadHeaderTimeout 设置读取请求头的超时，默认 DefaultReadHeaderTimeout。
// WithReadHeaderTimeout sets the request-header read timeout (default DefaultReadHeaderTimeout).
func WithReadHeaderTimeout(timeout time.Duration) ServerOption {
	return func(c *serverConfig) { c.readHeaderTimeout = timeout }
}

// WithWriteTimeout 设置写入超时。
// WithWriteTimeout sets the write timeout.
func WithWriteTimeout(timeout time.Duration) ServerOption {
	return func(c *serverConfig) { c.writeTimeout = timeout }
}

// WithIdleTimeout 设置空闲连接超时。
// WithIdleTimeout sets the idle connection timeout.
func WithIdleTimeout(timeout time.Duration) ServerOption {
	return func(c *serverConfig) { c.idleTimeout = timeout }
}

// WithShutdownTimeout 设置 RunContext/ServeContext 在 ctx 结束后等待在途请求的上限。
// WithShutdownTimeout sets how long RunContext/ServeContext wait for in-flight requests
// after ctx ends.
func WithShutdownTimeout(timeout time.Duration) ServerOption {
	return func(c *serverConfig) { c.shutdownTimeout = timeout }
}

// WithMaxHeaderBytes 设置最大头部字节数。
// WithMaxHeaderBytes sets the maximum header bytes.
func WithMaxHeaderBytes(n int) ServerOption {
	return func(c *serverConfig) { c.maxHeaderBytes = n }
}

// WithMaxBodyBytes 设置所有路由默认的请求体上限（字节），默认 DefaultMaxBodyBytes；n<=0 表示不限。
// 路由可用 WithBodyLimit 覆盖。在注册时生效，应在 Register 之前设置。
// WithMaxBodyBytes sets the default request-body limit in bytes for all routes (default
// DefaultMaxBodyBytes); n<=0 means unlimited. Routes override it with WithBodyLimit. It is
// applied at registration, so set it before Register.
func WithMaxBodyBytes(n int64) ServerOption {
	return func(c *serverConfig) { c.maxBodyBytes = n }
}

// WithStrictPath 额外拒绝含空段（"//"）的请求路径，返回 400。"." 与 ".." 段始终被拒绝。
// WithStrictPath additionally rejects request paths with empty segments ("//") with 400.
// "." and ".." segments are always rejected.
func WithStrictPath() ServerOption {
	return func(c *serverConfig) { c.strictPath = true }
}

// WithRedirectFixedPath 开启路径修正重定向：路由未命中时，先清理多余的 '/'（如 "/a//b"），
// 再做大小写不敏感匹配；能修正到已注册路由时重定向过去（GET/HEAD 用 301，其他方法用 308）。
// 默认关闭，以免同一资源出现多个可访问的 URL。
// WithRedirectFixedPath enables fixed-path redirects: when no route matches, redundant
// '/' are cleaned (e.g. "/a//b") and a case-insensitive lookup is attempted; if that hits a
// registered route the client is redirected there (301 for GET/HEAD, 308 otherwise). Off by
// default so a resource does not become reachable under several URLs.
func WithRedirectFixedPath() ServerOption {
	return func(c *serverConfig) { c.fixPath = true }
}

// WithProtocols 设置底层 http.Server 启用的协议集合（HTTP/1、HTTP/2、明文 HTTP/2）。
// 未设置时沿用标准库默认值：HTTP/1 总是启用，TLS 下协商 HTTP/2。
// WithProtocols sets the protocols enabled on the underlying http.Server (HTTP/1,
// HTTP/2, unencrypted HTTP/2). Unset keeps the stdlib default: HTTP/1 always, HTTP/2
// negotiated over TLS.
func WithProtocols(p *http.Protocols) ServerOption {
	return func(c *serverConfig) { c.protocols = p }
}

// WithH2C 在 HTTP/1 与（TLS 下的）HTTP/2 之外额外启用明文 HTTP/2（h2c，prior knowledge）。
// 适用于服务网格或 gRPC 网关等由前置代理终止 TLS 的场景；不要直接暴露在公网上。
// WithH2C additionally enables unencrypted HTTP/2 (h2c, prior knowledge) next to HTTP/1
// and HTTP/2 over TLS. Meant for meshes or gateways where a proxy terminates TLS; do not
// expose it directly to the internet.
func WithH2C() ServerOption {
	return func(c *serverConfig) {
		p := new(http.Protocols)
		p.SetHTTP1(true)
		p.SetHTTP2(true)
		p.SetUnencryptedHTTP2(true)
		c.protocols = p
	}
}

// WithHTTP2Config 设置 HTTP/2 参数（并发流数、帧大小、ping 超时等），透传给 http.Server.HTTP2。
// WithHTTP2Config sets HTTP/2 parameters (concurrent streams, frame size, ping timeouts,
// ...), passed through to http.Server.HTTP2.
func WithHTTP2Config(cfg *http.HTTP2Config) ServerOption {
	return func(c *serverConfig) { c.http2 = cfg }
}

// WithTLSConfig 设置 TLS 配置。
// WithTLSConfig sets the TLS configuration.
func WithTLSConfig(tlsConfig *tls.Config) ServerOption {
	return func(c *serverConfig) { c.tlsConfig = tlsConfig }
}

// WithBaseContext 设置每个 listener 的基础 context，请求 context 由它派生。
// WithBaseContext sets the per-listener base context from which request contexts derive.
func WithBaseContext(fn func(net.Listener) context.Context) ServerOption {
	return func(c *serverConfig) { c.baseContext = fn }
}

// WithErrorLog 设置框架内部告警与 net/http 错误的日志器；nil 时使用 log 包默认日志器。
// WithErrorLog sets the logger for framework warnings and net/http errors; nil uses the
// log package's default logger.
func WithErrorLog(l *log.Logger) ServerOption {
	return func(c *serverConfig) { c.errorLog = l }
}

// WithLogger 注入框架日志器：框架告警（如启动后调用 Use）以 Warnf 输出；未设置 WithErrorLog 时，
// net/http 的内部错误（如 TLS 握手失败）也以 Errorf 转发给它。nil 恢复默认（log 包）。
// WithLogger injects the framework logger: framework warnings (such as Use after start) go
// to Warnf; without WithErrorLog, net/http internal errors (TLS handshake failures, ...)
// are forwarded to Errorf as well. nil restores the default (the log package).
func WithLogger(l Logger) ServerOption {
	return func(c *serverConfig) { c.logger = l }
}

// WithErrorHandler 设置错误处理器，替换默认的 JSON 错误响应。可用 StatusFromError、
// ErrorResponseOf 复用默认映射。
// WithErrorHandler sets the error handler, replacing the default JSON error response.
// StatusFromError and ErrorResponseOf expose the default mapping for reuse.
func WithErrorHandler(handler func(context.Context, *Request, *Response, error)) ServerOption {
	return func(c *serverConfig) { c.errorHandler = handler }
}
