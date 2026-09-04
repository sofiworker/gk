package ghttp

// ===========================================================================
// 路由 builder 的【非泛型】共享内核。
//
// 它在所有 Go 版本下都编译,但只在 Go ≥ 1.27 下被链起点使用(见 builder_go127.go)——
// 因为「非泛型链 + 泛型终结方法」这一形态依赖 Go 1.27 的泛型方法:类型参数由终结方法
// 自己声明并从 handler 推断,链本身无须携带任何类型参数。Go 1.27 之前类型参数只能在
// 链的起点声明,那会迫使调用方手写 Req/Resp,故低版本仍走 typed.go 的自由函数入口。
//
// 本文件只保存链上累积的注册期意图(动词、路径、中间件),不含任何类型参数,也不触碰
// 请求热路径——终结方法一经调用即下沉到 typed.go 现有的 register* 实现,二者共享同一
// 套绑定计划、Content-Type 校验与 OpenAPI 登记逻辑,不存在第二条注册路径。
//
// The NON-GENERIC shared core of the route builder.
//
// It compiles under every Go version but is only used by the chain starters under
// Go >= 1.27 (see builder_go127.go), because the "non-generic chain + generic
// terminal method" shape depends on Go 1.27 generic methods: the terminal declares
// its own type parameters and infers them from the handler, so the chain itself
// carries none. Before Go 1.27 type parameters could only be declared at the start
// of the chain, forcing callers to spell out Req/Resp by hand; older versions
// therefore keep using the free-function entries in typed.go.
//
// This file holds only the registration-time intent accumulated along the chain
// (verb, path, middleware). It carries no type parameters and never touches the
// request hot path — once a terminal is called it delegates to the existing
// register* implementations in typed.go, so both forms share one binding plan,
// one Content-Type check, and one OpenAPI registry; there is no second
// registration path.
// ===========================================================================

// routeBuilderCore 是链上累积的注册期状态。它有意保持为纯数据:不持有 handler、
// 不持有类型信息,因此可以完全脱离泛型存在。
//
// r 保存【原始】的 router(*mux 或 *Group),终结时原样传给 register*。这一点是必须的:
// noteRoute 通过 r.(*Group) 类型断言推导分组前缀与 OpenAPI 标签,若在此处用包装类型
// 替换 router,断言会失败,spec 里的路径会静默丢掉分组前缀(见 route_docs.go)。
// 路由级中间件因此不经包装 router 注入,而是作为参数传给 register*。
//
// routeBuilderCore is the registration-time state accumulated along the chain. It
// is deliberately plain data: it holds no handler and no type information, so it
// can exist entirely without generics.
//
// r keeps the ORIGINAL router (*mux or *Group) and hands it to register* verbatim.
// This matters: noteRoute derives the group prefix and OpenAPI tag through an
// r.(*Group) type assertion, so substituting a wrapper router here would make that
// assertion fail and silently drop the group prefix from spec paths (see
// route_docs.go). Route-level middleware is therefore passed to register* as an
// argument rather than injected through a wrapping router.
type routeBuilderCore struct {
	r      router
	method string
	path   string

	// mws 是【本路由】的中间件,折叠位置在分组中间件之内、终端之外,因此顺序语义为
	// 全局 → 分组 → 路由 → 终端,与 gin 一致。
	// mws is THIS route's middleware, folded inside the group's and outside the
	// terminal, giving the gin-compatible order global -> group -> route -> terminal.
	mws []Middleware
}

// use 追加路由级中间件。
// use appends route-level middleware.
func (c *routeBuilderCore) use(mws ...Middleware) {
	c.mws = append(c.mws, mws...)
}
