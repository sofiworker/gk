package ghttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
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
			if !isValidRequestID(id) {
				id = newRequestID()
			}
			resp.Header().Set(HeaderRequestID, id)
			ctx = context.WithValue(ctx, requestIDKey{}, id)
			return next(ctx, req, resp)
		}
	}
}

// isValidRequestID 报告客户端提供的请求 ID 是否可原样回显与记录。
//
// 只放行 [0-9A-Za-z._-]:此前仅限长度,于是客户端可注入任意字符。请求 ID 同时进入响应头
// 与访问日志,含换行的值可伪造日志行(日志注入),含控制字符的值会污染下游日志管道;由于
// 它还被回显进响应头,值里的 CR/LF 更是响应头注入的经典载体。字符集不合法时一律改用自
// 生成的 ID 而非报错——请求 ID 只是关联标识,不值得为它拒绝一个请求。
// isValidRequestID reports whether a client-supplied request ID is safe to echo and log.
//
// Only [0-9A-Za-z._-] passes: the previous check bounded length alone, so a client could
// inject arbitrary characters. A request ID reaches both the response header and the
// access log, so a value containing newlines can forge log lines (log injection) and
// control characters pollute downstream log pipelines; since it is also echoed into a
// response header, CR/LF inside it is the classic response-header-injection vector. An
// invalid charset falls back to a generated ID rather than an error: a request ID is only
// a correlation handle and not worth rejecting a request over.
func isValidRequestID(id string) bool {
	if id == "" || len(id) > DefaultMaxRequestIDLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= '0' && c <= '9', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
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
		// 净化后再落日志：路径已在路由层拦掉控制字符，但 Method/ClientIP 等仍可能来自
		// 客户端可影响的输入，任何裸换行都会把一条访问记录劈成两行、伪造出多余记录。
		// Sanitize before writing: paths already have control characters refused at the
		// route layer, but Method/ClientIP and friends remain client-influenced, and any
		// bare newline splits one access record in two, forging an extra entry.
		log.Printf("ghttp %s %s -> %d (%s) %dB ip=%s",
			sanitizeLogToken(a.Method), sanitizeLogToken(route), a.Status, a.Elapsed, a.BytesOut, sanitizeLogToken(a.ClientIP))
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

			// 记账放进 defer,使 handler panic 时这条访问日志不会整条丢失——被拒绝或崩溃
			// 的请求恰恰是最需要留下记录的。
			// Accounting goes in a defer so a handler panic does not lose the access log
			// entirely — rejected or crashing requests are exactly the ones worth recording.
			var err error
			completed := false
			defer func() {
				status := resp.Status()
				if status == 0 {
					// 状态码为 0 意味着响应还没提交:错误渲染发生在中间件链【之外】
					// (writeError 是链外的单一出口),所以 Logger 运行时错误响应尚未写出。
					// 此前一律按 200 占位,导致 CSRF 403、BasicAuth 401、业务 400 在访问
					// 日志里全部记成 200,而 404/405 因在链内写入却是对的——同一日志流部分
					// 对部分错,比全错更难发现。这里用与 metrics 相同的方式从 error 推断
					// 终态码,让日志与客户端实际收到的状态一致。
					// A zero status means the response is not committed yet: error
					// rendering happens OUTSIDE the middleware chain (writeError is the
					// single exit beyond it), so no error response exists while Logger
					// runs. Defaulting to 200 previously logged CSRF 403, BasicAuth 401
					// and business 400 all as 200, while 404/405 were correct because
					// they are written inside the chain — a log stream partly right and
					// partly wrong is harder to notice than one that is always wrong.
					// Infer the final status from the error the same way metrics does, so
					// the log matches what the client actually received.
					switch {
					case err != nil:
						status = HTTPStatus(err)
					case !completed:
						// panic 正在向上传播,响应最终被兜底 recover 写成 500。
						// A panic is propagating; the safety-net recover writes 500.
						status = http.StatusInternalServerError
					default:
						status = http.StatusOK
					}
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
			}()

			err = next(ctx, req, resp)
			completed = true
			return err
		}
	}
}
