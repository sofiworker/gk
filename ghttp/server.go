package ghttp

import (
	"context"
	"net/http"
	"sync"
)

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

	middlewares []MiddlewareFunc

	httpServer *http.Server
	mu         sync.Mutex
	openAPI    *OpenAPI
	routed     bool
}

// New creates a new Server with the given name, version, and options.
func New(name, version string, opts ...ServerOption) *Server {
	c := &Config{
		address: ":8080",
	}
	for _, opt := range opts {
		opt(c)
	}

	s := &Server{
		router:    NewRadixRouter(),
		config:    c,
		codecMgr:  NewCodecManager(),
		envelope:  DefaultEnvelope,
		openAPI:   NewOpenAPI(name, version),
		validator: c.validator,
	}

	return s
}

// ServeHTTP implements http.Handler — applies middlewares then delegates to router.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.finalizeRoutes()
	h := s.buildHandlerChain()
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

	s.httpServer = &http.Server{
		Addr:    addrStr,
		Handler: s.buildHandlerChain(),
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(context.Background())
	}
	return nil
}

// Use adds middleware to the server.
func (s *Server) Use(mw MiddlewareFunc) {
	s.middlewares = append(s.middlewares, mw)
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

// Group holds a set of routes with a common prefix.
type Group struct {
	server      *Server
	prefix      string
	middlewares []MiddlewareFunc
}
