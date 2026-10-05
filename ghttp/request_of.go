package ghttp

import (
	"context"
	"sync"
)

// RequestOf 是类型化的请求包装，组合请求视图和 lazy body 解码。
// RequestOf is a typed request wrapper that combines request view and lazy body decoding.
//
// T 为 Body 类型，默认按 JSON 解码。
// T is the Body type, decoded as JSON by default.
type RequestOf[T BodyConstraint] struct {
	// RequestInput 提供对 path、query、header、cookie 的按需访问
	// RequestInput provides on-demand access to path, query, header, cookie
	RequestInput

	// lazyBody 封装 lazy body 解码逻辑
	// lazyBody encapsulates lazy body decoding logic
	lazyBody *lazyBodyData[T]
}

// RequestInput 是面向 handler 的请求视图。
// RequestInput is the request view for handlers.
//
// 提供对 path、query、header、cookie 的类型安全访问。
// Provides type-safe access to path, query, header, cookie.
type RequestInput struct {
	// req 是底层请求上下文
	// req is the underlying request context
	req *Request
}

// PathValue 返回路径参数的类型安全访问器。
// PathValue returns a type-safe accessor for path parameters.
func (in RequestInput) PathValue(name string) *Value {
	return PathValue(in.req, name)
}

// QueryValue 返回查询参数的类型安全访问器（单值）。
// QueryValue returns a type-safe accessor for query parameters (single value).
func (in RequestInput) QueryValue(name string) *Value {
	return QueryValue(in.req, name)
}

// QueryValues 返回查询参数的多值访问器。
// QueryValues returns a multi-value accessor for query parameters.
func (in RequestInput) QueryValues(name string) *Values {
	return QueryValues(in.req, name)
}

// HeaderValue 返回 HTTP 头的类型安全访问器。
// HeaderValue returns a type-safe accessor for HTTP headers.
func (in RequestInput) HeaderValue(name string) *Value {
	return HeaderValue(in.req, name)
}

// CookieValue 返回 Cookie 的类型安全访问器。
// CookieValue returns a type-safe accessor for cookies.
func (in RequestInput) CookieValue(name string) *Value {
	return CookieValue(in.req, name)
}

// lazyBodyData 封装 lazy body 解码逻辑。
// lazyBodyData encapsulates lazy body decoding logic.
//
// 使用 sync.Once 确保并发安全和单次解码。
// Uses sync.Once to ensure concurrency safety and single decoding.
type lazyBodyData[T BodyConstraint] struct {
	data        T
	decodeOnce  sync.Once
	decodeError error
	decoder     func(context.Context, *Request) (T, error)
	req         *Request
}

// Data 返回请求体数据，首次调用时解码并缓存。
// Data returns the request body data, decoding and caching on first call.
//
// 后续调用直接返回缓存结果，不重复解码。
// Subsequent calls return the cached result without re-decoding.
func (r *RequestOf[T]) Data(ctx context.Context) (T, error) {
	if r.lazyBody == nil {
		var zero T
		return zero, nil
	}
	r.lazyBody.decodeOnce.Do(func() {
		if r.lazyBody.decoder != nil {
			r.lazyBody.data, r.lazyBody.decodeError = r.lazyBody.decoder(ctx, r.lazyBody.req)
		}
	})
	return r.lazyBody.data, r.lazyBody.decodeError
}

// NewRequestOf 创建类型化请求包装，body 默认按 JSONInput 解码。
// NewRequestOf creates a typed request wrapper whose body is decoded with JSONInput.
func NewRequestOf[T BodyConstraint](req *Request) RequestOf[T] {
	return newRequestOf[T](req, JSONInput[T]())
}

// newRequestOf 用指定 Input 创建类型化请求包装。T 为 NoDataType 时不读取 body。
// newRequestOf creates a typed request wrapper with the given Input. When T is NoDataType
// the body is never read.
func newRequestOf[T BodyConstraint](req *Request, in Input[T]) RequestOf[T] {
	var zero T
	if _, noData := any(zero).(NoDataType); noData {
		return RequestOf[T]{RequestInput: RequestInput{req: req}}
	}
	lb := &lazyBodyData[T]{req: req, decoder: in.Decode}
	return RequestOf[T]{
		RequestInput: RequestInput{req: req},
		lazyBody:     lb,
	}
}

// Request 返回底层请求上下文，可访问 Raw（*http.Request）、Route() 等。
// Request returns the underlying request context, giving access to Raw (*http.Request),
// Route(), etc.
func (in RequestInput) Request() *Request {
	return in.req
}

// Sources 返回请求输入的原始视图，适合不想物化 DTO 或需要自行转换的场景。
// Sources returns the raw view of the request inputs, for callers that do not want to
// materialize a DTO or prefer converting values themselves.
func (in RequestInput) Sources() Sources {
	return Sources{req: in.req}
}

// Sources 提供 path、query、header、cookie 的原始字符串访问。
// Sources gives raw string access to path, query, header and cookie inputs.
type Sources struct {
	req *Request
}

// Path 返回路径参数及其是否存在。
// Path returns the path parameter and whether it exists.
func (s Sources) Path(name string) (string, bool) {
	return s.req.Params.Lookup(name)
}

// Query 返回查询参数的全部值；返回的切片为共享缓存，不应修改。
// Query returns all values of a query parameter; the slice is shared and must not be modified.
func (s Sources) Query(name string) []string {
	return s.req.Query()[name]
}

// Header 返回请求头的全部值。
// Header returns all values of a request header.
func (s Sources) Header(name string) []string {
	return s.req.Raw.Header.Values(name)
}

// Cookie 返回 cookie 值及其是否存在。
// Cookie returns the cookie value and whether it exists.
func (s Sources) Cookie(name string) (string, bool) {
	c, err := s.req.Raw.Cookie(name)
	if err != nil {
		return "", false
	}
	return c.Value, true
}
