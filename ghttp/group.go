package ghttp

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

// Use appends group-level middleware.
func (g *Group) Use(mws ...Middleware) *Group {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.panicIfFrozenLocked()
	g.middlewares = append(g.middlewares, mws...)
	return g
}

// Produces declares the default response Content-Types for routes in this group.
func (g *Group) Produces(contentTypes ...string) *Group {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.panicIfFrozenLocked()
	g.produces = normalizeContentTypes(contentTypes)
	return g
}

// Consumes declares the default request Content-Types for routes in this group.
func (g *Group) Consumes(contentTypes ...string) *Group {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.panicIfFrozenLocked()
	g.consumes = normalizeContentTypes(contentTypes)
	g.consumesSet = true
	return g
}

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
		middlewares: append([]Middleware(nil), mws...),
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

func (g *Group) currentMiddlewaresLocked() []Middleware {
	if g == nil {
		return nil
	}
	var all []Middleware
	if g.parent != nil {
		all = append(all, g.parent.currentMiddlewaresLocked()...)
	}
	all = append(all, g.middlewares...)
	return all
}
