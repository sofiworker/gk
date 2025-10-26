package gserver

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/sofiworker/gk/ghttp/codec"
	"github.com/valyala/fasthttp"
)

type RequestConverter func(ctx *fasthttp.RequestCtx) *http.Request

type Server struct {
	Addr string
	Port int

	TLSConfig *tls.Config

	server  *fasthttp.Server
	matcher Matcher
	ctxPool sync.Pool
	root    *RouterGroup
	Routers Routers
	logger  Logger

	convertFastRequestCtxFunc RequestConverter

	codecManager *codec.CodecManager

	noRoute      []HandlerFunc
	panicHandler func(*Context, interface{})

	started bool
	mu      sync.RWMutex
}

func NewServer(opts ...ServerOption) *Server {
	rootGroup := &RouterGroup{
		Handlers: nil,
		basePath: "/",
		root:     true,
	}

	s := &Server{
		Addr:                      "0.0.0.0",
		Port:                      8080,
		TLSConfig:                 nil,
		root:                      rootGroup,
		Routers:                   rootGroup,
		matcher:                   newServerMatcher(),
		codecManager:              codec.DefaultManager(),
		logger:                    newStdLogger(),
		convertFastRequestCtxFunc: convertFastRequestCtxSafe,
	}

	rootGroup.engine = s

	s.ctxPool.New = func() interface{} {
		ctx := &Context{}
		ctx.setEngine(s)
		return ctx
	}

	s.server = &fasthttp.Server{
		Handler:   s.FastHandler,
		TLSConfig: s.TLSConfig,
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.server != nil {
		s.server.Handler = s.FastHandler
		s.server.TLSConfig = s.TLSConfig
	}

	return s
}

func (s *Server) CodecManager() *codec.CodecManager {
	return s.codecManager
}

func (s *Server) SetCodecManager(manager *codec.CodecManager) {
	if manager == nil {
		return
	}
	s.codecManager = manager
}

func (s *Server) Logger() Logger {
	return s.logger
}

func (s *Server) SetLogger(logger Logger) {
	if logger == nil {
		return
	}
	s.logger = logger
}

func (s *Server) RegisterCodec(cdc codec.Codec) {
	if cdc == nil {
		return
	}
	if s.codecManager == nil {
		s.codecManager = codec.NewCodecManager()
	}
	s.codecManager.RegisterCodec(cdc)
}

func (s *Server) SetPanicHandler(handler func(*Context, interface{})) {
	s.panicHandler = handler
}

func (s *Server) SetRequestConverter(converter RequestConverter) {
	if converter == nil {
		return
	}
	s.convertFastRequestCtxFunc = converter
}

func (s *Server) Use(middleware ...HandlerFunc) {
	if s.root != nil {
		s.root.Use(middleware...)
	}
}

func (s *Server) NoRoute(handlers ...HandlerFunc) {
	s.noRoute = append([]HandlerFunc(nil), handlers...)
}

func (s *Server) addRoute(method, path string, handlers ...HandlerFunc) {
	if len(path) == 0 {
		panic("path should not be ''")
	}
	if path[0] != '/' {
		panic("path must begin with '/'")
	}
	if method == "" {
		panic("HTTP method cannot be empty")
	}
	if len(handlers) == 0 {
		panic("there must be at least one handler")
	}
	if err := s.matcher.AddRoute(method, path, handlers...); err != nil {
		panic(err)
	}
}

func (s *Server) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}

	if s.server == nil {
		s.server = &fasthttp.Server{
			Handler:   s.FastHandler,
			TLSConfig: s.TLSConfig,
		}
	}
	s.server.Handler = s.FastHandler
	s.server.TLSConfig = s.TLSConfig

	addr := net.JoinHostPort(s.Addr, strconv.Itoa(s.Port))
	server := s.server
	tlsCfg := s.TLSConfig
	s.started = true
	s.mu.Unlock()

	var err error
	if tlsCfg != nil {
		var ln net.Listener
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			s.mu.Lock()
			s.started = false
			s.mu.Unlock()
			return err
		}
		err = server.Serve(tls.NewListener(ln, tlsCfg))
	} else {
		err = server.ListenAndServe(addr)
	}

	s.mu.Lock()
	s.started = false
	s.mu.Unlock()

	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	server := s.server
	s.mu.RUnlock()
	if server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return server.ShutdownWithContext(shutdownCtx)
}

func (s *Server) FastHandler(ctx *fasthttp.RequestCtx) {
	request := s.convertFastRequestCtxFunc(ctx)
	if request == nil {
		if s.logger != nil {
			s.logger.Errorf("failed to convert fasthttp request to http.Request")
		}
		ctx.Error("failed to convert request", fasthttp.StatusInternalServerError)
		return
	}
	respWriter := &respWriter{ctx: ctx}
	s.ServeHTTP(respWriter, request)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := s.ctxPool.Get().(*Context)
	ctx.Reset()
	ctx.setEngine(s)
	ctx.Writer = wrapResponseWriter(w)
	ctx.Request = r

	defer func() {
		if rec := recover(); rec != nil {
			if s.panicHandler != nil {
				s.panicHandler(ctx, rec)
			} else {
				if s.logger != nil {
					s.logger.Errorf("panic recovered: %v\n%s", rec, debug.Stack())
				}
				if ctx.Writer != nil && !ctx.Writer.Written() {
					ctx.AbortWithStatus(http.StatusInternalServerError)
				}
			}
		}
		s.ctxPool.Put(ctx)
	}()

	matchResult := s.matcher.Match(r.Method, r.URL.Path)
	if matchResult == nil {
		s.handleNotFound(ctx)
		return
	}

	if len(matchResult.Handlers) > 0 {
		ctx.handlers = append(ctx.handlers[:0], matchResult.Handlers...)
	} else {
		ctx.handlers = ctx.handlers[:0]
	}

	if len(matchResult.Params) > 0 {
		for _, v := range matchResult.Params {
			ctx.AddParam(v.Key, v.Value)
		}
	}

	ctx.SetFullPath(matchResult.Path)
	ctx.Next()
}

func (s *Server) handleNotFound(ctx *Context) {
	if len(s.noRoute) == 0 {
		http.NotFound(ctx.Writer, ctx.Request)
		return
	}
	ctx.handlers = append(ctx.handlers[:0], s.combineHandlers(s.noRoute...)...)
	ctx.SetFullPath(ctx.Request.URL.Path)
	ctx.Next()
}

func (s *Server) combineHandlers(handlers ...HandlerFunc) []HandlerFunc {
	if s.root == nil {
		cp := make([]HandlerFunc, len(handlers))
		copy(cp, handlers)
		return cp
	}
	return s.root.copyHandler(handlers...)
}

func (s *Server) Group(relativePath string, handlers ...HandlerFunc) Routers {
	if s.root == nil {
		return nil
	}
	return s.root.Group(relativePath, handlers...)
}

func (s *Server) Handle(method, path string, handlers ...HandlerFunc) {
	if s.root == nil {
		return
	}
	s.root.Handle(method, path, handlers...)
}

func (s *Server) GET(path string, handlers ...HandlerFunc) {
	s.Handle(http.MethodGet, path, handlers...)
}

func (s *Server) POST(path string, handlers ...HandlerFunc) {
	s.Handle(http.MethodPost, path, handlers...)
}

func (s *Server) PUT(path string, handlers ...HandlerFunc) {
	s.Handle(http.MethodPut, path, handlers...)
}

func (s *Server) DELETE(path string, handlers ...HandlerFunc) {
	s.Handle(http.MethodDelete, path, handlers...)
}

func (s *Server) PATCH(path string, handlers ...HandlerFunc) {
	s.Handle(http.MethodPatch, path, handlers...)
}

func (s *Server) HEAD(path string, handlers ...HandlerFunc) {
	s.Handle(http.MethodHead, path, handlers...)
}

func (s *Server) OPTIONS(path string, handlers ...HandlerFunc) {
	s.Handle(http.MethodOptions, path, handlers...)
}

func (s *Server) ANY(path string, handlers ...HandlerFunc) {
	if s.root == nil {
		return
	}
	s.root.ANY(path, handlers...)
}

func convertFastRequestCtxSafe(ctx *fasthttp.RequestCtx) *http.Request {
	uri := ctx.URI()

	scheme := "http"
	if ctx.IsTLS() {
		scheme = "https"
	}

	u := &url.URL{
		Scheme:   scheme,
		Host:     string(ctx.Host()),
		Path:     string(uri.Path()),
		RawQuery: string(uri.QueryString()),
	}

	// copy body to make it stable
	bodyCopy := append([]byte(nil), ctx.PostBody()...)
	reqBody := io.NopCloser(bytes.NewReader(bodyCopy))

	req := &http.Request{
		Method:        string(ctx.Method()),
		URL:           u,
		Host:          string(ctx.Host()),
		Body:          reqBody,
		ContentLength: int64(len(bodyCopy)),
		Header:        make(http.Header, ctx.Request.Header.Len()),
		RemoteAddr:    ctx.RemoteAddr().String(),
		// minimal proto info
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}

	if ctx.IsTLS() {
		state := ctx.TLSConnectionState()
		req.TLS = state
	}

	ctx.Request.Header.VisitAll(func(k, v []byte) {
		req.Header.Add(http.CanonicalHeaderKey(string(k)), string(v))
	})

	// attach original fasthttp ctx in Context for downstream if needed
	baseCtx := context.WithValue(context.Background(), "", ctx)
	req = req.WithContext(baseCtx)

	return req
}
