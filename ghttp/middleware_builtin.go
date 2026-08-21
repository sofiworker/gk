package ghttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"
)

// requestIDKey 是 X-Request-ID 在 context 中的私有键类型,避免与其它包碰撞。
// requestIDKey is the private context key type for X-Request-ID, avoiding
// collisions with other packages.
type requestIDKey struct{}

// HeaderRequestID 是承载请求 ID 的响应/请求头名。
// HeaderRequestID is the header name carrying the request ID.
const HeaderRequestID = "X-Request-ID"

// DefaultMaxRequestIDLength 是回显客户端请求 ID 的默认长度上限;超限则替换为新 ID。
// DefaultMaxRequestIDLength is the default length cap for echoing a client
// request ID; an over-length value is replaced by a fresh ID.
const DefaultMaxRequestIDLength = 128

// RequestID 是前处理型中间件:为每个请求确定一个请求 ID(优先回显合法的客户端
// X-Request-ID,否则生成),写入响应头,并经 context 传给下游。它演示"前处理 +
// context 传值"——取代被删的 *Ctx store。
// RequestID is a pre-processing middleware: it determines a request ID per
// request (echoing a valid client X-Request-ID, else generating one), writes it
// to the response header, and passes it downstream via context. It demonstrates
// "pre-processing + context value passing" — replacing the removed *Ctx store.
func RequestID() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			id := req.Header.Get(HeaderRequestID)
			if id == "" || len(id) > DefaultMaxRequestIDLength {
				id = newRequestID()
			}
			resp.Header().Set(HeaderRequestID, id)
			ctx = context.WithValue(ctx, requestIDKey{}, id)
			return next(ctx, req, resp)
		}
	}
}

// RequestIDFromContext 返回 RequestID 中间件注入的请求 ID;不存在时返回空串。
// RequestIDFromContext returns the request ID injected by the RequestID
// middleware, or "" if absent.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// newRequestID 生成 16 字节随机 ID 的十六进制串。
// newRequestID generates a hex string of 16 random bytes.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败极罕见;退化为时间戳,保证仍有非空 ID。
		// crypto/rand failure is very rare; fall back to a timestamp to still
		// yield a non-empty ID.
		return "ts-" + time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// Logger 是后处理型中间件:调用 next 后读取 Response 的最终状态码与耗时并记录。它演示
// "后处理 + 读 status"——依赖本轮为 Response 增加的 status/written 追踪。log 目标可注入。
// Logger is a post-processing middleware: after calling next it reads the
// Response's final status code and elapsed time and logs them. It demonstrates
// "post-processing + reading status" — relying on this stage's status/written
// tracking added to Response. The log sink is injectable.
func Logger() Middleware {
	return LoggerWith(func(method, path string, status int, elapsed time.Duration) {
		log.Printf("ghttp %s %s -> %d (%s)", method, path, status, elapsed)
	})
}

// LoggerWith 用自定义记录函数构造 Logger 中间件,便于测试与替换日志后端。
// LoggerWith builds a Logger middleware with a custom record function, easing
// testing and log-backend replacement.
func LoggerWith(record func(method, path string, status int, elapsed time.Duration)) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			start := time.Now()
			err := next(ctx, req, resp)
			status := resp.Status()
			if status == 0 {
				// 下游既未写状态码也未写 body(如仅返回 error 待错误链处理):按 200
				// 记录占位,真实码由阶段 4 错误链落定。
				// Downstream wrote neither status nor body (e.g. returned an
				// error for the error chain): record a 200 placeholder; the real
				// code is settled by stage 4's error chain.
				status = 200
			}
			record(req.Method, req.URL.Path, status, time.Since(start))
			return err
		}
	}
}
