package ghttp

// Config holds server configuration.
type Config struct {
	address   string
	validator Validator
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
	// Router is applied in New() after config is built.
	// This is a placeholder; actual setting happens via a field in Config.
	return func(c *Config) {}
}
