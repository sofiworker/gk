package ghttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrCrossOriginRequest 标记被 CSRF 中间件拒绝的跨源请求。标准库的对应错误未导出，
// 因此由本包提供可判断的哨兵；错误链中仍保留标准库的原始错误。
// ErrCrossOriginRequest marks a cross-origin request rejected by the CSRF middleware. The
// stdlib's errors are unexported, so this package provides a matchable sentinel; the
// original stdlib error stays in the chain.
var ErrCrossOriginRequest = errors.New("ghttp: cross-origin request rejected")

// csrfConfig 是 CSRF 的配置。
// csrfConfig is the CSRF configuration.
type csrfConfig struct {
	origins []string
	bypass  func(*Request) bool
}

// CSRFOption 配置 CSRF。
// CSRFOption configures CSRF.
type CSRFOption func(*csrfConfig)

// WithCSRFTrustedOrigins 添加允许跨源发起非安全请求的受信 origin（形如 https://example.com）。
// 非法 origin 在 CSRF 构造期返回错误。
// WithCSRFTrustedOrigins adds trusted origins (like https://example.com) allowed to make
// cross-origin unsafe requests. Invalid origins make CSRF return an error at construction.
func WithCSRFTrustedOrigins(origins ...string) CSRFOption {
	return func(c *csrfConfig) { c.origins = append(c.origins, origins...) }
}

// WithCSRFBypassFunc 设置绕过判定函数，返回 true 的请求不做跨源检查（如 webhook）。
// 标准库的 bypass 基于 ServeMux 模式，与本包路由器不一致，因此改用函数。
// WithCSRFBypassFunc sets a bypass predicate; requests for which it returns true skip the
// cross-origin check (e.g. webhooks). The stdlib bypass is ServeMux-pattern based, which
// does not match this package's router, hence a function is used instead.
func WithCSRFBypassFunc(fn func(*Request) bool) CSRFOption {
	return func(c *csrfConfig) { c.bypass = fn }
}

// CSRF 返回基于 http.CrossOriginProtection 的跨站请求伪造防护中间件。
// 被拒绝的请求返回 403：errors.Is(err, ErrForbidden) 与 errors.Is(err, ErrCrossOriginRequest)
// 都成立，Cause 中保留标准库错误。
// CSRF returns a cross-site request forgery protection middleware built on
// http.CrossOriginProtection. Rejected requests yield a 403 (errors.Is(err, ErrForbidden)
// and errors.Is(err, ErrCrossOriginRequest) hold) whose Cause keeps the stdlib error.
func CSRF(opts ...CSRFOption) (Middleware, error) {
	cfg := &csrfConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	cop := http.NewCrossOriginProtection()
	for _, o := range cfg.origins {
		if err := cop.AddTrustedOrigin(o); err != nil {
			return nil, fmt.Errorf("ghttp: csrf trusted origin %q: %w", o, err)
		}
	}
	bypass := cfg.bypass
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			if bypass != nil && bypass(req) {
				return next(ctx, req, resp)
			}
			if err := cop.Check(req.Raw); err != nil {
				return HTTPError{Status: http.StatusForbidden, Message: ErrForbidden.Message, Cause: fmt.Errorf("%w: %w", ErrCrossOriginRequest, err)}
			}
			return next(ctx, req, resp)
		}
	}, nil
}
