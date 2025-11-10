package gserver

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/sofiworker/gk/glog"
	"github.com/valyala/fasthttp"
)

var (
	ctxPool = sync.Pool{
		New: func() interface{} {
			return &Context{}
		},
	}
	requestPool = sync.Pool{
		New: func() interface{} {
			return &http.Request{
				Header: make(http.Header),
				URL:    &url.URL{},
			}
		},
	}
	bodyReaderPool = sync.Pool{
		New: func() interface{} {
			return &bodyReader{}
		},
	}
	respWriterPool = sync.Pool{
		New: func() interface{} {
			return &respWriter{}
		},
	}
)

type RequestConverter func(ctx *fasthttp.RequestCtx) (*http.Request, error)
type RequestConverterFailedHandler func(ctx *fasthttp.RequestCtx)

type Server struct {
	Addr      string
	Port      int
	TLSConfig *tls.Config
	server    *fasthttp.Server

	*Config

	IRouter
	Match

	started bool
	mu      sync.RWMutex
}

func NewServer(opts ...ServerOption) *Server {
	c := &Config{
		matcher:                   newServerMatcher(),
		codec:                     newCodecFactory(),
		logger:                    glog.Default(),
		convertFastRequestCtxFunc: ConvertToHTTPRequest,
		convertFailedHandler: func(ctx *fasthttp.RequestCtx) {
			ctx.SetStatusCode(http.StatusInternalServerError)
		},
	}
	for _, opt := range opts {
		opt(c)
	}

	r := &RouterGroup{
		Handlers: nil,
		path:     "/",
		root:     true,
	}
	s := &Server{
		IRouter:   r,
		Match:     newServerMatcher(),
		Addr:      "0.0.0.0",
		Port:      8080,
		TLSConfig: nil,
		Config:    c,
	}
	r.engine = s

	if s.server == nil {
		s.server = &fasthttp.Server{}
	}
	s.server.Handler = s.FastHandler
	s.server.TLSConfig = s.TLSConfig

	return s
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
	s.logger.Infof("start http server %v", addr)

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

func (s *Server) Use(middleware ...HandlerFunc) IRouter {
	s.IRouter.Use(middleware...)
	return s.IRouter
}

func (s *Server) addRoute(method, path string, handlers ...HandlerFunc) {
	CheckPathValid(path)
	if method == "" {
		panic("HTTP method cannot be empty")
	}
	if len(handlers) == 0 {
		panic("there must be at least one handler")
	}
	s.logger.Infof("add route %s %s", method, path)
	if err := s.AddRoute(method, path, handlers...); err != nil {
		panic(err)
	}
}

func (s *Server) FastHandler(ctx *fasthttp.RequestCtx) {
	request, err := s.convertFastRequestCtxFunc(ctx)
	if err != nil {
		s.logger.Errorf("failed to convert fasthttp request to http.Request, error is %v", err)
		s.convertFailedHandler(ctx)
		return
	}
	if request == nil {
		s.convertFailedHandler(ctx)
		return
	}
	writer := respWriterPool.Get().(*respWriter)
	writer.ctx = ctx
	s.ServeHTTP(writer, request)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := ctxPool.Get().(*Context)
	ctx.Reset()
	ctx.Writer = wrapResponseWriter(w)
	ctx.Request = r

	defer func() {
		if r.Body != nil {
			r.Body.Close()
		}
		ResetRequest(r)
		requestPool.Put(r)

		if rw, ok := w.(*respWriter); ok {
			rw.Reset()
			respWriterPool.Put(rw)
		}

		ctxPool.Put(ctx)
	}()

	rPath := path.Clean(r.URL.Path)
	if s.UseRawPath && len(r.URL.RawPath) > 0 {
		rPath = r.URL.RawPath
	}

	matchResult := s.Lookup(r.Method, rPath)
	if matchResult == nil {
		s.handleNotFound(ctx)
		return
	}

	if len(matchResult.Handlers) > 0 {
		ctx.handlers = append(ctx.handlers, matchResult.Handlers...)
	}

	if len(matchResult.PathParams) > 0 {
		for k, v := range matchResult.PathParams {
			ctx.AddParam(k, v)
		}
	}

	ctx.Next()
}

func (s *Server) handleNotFound(ctx *Context) {
	ctx.Writer.WriteHeader(http.StatusNotFound)
}
