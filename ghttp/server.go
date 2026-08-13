package ghttp

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
)

type requestStateContextKey struct{}

type requestState struct {
	server        *Server
	responseState *responseWriteState
	// req 是派发入口的原始请求;表单缓存解析都在它上面进行。
	// req is the original request at dispatch entry; form parsing runs on it.
	req *http.Request
	// matched 在路由匹配成功后保存惰性路径参数源。
	// matched holds the lazy path-param source after a successful route match.
	matched *lazyPathParams
	// body 缓存请求体字节,供 RawBody/Body[T] 共享。
	// body memoizes the request body bytes for RawBody/Body[T] sharing.
	body *memoBody

	formOnce sync.Once
	form     url.Values
	postForm url.Values
	formErr  error

	multipartOnce sync.Once
	multipart     *multipart.Form
	multipartErr  error
}

func requestStateFromRequest(r *http.Request) *requestState {
	if r == nil {
		return nil
	}
	state, _ := r.Context().Value(requestStateContextKey{}).(*requestState)
	return state
}

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
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	responseState, w := newResponseWriteState(w, r.Method == http.MethodHead)
	reqState := &requestState{server: s, responseState: responseState}
	ctx := context.WithValue(r.Context(), requestStateContextKey{}, reqState)
	if r.Body != nil && r.Body != http.NoBody {
		// 只在有 body 时克隆请求再挂 memo,避免改写调用方的 *http.Request;
		// 复用同一请求对象(如基准 harness、自建循环)时也不会串状态。
		// clone the request only when wrapping the body, so the caller's
		// *http.Request is never mutated; reused requests (bench harnesses,
		// custom loops) cannot leak state between iterations.
		memo := &memoBody{src: r.Body}
		reqState.body = memo
		r = r.Clone(ctx)
		r.Body = memo
	} else {
		r = r.WithContext(ctx)
	}
	// req 记录 WithContext 之后的请求;下游 handler/表单解析都拿这个指针,
	// 避免 ParseForm/ParseMultipartForm 的缓存落在旧请求拷贝上。
	// req records the post-WithContext request, so form parsing caches land on
	// the same *http.Request the handlers see.
	reqState.req = r
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
	state := compileState(s.registry.snapshot(), s.middlewares, s.skipRules, s.config.strictRouting, s.errorHandler)
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
	mux          *routeMux
	strict       bool
	errorHandler ErrorHandler
	badRequest   http.Handler
	notFound     http.Handler
	notAllowed   http.Handler
}

func compileState(definitions []routeDefinition, serverMiddlewares []Middleware, serverSkips []skipRule, strict bool, errorHandler ErrorHandler) *compiledState {
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
		if route.definition.needsExtractor {
			terminal = extractorTerminal(route)
		}
		route.handler = Wrap(terminal, middlewares...)
	}
	state := &compiledState{mux: mux, strict: strict, errorHandler: errorHandler}
	state.badRequest = Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.writeOutcomeError(w, r, http.StatusBadRequest, Err(http.StatusBadRequest, ErrInvalidRequestPath.Error()))
	}), serverMiddlewares...)
	state.notFound = Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.writeOutcomeError(w, r, http.StatusNotFound, Err(http.StatusNotFound, http.StatusText(http.StatusNotFound)))
	}), serverMiddlewares...)
	state.notAllowed = Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.writeOutcomeError(w, r, http.StatusMethodNotAllowed, Err(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed)))
	}), serverMiddlewares...)
	return state
}

func extractorTerminal(route *compiledRoute) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, ok := route.definition.handler.(pathParamHandler)
		if !ok {
			writeRouteError(w, r, serverFromRequest(r), route.definition.errorWriter, route.definition.produces, http.StatusInternalServerError, Err(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)))
			return
		}
		// 防御性回退:中间件替换请求 context 导致 requestState 丢失时现场重建惰性源。
		// defensive fallback: rebuild the lazy source when a middleware replaced the
		// request context and dropped requestState.
		reqState := requestStateFromRequest(r)
		var fallback pathParamList
		if reqState == nil {
			requestPath, err := parseRequestPath(r.URL.EscapedPath(), route.definition.pattern.strict)
			if err != nil {
				writeRouteError(w, r, serverFromRequest(r), route.definition.errorWriter, route.definition.produces, http.StatusBadRequest, err)
				return
			}
			fallback, err = route.extract(requestPath)
			if err != nil {
				writeRouteError(w, r, serverFromRequest(r), route.definition.errorWriter, route.definition.produces, http.StatusBadRequest, err)
				return
			}
		} else if reqState.matched == nil {
			requestPath, err := parseRequestPath(r.URL.EscapedPath(), route.definition.pattern.strict)
			if err != nil {
				writeRouteError(w, r, serverFromRequest(r), route.definition.errorWriter, route.definition.produces, http.StatusBadRequest, err)
				return
			}
			if err := route.validate(requestPath); err != nil {
				writeRouteError(w, r, serverFromRequest(r), route.definition.errorWriter, route.definition.produces, http.StatusBadRequest, err)
				return
			}
			reqState.matched = &lazyPathParams{route: route, path: requestPath}
		}
		handler.ServeHTTPWithPathParams(w, r, fallback)
	})
}

func (s *compiledState) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestPath, err := parseRequestPath(r.URL.EscapedPath(), s.strict)
	if err != nil {
		s.badRequest.ServeHTTP(w, r)
		return
	}
	result := s.mux.match(r.Method, requestPath)
	switch result.kind {
	case routeMatchFound:
		var pooled *lazyPathParams
		if result.route.definition.needsExtractor {
			if reqState := requestStateFromRequest(r); reqState != nil {
				pooled = lazyPathParamsPool.Get().(*lazyPathParams)
				pooled.route = result.route
				pooled.path = requestPath
				reqState.matched = pooled
			}
		}
		defer func() {
			if pooled != nil {
				pooled.route = nil
				lazyPathParamsPool.Put(pooled)
			}
		}()
		result.route.handler.ServeHTTP(w, r)
	case routeMatchMethodNotAllowed:
		w.Header().Set("Allow", strings.Join(result.allow, ", "))
		s.notAllowed.ServeHTTP(w, r)
	default:
		s.notFound.ServeHTTP(w, r)
	}
}

func (s *compiledState) writeOutcomeError(w http.ResponseWriter, r *http.Request, code int, err error) {
	writeError(w, r, serverFromRequest(r), code, err)
}

func listenerAddress(ln net.Listener) string {
	if ln == nil || ln.Addr() == nil {
		return ""
	}
	return ln.Addr().String()
}
