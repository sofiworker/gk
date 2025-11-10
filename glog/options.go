package glog

// Option 是一个函数，用于修改 glog 的配置。
type Option func(*Config)

// WithLevel 设置日志级别。
func WithLevel(level Level) Option {
	return func(c *Config) {
		c.Level = level
	}
}

// WithOutputPaths 设置日志的输出路径。
func WithOutputPaths(paths ...string) Option {
	return func(c *Config) {
		c.FilePaths = paths
	}
}

// WithEncoding 设置日志的编码格式 (json/console)。
func WithEncoding(encoding Encoding) Option {
	return func(c *Config) {
		c.Encoding = encoding
		// 切换到控制台时，通常也希望开启开发模式
		if encoding == ConsoleEncoding {
			c.Development = true
		}
	}
}

// WithDevelopment 切换开发模式。
func WithDevelopment(isDev bool) Option {
	return func(c *Config) {
		c.Development = isDev
	}
}

// WithRotation 启用并配置日志轮转。
func WithRotation(maxSize, maxAge, maxBackups int, compress, localTime bool) Option {
	return func(c *Config) {
		c.RotationConfig = &RotationConfig{
			MaxSize:    maxSize,
			MaxAge:     maxAge,
			MaxBackups: maxBackups,
			Compress:   compress,
			LocalTime:  localTime,
		}
	}
}

// WithInitialFields 添加默认字段到所有日志中。
func WithInitialFields(fields map[string]interface{}) Option {
	return func(c *Config) {
		if c.InitialFields == nil {
			c.InitialFields = make(map[string]interface{})
		}
		for k, v := range fields {
			c.InitialFields[k] = v
		}
	}
}

// WithDisableCaller 禁用调用者信息。
func WithDisableCaller(disabled bool) Option {
	return func(c *Config) {
		c.DisableCaller = disabled
	}
}

// WithDisableStacktrace 禁用错误级别以上的堆栈跟踪。
func WithDisableStacktrace(disabled bool) Option {
	return func(c *Config) {
		c.DisableStacktrace = disabled
	}
}

// --- EncoderConfig Options ---

// WithMessageKey 设置日志消息的字段名。
func WithMessageKey(key string) Option {
	return func(c *Config) {
		c.EncoderConfig.MessageKey = key
	}
}

// WithLevelKey 设置日志级别的字段名。
func WithLevelKey(key string) Option {
	return func(c *Config) {
		c.EncoderConfig.LevelKey = key
	}
}

// WithTimeKey 设置时间戳的字段名。
func WithTimeKey(key string) Option {
	return func(c *Config) {
		c.EncoderConfig.TimeKey = key
	}
}

// WithCallerKey 设置调用者信息的字段名。
func WithCallerKey(key string) Option {
	return func(c *Config) {
		c.EncoderConfig.CallerKey = key
	}
}

// WithStacktraceKey 设置堆栈跟踪的字段名。
func WithStacktraceKey(key string) Option {
	return func(c *Config) {
		c.EncoderConfig.StacktraceKey = key
	}
}

func WithTimeFormat(format string) Option {
	return func(c *Config) {
		c.TimeFormat = format
	}
}
