package ghttp

import (
	"context"
	"fmt"
	"net/http"
)

// newRoute 校验通用参数并构造 Route。
// newRoute validates the common arguments and builds a Route.
func newRoute(method, path string, meta routeMeta, build func(routeOptions) (Handler, error), opts []Option) Route {
	if !validMethod(method) {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: invalid HTTP method %q", method)}
	}
	if path == "" {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: route path cannot be empty")}
	}
	return Route{Method: method, Path: path, build: build, opts: opts, meta: meta}
}

// Method 创建指定 HTTP 方法的 typed 路由。body 按 Input（默认 JSON）惰性解码，返回值按
// Output（默认 JSON，并识别 Reply 等响应类型）写出。
// Method creates a typed route for the given HTTP method. The body is lazily decoded via
// the Input (JSON by default) and the result written via the Output (JSON by default,
// honoring Reply and friends).
func Method[T BodyConstraint, O ResponseConstraint](method, path string, h Endpoint[T, O], opts ...Option) Route {
	if h == nil {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: handler cannot be nil")}
	}
	if err := validateBodyType[T](); err != nil {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: invalid body type: %w", err)}
	}
	if err := validateResponseType[O](); err != nil {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: invalid response type: %w", err)}
	}
	meta := routeMeta{kind: RouteEndpoint, bodyType: typeOrNil[T](), resultType: typeOrNil[O]()}
	return newRoute(method, path, meta, func(o routeOptions) (Handler, error) {
		return compileEndpoint(h, o)
	}, opts)
}

// Get 创建 GET 路由。
// Get creates a GET route.
func Get[T BodyConstraint, O ResponseConstraint](path string, h Endpoint[T, O], opts ...Option) Route {
	return Method(http.MethodGet, path, h, opts...)
}

// Post 创建 POST 路由。
// Post creates a POST route.
func Post[T BodyConstraint, O ResponseConstraint](path string, h Endpoint[T, O], opts ...Option) Route {
	return Method(http.MethodPost, path, h, opts...)
}

// Put 创建 PUT 路由。
// Put creates a PUT route.
func Put[T BodyConstraint, O ResponseConstraint](path string, h Endpoint[T, O], opts ...Option) Route {
	return Method(http.MethodPut, path, h, opts...)
}

// Patch 创建 PATCH 路由。
// Patch creates a PATCH route.
func Patch[T BodyConstraint, O ResponseConstraint](path string, h Endpoint[T, O], opts ...Option) Route {
	return Method(http.MethodPatch, path, h, opts...)
}

// Delete 创建 DELETE 路由。
// Delete creates a DELETE route.
func Delete[T BodyConstraint, O ResponseConstraint](path string, h Endpoint[T, O], opts ...Option) Route {
	return Method(http.MethodDelete, path, h, opts...)
}

// Head 创建 HEAD 路由。
// Head creates a HEAD route.
func Head[T BodyConstraint, O ResponseConstraint](path string, h Endpoint[T, O], opts ...Option) Route {
	return Method(http.MethodHead, path, h, opts...)
}

// Options 创建 OPTIONS 路由。
// Options creates an OPTIONS route.
func Options[T BodyConstraint, O ResponseConstraint](path string, h Endpoint[T, O], opts ...Option) Route {
	return Method(http.MethodOptions, path, h, opts...)
}

// HandleAction 创建无返回值的 typed 路由：handler 成功时返回 204 No Content。
// HandleAction creates a typed route without a result: success answers 204 No Content.
func HandleAction[T BodyConstraint](method, path string, h Action[T], opts ...Option) Route {
	if h == nil {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: handler cannot be nil")}
	}
	if err := validateBodyType[T](); err != nil {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: invalid body type: %w", err)}
	}
	meta := routeMeta{kind: RouteAction, bodyType: typeOrNil[T]()}
	return newRoute(method, path, meta, func(o routeOptions) (Handler, error) {
		return compileAction(h, o)
	}, opts)
}

// HandleProcedure 创建既不读请求也不返回结果的路由：成功时返回 204 No Content。
// HandleProcedure creates a route that neither reads the request nor returns a result:
// success answers 204 No Content.
func HandleProcedure(method, path string, h Procedure, opts ...Option) Route {
	if h == nil {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: handler cannot be nil")}
	}
	r := HandleAction(method, path, func(ctx context.Context, _ RequestOf[NoDataType]) error {
		return h(ctx)
	}, opts...)
	r.meta.kind = RouteProcedure
	return r
}

// Raw 创建原始 handler 路由，用于完全接管响应的场景（流式、协议升级等）。
// 仍经过相同的路由树、中间件与错误链；WithInput/WithOutput 不适用。
// Raw creates a raw handler route for taking over the response entirely (streaming,
// protocol upgrades, ...). It still goes through the same router, middleware and error
// chain; WithInput/WithOutput do not apply.
func Raw(method, path string, h RawHandlerFunc, opts ...Option) Route {
	if h == nil {
		return Route{Method: method, Path: path, err: fmt.Errorf("ghttp: handler cannot be nil")}
	}
	return newRoute(method, path, routeMeta{kind: RouteRaw}, func(o routeOptions) (Handler, error) {
		if o.input != nil || o.output != nil {
			return nil, fmt.Errorf("ghttp: WithInput/WithOutput are not applicable to Raw routes")
		}
		return Handler(h), nil
	}, opts)
}

// validMethod 报告 method 是否为合法的 HTTP token（RFC 9110 §5.6.2）。
// validMethod reports whether method is a valid HTTP token (RFC 9110 §5.6.2).
func validMethod(method string) bool {
	if method == "" {
		return false
	}
	for i := 0; i < len(method); i++ {
		if !isTokenChar(method[i]) {
			return false
		}
	}
	return true
}

// isTokenChar 报告 c 是否为 tchar。
// isTokenChar reports whether c is a tchar.
func isTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}
