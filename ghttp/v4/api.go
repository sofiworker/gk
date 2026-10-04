package v4

// Get 创建 GET 路由。
// Get creates a GET route.
//
// 用法 / Usage:
//   - 无参数 / No parameters: Get(path, func(ctx, req RequestSimple[EmptyBody]) (Out, error))
//   - 带路径参数 / With path params: Get(path, func(ctx, req RequestOfMulti[Path, EmptyBody]) (Out, error))
//   - 带查询参数 / With query params: Get(path, func(ctx, req RequestWithQuery[Query, EmptyBody]) (Out, error))
func Get[In, Out any](path string, handler Handler[In, Out]) Route[In, Out] {
	return Route[In, Out]{
		method:  "GET",
		path:    path,
		handler: handler,
		wrapper: createWrapper[In](),
	}
}

// Post 创建 POST 路由。
// Post creates a POST route.
//
// 用法 / Usage:
//   - 简单 JSON body: Post(path, func(ctx, req RequestSimple[Body]) (Out, error))
//   - 带路径参数: Post(path, func(ctx, req RequestOfMulti[Path, Body]) (Out, error))
func Post[In, Out any](path string, handler Handler[In, Out]) Route[In, Out] {
	return Route[In, Out]{
		method:  "POST",
		path:    path,
		handler: handler,
		wrapper: createWrapper[In](),
	}
}

// Put 创建 PUT 路由。
// Put creates a PUT route.
func Put[In, Out any](path string, handler Handler[In, Out]) Route[In, Out] {
	return Route[In, Out]{
		method:  "PUT",
		path:    path,
		handler: handler,
		wrapper: createWrapper[In](),
	}
}

// Patch 创建 PATCH 路由。
// Patch creates a PATCH route.
func Patch[In, Out any](path string, handler Handler[In, Out]) Route[In, Out] {
	return Route[In, Out]{
		method:  "PATCH",
		path:    path,
		handler: handler,
		wrapper: createWrapper[In](),
	}
}

// Delete 创建 DELETE 路由。
// Delete creates a DELETE route.
func Delete[In, Out any](path string, handler Handler[In, Out]) Route[In, Out] {
	return Route[In, Out]{
		method:  "DELETE",
		path:    path,
		handler: handler,
		wrapper: createWrapper[In](),
	}
}

// createWrapper 根据 In 的类型创建请求包装器。
// createWrapper creates a request wrapper based on the type of In.
//
// 注意：由于 Go 泛型的限制，这里无法使用类型断言来动态创建包装器。
// 实际使用时，需要在 handler 中手动构造 RequestOf 类型。
//
// Note: Due to Go generics limitations, we cannot use type assertions to dynamically create wrappers.
// In actual use, you need to manually construct RequestOf types in handlers.
func createWrapper[In any]() func(*Request) In {
	return func(r *Request) In {
		// 这里返回零值，实际的包装在 Route.compile 中处理
		// Return zero value here, actual wrapping is handled in Route.compile
		var zero In
		return zero
	}
}
