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

// AccessLog 是一次请求的可观测快照,传给日志 sink。相比原始四元组,它补充了命中路由
// 模板(低基数,适合聚合)、客户端 IP、响应体字节数与下游返回的 error。
// AccessLog is an observability snapshot of one request handed to the log sink.
// Beyond the original tuple it adds the matched route template (low-cardinality,
// aggregation-friendly), the client IP, the response byte count, and the error
// returned downstream.
type AccessLog struct {
	Method   string        // 请求方法 / request method
	Path     string        // 原始请求 path(高基数)/ raw request path (high cardinality)
	Route    string        // 命中的路由模板(低基数,可能为空)/ matched route template (low cardinality, may be empty)
	Status   int           // 最终状态码 / final status code
	Elapsed  time.Duration // 处理耗时 / handling duration
	ClientIP string        // 客户端 IP(依可信代理策略解析)/ client IP (per trusted-proxy policy)
	BytesOut int           // 写出的响应体字节数 / response body bytes written
	Err      error         // 下游返回的 error(可能为 nil)/ error returned downstream (may be nil)
}

// Logger 是后处理型中间件:调用 next 后把请求的结构化快照 AccessLog 交给 sink 记录。
// 默认实现打印到标准 log。要自定义结构化字段/后端,用 LoggerWith。
// Logger is a post-processing middleware: after calling next it hands a
// structured AccessLog snapshot to a sink. The default prints to the standard
// log. Use LoggerWith to customize the structured fields/backend.
func Logger() Middleware {
	return LoggerWith(func(a AccessLog) {
		route := a.Route
		if route == "" {
			route = a.Path
		}
		log.Printf("ghttp %s %s -> %d (%s) %dB ip=%s", a.Method, route, a.Status, a.Elapsed, a.BytesOut, a.ClientIP)
	})
}

// LoggerWith 用自定义记录函数构造 Logger 中间件,便于测试与替换日志后端。record 收到
// 一个完整的 AccessLog 值(按值传递,sink 不得持有其内部引用做异步复用)。
// LoggerWith builds a Logger middleware with a custom record function, easing
// testing and log-backend replacement. record receives a full AccessLog value
// (passed by value; the sink must not retain internal references for async use).
func LoggerWith(record func(AccessLog)) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			start := time.Now()
			err := next(ctx, req, resp)
			status := resp.Status()
			if status == 0 {
				// 下游既未写状态码也未写 body(如仅返回 error 交错误链处理):此处按
				// 200 占位。装了 Logger 的场景下若需真实错误码,应把 Logger 放在错误
				// 链之后或用 WithErrorHook 观测。
				// Downstream wrote neither status nor body (e.g. returned an error
				// for the error chain): record a 200 placeholder here. For the real
				// error code, place Logger after the error chain or observe via
				// WithErrorHook.
				status = 200
			}
			record(AccessLog{
				Method:   req.Method,
				Path:     req.URL.Path,
				Route:    req.MatchedRoute(),
				Status:   status,
				Elapsed:  time.Since(start),
				ClientIP: req.ClientIP(),
				BytesOut: resp.BytesOut(),
				Err:      err,
			})
			return err
		}
	}
}
