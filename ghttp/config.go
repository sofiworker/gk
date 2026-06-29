package ghttp

// Config holds server configuration.
type Config struct {
	address        string
	router         Router
	validator      Validator
	openAPIEnabled bool
	openAPITitle   string
	openAPIVersion string
}

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

// WithOpenAPI sets the OpenAPI document title and version.
func WithOpenAPI(title, version string) ServerOption {
	return func(c *Config) {
		c.openAPIEnabled = true
		c.openAPITitle = title
		c.openAPIVersion = version
	}
}
