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
}

func NewServer() *Server {
	s := &Server{}
	fastServer := &fasthttp.Server{
		Handler:   s.FastHandler,
		TLSConfig: s.TLSConfig,
	}
	s.server = fastServer
	return s
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) FastHandler(ctx *fasthttp.RequestCtx) {

}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {

}
