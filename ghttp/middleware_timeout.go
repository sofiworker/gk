package ghttp

import (
	"context"
	"fmt"
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
// 超时且尚未写响应，则返回 ErrRequestTimeout 交统一错误链渲染为 504。
//
// Cooperative, not goroutine-preemptive: this package pools Request/Response in
// ServeHTTP, so returning early on timeout would let ServeHTTP reset/recycle the
// objects while a background handler goroutine still holds the old ones — a
// use-after-pool race and cross-request corruption. So we only inject a deadline
// context and run downstream synchronously; downstream must watch ctx.Done() and
// return early (blocking ops should take ctx as the first argument). If it
// returns past the deadline without having written a response, it returns
// ErrRequestTimeout for the unified error chain to render as 504.
// ===========================================================================

// Timeout 返回协作式请求超时中间件：为下游注入 d deadline 的 context；下游超时返回
// 且未提交响应时返回 ErrRequestTimeout（统一错误链渲染为 504 + 统一 JSON 错误体，且
// onError 钩子可观测）。d<=0 时透传（不启用超时）。
// Timeout returns a cooperative request-timeout middleware: it injects a context
// with a d deadline for downstream; if downstream returns past the deadline without
// committing a response, it returns ErrRequestTimeout (rendered by the unified error
// chain as 504 with the unified JSON error body, and observable by the onError hook).
// With d<=0 it passes through.
func Timeout(d time.Duration) Middleware {
	return TimeoutWithMessage(d, "")
}

// TimeoutWithMessage 用自定义超时说明构造协作式超时中间件，状态码固定 504。
// message 非空时会包进 ErrRequestTimeout 的错误信息（仅在 WithExposeErrorDetails
// 开启时才出现在响应体中，默认脱敏）。
// TimeoutWithMessage builds a cooperative timeout middleware with custom timeout
// detail; the status code is fixed at 504. A non-empty message is wrapped into the
// ErrRequestTimeout error text (which reaches the response body only when
// WithExposeErrorDetails is on; sanitized by default).
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
			// 未提交且 deadline 已过：判定为超时,交统一错误链渲染。
			//
			// 此前这里直接 http.Error 写 503 再 return nil,有三个问题:超时事件对
			// onError 钩子与外层中间件完全不可见(无法打点告警);503 语义错误(那表示
			// 整个服务不可用,而这是单个请求超时);text/plain 响应体与本框架统一的 JSON
			// 错误体不一致,客户端要为超时单独写一套解析。返回哨兵错误则三者一并解决:
			// 状态码 504、统一 JSON 体、钩子可观测。
			// Not committed and the deadline has passed: treat as a timeout and let the
			// unified error chain render it.
			//
			// This previously wrote 503 via http.Error and returned nil, which had three
			// problems: the timeout was invisible to the onError hook and outer
			// middleware (no way to alert on it); 503 was the wrong semantic (it means
			// the whole service is down, but this is one request timing out); and the
			// text/plain body was inconsistent with this framework's unified JSON error
			// body, forcing clients to special-case timeouts. Returning a sentinel fixes
			// all three at once: status 504, unified JSON body, observable by hooks.
			if ctx.Err() == context.DeadlineExceeded {
				if message != "" {
					return fmt.Errorf("%w: %s", ErrRequestTimeout, message)
				}
				return ErrRequestTimeout
			}
			return err
		}
	}
}
