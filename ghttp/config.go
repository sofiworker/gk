package ghttp

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"time"
)

// DefaultMaxBodyBytes is the default maximum size for automatically decoded request bodies.
const DefaultMaxBodyBytes int64 = 4 << 20

// Config holds server configuration.
type Config struct {
	address           string
	renderer          Renderer
	validator         Validator
	logger            Logger
	envelope          EnvelopeFunc
	errorHandler      ErrorHandler
	bodyDecoder       BodyDecodeFunc
	produces          []string
	consumes          []string
	clientIPResolver  ClientIPResolver
	vfsPath           string
	strictRouting     bool
	openAPIEnabled    bool
	openAPITitle      string
	openAPIVersion    string
	openAPIPath       string
	openAPIPathSet    bool
	readTimeout       time.Duration
	readHeaderTimeout time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	maxHeaderBytes    int
	maxBodyBytes      int64
	tlsConfig         *tls.Config
	baseContext       func(net.Listener) context.Context
	connContext       func(context.Context, net.Conn) context.Context
	errorLog          *log.Logger
}

// ClientIPResolver resolves a client IP from an HTTP request.
type ClientIPResolver func(*http.Request) string

// ServerOption configures a Server.
type ServerOption func(*Config)

// WithAddress sets the server listen address.
func WithAddress(addr string) ServerOption {
	return func(c *Config) {
		c.address = addr
	}
}

// WithValidator sets a custom validator.
func WithValidator(v Validator) ServerOption {
	return func(c *Config) {
		c.validator = v
	}
}

// WithRenderer sets the template renderer.
func WithRenderer(renderer Renderer) ServerOption {
	return func(c *Config) {
		c.renderer = renderer
	}
}

// WithLogger sets the server logger used by built-in logging middleware.
func WithLogger(logger Logger) ServerOption {
	return func(c *Config) {
		c.logger = logger
	}
}

// WithEnvelope sets a custom envelope function.
func WithEnvelope(fn EnvelopeFunc) ServerOption {
	return func(c *Config) {
		c.envelope = fn
	}
}

// WithErrorHandler replaces the default framework error response writer.
func WithErrorHandler(handler ErrorHandler) ServerOption {
	return func(c *Config) {
		c.errorHandler = handler
	}
}

// WithBodyDecoder sets a custom request body decoder for this server.
func WithBodyDecoder(fn BodyDecodeFunc) ServerOption {
	return func(c *Config) {
		c.bodyDecoder = fn
	}
}

// WithProduces sets the default response Content-Types for automatic route encoding.
func WithProduces(contentTypes ...string) ServerOption {
	return func(c *Config) {
		c.produces = normalizeContentTypes(contentTypes)
	}
}

// WithConsumes sets the default request Content-Types for automatic body decoding.
func WithConsumes(contentTypes ...string) ServerOption {
	return func(c *Config) {
		c.consumes = normalizeContentTypes(contentTypes)
	}
}

// WithStrictRouting makes trailing slashes part of route identity.
func WithStrictRouting() ServerOption {
	return func(c *Config) {
		c.strictRouting = true
	}
}

// WithOpenAPI sets the OpenAPI document title and version.
func WithOpenAPI(title, version string) ServerOption {
	return func(c *Config) {
		c.openAPIEnabled = true
		c.openAPITitle = title
		c.openAPIVersion = version
	}
}

// WithOpenAPIPath sets the HTTP endpoint that exposes the OpenAPI document.
// An empty path disables HTTP exposure without disabling Server.OpenAPI.
func WithOpenAPIPath(path string) ServerOption {
	return func(c *Config) {
		c.openAPIPath = path
		c.openAPIPathSet = true
	}
}

// WithClientIPResolver sets the client IP resolver used by Params snapshots.
func WithClientIPResolver(resolver ClientIPResolver) ServerOption {
	return func(c *Config) {
		c.clientIPResolver = resolver
	}
}

// WithVFSPath sets the default safe static-file root for ToStatic().
func WithVFSPath(root string) ServerOption {
	return func(c *Config) {
		c.vfsPath = root
	}
}

// WithReadTimeout sets the maximum duration for reading the entire request.
func WithReadTimeout(timeout time.Duration) ServerOption {
	return func(c *Config) {
		c.readTimeout = timeout
	}
}

// WithReadHeaderTimeout sets the maximum duration for reading request headers.
func WithReadHeaderTimeout(timeout time.Duration) ServerOption {
	return func(c *Config) {
		c.readHeaderTimeout = timeout
	}
}

// WithWriteTimeout sets the maximum duration before timing out response writes.
func WithWriteTimeout(timeout time.Duration) ServerOption {
	return func(c *Config) {
		c.writeTimeout = timeout
	}
}

// WithIdleTimeout sets the maximum time to wait for the next request.
func WithIdleTimeout(timeout time.Duration) ServerOption {
	return func(c *Config) {
		c.idleTimeout = timeout
	}
}

// WithMaxHeaderBytes sets the maximum size of request headers.
func WithMaxHeaderBytes(n int) ServerOption {
	return func(c *Config) {
		c.maxHeaderBytes = n
	}
}

// WithMaxBodyBytes sets the maximum size of automatically decoded request bodies.
// Values less than or equal to zero disable the request body size limit.
func WithMaxBodyBytes(n int64) ServerOption {
	return func(c *Config) {
		c.maxBodyBytes = n
	}
}

// WithTLSConfig sets the TLS configuration used by TLS serving methods.
func WithTLSConfig(tlsConfig *tls.Config) ServerOption {
	return func(c *Config) {
		c.tlsConfig = tlsConfig
	}
}

// WithBaseContext sets the base context for accepted connections.
func WithBaseContext(fn func(net.Listener) context.Context) ServerOption {
	return func(c *Config) {
		c.baseContext = fn
	}
}

// WithConnContext sets the context for each accepted connection.
func WithConnContext(fn func(context.Context, net.Conn) context.Context) ServerOption {
	return func(c *Config) {
		c.connContext = fn
	}
}

// WithErrorLog sets the logger used by the underlying http.Server.
func WithErrorLog(logger *log.Logger) ServerOption {
	return func(c *Config) {
		c.errorLog = logger
	}
}

func defaultClientIPResolver(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
