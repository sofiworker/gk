package ghttp

// Group 是路由分组:拼接公共前缀并携带一份【仅属于本组】的中间件快照。它只负责前缀与
// 中间件继承,不承载类型契约——typed 契约仍走 Handle 的参数式(设计 §4.4)。
//
// 注意分组中间件与全局中间件的分工:全局中间件(Server.Use)在 ServeHTTP 期统一施加于
// 所有请求,因此【不】进入分组链;分组只折叠自己这一层及父组的中间件。二者相加即该路由
// 的完整链,各执行一次。
//
// Group is a route group: it joins a common prefix and carries a middleware
// snapshot of ITS OWN layers. It only handles prefixing and middleware
// inheritance; it carries no type contract — the typed contract still flows
// through Handle's parameters.
//
// Note the split between group and global middleware: global middleware
// (Server.Use) is applied uniformly at ServeHTTP time to every request and thus
// does NOT enter the group chain; a group folds only its own and its parents'
// layers. Together they form the route's complete chain, each running exactly once.
type Group struct {
	m      *mux
	prefix string
	mws    []Middleware
}

// Group 基于当前 Server 创建一个子分组:前缀为 prefix,中间件为 mws 的独立副本。
// 全局中间件不并入本组(见 Group 的类型注释)。
// Group creates a child group off the current Server: its prefix is prefix and its
// middleware is an independent copy of mws. Global middleware is not merged in
// (see Group's type comment).
func (m *mux) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		m:      m,
		prefix: prefix,
		mws:    snapshotMiddleware(nil, mws),
	}
}

// Group 基于已有分组再分组:前缀经 joinRoutePath 规范化累加(连接处恰好一个 '/'),
// 中间件在父组快照上再追加。快照发生在创建时(gin 语义):此后对父组的 Use 不影响本组。
// Group nests a further group off an existing one: prefixes accumulate through
// joinRoutePath (exactly one '/' at the junction) and middleware appends onto the
// parent group's snapshot. The snapshot happens at creation time (gin semantics):
// later Use on the parent group does not affect this one.
func (g *Group) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		m:      g.m,
		prefix: joinRoutePath(g.prefix, prefix),
		mws:    snapshotMiddleware(g.mws, mws),
	}
}

// Use 向本分组追加中间件(仅影响本组之后注册的路由)。返回自身以便链式调用。
// Use appends middleware to this group (affecting only routes registered on it
// afterwards). Returns itself for chaining.
func (g *Group) Use(mws ...Middleware) *Group {
	g.mws = append(g.mws, mws...)
	return g
}

// register 实现 router:把 terminal 折叠上本组中间件栈后,以【规范化的】组前缀 + path
// 注册。拼接经 joinRoutePath,保证连接处恰好一个 '/'(见其文档说明的边界语义)。
// 全局中间件不在此叠加——它们在 ServeHTTP 期统一施加。
// register implements router: fold terminal with this group's middleware stack, then
// register at the NORMALIZED group-prefix + path. Joining goes through joinRoutePath,
// guaranteeing exactly one '/' at the junction (see its doc for the edge semantics).
// Global middleware is not layered here — it is applied uniformly at ServeHTTP time.
func (g *Group) register(method, path string, terminal Handler) error {
	return g.m.handle(method, joinRoutePath(g.prefix, path), chain(terminal, g.mws))
}

// owner 实现 router:分组的 owner 是其背后的 mux。
// owner implements router: a group's owner is its backing mux.
func (g *Group) owner() *mux { return g.m }

// RawHandle 在本分组上注册一个原始处理器,经本组中间件链、挂在组前缀下。
// 开启 OpenAPI 收集时同样登记(传 g 以取得本组前缀与派生标签),理由见 mux.RawHandle。
// RawHandle registers a raw handler on this group, wrapped by the group's
// middleware chain and mounted under the group prefix. With OpenAPI collection
// enabled it is recorded as well (passing g supplies the group prefix and derived
// tag); see mux.RawHandle for the rationale.
func (g *Group) RawHandle(method, path string, fn RawHandlerFunc) error {
	if err := g.register(method, path, Handler(fn)); err != nil {
		return err
	}
	g.m.noteRoute(g, method, path, routeDoc{raw: true})
	return nil
}

// MustRawHandle registers a raw route and panics when registration fails.
func (g *Group) MustRawHandle(method, path string, fn RawHandlerFunc) {
	if err := g.RawHandle(method, path, fn); err != nil {
		panic(err)
	}
}

// snapshotMiddleware 返回 base 与 extra 拼接后的独立副本,避免共享底层数组导致后续
// append 串扰(gin 快照语义的实现关键)。
// snapshotMiddleware returns an independent copy of base concatenated with
// extra, avoiding a shared backing array whose later append would cross-
// contaminate (the crux of gin's snapshot semantics).
func snapshotMiddleware(base, extra []Middleware) []Middleware {
	out := make([]Middleware, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return out
}
