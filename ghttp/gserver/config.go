package gserver

import "time"

type ServerOption func(config *Config)

type Config struct {
	matcher    Match
	codec      *CodecFactory
	logger     Logger
	UseRawPath bool
	render     Render

	// The fields below are all fasthttp, so we will expose them
	Concurrency                   int
	ReadBufferSize                int
	WriteBufferSize               int
	ReadTimeout                   time.Duration
	WriteTimeout                  time.Duration
	IdleTimeout                   time.Duration
	MaxConnsPerIP                 int
	MaxRequestsPerConn            int
	MaxIdleWorkerDuration         time.Duration
	TCPKeepalivePeriod            time.Duration
	MaxRequestBodySize            int
	DisableKeepalive              bool
	TCPKeepalive                  bool
	ReduceMemoryUsage             bool
	GetOnly                       bool
	DisablePreParseMultipartForm  bool
	DisableHeaderNamesNormalizing bool
	NoDefaultServerHeader         bool
	NoDefaultDate                 bool
	NoDefaultContentType          bool
	KeepHijackedConns             bool
	CloseOnShutdown               bool
	StreamRequestBody             bool
}

func WithMatcher(m Match) ServerOption {
	return func(c *Config) {
		if m != nil {
			c.matcher = m
		}
	}
}

func WithCodec(codec *CodecFactory) ServerOption {
	return func(c *Config) {
		if codec != nil {
			c.codec = codec
		}
	}
}

func WithLogger(logger Logger) ServerOption {
	return func(c *Config) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func WithUseRawPath(use bool) ServerOption {
	return func(c *Config) {
		c.UseRawPath = use
	}
}

func WithRender(r Render) ServerOption {
	return func(c *Config) {
		if r != nil {
			c.render = r
		}
	}
}
