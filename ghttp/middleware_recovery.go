package ghttp

import (
	"context"
	"fmt"
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
		// panic 值与方法/路径都可能含用户输入（某库对坏字段直接 panic、方法名来自请求行），
		// 未净化时一个 %0a 就能把这行崩溃记录劈成两行并伪造出后续条目，事后无从分辨。
		// 堆栈不净化：它本就多行，且只由 runtime 生成、不含请求原文。
		// The panic value and the method/path can both carry user input (a library
		// panicking on a bad field, a method from the request line); unescaped, a single
		// %0a splits this crash record and forges the entries after it, leaving no way to
		// tell them apart later. The stack is left as-is: it is multi-line by nature and
		// produced by the runtime, never echoing the request.
		log.Printf("ghttp: panic recovered %s %s: %s\n%s",
			sanitizeLogToken(info.Method), sanitizeLogToken(info.Path), sanitizeLogToken(fmt.Sprint(info.Value)), info.Stack)
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
				if rec == http.ErrAbortHandler {
					// 标准库约定:panic(http.ErrAbortHandler) 表示"静默放弃这个响应",
					// net/http 据此断连且不打日志。它不是故障,既不该记 panic 日志,也不该
					// 写 500 覆盖调用方的意图,故原样向上传播。
					// Stdlib contract: panic(http.ErrAbortHandler) means "abandon this
					// response silently"; net/http drops the connection without logging.
					// It is not a failure, so neither log it as a panic nor write a 500
					// over the caller's intent — re-panic as-is.
					panic(rec)
				}
				if onPanic != nil {
					onPanic(PanicInfo{
						Value:  rec,
						Stack:  debug.Stack(),
						Method: req.Method,
						Path:   req.URL.Path,
					})
				}
				// 一律上抛 panicError，绝不在本中间件里手写响应：交给统一错误链才能与
				// 其余 5xx 同形（JSON + code）、触发 onError、被 Logger 与 Metrics 记为
				// 500。此前这里用 http.Error 写 text/plain 并 return nil，是包内最后一处
				// 绕过错误链的出口；而"响应已提交"也不是咽掉 panic 的理由——流式场景
				// （SSE、大文件、已 Flush）里崩溃的请求此前在错误钩子、访问日志与 5xx
				// 指标中彻底消失，恰恰是最需要告警的一类事故。上抛后 writeError 遇到已
				// 提交只会如实上报而不会改写响应。
				// Always propagate panicError and never hand-write a response here: only
				// the unified error chain matches the shape of other 5xx (JSON with a
				// code), fires onError, and lets Logger and Metrics record a 500. Writing
				// text/plain via http.Error and returning nil was the last exit in this
				// package that bypassed the chain — and an already-committed response is no
				// reason to swallow the panic: requests that crashed mid-stream (SSE, large
				// files, after a Flush) vanished from the error hook, the access log and the
				// 5xx metrics, which is exactly the class worth alerting on. Once propagated,
				// writeError reports it without rewriting a committed response.
				err = panicError(rec)
			}()
			return next(ctx, req, resp)
		}
	}
}
