package ghttp

// Group 是路由分组:拼接公共前缀并继承一份中间件快照。它只负责前缀与中间件继承,
// 不承载类型契约——typed 契约仍走 Handle 的参数式(设计 §4.4)。
// Group is a route group: it joins a common prefix and inherits a middleware
// snapshot. It only handles prefixing and middleware inheritance; it carries no
// type contract — the typed contract still flows through Handle's parameters.
type Group struct {
	mux    *Mux
	prefix string
	mws    []Middleware
}

// Group 基于当前 Mux 创建一个子分组:前缀为 mux 级前缀(空)拼接 prefix,中间件为
// 全局中间件的快照再追加 mws。快照发生在创建时(gin 语义):此后对父 Mux 的 Use
// 不影响本组。
// Group creates a child group off the current Mux: its prefix is the mux-level
// prefix (empty) joined with prefix, and its middleware is a snapshot of the
// global middleware plus mws. The snapshot happens at creation time (gin
// semantics): later Use on the parent Mux does not affect this group.
func (m *Mux) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		mux:    m,
		prefix: prefix,
		mws:    snapshotMiddleware(m.mws, mws),
	}
}

// Group 基于已有分组再分组:前缀累加,中间件在父组快照上再追加。
// Group nests a further group off an existing one: prefixes concatenate and
// middleware appends onto the parent group's snapshot.
func (g *Group) Group(prefix string, mws ...Middleware) *Group {
	return &Group{
		mux:    g.mux,
		prefix: g.prefix + prefix,
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

// register 实现 router:把 terminal 折叠上本组中间件栈后,以组前缀 + path 注册到底层
// Mux。注意此处不再叠加 Mux 全局中间件——它们已在 Group 创建时被快照进 g.mws。
// register implements router: fold terminal with this group's middleware stack,
// then register at group-prefix + path on the underlying Mux. The Mux's global
// middleware is NOT added again here — it was snapshotted into g.mws at Group
// creation.
func (g *Group) register(method, path string, terminal Handler) error {
	return g.mux.handle(method, g.prefix+path, chain(terminal, g.mws))
}

// RawHandle 在本分组上注册一个原始处理器,经本组中间件链、挂在组前缀下。
// RawHandle registers a raw handler on this group, wrapped by the group's
// middleware chain and mounted under the group prefix.
func (g *Group) RawHandle(method, path string, fn RawHandlerFunc) error {
	return g.register(method, path, Handler(fn))
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
