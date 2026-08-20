package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
)

// Server 是核心 HTTP 服务器。
// Server is the core HTTP server.
// 实现 http.Handler，因此可以：
// It implements http.Handler, so it can be:
//   - 通过 Run() 独立使用；used standalone via Run().
//   - 作为子 handler 嵌入任意 http.ServeMux；embedded in any http.ServeMux.
//   - 通过 httptest 测试；tested via httptest.
//   - 被任意 func(http.Handler) http.Handler 中间件包装；wrapped by any middleware.
type Server struct {
	registry *routeRegistry
	config   *Config
	compiled atomic.Pointer[compiledState]

	codecMgr     *CodecManager
	renderer     Renderer
	envelope     EnvelopeFunc
	errorHandler ErrorHandler
	validator    Validator
	logger       Logger
	produces     []string
	consumes     []string

	// resolvedCodecs 是注册期预解析的响应编码器列表,避免每请求调用
	// codecMgr.Resolve(含 mime.ParseMediaType 约 75ns)。
	// resolvedCodecs is the registration-time resolved response codecs,
	// avoiding per-request codecMgr.Resolve (~75ns via mime.ParseMediaType).
	resolvedCodecs []responseCodec

	// resolvedJSONCodec 是 JSON 编码器的注册期缓存,供错误路径等直接使用。
	// resolvedJSONCodec is the registration-time cached JSON codec, for
	// direct use in error paths and the outcome cache.
	resolvedJSONCodec Codec

	middlewares []Middleware
	skipRules   []skipRule

	httpServer   *http.Server
	listenerAddr net.Addr
	mu           sync.Mutex
	routed       bool
	freezeErr    error
}

// New 使用给定选项创建 Server。
// New creates a new Server with the given options.
func New(opts ...ServerOption) *Server {
	c := &Config{
		address:          ":8080",
		clientIPResolver: defaultClientIPResolver,
		maxBodyBytes:     DefaultMaxBodyBytes,
	}
	for _, opt := range opts {
		opt(c)
	}
	// 信任代理默认信任所有 IP（gin 同款）；覆盖所有 IP 时输出 unsafe 警告。
	// trusted proxies default to trusting every IP (same as gin); an unsafe
	// warning is emitted when the boundary covers all IPs.
	if c.trustedCIDRs == nil {
		c.trustedCIDRs = defaultTrustedCIDRs
	}
	if isUnsafeTrustedProxies(c.trustedCIDRs) {
		warnUnsafeTrustedProxies(c)
	}
	if c.openAPIEnabled && !c.openAPIPathSet {
		c.openAPIPath = "/openapi.json"
	}

	s := &Server{
		registry:     newRouteRegistry(),
		config:       c,
		codecMgr:     NewCodecManager(),
		envelope:     c.envelope,
		errorHandler: c.errorHandler,
		validator:    nil,
		logger:       c.logger,
		produces:     c.produces,
		consumes:     c.consumes,
	}
	if c.validator != nil {
		s.validator = c.validator
	}
	if c.renderer != nil {
		s.renderer = c.renderer
	}
	if c.openAPIEnabled && c.openAPIPath != "" {
		s.registerOpenAPIEndpoint()
	}

	return s
}

// ServeHTTP 实现 http.Handler。
// ServeHTTP implements http.Handler.
// 首次请求前 lazy-compile 路由表；编译完成后走 compiledState 上的冻结闭包，
// 绕过后续请求的 finalizeRoutes 判空、compiled.Load 二次加载、validateHost
// 调用与 Server 级 defer/recover 四层开销。
// lazy-compiles the route table before the first request; after compilation
// the frozen closure on compiledState is used, bypassing the per-request
// finalizeRoutes check, second compiled.Load, validateHost call and the
// Server-level defer/recover overhead.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if state := s.compiled.Load(); state != nil {
		state.serveHTTP(w, r)
		return
	}
	s.finalizeRoutes()
	state := s.compiled.Load()
	if state == nil {
		panic("ghttp: route state was not compiled")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			s.writeRecoveredPanic(w, r, recovered)
		}
	}()
	if !s.validateHost(w, r) {
		return
	}
	state.ServeHTTP(w, r)
}

// validateHost 在路由分发前校验 Host 头（DNS rebinding 防护）。
// validateHost checks the Host header before route dispatch (DNS rebinding guard).
// 未配置 hostValidator 时直接放行；失败返回 400。
// passes through when no hostValidator is configured; failures return 400.
func (s *Server) validateHost(w http.ResponseWriter, r *http.Request) bool {
	validator := s.config.hostValidator
	if validator == nil {
		return true
	}
	host := s.config.resolveHost(r)
	if validator(host) {
		return true
	}
	if s.logger != nil {
		s.logger.WarnContext(r.Context(), "request host rejected", "host", host)
	}
	http.Error(w, "invalid host", http.StatusBadRequest)
	return false
}

func (s *Server) writeRecoveredPanic(w http.ResponseWriter, r *http.Request, recovered any) {
	if s.logger != nil {
		s.logger.ErrorContext(r.Context(), "panic recovered", "panic", recovered, "stack", string(debug.Stack()))
	}
	err := Err(
		http.StatusInternalServerError,
		http.StatusText(http.StatusInternalServerError),
		WithCause(fmt.Errorf("%w: %v", ErrHandlerPanic, recovered)),
	)
	if responseErrorHandlerStarted(r) {
		if !responseErrorWriteBlocked(r) {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}
	defer func() {
		if recover() != nil {
			if responseErrorWriteBlocked(r) {
				return
			}
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}()
	if s.dispatchError(w, r, http.StatusInternalServerError, err) {
		return
	}
	writeErrorWithCodec(w, r, s, http.StatusInternalServerError, err, nil, nil)
}

func (s *Server) buildServerHandler() http.Handler {
	return s
}

// Run 在指定地址（或配置地址）启动 HTTP 服务器。
// Run starts the HTTP server on the given address (or config address).
func (s *Server) Run(addr ...string) error {
	addrStr := s.config.address
	if len(addr) > 0 {
		addrStr = addr[0]
	}

	httpServer := s.prepareHTTPServer(addrStr)
	ln, err := net.Listen("tcp", addrStr)
	if err != nil {
		return err
	}
	return s.serveListener(httpServer, ln, httpServer.Serve)
}

// Serve 在已有 listener 上启动 HTTP 服务器。
// Serve starts the HTTP server on an existing listener.
func (s *Server) Serve(ln net.Listener) error {
	if ln == nil {
		return ErrNilListener
	}
	httpServer := s.prepareHTTPServer(listenerAddress(ln))
	return s.serveListener(httpServer, ln, httpServer.Serve)
}

// ListenAndServeTLS 在指定地址启动 HTTPS 服务器。
// ListenAndServeTLS starts the HTTPS server on the given address.
func (s *Server) ListenAndServeTLS(addr, certFile, keyFile string) error {
	if addr == "" {
		addr = s.config.address
	}
	httpServer := s.prepareHTTPServer(addr)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.serveListener(httpServer, ln, func(l net.Listener) error {
		return httpServer.ServeTLS(l, certFile, keyFile)
	})
}

// ServeTLS 在已有 listener 上启动 HTTPS 服务器。
// ServeTLS starts the HTTPS server on an existing listener.
func (s *Server) ServeTLS(ln net.Listener, certFile, keyFile string) error {
	if ln == nil {
		return ErrNilListener
	}
	httpServer := s.prepareHTTPServer(listenerAddress(ln))
	return s.serveListener(httpServer, ln, func(l net.Listener) error {
		return httpServer.ServeTLS(l, certFile, keyFile)
	})
}

func (s *Server) prepareHTTPServer(addr string) *http.Server {
	s.finalizeRoutes()

	httpServer := s.newHTTPServer(addr)
	s.mu.Lock()
	s.httpServer = httpServer
	s.listenerAddr = nil
	s.mu.Unlock()
	return httpServer
}

func (s *Server) newHTTPServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           s.buildServerHandler(),
		ReadTimeout:       s.config.readTimeout,
		ReadHeaderTimeout: s.config.readHeaderTimeout,
		WriteTimeout:      s.config.writeTimeout,
		IdleTimeout:       s.config.idleTimeout,
		MaxHeaderBytes:    s.config.maxHeaderBytes,
		TLSConfig:         s.config.tlsConfig,
		BaseContext:       s.config.baseContext,
		ConnContext:       s.config.connContext,
		ErrorLog:          s.config.errorLog,
	}
}

func (s *Server) serveListener(httpServer *http.Server, ln net.Listener, serve func(net.Listener) error) error {
	s.mu.Lock()
	s.listenerAddr = ln.Addr()
	if s.listenerAddr != nil {
		httpServer.Addr = s.listenerAddr.String()
	}
	s.mu.Unlock()

	if err := serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown 优雅关闭服务器。
// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	s.mu.Lock()
	httpServer := s.httpServer
	s.mu.Unlock()
	if httpServer != nil {
		return httpServer.Shutdown(ctx)
	}
	return nil
}

// Close 立即关闭服务器，不等待活动请求。
// Close immediately closes the server without waiting for active requests.
func (s *Server) Close() error {
	s.mu.Lock()
	httpServer := s.httpServer
	s.mu.Unlock()
	if httpServer != nil {
		return httpServer.Close()
	}
	return nil
}

// Addr 返回活动 listener 地址（:0 时含实际端口）。
// Addr returns the active listener address, including the actual port for :0.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listenerAddr
}

// Use 向服务器追加中间件并返回服务器以支持链式调用。
// Use adds middleware to the server and returns the server for chaining.
func (s *Server) Use(mws ...Middleware) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicIfFrozenLocked()
	s.middlewares = append(s.middlewares, mws...)
	return s
}

// SkipUse 豁免服务器级中间件在匹配路由上执行（精确路径模式）。
// SkipUse exempts a server-level middleware on the matching route (exact pattern).
// 典型用途：全局鉴权中间件放行登录/健康检查路由。
// Typical use: exempt an auth middleware for login and health routes.
func (s *Server) SkipUse(mw Middleware, method, pattern string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicIfFrozenLocked()
	s.skipRules = append(s.skipRules, skipRule{mw: mw, method: method, pattern: pattern})
	return s
}

// Consumes 声明自动解码的默认请求 Content-Type。
// Consumes declares default request Content-Types for body decoding.
func (s *Server) Consumes(contentTypes ...string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicIfFrozenLocked()
	s.consumes = normalizeContentTypes(contentTypes)
	return s
}

func (s *Server) routePath(path string) string {
	return path
}

func (s *Server) routeGroup() *Group {
	return nil
}

func (s *Server) registerDefinitions(definitions ...routeDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicIfFrozenLocked()
	return s.registry.register(definitions...)
}

func (s *Server) panicIfFrozenLocked() {
	if s.routed {
		panic(ErrServerFrozen)
	}
}

func (s *Server) assertMutable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicIfFrozenLocked()
}

func (s *Server) dispatchError(w http.ResponseWriter, r *http.Request, defaultCode int, err error) bool {
	if responseErrorWriteBlocked(r) {
		if s != nil && s.logger != nil {
			s.logger.ErrorContext(r.Context(), "http error after response completion", "error", err)
		}
		return true
	}
	if s == nil || s.errorHandler == nil {
		return false
	}
	if !beginResponseErrorHandler(r) {
		if s.logger != nil {
			s.logger.ErrorContext(r.Context(), "http error handler reentry suppressed", "error", err)
		}
		return true
	}
	normalized := normalizeHTTPError(defaultCode, err)
	s.errorHandler(w, r, normalized)
	return true
}

func normalizeHTTPError(defaultCode int, err error) *HTTPError {
	if explicit := AsError(err); explicit != nil {
		copy := *explicit
		if copy.Code == 0 {
			copy.Code = defaultCode
		}
		if copy.Message == "" {
			copy.Message = http.StatusText(copy.Code)
		}
		return &copy
	}
	code := defaultCode
	if code == 0 {
		code = http.StatusInternalServerError
	}
	message := http.StatusText(code)
	if code >= http.StatusInternalServerError {
		message = http.StatusText(http.StatusInternalServerError)
	}
	return &HTTPError{Code: code, Message: message, Err: err}
}

func (s *Server) registerOpenAPIEndpoint() {
	pattern, err := parseRoutePattern(s.config.openAPIPath, s.config.strictRouting)
	if err != nil {
		panic(err)
	}
	s.registry.reserve(pattern)
	err = s.registry.register(routeDefinition{
		method:  http.MethodGet,
		pattern: pattern,
		handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			document, err := s.OpenAPI()
			if err != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(document)
		}),
		internal: true,
	})
	if err != nil {
		panic(err)
	}
}

func (s *Server) producesContentTypes() []string {
	return s.produces
}

func (s *Server) consumesContentTypes() []string {
	return s.consumes
}

func (s *Server) owner() *Server {
	return s
}

// MatchedParams 返回请求的路径参数与惰性请求视图。
// MatchedParams returns the request's path params plus the lazy request view.
// 可在请求链上的任意中间件或 handler 中安全调用。
// it is safe to call from any middleware or handler on the request path.
func (s *Server) MatchedParams(r *http.Request) Params {
	if r == nil {
		return Params{}
	}
	return paramsFromRequest(r, s.config)
}

func (s *Server) finalizeRoutes() {
	if s.compiled.Load() != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.freezeErr != nil {
		panic(s.freezeErr)
	}
	if s.compiled.Load() != nil {
		return
	}
	if s.routed {
		return
	}

	s.routed = true
	defer func() {
		if recovered := recover(); recovered != nil {
			if err, ok := recovered.(error); ok {
				s.freezeErr = err
			} else {
				s.freezeErr = fmt.Errorf("route freeze panic: %v", recovered)
			}
			panic(recovered)
		}
	}()
	// 注册期一次性解析响应编码器,错误路径/协商路径不再每请求 Resolve。
	// resolve response codecs once at registration; error and negotiation
	// paths no longer call Resolve per request.
	s.resolvedCodecs = resolveResponseCodecs(s, s.produces)
	s.resolvedJSONCodec, _ = s.codecMgr.Resolve(MIMEJSON)
	state := compileState(s, s.registry.snapshot(), s.middlewares, s.skipRules, s.config.strictRouting, s.errorHandler)
	s.compiled.Store(state)
}

// Group 创建带前缀与可选中间件的路由组。
// Group creates a route group with a prefix and optional middlewares.
func (s *Server) Group(prefix string, mws ...Middleware) *Group {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicIfFrozenLocked()
	return &Group{
		server:      s,
		prefix:      prefix,
		middlewares: append([]Middleware(nil), mws...),
	}
}

type compiledState struct {
	server       *Server
	mux          *routeMux
	strict       bool
	errorHandler ErrorHandler
	// serveHTTP 是冻结后的热路径入口：无 hostValidator 时直通自身 ServeHTTP；
	// 有 validator 时包一层校验与 panic 兜底。通过 compiled 原子指针发布，
	// 与 compiledState 一起获得 happens-before 保证。
	// serveHTTP is the frozen hot-path entry: it calls ServeHTTP directly when
	// no hostValidator is configured, otherwise it wraps validation and a panic
	// guard. Published through the compiled atomic pointer so it shares the
	// same happens-before guarantee as compiledState.
	serveHTTP http.HandlerFunc
	// outcomeFast 表示 400/404/405 分支可无状态执行(无中间件、无错误模型)。
	// outcomeFast marks the 400/404/405 branches as stateless (no middleware,
	// no error model).
	outcomeFast bool
	// outcomes 缓存 400/404/405 的预序列化响应;条件不满足时为 nil。
	// outcomes caches pre-serialized 400/404/405 responses; nil when the
	// preconditions do not hold.
	outcomes map[int]*compiledOutcome
	// 404/405/400 结局同样以切片链执行,服务器级中间件统一生效。
	// the 404/405/400 outcomes also run as slice chains so server-level
	// middleware applies uniformly.
	badRequestChain []HandlerFunc
	notFoundChain   []HandlerFunc
	notAllowedChain []HandlerFunc
}

// compiledOutcome 是 400/404/405 的预序列化响应。
// compiledOutcome is a pre-serialized 400/404/405 response.
type compiledOutcome struct {
	status      int
	contentType string
	body        []byte
}

// jsonContentTypeHeader 是缓存响应共享的 Content-Type 头值,避免每请求分配。
// jsonContentTypeHeader is the shared Content-Type value for cached responses.
var jsonContentTypeHeader = []string{MIMEJSON}

func compileState(server *Server, definitions []routeDefinition, serverMiddlewares []Middleware, serverSkips []skipRule, strict bool, errorHandler ErrorHandler) *compiledState {
	mux := newRouteMux(definitions)
	for _, route := range mux.routes {
		middlewares := append([]Middleware(nil), serverMiddlewares...)
		var skips []skipRule
		if route.definition.group != nil {
			middlewares = append(middlewares, route.definition.group.middlewares...)
			skips = route.definition.group.skipRules
		}
		middlewares = append(middlewares, route.definition.middlewares...)
		skips = append(append([]skipRule(nil), serverSkips...), skips...)
		middlewares = filterSkippedMiddlewares(middlewares, route.definition.method, route.definition.pattern.path, skips)
		terminal := route.definition.handler
		stateIndependent := route.definition.stateIndependent
		hasErrorModel := errorHandler != nil || route.definition.errorWriter != nil || server.config.errorWriter != nil
		// 编译终端为 HandlerFunc,并按形态决定是否需要旧 requestState 注入。
		// 无状态形态(fastBuild/raw/stateIndependent)零注入;
		// typed body/form 终端经 ctxFromRequest 读 Ctx 上的缓存。
		// Compile the terminal into a HandlerFunc and decide per shape
		// whether the Ctx must be read back through the request context.
		// Stateless shapes (fastBuild/raw/stateIndependent) inject nothing;
		// typed body/form terminals read the Ctx caches via ctxFromRequest.
		var terminalFunc HandlerFunc
		// needsState:需要经请求 context 读回 Ctx 的路由挂 Ctx(每请求一次)。
		// error 模型与 typed body/form 路由必挂;fastBuild/raw 在带参或有
		// 中间件时挂(MatchedParams 经 context 读回参数);path-only
		// (stateIndependent)路由从不为此挂——参数在 c.params 直传。
		// needsState: routes whose handler reads the Ctx back through the
		// request context attach it (once per request). Error models and typed
		// body/form routes always attach; fastBuild/raw attach with params or
		// middleware (MatchedParams reads params back via the context);
		// path-only (stateIndependent) routes never attach for params — they
		// ride c.params directly.
		needsState := hasErrorModel
		switch {
		case route.definition.fastBuild != nil && !hasErrorModel:
			// NoInput + JSON/Text 无错误模型时的注册期直编。
			// registration-time direct compile of NoInput + JSON/Text
			// without error model.
			fast := route.definition.fastBuild(server, nil)
			if len(middlewares) == 0 && !route.definition.pattern.hasParams() &&
				route.definition.method == http.MethodGet {
				// 免 Ctx 直编终端:GET + 无中间件 + 无参数 + 无错误模型时
				// dispatch 完全跳过 Ctx 池与切片链,直接调用该 handler。
				// Ctx-free direct terminal: GET + no middleware + no params +
				// no error model lets dispatch skip the Ctx pool and slice
				// chain entirely.
				route.direct = fast
				terminalFunc = func(c *Ctx) { fast(c.W, c.R) }
				break
			}
			terminalFunc = func(c *Ctx) { fast(c.W, c.R) }
			needsState = needsState || route.definition.pattern.hasParams()
		case route.definition.terminal == routeTerminalRaw:
			// raw handler 经 Ctx 写入器获得 HEAD 体抑制;中间件经 Ctx API
			// (c.Body/c.Param) 读 body/params,不依赖 request context 挂载。
			// the raw handler gets the Ctx as its writer (HEAD body
			// suppression); middleware reads body/params via Ctx API, no
			// request context attach needed.
			terminalFunc = func(c *Ctx) { route.definition.handler.ServeHTTP(c, c.R) }
			if !hasErrorModel && len(middlewares) == 0 &&
				route.definition.method == http.MethodGet {
				route.direct = route.definition.handler.ServeHTTP
			}
		case stateIndependent:
			// path-only 类型化终结器:params 由 match 填充在 Ctx 上直传,
			// 从不为此挂 Ctx(旧模型同样免状态注入);无错误模型时走 raw
			// writer 直写(无 committed 追踪开销)。
			// path-only typed terminals: params ride the Ctx directly and
			// never force the attach (old model parity); without an error
			// model the raw writer is used directly (no committed-tracking).
			pathHandler, ok := terminal.(pathParamHandler)
			if ok {
				terminalFunc = func(c *Ctx) { pathHandler.ServeHTTPWithPathParams(c.W, c.R, c.params) }
			} else {
				terminalFunc = func(c *Ctx) { terminal.ServeHTTP(c.W, c.R) }
			}
			// 单参数直达 + 直编取值终端:命中时参数值直传,免 Ctx。
			// direct single-param hit with a raw-value terminal: the value
			// flows straight in, Ctx-free.
			if raw, rawOK := terminal.(directPathValueHandlerFunc); rawOK &&
				!hasErrorModel && len(middlewares) == 0 &&
				route.definition.method == http.MethodGet && route.directName != "" {
				route.directParamHandler = raw
			}
		default:
			// typed 路由(body/form/query):params 由 match 填充在 Ctx 上,
			// 终端经 ServeHTTPWithPathParams 直读;body/form 经 ctxFromRequest
			// 读 Ctx 缓存。
			// typed routes (body/form/query): params are filled on the Ctx by
			// match and the terminal reads them via ServeHTTPWithPathParams;
			// body/form read the Ctx caches via ctxFromRequest.
			needsState = true
			if pathHandler, ok := terminal.(pathParamHandler); ok {
				terminalFunc = func(c *Ctx) { pathHandler.ServeHTTPWithPathParams(c, c.R, c.params) }
			} else {
				terminalFunc = func(c *Ctx) { terminal.ServeHTTP(c, c.R) }
			}
		}
		route.compiledHandlers = Chain(terminalFunc, middlewares...)
		route.needsState = needsState
	}
	state := &compiledState{
		server:       server,
		mux:          mux,
		strict:       strict,
		errorHandler: errorHandler,
		outcomeFast:  len(serverMiddlewares) == 0 && errorHandler == nil && server.config.errorWriter == nil,
	}
	// 冻结热路径入口:无 hostValidator 时直通自身 ServeHTTP(零检查零 defer),
	// 有 validator 时保留校验与 panic 兜底语义。
	// freeze the hot-path entry: without a hostValidator it calls ServeHTTP
	// directly (zero checks, zero defer); with one it keeps the validateHost
	// call and the panic guard semantics.
	if server.config.hostValidator == nil {
		state.serveHTTP = state.ServeHTTP
	} else {
		state.serveHTTP = func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					server.writeRecoveredPanic(w, r, recovered)
				}
			}()
			if !server.validateHost(w, r) {
				return
			}
			state.ServeHTTP(w, r)
		}
	}
	state.outcomes = buildOutcomeCache(server, state.outcomeFast)
	state.badRequestChain = Chain(func(c *Ctx) {
		state.writeOutcomeError(c, c.R, http.StatusBadRequest, Err(http.StatusBadRequest, ErrInvalidRequestPath.Error()))
	}, serverMiddlewares...)
	state.notFoundChain = Chain(func(c *Ctx) {
		state.writeOutcomeError(c, c.R, http.StatusNotFound, Err(http.StatusNotFound, http.StatusText(http.StatusNotFound)))
	}, serverMiddlewares...)
	state.notAllowedChain = Chain(func(c *Ctx) {
		state.writeOutcomeError(c, c.R, http.StatusMethodNotAllowed, Err(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)))
	}, serverMiddlewares...)
	return state
}

// buildOutcomeCache 在响应确定时预序列化 400/404/405 响应。
// buildOutcomeCache pre-serializes 400/404/405 responses when they are
// deterministic. 每请求序列化错误体 + Accept 协商是 miss 路径的主要开销
// (NotFound 场景 8 次分配);缓存后 miss 路径零分配。
// per-request error serialization plus Accept negotiation dominates the miss
// path (8 allocs in the NotFound scenario); caching makes the miss path
// allocation-free.
func buildOutcomeCache(server *Server, outcomeFast bool) map[int]*compiledOutcome {
	if !outcomeFast || server == nil || server.config == nil {
		return nil
	}
	// 任何参与错误输出的配置都会使响应非确定,放弃缓存。
	// any configuration that participates in error output makes the response
	// non-deterministic; skip caching.
	if server.envelope != nil || server.config.problemDetails {
		return nil
	}
	for _, contentType := range server.produces {
		if contentType != MIMEJSON {
			return nil
		}
	}
	codec := server.resolvedJSONCodec
	if codec == nil {
		var ok bool
		codec, ok = server.codecMgr.Resolve(MIMEJSON)
		if !ok {
			return nil
		}
	}
	if _, isJSON := codec.(*JSONCodec); !isJSON {
		return nil
	}
	// 与 writeErrorWithCodec 的 body 构造保持一致:Err 字段不参与序列化。
	// mirror writeErrorWithCodec's body construction: Err is not serialized.
	cache := make(map[int]*compiledOutcome, 3)
	statuses := []struct {
		status  int
		message string
	}{
		{http.StatusBadRequest, ErrInvalidRequestPath.Error()},
		{http.StatusNotFound, http.StatusText(http.StatusNotFound)},
		{http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)},
	}
	for _, entry := range statuses {
		body, err := json.Marshal(HTTPError{Code: entry.status, Message: entry.message})
		if err != nil {
			return nil
		}
		// JSONCodec.Marshal 追加换行;缓存字节必须逐字节一致。
		// JSONCodec.Marshal appends a newline; cached bytes must match it.
		body = append(body, '\n')
		cache[entry.status] = &compiledOutcome{
			status:      entry.status,
			contentType: MIMEJSON,
			body:        body,
		}
	}
	return cache
}

// writeCachedOutcome 写预序列化响应;无缓存条目时返回 false。
// writeCachedOutcome writes a cached response; false when no entry exists.
func (s *compiledState) writeCachedOutcome(w http.ResponseWriter, code int) bool {
	cached := s.outcomes[code]
	if cached == nil {
		return false
	}
	// 直接赋值 header 切片,跳过 Set 的规范化分配。
	// assign the header slice directly, skipping Set's normalization alloc.
	w.Header()[cached.contentType] = jsonContentTypeHeader
	w.WriteHeader(cached.status)
	_, _ = w.Write(cached.body)
	return true
}

func (s *compiledState) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 直编预扫描:GET + 静态命中 + 直编终端时,完全绕过 Ctx 池、切片链与
	// 主调度(单次 staticGet 查找);panic 恢复语义与主路径一致。
	// direct pre-scan: a static GET hit with a direct terminal bypasses the
	// Ctx pool, slice chain and main dispatch entirely (one staticGet lookup);
	// panic recovery semantics match the main path.
	if r.URL.RawPath == "" && r.Method == http.MethodGet {
		if route := s.mux.staticGet[r.URL.Path]; route != nil {
			if route.direct != nil {
				func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							s.server.writeRecoveredPanic(w, r, recovered)
						}
					}()
					route.direct(w, r)
				}()
				return
			}
		} else if route, value, ok := s.mux.matchDirectParam(http.MethodGet, r.URL.Path); ok && route.directParamHandler != nil {
			// 静态未命中才尝试单参数直达,避免给带中间件的静态路由
			// 增加一次无效的 prefix 查找。
			// try the single-param index only on a static miss, keeping
			// middleware-carrying static routes free of a wasted lookup.
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						s.server.writeRecoveredPanic(w, r, recovered)
					}
				}()
				route.directParamHandler(w, r, value)
			}()
			return
		}
	}
	// 池化 Ctx:整条链直传同一实例,请求结束回收。
	// pooled Ctx: the whole chain passes the same instance; recycled at the
	// end of the request.
	c := acquireCtx(w, r)
	c.server = s.server
	c.suppressBody = r.Method == http.MethodHead
	directHandled := false
	defer func() {
		if recovered := recover(); recovered != nil {
			rr := r
			if c.R != nil {
				rr = c.R
			}
			s.server.writeRecoveredPanic(w, rr, recovered)
		}
		// 释放必须在 recover 之后:错误处理还需要读取状态。直接路径已在
		// 分支中单独释放,此处跳过避免重复放入池。
		// release must follow recover: error handling still reads the state.
		// the direct path releases separately; skip here to avoid double Put.
		if !directHandled {
			releaseCtx(c)
		}
	}()
	var requestPath requestPath
	// 静态路径先走 O(1) 直达索引,未转义的单参数路径走前缀直达索引;其余
	// 走 radix 树一次遍历完成匹配、段偏移收集与路径校验;badPath 走 400。
	// static paths take the O(1) direct index and unescaped single-param
	// paths take the prefix index; everything else falls through to one radix
	// traversal that matches, collects segment offsets and validates the path.
	var result routeMatchResult
	staticMatched := false
	if r.URL.RawPath == "" {
		if staticResult, ok := s.mux.matchStatic(r.Method, r.URL.Path); ok {
			result = staticResult
			staticMatched = true
		}
	}
	resultResolved := staticMatched
	if !resultResolved && !s.mux.hasDynamic && r.URL.RawPath == "" &&
		(r.URL.Path == "/" || !strings.HasSuffix(r.URL.Path, "/")) {
		// 纯静态路由表快路径:静态索引未命中即可判定结果,跳过参数索引与
		// radix 遍历。需先校验路径合法性(空段/dot 段/非法转义 → 400)。
		// purely-static table fast path: the static index alone decides;
		// skip the param index and radix traversal. Validate the path first
		// (empty segments, dot segments, bad escapes → 400).
		if err := validateRawPath(r.URL.Path); err != nil {
			result = routeMatchResult{kind: routeMatchBadPath, badPath: err}
			resultResolved = true
		} else if allow, exists := s.mux.staticPathAllows[r.URL.Path]; exists {
			result = routeMatchResult{kind: routeMatchMethodNotAllowed, allow: strings.Split(allow, ", ")}
			resultResolved = true
		} else {
			result = routeMatchResult{kind: routeMatchNotFound}
			resultResolved = true
		}
	}
	if !resultResolved && r.URL.RawPath == "" && (r.Body == nil || r.Body == http.NoBody) {
		if route, value, ok := s.mux.matchDirectParam(r.Method, r.URL.Path); ok {
			c.params.Add(route.directName, value)
			if route.needsState {
				c.R = attachCtx(c)
				if route.definition.needsExtractor {
					// 惰性参数源:MatchedParams/Params.Path 在中间件
					// 中读取时依赖 lazyParams。prefix 段偏移从注册期
					// 静态段长度直接计算(无转义段不会进入直达索引)。
					// lazy param source: MatchedParams/Params.Path
					// in middleware reads through lazyParams. Prefix
					// offsets are computed from the static segment
					// lengths (no escaped segments in the index).
					c.lazyParams = lazyPathParamsPool.Get().(*lazyPathParams)
					c.lazyParams.route = route
					c.lazyParams.path.raw = r.URL.Path
					c.lazyParams.path.segments = pathSegmentList{}
					offset := 1
					segs := route.definition.pattern.segments
					for i := 0; i < len(segs)-1; i++ {
						end := offset + len(segs[i].value)
						c.lazyParams.path.segments.Add(pathSegment{start: offset, end: end})
						offset = end + 1
					}
					c.lazyParams.path.segments.Add(pathSegment{start: offset, end: len(r.URL.Path)})
					defer func() {
						c.lazyParams.route = nil
						lazyPathParamsPool.Put(c.lazyParams)
					}()
				}
			}
			c.handlers = route.compiledHandlers
			c.Next()
			return
		}
	}
	if !resultResolved {
		result = s.mux.match(r.Method, r.URL.EscapedPath(), s.strict)
		if result.kind == routeMatchBadPath {
			if s.outcomeFast && r.Method != http.MethodHead {
				if s.writeCachedOutcome(w, http.StatusBadRequest) {
					return
				}
				s.writeOutcomeError(w, r, http.StatusBadRequest, Err(http.StatusBadRequest, ErrInvalidRequestPath.Error()))
				return
			}
			c.R = attachCtx(c)
			c.handlers = s.badRequestChain
			c.Next()
			return
		}
		requestPath = result.path
	}
	switch result.kind {
	case routeMatchFound:
		route := result.route
		// 免 Ctx 直编终端:无中间件+无参数+无错误模型的 GET fastBuild 路由
		// 直接作为 http.HandlerFunc 执行,绕过 Ctx 池与切片链,回收 Ctx 后退出。
		// Ctx-free direct terminal: GET fastBuild routes with no middleware,
		// no params and no error model run as a plain http.HandlerFunc, bypass
		// the Ctx pool and slice chain, then recycle the Ctx and return.
		if route.direct != nil && r.Method != http.MethodHead {
			directHandled = true
			releaseCtx(c)
			route.direct(w, r)
			return
		}
		// params 由 match 一次性填充到 Ctx,终端经 c.Param()/c.Params() 直读。
		// params are filled onto the Ctx by match; the terminal reads them
		// directly via c.Param()/c.Params().
		c.params = result.params
		// typed body/form 终端经 ctxFromRequest 读 Ctx 上的 body/form 缓存,
		// 此处把 Ctx 挂到请求 context(每请求一次 WithContext,替代旧
		// installState 的 requestState 注入);其余形态零注入。
		// typed body/form terminals read the Ctx's body/form caches via
		// ctxFromRequest, so the Ctx is attached to the request context here
		// (one WithContext per request, replacing installState's requestState
		// injection); every other shape runs with zero injection.
		if route.needsState {
			c.R = attachCtx(c)
			if route.definition.needsExtractor {
				c.lazyParams = lazyPathParamsPool.Get().(*lazyPathParams)
				c.lazyParams.route = route
				c.lazyParams.path = requestPath
				defer func() {
					c.lazyParams.route = nil
					lazyPathParamsPool.Put(c.lazyParams)
				}()
			}
		}
		c.handlers = route.compiledHandlers
		c.Next()
	case routeMatchMethodNotAllowed:
		w.Header().Set("Allow", strings.Join(result.allow, ", "))
		if s.outcomeFast && r.Method != http.MethodHead {
			if s.writeCachedOutcome(w, http.StatusMethodNotAllowed) {
				return
			}
			s.writeOutcomeError(w, r, http.StatusMethodNotAllowed, Err(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)))
			return
		}
		c.R = attachCtx(c)
		c.handlers = s.notAllowedChain
		c.Next()
	default:
		if s.outcomeFast && r.Method != http.MethodHead {
			if s.writeCachedOutcome(w, http.StatusNotFound) {
				return
			}
			s.writeOutcomeError(w, r, http.StatusNotFound, Err(http.StatusNotFound, http.StatusText(http.StatusNotFound)))
			return
		}
		c.R = attachCtx(c)
		c.handlers = s.notFoundChain
		c.Next()
	}
}

// attachCtx 把 Ctx 挂到请求 context 一次,替代旧 installState。
// attachCtx attaches the Ctx to the request context once, replacing the old
// installState.
func attachCtx(c *Ctx) *http.Request {
	return c.R.WithContext(context.WithValue(c.R.Context(), ctxKey{}, c))
}

func (s *compiledState) writeOutcomeError(w http.ResponseWriter, r *http.Request, code int, err error) {
	writeError(w, r, s.server, code, err)
}

func listenerAddress(ln net.Listener) string {
	if ln == nil || ln.Addr() == nil {
		return ""
	}
	return ln.Addr().String()
}
