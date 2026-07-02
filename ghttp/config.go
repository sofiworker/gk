package ghttp

import (
	"net"
	"net/http"
)

// Config holds server configuration.
type Config struct {
	address          string
	router           Router
	renderer         Renderer
	validator        Validator
	logger           Logger
	envelope         EnvelopeFunc
	produces         string
	clientIPResolver ClientIPResolver
	openAPIEnabled   bool
	openAPITitle     string
	openAPIVersion   string
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

// WithRouter sets a custom router.
func WithRouter(router Router) ServerOption {
	return func(c *Config) {
		c.router = router
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

// WithProduces sets the default response Content-Type for automatic route encoding.
func WithProduces(contentType string) ServerOption {
	return func(c *Config) {
		c.produces = contentType
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

// WithClientIPResolver sets the client IP resolver used by Params snapshots.
func WithClientIPResolver(resolver ClientIPResolver) ServerOption {
	return func(c *Config) {
		c.clientIPResolver = resolver
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
