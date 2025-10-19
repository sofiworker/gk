package gserver

import (
	"net/http"
	"regexp"
)

var (
	// regEnLetter matches english letters for http method name
	regEnLetter = regexp.MustCompile("^[A-Z]+$")

	// anyMethods for RouterGroup Any method
	anyMethods = []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodDelete, http.MethodConnect,
		http.MethodTrace,
	}
)

type Router interface {
	Handle(method, path string, handlers ...HandlerFunc)
	GET(path string, handlers ...HandlerFunc)
	POST(path string, handlers ...HandlerFunc)
	PUT(path string, handlers ...HandlerFunc)
	DELETE(path string, handlers ...HandlerFunc)
	PATCH(path string, handlers ...HandlerFunc)
	HEAD(path string, handlers ...HandlerFunc)
	OPTIONS(path string, handlers ...HandlerFunc)
	ANY(path string, handlers ...HandlerFunc)
}

type Routers interface {
	Router
	Group(prefix string, handlers ...HandlerFunc) Routers
}

type RouterGroup struct {
	Handlers []HandlerFunc
	basePath string
	engine   *Server
	root     bool
}

func (g *RouterGroup) Group(relativePath string, handlers ...HandlerFunc) Routers {
	return &RouterGroup{
		engine:   g.engine,
		basePath: g.calculateAbsolutePath(relativePath),
		Handlers: g.copyHandler(handlers...),
	}
}

func (g *RouterGroup) Handle(method, path string, handlers ...HandlerFunc) {
	if matched := regEnLetter.MatchString(method); !matched {
		panic("http method " + method + " is not valid")
	}
	absolutePath := g.calculateAbsolutePath(path)
	finalHandlers := g.combineHandlers(handlers...)
	g.engine.addRoute(method, absolutePath, finalHandlers...)
}

func (g *RouterGroup) GET(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodGet, path, handlers...)
}

func (g *RouterGroup) POST(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPost, path, handlers...)
}

func (g *RouterGroup) PUT(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPut, path, handlers...)
}

func (g *RouterGroup) DELETE(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodDelete, path, handlers...)
}

func (g *RouterGroup) PATCH(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPatch, path, handlers...)
}

func (g *RouterGroup) HEAD(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodHead, path, handlers...)
}

func (g *RouterGroup) OPTIONS(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodOptions, path, handlers...)
}

func (g *RouterGroup) ANY(path string, handlers ...HandlerFunc) {
	for _, method := range anyMethods {
		g.Handle(method, path, handlers...)
	}
}

func (g *RouterGroup) calculateAbsolutePath(relativePath string) string {
	if relativePath == "" {
		return g.basePath
	}
	if relativePath[0] == '/' {
		return JoinPaths("", relativePath)
	}
	return JoinPaths(g.basePath, relativePath)
}

func (g *RouterGroup) copyHandler(handlers ...HandlerFunc) []HandlerFunc {
	copyHandlers := make([]HandlerFunc, len(g.Handlers))
	copy(copyHandlers, g.Handlers)
	if len(handlers) == 0 {
		return copyHandlers
	}
	return append(copyHandlers, handlers...)
}

func (g *RouterGroup) combineHandlers(handlers ...HandlerFunc) []HandlerFunc {
	length := len(g.Handlers) + len(handlers)
	if length == 0 {
		return nil
	}
	finalHandlers := make([]HandlerFunc, 0, length)
	if len(g.Handlers) > 0 {
		finalHandlers = append(finalHandlers, g.Handlers...)
	}
	if len(handlers) > 0 {
		finalHandlers = append(finalHandlers, handlers...)
	}
	return finalHandlers
}
