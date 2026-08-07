package ghttp

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"time"
)

// DefaultMaxBodyBytes 是自动解码请求体的默认最大大小。
// DefaultMaxBodyBytes is the default maximum decoded request body size.
const DefaultMaxBodyBytes int64 = 4 << 20

// Config 保存服务器配置。
// Config holds server configuration.
type Config struct {
	address                   string
	renderer                  Renderer
	validator                 Validator
	logger                    Logger
	envelope                  EnvelopeFunc
	errorHandler              ErrorHandler
	errorWriter               ErrorWriter
	bodyDecoder               BodyDecodeFunc
	produces                  []string
	consumes                  []string
	clientIPResolver          ClientIPResolver
	vfsPath                   string
	strictRouting             bool
	exposeErrorDetails        bool
	openAPIEnabled            bool
	openAPITitle              string
	openAPIVersion            string
	openAPIPath               string
	openAPIPathSet            bool
	openAPIServers            []string
	openAPISecurity           []map[string][]string
	lenientContentNegotiation bool
	lenientContentType        bool
	problemDetails            bool
	webSocketCheckOrigin      func(*http.Request) bool
	webSocketSubprotocols     []string
	webSocketReadBufferSize   int
	webSocketWriteBufferSize  int
	webSocketPingPeriod       time.Duration
	webSocketPongWait         time.Duration
	readTimeout               time.Duration
	readHeaderTimeout         time.Duration
	writeTimeout              time.Duration
	idleTimeout               time.Duration
	maxHeaderBytes            int
	maxBodyBytes              int64
	tlsConfig                 *tls.Config
	baseContext               func(net.Listener) context.Context
	connContext               func(context.Context, net.Conn) context.Context
	errorLog                  *log.Logger
}

// ClientIPResolver 从 HTTP 请求解析客户端 IP。
// ClientIPResolver resolves a client IP from an HTTP request.
type ClientIPResolver func(*http.Request) string

// ServerOption 配置 Server。
// ServerOption configures a Server.
type ServerOption func(*Config)

// WithAddress 设置服务器监听地址。
// WithAddress sets the server listen address.
func WithAddress(addr string) ServerOption {
	return func(c *Config) {
		c.address = addr
	}
}

// WithValidator 设置自定义校验器。
// WithValidator sets a custom validator.
func WithValidator(v Validator) ServerOption {
	return func(c *Config) {
		c.validator = v
	}
}

// WithRenderer 设置模板渲染器。
// WithRenderer sets the template renderer.
func WithRenderer(renderer Renderer) ServerOption {
	return func(c *Config) {
		c.renderer = renderer
	}
}

// WithLogger 设置内置日志中间件使用的服务器日志。
// WithLogger sets the server logger for built-in logging middleware.
func WithLogger(logger Logger) ServerOption {
	return func(c *Config) {
		c.logger = logger
	}
}

// WithEnvelope 设置自定义 envelope 函数。
// WithEnvelope sets a custom envelope function.
func WithEnvelope(fn EnvelopeFunc) ServerOption {
	return func(c *Config) {
		c.envelope = fn
	}
}

// WithErrorHandler 替换默认框架错误响应 writer。
// WithErrorHandler replaces the default error response writer.
func WithErrorHandler(handler ErrorHandler) ServerOption {
	return func(c *Config) {
		c.errorHandler = handler
	}
}

// WithErrorWriter 安装可组合的错误 writer，用于服务器级错误。
// WithErrorWriter installs a composable error writer for server-wide errors.
// （404/405/panic 及无自有 writer 的路由）；返回 true 表示已处理。
// (404/405/panic and routes without their own writer); true means handled.
// false 则回退到内置错误 writer。
// false falls through to the built-in error writer.
func WithErrorWriter(writer ErrorWriter) ServerOption {
	return func(c *Config) {
		c.errorWriter = writer
	}
}

// WithBodyDecoder 为服务器设置自定义请求体解码器。
// WithBodyDecoder sets a custom request body decoder.
func WithBodyDecoder(fn BodyDecodeFunc) ServerOption {
	return func(c *Config) {
		c.bodyDecoder = fn
	}
}

// WithProduces 设置路由自动编码的默认响应 Content-Type。
// WithProduces sets default response Content-Types.
func WithProduces(contentTypes ...string) ServerOption {
	return func(c *Config) {
		c.produces = normalizeContentTypes(contentTypes)
	}
}

// WithConsumes 设置自动解码的默认请求 Content-Type。
// WithConsumes sets default request Content-Types.
func WithConsumes(contentTypes ...string) ServerOption {
	return func(c *Config) {
		c.consumes = normalizeContentTypes(contentTypes)
	}
}

// WithStrictRouting 让尾斜杠成为路由标识的一部分。
// WithStrictRouting makes trailing slashes part of route identity.
func WithStrictRouting() ServerOption {
	return func(c *Config) {
		c.strictRouting = true
	}
}

// WithOpenAPI 设置 OpenAPI 文档标题与版本。
// WithOpenAPI sets the OpenAPI document title and version.
func WithOpenAPI(title, version string) ServerOption {
	return func(c *Config) {
		c.openAPIEnabled = true
		c.openAPITitle = title
		c.openAPIVersion = version
	}
}

// WithOpenAPIPath 设置暴露 OpenAPI 文档的 HTTP 端点。
// WithOpenAPIPath sets the OpenAPI HTTP endpoint.
// 空路径仅禁用 HTTP 暴露，不关闭 Server.OpenAPI。
// empty path disables HTTP exposure only.
func WithOpenAPIPath(path string) ServerOption {
	return func(c *Config) {
		c.openAPIPath = path
		c.openAPIPathSet = true
	}
}

// WithOpenAPIServers 设置 OpenAPI servers 列表。
// WithOpenAPIServers sets the OpenAPI servers list.
func WithOpenAPIServers(urls ...string) ServerOption {
	return func(c *Config) {
		c.openAPIServers = append([]string(nil), urls...)
	}
}

// WithOpenAPISecurity 设置 OpenAPI 顶层安全要求。
// WithOpenAPISecurity sets the OpenAPI top-level security requirements.
func WithOpenAPISecurity(requirements ...map[string][]string) ServerOption {
	return func(c *Config) {
		c.openAPISecurity = append([]map[string][]string(nil), requirements...)
	}
}

// Deprecated: WithStrictContentNegotiation 是保留的 no-op。
// Deprecated: WithStrictContentNegotiation is a no-op kept for source compatibility.
// 默认行为已返回 406；可用 WithLenientContentNegotiation 显式放宽。
// the default already returns 406; use WithLenientContentNegotiation to opt out.
func WithStrictContentNegotiation() ServerOption {
	return func(c *Config) {
		c.lenientContentNegotiation = false
	}
}

// WithLenientContentNegotiation 无匹配时回退到第一个 Produces。
// WithLenientContentNegotiation falls back to the first Produces type.
// （gin 风格便利）。
// (gin-style convenience).
func WithLenientContentNegotiation() ServerOption {
	return func(c *Config) {
		c.lenientContentNegotiation = true
	}
}

// Deprecated: WithStrictContentType 是保留的 no-op。
// Deprecated: WithStrictContentType is a no-op kept for source compatibility.
// 默认对未知/缺失 Content-Type 返回 415；可用 WithLenientContentType 放宽。
// the default already returns 415; use WithLenientContentType to opt out.
func WithStrictContentType() ServerOption {
	return func(c *Config) {
		c.lenientContentType = false
	}
}

// WithLenientContentType 将未知请求 Content-Type 按 JSON 解码。
// WithLenientContentType decodes unknown Content-Types as JSON.
// （gin 风格便利）；默认 415。
// (gin-style convenience); default is 415.
func WithLenientContentType() ServerOption {
	return func(c *Config) {
		c.lenientContentType = true
	}
}

// WithProblemDetails 启用 RFC 9457 application/problem+json 错误响应。
// WithProblemDetails enables RFC 9457 problem+json errors.
// 默认使用框架的 {code,message} 错误体。
// default is the framework {code,message} error body.
func WithProblemDetails() ServerOption {
	return func(c *Config) {
		c.problemDetails = true
	}
}

// WithWebSocketOriginChecker 替换默认的同源 WebSocket 校验。
// WithWebSocketOriginChecker replaces the default same-origin check.
// 默认允许同源或缺 Origin。
// default: same-origin or missing Origin is allowed.
func WithWebSocketOriginChecker(check func(*http.Request) bool) ServerOption {
	return func(c *Config) {
		c.webSocketCheckOrigin = check
	}
}

// WithServerWebSocketSubprotocols 设置服务器接受的子协议。
// WithServerWebSocketSubprotocols sets the accepted subprotocols.
// 协商结果通过 WebSocketConn.Subprotocol 获取。
// the negotiated one is available via WebSocketConn.Subprotocol.
func WithServerWebSocketSubprotocols(protos []string) ServerOption {
	return func(c *Config) {
		c.webSocketSubprotocols = append([]string(nil), protos...)
	}
}

// WithServerWebSocketReadBufferSize 设置服务器 WebSocket 读缓冲大小。
// WithServerWebSocketReadBufferSize sets the read buffer size.
func WithServerWebSocketReadBufferSize(size int) ServerOption {
	return func(c *Config) {
		c.webSocketReadBufferSize = size
	}
}

// WithServerWebSocketWriteBufferSize 设置服务器 WebSocket 写缓冲大小。
// WithServerWebSocketWriteBufferSize sets the write buffer size.
func WithServerWebSocketWriteBufferSize(size int) ServerOption {
	return func(c *Config) {
		c.webSocketWriteBufferSize = size
	}
}

// WithServerWebSocketPingPeriod 以给定周期启用服务器 keepalive ping。
// WithServerWebSocketPingPeriod enables keepalive pings at the given period.
// 需要正的 WithServerWebSocketPongWait；默认关闭。
// requires a positive pong wait; disabled by default.
func WithServerWebSocketPingPeriod(period time.Duration) ServerOption {
	return func(c *Config) {
		c.webSocketPingPeriod = period
	}
}

// WithServerWebSocketPongWait 设置服务器等待 pong 的超时。
// WithServerWebSocketPongWait sets the pong wait before the conn is dead.
// 仅与 WithServerWebSocketPingPeriod 一起生效；默认关闭。
// only effective with the ping period; disabled by default.
func WithServerWebSocketPongWait(wait time.Duration) ServerOption {
	return func(c *Config) {
		c.webSocketPongWait = wait
	}
}

// WithExposeErrorDetails 启用错误响应中的内部错误消息。
// WithExposeErrorDetails enables internal error messages in error bodies.
// 默认非 HTTPError 仅返回状态文本，显式 HTTPError 消息总是返回。
// default: non-HTTPError returns status text; explicit messages always return.
func WithExposeErrorDetails() ServerOption {
	return func(c *Config) {
		c.exposeErrorDetails = true
	}
}

// WithClientIPResolver 设置 Params 快照使用的客户端 IP 解析器。
// WithClientIPResolver sets the client IP resolver for Params snapshots.
func WithClientIPResolver(resolver ClientIPResolver) ServerOption {
	return func(c *Config) {
		c.clientIPResolver = resolver
	}
}

// WithVFSPath 设置 ToStatic() 的默认安全静态文件根。
// WithVFSPath sets the default safe static root for ToStatic().
func WithVFSPath(root string) ServerOption {
	return func(c *Config) {
		c.vfsPath = root
	}
}

// WithReadTimeout 设置读取整个请求的最大时长。
// WithReadTimeout sets the maximum duration to read the whole request.
func WithReadTimeout(timeout time.Duration) ServerOption {
	return func(c *Config) {
		c.readTimeout = timeout
	}
}

// WithReadHeaderTimeout 设置读取请求头的最大时长。
// WithReadHeaderTimeout sets the maximum duration to read headers.
func WithReadHeaderTimeout(timeout time.Duration) ServerOption {
	return func(c *Config) {
		c.readHeaderTimeout = timeout
	}
}

// WithWriteTimeout 设置响应写入超时前的最大时长。
// WithWriteTimeout sets the maximum duration before response write timeout.
func WithWriteTimeout(timeout time.Duration) ServerOption {
	return func(c *Config) {
		c.writeTimeout = timeout
	}
}

// WithIdleTimeout 设置等待下一个请求的最大时长。
// WithIdleTimeout sets the maximum wait for the next request.
func WithIdleTimeout(timeout time.Duration) ServerOption {
	return func(c *Config) {
		c.idleTimeout = timeout
	}
}

// WithMaxHeaderBytes 设置请求头最大大小。
// WithMaxHeaderBytes sets the maximum request header size.
func WithMaxHeaderBytes(n int) ServerOption {
	return func(c *Config) {
		c.maxHeaderBytes = n
	}
}

// WithMaxBodyBytes 设置自动解码请求体的最大大小。
// WithMaxBodyBytes sets the maximum decoded request body size.
// 小于等于 0 时禁用请求体大小限制。
// values <= 0 disable the body size limit.
func WithMaxBodyBytes(n int64) ServerOption {
	return func(c *Config) {
		c.maxBodyBytes = n
	}
}

// WithTLSConfig 设置 TLS 服务方法使用的 TLS 配置。
// WithTLSConfig sets the TLS configuration for TLS serving methods.
func WithTLSConfig(tlsConfig *tls.Config) ServerOption {
	return func(c *Config) {
		c.tlsConfig = tlsConfig
	}
}

// WithBaseContext 设置已接受连接的 base context。
// WithBaseContext sets the base context for accepted connections.
func WithBaseContext(fn func(net.Listener) context.Context) ServerOption {
	return func(c *Config) {
		c.baseContext = fn
	}
}

// WithConnContext 为每个已接受连接设置 context。
// WithConnContext sets the context for each accepted connection.
func WithConnContext(fn func(context.Context, net.Conn) context.Context) ServerOption {
	return func(c *Config) {
		c.connContext = fn
	}
}

// WithErrorLog 设置底层 http.Server 使用的日志。
// WithErrorLog sets the logger used by the underlying http.Server.
func WithErrorLog(logger *log.Logger) ServerOption {
	return func(c *Config) {
		c.errorLog = logger
	}
}

func defaultClientIPResolver(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
