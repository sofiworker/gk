package ghttp

import "net/http"

// Group holds a set of routes with a common prefix.
type Group struct {
	server      *Server
	prefix      string
	middlewares []MiddlewareFunc
}

// Use appends group-level middleware.
func (g *Group) Use(mws ...MiddlewareFunc) *Group {
	g.middlewares = append(g.middlewares, mws...)
	return g
}

// Group creates a nested route group.
func (g *Group) Group(prefix string, mws ...MiddlewareFunc) *Group {
	groupMiddlewares := make([]MiddlewareFunc, 0, len(g.middlewares)+len(mws))
	groupMiddlewares = append(groupMiddlewares, g.middlewares...)
	groupMiddlewares = append(groupMiddlewares, mws...)
	return &Group{
		server:      g.server,
		prefix:      JoinPaths(g.prefix, prefix),
		middlewares: groupMiddlewares,
	}
}

// Handle registers a raw http.Handler under the group prefix.
func (g *Group) Handle(method, path string, handler http.Handler) error {
	return g.server.Handle(method, JoinPaths(g.prefix, path), handler, g.middlewares...)
}

// Raw registers a RawHandler under the group prefix.
func (g *Group) Raw(method, path string, handler RawHandler) error {
	return g.Handle(method, path, http.HandlerFunc(handler))
}

// GroupRoute creates a generic route builder under a group.
func GroupRoute[Req, Resp any](g *Group, path string) *RouteBuilder[Req, Resp] {
	b := Route[Req, Resp](g.server, JoinPaths(g.prefix, path))
	b.addMiddlewares(g.middlewares...)
	return b
}

// GroupGet registers a GET route under a group.
func GroupGet[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Get(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

// GroupHead registers a HEAD route under a group.
func GroupHead[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Head(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

// GroupPost registers a POST route under a group.
func GroupPost[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Post(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

// GroupPut registers a PUT route under a group.
func GroupPut[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Put(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

// GroupDelete registers a DELETE route under a group.
func GroupDelete[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Delete(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

// GroupPatch registers a PATCH route under a group.
func GroupPatch[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Patch(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

// GroupConnect registers a CONNECT route under a group.
func GroupConnect[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Connect(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

// GroupOptions registers an OPTIONS route under a group.
func GroupOptions[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Options(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

// GroupTrace registers a TRACE route under a group.
func GroupTrace[Req, Resp any](g *Group, path string, handler HandlerFunc[Req, Resp], opts ...RouteOption) {
	Trace(g.server, JoinPaths(g.prefix, path), handler, g.routeOptions(opts)...)
}

func (g *Group) routeOptions(opts []RouteOption) []RouteOption {
	groupMiddlewares := append([]MiddlewareFunc(nil), g.middlewares...)
	routeOptions := make([]RouteOption, 0, len(opts)+1)
	routeOptions = append(routeOptions, withRouteMiddlewares(groupMiddlewares...))
	routeOptions = append(routeOptions, opts...)
	return routeOptions
}
