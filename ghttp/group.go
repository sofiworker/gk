package ghttp

// Group 表示路由分组：共享路径前缀与默认路由选项。
// Group is a route group sharing a path prefix and default route options.
//
// 子组在创建时复制父组的前缀与选项快照，之后父组的变化不影响子组。中间件顺序为：
// Server 全局 → Server.With → 父组 → 子组 → 路由。
// A child group copies the parent's prefix and options when created; later changes to
// the parent do not affect it. Middleware order is: Server global → Server.With →
// parent group → child group → route.
type Group struct {
	// server 是所属的服务器
	// server is the owning server
	server *Server

	// prefix 是路由前缀
	// prefix is the route prefix
	prefix string

	// opts 是组级默认路由选项，按"外层在前"排列
	// opts are the group's default route options, outermost first
	opts []Option
}

// GroupOption 在创建分组时配置分组。
// GroupOption configures a group at creation.
type GroupOption func(*Group)

// WithGroupMiddleware 为分组添加中间件。
// WithGroupMiddleware adds middleware to the group.
func WithGroupMiddleware(middleware ...Middleware) GroupOption {
	return func(g *Group) { g.opts = append(g.opts, WithMiddleware(middleware...)) }
}

// WithGroupOptions 为分组添加默认路由选项（WithInput/WithOutput 除外）。
// WithGroupOptions adds default route options to the group (except WithInput/WithOutput).
func WithGroupOptions(opts ...Option) GroupOption {
	return func(g *Group) { g.opts = append(g.opts, opts...) }
}

// newGroup 创建分组并应用 GroupOption。
// newGroup creates a group and applies the GroupOptions.
func newGroup(s *Server, prefix string, inherited []Option, opts []GroupOption) *Group {
	g := &Group{server: s, prefix: prefix, opts: inherited}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g
}

// Prefix 返回分组的完整路径前缀。
// Prefix returns the group's full path prefix.
func (g *Group) Prefix() string {
	return g.prefix
}

// Register 注册路由到分组：拼接前缀并应用分组的默认选项，语义同 Server.Register。
// Register registers routes in the group, joining the prefix and applying the group's
// defaults; semantics match Server.Register.
func (g *Group) Register(routes ...Route) error {
	return g.server.register(g.prefix, g.cloneOpts(), routes)
}

// Group 创建子分组，继承当前的前缀与选项快照。
// Group creates a child group inheriting the current prefix and options snapshot.
func (g *Group) Group(prefix string, opts ...GroupOption) *Group {
	return newGroup(g.server, joinPaths(g.prefix, prefix), g.cloneOpts(), opts)
}

// Use 添加组级中间件，对之后注册的路由与之后创建的子组生效。
// Use adds group middleware, effective for routes registered and child groups created afterwards.
func (g *Group) Use(middleware ...Middleware) *Group {
	g.opts = append(g.opts, WithMiddleware(middleware...))
	return g
}

// With 返回一个附加了默认路由选项的新分组（前缀相同），原分组不受影响。
// With returns a new group (same prefix) with extra default route options; the receiver
// is unchanged.
func (g *Group) With(opts ...Option) *Group {
	return &Group{server: g.server, prefix: g.prefix, opts: append(g.cloneOpts(), opts...)}
}

// cloneOpts 返回选项切片的副本，避免父子分组共享底层数组。
// cloneOpts returns a copy of the options so parent and child never share a backing array.
func (g *Group) cloneOpts() []Option {
	return append([]Option(nil), g.opts...)
}
