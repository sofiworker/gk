package ghttp

import (
	"net/http"
	"reflect"
)

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

// UseFunc appends handler-function middleware to the group.
func (g *Group) UseFunc(mws ...HandlerMiddlewareFunc) *Group {
	for _, mw := range mws {
		g.middlewares = append(g.middlewares, HandlerMiddleware(mw))
	}
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
func (g *Group) Handle(method, path string, handler http.Handler, mws ...MiddlewareFunc) error {
	return g.handleRoute(method, path, handler, mws...)
}

// Raw registers a RawHandler under the group prefix.
func (g *Group) Raw(method, path string, handler RawHandler) error {
	return g.Handle(method, path, http.HandlerFunc(handler))
}

func (g *Group) handleRoute(method, path string, handler http.Handler, mws ...MiddlewareFunc) error {
	all := make([]MiddlewareFunc, 0, len(g.middlewares)+len(mws))
	all = append(all, g.middlewares...)
	all = append(all, mws...)
	return g.server.Handle(method, JoinPaths(g.prefix, path), handler, all...)
}

func (g *Group) addRouteSpec(method, path, doc string, tags []string, operationID string, reqType reflect.Type, responses []responseSpec) {
	g.server.addRouteSpec(method, JoinPaths(g.prefix, path), doc, tags, operationID, reqType, responses)
}

func (g *Group) owner() *Server {
	return g.server
}
