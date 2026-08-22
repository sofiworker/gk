package ghttp

import (
	"context"
	"net/http"
	"time"
)

// ===========================================================================
// Timeout 中间件：为每个请求派生带 deadline 的 context，采用「协作式」超时。
//
// 为何是协作式而非 goroutine 抢占式：本包在 ServeHTTP 中用 sync.Pool 复用
// Request/Response，一旦中间件因超时提前返回，ServeHTTP 会 reset 并回收对象；
// 若此时仍有 handler goroutine 在后台持有旧 Request/Response，将发生 use-after-
// pool 数据竞争与跨请求串写。因此这里只注入 deadline context，同步执行下游，由
// 下游监听 ctx.Done() 主动退出（阻塞型操作应以 ctx 为第一参数）。当下游返回时若因
// 超时且尚未写响应，则补写 503。
//
// Cooperative, not goroutine-preemptive: this package pools Request/Response in
// ServeHTTP, so returning early on timeout would let ServeHTTP reset/recycle the
// objects while a background handler goroutine still holds the old ones — a
// use-after-pool race and cross-request corruption. So we only inject a deadline
// context and run downstream synchronously; downstream must watch ctx.Done() and
// return early (blocking ops should take ctx as the first argument). If it
// returns past the deadline without having written a response, we write 503.
// ===========================================================================

// Timeout 返回协作式请求超时中间件：为下游注入 d deadline 的 context；下游超时返回
// 且未提交响应时补写 503。d<=0 时透传（不启用超时）。
// Timeout returns a cooperative request-timeout middleware: it injects a context
// with a d deadline for downstream; if downstream returns past the deadline
// without committing a response, it writes 503. With d<=0 it passes through.
func Timeout(d time.Duration) Middleware {
	return TimeoutWithMessage(d, "503 request timeout")
}

// TimeoutWithMessage 用自定义超时响应体构造协作式超时中间件，状态码固定 503。
// TimeoutWithMessage builds a cooperative timeout middleware with a custom
// timeout body; the status code is fixed at 503.
func TimeoutWithMessage(d time.Duration, message string) Middleware {
	if d <= 0 {
		return func(next Handler) Handler { return next }
	}
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()

			err := next(ctx, req, resp)

			// 下游已提交响应：无论是否超时都不再改写，尊重下游已写出的内容。
			// Downstream committed: never overwrite, whether or not it timed out.
			if resp.Written() {
				return err
			}
			// 未提交且 deadline 已过：判定为超时，补写 503。
			// Not committed and the deadline has passed: treat as timeout, write 503.
			if ctx.Err() == context.DeadlineExceeded {
				http.Error(resp.ResponseWriter, message, http.StatusServiceUnavailable)
				return nil
			}
			return err
		}
	}
}
