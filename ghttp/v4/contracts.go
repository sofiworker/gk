// Package v4 实现基于 Accessor 模式的类型安全 HTTP 框架。
// Package v4 implements a type-safe HTTP framework based on the Accessor pattern.
package v4

import (
	"context"
	"net/http"
	"sync"

	ghttp "github.com/sofiworker/gk/ghttp"
)

// Request 是 ghttp.Request 的别名。
// Request is an alias for ghttp.Request.
type Request = ghttp.Request

// Response 是 ghttp.Response 的别名。
// Response is an alias for ghttp.Response.
type Response = ghttp.Response

// Params 是 ghttp.Params 的别名。
// Params is an alias for ghttp.Params.
type Params = ghttp.Params

// Handler 是 v4 的核心处理器签名。
// Handler is the core handler signature for v4.
//
// 与 v1 的 func(ctx, *Request, *Response) error 不同，
// v4 使用泛型参数 In 和返回值 Out 实现类型安全。
//
// Unlike v1's func(ctx, *Request, *Response) error,
// v4 uses generic parameters In and return value Out for type safety.
type Handler[In, Out any] func(context.Context, In) (Out, error)

// Accessor 是延迟访问的抽象接口。
// Accessor is the abstraction for lazy access.
//
// Accessor 持有原始 Request，在首次调用 Get() 时才提取值。
// 后续调用返回缓存值，避免重复解析。
//
// Accessor holds the raw Request and extracts the value only on the first Get() call.
// Subsequent calls return the cached value, avoiding redundant parsing.
type Accessor[T any] interface {
	// Get 获取值（首次调用时才提取）。
	// Get retrieves the value (extraction happens on first call).
	Get() (T, error)

	// MustGet 获取值，失败时 panic（用于已验证场景）。
	// MustGet retrieves the value, panics on error (for validated scenarios).
	MustGet() T

	// Has 检查是否有值（不触发提取）。
	// Has checks if a value is present (does not trigger extraction).
	Has() bool
}

// PathAccessor 提供路径参数的延迟访问。
// PathAccessor provides lazy access to path parameters.
//
// 使用注册期编译的解析器，性能优于运行时反射。
// Uses registration-time compiled parsers for better performance than runtime reflection.
type PathAccessor[T any] struct {
	req    *Request
	cached *T
	err    error
	once   sync.Once
}

// QueryAccessor 提供查询参数的延迟访问。
// QueryAccessor provides lazy access to query parameters.
type QueryAccessor[T any] struct {
	req    *Request
	cached *T
	err    error
	once   sync.Once
}

// JsonAccessor 提供 JSON body 的延迟访问。
// JsonAccessor provides lazy access to JSON body.
type JsonAccessor[T any] struct {
	req    *Request
	cached *T
	err    error
	once   sync.Once
}

// FormAccessor 提供表单数据的延迟访问。
// FormAccessor provides lazy access to form data.
type FormAccessor[T any] struct {
	req    *Request
	cached *T
	err    error
	once   sync.Once
}

// HeaderAccessor 提供 HTTP 头的延迟访问。
// HeaderAccessor provides lazy access to HTTP headers.
type HeaderAccessor[T any] struct {
	req    *Request
	cached *T
	err    error
	once   sync.Once
}

// EmptyPath 表示无路径参数。
// EmptyPath indicates no path parameters.
type EmptyPath struct{}

// EmptyQuery 表示无查询参数。
// EmptyQuery indicates no query parameters.
type EmptyQuery struct{}

// EmptyBody 表示无请求体。
// EmptyBody indicates no request body.
type EmptyBody struct{}

// HTTPError 表示 HTTP 错误响应。
// HTTPError represents an HTTP error response.
type HTTPError struct {
	Status  int
	Message string
}

func (e HTTPError) Error() string {
	return e.Message
}

// 常见 HTTP 错误构造器。
// Common HTTP error constructors.

func BadRequest(msg string) HTTPError {
	return HTTPError{Status: http.StatusBadRequest, Message: msg}
}

func Unauthorized(msg string) HTTPError {
	return HTTPError{Status: http.StatusUnauthorized, Message: msg}
}

func Forbidden(msg string) HTTPError {
	return HTTPError{Status: http.StatusForbidden, Message: msg}
}

func NotFound(msg string) HTTPError {
	return HTTPError{Status: http.StatusNotFound, Message: msg}
}

func InternalServerError(msg string) HTTPError {
	return HTTPError{Status: http.StatusInternalServerError, Message: msg}
}
