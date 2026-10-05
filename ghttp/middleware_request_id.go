package ghttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// HeaderRequestID 是默认的请求 ID 头名称。
// HeaderRequestID is the default request ID header name.
const HeaderRequestID = "X-Request-ID"

// ridMaxLen 是入站请求 ID 允许的最大长度。
// ridMaxLen is the maximum accepted length of an inbound request ID.
const ridMaxLen = 128

// ridKey 是 context 中保存请求 ID 的键类型。
// ridKey is the context key type holding the request ID.
type ridKey struct{}

// RequestIDOption 配置 RequestID 中间件。
// RequestIDOption configures the RequestID middleware.
type RequestIDOption func(*ridConfig)

type ridConfig struct {
	header    string
	generator func() string
}

// WithRequestIDHeader 设置读取与回写请求 ID 的头名称，空串被忽略。
// WithRequestIDHeader sets the header used to read and echo the request ID; empty is ignored.
func WithRequestIDHeader(name string) RequestIDOption {
	return func(c *ridConfig) {
		if name != "" {
			c.header = name
		}
	}
}

// WithRequestIDGenerator 设置自定义 ID 生成函数，nil 被忽略。
// WithRequestIDGenerator sets a custom ID generator; nil is ignored.
func WithRequestIDGenerator(fn func() string) RequestIDOption {
	return func(c *ridConfig) {
		if fn != nil {
			c.generator = fn
		}
	}
}

// RequestIDFrom 返回 ctx 中的请求 ID；不存在时返回空串。
// RequestIDFrom returns the request ID stored in ctx, or "" when absent.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ridKey{}).(string)
	return id
}

// RequestID 返回请求 ID 中间件：合法的入站 ID 被沿用，否则生成新的；
// ID 写入响应头与 context（同时更新 req.Raw）。
// 合法 ID：长度 1..128，且仅含可见 ASCII 安全字符（字母数字及 -_.:/+=~）。
// RequestID returns a request ID middleware. A valid inbound ID is reused, otherwise a
// new one is generated; the ID goes to the response header and the context (req.Raw is
// updated too). Valid: length 1..128 and only alphanumerics plus -_.:/+=~.
func RequestID(opts ...RequestIDOption) Middleware {
	cfg := ridConfig{header: HeaderRequestID, generator: ridGenerate}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			id := req.Raw.Header.Get(cfg.header)
			if !ridValid(id) {
				id = cfg.generator()
			}
			if !resp.Written() {
				resp.Header().Set(cfg.header, id)
			}
			ctx = context.WithValue(ctx, ridKey{}, id)
			req.Raw = req.Raw.WithContext(context.WithValue(req.Raw.Context(), ridKey{}, id))
			return next(ctx, req, resp)
		}
	}
}

// ridGenerate 生成 16 字节随机数的 hex 表示。
// ridGenerate returns the hex of 16 random bytes.
func ridGenerate() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // Go 1.24+ 不会失败 / never fails since Go 1.24
	return hex.EncodeToString(b[:])
}

// ridValid 判断入站请求 ID 是否安全可沿用。
// ridValid reports whether an inbound request ID is safe to reuse.
func ridValid(id string) bool {
	if id == "" || len(id) > ridMaxLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == ':', c == '/', c == '+', c == '=', c == '~':
		default:
			return false
		}
	}
	return true
}
