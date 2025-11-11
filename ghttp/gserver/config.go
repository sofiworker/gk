package gserver

type ServerOption func(config *Config)

type Config struct {
	matcher    Match
	codec      *CodecFactory
	logger     Logger
	UseRawPath bool
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
