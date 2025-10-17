package gserver

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"net/url"
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
	s := &Server{
		Addr:      "0.0.0.0",
		Port:      8080,
		TLSConfig: nil,
		Routers: &RouterGroup{
			Handlers: nil,
			basePath: "/",
			root:     true,
		},
		matcher:                   newServerMatcher(),
		convertFastRequestCtxFunc: convertToHTTPRequest,
		w:                         nil,
	}

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
	return nil
}

func (s *Server) FastHandler(ctx *fasthttp.RequestCtx) {
	r := s.convertFastRequestCtxFunc(ctx)
	s.ServeHTTP(s.w, r)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := s.ctxPool.Get().(*Context)
	c.Reset()
	c.Writer = w

	httpMethod := c.Request.Method
	rPath := c.Request.URL.Path
	matchResult := s.matcher.Match(httpMethod, rPath)
	if matchResult == nil {
		http.NotFound(w, r)
		return
	}
	if len(matchResult.Handlers) > 0 {
		c.handlers = matchResult.Handlers
	}
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
