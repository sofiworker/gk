package ws

import (
	"net/http"
	"time"
)

// Option 配置 Upgrade 与升级后的 Conn。
// Option configures Upgrade and the resulting Conn.
type Option func(*config)

// HandlerFunc 处理收到的 ping/pong 控制帧，在读 goroutine 中执行；返回错误会终止读取。
// appData 只在调用期间有效，需要保留时请复制。
// HandlerFunc handles a received ping/pong control frame on the reading goroutine;
// returning an error stops reading. appData is only valid during the call; copy it to
// retain it.
type HandlerFunc func(c *Conn, appData []byte) error

// ErrorHandlerFunc 在握手失败时写出 HTTP 响应。status 是建议的状态码，err 是 *HandshakeError。
// ErrorHandlerFunc writes the HTTP response for a failed handshake. status is the
// suggested status code and err is a *HandshakeError.
type ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, status int, err error)

// config 是 Upgrade 与 Conn 的配置集合。
// config collects the Upgrade and Conn settings.
type config struct {
	subprotocols     []string
	checkOrigin      func(*http.Request) bool
	readLimit        int64
	readBufferSize   int
	writeBufferSize  int
	pingHandler      HandlerFunc
	pongHandler      HandlerFunc
	handshakeTimeout time.Duration
	controlTimeout   time.Duration
	responseHeader   http.Header
	errorHandler     ErrorHandlerFunc
}

// newConfig 应用选项并返回配置。
// newConfig applies options and returns the config.
func newConfig(opts []Option) *config {
	cfg := &config{
		readLimit:       DefaultReadLimit,
		readBufferSize:  DefaultReadBufferSize,
		writeBufferSize: DefaultWriteBufferSize,
		controlTimeout:  DefaultControlTimeout,
		checkOrigin:     SameOrigin,
		errorHandler:    defaultErrorHandler,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg
}

// WithSubprotocols 设置服务端支持的子协议，按服务端偏好顺序排列；握手时选出第一个
// 同时被客户端提供的子协议。
// WithSubprotocols sets the subprotocols supported by the server in server preference
// order; the handshake selects the first one also offered by the client.
func WithSubprotocols(protocols ...string) Option {
	return func(c *config) { c.subprotocols = append([]string(nil), protocols...) }
}

// WithCheckOrigin 覆盖 Origin 校验；返回 false 时握手以 403 失败。nil 表示恢复默认的 SameOrigin。
// WithCheckOrigin overrides the Origin check; returning false fails the handshake with
// 403. nil restores the default SameOrigin.
func WithCheckOrigin(fn func(*http.Request) bool) Option {
	return func(c *config) {
		if fn == nil {
			fn = SameOrigin
		}
		c.checkOrigin = fn
	}
}

// WithReadLimit 设置单条消息（分片重组后）的最大字节数，超限以 1009 关闭；n <= 0 时忽略。
// WithReadLimit sets the maximum size of a (reassembled) message; exceeding it closes
// with 1009. n <= 0 is ignored.
func WithReadLimit(n int64) Option {
	return func(c *config) {
		if n > 0 {
			c.readLimit = n
		}
	}
}

// WithReadBufferSize 设置读缓冲大小；n <= 0 时忽略。
// WithReadBufferSize sets the read buffer size; n <= 0 is ignored.
func WithReadBufferSize(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.readBufferSize = n
		}
	}
}

// WithWriteBufferSize 设置写缓冲大小；n <= 0 时忽略。
// WithWriteBufferSize sets the write buffer size; n <= 0 is ignored.
func WithWriteBufferSize(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.writeBufferSize = n
		}
	}
}

// WithPingHandler 覆盖收到 ping 时的处理（默认回复同负载的 pong）。覆盖后需自行回复 pong。
// WithPingHandler overrides ping handling (the default replies with a pong carrying the
// same payload). A custom handler must send the pong itself.
func WithPingHandler(fn HandlerFunc) Option {
	return func(c *config) { c.pingHandler = fn }
}

// WithPongHandler 设置收到 pong 时的处理（默认忽略），常用于刷新读超时。
// WithPongHandler sets pong handling (ignored by default), typically used to extend the
// read deadline.
func WithPongHandler(fn HandlerFunc) Option {
	return func(c *config) { c.pongHandler = fn }
}

// WithHandshakeTimeout 设置写出 101 响应的超时；0 表示不限制。
// WithHandshakeTimeout sets the timeout for writing the 101 response; 0 means none.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(c *config) {
		if d >= 0 {
			c.handshakeTimeout = d
		}
	}
}

// WithControlTimeout 设置自动回复 pong/close、协议错误关闭以及 Close 发送关闭帧的超时；
// d <= 0 时忽略。
// WithControlTimeout sets the timeout for automatic pong/close replies, protocol-error
// closes and the close frame sent by Close; d <= 0 is ignored.
func WithControlTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.controlTimeout = d
		}
	}
}

// WithResponseHeader 设置随 101 响应额外发送的头（如 Set-Cookie）。握手相关的头会被忽略。
// WithResponseHeader sets extra headers sent with the 101 response (e.g. Set-Cookie).
// Handshake headers are ignored.
func WithResponseHeader(h http.Header) Option {
	return func(c *config) { c.responseHeader = h.Clone() }
}

// WithErrorHandler 覆盖握手失败时的响应写出（默认写纯文本状态描述）；nil 表示恢复默认。
// WithErrorHandler overrides how a failed handshake response is written (plain-text
// status text by default); nil restores the default.
func WithErrorHandler(fn ErrorHandlerFunc) Option {
	return func(c *config) {
		if fn == nil {
			fn = defaultErrorHandler
		}
		c.errorHandler = fn
	}
}

// defaultErrorHandler 写出纯文本错误响应。
// defaultErrorHandler writes a plain-text error response.
func defaultErrorHandler(w http.ResponseWriter, _ *http.Request, status int, _ error) {
	http.Error(w, http.StatusText(status), status)
}
