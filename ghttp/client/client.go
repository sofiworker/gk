package client

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	internalcodec "github.com/sofiworker/gk/ghttp/internal/codec"
)

// defaultResponseBodyLimit 是响应体读入内存的默认上限。取 32 MiB：足够容纳常规 API 的
// JSON/XML 响应，又能拦住"服务端被劫持或配置错误后返回巨型错误页"这一内存放大场景。
// 需要无上限时用 WithUnlimitedResponseBody 显式声明，或改用流式模式。
// defaultResponseBodyLimit caps an in-memory response body at 32 MiB: enough for
// ordinary JSON/XML API responses, while blocking the memory-amplification case where
// a compromised or misconfigured server returns a huge error page. Opt out explicitly
// with WithUnlimitedResponseBody, or switch to stream mode.
const defaultResponseBodyLimit int64 = 32 << 20

// Logger 是 client 的日志契约：一个由调用方注入的最小接口，方法名与 ghttp server 的
// Logger 保持一致，因此同一份实现（如 glog 适配器）可在两侧复用。
// Logger is the client's logging contract: a minimal interface injected by the caller
// whose method names match the ghttp server's Logger, so one implementation (a glog
// adapter, say) serves both sides.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Client 是 HTTP 客户端的配置载体：构造期经 Option 确定，运行期只读，因而并发安全。
// 它内部持有一个 *http.Client，但只在其上做编排（中间件、编解码、错误映射、重试），
// 不重新实现传输层。
//
// 一次请求由 Client.R() 派生出的 *Request 表达；Request 可变且【不】并发安全，
// 约定在单次请求内使用完即弃。
// Client carries the HTTP client's configuration: fixed at construction through
// Options and read-only afterwards, hence safe for concurrent use. It holds an
// *http.Client but merely orchestrates on top of it (middleware, codecs, error
// mapping, retries) and never reimplements the transport.
//
// A single request is expressed by a *Request derived from Client.R(). A Request is
// mutable and NOT safe for concurrent use; the convention is to use it within one
// request and discard it.
type Client struct {
	baseURL string

	// httpClient 是最终用于发送的实例；explicitClient 为真表示由 WithHTTPClient 提供，
	// 此时本包不再改写它的 Timeout/Jar/CheckRedirect。
	// httpClient is the instance actually used to send; explicitClient reports that it
	// came from WithHTTPClient, in which case this package stops rewriting its
	// Timeout/Jar/CheckRedirect.
	httpClient     *http.Client
	explicitClient bool

	// transportChanged 记录某个 Option 是否动过必须落到 http.Client 的配置。Clone 据此
	// 决定重建独立实例，而不是改动与源 Client 共享的那个对象（否则 Clone 一个变体就会
	// 静默改掉源 Client 的 Timeout/Jar/重定向策略）。
	// transportChanged records whether an Option touched configuration that must land
	// on the http.Client. Clone uses it to rebuild an independent instance instead of
	// mutating the object shared with the source Client (otherwise cloning a variant
	// would silently rewrite the source's Timeout/Jar/redirect policy).
	transportChanged bool

	// 传输层构建参数，仅在 explicitClient 为假时生效。
	// Transport-build parameters, effective only when explicitClient is false.
	transport       http.RoundTripper
	dialer          *net.Dialer
	dialContext     func(ctx context.Context, network, addr string) (net.Conn, error)
	tlsConfig       *tls.Config
	proxyURL        *url.URL
	proxyFromEnv    bool
	dialTimeout     time.Duration
	tlsTimeout      time.Duration
	headerTimeout   time.Duration
	idleConnTimeout time.Duration
	jar             http.CookieJar
	checkRedirect   func(req *http.Request, via []*http.Request) error

	// 请求默认值：在请求期与 Request 自身的设置合并，Request 优先。
	// Request defaults: merged with the Request's own settings at request time,
	// the Request winning.
	header    http.Header
	query     url.Values
	userAgent string
	authKey   string

	timeout           time.Duration
	responseBodyLimit int64
	acceptedStatus    func(status int) bool
	// acceptRedirects 由 WithDisableRedirects 打开：3xx 不再算错误，因为"禁止跟随重定向"
	// 的意图正是自己处理 3xx；否则 Once 禁用重定向就会得到一堆"302 是错误"的困惑。
	// acceptRedirects is turned on by WithDisableRedirects: 3xx stops being an error,
	// since the very intent of refusing to follow redirects is to handle 3xx manually;
	// otherwise disabling redirects would just yield confusing "302 is an error" results.
	acceptRedirects bool

	codecs map[string]Codec

	reqMiddleware  []RequestMiddleware
	respMiddleware []ResponseMiddleware
	beforeRequest  []BeforeRequestHook
	afterResponse  []AfterResponseHook
	successHooks   []SuccessHook
	errorHooks     []ErrorHook
	panicHooks     []PanicHook

	// handler / respHandler 是配置期折叠好的中间件链：请求期只做一次函数调用，
	// 不迭代、不组装、不分配。它们由 rebuildHandlers 维护，配置变更后必须重建。
	// handler/respHandler are the middleware chains folded at configuration time: a
	// request makes a single call, iterating/assembling/allocating nothing. They are
	// maintained by rebuildHandlers and must be rebuilt after a configuration change.
	handler     Handler
	respHandler ResponseHandler

	errorDecoder func(resp *Response, e *Error) error

	// retry 是 Client 级重试策略；请求级可经 SetRetry 覆盖。
	// retry is the client-level retry policy; a request can override it via SetRetry.
	retry RetryPolicy

	logger         Logger
	debug          bool
	debugBodyLimit int
	trace          bool
}

// Option 在构造期配置 Client。
// Option configures a Client at construction time.
type Option func(*Client)

// New 构造一个 Client。默认值：Transport 为 http.DefaultTransport 的克隆（不污染全局、
// 保留 ForceAttemptHTTP2）、只接受 2xx、响应体上限 32 MiB、内置 JSON/XML/Text/Form 编解码器。
//
// 注意默认【不】设置 http.Client.Timeout：它是覆盖建连到读 body 的整体墙钟上限，会悄悄
// 砍掉大响应。整请求超时请用 context，或显式 WithTimeout。
// New builds a Client. Defaults: a clone of http.DefaultTransport (never mutating the
// global, keeping ForceAttemptHTTP2), only 2xx accepted, a 32 MiB response-body limit,
// and built-in JSON/XML/Text/Form codecs.
//
// Note that http.Client.Timeout is NOT set by default: it is a whole-request wall-clock
// limit covering connect through body read, and would silently cut off large
// responses. Use a context for whole-request timeouts, or opt in via WithTimeout.
func New(opts ...Option) *Client {
	c := &Client{
		header:            http.Header{},
		query:             url.Values{},
		responseBodyLimit: defaultResponseBodyLimit,
		acceptedStatus:    isSuccessStatus,
		codecs:            defaultCodecs(),
		authKey:           "Authorization",
		debugBodyLimit:    1 << 10,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	c.finalize()
	c.rebuildHandlers()
	return c
}

// NewWithHTTPClient 用一个已存在的 *http.Client 构造 Client：完全接管传输、Cookie Jar、
// 重定向策略与 Timeout，本包的传输类选项（WithTransport/WithDialer/WithDialContext/
// WithTLSConfig/WithProxy/WithTimeout）在此时不生效。
// NewWithHTTPClient builds a Client from an existing *http.Client: it takes over the
// transport, cookie jar, redirect policy and timeout entirely, and this package's
// transport-level options (WithTransport/WithDialer/WithDialContext/WithTLSConfig/
// WithProxy/WithTimeout) have no effect.
func NewWithHTTPClient(hc *http.Client, opts ...Option) *Client {
	if hc == nil {
		return New(opts...)
	}
	all := append([]Option{WithHTTPClient(hc)}, opts...)
	return New(all...)
}

// Clone 派生一个配置变体：复制当前全部配置与中间件，并应用 opts。底层
// http.Client/Transport 被共享（连接池复用），因此两个 Client 共用一条连接池。
// Clone derives a configuration variant: it copies the current configuration and
// middleware, then applies opts. The underlying http.Client/Transport are shared (the
// connection pool is reused), so both Clients draw from one pool.
func (c *Client) Clone(opts ...Option) *Client {
	nc := *c
	nc.header = cloneHeader(c.header)
	nc.query = cloneValues(c.query)
	nc.codecs = make(map[string]Codec, len(c.codecs))
	for k, v := range c.codecs {
		nc.codecs[k] = v
	}
	nc.reqMiddleware = append([]RequestMiddleware(nil), c.reqMiddleware...)
	nc.respMiddleware = append([]ResponseMiddleware(nil), c.respMiddleware...)
	nc.beforeRequest = append([]BeforeRequestHook(nil), c.beforeRequest...)
	nc.afterResponse = append([]AfterResponseHook(nil), c.afterResponse...)
	nc.successHooks = append([]SuccessHook(nil), c.successHooks...)
	nc.errorHooks = append([]ErrorHook(nil), c.errorHooks...)
	nc.panicHooks = append([]PanicHook(nil), c.panicHooks...)
	for _, opt := range opts {
		if opt != nil {
			opt(&nc)
		}
	}
	// opts 若动过必须落到 http.Client 的配置，就重建独立实例，绝不改动与源 Client
	// 共享的那个对象；否则沿用共享实例，两个 Client 共用一条连接池。
	// If opts touched configuration that must land on the http.Client, rebuild an
	// independent instance rather than mutating the object shared with the source
	// Client; otherwise share the instance and its connection pool.
	if !nc.explicitClient && nc.transportChanged {
		nc.httpClient = nil
		nc.finalize()
	}
	// 中间件链必须重建：链上的闭包捕获的是方法值，复制过来的链仍指向源 Client。
	// The middleware chains must be rebuilt: their closures capture method values and
	// still point at the source Client.
	nc.rebuildHandlers()
	return &nc
}

// R 派生一个新请求。每次调用返回独立对象，可安全地在不同 goroutine 各自构建。
// R derives a new request. Each call returns an independent object, so separate
// goroutines can build their own safely.
func (c *Client) R() *Request {
	return &Request{
		client: c,
		header: http.Header{},
		query:  url.Values{},
		path:   map[string]string{},
		result: nil,
		errTgt: nil,
	}
}

// NewRequest 是 R 的别名。
// NewRequest is an alias for R.
func (c *Client) NewRequest() *Request { return c.R() }

// BaseURL 返回构造期设置的 base URL。
// BaseURL returns the base URL set at construction.
func (c *Client) BaseURL() string { return c.baseURL }

// HTTPClient 返回内部使用的 *http.Client。注意 Client.Timeout 与 CheckRedirect 由其持有，
// 运行期修改它会影响本 Client 的所有后续请求。
// HTTPClient returns the *http.Client in use. Note that it owns Timeout and
// CheckRedirect, so mutating it at runtime affects every later request of this Client.
func (c *Client) HTTPClient() *http.Client { return c.httpClient }

// Transport 返回内部使用的 http.RoundTripper。
// Transport returns the http.RoundTripper in use.
func (c *Client) Transport() http.RoundTripper { return c.httpClient.Transport }

// CloseIdleConnections 关闭底层连接池中的空闲连接；底层 Transport 不支持时是空操作。
// CloseIdleConnections closes idle connections in the underlying pool; a no-op when
// the transport does not support it.
func (c *Client) CloseIdleConnections() {
	type closer interface{ CloseIdleConnections() }
	if cl, ok := c.httpClient.Transport.(closer); ok {
		cl.CloseIdleConnections()
	}
}

// ——— 中间件与 Hook ——— //

// Use 追加请求中间件（洋葱序：先追加的最外层、最先执行），与 ghttp.Server.Use 语义一致。
// Use appends request middleware (onion order: the first appended is outermost and runs
// first), matching ghttp.Server.Use.
func (c *Client) Use(mw ...RequestMiddleware) *Client {
	c.reqMiddleware = append(c.reqMiddleware, mw...)
	c.rebuildHandlers()
	return c
}

// UseResponse 追加响应中间件。
// UseResponse appends response middleware.
func (c *Client) UseResponse(mw ...ResponseMiddleware) *Client {
	c.respMiddleware = append(c.respMiddleware, mw...)
	c.rebuildHandlers()
	return c
}

// OnBeforeRequest 注册请求发送前的观察钩子。
// OnBeforeRequest registers an observation hook that runs before sending.
func (c *Client) OnBeforeRequest(h BeforeRequestHook) *Client {
	c.beforeRequest = append(c.beforeRequest, h)
	return c
}

// OnAfterResponse 注册响应到达后的观察钩子。
// OnAfterResponse registers an observation hook that runs after a response arrives.
func (c *Client) OnAfterResponse(h AfterResponseHook) *Client {
	c.afterResponse = append(c.afterResponse, h)
	return c
}

// OnSuccess 注册成功钩子。
// OnSuccess registers a success hook.
func (c *Client) OnSuccess(h SuccessHook) *Client {
	c.successHooks = append(c.successHooks, h)
	return c
}

// OnError 注册失败钩子。
// OnError registers a failure hook.
func (c *Client) OnError(h ErrorHook) *Client {
	c.errorHooks = append(c.errorHooks, h)
	return c
}

// OnPanic 注册 panic 观察钩子。它只观察，不吞掉 panic。
// OnPanic registers a panic observer. It only observes; it never swallows the panic.
func (c *Client) OnPanic(h PanicHook) *Client {
	c.panicHooks = append(c.panicHooks, h)
	return c
}

// ——— 构造选项 ——— //

// WithBaseURL 设置所有相对路径请求的基准地址。
// WithBaseURL sets the base address for every relative request path.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient 用一个既有 *http.Client 完全接管传输层（见 NewWithHTTPClient 说明）。
// WithHTTPClient hands the transport layer over to an existing *http.Client (see
// NewWithHTTPClient).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
			c.explicitClient = true
		}
	}
}

// WithTransport 设置 http.RoundTripper。若同时给出 WithDialer/WithDialContext/
// WithTLSConfig/WithProxy，它们将被忽略（无法安全改写一个未知实现）。
// WithTransport sets the http.RoundTripper. When combined with
// WithDialer/WithDialContext/WithTLSConfig/WithProxy, those are ignored, since an
// unknown implementation cannot be safely rewritten.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) { c.transport = rt; c.transportChanged = true }
}

// WithDialer 设置拨号器。内部会复制该 *net.Dialer 再改，绝不修改调用方传入的对象。
// WithDialer sets the dialer. The *net.Dialer is copied before any change; the
// caller's object is never mutated.
func WithDialer(d *net.Dialer) Option {
	return func(c *Client) { c.dialer = d; c.transportChanged = true }
}

// WithDialContext 设置自定义拨号函数（优先级高于 WithDialer），用于绑定本地地址、
// 走自研网络栈等场景。
// WithDialContext sets a custom dial function (taking precedence over WithDialer) for
// binding a local address, using a bespoke network stack, and the like.
func WithDialContext(fn func(ctx context.Context, network, addr string) (net.Conn, error)) Option {
	return func(c *Client) {
		c.dialContext = fn
		c.transportChanged = true
	}
}

// WithDialTimeout 设置 TCP 建连超时。
// WithDialTimeout sets the TCP connect timeout.
func WithDialTimeout(d time.Duration) Option {
	return func(c *Client) { c.dialTimeout = d; c.transportChanged = true }
}

// WithTLSHandshakeTimeout 设置 TLS 握手超时。
// WithTLSHandshakeTimeout sets the TLS handshake timeout.
func WithTLSHandshakeTimeout(d time.Duration) Option {
	return func(c *Client) { c.tlsTimeout = d; c.transportChanged = true }
}

// WithResponseHeaderTimeout 设置"请求写出到收到响应头"的超时。它保护的是服务端迟迟不返回
// 响应头的情形，与读 body 无关。
// WithResponseHeaderTimeout sets the timeout from finishing the request write to
// receiving the response header. It guards against a server that never sends headers,
// and does not cover body reads.
func WithResponseHeaderTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.headerTimeout = d
		c.transportChanged = true
	}
}

// WithIdleConnTimeout 设置空闲连接在池中的最长存活时间。
// WithIdleConnTimeout sets how long an idle connection may stay in the pool.
func WithIdleConnTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.idleConnTimeout = d
		c.transportChanged = true
	}
}

// WithProxy 设置显式代理 URL。
// WithProxy sets an explicit proxy URL.
func WithProxy(proxyURL string) Option {
	return func(c *Client) {
		if u, err := url.Parse(proxyURL); err == nil {
			c.proxyURL = u
			c.transportChanged = true
		}
	}
}

// WithProxyFromEnvironment 启用环境变量代理（HTTP_PROXY/HTTPS_PROXY/NO_PROXY）。
//
// 注意标准库的 http.ProxyFromEnvironment 对每个进程只读一次环境变量（包级缓存），
// 之后修改环境变量不会生效；需要按请求可变的代理时必须用 WithProxy。
// WithProxyFromEnvironment enables proxy lookup from the environment
// (HTTP_PROXY/HTTPS_PROXY/NO_PROXY).
//
// Note that the standard library's http.ProxyFromEnvironment reads the environment
// once per process (a package-level cache); later changes have no effect. Use
// WithProxy when the proxy must vary at runtime.
func WithProxyFromEnvironment() Option {
	return func(c *Client) { c.proxyFromEnv = true; c.transportChanged = true }
}

// WithCookieJar 设置 Cookie Jar。
// WithCookieJar sets the cookie jar.
func WithCookieJar(jar http.CookieJar) Option {
	return func(c *Client) { c.jar = jar; c.transportChanged = true }
}

// WithCheckRedirect 设置标准库的重定向判定函数，语义与 http.Client.CheckRedirect 一致。
// WithCheckRedirect sets the standard library's redirect predicate with identical
// semantics to http.Client.CheckRedirect.
func WithCheckRedirect(fn func(req *http.Request, via []*http.Request) error) Option {
	return func(c *Client) { c.checkRedirect = fn; c.transportChanged = true }
}

// WithDisableRedirects 禁止跟随重定向：收到 3xx 时把响应原样返回，而不是报错。
// WithDisableRedirects refuses to follow redirects: a 3xx response is returned as-is
// rather than treated as an error.
func WithDisableRedirects() Option {
	return func(c *Client) {
		c.checkRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		c.acceptRedirects = true
		c.transportChanged = true
	}
}

// WithAcceptRedirects 把 3xx 视为可接受状态，不再产生 *Error，供"重定向自己处理"的场景。
// WithDisableRedirects 已隐含开启它；当 CheckRedirect 是通过 WithHTTPClient 自带的时候，
// 需要显式加上本选项。
// WithAcceptRedirects treats 3xx as acceptable and no longer yields an *Error, for
// cases that handle redirects themselves. WithDisableRedirects implies it; when the
// CheckRedirect comes in via WithHTTPClient, add this option explicitly.
func WithAcceptRedirects() Option {
	return func(c *Client) { c.acceptRedirects = true }
}

// WithTimeout 设置 http.Client.Timeout。它是覆盖建连、重定向与【读 body】的整体墙钟上限，
// 大响应或流式下载会被它中途砍断；这类场景请改用 context 或 WithResponseHeaderTimeout。
// WithTimeout sets http.Client.Timeout: a whole-request wall-clock limit covering
// connect, redirects and BODY READS. Large or streamed responses get cut off mid-way;
// prefer a context or WithResponseHeaderTimeout for those.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d; c.transportChanged = true }
}

// WithHeader 追加一个对所有请求生效的默认头。
// WithHeader adds a default header applied to every request.
func WithHeader(key, value string) Option {
	return func(c *Client) { c.header.Set(key, value) }
}

// WithHeaderAuthorizationKey 设置承载认证 token 的头名（默认 "Authorization"）。
// WithHeaderAuthorizationKey sets the header carrying the auth token (default
// "Authorization").
func WithHeaderAuthorizationKey(key string) Option {
	return func(c *Client) {
		if key != "" {
			c.authKey = key
		}
	}
}

// WithQueryParam 追加一个对所有请求生效的默认查询参数。
// WithQueryParam adds a default query parameter applied to every request.
func WithQueryParam(key, value string) Option {
	return func(c *Client) { c.query.Set(key, value) }
}

// WithUserAgent 设置默认 User-Agent。
// WithUserAgent sets the default User-Agent.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// WithCodec 为某个 Content-Type 注册编解码器（覆盖同类型的内置实现）。
// WithCodec registers a codec for a Content-Type, overriding any built-in of the same
// type.
func WithCodec(contentType string, codec Codec) Option {
	return func(c *Client) {
		if key := registryKey(contentType); key != "" && codec != nil {
			if c.codecs == nil {
				c.codecs = defaultCodecs()
			}
			c.codecs[key] = codec
		}
	}
}

// WithResponseBodyLimit 设置响应体读入内存的上限（字节）。非正值含义由
// WithUnlimitedResponseBody 表达。
// WithResponseBodyLimit caps an in-memory response body in bytes. A non-positive
// value is expressed by WithUnlimitedResponseBody instead.
func WithResponseBodyLimit(n int64) Option {
	return func(c *Client) {
		if n > 0 {
			c.responseBodyLimit = n
		}
	}
}

// WithUnlimitedResponseBody 解除响应体上限。仅在你完全信任对端规模时使用：上限存在的
// 意义是拦住巨型响应导致的内存放大。
// WithUnlimitedResponseBody lifts the response-body cap. Use it only when you fully
// trust the peer's size: the cap exists to stop memory amplification from huge
// responses.
func WithUnlimitedResponseBody() Option {
	return func(c *Client) { c.responseBodyLimit = 0 }
}

// WithAcceptedStatus 设置可接受的状态码集合：不在集合内的响应被视为错误（返回 *Error）。
// 未调用时默认只接受 2xx。
// WithAcceptedStatus sets the accepted status codes: a response outside the set is an
// error (*Error). When not called, only 2xx is accepted.
func WithAcceptedStatus(codes ...int) Option {
	return func(c *Client) {
		set := make(map[int]struct{}, len(codes))
		for _, code := range codes {
			set[code] = struct{}{}
		}
		c.acceptedStatus = func(status int) bool {
			_, ok := set[status]
			return ok
		}
	}
}

// WithAllowAllStatus 接受所有状态码：非 2xx 不再产生 error，由调用方自行判断。
// 这是 resty/req/sling 等库的惯例，显式选择即可退回。
// WithAllowAllStatus accepts every status: non-2xx no longer yields an error, leaving
// the decision to the caller. This is the resty/req/sling convention, opted into
// explicitly.
func WithAllowAllStatus() Option {
	return func(c *Client) { c.acceptedStatus = func(int) bool { return true } }
}

// WithErrorDecoder 接入服务端错误体的结构化解析：非 2xx 时先调用 fn，把错误体解进调用方
// 自己的结构或改写 *Error。本包对服务端错误格式【零假设】，这是唯一的接入点。
// WithErrorDecoder wires in structured parsing of the server's error body: on a
// non-2xx response fn runs first, decoding the body into the caller's own struct or
// amending *Error. This package makes ZERO assumptions about the error shape, and
// this is the only hook.
func WithErrorDecoder(fn func(resp *Response, e *Error) error) Option {
	return func(c *Client) { c.errorDecoder = fn }
}

// WithLogger 注入日志实现。
// WithLogger injects a logger implementation.
func WithLogger(l Logger) Option { return func(c *Client) { c.logger = l } }

// WithDebug 打开调试日志（请求行与响应状态；请求/响应体 dump 受 WithDebugBodyLimit 限制）。
// WithDebug turns on debug logging (request line and response status; body dumps are
// bounded by WithDebugBodyLimit).
func WithDebug(enabled bool) Option { return func(c *Client) { c.debug = enabled } }

// WithDebugBodyLimit 设置 debug dump 中单个体最多打印的字节数。
// WithDebugBodyLimit caps how many bytes of a single body a debug dump prints.
func WithDebugBodyLimit(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.debugBodyLimit = n
		}
	}
}

// WithTrace 打开 httptrace 采集（DNS/建连/TLS/首字节等时序），结果经 Response.Traces 读取。
// WithTrace enables httptrace collection (DNS/connect/TLS/first byte), readable via
// Response.Traces.
func WithTrace(enabled bool) Option { return func(c *Client) { c.trace = enabled } }

// WithRequestMiddleware 追加请求中间件。
// WithRequestMiddleware appends request middleware.
func WithRequestMiddleware(mw ...RequestMiddleware) Option {
	return func(c *Client) { c.reqMiddleware = append(c.reqMiddleware, mw...) }
}

// WithResponseMiddleware 追加响应中间件。
// WithResponseMiddleware appends response middleware.
func WithResponseMiddleware(mw ...ResponseMiddleware) Option {
	return func(c *Client) { c.respMiddleware = append(c.respMiddleware, mw...) }
}

// ——— 内部工具 ——— //

func isSuccessStatus(status int) bool { return status >= 200 && status <= 299 }

func cloneHeader(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	for k, vs := range src {
		dst[k] = append([]string(nil), vs...)
	}
	return dst
}

func cloneValues(src url.Values) url.Values {
	dst := make(url.Values, len(src))
	for k, vs := range src {
		dst[k] = append([]string(nil), vs...)
	}
	return dst
}

// mediaTypeOf 是 internal/codec.MediaType 的包内简写，供本包多处归一化使用。
// mediaTypeOf is the in-package shorthand for internal/codec.MediaType.
func mediaTypeOf(contentType string) string { return internalcodec.MediaType(contentType) }

// readAllLimited 读取 r 至多 limit 字节；limit <= 0 表示不限。
// 超出时返回 ErrBodyTooLarge，并且【只】读到 limit 就不再继续，避免把超限部分也读进内存。
// readAllLimited reads at most limit bytes from r; limit <= 0 means unlimited. On
// overrun it returns ErrBodyTooLarge and stops at limit, never pulling the overflow
// into memory.
func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrBodyTooLarge
	}
	return data, nil
}
