package ghttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
)

type requestStateContextKey struct{}
type matchedParamsContextKey struct{}

type requestState struct {
	server        *Server
	responseState *responseWriteState
}

// Server is the core HTTP server.
// It implements http.Handler, so it can be:
//   - used standalone via Run()
//   - embedded in any http.ServeMux as a sub-handler
//   - tested via httptest
//   - wrapped by any func(http.Handler) http.Handler middleware
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

	httpServer   *http.Server
	listenerAddr net.Addr
	mu           sync.Mutex
	routed       bool
	freezeErr    error
}

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
	if c.openAPIEnabled && !c.openAPIPathSet {
		c.openAPIPath = "/openapi.json"
	}

	s := &Server{
		registry:     newRouteRegistry(c.strictRouting),
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

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	responseState, w := newResponseWriteState(w, r.Method == http.MethodHead)
	ctx := context.WithValue(r.Context(), requestStateContextKey{}, requestState{server: s, responseState: responseState})
	r = r.WithContext(ctx)
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
	state.ServeHTTP(w, r)
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

// buildHandlerChain returns the Server handler after route compilation.
func (s *Server) buildHandlerChain() http.Handler {
	return s
}

func (s *Server) buildServerHandler() http.Handler {
	return s
}

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

// Serve starts the HTTP server on an existing listener.
func (s *Server) Serve(ln net.Listener) error {
	if ln == nil {
		return ErrNilListener
	}
	httpServer := s.prepareHTTPServer(listenerAddress(ln))
	return s.serveListener(httpServer, ln, httpServer.Serve)
}

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

// Addr returns the active listener address, including the actual port for :0.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listenerAddr
}

// Use adds middleware to the server.
func (s *Server) Use(mws ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicIfFrozenLocked()
	s.middlewares = append(s.middlewares, mws...)
}

// Consumes declares the default request Content-Types for automatic body decoding.
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

// MatchedParams returns the request's path params plus the lazy request view.
// It is safe to call from any middleware or handler on the request path.
func (s *Server) MatchedParams(r *http.Request) Params {
	if r == nil {
		return Params{}
	}
	if list, ok := r.Context().Value(matchedParamsContextKey{}).(pathParamList); ok {
		return paramsFromRequestWithPathParams(r, s.config, list)
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
	state := compileState(s.registry.snapshot(), s.middlewares, s.config.strictRouting, s.errorHandler)
	s.compiled.Store(state)
}

// Group creates a route group with a prefix and optional middlewares.
func (s *Server) Group(prefix string, mws ...Middleware) *Group {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicIfFrozenLocked()
	return &Group{
		server:      s,
		prefix:      prefix,
		middlewares: mws,
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

func compileState(definitions []routeDefinition, serverMiddlewares []Middleware, strict bool, errorHandler ErrorHandler) *compiledState {
	mux := newRouteMux(definitions)
	for _, route := range mux.routes {
		middlewares := append([]Middleware(nil), serverMiddlewares...)
		if route.definition.group != nil {
			middlewares = append(middlewares, route.definition.group.currentMiddlewaresLocked()...)
		}
		middlewares = append(middlewares, route.definition.middlewares...)
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
		requestPath, err := parseRequestPath(r.URL.EscapedPath(), route.definition.pattern.strict)
		if err != nil {
			writeError(w, r, serverFromRequest(r), http.StatusBadRequest, err)
			return
		}
		params, err := route.extract(requestPath)
		if err != nil {
			writeError(w, r, serverFromRequest(r), http.StatusBadRequest, err)
			return
		}
		handler, ok := route.definition.handler.(pathParamHandler)
		if !ok {
			writeError(w, r, serverFromRequest(r), http.StatusInternalServerError, Err(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)))
			return
		}
		handler.ServeHTTPWithPathParams(w, r, params)
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
		if result.route.definition.needsExtractor {
			params, err := result.route.extract(requestPath)
			if err != nil {
				s.badRequest.ServeHTTP(w, r)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), matchedParamsContextKey{}, params))
		}
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
