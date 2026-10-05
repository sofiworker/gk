package ghttp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TimeoutOption 配置 Timeout 中间件。
// TimeoutOption configures the Timeout middleware.
type TimeoutOption func(*tmoConfig)

type tmoConfig struct {
	err error
}

// WithTimeoutError 设置超时时返回的自定义错误；nil 被忽略。
// WithTimeoutError sets a custom error returned on timeout; nil is ignored.
func WithTimeoutError(err error) TimeoutOption {
	return func(c *tmoConfig) {
		if err != nil {
			c.err = err
		}
	}
}

// Timeout 给请求 ctx 设置截止时间 d 并传给 next。
//
// 它在同一 goroutine 中执行 next，不会抢先写响应，因此不存在并发写响应的问题；
// 代价是 handler 必须自行尊重 ctx（把它传给数据库、HTTP 客户端等），否则无法被打断。
// next 返回后，若响应尚未写出且 ctx 已超时（或 next 返回的错误本身是
// context.DeadlineExceeded），则返回 503（ErrServiceUnavailable，Cause 包装
// context.DeadlineExceeded），由统一错误链写出。d <= 0 时透传。
//
// Timeout sets a deadline d on the request ctx and passes it to next. next runs in the
// same goroutine and the middleware never writes the response early, so there is no
// concurrent response writing; the handler MUST honor ctx (pass it to DB / HTTP clients),
// otherwise it cannot be interrupted. After next returns, if nothing was written and ctx
// timed out (or next's error is context.DeadlineExceeded), it returns 503
// (ErrServiceUnavailable whose Cause wraps context.DeadlineExceeded). d <= 0 passes through.
func Timeout(d time.Duration, opts ...TimeoutOption) Middleware {
	var cfg tmoConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	return func(next Handler) Handler {
		if d <= 0 {
			return next
		}
		return func(ctx context.Context, req *Request, resp *Response) error {
			tctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			prev := req.Raw
			req.Raw = prev.WithContext(tctx)
			err := next(tctx, req, resp)
			req.Raw = prev
			if resp.Written() {
				return err
			}
			if errors.Is(err, context.DeadlineExceeded) || (err == nil && tctx.Err() == context.DeadlineExceeded) {
				if cfg.err != nil {
					return cfg.err
				}
				he := ErrServiceUnavailable
				he.Cause = fmt.Errorf("ghttp: handler exceeded %v: %w", d, context.DeadlineExceeded)
				return he
			}
			return err
		}
	}
}
