package ghttp

import (
	"context"
)

// Key 是类型安全的请求级键,挂在标准 request context 上。与 gin 的 c.Set/c.Get
// 不同,值类型在编译期固定,键自身携带读写操作;net/http 中间件可直接使用。
// Key is a typed request-scoped key carried on the standard request context.
// Unlike gin's c.Set/c.Get, the value type is fixed at compile time and the key
// carries the operation; plain net/http middleware can use it directly.
//
//	var requestID = ghttp.NewKey[string]("request-id")
//	ctx = requestID.Set(ctx, "abc123")
//	id, ok := requestID.Get(ctx)
type Key[T any] struct {
	name string
}

// NewKey 创建类型安全键。键应为包级变量,保证所有 setter/getter 使用同一类型。
// NewKey creates a typed key. Keys should be package-level variables so every
// setter and getter uses the same T.
func NewKey[T any](name string) Key[T] { return Key[T]{name: name} }

// Set 在请求生命周期内存储 v,返回携带该值的新 context。
// Set stores v for the lifetime of this request and returns a new context
// carrying the value.
func (k Key[T]) Set(ctx context.Context, v T) context.Context {
	return context.WithValue(ctx, k, v)
}

// Get 返回存储的值与是否存在的标记。
// Get returns the stored value and whether it was present.
func (k Key[T]) Get(ctx context.Context) (T, bool) {
	v := ctx.Value(k)
	if v == nil {
		var zero T
		return zero, false
	}
	return v.(T), true
}
