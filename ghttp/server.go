package ghttp

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server 是本包唯一的顶层类型:它【就是】一个 HTTP server——内部组合路由核心(mux)与
// 监听生命周期(*http.Server)。用户在它上面挂中间件、注册路由,然后启动与优雅关闭,
// 无需再认识第二个类型。
//
// mux 以【首个嵌入字段】的形式并入,因此 ServeHTTP / RawHandle / Group / register 等
// 由 Go 字段提升直接成为 Server 的方法,不产生手写委托层;且 mux 位于零偏移处,底层
// http.Server 的 Handler 直接指向它,真实请求路径无任何额外间接。
//
// 零值不可用,必须经 New 构造。
//
// Server is this package's only top-level type: it IS an HTTP server — internally
// composing the routing core (mux) and the listen lifecycle (*http.Server). Users
// attach middleware and register routes on it, then start and gracefully stop it,
// without meeting a second type.
//
// mux is merged in as the FIRST embedded field, so ServeHTTP / RawHandle / Group /
// register become Server's methods through Go's field promotion with no hand-written
// delegation; and since mux sits at offset zero, the underlying http.Server's
// Handler points straight at it, leaving the real request path free of any extra
// indirection.
//
// The zero value is unusable; construct it via New.
type Server struct {
	mux

	httpSrv *http.Server

	// openAPI 非 nil 表示已开启 OpenAPI 生成(经 WithOpenAPI);它只在注册期与首次
	// spec 构建时被读取。
	// A non-nil openAPI means OpenAPI generation is enabled (via WithOpenAPI); it
	// is read only at registration and on the first spec build.
	openAPI *openAPIConfig
	// pendingOpenAPIRoute 是待注册的 spec 暴露路径。New 在应用完所有 Option 后注册它,
	// 从而使 spec 路由本身不被登记进 spec。
	// pendingOpenAPIRoute is the spec route awaiting registration. New registers it
	// after all Options are applied, keeping the spec route out of the spec itself.
	pendingOpenAPIRoute string
	specOnceHolder

	// mu 守护 state。它守护的是【复合的判定—落笔】:markStarted 与 endRun 都必须在同一
	// 临界区内先判定状态再改写,因此单纯把 state 换成原子变量并不够——那会留下"读到 idle
	// → 期间被关闭 → 仍改写为 running"的竞态,即启动一个已关闭的 Server。
	//
	// 临界区内只有整数比较与赋值:不调用其它函数、不取第二把锁、不做 I/O,Shutdown/Close
	// 的阻塞排空一律在解锁【之后】执行。因此既无循环等待也无重入,不存在死锁路径。生命周期
	// 方法每进程仅调用一两次且不在请求路径上,故用最简单的 Mutex,不引入 RWMutex 或原子 CAS。
	// mu guards state. What it guards is a COMPOUND check-then-write: both markStarted
	// and endRun must inspect the state and rewrite it inside the same critical
	// section, so merely making state atomic is not enough — that would leave a "read
	// idle → closed meanwhile → still rewritten to running" race, i.e. starting an
	// already-closed Server.
	//
	// A critical section holds only integer compares and assignments: no calls, no
	// second lock, no I/O, and Shutdown/Close run their blocking drain AFTER
	// unlocking. So there is neither a wait cycle nor reentrancy, and no deadlock is
	// reachable. Lifecycle methods run once or twice per process and never on the
	// request path, so a plain Mutex suffices; no RWMutex or atomic CAS.
	mu    sync.Mutex
	state serverState
}

// serverState 是 Server 的三态生命周期。用单字段而非多个 bool,使"已启动"与"已关闭"
// 这组有序且互斥的状态不可能出现矛盾组合。
// serverState is Server's three-state lifecycle. A single field rather than several
// bools makes the ordered, mutually exclusive "started"/"closed" states unable to
// form a contradictory combination.
type serverState uint8

const (
	stateIdle    serverState = iota // 已构造未启动 / constructed, not started
	stateRunning                    // 正在服务 / serving
	stateClosed                     // 已关闭,不可再启动 / closed, not startable
)

// Option 以函数式选项配置 Server(对齐仓库 WithXxx 约定),涵盖路由行为与监听参数。
// Option configures a Server via functional options (matching the repo's WithXxx
// convention), covering both routing behavior and listener parameters.
type Option func(*Server)

// New 构造一个 HTTP Server:空路由 + 默认监听配置,opts 按顺序应用。默认无读写超时,
// IdleTimeout=60s(长连接闲置回收),MaxHeaderBytes 用标准库默认。
// New constructs an HTTP Server: empty routes plus default listener configuration,
// applying opts in order. Defaults: no read/write timeout, IdleTimeout=60s (idle
// keep-alive reaping), standard-library default MaxHeaderBytes.
func New(opts ...Option) *Server {
	s := &Server{}
	// 池化 Request 一次性绑定 owner=&s.mux(reset 不清空),使 ClientIP() 等访问器
	// 无需每请求写入即可读取 server 级配置。
	// Pooled Requests bind owner=&s.mux once (never cleared on reset), so
	// accessors like ClientIP() read server-level config with no per-request write.
	s.pool.New = func() any { return &Request{owner: &s.mux} }
	// 默认严格 Content-Type 校验(body 入口不符声明的 CT 即 415);经
	// WithStrictContentType(false) 关闭。零值 false 不是期望默认,故在此显式置真。
	// Default strict Content-Type checking (body entries yield 415 on CT
	// mismatch); disable via WithStrictContentType(false). The zero value false
	// is not the desired default, so set it true here explicitly.
	s.strictContentType = true
	s.httpSrv = &http.Server{
		// 直接指向内部 mux(零偏移嵌入),使请求路径不经 Server 的提升包装。
		// Point straight at the inner mux (embedded at offset zero) so the request
		// path skips Server's promotion wrapper.
		Handler:     &s.mux,
		IdleTimeout: 60 * time.Second,
	}
	for _, opt := range opts {
		opt(s)
	}
	// spec 暴露路由在全部 Option 之后注册,使它自身不出现在 spec 里。注册失败只可能是
	// 用户给了非法路径,此时静默跳过暴露(spec 仍可经 SpecJSON 取得),不破坏 New 的
	// 无错签名。
	// The spec route is registered after all Options so it stays out of the spec.
	// Registration can only fail on a user-supplied illegal path; exposure is then
	// skipped silently (the spec remains available via SpecJSON) rather than
	// breaking New's error-free signature.
	_ = s.registerOpenAPIRoute()
	return s
}

// Use 向全局中间件栈追加中间件,对【所有】请求生效(含未命中路由与预检)。须在注册路由
// 与开始服务前调用(gin 同款约束)。返回自身以便链式调用。
// Use appends middleware to the global stack, applying to ALL requests (including
// route misses and preflight). Call before registering routes and before serving
// (gin's constraint). Returns itself for chaining.
func (s *Server) Use(mws ...Middleware) *Server {
	s.mux.use(mws...)
	return s
}

// ---------------------------------------------------------------------------
// 路由行为选项 / Routing behavior options
// ---------------------------------------------------------------------------

// WithStrictPath 开启严格路径校验:dot 段与空段均返回 400。默认关闭(快速模式,
// 仅拦 dot 段以防路径遍历,空段交给匹配,与 gin 一致的高性能取舍)。
// WithStrictPath enables strict path validation: both dot and empty segments
// yield 400. Off by default (fast mode: only dot segments are rejected as a
// path-traversal guard, empty segments pass to matching — the gin-aligned
// high-performance tradeoff).
func WithStrictPath(strict bool) Option {
	return func(s *Server) { s.strictPath = strict }
}

// WithAutoHEAD makes registered GET routes also accept HEAD requests. The
// default is false to preserve ghttp's Gin-style routing semantics.
func WithAutoHEAD(enabled bool) Option {
	return func(s *Server) { s.autoHEAD = enabled }
}

// WithStrictContentType 控制 body 入口是否在解码前校验请求 Content-Type 属于端点声明的
// 可接受集合。默认 true(不符即 415);置 false 时跳过校验,直接把请求体交给解码器
// (旧宽松行为)。可接受集合优先取解码器实现的 MultiContentTypeDecoder.ContentTypes()
// (表单据此声明 urlencoded 与 multipart 两种),否则回退到单值 RequestDecoder.ContentType()。
// WithStrictContentType controls whether body entries verify that the request
// Content-Type belongs to the endpoint's declared accepted set before decoding.
// Default true (415 on mismatch); false skips the check and hands the body straight
// to the decoder (the older lenient behavior). The accepted set prefers the decoder's
// MultiContentTypeDecoder.ContentTypes() (which is how a form declares both
// urlencoded and multipart), falling back to the single-valued
// RequestDecoder.ContentType().
func WithStrictContentType(strict bool) Option {
	return func(s *Server) { s.strictContentType = strict }
}

// WithNotFoundHandler 注册自定义 404 处理器,替代默认 JSON 错误体。处理器在全局中间件
// 链内运行,可读取请求并完全接管响应。nil 时回退默认错误体。
// WithNotFoundHandler registers a custom 404 handler replacing the default JSON
// error body. It runs inside the global middleware chain, can read the request,
// and fully owns the response. A nil handler falls back to the default body.
func WithNotFoundHandler(h RawHandlerFunc) Option {
	return func(s *Server) { s.notFoundHandler = h }
}

// WithMethodNotAllowedHandler 注册自定义 405 处理器。框架仍会先设置 Allow 头,再调用
// 该处理器。nil 时回退默认错误体。
// WithMethodNotAllowedHandler registers a custom 405 handler. The framework still
// sets the Allow header first, then invokes the handler. Nil falls back to the
// default body.
func WithMethodNotAllowedHandler(h RawHandlerFunc) Option {
	return func(s *Server) { s.methodNotAllowedHandler = h }
}

// ---------------------------------------------------------------------------
// 监听与超时选项 / Listener and timeout options
// ---------------------------------------------------------------------------

// WithAddr 设置监听地址（形如 ":8080" 或 "127.0.0.1:8080"）。
// WithAddr sets the listen address (e.g. ":8080" or "127.0.0.1:8080").
func WithAddr(addr string) Option {
	return func(s *Server) { s.httpSrv.Addr = addr }
}

// WithReadTimeout 设置读取整个请求（含 body）的最大时长；<=0 表示无超时。
// WithReadTimeout caps reading the entire request (including body); <=0 disables.
func WithReadTimeout(d time.Duration) Option {
	return func(s *Server) { s.httpSrv.ReadTimeout = d }
}

// WithReadHeaderTimeout 设置读取请求头的最大时长；<=0 表示回退到 ReadTimeout。
// WithReadHeaderTimeout caps reading request headers; <=0 falls back to ReadTimeout.
func WithReadHeaderTimeout(d time.Duration) Option {
	return func(s *Server) { s.httpSrv.ReadHeaderTimeout = d }
}

// WithWriteTimeout 设置写响应的最大时长；<=0 表示无超时。
// WithWriteTimeout caps writing the response; <=0 disables.
func WithWriteTimeout(d time.Duration) Option {
	return func(s *Server) { s.httpSrv.WriteTimeout = d }
}

// WithIdleTimeout 设置 keep-alive 连接的空闲最大时长；<=0 表示无超时。
// WithIdleTimeout caps idle time for keep-alive connections; <=0 disables.
func WithIdleTimeout(d time.Duration) Option {
	return func(s *Server) { s.httpSrv.IdleTimeout = d }
}

// WithMaxHeaderBytes 设置请求头（含请求行）解析的最大字节数；<=0 用标准库默认。
// WithMaxHeaderBytes caps bytes parsed for request headers (incl. request line);
// <=0 uses the standard-library default.
func WithMaxHeaderBytes(n int) Option {
	return func(s *Server) {
		if n > 0 {
			s.httpSrv.MaxHeaderBytes = n
		}
	}
}

// WithTLSConfig 直接注入 *tls.Config（用于 mTLS、自定义 CA、ALPN 等高级场景）。
// WithTLSConfig injects a *tls.Config directly (for mTLS, custom CA, ALPN, etc.).
func WithTLSConfig(cfg *tls.Config) Option {
	return func(s *Server) { s.httpSrv.TLSConfig = cfg }
}

// WithBaseContext 设置所有入站请求的根 context 构造器，便于注入全局取消或值。
// WithBaseContext sets the base-context constructor for all inbound requests,
// useful for injecting global cancellation or values.
func WithBaseContext(fn func(net.Listener) context.Context) Option {
	return func(s *Server) { s.httpSrv.BaseContext = fn }
}

// ---------------------------------------------------------------------------
// 启动与关闭 / Startup and shutdown
// ---------------------------------------------------------------------------

// Run 在 addr 上阻塞式启动明文 HTTP 服务。addr 非空时覆盖 WithAddr 的配置;二者皆空
// 时默认 ":8080"。正常关闭返回 ErrServerClosed(可 errors.Is 判定)。启动后可从另一
// goroutine 调 Shutdown 优雅停止。监听失败(如端口被占用)时返回该错误,且 Server 退回未
// 启动状态,可换 addr 重试。
// Run starts a plaintext HTTP server on addr and blocks. A non-empty addr overrides
// WithAddr; if both are empty it defaults to ":8080". A clean shutdown returns
// ErrServerClosed (test via errors.Is). After starting, call Shutdown from another
// goroutine to stop gracefully. If listening fails (e.g. the port is already in use)
// that error is returned and the Server falls back to not-started, so it can be
// retried on another addr.
func (s *Server) Run(addr string) error {
	// 先过启动守卫再写 Addr:守卫是互斥的,只有胜出的那次调用会改配置,从而避免并发 Run
	// 同时写 httpSrv.Addr 造成数据竞争。
	// Take the start guard before writing Addr: the guard is mutually exclusive, so
	// only the winning call mutates configuration, avoiding a data race on
	// httpSrv.Addr between concurrent Run calls.
	if err := s.markStarted(); err != nil {
		return err
	}
	if addr != "" {
		s.httpSrv.Addr = addr
	}
	if s.httpSrv.Addr == "" {
		s.httpSrv.Addr = ":8080"
	}
	return s.endRun(s.httpSrv.ListenAndServe())
}

// RunTLS 在 addr 上阻塞式启动 HTTPS 服务。addr 非空时覆盖 WithAddr 的配置;二者皆空
// 时默认 ":8443"。若已注入 TLSConfig,certFile/keyFile 可为空;否则二者必填。
// RunTLS starts an HTTPS server on addr and blocks. A non-empty addr overrides
// WithAddr; if both are empty it defaults to ":8443". certFile/keyFile may be empty
// if a TLSConfig was injected; otherwise both are required.
func (s *Server) RunTLS(addr, certFile, keyFile string) error {
	if s.httpSrv.TLSConfig == nil && (certFile == "" || keyFile == "") {
		return ErrTLSConfig
	}
	// 同 Run:守卫先行,确保只有一次调用写 Addr。
	// As in Run: guard first so only one call writes Addr.
	if err := s.markStarted(); err != nil {
		return err
	}
	if addr != "" {
		s.httpSrv.Addr = addr
	}
	if s.httpSrv.Addr == "" {
		s.httpSrv.Addr = ":8443"
	}
	return s.endRun(s.httpSrv.ListenAndServeTLS(certFile, keyFile))
}

// Serve 在已有 net.Listener 上启动服务，供自定义监听器（Unix socket、限流监听等）
// 或测试注入使用。
// Serve runs the server on an existing net.Listener, for custom listeners (Unix
// socket, throttled listener, etc.) or test injection.
func (s *Server) Serve(l net.Listener) error {
	if err := s.markStarted(); err != nil {
		return err
	}
	return s.endRun(s.httpSrv.Serve(l))
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
	return s.endRun(s.httpSrv.ServeTLS(l, certFile, keyFile))
}

// Shutdown 优雅关闭：先关监听器拒绝新连接，再等待进行中请求完成，直到 ctx 到期。
// 幂等，且【每个】调用者(含并发调用)都等到真正排空完成才返回；ctx 到期时返回其错误。
//
// 每次调用都下沉到标准库,而不对重复调用提前 return nil:http.Server.Shutdown 本身可
// 重复调用,并让每个调用者都等到排空完成。若在此短路,第二个调用者会在实际尚未排空时
// 拿到 nil,误判为已完成。
//
// Shutdown gracefully stops the server: it closes listeners to reject new
// connections, then waits for in-flight requests until ctx expires. Idempotent, and
// EVERY caller (including concurrent ones) waits for the drain to actually finish;
// it returns ctx's error if ctx expires first.
//
// Each call delegates to the standard library rather than short-circuiting a repeat
// call with nil: http.Server.Shutdown is itself safe to call repeatedly and makes
// every caller wait for completion. Short-circuiting here would hand a second
// caller a nil before the drain finished, misreporting completion.
func (s *Server) Shutdown(ctx context.Context) error {
	s.markClosed()
	return s.httpSrv.Shutdown(ctx)
}

// Close 立即关闭：强制中断所有活动连接，不等待进行中请求。仅用于紧急停止。
// 与 Shutdown 同理,每次调用都下沉到标准库以传递真实的关闭结果(标准库在监听器已摘除后
// 重复调用返回 nil,故仍是幂等的)。
// Close stops immediately, forcibly interrupting all active connections without
// waiting for in-flight requests. Use only for emergency stops. As with Shutdown,
// each call delegates to the standard library so the real close result propagates
// (the stdlib returns nil once listeners are already detached, so it stays
// idempotent).
func (s *Server) Close() error {
	s.markClosed()
	return s.httpSrv.Close()
}

// markStarted 原子地把 Server 标记为已启动，重复启动或关闭后启动均报错。
// markStarted atomically flags the server started, rejecting a double start or a
// start after close.
func (s *Server) markStarted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state {
	case stateIdle:
		s.state = stateRunning
		s.mux.serving.Store(true)
		return nil
	case stateRunning:
		return ErrServerStarted
	}
	// 仅剩 stateClosed:已终结的 Server 不可复用。
	// Only stateClosed remains: a terminated Server cannot be reused.
	return ErrServerNotStartable
}

// endRun 收敛一次启动调用的结局。监听失败(端口被占用、权限不足、地址非法)意味着 Server
// 从未真正服务过,必须退回 stateIdle;否则它会永久停在 stateRunning——IsStarted 谎报正在
// 服务,换端口重试又被 ErrServerStarted 挡回,Server 成为不可用的僵尸对象。
//
// 回退本身也是一次复合判定:仅当自己仍是 stateRunning 时才退回。若期间已有 Shutdown/Close
// 把状态推进到 stateClosed(正常关闭正是这条路径,底层返回 ErrServerClosed),必须保留终态;
// 无条件回退才会造成真正的状态污染——把一个已关闭的 Server 改回可启动。
//
// endRun settles the outcome of one start call. A listen failure (port already in
// use, insufficient privileges, malformed address) means the server never served at
// all, so it must fall back to stateIdle; otherwise it would sit in stateRunning
// forever — IsStarted lying that it serves and a retry on another port bouncing off
// ErrServerStarted, leaving an unusable zombie.
//
// The rollback is itself a compound decision: fall back only while still
// stateRunning. If Shutdown/Close has meanwhile advanced the state to stateClosed
// (the normal-shutdown path, where the stdlib returns ErrServerClosed), that terminal
// state must survive; an unconditional rollback is what would truly corrupt state, by
// turning a closed Server back into a startable one.
func (s *Server) endRun(err error) error {
	if err == nil || errors.Is(err, ErrServerClosed) {
		return err
	}
	s.mu.Lock()
	if s.state == stateRunning {
		s.state = stateIdle
	}
	s.mu.Unlock()
	return err
}

// markClosed 置为已关闭终态。幂等且无错误可报——关闭是终态,重复置入无副作用。
// markClosed moves to the terminal closed state. Idempotent with nothing to report:
// closed is terminal, so re-entering it has no effect.
func (s *Server) markClosed() {
	s.mu.Lock()
	s.state = stateClosed
	s.mu.Unlock()
}

// IsStarted 报告 Server 是否正在服务(已启动且尚未关闭)。关闭后返回 false——生命周期
// 是三态的,"正在运行"与"已关闭"互斥。
// IsStarted reports whether the server is serving (started and not yet closed). It
// returns false after close: the lifecycle is three-state, so "running" and "closed"
// are mutually exclusive.
func (s *Server) IsStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == stateRunning
}

// IsClosed 报告 Server 是否已关闭。
// IsClosed reports whether the server has been shut down.
func (s *Server) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == stateClosed
}

// unwrap 暴露底层 *http.Server，仅供同包测试断言配置用。
// unwrap exposes the underlying *http.Server for same-package test assertions.
func (s *Server) unwrap() *http.Server {
	return s.httpSrv
}
