package ghttp

// Group 持有共享前缀的一组路由。
// Group holds a set of routes with a common prefix.
type Group struct {
	server      *Server
	parent      *Group
	prefix      string
	produces    []string
	consumes    []string
	consumesSet bool
	middlewares []Middleware
}

// Use 追加组级中间件。
// Use appends group-level middleware.
func (g *Group) Use(mws ...Middleware) *Group {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.panicIfFrozenLocked()
	g.middlewares = append(g.middlewares, mws...)
	return g
}

// Produces 声明组内路由的默认响应 Content-Type。
// Produces declares default response Content-Types for this group.
func (g *Group) Produces(contentTypes ...string) *Group {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.panicIfFrozenLocked()
	g.produces = normalizeContentTypes(contentTypes)
	return g
}

// Consumes 声明组内路由的默认请求 Content-Type。
// Consumes declares default request Content-Types for this group.
func (g *Group) Consumes(contentTypes ...string) *Group {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.panicIfFrozenLocked()
	g.consumes = normalizeContentTypes(contentTypes)
	g.consumesSet = true
	return g
}

// Group 创建嵌套路由组。
// Group creates a nested route group.
func (g *Group) Group(prefix string, mws ...Middleware) *Group {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.panicIfFrozenLocked()
	return &Group{
		server:      g.server,
		parent:      g,
		prefix:      joinRoutePaths(g.prefix, prefix),
		produces:    append([]string(nil), g.produces...),
		consumes:    append([]string(nil), g.consumes...),
		consumesSet: g.consumesSet,
		middlewares: append(append([]Middleware(nil), g.middlewares...), mws...),
	}
}

func (g *Group) routePath(path string) string {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	return joinRoutePaths(g.prefix, path)
}

func (g *Group) routeGroup() *Group {
	return g
}

func (g *Group) producesContentTypes() []string {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	if len(g.produces) > 0 {
		return append([]string(nil), g.produces...)
	}
	return append([]string(nil), g.server.producesContentTypes()...)
}

func (g *Group) consumesContentTypes() []string {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	if g.consumesSet {
		return append([]string(nil), g.consumes...)
	}
	return append([]string(nil), g.server.consumesContentTypes()...)
}

func (g *Group) owner() *Server {
	return g.server
}
