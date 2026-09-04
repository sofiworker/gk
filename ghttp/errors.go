package ghttp

import (
	"errors"
	"fmt"
	"net/http"
)

// 注册期错误(骨架阶段先声明哨兵,后续阶段填充校验逻辑)。
// Registration-time errors (sentinels declared now; validation logic lands in
// later stages).
var (
	// ErrEmptyPath 表示注册路径为空或不以 "/" 开头。
	// ErrEmptyPath indicates an empty path or one not starting with "/".
	ErrEmptyPath = errors.New("ghttp: path must start with '/'")

	// ErrDuplicateRoute 表示同一 method + path 被重复注册。
	// ErrDuplicateRoute indicates the same method + path was registered twice.
	ErrDuplicateRoute = errors.New("ghttp: duplicate route registration")

	// ErrInvalidParam 表示参数名非法或重复。
	// ErrInvalidParam indicates an illegal or duplicated parameter name.
	ErrInvalidParam = errors.New("ghttp: invalid path parameter")

	// ErrCatchAllPosition 表示 catch-all 段不在路径末尾。
	// ErrCatchAllPosition indicates a catch-all segment is not the last one.
	ErrCatchAllPosition = errors.New("ghttp: catch-all must be the final segment")

	// ErrMissingOutput 表示 typed 端点未声明输出契约。
	// ErrMissingOutput indicates a typed endpoint declared no output contract.
	ErrMissingOutput = errors.New("ghttp: endpoint declares no output spec")

	// ErrMissingCodec 表示 Body 来源未提供 codec。
	// ErrMissingCodec indicates a Body source was given no codec.
	ErrMissingCodec = errors.New("ghttp: body source requires a codec")
)

// 请求期错误(骨架阶段先声明,后续接入统一错误链)。
// Request-time errors (declared now; wired into the unified error chain later).
var (
	// ErrInvalidRequestPath 表示请求路径非法(空段 / dot 段 / 非法转义 /
	// strict 下的尾斜杠),对应 400。
	// ErrInvalidRequestPath indicates an invalid request path (empty / dot
	// segment, invalid escape, or a trailing slash under strict), mapping to 400.
	ErrInvalidRequestPath = errors.New("ghttp: invalid request path")

	// ErrInvalidInput 表示输入解析失败(路径参数类型不符 / 请求体解码失败等),
	// 对应 400。阶段 4 接入统一错误链后据此映射状态码。
	// ErrInvalidInput indicates input parsing failure (path param type mismatch,
	// body decode failure, etc.), mapping to 400. Stage 4's unified error chain
	// maps the status from it.
	ErrInvalidInput = errors.New("ghttp: invalid input")
)

// 运行期/服务错误。
// Runtime/server errors.
var (
	// ErrNotReady 表示就绪门闸尚未置为就绪,由 Ready 探针据此返回 503。
	// ErrNotReady indicates a readiness gate is not yet ready; the Ready probe
	// returns 503 based on it.
	ErrNotReady = errors.New("ghttp: not ready")

	// ErrShuttingDown 是 RunGraceful 收到停止信号后置就绪门闸的不就绪原因,使 Ready
	// 探针返回 503,让负载均衡器摘流。
	// ErrShuttingDown is the not-ready cause RunGraceful sets on the readiness gate
	// after a stop signal, so the Ready probe returns 503 and the load balancer
	// drains traffic.
	ErrShuttingDown = errors.New("ghttp: shutting down")

	// ErrServerClosed 是 Serve 系方法在正常关闭后返回的哨兵,等价于
	// http.ErrServerClosed。导出以供调用方 errors.Is 判断"正常关闭"而非异常。
	// ErrServerClosed is the sentinel returned by the Serve family after a clean
	// shutdown; it aliases http.ErrServerClosed. Exported so callers can errors.Is
	// it to distinguish a normal close from a failure.
	ErrServerClosed = http.ErrServerClosed

	// ErrServerStarted 表示 Server 已启动,再次启动(重复启动)失败。
	// ErrServerStarted indicates the Server was already started, so starting it
	// again fails.
	ErrServerStarted = errors.New("ghttp: server already started")

	// ErrServerNotStartable 表示 Server 已关闭(经 Shutdown 或 Close),不可再启动。
	// 与 ErrServerClosed 区分:后者表示一次正常关闭的【结果】,本错误表示生命周期
	// 阶段错误——试图复用一个已终结的 Server。
	// ErrServerNotStartable indicates the Server is already closed (via Shutdown or
	// Close) and cannot be started again. Distinct from ErrServerClosed: the latter
	// is the OUTCOME of a clean close, while this is a lifecycle error — attempting
	// to reuse a terminated Server.
	ErrServerNotStartable = errors.New("ghttp: server already closed")

	// ErrRegistrationAfterStart 表示在服务已开始接收请求后尝试注册端点，被拒绝。
	// 路由树是无锁读的结构，运行期写会与匹配路径构成数据竞争（race detector 直接判
	// 死），因此宁可显式拒绝，也不留"多数时候能用、压测时随机崩"的陷阱。确需热注册请
	// 显式走 COW 或另建实例。
	// ErrRegistrationAfterStart refuses an endpoint registration after the server
	// began accepting requests. The route tree is read lock-free, so a runtime write
	// races with matching (the race detector rightly fails). Refusing explicitly beats
	// a "works until load-tested" trap; use copy-on-write or a second instance if hot
	// registration is genuinely required.
	ErrRegistrationAfterStart = errors.New("ghttp: cannot register after the server started serving")

	// ErrTLSConfig 表示 TLS 配置不足:RunTLS/ServeTLS 既未注入含证书的
	// TLSConfig,也未提供 certFile/keyFile。调用方可经 errors.Is 判定 TLS 校验失败。
	// ErrTLSConfig indicates insufficient TLS configuration: RunTLS/ServeTLS was
	// given neither a TLSConfig with certificates nor certFile/keyFile. Callers can
	// errors.Is it to detect a TLS-validation failure.
	ErrTLSConfig = errors.New("ghttp: TLS requires certFile and keyFile, or a TLSConfig")

	// ErrHandlerPanic 是无 Recovery 中间件时,最外层兜底 recover 把 handler panic
	// 收敛成的错误(经统一错误链写 500)。调用方可经 errors.Is 区分 panic 与普通错误。
	// ErrHandlerPanic is the error the outermost safety-net recover collapses a
	// handler panic into when no Recovery middleware is present (written as 500 via
	// the unified error chain). Callers can errors.Is it to distinguish a panic from
	// an ordinary error.
	ErrHandlerPanic = errors.New("ghttp: handler panicked")

	// ErrUnsupportedMediaType 表示请求的 Content-Type 与端点声明的
	// RequestDecoder.ContentType() 不符,对应 415。由统一错误链据此映射状态码。
	// ErrUnsupportedMediaType indicates the request Content-Type does not match
	// the endpoint's declared RequestDecoder.ContentType(), mapping to 415. The
	// unified error chain maps the status from it.
	ErrUnsupportedMediaType = errors.New("ghttp: unsupported media type")

	// ErrRequestEntityTooLarge 表示请求体超过 LimitBody 配置的上限,对应 413。
	// ErrRequestEntityTooLarge indicates the body exceeds the LimitBody cap,
	// mapping to 413.
	ErrRequestEntityTooLarge = errors.New("ghttp: request entity too large")

	// ErrRateLimitExceeded 表示请求因 RateLimit 中间件被拒绝,对应 429 Too Many Requests。
	// 调用方可经 errors.Is 判定,自定义错误渲染器亦可据此区分限流与其它 4xx。
	// ErrRateLimitExceeded indicates the request was rejected by the RateLimit middleware,
	// mapping to 429 Too Many Requests. Callers can errors.Is it, and a custom error renderer
	// can distinguish throttling from other 4xx cases.
	ErrRateLimitExceeded = errors.New("ghttp: rate limit exceeded")

	// ErrCSRFTokenInvalid 表示 CSRF 校验失败(token 缺失、不匹配或来源不可信),对应 403。
	// 统一用一个哨兵而不区分具体原因:向客户端区分"缺 token"与"token 错"会泄露防护细节。
	// ErrCSRFTokenInvalid indicates CSRF verification failed (token missing, mismatched,
	// or untrusted origin), mapping to 403. A single sentinel covers every cause on
	// purpose: telling a client "missing" from "mismatched" would leak protection detail.
	ErrCSRFTokenInvalid = errors.New("ghttp: CSRF verification failed")

	// ErrNotHijackable 表示底层 http.ResponseWriter 不支持连接接管(不实现
	// http.Hijacker),因此无法进行 WebSocket 升级等需要夺取原始连接的操作。
	// 常见于被不透传 Hijack 的中间件包裹、或运行在不支持 hijack 的服务器上。
	// ErrNotHijackable indicates the underlying http.ResponseWriter does not
	// support connection takeover (it does not implement http.Hijacker), so
	// operations needing the raw connection such as a WebSocket upgrade cannot
	// proceed. Typically caused by a middleware that does not pass Hijack through,
	// or a server that does not support hijacking.
	ErrNotHijackable = errors.New("ghttp: response writer does not support hijacking")

	// ErrRequestTimeout 表示请求处理超过了 Timeout 中间件设定的时限,对应 504。
	// 选 504 而非 503:503 表示"整个服务不可用",而这里是【单个请求】超时,上游/服务本身
	// 仍然健康;504 Gateway Timeout 才准确表达"我等下游等超时了"。调用方可 errors.Is 它
	// 来区分超时与其它失败,并在 WithErrorHook 中打点告警。
	// ErrRequestTimeout indicates request handling exceeded the Timeout middleware's
	// limit, mapping to 504.
	// 504 rather than 503: 503 says "the whole service is unavailable", whereas this is
	// a SINGLE request timing out while the service itself stays healthy; 504 Gateway
	// Timeout accurately expresses "I waited for the downstream and it timed out".
	// Callers can errors.Is it to tell a timeout from other failures and alert on it via
	// WithErrorHook.
	ErrRequestTimeout = errors.New("ghttp: request handling timed out")
)

// panicErr 把 recover 到的 panic 值包成错误,同时保持 errors.Is(err, ErrHandlerPanic)
// 成立。它让 onError 钩子与日志能拿到真正的 panic 原因:此前兜底 recover 直接丢弃该值
// (`_ = rec`),不挂 Recovery 中间件时排障只剩一句 "handler panicked",既无原因也无
// 类型,是生产排障黑洞。
// panicErr wraps a recovered panic value into an error while keeping
// errors.Is(err, ErrHandlerPanic) true. It lets the onError hook and logs see the real
// panic cause: the safety-net recover previously discarded that value (`_ = rec`), so
// without a Recovery middleware debugging was left with just "handler panicked" — no
// cause, no type — a production blind spot.
type panicErr struct {
	// value 是 recover() 的原始返回值。
	// value is the raw recover() return value.
	value any
}

// Error 实现 error。
// Error implements error.
func (e *panicErr) Error() string {
	return fmt.Sprintf("%s: %v", ErrHandlerPanic.Error(), e.value)
}

// Is 让 errors.Is(err, ErrHandlerPanic) 对包装后的错误仍然成立。
// Is keeps errors.Is(err, ErrHandlerPanic) true for the wrapped error.
func (e *panicErr) Is(target error) bool { return target == ErrHandlerPanic }

// PanicValue 返回被包装的 panic 值,供调用方类型断言原始原因。
// PanicValue returns the wrapped panic value so callers can type-assert the cause.
func (e *panicErr) PanicValue() any { return e.value }

// panicError 构造 panicErr;value 为 nil 时退回裸哨兵。
// panicError builds a panicErr; a nil value falls back to the bare sentinel.
func panicError(value any) error {
	if value == nil {
		return ErrHandlerPanic
	}
	return &panicErr{value: value}
}

// PanicValueOf 从错误链中提取 panic 原始值。err 非 panic 错误时返回 (nil, false)。
// 供 onError 钩子与日志记录真正的 panic 原因。
// PanicValueOf extracts the original panic value from an error chain, returning
// (nil, false) when err is not a panic error. Intended for onError hooks and logs to
// record the real panic cause.
func PanicValueOf(err error) (any, bool) {
	var pe *panicErr
	if errors.As(err, &pe) {
		return pe.value, true
	}
	return nil, false
}
