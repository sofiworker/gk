package gserver

import (
	"crypto/tls"
	"net/http"

	"github.com/valyala/fasthttp"
)

type Server struct {
	Addr string
	Port int

	server *fasthttp.Server

	TLSConfig *tls.Config

	RouterGroup

	matcher Matcher
}

func NewServer() *Server {
	s := &Server{
		RouterGroup: RouterGroup{
			Handlers: nil,
			basePath: "/",
			root:     true,
		},
		matcher: newServerMatcher(),
	}
	s.engine = s

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
	r := new(http.Request)
	s.ServeHTTP(nil, r)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	matchResult := s.matcher.Match("", "")
	if matchResult == nil {
		http.NotFound(w, r)
		return
	}

	for _, h := range matchResult.Handlers {
		h.Handle(&Context{})
	}
}
