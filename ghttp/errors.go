package ghttp

import "errors"

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
)

// 请求期错误(骨架阶段先声明,后续接入统一错误链)。
// Request-time errors (declared now; wired into the unified error chain later).
var (
	// ErrInvalidRequestPath 表示请求路径非法(空段 / dot 段 / 非法转义 /
	// strict 下的尾斜杠),对应 400。
	// ErrInvalidRequestPath indicates an invalid request path (empty / dot
	// segment, invalid escape, or a trailing slash under strict), mapping to 400.
	ErrInvalidRequestPath = errors.New("ghttp: invalid request path")
)
