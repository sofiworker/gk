package ghttp

import (
	"context"
	"net/http"
	"strings"
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

			// deadline 必须同时挂到 req.Request 上。只把它作为闭包参数传下去是不够的：
			// raw 端点与中间件普遍用 req.Request.Context()（或 req.Context()）取上下文，
			// 过去它们看到的仍是无 deadline 的原 ctx，于是"带超时的下游"照样能无限阻塞，
			// 而 Timeout 已判定超时并写了 504——同一个请求里存在两个互相矛盾的上下文。
			// The deadline must also land on req.Request. Passing it only as the closure
			// argument is not enough: raw endpoints and middleware typically read
			// req.Request.Context() (or req.Context()), and those previously saw the
			// deadline-free parent, so a "timed-out" downstream could still block
			// forever while Timeout had already written a 504 — two contradictory
			// contexts within one request.
			orig := req.Request
			req.Request = orig.WithContext(ctx)
			// Requests 是池化对象，改写必须在返回前复原，否则下一个持有该对象的
			// 请求会拿到已被 cancel 的 deadline context。
			// Requests are pooled, so the mutation must be undone before returning;
			// otherwise the next holder of this object gets a cancelled deadline.
			defer func() { req.Request = orig }()

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
				// 下游可能同时带着自己的错误返回。过去这里无条件 return ErrRequestTimeout,
				// 把 cause 整个丢弃:排障时只剩"超时"二字,看不到真正失败的那一层。现在用
				// timeoutError 同时保住两者,并靠 StatusCoder 让 504 仍然优先
				// （分类表 StatusCoder 先于哨兵,否则 cause 是个 400 类错误时会顶掉超时状态）。
				// Downstream may return its own error too. The unconditional
				// `return ErrRequestTimeout` discarded that cause, leaving only "timed
				// out" for debugging with no sign of the layer that actually failed.
				// timeoutError keeps both, and relies on StatusCoder so 504 still wins
				// (the classifier checks StatusCoder before sentinels; otherwise a 400-ish
				// cause would override the timeout status).
				te := &timeoutError{message: message, cause: err}
				if err == nil {
					// 无 cause 时仍返回哨兵本身，保持既有 errors.Is/文案契约且零分配。
					// Without a cause, return the sentinel itself: same errors.Is and
					// text contract, no allocation.
					if message != "" {
						return &timeoutError{message: message}
					}
					return ErrRequestTimeout
				}
				return te
			}
			return err
		}
	}
}

// timeoutError 合并"请求超时"与下游 cause：对外仍是 504 且 errors.Is 命中
// ErrRequestTimeout，对内保留 cause 供钩子与日志排查。
// timeoutError merges "request timed out" with downstream cause: externally it is
// still a 504 matching ErrRequestTimeout via errors.Is, while the cause stays
// available to hooks and logs.
type timeoutError struct {
	message string // 可选说明 / optional detail
	cause   error  // 下游返回的错误，可为 nil / downstream error, may be nil
}

// Error 实现 error。
// Error implements error.
func (e *timeoutError) Error() string {
	var b strings.Builder
	b.WriteString(ErrRequestTimeout.Error())
	if e.message != "" {
		b.WriteString(": ")
		b.WriteString(e.message)
	}
	if e.cause != nil {
		b.WriteString(": ")
		b.WriteString(e.cause.Error())
	}
	return b.String()
}

// HTTPStatus 固定 504，使超时状态不被 cause 的分类顶掉。
// HTTPStatus pins 504 so the timeout status cannot be overridden by the cause's
// own classification.
func (e *timeoutError) HTTPStatus() int { return http.StatusGatewayTimeout }

// Unwrap 返回 [ErrRequestTimeout, cause]，使两个方向的 errors.Is 都成立。
// Unwrap returns [ErrRequestTimeout, cause] so errors.Is matches either side.
func (e *timeoutError) Unwrap() []error {
	if e.cause == nil {
		return []error{ErrRequestTimeout}
	}
	return []error{ErrRequestTimeout, e.cause}
}

var (
	_ error       = (*timeoutError)(nil)
	_ StatusCoder = (*timeoutError)(nil)
)
