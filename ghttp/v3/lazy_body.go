package v3

import (
	"context"
	"sync"
)

// BodyAccessor 提供 Body 的延迟访问。
// BodyAccessor provides lazy access to the request body.
type BodyAccessor[T any] struct {
	ctx     context.Context
	req     *Request
	codec   Input[T]
	cached  *T
	err     error
	once    sync.Once
	decoded bool
}

// Get 获取 Body 数据（首次调用时才解码）。
// Get retrieves body data (decoded on first call).
func (b *BodyAccessor[T]) Get() (T, error) {
	b.once.Do(func() {
		if b.req == nil || b.req.Request == nil {
			var zero T
			b.cached = &zero
			b.err = ErrMissingBody
			return
		}

		val, err := ReadBody(b.ctx, RequestInput{Request: b.req}, b.codec)
		if err != nil {
			b.err = err
			return
		}

		b.cached = &val
		b.decoded = true
	})

	if b.err != nil {
		var zero T
		return zero, b.err
	}
	return *b.cached, nil
}

// MustGet 获取 Body 数据，失败时 panic。
// MustGet retrieves body data, panics on error.
func (b *BodyAccessor[T]) MustGet() T {
	val, err := b.Get()
	if err != nil {
		panic("BodyAccessor.MustGet failed: " + err.Error())
	}
	return val
}

// IsDecoded 检查 Body 是否已解码。
// IsDecoded checks if the body has been decoded.
func (b *BodyAccessor[T]) IsDecoded() bool {
	return b.decoded
}

// WithLazyBody 为 RequestOf 添加延迟读取的 Body 访问器。
// WithLazyBody adds a lazy body accessor to RequestOf.
//
// 用法 / Usage:
//
//	func UpdateUser(ctx context.Context, req RequestOf[UpdateUserRequest]) (User, error) {
//	    bodyAccessor := v3.WithLazyBody(ctx, &req)
//
//	    // 可以先做其他检查
//	    // Can do other checks first
//	    pathAccessor := v3.Path[UserID](&req)
//	    path, _ := pathAccessor.Get()
//
//	    if !hasPermission(ctx, path.ID) {
//	        return User{}, Forbidden("no permission")
//	        // ✅ Body 未解码
//	    }
//
//	    // 只在需要时才解码 Body
//	    // Decode body only when needed
//	    body, err := bodyAccessor.Get()
//	    if err != nil {
//	        return User{}, err
//	    }
//
//	    return db.UpdateUser(ctx, path.ID, body)
//	}
func WithLazyBody[T any](ctx context.Context, req *RequestOf[T]) *BodyAccessor[T] {
	// 获取原始的 codec
	// Get the original codec
	codec := JSONInput[T]()

	return &BodyAccessor[T]{
		ctx:   ctx,
		req:   req.Request,
		codec: codec,
	}
}

// LazyRequestOf 是 RequestOf 的增强版本，Body 默认延迟读取。
// LazyRequestOf is an enhanced version of RequestOf with lazy body loading by default.
//
// 注意：这是一个新的类型，与现有的 RequestOf[T] 不兼容。
// Note: This is a new type, incompatible with existing RequestOf[T].
type LazyRequestOf[T any] struct {
	RequestInput
	bodyAccessor *BodyAccessor[T]
	ctx          context.Context
}

// Body 获取延迟读取的 Body 访问器。
// Body returns the lazy body accessor.
func (r *LazyRequestOf[T]) Body() *BodyAccessor[T] {
	return r.bodyAccessor
}

// NewLazyRequestOf 创建一个延迟读取 Body 的 RequestOf。
// NewLazyRequestOf creates a RequestOf with lazy body loading.
func NewLazyRequestOf[T any](ctx context.Context, req *Request, codec Input[T]) LazyRequestOf[T] {
	return LazyRequestOf[T]{
		RequestInput: RequestInput{Request: req},
		bodyAccessor: &BodyAccessor[T]{
			ctx:   ctx,
			req:   req,
			codec: codec,
		},
		ctx: ctx,
	}
}

// 辅助方法：直接从 LazyRequestOf 获取类型化访问器
// Helper methods: get typed accessors directly from LazyRequestOf

// PathOf 从 LazyRequestOf 获取路径参数访问器。
// PathOf gets a path parameter accessor from LazyRequestOf.
func PathOf[P any, T any](req *LazyRequestOf[T]) *PathAccessor[P] {
	return &PathAccessor[P]{req: req.Request}
}

// QueryOf 从 LazyRequestOf 获取查询参数访问器。
// QueryOf gets a query parameter accessor from LazyRequestOf.
func QueryOf[Q any, T any](req *LazyRequestOf[T]) *QueryAccessor[Q] {
	return &QueryAccessor[Q]{req: req.Request}
}

// HeaderOf 从 LazyRequestOf 获取 HTTP 头访问器。
// HeaderOf gets a header accessor from LazyRequestOf.
func HeaderOf[H any, T any](req *LazyRequestOf[T]) *HeaderAccessor[H] {
	return &HeaderAccessor[H]{req: req.Request}
}
