package ghttp

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// InFlightDefaultRetryAfter 是超限时 Retry-After 头的默认建议等待时间。
// InFlightDefaultRetryAfter is the default Retry-After hint sent when the limit is hit.
const InFlightDefaultRetryAfter = time.Second

// ErrOverloaded 表示并发已达上限（503）。按状态码匹配，因此 errors.Is(err, ErrServiceUnavailable) 同样成立。
// ErrOverloaded means the in-flight limit was reached (503). HTTPError matches by status, so
// errors.Is(err, ErrServiceUnavailable) also holds.
var ErrOverloaded = HTTPError{
	Status:  http.StatusServiceUnavailable,
	Message: "server overloaded",
}

// infConfig 是 MaxInFlight 的配置。
// infConfig is the MaxInFlight configuration.
type infConfig struct {
	wait       time.Duration
	retryAfter time.Duration
}

// InFlightOption 配置 MaxInFlight。
// InFlightOption configures MaxInFlight.
type InFlightOption func(*infConfig)

// WithInFlightWait 设置超限时最多排队等待的时长；<= 0 表示不排队、立即拒绝（默认）。
// WithInFlightWait sets how long an over-limit request may queue; <= 0 rejects immediately (default).
func WithInFlightWait(d time.Duration) InFlightOption {
	return func(c *infConfig) { c.wait = d }
}

// WithInFlightRetryAfter 设置拒绝响应的 Retry-After（向上取整到秒）；<= 0 表示不发送该头。默认 1s。
// WithInFlightRetryAfter sets the Retry-After of rejections (rounded up to seconds); <= 0 omits
// the header. Default 1s.
func WithInFlightRetryAfter(d time.Duration) InFlightOption {
	return func(c *infConfig) { c.retryAfter = d }
}

// MaxInFlight 返回限制同时处理请求数的中间件。超限请求立即（或在 WithInFlightWait 内排队后）
// 以 ErrOverloaded（503 + Retry-After）拒绝；排队期间 ctx 取消则返回 ctx.Err()。n <= 0 表示不限制。
// MaxInFlight returns middleware capping concurrently handled requests. Over-limit requests are
// rejected with ErrOverloaded (503 + Retry-After) immediately, or after queueing for at most
// WithInFlightWait; if ctx is cancelled while queueing, ctx.Err() is returned. n <= 0 disables it.
func MaxInFlight(n int, opts ...InFlightOption) Middleware {
	if n <= 0 {
		return func(next Handler) Handler { return next }
	}
	cfg := infConfig{retryAfter: InFlightDefaultRetryAfter}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	retry := ""
	if cfg.retryAfter > 0 {
		retry = strconv.FormatInt(int64((cfg.retryAfter+time.Second-1)/time.Second), 10)
	}
	sem := make(chan struct{}, n)

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			if err := infAcquire(ctx, sem, cfg.wait); err != nil {
				if err == ErrOverloaded && retry != "" {
					resp.Header().Set("Retry-After", retry)
				}
				return err
			}
			defer func() { <-sem }()
			return next(ctx, req, resp)
		}
	}
}

// infAcquire 获取一个并发名额；失败时返回 ErrOverloaded 或 ctx.Err()。
// infAcquire acquires a slot; on failure it returns ErrOverloaded or ctx.Err().
func infAcquire(ctx context.Context, sem chan struct{}, wait time.Duration) error {
	select {
	case sem <- struct{}{}:
		return nil
	default:
	}
	if wait <= 0 {
		return ErrOverloaded
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case sem <- struct{}{}:
		return nil
	case <-t.C:
		return ErrOverloaded
	case <-ctx.Done():
		return ctx.Err()
	}
}
