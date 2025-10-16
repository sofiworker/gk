package gserver

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"net/url"
	"sync"

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

	httpMethod := c.Request.Method
	rPath := c.Request.URL.Path
	matchResult := s.matcher.Match(httpMethod, rPath)
	if matchResult == nil {
		http.NotFound(w, r)
		return
	}

	for _, h := range matchResult.Handlers {
		h(c)
	}

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
