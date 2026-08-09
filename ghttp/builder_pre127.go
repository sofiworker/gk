//go:build !go1.27

package ghttp

import (
	"errors"
	"net/http"
	"reflect"
)

// RouteBuilder 以 go-restful 风格链式构建路由。
// RouteBuilder builds a route in a go-restful-style chain.
//
// 这是 Go 1.27 前的 API：Req/Resp 是 builder 的类型参数。
// This is the pre-Go-1.27 API: Req/Resp are builder type parameters.
// 因为当时方法不能声明类型参数；Go 1.27 泛型方法可用后由非泛型链承载。
// methods could not declare type parameters then; Go 1.27 serves the same chain.
type RouteBuilder[Req, Resp any] struct {
	core  *routeBuilderCore
	input compiledInput[Req]
}

// Route 在给定 target 上创建新的 RouteBuilder。
// Route creates a new RouteBuilder on the given target.
// 用法：Route[CreateUserReq, UserResp](s).POST("/users/{id}").To(handler)
// Usage: Route[CreateUserReq, UserResp](s).POST("/users/{id}").To(handler)
func Route[Req, Resp any](target routeTarget) *RouteBuilder[Req, Resp] {
	return &RouteBuilder[Req, Resp]{
		core:  newRouteBuilderCore(target),
		input: compileInput[Req](),
	}
}

func (b *RouteBuilder[Req, Resp]) POST(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodPost, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) GET(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodGet, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) PUT(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodPut, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) DELETE(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodDelete, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) PATCH(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodPatch, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) HEAD(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodHead, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) OPTIONS(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodOptions, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) CONNECT(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodConnect, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) TRACE(path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(http.MethodTrace, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) ANY(path string) *RouteBuilder[Req, Resp] {
	b.core.methodsSet(allHTTPMethods, path)
	return b
}

func (b *RouteBuilder[Req, Resp]) CUSTOM(method, path string) *RouteBuilder[Req, Resp] {
	b.core.methodSet(method, path)
	return b
}

// Apply 批量应用路由选项，等价于依次调用对应链式方法。
// Apply applies route options in bulk, equivalent to the chained calls.
// 必须在终结方法之前调用；终结后调用触发 ErrRouteBuilderFinalized。
// It must be called before a terminal; calls after finalizing panic with ErrRouteBuilderFinalized.
func (b *RouteBuilder[Req, Resp]) Apply(opts ...RouteOption) *RouteBuilder[Req, Resp] {
	for _, opt := range opts {
		if opt != nil {
			opt(b.core)
		}
	}
	return b
}

// Doc 配置路由文档元数据。
// Doc configures route documentation metadata.
func (b *RouteBuilder[Req, Resp]) Doc(opts ...DocOption) *RouteBuilder[Req, Resp] {
	b.core.docOptions(opts...)
	return b
}

// Produces 声明自动响应编码的 Content-Type。
// Produces declares response Content-Types for automatic encoding.
func (b *RouteBuilder[Req, Resp]) Produces(contentTypes ...string) *RouteBuilder[Req, Resp] {
	b.core.setProduces(contentTypes...)
	return b
}

// Consumes 声明自动解码接受的请求 Content-Type。
// Consumes declares accepted request Content-Types.
func (b *RouteBuilder[Req, Resp]) Consumes(contentTypes ...string) *RouteBuilder[Req, Resp] {
	b.core.setConsumes(contentTypes...)
	return b
}

// MaxBodyBytes 覆盖本路由的服务器请求体大小限制。
// MaxBodyBytes overrides the server body size limit for this route.
// 小于等于 0 时禁用限制。
// values <= 0 disable the limit.
func (b *RouteBuilder[Req, Resp]) MaxBodyBytes(n int64) *RouteBuilder[Req, Resp] {
	b.core.setMaxBodyBytes(n)
	return b
}

// Validate 在输入绑定后添加路由级校验。
// Validate adds route-level validation after input binding.
func (b *RouteBuilder[Req, Resp]) Validate(fn interface{}, opts ...ValidateOption) *RouteBuilder[Req, Resp] {
	b.core.ensureMutable()
	validator, err := coerceRouteValidator[Req](fn)
	if err != nil {
		b.core.setupErr = errors.Join(b.core.setupErr, err)
	} else {
		b.core.validator = validator
	}
	var cfg validateOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	b.core.validationError = cfg.err
	return b
}

// SkipValidation 为本路由禁用服务器级校验器。
// SkipValidation disables the server-level validator for this route.
func (b *RouteBuilder[Req, Resp]) SkipValidation() *RouteBuilder[Req, Resp] {
	b.core.skipValidationSet()
	return b
}

// Status 声明 To handler 的固定成功状态码。
// Status declares the fixed success status for To handlers.
func (b *RouteBuilder[Req, Resp]) Status(code int) *RouteBuilder[Req, Resp] {
	b.core.status(code)
	return b
}

// ResponseHeader declares a fixed response header for To handlers.
func (b *RouteBuilder[Req, Resp]) ResponseHeader(name, value string) *RouteBuilder[Req, Resp] {
	b.core.responseHeader(name, value)
	return b
}

// ErrorWriter 安装路由级错误 writer；返回 true 表示已处理。
// ErrorWriter installs a route-level error writer; true means handled.
// false 时回退到服务器 writer 或内置 writer。
// false falls through to the server or built-in writer.
// the built-in error writer.
func (b *RouteBuilder[Req, Resp]) ErrorWriter(writer ErrorWriter) *RouteBuilder[Req, Resp] {
	b.core.setErrorWriter(writer)
	return b
}

// ProblemDetails 让本路由使用 RFC 9457 application/problem+json。
// ProblemDetails enables RFC 9457 problem+json for this route.
// 覆盖服务器级错误模型。
// overriding the server-wide error model.
func (b *RouteBuilder[Req, Resp]) ProblemDetails() *RouteBuilder[Req, Resp] {
	b.core.problemDetails()
	return b
}

// Use 添加路由级中间件。
// Use adds route-level middleware.
func (b *RouteBuilder[Req, Resp]) Use(mws ...Middleware) *RouteBuilder[Req, Resp] {
	b.core.use(mws...)
	return b
}

// Group 以 builder target 为根创建新路由组（gin 的 r.Group 语义）。
// Group branches into a new route group rooted at the builder target.
// 须在设置 method/path 之前调用；已设置的路由级选项不转移。
// it must be called before a method/path is set; route options are not transferred.
// options already configured on this builder are not transferred.
func (b *RouteBuilder[Req, Resp]) Group(prefix string, mws ...Middleware) *Group {
	return b.core.group(prefix, mws...)
}

// To 注册 handler 并终结路由。
// To registers the handler and finalizes the route.
func (b *RouteBuilder[Req, Resp]) To(handler HandlerFunc[Req, Resp]) {
	registerTypedHandler(b.core, b.input, handler)
}

// ToNoInput 注册无请求输入的 handler；响应与 To 走同一类型化管线。
// ToNoInput registers a handler with no input; response uses the same typed pipeline.
func (b *RouteBuilder[Req, Resp]) ToNoInput(handler NoInputHandler[Resp]) {
	registerNoInputHandler(b.core, handler)
}

// ToNoOutput 注册只返回 error 的 handler；成功默认 204。
// ToNoOutput registers a handler returning only an error; success is 204.
// by default and can be overridden with Status.
func (b *RouteBuilder[Req, Resp]) ToNoOutput(handler NoOutputHandler[Req]) {
	registerNoOutputHandler(b.core, b.input, handler)
}

// ToHTTP 用所选方法与路径注册原始 http.Handler。
// ToHTTP registers a raw http.Handler.
func (b *RouteBuilder[Req, Resp]) ToHTTP(handler http.Handler) {
	b.core.toHandler(handler, false, routeTerminalRaw, 0, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToRaw 注册原始 handler 函数。
// ToRaw registers a raw handler function.
func (b *RouteBuilder[Req, Resp]) ToRaw(handler RawHandler) {
	b.ToHTTP(http.HandlerFunc(handler))
}

// ToHTTPFunc 注册自行写响应的解析输入 handler。
// ToHTTPFunc registers a parsed-input handler that writes the response itself.
func (b *RouteBuilder[Req, Resp]) ToHTTPFunc(handler HTTPHandlerFunc[Req]) {
	registerHTTPFuncHandler(b.core, b.input, handler, routeTerminalHTTPFunc, 0)
}

// ToRedirect 注册固定重定向响应。
// ToRedirect registers a fixed redirect response.
func (b *RouteBuilder[Req, Resp]) ToRedirect(code int, location string) {
	b.ToRedirectFunc(code, func(Req) (string, error) {
		return location, nil
	})
}

// ToRedirectFunc 注册使用解析输入的重定向响应。
// ToRedirectFunc registers a redirect using parsed input.
func (b *RouteBuilder[Req, Resp]) ToRedirectFunc(code int, redirect RedirectFunc[Req]) {
	registerRedirectFuncHandler(b.core, b.input, code, redirect)
}

// ToSSE 注册 Server-Sent Events handler。
// ToSSE registers a Server-Sent Events handler.
func (b *RouteBuilder[Req, Resp]) ToSSE(handler SSEHandler) {
	b.core.toSSE(handler, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToWebSocket 注册 WebSocket 升级 handler。
// ToWebSocket registers a WebSocket upgrade handler.
func (b *RouteBuilder[Req, Resp]) ToWebSocket(handler WebSocketHandler) {
	b.core.toWebSocket(handler, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// WebSocketCheckOrigin 覆盖本路由的服务器级 WebSocket origin 校验。
// WebSocketCheckOrigin overrides the server-level origin check for this route.
// this route. It must be called before ToWebSocket.
func (b *RouteBuilder[Req, Resp]) WebSocketCheckOrigin(check func(*http.Request) bool) *RouteBuilder[Req, Resp] {
	b.core.setWebSocketCheckOrigin(check)
	return b
}

// ToStatic 以所选路径为 URL 前缀注册安全文件服务器。
// ToStatic registers a safe file server.
func (b *RouteBuilder[Req, Resp]) ToStatic(root ...string) {
	b.core.beginTerminal()
	staticRoot := b.core.target.owner().config.vfsPath
	if len(root) > 0 {
		staticRoot = root[0]
	}
	if staticRoot == "" {
		b.core.panicSetupError(ErrStaticRootRequired)
	}
	fsys, err := NewSafeFS(staticRoot)
	if err != nil {
		b.core.panicSetupError(err)
	}
	b.core.registerStaticFS(fsys, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToStaticFS 以所选路径为 URL 前缀注册文件服务器。
// ToStaticFS registers a file server.
func (b *RouteBuilder[Req, Resp]) ToStaticFS(fs http.FileSystem) {
	b.core.beginTerminal()
	b.core.registerStaticFS(fs, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToStaticFile 注册单个静态文件。
// ToStaticFile registers a single static file.
func (b *RouteBuilder[Req, Resp]) ToStaticFile(filepath string) {
	b.core.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath)
	}), false, routeTerminalStatic, 0, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}

// ToHTML 用固定数据渲染配置的模板。
// ToHTML renders a configured template with fixed data.
func (b *RouteBuilder[Req, Resp]) ToHTML(status int, name string, data interface{}) {
	b.core.toHTML(status, name, data, reflect.TypeFor[Req](), reflect.TypeFor[Resp]())
}
