//go:build go1.27

package ghttp

import (
	"context"
	"net/http"
)

// RouteBuilder 以 go-restful 风格链式构建路由。
// RouteBuilder builds a route in a go-restful-style chain.
//
// 这是 Go 1.27 API：链为非泛型，终结方法声明自己的类型参数并由 handler 推断。
// This is the Go 1.27 API: non-generic chain, generic terminal methods inferred from handlers.
// 包级 Route 保留为源码兼容 shim。
// the package-level Route remains as a source-compatible shim.
type RouteBuilder struct {
	core *routeBuilderCore
}

// Deprecated: Route 是 pre-1.27 兼容入口。
// Deprecated: Route is the pre-1.27 compatibility entry point.
// 类型参数仅为源码兼容而被忽略；它返回与 s.GET(path) 相同的根组 builder。
// type arguments are accepted but ignored; it returns the same root-group builder.
// 新代码应直接使用动词方法。
// new code should call the verb methods directly.
func Route[Req, Resp any](target routeTarget) *RouteBuilder {
	return newRouteBuilder(target)
}

func newRouteBuilder(target routeTarget) *RouteBuilder {
	return &RouteBuilder{core: newRouteBuilderCore(target)}
}

// GET 在服务器上开启 GET 路由链（类似 gin 根组）。
// GET starts a GET route chain on the server.
func (s *Server) GET(path string) *RouteBuilder {
	return newRouteBuilder(s).GET(path)
}

// POST 在服务器上开启 POST 路由链。
// POST starts a POST route chain on the server.
func (s *Server) POST(path string) *RouteBuilder {
	return newRouteBuilder(s).POST(path)
}

// PUT 在服务器上开启 PUT 路由链。
// PUT starts a PUT route chain on the server.
func (s *Server) PUT(path string) *RouteBuilder {
	return newRouteBuilder(s).PUT(path)
}

// DELETE 在服务器上开启 DELETE 路由链。
// DELETE starts a DELETE route chain on the server.
func (s *Server) DELETE(path string) *RouteBuilder {
	return newRouteBuilder(s).DELETE(path)
}

// PATCH 在服务器上开启 PATCH 路由链。
// PATCH starts a PATCH route chain on the server.
func (s *Server) PATCH(path string) *RouteBuilder {
	return newRouteBuilder(s).PATCH(path)
}

// HEAD 在服务器上开启 HEAD 路由链。
// HEAD starts a HEAD route chain on the server.
func (s *Server) HEAD(path string) *RouteBuilder {
	return newRouteBuilder(s).HEAD(path)
}

// OPTIONS 在服务器上开启 OPTIONS 路由链。
// OPTIONS starts an OPTIONS route chain on the server.
func (s *Server) OPTIONS(path string) *RouteBuilder {
	return newRouteBuilder(s).OPTIONS(path)
}

// CONNECT 在服务器上开启 CONNECT 路由链。
// CONNECT starts a CONNECT route chain on the server.
func (s *Server) CONNECT(path string) *RouteBuilder {
	return newRouteBuilder(s).CONNECT(path)
}

// TRACE 在服务器上开启 TRACE 路由链。
// TRACE starts a TRACE route chain on the server.
func (s *Server) TRACE(path string) *RouteBuilder {
	return newRouteBuilder(s).TRACE(path)
}

// ANY 在服务器上开启覆盖所有标准 HTTP 方法的路由链。
// ANY starts a route chain for every standard HTTP method.
func (s *Server) ANY(path string) *RouteBuilder {
	return newRouteBuilder(s).ANY(path)
}

// CUSTOM 在服务器上用自定义 HTTP 方法开启路由链。
// CUSTOM starts a route chain with a custom HTTP method.
func (s *Server) CUSTOM(method, path string) *RouteBuilder {
	return newRouteBuilder(s).CUSTOM(method, path)
}

// GET 在组上开启 GET 路由链。
// GET starts a GET route chain on the group.
func (g *Group) GET(path string) *RouteBuilder {
	return newRouteBuilder(g).GET(path)
}

// POST 在组上开启 POST 路由链。
// POST starts a POST route chain on the group.
func (g *Group) POST(path string) *RouteBuilder {
	return newRouteBuilder(g).POST(path)
}

// PUT 在组上开启 PUT 路由链。
// PUT starts a PUT route chain on the group.
func (g *Group) PUT(path string) *RouteBuilder {
	return newRouteBuilder(g).PUT(path)
}

// DELETE 在组上开启 DELETE 路由链。
// DELETE starts a DELETE route chain on the group.
func (g *Group) DELETE(path string) *RouteBuilder {
	return newRouteBuilder(g).DELETE(path)
}

// PATCH 在组上开启 PATCH 路由链。
// PATCH starts a PATCH route chain on the group.
func (g *Group) PATCH(path string) *RouteBuilder {
	return newRouteBuilder(g).PATCH(path)
}

// HEAD 在组上开启 HEAD 路由链。
// HEAD starts a HEAD route chain on the group.
func (g *Group) HEAD(path string) *RouteBuilder {
	return newRouteBuilder(g).HEAD(path)
}

// OPTIONS 在组上开启 OPTIONS 路由链。
// OPTIONS starts an OPTIONS route chain on the group.
func (g *Group) OPTIONS(path string) *RouteBuilder {
	return newRouteBuilder(g).OPTIONS(path)
}

// CONNECT 在组上开启 CONNECT 路由链。
// CONNECT starts a CONNECT route chain on the group.
func (g *Group) CONNECT(path string) *RouteBuilder {
	return newRouteBuilder(g).CONNECT(path)
}

// TRACE 在组上开启 TRACE 路由链。
// TRACE starts a TRACE route chain on the group.
func (g *Group) TRACE(path string) *RouteBuilder {
	return newRouteBuilder(g).TRACE(path)
}

// ANY 在组上开启覆盖所有标准 HTTP 方法的路由链。
// ANY starts a route chain for every standard HTTP method on the group.
func (g *Group) ANY(path string) *RouteBuilder {
	return newRouteBuilder(g).ANY(path)
}

// CUSTOM 在组上用自定义 HTTP 方法开启路由链。
// CUSTOM starts a route chain with a custom HTTP method on the group.
func (g *Group) CUSTOM(method, path string) *RouteBuilder {
	return newRouteBuilder(g).CUSTOM(method, path)
}

func (b *RouteBuilder) POST(path string) *RouteBuilder {
	b.core.methodSet(http.MethodPost, path)
	return b
}

func (b *RouteBuilder) GET(path string) *RouteBuilder {
	b.core.methodSet(http.MethodGet, path)
	return b
}

func (b *RouteBuilder) PUT(path string) *RouteBuilder {
	b.core.methodSet(http.MethodPut, path)
	return b
}

func (b *RouteBuilder) DELETE(path string) *RouteBuilder {
	b.core.methodSet(http.MethodDelete, path)
	return b
}

func (b *RouteBuilder) PATCH(path string) *RouteBuilder {
	b.core.methodSet(http.MethodPatch, path)
	return b
}

func (b *RouteBuilder) HEAD(path string) *RouteBuilder {
	b.core.methodSet(http.MethodHead, path)
	return b
}

func (b *RouteBuilder) OPTIONS(path string) *RouteBuilder {
	b.core.methodSet(http.MethodOptions, path)
	return b
}

func (b *RouteBuilder) CONNECT(path string) *RouteBuilder {
	b.core.methodSet(http.MethodConnect, path)
	return b
}

func (b *RouteBuilder) TRACE(path string) *RouteBuilder {
	b.core.methodSet(http.MethodTrace, path)
	return b
}

func (b *RouteBuilder) ANY(path string) *RouteBuilder {
	b.core.methodsSet(allHTTPMethods, path)
	return b
}

func (b *RouteBuilder) CUSTOM(method, path string) *RouteBuilder {
	b.core.methodSet(method, path)
	return b
}

// Doc 配置路由文档元数据。
// Doc configures route documentation metadata.
func (b *RouteBuilder) Doc(opts ...DocOption) *RouteBuilder {
	b.core.docOptions(opts...)
	return b
}

// Produces 声明自动响应编码的 Content-Type。
// Produces declares response Content-Types for automatic encoding.
func (b *RouteBuilder) Produces(contentTypes ...string) *RouteBuilder {
	b.core.setProduces(contentTypes...)
	return b
}

// Consumes 声明自动解码接受的请求 Content-Type。
// Consumes declares accepted request Content-Types.
func (b *RouteBuilder) Consumes(contentTypes ...string) *RouteBuilder {
	b.core.setConsumes(contentTypes...)
	return b
}

// MaxBodyBytes 覆盖本路由的服务器请求体大小限制。
// MaxBodyBytes overrides the server body size limit for this route.
// 小于等于 0 时禁用限制。
// values <= 0 disable the limit.
func (b *RouteBuilder) MaxBodyBytes(n int64) *RouteBuilder {
	b.core.setMaxBodyBytes(n)
	return b
}

// Validate 在输入绑定后添加路由级校验；具体校验器形状在终结方法处检查。
// Validate adds route-level validation; the concrete shape is checked at the terminal.
func (b *RouteBuilder) Validate(fn interface{}, opts ...ValidateOption) *RouteBuilder {
	b.core.ensureMutable()
	b.core.validator = fn
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
func (b *RouteBuilder) SkipValidation() *RouteBuilder {
	b.core.skipValidationSet()
	return b
}

// Status 声明类型化 handler 的固定成功状态码。
// Status declares the fixed success status for typed handlers.
func (b *RouteBuilder) Status(code int) *RouteBuilder {
	b.core.status(code)
	return b
}

// ResponseHeader 声明类型化 handler 的固定响应头。
// ResponseHeader declares a fixed response header.
func (b *RouteBuilder) ResponseHeader(name, value string) *RouteBuilder {
	b.core.responseHeader(name, value)
	return b
}

// ErrorWriter 安装路由级错误 writer；返回 true 表示已处理。
// ErrorWriter installs a route-level error writer; true means handled.
// false 时回退到服务器 writer 或内置 writer。
// false falls through to the server or built-in writer.
func (b *RouteBuilder) ErrorWriter(writer ErrorWriter) *RouteBuilder {
	b.core.setErrorWriter(writer)
	return b
}

// ProblemDetails 让本路由使用 RFC 9457 application/problem+json。
// ProblemDetails enables RFC 9457 problem+json for this route.
// 覆盖服务器级错误模型。
// overriding the server-wide error model.
func (b *RouteBuilder) ProblemDetails() *RouteBuilder {
	b.core.problemDetails()
	return b
}

// Use 添加路由级中间件。
// Use adds route-level middleware.
func (b *RouteBuilder) Use(mws ...Middleware) *RouteBuilder {
	b.core.use(mws...)
	return b
}

// Group 以 builder target 为根创建新路由组（gin 的 r.Group 语义）。
// Group branches into a new route group rooted at the builder target.
// 须在设置 method/path 之前调用；已设置的路由级选项不转移。
// it must be called before a method/path is set; route options are not transferred.
func (b *RouteBuilder) Group(prefix string, mws ...Middleware) *Group {
	return b.core.group(prefix, mws...)
}

// To 注册类型化 handler 并终结路由；Req/Resp 由 handler 推断。
// To registers a typed handler and finalizes the route; Req/Resp are inferred.
func (b *RouteBuilder) To[Req, Resp any](handler func(context.Context, Req) (Resp, error)) {
	registerTypedHandler(b.core, compileInput[Req](), HandlerFunc[Req, Resp](handler))
}

// ToNoInput 注册无请求输入的 handler；Resp 由 handler 推断。
// ToNoInput registers a handler with no request input.
func (b *RouteBuilder) ToNoInput[Resp any](handler func(context.Context) (Resp, error)) {
	registerNoInputHandler(b.core, NoInputHandler[Resp](handler))
}

// ToNoOutput 注册只返回 error 的 handler；成功默认 204。
// ToNoOutput registers a handler returning only an error; success is 204.
func (b *RouteBuilder) ToNoOutput[Req any](handler func(context.Context, Req) error) {
	registerNoOutputHandler(b.core, compileInput[Req](), NoOutputHandler[Req](handler))
}

// ToHTTP 用所选方法与路径注册原始 http.Handler。
// ToHTTP registers a raw http.Handler.
func (b *RouteBuilder) ToHTTP(handler http.Handler) {
	b.core.toHandler(handler, false, routeTerminalRaw, 0, nil, nil)
}

// ToRaw 注册原始 handler 函数。
// ToRaw registers a raw handler function.
func (b *RouteBuilder) ToRaw(handler RawHandler) {
	b.ToHTTP(http.HandlerFunc(handler))
}

// ToHTTPFunc 注册自行写响应的解析输入 handler。
// ToHTTPFunc registers a parsed-input handler that writes the response itself.
func (b *RouteBuilder) ToHTTPFunc[Req any](handler func(http.ResponseWriter, *http.Request, Req) error) {
	registerHTTPFuncHandler(b.core, compileInput[Req](), HTTPHandlerFunc[Req](handler), routeTerminalHTTPFunc, 0)
}

// ToRedirect 注册固定重定向响应。
// ToRedirect registers a fixed redirect response.
func (b *RouteBuilder) ToRedirect(code int, location string) {
	registerRedirectFuncHandler(b.core, compileInput[struct{}](), code, func(struct{}) (string, error) {
		return location, nil
	})
}

// ToRedirectFunc 注册使用解析输入的重定向响应。
// ToRedirectFunc registers a redirect using parsed input.
func (b *RouteBuilder) ToRedirectFunc[Req any](code int, redirect func(Req) (string, error)) {
	registerRedirectFuncHandler(b.core, compileInput[Req](), code, redirect)
}

// ToSSE 注册 Server-Sent Events handler。
// ToSSE registers a Server-Sent Events handler.
func (b *RouteBuilder) ToSSE(handler SSEHandler) {
	b.core.toSSE(handler, nil, nil)
}

// ToWebSocket 注册 WebSocket 升级 handler。
// ToWebSocket registers a WebSocket upgrade handler.
func (b *RouteBuilder) ToWebSocket(handler WebSocketHandler) {
	b.core.toWebSocket(handler, nil, nil)
}

// WebSocketCheckOrigin 覆盖本路由的服务器级 WebSocket origin 校验。
// WebSocketCheckOrigin overrides the server-level origin check for this route.
func (b *RouteBuilder) WebSocketCheckOrigin(check func(*http.Request) bool) *RouteBuilder {
	b.core.setWebSocketCheckOrigin(check)
	return b
}

// ToStatic 以所选路径为 URL 前缀注册安全文件服务器。
// ToStatic registers a safe file server.
func (b *RouteBuilder) ToStatic(root ...string) {
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
	b.core.registerStaticFS(fsys, nil, nil)
}

// ToStaticFS 以所选路径为 URL 前缀注册文件服务器。
// ToStaticFS registers a file server.
func (b *RouteBuilder) ToStaticFS(fs http.FileSystem) {
	b.core.beginTerminal()
	b.core.registerStaticFS(fs, nil, nil)
}

// ToStaticFile 注册单个静态文件。
// ToStaticFile registers a single static file.
func (b *RouteBuilder) ToStaticFile(filepath string) {
	b.core.toHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath)
	}), false, routeTerminalStatic, 0, nil, nil)
}

// ToHTML 用固定数据渲染配置的模板。
// ToHTML renders a configured template with fixed data.
func (b *RouteBuilder) ToHTML(status int, name string, data interface{}) {
	b.core.toHTML(status, name, data, nil, nil)
}
