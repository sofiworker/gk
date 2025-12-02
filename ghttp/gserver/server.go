package gserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
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

	matcher := c.matcher
	if matcher == nil {
		matcher = newServerMatcher()
	}

	r := &RouterGroup{
		Handlers: nil,
		path:     "/",
		root:     true,
	}
	s := &Server{
		IRouter: r,
		Match:   matcher,
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

func (s *Server) Static(relativePath, root string) IRouter {
	if root == "" {
		panic("static root cannot be empty")
	}
	return s.StaticFS(relativePath, http.Dir(root))
}

func (s *Server) StaticFS(relativePath string, fs http.FileSystem) IRouter {
	CheckPathValid(relativePath)
	if fs == nil {
		panic("filesystem is nil")
	}

	handler := s.createStaticHandler(fs)
	absolutePath := JoinPaths(relativePath, "/*filepath")

	s.GET(absolutePath, handler)
	s.HEAD(absolutePath, handler)
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
	s.logger.Debugf("add route %s %s", method, path)
	if err := s.AddRoute(method, path, handlers...); err != nil {
		panic(err)
	}
}

func (s *Server) FastHandler(ctx *fasthttp.RequestCtx) {
	// Get Context from pool
	gctx := ctxPool.Get().(*Context)
	gctx.fastCtx = ctx
	gctx.logger = s.logger

	// Get ResponseWriter from pool
	writer := respWriterPool.Get().(*respWriter)
	writer.ctx = ctx
	gctx.Writer = writer // Direct assignment instead of wrapping for better performance
	gctx.codec = s.codec

	// Defer cleanup and return objects to pools
	defer func() {
		gctx.Reset()
		writer.Reset()
		respWriterPool.Put(writer)
		ctxPool.Put(gctx)
	}()

	// Determine route path
	method := string(ctx.Method())
	routePath := string(ctx.URI().PathOriginal())
	if routePath == "" {
		routePath = string(ctx.Path())
	}
	if !s.UseRawPath && routePath != "" && !strings.Contains(routePath, "..") {
		routePath = path.Clean(routePath)
	}
	if routePath == "" {
		routePath = "/"
	}

	if strings.Contains(string(ctx.RequestURI()), "..") {
		s.handleBadRequest(gctx)
		return
	}

	// Find matching route
	matchResult := s.Lookup(method, routePath)
	if matchResult == nil {
		s.handleNotFound(gctx)
		return
	}

	// Apply route handlers
	if len(matchResult.Handlers) > 0 {
		gctx.handlers = append(gctx.handlers, matchResult.Handlers...)
	}

	// Apply path parameters
	if len(matchResult.PathParams) > 0 {
		for k, v := range matchResult.PathParams {
			gctx.AddParam(k, v)
		}
	}

	// Execute middleware chain
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

func (s *Server) createStaticHandler(fs http.FileSystem) HandlerFunc {
	return func(ctx *Context) {
		filepath := strings.TrimSpace(ctx.Param("filepath"))
		if filepath == "" {
			filepath = "."
		} else {
			filepath = path.Clean("/" + filepath)
			filepath = strings.TrimPrefix(filepath, "/")
		}

		if strings.Contains(filepath, "..") {
			ctx.Status(http.StatusBadRequest)
			return
		}

		req := buildHTTPRequestFromFast(ctx.fastCtx)
		if req == nil || ctx.Writer == nil {
			return
		}

		if !serveFileFromFS(ctx, req, fs, filepath) && !ctx.Writer.Written() {
			ctx.Status(http.StatusNotFound)
		}
	}
}

func (s *Server) handleNotFound(ctx *Context) {
	ctx.Writer.WriteHeader(http.StatusNotFound)
}

func (s *Server) handleBadRequest(ctx *Context) {
	ctx.Writer.WriteHeader(http.StatusBadRequest)
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

	// Copy headers more efficiently
	for k, values := range src.Header {
		for _, v := range values {
			dst.Header.Add(k, v)
		}
	}

	// Optimized body copying to reduce memory allocations
	// Use direct copy when possible instead of.ReadAll which creates an intermediate buffer
	if src.Body != nil {
		bodyReader := src.Body
		// Instead of reading all into memory, stream directly to fasthttp request body
		// This reduces memory allocations and improves performance for large payloads
		if _, err := io.Copy(dst.BodyWriter(), bodyReader); err != nil {
			// Close the body reader on error
			_ = bodyReader.Close()
			return err
		}
		// Close the body reader after successful copy
		_ = bodyReader.Close()
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

func buildHTTPRequestFromFast(ctx *fasthttp.RequestCtx) *http.Request {
	if ctx == nil {
		return nil
	}

	req := &http.Request{
		Method: string(ctx.Method()),
		Header: make(http.Header),
		Host:   string(ctx.Host()),
		URL: &url.URL{
			Path:     string(ctx.Path()),
			RawQuery: string(ctx.URI().QueryString()),
		},
	}

	ctx.Request.Header.VisitAll(func(k, v []byte) {
		req.Header.Add(string(k), string(v))
	})
	return req
}

func serveFileFromFS(ctx *Context, req *http.Request, fs http.FileSystem, filepath string) bool {
	file, info, err := openStaticFile(fs, filepath)
	if err != nil {
		return false
	}
	defer file.Close()

	if req.URL != nil {
		servedPath := "/" + strings.TrimPrefix(filepath, "/")
		if servedPath == "/." {
			servedPath = "/"
		}
		req.URL.Path = servedPath
	}

	http.ServeContent(ctx.Writer, req, info.Name(), info.ModTime(), file)
	return true
}

func openStaticFile(fs http.FileSystem, name string) (http.File, os.FileInfo, error) {
	if name == "" {
		name = "."
	}

	f, err := fs.Open(name)
	if err != nil {
		return nil, nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}

	if info.IsDir() {
		_ = f.Close()
		indexPath := path.Join(name, "index.html")
		indexFile, err := fs.Open(indexPath)
		if err != nil {
			return nil, nil, err
		}
		indexInfo, err := indexFile.Stat()
		if err != nil {
			_ = indexFile.Close()
			return nil, nil, err
		}
		if indexInfo.IsDir() {
			_ = indexFile.Close()
			return nil, nil, os.ErrNotExist
		}
		return indexFile, indexInfo, nil
	}

	return f, info, nil
}
