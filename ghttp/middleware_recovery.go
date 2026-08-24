package ghttp

import (
	"context"
	"log"
	"net/http"
	"runtime/debug"
)

// ===========================================================================
// Recovery 中间件：捕获下游 handler 的 panic，写规范的 500，并记录堆栈。
// 生产要点：客户端只收到通用 500（不泄露内部细节/堆栈），堆栈仅进服务端日志。
// Recovery middleware: catches a downstream handler panic, writes a canonical
// 500, and logs the stack. Production stance: the client receives only a generic
// 500 (no internal details/stack leaked); the stack goes to the server log only.
// ===========================================================================

// PanicInfo 承载一次被捕获的 panic 的上下文，交给 RecoveryHandler 决策。
// PanicInfo carries the context of one recovered panic for a RecoveryHandler.
type PanicInfo struct {
	Value  any    // recover() 返回值 / the recover() value
	Stack  []byte // 堆栈快照 / stack snapshot
	Method string
	Path   string
}

// RecoveryHandler 在捕获 panic 后被调用，用于自定义日志/上报。它不负责写响应，
// 响应由中间件统一写 500（除非下游已提交）。
// RecoveryHandler is invoked after a panic is recovered, for custom logging/
// reporting. It does not write the response; the middleware writes the 500
// uniformly (unless the downstream already committed).
type RecoveryHandler func(info PanicInfo)

// Recovery 返回默认 Recovery 中间件：捕获 panic → 记录堆栈到标准日志 → 写 500。
// Recovery returns the default Recovery middleware: catch panic → log stack to
// the standard logger → write 500.
func Recovery() Middleware {
	return RecoveryWith(func(info PanicInfo) {
		log.Printf("ghttp: panic recovered %s %s: %v\n%s", info.Method, info.Path, info.Value, info.Stack)
	})
}

// RecoveryWith 用自定义 RecoveryHandler 构造 Recovery 中间件，便于注入结构化日志或
// 错误上报（Sentry 等）。onPanic 为 nil 时不额外记录（仍写 500）。
// RecoveryWith builds a Recovery middleware with a custom RecoveryHandler, easing
// injection of structured logging or error reporting (e.g. Sentry). A nil
// onPanic skips extra logging (the 500 is still written).
func RecoveryWith(onPanic RecoveryHandler) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) (err error) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if onPanic != nil {
					onPanic(PanicInfo{
						Value:  rec,
						Stack:  debug.Stack(),
						Method: req.Method,
						Path:   req.URL.Path,
					})
				}
				// 仅在下游尚未提交响应时写 500，避免破坏已写出的部分响应。
				// Write 500 only if the downstream hasn't committed, to avoid
				// corrupting an already-written partial response.
				if !resp.Written() {
					http.Error(resp, "500 internal server error", http.StatusInternalServerError)
				}
				// panic 已被处理，返回 nil，不再向上冒泡到核心错误路径重复写响应。
				// The panic is handled; return nil so it does not bubble to the
				// core error path and double-write.
				err = nil
			}()
			return next(ctx, req, resp)
		}
	}
}
