package gserver

import (
	"bytes"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/sofiworker/gk/ghttp/codec"
	"github.com/valyala/fasthttp"
)

type Server struct {
	Addr string
	Port int

	server *fasthttp.Server

	TLSConfig *tls.Config

	Routers

	matcher Matcher

	ctxPool sync.Pool

	convertFastRequestCtxFunc func(ctx *fasthttp.RequestCtx) *http.Request

	w http.ResponseWriter

	codec codec.Codec
}

func NewServer() *Server {
	rootGroup := &RouterGroup{
		Handlers: nil,
		basePath: "/",
		root:     true,
	}

	s := &Server{
		Addr:                      "0.0.0.0",
		Port:                      8080,
		TLSConfig:                 nil,
		Routers:                   rootGroup,
		matcher:                   newServerMatcher(),
		convertFastRequestCtxFunc: convertToHTTPRequest,
		w:                         nil,
	}

	rootGroup.engine = s

	s.ctxPool.New = func() interface{} {
		return &Context{}
	}

	fastServer := &fasthttp.Server{
		Handler:   s.FastHandler,
		TLSConfig: s.TLSConfig,
	}
	s.server = fastServer
	return s
}

func (s *Server) addRoute(method, path string, handlers ...HandlerFunc) {
	if len(path) == 0 {
		panic("path should not be ''")
	}
	if path[0] != '/' {
		panic("path must begin with '/'")
	}
	if method == "" {
		panic("HTTP method can not be empty")
	}
	if len(handlers) == 0 {
		panic("there must be at least one handler")
	}
	err := s.matcher.AddRoute(method, path, handlers...)
	if err != nil {
		panic(err)
	}
}

func (s *Server) Start() error {
	if s.server == nil {
		s.server = &fasthttp.Server{
			Handler:   s.FastHandler,
			TLSConfig: s.TLSConfig,
		}
	} else {
		s.server.Handler = s.FastHandler
		s.server.TLSConfig = s.TLSConfig
	}

	addr := net.JoinHostPort(s.Addr, strconv.Itoa(s.Port))

	if s.TLSConfig != nil {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		return s.server.Serve(tls.NewListener(ln, s.TLSConfig))
	}

	return s.server.ListenAndServe(addr)
}

func (s *Server) FastHandler(ctx *fasthttp.RequestCtx) {
	r := s.convertFastRequestCtxFunc(ctx)
	if r == nil {
		ctx.Error("failed to convert request", fasthttp.StatusInternalServerError)
		return
	}
	respWriter := &respWriter{ctx: ctx}
	s.ServeHTTP(respWriter, r)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := s.ctxPool.Get().(*Context)
	c.Reset()
	c.Writer = w
	c.Request = r

	httpMethod := r.Method
	rPath := r.URL.Path
	matchResult := s.matcher.Match(httpMethod, rPath)
	if matchResult == nil {
		http.NotFound(w, r)
		s.ctxPool.Put(c)
		return
	}
	if len(matchResult.Handlers) > 0 {
		c.handlers = matchResult.Handlers
	}
	if len(matchResult.Params) > 0 {
		for k, v := range matchResult.Params {
			c.AddParam(k, v)
		}
	}
	c.queryCache = nil
	c.Next()

	s.ctxPool.Put(c)
}

// 将 fasthttp.RequestCtx 转换为标准 http.Request
func convertToHTTPRequest(ctx *fasthttp.RequestCtx) *http.Request {
	u := &url.URL{
		Scheme:   "http",
		Host:     string(ctx.Host()),
		Path:     string(ctx.Path()),
		RawQuery: string(ctx.URI().QueryString()),
	}

	// 创建请求体
	body := bytes.NewReader(ctx.Request.Body())

	// 创建 http.Request
	req, err := http.NewRequest(
		string(ctx.Method()),
		u.String(),
		body,
	)
	if err != nil {
		return nil
	}

	// 复制请求头
	ctx.Request.Header.VisitAll(func(key, value []byte) {
		req.Header.Set(string(key), string(value))
	})

	// 设置远程地址
	req.RemoteAddr = ctx.RemoteAddr().String()

	return req
}
