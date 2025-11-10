package gserver

type ServerOption func(config *Config)

type Config struct {
	matcher                   Match
	codec                     *CodecFactory
	logger                    Logger
	UseRawPath                bool
	convertFastRequestCtxFunc RequestConverter
	convertFailedHandler      RequestConverterFailedHandler
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

func WithRequestConverter(converter RequestConverter) ServerOption {
	return func(c *Config) {
		if converter != nil {
			c.convertFastRequestCtxFunc = converter
		}
	}
}

func WithRequestConvertFailedHandler(handler RequestConverterFailedHandler) ServerOption {
	return func(c *Config) {
		if handler != nil {
			c.convertFailedHandler = handler
		}
	}
}

func WithUseRawPath(use bool) ServerOption {
	return func(c *Config) {
		c.UseRawPath = use
	}
}
