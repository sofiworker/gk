package ghttp

import "reflect"

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
	// skipRules 豁免指定中间件在本组路由上执行；按方法+路径模式精确匹配。
	// skipRules exempts the named middlewares for this group's routes; matched by method+path pattern.
	skipRules []skipRule
}

// skipRule 描述一条中间件豁免规则。
// skipRule describes one middleware exemption rule.
// method 为 "*" 时匹配所有方法；pattern 是路由路径模式（含 {param}）。
// method "*" matches any method; pattern is the route path pattern (may contain {param}).
type skipRule struct {
	mw      Middleware
	method  string
	pattern string
}

// skipRuleMatches 判断路由的方法+路径模式是否命中豁免规则。
// skipRuleMatches reports whether a route's method+pattern matches the rule.
func (r skipRule) matches(method, pattern string) bool {
	if r.method != "*" && r.method != method {
		return false
	}
	return r.pattern == pattern
}

// SkipUse 豁免指定中间件在本组匹配路由上执行（精确路径）。豁免采用
// 闭包指针比较,必须与 Use 传入完全同一实例（如 `mw := RequestID();
// g.Use(mw); g.SkipUse(mw, ...)`）——各自调用工厂函数产生的新闭包无法豁免。
// SkipUse exempts the middleware on this group's matching route (exact pattern).
// The exemption compares closure pointers, so the instance must be the exact
// one passed to Use (e.g. `mw := RequestID(); g.Use(mw); g.SkipUse(mw, ...)`).
// Distinct factory-call closures cannot exempt each other.
// 典型用途：全局鉴权中间件放行登录/健康检查路由。
// Typical use: exempt an auth middleware for login and health routes.
func (g *Group) SkipUse(mw Middleware, method, pattern string) *Group {
	g.server.mu.Lock()
	defer g.server.mu.Unlock()
	g.server.panicIfFrozenLocked()
	g.skipRules = append(g.skipRules, skipRule{mw: mw, method: method, pattern: pattern})
	return g
}

// filterSkippedMiddlewares 按豁免规则剔除中间件。
// filterSkippedMiddlewares drops middlewares matching any exemption rule.
func filterSkippedMiddlewares(middlewares []Middleware, method, pattern string, rules []skipRule) []Middleware {
	if len(rules) == 0 {
		return middlewares
	}
	filtered := make([]Middleware, 0, len(middlewares))
	for _, mw := range middlewares {
		skip := false
		for _, rule := range rules {
			// 需用函数指针比较识别同一个中间件（buffalo 同款机制）。
			// function pointer comparison identifies the same middleware (same as buffalo).
			if rule.mw != nil && sameMiddleware(rule.mw, mw) && rule.matches(method, pattern) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, mw)
		}
	}
	return filtered
}

// sameMiddleware 判断两个中间件是否同一函数（按反射函数指针比较）。
// sameMiddleware reports whether two middlewares are the same function (reflect pointer comparison).
func sameMiddleware(a, b Middleware) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
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
