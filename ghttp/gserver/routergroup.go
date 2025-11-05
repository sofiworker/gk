package gserver

import (
	"net/http"
	"regexp"
)

var (
	// regEnLetter matches english letters for http method name.
	regEnLetter = regexp.MustCompile("^[A-Z]+$")

	// anyMethods lists all HTTP methods for the Any method.
	anyMethods = []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodDelete, http.MethodConnect,
		http.MethodTrace,
	}
)

// IRouter defines the complete interface for routing, middleware, and grouping.
type IRouter interface {
	// Middleware
	Use(handlers ...HandlerFunc) IRouter

	// Route registration
	Handle(method, path string, handlers ...HandlerFunc) IRouter
	ANY(path string, handlers ...HandlerFunc) IRouter
	GET(path string, handlers ...HandlerFunc) IRouter
	POST(path string, handlers ...HandlerFunc) IRouter
	DELETE(path string, handlers ...HandlerFunc) IRouter
	PATCH(path string, handlers ...HandlerFunc) IRouter
	PUT(path string, handlers ...HandlerFunc) IRouter
	HEAD(path string, handlers ...HandlerFunc) IRouter
	OPTIONS(path string, handlers ...HandlerFunc) IRouter
	Match(methods []string, path string, handlers ...HandlerFunc) IRouter

	// Grouping
	Group(prefix string, handlers ...HandlerFunc) IRouter

	// Static files
	Static(relativePath, root string) IRouter
	StaticFS(relativePath string, fs http.FileSystem) IRouter
}

// RouterGroup is used to group routes with a common prefix and middlewares.
type RouterGroup struct {
	Handlers []HandlerFunc
	path     string
	engine   *Server
	root     bool
}

// Group creates a new router group. It inherits middlewares from the parent group.
func (g *RouterGroup) Group(relativePath string, handlers ...HandlerFunc) IRouter {
	return &RouterGroup{
		engine:   g.engine,
		path:     g.calculateAbsolutePath(relativePath),
		Handlers: g.combineHandlers(handlers...),
	}
}

// Use adds middleware handlers to the router group for chaining.
func (g *RouterGroup) Use(handlers ...HandlerFunc) IRouter {
	g.Handlers = append(g.Handlers, handlers...)
	return g.returnObj()
}

// Handle registers a new request handler and returns the router for chaining.
func (g *RouterGroup) Handle(method, path string, handlers ...HandlerFunc) IRouter {
	g.addRoute(method, path, handlers...)
	return g.returnObj()
}

// addRoute is the internal method that adds a route.
func (g *RouterGroup) addRoute(method, path string, handlers ...HandlerFunc) {
	if matched := regEnLetter.MatchString(method); !matched {
		panic("http method " + method + " is not valid")
	}
	absolutePath := g.calculateAbsolutePath(path)
	finalHandlers := g.combineHandlers(handlers...)
	g.engine.addRoute(method, absolutePath, finalHandlers...)
}

// GET is a shortcut for Handle(http.MethodGet, path, handlers).
func (g *RouterGroup) GET(path string, handlers ...HandlerFunc) IRouter {
	return g.Handle(http.MethodGet, path, handlers...)
}

// POST is a shortcut for Handle(http.MethodPost, path, handlers).
func (g *RouterGroup) POST(path string, handlers ...HandlerFunc) IRouter {
	return g.Handle(http.MethodPost, path, handlers...)
}

// PUT is a shortcut for Handle(http.MethodPut, path, handlers).
func (g *RouterGroup) PUT(path string, handlers ...HandlerFunc) IRouter {
	return g.Handle(http.MethodPut, path, handlers...)
}

// DELETE is a shortcut for Handle(http.MethodDelete, path, handlers).
func (g *RouterGroup) DELETE(path string, handlers ...HandlerFunc) IRouter {
	return g.Handle(http.MethodDelete, path, handlers...)
}

// PATCH is a shortcut for Handle(http.MethodPatch, path, handlers).
func (g *RouterGroup) PATCH(path string, handlers ...HandlerFunc) IRouter {
	return g.Handle(http.MethodPatch, path, handlers...)
}

// HEAD is a shortcut for Handle(http.MethodHead, path, handlers).
func (g *RouterGroup) HEAD(path string, handlers ...HandlerFunc) IRouter {
	return g.Handle(http.MethodHead, path, handlers...)
}

// OPTIONS is a shortcut for Handle(http.MethodOptions, path, handlers).
func (g *RouterGroup) OPTIONS(path string, handlers ...HandlerFunc) IRouter {
	return g.Handle(http.MethodOptions, path, handlers...)
}

// ANY registers a route that matches all HTTP methods.
func (g *RouterGroup) ANY(path string, handlers ...HandlerFunc) IRouter {
	for _, method := range anyMethods {
		g.addRoute(method, path, handlers...)
	}
	return g.returnObj()
}

// Match registers a route that matches the given HTTP methods.
func (g *RouterGroup) Match(methods []string, path string, handlers ...HandlerFunc) IRouter {
	for _, method := range methods {
		g.addRoute(method, path, handlers...)
	}
	return g.returnObj()
}

// Static serves static files from a directory.
func (g *RouterGroup) Static(relativePath, root string) IRouter {
	return g.engine.Static(g.calculateAbsolutePath(relativePath), root)
}

// StaticFS serves static files from an abstract file system.
func (g *RouterGroup) StaticFS(relativePath string, fs http.FileSystem) IRouter {
	return g.engine.StaticFS(g.calculateAbsolutePath(relativePath), fs)
}

// calculateAbsolutePath calculates the full path for a route, including the group's base path.
func (g *RouterGroup) calculateAbsolutePath(relativePath string) string {
	return JoinPaths(g.path, relativePath)
}

// combineHandlers merges the group's handlers with new handlers.
func (g *RouterGroup) combineHandlers(handlers ...HandlerFunc) []HandlerFunc {
	finalSize := len(g.Handlers) + len(handlers)
	if finalSize == 0 {
		return nil
	}
	mergedHandlers := make([]HandlerFunc, 0, finalSize)
	mergedHandlers = append(mergedHandlers, g.Handlers...)
	mergedHandlers = append(mergedHandlers, handlers...)
	return mergedHandlers
}

// returnObj returns the correct router instance for method chaining.
func (g *RouterGroup) returnObj() IRouter {
	if g.root {
		return g.engine.IRouter
	}
	return g
}
