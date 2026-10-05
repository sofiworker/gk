package ghttp

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime/debug"

	"github.com/sofiworker/gk/ghttp/wire"
)

// Recovery 返回一个恢复 panic 的中间件。
// Recovery returns a middleware that recovers from panics.
//
// 当处理器发生 panic 时，捕获并返回 500 Internal Server Error。
// When a handler panics, it catches the panic and returns 500 Internal Server Error.
func Recovery() Middleware {
	return RecoveryWithWriter(os.Stderr)
}

// RecoveryWithWriter 返回一个恢复 panic 的中间件，将日志写入指定的 writer。
// RecoveryWithWriter returns a middleware that recovers from panics and writes logs to the specified writer.
func RecoveryWithWriter(out io.Writer) Middleware {
	return RecoveryWithHandler(out, nil)
}

// RecoveryWithHandler 返回一个恢复 panic 的中间件，支持自定义处理函数。
// RecoveryWithHandler returns a middleware that recovers from panics with a custom handler.
func RecoveryWithHandler(out io.Writer, handler func(context.Context, *Request, *Response, any)) Middleware {
	var logger *log.Logger
	if out != nil {
		logger = log.New(out, "[Recovery] ", log.LstdFlags)
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					// http.ErrAbortHandler 是 net/http 约定的"静默中止"信号，原样继续传播
					// http.ErrAbortHandler is net/http's "abort silently" signal; keep propagating it
					if rec == http.ErrAbortHandler {
						panic(rec)
					}

					// 记录堆栈信息
					// Log stack trace
					if logger != nil {
						stack := debug.Stack()
						// panic 值可能含客户端输入，单行化后再写入；堆栈本身是多行的可信内容
						// The panic value may carry client input, so keep it single-line; the stack is trusted multi-line output
						logger.Printf("panic recovered: %s\n%s", wire.LogToken(fmt.Sprint(rec)), stack)
					}

					// 如果已经写入响应，不再处理
					// If response already written, don't handle
					if resp.written {
						return
					}

					// 自定义处理器
					// Custom handler
					if handler != nil {
						handler(ctx, req, resp, rec)
						return
					}

					// 默认转为 500 错误交给统一错误链，响应体与其他错误一致，panic 值不外泄
					// By default turn it into a 500 for the error chain, so the body matches
					// other errors and the panic value never leaks
					err = HTTPError{
						Status:  http.StatusInternalServerError,
						Message: ErrInternalServerError.Message,
						Cause:   fmt.Errorf("ghttp: panic recovered: %v", rec),
					}
				}
			}()

			err = next(ctx, req, resp)
			return
		}
	}
}
