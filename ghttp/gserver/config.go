package gserver

import (
	"crypto/tls"

	"github.com/sofiworker/gk/ghttp/codec"
	"github.com/valyala/fasthttp"
)

type ServerOption func(*Server)

func WithAddress(addr string) ServerOption {
	return func(s *Server) {
		if addr != "" {
			s.Addr = addr
		}
	}
}

func WithPort(port int) ServerOption {
	return func(s *Server) {
		if port > 0 {
			s.Port = port
		}
	}
}

func WithTLSConfig(cfg *tls.Config) ServerOption {
	return func(s *Server) {
		s.TLSConfig = cfg
		if s.server != nil {
			s.server.TLSConfig = cfg
		}
	}
}

func WithMatcher(m Match) ServerOption {
	return func(s *Server) {
		if m != nil {
			s.Match = m
		}
	}
}

func WithCodecManager(manager *codec.CodecManager) ServerOption {
	return func(s *Server) {
		if manager != nil {
			s.codecManager = manager
		}
	}
}

func WithLogger(logger Logger) ServerOption {
	return func(s *Server) {
		if logger != nil {
			s.logger = logger
		}
	}
}

func WithFastHTTPServer(server *fasthttp.Server) ServerOption {
	return func(s *Server) {
		if server != nil {
			s.server = server
		}
	}
}

func WithRequestConverter(converter RequestConverter) ServerOption {
	return func(s *Server) {
		if converter != nil {
			s.convertFastRequestCtxFunc = converter
		}
	}
}
