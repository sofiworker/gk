package ghttp

import (
	"context"
	"errors"
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

	middlewares []MiddlewareFunc

	httpServer *http.Server
	mu         sync.Mutex
	openAPI    *OpenAPI
	routed     bool
}

// New creates a new Server with the given options.
func New(opts ...ServerOption) *Server {
	c := &Config{
		address:          ":8080",
		clientIPResolver: defaultClientIPResolver,
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
	h := s.buildHandlerChain()
	r = r.WithContext(context.WithValue(r.Context(), serverContextKey{}, s))
	h.ServeHTTP(w, r)
}

// buildHandlerChain wraps the router with all middlewares.
func (s *Server) buildHandlerChain() http.Handler {
	h := http.Handler(s.router)
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		h = s.middlewares[i](h)
	}
	return h
}

// Run starts the HTTP server on the given address (or config address).
func (s *Server) Run(addr ...string) error {
	addrStr := s.config.address
	if len(addr) > 0 {
		addrStr = addr[0]
	}

	s.finalizeRoutes()

	httpServer := &http.Server{
		Addr:    addrStr,
		Handler: s.buildHandlerChain(),
	}
	s.mu.Lock()
	s.httpServer = httpServer
	s.mu.Unlock()

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

// Use adds middleware to the server.
func (s *Server) Use(mw MiddlewareFunc) {
	s.middlewares = append(s.middlewares, mw)
}

// UseFunc adds handler-function middleware to the server.
func (s *Server) UseFunc(mw HandlerMiddlewareFunc) {
	s.Use(HandlerMiddleware(mw))
}

func (s *Server) handleRoute(method, path string, handler http.Handler, mws ...MiddlewareFunc) error {
	h := handler
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return s.router.Register(method, path, h)
}

func (s *Server) addRouteSpec(method, path, doc string, tags []string, operationID string, reqType, pathType, queryType reflect.Type, responses []responseSpec) {
	if s.openAPI != nil {
		s.openAPI.AddRoute(method, path, doc, tags, operationID, reqType, pathType, queryType, responses)
	}
}

func (s *Server) owner() *Server {
	return s
}

func (s *Server) finalizeRoutes() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.routed {
		return
	}
	s.routed = true

	if s.openAPI != nil {
		spec := s.openAPI.Build()
		_ = s.router.Register(http.MethodGet, "/openapi.json", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(spec)
		}))
	}
}

// Router returns the underlying router for direct access.
func (s *Server) Router() Router {
	return s.router
}

// Group creates a route group with a prefix and optional middlewares.
func (s *Server) Group(prefix string, mws ...MiddlewareFunc) *Group {
	return &Group{
		server:      s,
		prefix:      prefix,
		middlewares: mws,
	}
}
