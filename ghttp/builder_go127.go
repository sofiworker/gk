//go:build go1.27

package ghttp

import (
	"context"
	"net/http"
)

// ===========================================================================
// Go ≥ 1.27 的链式注册入口:非泛型链 + 泛型终结方法。
//
// 依赖 Go 1.27 的泛型方法(spec: MethodDecl 允许 TypeParameters)。终结方法自己声明
// 类型参数并从 handler 推断,因此链上不出现任何类型参数,调用方也无须手写 Req/Resp:
//
//	s.Get("/items/{id}").To(JSON[Resp](), func(ctx, p Params) (Resp, error) { … })
//	s.Post("/items").ToBody(JSONBody[Req](), JSON[Resp]().Status(201), handler)
//	s.Get("/healthz").ToNone(JSON[Health](), handler)
//
// 【为何链起点与终结方法必须同处本文件】
// 若把非泛型的链起点(Get/Post/…)放进无构建标签的文件、只把泛型终结方法留在这里,
// 那么 Go < 1.27 的用户能成功拿到一个 builder,却找不到任何终结方法,得到一条永远
// 走不到终点的悬空链,编译错误还只是 "has no field or method To",极难理解。二者同处
// 一个 //go:build go1.27 文件,可保证低版本下这套 API 【整体】不存在,用户看到的是
// 明确的 "has no field or method Get",自然回退到 typed.go 的自由函数入口。
//
// 本文件只做参数搬运:所有终结方法都下沉到 typed.go 的 register*,与自由函数入口共享
// 同一套绑定计划、严格 Content-Type 校验与 OpenAPI 登记,请求热路径完全不受影响。
//
// The chained registration entries for Go >= 1.27: a non-generic chain with generic
// terminal methods.
//
// This relies on Go 1.27 generic methods (spec: MethodDecl permits TypeParameters).
// A terminal declares its own type parameters and infers them from the handler, so
// the chain carries none and callers never spell out Req/Resp.
//
// WHY THE CHAIN STARTERS MUST LIVE IN THIS FILE TOO:
// putting the non-generic starters (Get/Post/...) in an untagged file while keeping
// only the generic terminals here would let a Go < 1.27 user obtain a builder that
// has no terminal at all — a dangling chain whose compile error is merely "has no
// field or method To", which is very hard to understand. Keeping both behind one
// //go:build go1.27 file makes the whole API absent on older versions, so the user
// gets a clear "has no field or method Get" and naturally falls back to the
// free-function entries in typed.go.
//
// This file only shuffles arguments: every terminal delegates to typed.go's
// register*, sharing one binding plan, one strict Content-Type check, and one
// OpenAPI registry with the free-function entries; the request hot path is
// untouched.
// ===========================================================================

// RouteBuilder 是一条链式路由注册的中间态:非泛型,只累积动词、路径与路由级中间件,
// 由泛型终结方法(To/ToBody/ToNone/ToRaw)完成注册。
//
// 它总是由动词方法(s.Get(path) 等)构造,零值不可用。每个 builder 只应终结一次;
// 终结方法返回注册错误,与自由函数入口的错误语义完全一致(ErrMissingOutput、
// ErrMissingCodec、ErrDuplicateRoute 等)。
//
// RouteBuilder is the intermediate state of a chained route registration: it is
// non-generic and merely accumulates the verb, path, and route-level middleware,
// while the generic terminals (To/ToBody/ToNone/ToRaw) perform the registration.
//
// It is always constructed by a verb method (s.Get(path) and friends); the zero
// value is unusable. Each builder should be finalized once. Terminals return the
// registration error with exactly the same semantics as the free-function entries
// (ErrMissingOutput, ErrMissingCodec, ErrDuplicateRoute, ...).
type RouteBuilder struct {
	core routeBuilderCore
}

// newRouteBuilder 用给定 router 与动词开启一条链。
// newRouteBuilder starts a chain with the given router and verb.
func newRouteBuilder(r router, method, path string) *RouteBuilder {
	return &RouteBuilder{core: routeBuilderCore{r: r, method: method, path: path}}
}

// ——— 链起点:定义在 *mux 上,经嵌入提升为 *Server 的方法 ——
// mux 是 Server 的首个嵌入字段,故这些方法自动成为 Server 的方法,无需手写委托。
// Chain starters on *mux, promoted onto *Server: mux is Server's first embedded
// field, so these become Server's methods with no hand-written delegation.

// Get 开启一条 GET 路由链。
// Get starts a GET route chain.
func (m *mux) Get(path string) *RouteBuilder { return newRouteBuilder(m, http.MethodGet, path) }

// Post 开启一条 POST 路由链。
// Post starts a POST route chain.
func (m *mux) Post(path string) *RouteBuilder { return newRouteBuilder(m, http.MethodPost, path) }

// Put 开启一条 PUT 路由链。
// Put starts a PUT route chain.
func (m *mux) Put(path string) *RouteBuilder { return newRouteBuilder(m, http.MethodPut, path) }

// Patch 开启一条 PATCH 路由链。
// Patch starts a PATCH route chain.
func (m *mux) Patch(path string) *RouteBuilder { return newRouteBuilder(m, http.MethodPatch, path) }

// Delete 开启一条 DELETE 路由链。
// Delete starts a DELETE route chain.
func (m *mux) Delete(path string) *RouteBuilder { return newRouteBuilder(m, http.MethodDelete, path) }

// Head 开启一条 HEAD 路由链。HEAD 语义上无响应体,通常配 NoContent。
// Head starts a HEAD route chain. HEAD carries no body by definition; pair it with
// NoContent.
func (m *mux) Head(path string) *RouteBuilder { return newRouteBuilder(m, http.MethodHead, path) }

// Options 开启一条 OPTIONS 路由链(业务化 OPTIONS;标准预检由 CORS 中间件处理)。
// Options starts an OPTIONS route chain (business-level OPTIONS; standard preflight
// is handled by the CORS middleware).
func (m *mux) Options(path string) *RouteBuilder {
	return newRouteBuilder(m, http.MethodOptions, path)
}

// Method 用自定义 HTTP 方法开启一条路由链(WEBHOOK 等扩展方法)。
// method 非法或为小写标准方法拼写时,终结方法返回 ErrInvalidParam。
// Method starts a route chain with a custom HTTP method (extensions such as
// WEBHOOK). When method is invalid or a lowercase spelling of a standard method,
// the terminal returns ErrInvalidParam.
func (m *mux) Method(method, path string) *RouteBuilder { return newRouteBuilder(m, method, path) }

// ——— 链起点:*Group ——
// Group 不嵌入 mux(它只持有 mux 指针与前缀),故需各自定义一份。
// Chain starters on *Group: it does not embed mux (it holds a mux pointer plus a
// prefix), so it needs its own set.

// Get 开启一条 GET 路由链(路径相对本组前缀)。
// Get starts a GET route chain (the path is relative to this group's prefix).
func (g *Group) Get(path string) *RouteBuilder { return newRouteBuilder(g, http.MethodGet, path) }

// Post 开启一条 POST 路由链(路径相对本组前缀)。
// Post starts a POST route chain (relative to this group's prefix).
func (g *Group) Post(path string) *RouteBuilder { return newRouteBuilder(g, http.MethodPost, path) }

// Put 开启一条 PUT 路由链(路径相对本组前缀)。
// Put starts a PUT route chain (relative to this group's prefix).
func (g *Group) Put(path string) *RouteBuilder { return newRouteBuilder(g, http.MethodPut, path) }

// Patch 开启一条 PATCH 路由链(路径相对本组前缀)。
// Patch starts a PATCH route chain (relative to this group's prefix).
func (g *Group) Patch(path string) *RouteBuilder { return newRouteBuilder(g, http.MethodPatch, path) }

// Delete 开启一条 DELETE 路由链(路径相对本组前缀)。
// Delete starts a DELETE route chain (relative to this group's prefix).
func (g *Group) Delete(path string) *RouteBuilder {
	return newRouteBuilder(g, http.MethodDelete, path)
}

// Head 开启一条 HEAD 路由链(路径相对本组前缀)。
// Head starts a HEAD route chain (relative to this group's prefix).
func (g *Group) Head(path string) *RouteBuilder { return newRouteBuilder(g, http.MethodHead, path) }

// Options 开启一条 OPTIONS 路由链(路径相对本组前缀)。
// Options starts an OPTIONS route chain (relative to this group's prefix).
func (g *Group) Options(path string) *RouteBuilder {
	return newRouteBuilder(g, http.MethodOptions, path)
}

// Method 用自定义 HTTP 方法开启一条路由链(路径相对本组前缀)。
// Method starts a route chain with a custom HTTP method (relative to this group's
// prefix).
func (g *Group) Method(method, path string) *RouteBuilder {
	return newRouteBuilder(g, method, path)
}

// ——— 链上修饰符 ——

// Use 为本路由追加路由级中间件。折叠顺序为全局 → 分组 → 路由 → 终端,与 gin 一致。
// 返回自身以便继续链式调用。
// Use appends route-level middleware to this route. The fold order is
// global -> group -> route -> terminal, matching gin. Returns itself for chaining.
func (b *RouteBuilder) Use(mws ...Middleware) *RouteBuilder {
	b.core.use(mws...)
	return b
}

// ——— 泛型终结方法(Go 1.27 泛型方法)——
// 每个终结方法只写一份,*Server 与 *Group 经同一个 *RouteBuilder 共享。
// Generic terminals (Go 1.27 generic methods): each is written once and shared by
// *Server and *Group through the same *RouteBuilder.

// To 以 params(path/query/header 经 struct tag 绑定)注册端点;P 与 O 从 handler 推断。
// 等价于自由函数 GetParams/PostParams/… 的链式形态。
// To registers an endpoint taking params (path/query/header bound via struct tags);
// P and O are inferred from the handler. It is the chained form of the
// GetParams/PostParams/... free functions.
func (b *RouteBuilder) To[P, O any](out OutputSpec[O], h func(context.Context, P) (O, error)) error {
	return registerParams(b.core.r, b.core.method, b.core.path, out, h, b.core.mws...)
}

// ToNone 注册无 params 无 body 的端点(如健康检查);O 从 handler 推断。
// ToNone registers an endpoint with neither params nor body (health checks and the
// like); O is inferred from the handler.
func (b *RouteBuilder) ToNone[O any](out OutputSpec[O], h func(context.Context) (O, error)) error {
	return registerNone(b.core.r, b.core.method, b.core.path, out, h, b.core.mws...)
}

// ToBody 注册仅带请求体的端点;B 与 O 从 handler 推断。表单文本与上传文件均归请求体
// (用 FormBody[B]())。
// ToBody registers a body-only endpoint; B and O are inferred from the handler. Form
// text and uploaded files both belong to the body (use FormBody[B]()).
func (b *RouteBuilder) ToBody[B, O any](in InputSpec[B], out OutputSpec[O], h func(context.Context, B) (O, error)) error {
	return registerBody(b.core.r, b.core.method, b.core.path, in, out, h, b.core.mws...)
}

// ToParamsBody 注册同时带 params 与请求体的端点;P、B、O 从 handler 推断。
// ToParamsBody registers an endpoint taking both params and a body; P, B, and O are
// inferred from the handler.
func (b *RouteBuilder) ToParamsBody[P, B, O any](in InputSpec[B], out OutputSpec[O], h func(context.Context, P, B) (O, error)) error {
	return registerParamsBody(b.core.r, b.core.method, b.core.path, in, out, h, b.core.mws...)
}

// ToRaw 注册完全接管响应的原始处理器(逃生入口)。它不涉及类型契约,故非泛型;
// 与 RawHandle 的区别仅在于可经链挂载路由级中间件。
// ToRaw registers a raw handler that fully owns the response (the escape hatch). It
// involves no type contract and so is not generic; it differs from RawHandle only
// in allowing route-level middleware via the chain.
func (b *RouteBuilder) ToRaw(fn RawHandlerFunc) error {
	if err := b.core.r.register(b.core.method, b.core.path, chain(Handler(fn), b.core.mws)); err != nil {
		return err
	}
	b.core.r.owner().noteRoute(b.core.r, b.core.method, b.core.path, routeDoc{raw: true})
	return nil
}
