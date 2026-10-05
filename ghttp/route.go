package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
)

// Route 表示一个待注册的路由。由 Method、Get、Raw 等构造，交给 Server.Register 或
// Group.Register 时才与 Server/Group 的默认选项合并并编译。
// Route is a route awaiting registration. It is built by Method, Get, Raw, etc. and is
// merged with Server/Group default options and compiled only when passed to
// Server.Register or Group.Register.
type Route struct {
	// Method 是 HTTP 方法（GET、POST 等）
	// Method is the HTTP method (GET, POST, etc.)
	Method string

	// Path 是路由路径（如 /users/:id）
	// Path is the route path (e.g., /users/:id)
	Path string

	// compiledHandler 是已编译的处理器；build 为 nil 时直接使用（内部与测试使用）
	// compiledHandler is a precompiled handler, used as-is when build is nil (internal/tests)
	compiledHandler Handler

	// build 按合并后的选项编译处理器
	// build compiles the handler from the merged options
	build func(routeOptions) (Handler, error)

	// opts 是路由级选项
	// opts are the route-level options
	opts []Option

	// meta 是构造期记录的类型信息
	// meta is the type information recorded at construction
	meta routeMeta

	// err 记录构造期的错误
	// err records construction-time errors
	err error
}

// Err 返回路由构造期的错误。
// Err returns errors recorded while building the route.
func (r Route) Err() error {
	return r.err
}

// Option 是路由选项，可用于单个路由，也可经 Server.With、Group.With 作为默认值。
// Option is a route option, usable per route or as defaults via Server.With / Group.With.
type Option func(*routeOptions)

// routeOptions 存储合并后的路由配置。
// routeOptions stores the merged route configuration.
type routeOptions struct {
	// middleware 按"外层在前"的顺序保存
	// middleware is stored outermost first
	middleware []Middleware

	// bodyLimit 是请求体上限；0 表示沿用 Server 默认值，<0 表示不限
	// bodyLimit is the body limit; 0 inherits the server default, <0 means unlimited
	bodyLimit int64

	// input 是 Input[T]，output 是 Output[O]；类型在编译时校验
	// input holds an Input[T] and output an Output[O]; types are checked at compile time
	input  any
	output any

	// doc 是文档信息
	// doc is the documentation
	doc RouteDoc

	// validators 在 body 解码成功后依次执行
	// validators run in order after a successful body decode
	validators []Validator

	// err 记录选项自身的错误（如 nil middleware）
	// err records errors from the options themselves (e.g. nil middleware)
	err error
}

// apply 依次应用选项。
// apply applies the options in order.
func (o *routeOptions) apply(opts []Option) {
	for _, opt := range opts {
		if opt == nil {
			o.setErr(fmt.Errorf("ghttp: nil route option"))
			continue
		}
		opt(o)
	}
}

// setErr 只保留第一个错误。
// setErr keeps the first error only.
func (o *routeOptions) setErr(err error) {
	if o.err == nil {
		o.err = err
	}
}

// WithMiddleware 为路由添加中间件。多次调用时先添加的位于外层。
// WithMiddleware adds route middleware; earlier additions wrap later ones.
func WithMiddleware(middleware ...Middleware) Option {
	return func(o *routeOptions) {
		for _, m := range middleware {
			if m == nil {
				o.setErr(fmt.Errorf("ghttp: nil middleware"))
				return
			}
		}
		o.middleware = append(o.middleware, middleware...)
	}
}

// WithBodyLimit 设置路由的请求体上限（字节），覆盖 Server 的 WithMaxBodyBytes；n<0 表示不限，
// n==0 表示沿用 Server 默认值。超限时读取 body 返回 *http.MaxBytesError，映射为 413。
// WithBodyLimit sets the route's request-body limit in bytes, overriding the server's
// WithMaxBodyBytes; n<0 means unlimited and n==0 inherits the server default. Reading past
// the limit yields *http.MaxBytesError, mapped to 413.
func WithBodyLimit(n int64) Option {
	return func(o *routeOptions) { o.bodyLimit = n }
}

// WithInput 指定请求体的解码方式（仅路由级）。T 必须与 handler 的 body 类型一致，否则注册失败。
// WithInput sets how the request body is decoded (route level only). T must match the
// handler's body type or registration fails.
func WithInput[T BodyConstraint](in Input[T]) Option {
	return func(o *routeOptions) {
		if in == nil {
			o.setErr(fmt.Errorf("ghttp: nil Input"))
			return
		}
		o.input = in
	}
}

// WithOutput 指定返回值的写出方式（仅路由级）。O 必须与 handler 的返回类型一致，否则注册失败。
// 指定后不再识别 Reply 等响应类型，整个返回值交给 Output 处理。
// WithOutput sets how the result is written (route level only). O must match the
// handler's result type or registration fails. Once set, Reply and friends are no longer
// special-cased: the whole result goes to the Output.
func WithOutput[O ResponseConstraint](out Output[O]) Option {
	return func(o *routeOptions) {
		if out == nil {
			o.setErr(fmt.Errorf("ghttp: nil Output"))
			return
		}
		o.output = out
	}
}

// resolveInput 返回路由的 Input（未指定时为 JSONInput），并叠加校验。
// resolveInput returns the route's Input (JSONInput by default) with validation layered on.
func resolveInput[T BodyConstraint](o routeOptions) (Input[T], error) {
	in := JSONInput[T]()
	if o.input != nil {
		var ok bool
		if in, ok = o.input.(Input[T]); !ok {
			var zero T
			return nil, fmt.Errorf("ghttp: WithInput type %T does not match body type %T", o.input, zero)
		}
	}
	return validatingInput(in, o.validators), nil
}

// resolveOutput 返回路由的 Output；未指定时返回 nil，表示默认 JSON 并识别响应类型。
// resolveOutput returns the route's Output, or nil meaning default JSON with reply types honored.
func resolveOutput[O ResponseConstraint](o routeOptions) (Output[O], error) {
	if o.output == nil {
		return nil, nil
	}
	out, ok := o.output.(Output[O])
	if !ok {
		var zero O
		return nil, fmt.Errorf("ghttp: WithOutput type %T does not match result type %T", o.output, zero)
	}
	return out, nil
}

// compileEndpoint 将 Endpoint[T, O] 编译为 Handler：构造 lazy 的 RequestOf[T]，调用 handler，
// 再按 Output 或默认规则写出结果。
// compileEndpoint compiles Endpoint[T, O] into a Handler: it builds a lazy RequestOf[T],
// calls the handler and writes the result via the Output or the default rules.
func compileEndpoint[T BodyConstraint, O ResponseConstraint](endpoint Endpoint[T, O], o routeOptions) (Handler, error) {
	in, err := resolveInput[T](o)
	if err != nil {
		return nil, err
	}
	out, err := resolveOutput[O](o)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, req *Request, resp *Response) error {
		result, err := endpoint(ctx, newRequestOf[T](req, in))
		if err != nil {
			return err
		}
		if out != nil {
			return out.Encode(ctx, resp, result)
		}
		return writeResult(ctx, req, resp, result)
	}, nil
}

// compileAction 将 Action[T] 编译为 Handler，成功时返回 204。
// compileAction compiles Action[T] into a Handler that answers 204 on success.
func compileAction[T BodyConstraint](action Action[T], o routeOptions) (Handler, error) {
	if o.output != nil {
		return nil, fmt.Errorf("ghttp: WithOutput is not applicable to actions")
	}
	in, err := resolveInput[T](o)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, req *Request, resp *Response) error {
		if err := action(ctx, newRequestOf[T](req, in)); err != nil {
			return err
		}
		resp.WriteHeader(http.StatusNoContent)
		return nil
	}, nil
}

// writeResult 写出默认结果：responder 自行写出，其余按 JSON 200。
// writeResult writes a default result: responders write themselves, everything else is JSON 200.
func writeResult(ctx context.Context, req *Request, resp *Response, result any) error {
	if r, ok := asResponder(result); ok {
		return r.respond(ctx, req, resp)
	}
	return writeJSON(resp, http.StatusOK, result)
}

// validateBodyType 验证 Body 类型是否合法：结构体、结构体指针或 NoDataType。
// validateBodyType checks the body type is a struct, a struct pointer or NoDataType.
func validateBodyType[T BodyConstraint]() error {
	typ := reflect.TypeFor[T]()
	if typ == reflect.TypeFor[NoDataType]() {
		return nil
	}
	if typ.Kind() == reflect.Struct {
		return nil
	}
	if typ.Kind() == reflect.Pointer && typ.Elem().Kind() == reflect.Struct {
		return nil
	}
	return fmt.Errorf("body type must be struct, struct pointer, or NoData, got %v", typ)
}

// validateResponseType 验证响应类型可被序列化：拒绝 func、chan、复数与 unsafe.Pointer。
// validateResponseType checks the result type is serializable: func, chan, complex and
// unsafe.Pointer are rejected.
func validateResponseType[O ResponseConstraint]() error {
	typ := reflect.TypeFor[O]()
	switch typ.Kind() {
	case reflect.Func, reflect.Chan, reflect.Complex64, reflect.Complex128, reflect.UnsafePointer, reflect.Invalid:
		return fmt.Errorf("response type must be serializable, got %v", typ)
	}
	return nil
}
