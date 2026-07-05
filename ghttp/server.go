package ghttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"
	"sync"
)

type serverContextKey struct{}

// Server is the core HTTP server.
// It implements http.Handler, so it can be:
//   - used standalone via Run()
//   - embedded in any http.ServeMux as a sub-handler
//   - tested via httptest
//   - wrapped by any func(http.Handler) http.Handler middleware
type Server struct {
	router Router // pluggable routing engine
	config *Config

	codecMgr  *CodecManager
	renderer  Renderer
	envelope  EnvelopeFunc
	validator Validator
	logger    Logger
	produces  []string
	consumes  []string

	middlewares []Middleware

	httpServer   *http.Server
	listenerAddr net.Addr
	mu           sync.Mutex
	openAPI      *OpenAPI
	routed       bool
	setupErr     error
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

	s := &Server{
		router:    NewRadixRouter(),
		config:    c,
		codecMgr:  NewCodecManager(),
		envelope:  c.envelope,
		validator: newDefaultValidator(),
		logger:    c.logger,
		produces:  c.produces,
		consumes:  c.consumes,
	}
	if c.validator != nil {
		s.validator = c.validator
	}
	if c.openAPIEnabled {
		s.openAPI = NewOpenAPI(c.openAPITitle, c.openAPIVersion)
	}
	if c.renderer != nil {
		s.renderer = c.renderer
	}
	if c.router != nil {
		s.router = c.router
	}

	return s
}

// ServeHTTP implements http.Handler - applies middlewares then delegates to router.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.finalizeRoutes()
	s.buildServerHandler().ServeHTTP(w, r)
}

// buildHandlerChain wraps the router with all middlewares.
func (s *Server) buildHandlerChain() http.Handler {
	return Wrap(s.router, s.middlewares...)
}

func (s *Server) buildServerHandler() http.Handler {
	h := s.buildHandlerChain()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), serverContextKey{}, s))
		h.ServeHTTP(w, r)
	})
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
	s.middlewares = append(s.middlewares, mws...)
}

// Consumes declares the default request Content-Types for automatic body decoding.
func (s *Server) Consumes(contentTypes ...string) *Server {
	s.consumes = normalizeContentTypes(contentTypes)
	return s
}

func (s *Server) handleRoute(method, path string, handler http.Handler, mws ...Middleware) error {
	return s.router.Register(method, path, wrapRouteHandler(handler, mws...))
}

func wrapRouteHandler(handler http.Handler, mws ...Middleware) http.Handler {
	if len(mws) == 0 {
		return handler
	}
	wrapped := Wrap(handler, mws...)
	if _, ok := handler.(pathParamHandler); !ok {
		return wrapped
	}
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, params pathParamList) {
		wrapped.ServeHTTP(w, requestWithPathParams(r, params))
	})
}

func (s *Server) recordSetupError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setupErr = errors.Join(s.setupErr, err)
}

func (s *Server) panicSetupErrorLocked() {
	if s.setupErr != nil {
		panic(s.setupErr)
	}
}

func (s *Server) panicSetupError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicSetupErrorLocked()
}

func (s *Server) addRouteSpec(method, path string, reqType, respType reflect.Type, doc RouteDoc, consumes, produces []string) {
	if s.openAPI != nil {
		s.openAPI.AddRoute(method, path, reqType, respType, doc, consumes, produces)
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

func (s *Server) finalizeRoutes() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panicSetupErrorLocked()
	if s.routed {
		return
	}
	s.routed = true

	if s.openAPI != nil {
		spec := s.openAPI.Build()
		if err := s.router.Register(http.MethodGet, "/openapi.json", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(spec)
		})); err != nil {
			s.setupErr = errors.Join(s.setupErr, err)
			panic(s.setupErr)
		}
	}
}

// Router returns the underlying router for direct access.
func (s *Server) Router() Router {
	return s.router
}

// Group creates a route group with a prefix and optional middlewares.
func (s *Server) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		server:      s,
		prefix:      prefix,
		middlewares: mws,
	}
}

func listenerAddress(ln net.Listener) string {
	if ln == nil || ln.Addr() == nil {
		return ""
	}
	return ln.Addr().String()
}
