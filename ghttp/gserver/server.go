package gserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/sofiworker/gk/glog"
	"github.com/valyala/fasthttp"
)

var (
	ctxPool = sync.Pool{
		New: func() interface{} {
			return &Context{
				handlerIndex: -1,
				pathParams:   make(map[string]string),
			}
		},
	}
	respWriterPool = sync.Pool{
		New: func() interface{} {
			return &respWriter{}
		},
	}
)

type Server struct {
	server *fasthttp.Server

	*Config

	IRouter
	Match
}

func NewServer(opts ...ServerOption) *Server {
	c := &Config{
		matcher:    newServerMatcher(),
		codec:      newCodecFactory(),
		logger:     glog.Default(),
		UseRawPath: false,
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
		IRouter: r,
		Match:   newServerMatcher(),
		Config:  c,
		server: &fasthttp.Server{
			Concurrency:                   c.Concurrency,
			IdleTimeout:                   c.IdleTimeout,
			MaxRequestBodySize:            c.MaxRequestBodySize,
			MaxIdleWorkerDuration:         c.MaxIdleWorkerDuration,
			MaxConnsPerIP:                 c.MaxConnsPerIP,
			MaxRequestsPerConn:            c.MaxRequestsPerConn,
			TCPKeepalive:                  c.TCPKeepalive,
			TCPKeepalivePeriod:            c.TCPKeepalivePeriod,
			DisableKeepalive:              c.DisableKeepalive,
			DisableHeaderNamesNormalizing: c.DisableHeaderNamesNormalizing,
			DisablePreParseMultipartForm:  c.DisablePreParseMultipartForm,
			NoDefaultContentType:          c.NoDefaultContentType,
			NoDefaultDate:                 c.NoDefaultDate,
			NoDefaultServerHeader:         c.NoDefaultServerHeader,
			ReduceMemoryUsage:             c.ReduceMemoryUsage,
			StreamRequestBody:             c.StreamRequestBody,
		},
	}
	r.engine = s
	s.server.Handler = s.FastHandler

	return s
}

func (s *Server) Run(addr ...string) error {
	address := resolveAddress(addr)
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	return s.RunListener(ln)
}

func (s *Server) RunTLS(addr, certFile, keyFile string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.server.ServeTLS(ln, certFile, keyFile)
}

func (s *Server) RunListener(l net.Listener) error {
	return s.server.Serve(l)
}

func (s *Server) Shutdown() error {
	return s.server.Shutdown()
}

func (s *Server) ShutdownWithContext(ctx context.Context) error {
	return s.server.ShutdownWithContext(ctx)
}

func resolveAddress(addr []string) string {
	switch len(addr) {
	case 0:
		if port := os.Getenv("PORT"); port != "" {
			return ":" + port
		}
		return ":8080"
	case 1:
		return addr[0]
	default:
		panic("too many parameters")
	}
}

func (s *Server) Use(middleware ...HandlerFunc) IRouter {
	s.IRouter.Use(middleware...)
	return s.IRouter
}

func (s *Server) addRoute(method, path string, handlers ...HandlerFunc) {
	if method == "" {
		panic("HTTP method cannot be empty")
	}
	if len(handlers) == 0 {
		panic("there must be at least one handler")
	}
	CheckPathValid(path)
	s.logger.Infof("add route %s %s", method, path)
	if err := s.AddRoute(method, path, handlers...); err != nil {
		panic(err)
	}
}

func (s *Server) FastHandler(ctx *fasthttp.RequestCtx) {
	gctx := ctxPool.Get().(*Context)
	gctx.fastCtx = ctx

	writer := respWriterPool.Get().(*respWriter)
	writer.ctx = ctx
	gctx.Writer = wrapResponseWriter(writer)
	gctx.codec = s.codec

	defer func() {
		gctx.Reset()
		writer.Reset()
		respWriterPool.Put(writer)
		ctxPool.Put(gctx)
	}()

	method := string(ctx.Method())
	routePath := path.Clean(string(ctx.Path()))
	if s.UseRawPath {
		if raw := ctx.URI().PathOriginal(); len(raw) > 0 {
			routePath = string(raw)
		}
	}

	matchResult := s.Lookup(method, routePath)
	if matchResult == nil {
		s.handleNotFound(gctx)
		return
	}

	if len(matchResult.Handlers) > 0 {
		gctx.handlers = append(gctx.handlers, matchResult.Handlers...)
	}

	if len(matchResult.PathParams) > 0 {
		for k, v := range matchResult.PathParams {
			gctx.AddParam(k, v)
		}
	}

	gctx.Next()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fastReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(fastReq)

	if err := copyHTTPRequestToFast(fastReq, r); err != nil {
		http.Error(w, "failed to convert request", http.StatusBadRequest)
		return
	}

	var fastCtx fasthttp.RequestCtx
	fastCtx.Init(fastReq, resolveRemoteAddr(r.RemoteAddr), nil)

	s.FastHandler(&fastCtx)

	writeFastResponseToHTTP(w, &fastCtx.Response)
}

func (s *Server) handleNotFound(ctx *Context) {
	ctx.Writer.WriteHeader(http.StatusNotFound)
}

func copyHTTPRequestToFast(dst *fasthttp.Request, src *http.Request) error {
	dst.Header.Reset()
	dst.ResetBody()

	if src.Method != "" {
		dst.Header.SetMethod(src.Method)
	}
	if src.Proto != "" {
		dst.Header.SetProtocol(src.Proto)
	}

	rawURI := src.RequestURI
	if rawURI == "" && src.URL != nil {
		rawURI = src.URL.RequestURI()
	}
	if rawURI == "" {
		rawURI = "/"
	}
	dst.SetRequestURI(rawURI)

	scheme := "http"
	if src.URL != nil && src.URL.Scheme != "" {
		scheme = src.URL.Scheme
	} else if src.TLS != nil {
		scheme = "https"
	}
	dst.URI().SetScheme(scheme)

	host := src.Host
	if host == "" && src.URL != nil {
		host = src.URL.Host
	}
	if host != "" {
		dst.Header.SetHost(host)
		dst.URI().SetHost(host)
	}

	for k, values := range src.Header {
		for _, v := range values {
			dst.Header.Add(k, v)
		}
	}

	if src.Body != nil {
		bodyReader := src.Body
		defer bodyReader.Close()
		body, err := io.ReadAll(bodyReader)
		if err != nil {
			return err
		}
		dst.SetBody(body)
	} else {
		dst.SetBody(nil)
	}

	if src.Close {
		dst.SetConnectionClose()
	}

	return nil
}

func resolveRemoteAddr(addr string) net.Addr {
	if addr == "" {
		return &net.TCPAddr{}
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return &net.TCPAddr{IP: net.ParseIP(addr)}
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		p = 0
	}
	ip := net.ParseIP(host)
	return &net.TCPAddr{IP: ip, Port: p}
}

func writeFastResponseToHTTP(w http.ResponseWriter, resp *fasthttp.Response) {
	if resp == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	header := w.Header()
	if len(header) > 0 {
		for k := range header {
			delete(header, k)
		}
	}

	hasContentType := false
	resp.Header.All()(func(key, value []byte) bool {
		k := string(key)
		if !hasContentType && strings.EqualFold(k, fasthttp.HeaderContentType) {
			hasContentType = true
		}
		header.Add(k, string(value))
		return true
	})

	status := resp.StatusCode()
	if status == 0 {
		status = http.StatusOK
	}

	body := resp.Body()
	if !hasContentType && len(body) > 0 {
		sample := body
		if len(sample) > 512 {
			sample = sample[:512]
		}
		header.Set(fasthttp.HeaderContentType, http.DetectContentType(sample))
	}

	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}
