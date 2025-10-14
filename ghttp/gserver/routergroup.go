package gserver

type RouterGroup struct {
	//Handlers HandlersChain
	basePath string
	engine   *Server
	root     bool
}

func (g *RouterGroup) GET(path string, handlers ...HandlerFunc) {
	g.Handle("GET", path, handlers...)
}

func (g *RouterGroup) POST(path string, handlers ...HandlerFunc) {
	g.Handle("POST", path, handlers...)
}

func (g *RouterGroup) PUT(path string, handlers ...HandlerFunc) {
	g.Handle("PUT", path, handlers...)
}

func (g *RouterGroup) DELETE(path string, handlers ...HandlerFunc) {
	g.Handle("DELETE", path, handlers...)
}

func (g *RouterGroup) PATCH(path string, handlers ...HandlerFunc) {
	g.Handle("PATCH", path, handlers...)
}

func (g *RouterGroup) HEAD(path string, handlers ...HandlerFunc) {
	g.Handle("HEAD", path, handlers...)
}

func (g *RouterGroup) OPTIONS(path string, handlers ...HandlerFunc) {
	g.Handle("OPTIONS", path, handlers...)
}

func (g *RouterGroup) ANY(path string, handlers ...HandlerFunc) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for _, method := range methods {
		g.Handle(method, path, handlers...)
	}
}

//	func (g *RouterGroup) Group(prefix string, middleware ...Middleware) *RouterGroup {
//		return &RouterGroup{
//			router:     g.router,
//			prefix:     g.prefix + prefix,
//			middleware: append(g.middleware, middleware...),
//		}
//	}
//
//	func (g *RouterGroup) Use(middleware ...Middleware) {
//		g.middleware = append(g.middleware, middleware...)
//	}
func (g *RouterGroup) Handle(method, path string, handlers ...HandlerFunc) {
	//fullPath := g.prefix + path
	//fastHandler := g.router.wrapHandlers(handlers...)
	//g.router.matcher.AddRoute(method, fullPath, fastHandler, g.middleware...)
}
