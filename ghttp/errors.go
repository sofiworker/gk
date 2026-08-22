package ghttp

import (
	"errors"
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

	// ErrMissingRequired 表示标记为必填的 query/header 字段在请求中缺失,对应 400。
	// 与 ErrInvalidInput 分开,便于用户侧与阶段 4 错误链区分"缺失"与"类型不符"。
	// ErrMissingRequired indicates a query/header field marked required was
	// absent in the request, mapping to 400. Kept separate from ErrInvalidInput
	// so the caller and stage 4's error chain can distinguish "missing" from
	// "type mismatch".
	ErrMissingRequired = errors.New("ghttp: missing required field")
)

// 运行期/服务错误。
// Runtime/server errors.
var (
	// ErrNotReady 表示就绪门闸尚未置为就绪,由 Ready 探针据此返回 503。
	// ErrNotReady indicates a readiness gate is not yet ready; the Ready probe
	// returns 503 based on it.
	ErrNotReady = errors.New("ghttp: not ready")

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

	// ErrEngineNotStarted 表示未先经 Engine.Run/RunTLS 启动就调用 Engine.Shutdown。
	// ErrEngineNotStarted indicates Engine.Shutdown was called before the engine was
	// started via Engine.Run/RunTLS.
	ErrEngineNotStarted = errors.New("ghttp: engine not started via Run/RunTLS")

	// ErrTLSConfig 表示 TLS 配置不足:ServeTLS/ListenAndServeTLS 既未注入含证书的
	// TLSConfig,也未提供 certFile/keyFile。调用方可经 errors.Is 判定 TLS 校验失败。
	// ErrTLSConfig indicates insufficient TLS configuration: ServeTLS/
	// ListenAndServeTLS was given neither a TLSConfig with certificates nor
	// certFile/keyFile. Callers can errors.Is it to detect a TLS-validation failure.
	ErrTLSConfig = errors.New("ghttp: TLS requires certFile and keyFile, or a TLSConfig")

	// ErrHandlerPanic 是无 Recovery 中间件时,最外层兜底 recover 把 handler panic
	// 收敛成的错误(经统一错误链写 500)。调用方可经 errors.Is 区分 panic 与普通错误。
	// ErrHandlerPanic is the error the outermost safety-net recover collapses a
	// handler panic into when no Recovery middleware is present (written as 500 via
	// the unified error chain). Callers can errors.Is it to distinguish a panic from
	// an ordinary error.
	ErrHandlerPanic = errors.New("ghttp: handler panicked")
)
