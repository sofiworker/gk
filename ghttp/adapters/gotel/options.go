package ghttpotel

// config 是 Tracing/Metrics 共用的配置。
// config is shared by Tracing and Metrics.
type config struct {
	traceIDHeader string
	serverAddress string
	bodySizes     bool
	activeReqs    bool
}

// Option 配置 Tracing / Metrics。
// Option configures Tracing / Metrics.
type Option func(*config)

func newConfig(opts []Option) config {
	c := config{bodySizes: true, activeReqs: true}
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// WithTraceIDHeader 让 Tracing 把 trace id 写入指定响应头；空串表示不写（默认）。
// WithTraceIDHeader makes Tracing write the trace id to the named response header;
// empty (default) disables it.
func WithTraceIDHeader(name string) Option {
	return func(c *config) { c.traceIDHeader = name }
}

// WithServerAddress 固定 server.address 属性；默认取请求 Host（去端口并净化）。
// WithServerAddress pins the server.address attribute; by default it is derived from the
// request Host (port stripped, sanitized).
func WithServerAddress(addr string) Option {
	return func(c *config) { c.serverAddress = addr }
}

// WithBodySizeMetrics 开关请求/响应体大小指标（默认开启，仅 Metrics 使用）。
// WithBodySizeMetrics toggles request/response body size metrics (default on, Metrics only).
func WithBodySizeMetrics(enabled bool) Option {
	return func(c *config) { c.bodySizes = enabled }
}

// WithActiveRequests 开关 http.server.active_requests 指标（默认开启，仅 Metrics 使用）。
// WithActiveRequests toggles the http.server.active_requests metric (default on, Metrics only).
func WithActiveRequests(enabled bool) Option {
	return func(c *config) { c.activeReqs = enabled }
}
