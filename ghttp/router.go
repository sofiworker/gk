package ghttp

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
)

// ErrInvalidRoutePath 表示路由路径非法（为空或不以 '/' 开头）。
// ErrInvalidRoutePath indicates an invalid route path (empty or not starting with '/').
var ErrInvalidRoutePath = errors.New("ghttp: route path must begin with '/'")

// ErrRouteConflict 表示路由与已注册路由冲突或通配符定义非法。
// ErrRouteConflict indicates a route conflicts with a registered route or has an invalid wildcard.
var ErrRouteConflict = errors.New("ghttp: route conflict")

// router 管理所有路由，每个 HTTP method 一棵 Gin 风格 radix tree。
// router manages all routes with one Gin-style radix tree per HTTP method.
//
// 注册非并发安全，由 Server.mu 保护；查找在启动后只读，可并发。
// Registration is not concurrency-safe and is guarded by Server.mu; lookups are read-only after start.
type router struct {
	// trees 按 HTTP 方法存储路由树
	// trees stores route trees by HTTP method
	trees    map[string]*radixNode
	catchAll map[string][]catchAllRoute

	// maxParams 是所有路由中参数数量的最大值，用于预分配
	// maxParams is the maximum param count across routes, used for preallocation
	maxParams uint16

	// skippedPool 复用回溯栈，避免每次查找分配
	// skippedPool reuses backtracking stacks to avoid per-lookup allocation
	skippedPool sync.Pool
}

type catchAllRoute struct {
	prefix  string
	name    string
	path    string
	handler Handler
}

// routeMatch 是一次路由查找的结果。
// routeMatch is the result of a route lookup.
type routeMatch struct {
	// handler 是匹配到的处理器，未匹配时为 nil
	// handler is the matched handler, nil when not matched
	handler Handler

	// params 是路径参数
	// params are the path parameters
	params Params

	// fullPath 是匹配到的路由模板
	// fullPath is the matched route template
	fullPath string

	// tsr 表示仅差一个尾斜杠即可匹配（Trailing Slash Redirect）
	// tsr reports that the path would match with/without a trailing slash
	tsr bool
}

// newRouter 创建新路由器。
// newRouter creates a new router.
func newRouter() *router {
	return &router{
		trees:    make(map[string]*radixNode),
		catchAll: make(map[string][]catchAllRoute),
	}
}

// addRoute 添加路由。
// addRoute adds a route.
//
// 冲突、重复注册或非法通配符返回包装了 ErrRouteConflict 的错误。
// Conflicts, duplicates or invalid wildcards return an error wrapping ErrRouteConflict.
func (r *router) addRoute(route Route) error {
	path, err := canonicalRoutePath(route.Path)
	if err != nil {
		return err
	}
	if route.compiledHandler == nil {
		return fmt.Errorf("ghttp: nil handler for %s %s", route.Method, path)
	}

	root, ok := r.trees[route.Method]
	if !ok {
		root = &radixNode{fullPath: "/"}
	}

	// radix tree 沿用 Gin 实现，在冲突时 panic；在此转换为错误。
	// The radix tree follows Gin and panics on conflicts; convert to an error here.
	if err := addRadixRoute(root, path, route.compiledHandler); err != nil {
		if strings.Contains(path, "*") && strings.Contains(err.Error(), "conflicts with existing wildcard") {
			i := strings.IndexByte(path, '*')
			for _, existing := range r.catchAll[route.Method] {
				if existing.path == path {
					return fmt.Errorf("%w: route already registered for %s %s", ErrRouteConflict, route.Method, path)
				}
			}
			r.catchAll[route.Method] = append(r.catchAll[route.Method], catchAllRoute{
				prefix: path[:i], name: path[i+1:], path: path, handler: route.compiledHandler,
			})
			return nil
		}
		return err
	}

	// 仅在插入成功后登记新树，避免失败时留下空树
	// Only record a new tree after a successful insert to avoid leaving an empty tree
	if !ok {
		r.trees[route.Method] = root
	}
	if n := countParams(path); n > r.maxParams {
		r.maxParams = n
	}
	return nil
}

func addRadixRoute(root *radixNode, path string, handler Handler) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%w: %v", ErrRouteConflict, rec)
		}
	}()
	root.addRoute(path, handler)
	return nil
}

// canonicalRoutePath 校验路由路径以 '/' 开头，并把模板写法转换为 Gin 写法。
// canonicalRoutePath checks the route path starts with '/' and converts template syntax
// to Gin syntax.
func canonicalRoutePath(p string) (string, error) {
	if p == "" || p[0] != '/' {
		return "", fmt.Errorf("%w: %q", ErrInvalidRoutePath, p)
	}
	return normalizeRoutePath(p)
}

// lookup 在指定方法的树中查找路径。
// lookup looks up a path in the tree of the given method.
func (r *router) lookup(method, path string) routeMatch {
	return r.lookupWithParams(method, path, nil)
}

// lookupNoParams matches a route when the caller only needs to know whether a
// handler exists (for example, while building Allow for a 405). It deliberately
// does not create a Params slice, so a miss does not escape a local buffer.
func (r *router) lookupNoParams(method, path string) routeMatch {
	root := r.trees[method]
	if root == nil {
		return r.lookupCatchAllNoParams(method, path)
	}
	if r.maxParams == 0 {
		value := root.getValue(path, nil, nil, false)
		return routeMatch{handler: value.handler, fullPath: value.fullPath, tsr: value.tsr}
	}
	skipped := r.getSkipped()
	value := root.getValue(path, nil, skipped, false)
	r.putSkipped(skipped)
	m := routeMatch{handler: value.handler, fullPath: value.fullPath, tsr: value.tsr}
	if m.handler == nil {
		if fallback := r.lookupCatchAll(method, path); fallback.handler != nil {
			return fallback
		}
	}
	return m
}

func (r *router) lookupCatchAllNoParams(method, requestPath string) routeMatch {
	var best *catchAllRoute
	for i := range r.catchAll[method] {
		candidate := &r.catchAll[method][i]
		if !strings.HasPrefix(requestPath, candidate.prefix) {
			continue
		}
		if strings.HasSuffix(candidate.prefix, "/") {
			if len(requestPath) <= len(candidate.prefix)-1 {
				continue
			}
		} else if len(requestPath) <= len(candidate.prefix) || requestPath[len(candidate.prefix)] != '/' {
			continue
		}
		if best == nil || len(candidate.prefix) > len(best.prefix) {
			best = candidate
		}
	}
	if best == nil {
		return routeMatch{}
	}
	return routeMatch{handler: best.handler, fullPath: best.path}
}

func (r *router) lookupWithParams(method, path string, params *Params) routeMatch {
	root := r.trees[method]
	if root == nil {
		return r.lookupCatchAll(method, path)
	}

	var value nodeValue
	if r.maxParams == 0 {
		value = root.getValue(path, nil, nil, false)
	} else {
		if params == nil {
			var local Params
			params = &local
		}
		skipped := r.getSkipped()
		value = root.getValue(path, params, skipped, false)
		r.putSkipped(skipped)
	}

	m := routeMatch{handler: value.handler, fullPath: value.fullPath, tsr: value.tsr}
	if value.handler != nil && value.params != nil {
		m.params = *value.params
	}
	if m.handler == nil {
		if fallback := r.lookupCatchAll(method, path); fallback.handler != nil {
			return fallback
		}
	}
	return m
}

func (r *router) lookupCatchAll(method, requestPath string) routeMatch {
	var best *catchAllRoute
	for i := range r.catchAll[method] {
		candidate := &r.catchAll[method][i]
		if !strings.HasPrefix(requestPath, candidate.prefix) {
			continue
		}
		if strings.HasSuffix(candidate.prefix, "/") {
			if len(requestPath) <= len(candidate.prefix)-1 {
				continue
			}
		} else if len(requestPath) <= len(candidate.prefix) || requestPath[len(candidate.prefix)] != '/' {
			continue
		}
		if best == nil || len(candidate.prefix) > len(best.prefix) {
			best = candidate
		}
	}
	if best == nil {
		return routeMatch{}
	}
	valueStart := len(best.prefix)
	if strings.HasSuffix(best.prefix, "/") {
		valueStart--
	}
	return routeMatch{handler: best.handler, fullPath: best.path, params: Params{{Key: best.name, Value: requestPath[valueStart:]}}}
}

// match 匹配路由；HEAD 请求在没有显式 HEAD 路由时回退到 GET。
// match matches a route; HEAD falls back to GET when no explicit HEAD route exists.
//
// 返回 handler、参数、匹配的路径模板。
// Returns handler, parameters, matched path template.
func (r *router) match(method, path string) (Handler, Params, string) {
	m := r.find(method, path)
	return m.handler, m.params, m.fullPath
}

// find 匹配路由并保留 TSR 信息；HEAD 在无显式路由时回退到 GET。
// find matches a route and keeps TSR info; HEAD falls back to GET without an explicit route.
func (r *router) find(method, path string) routeMatch {
	return r.findWithParams(method, path, nil)
}

func (r *router) findWithParams(method, path string, params *Params) routeMatch {
	m := r.lookupWithParams(method, path, params)
	if m.handler == nil && method == http.MethodHead {
		if params != nil {
			*params = (*params)[:0]
		}
		if g := r.lookupWithParams(http.MethodGet, path, params); g.handler != nil || (!m.tsr && g.tsr) {
			return g
		}
	}
	return m
}

// allowed 返回能匹配该路径的方法列表（已排序），用于 405 的 Allow 头。
// allowed returns the sorted methods that match the path, used for the 405 Allow header.
func (r *router) allowed(path string) []string {
	var methods []string
	hasGet, hasHead := false, false
	for method := range r.trees {
		if r.lookupNoParams(method, path).handler == nil {
			continue
		}
		methods = append(methods, method)
		switch method {
		case http.MethodGet:
			hasGet = true
		case http.MethodHead:
			hasHead = true
		}
	}
	for method := range r.catchAll {
		if m := r.lookupCatchAll(method, path); m.handler != nil {
			methods = append(methods, method)
		}
	}
	// GET 隐式提供 HEAD
	// GET implicitly provides HEAD
	if hasGet && !hasHead {
		methods = append(methods, http.MethodHead)
	}
	sort.Strings(methods)
	return methods
}

// getSkipped 从池中获取回溯栈。
// getSkipped gets a backtracking stack from the pool.
func (r *router) getSkipped() *[]skippedNode {
	if v, ok := r.skippedPool.Get().(*[]skippedNode); ok {
		return v
	}
	s := make([]skippedNode, 0, 8)
	return &s
}

// putSkipped 将回溯栈归还池中。
// putSkipped returns a backtracking stack to the pool.
func (r *router) putSkipped(s *[]skippedNode) {
	*s = (*s)[:0]
	r.skippedPool.Put(s)
}

// countParams 统计路径中的参数（':' 与 '*'）数量。
// countParams counts the parameters (':' and '*') in a path.
func countParams(path string) uint16 {
	n := strings.Count(path, ":") + strings.Count(path, "*")
	if n > int(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(n)
}

// tsrPath 返回尾斜杠重定向的目标路径；目标不安全时返回 false。
// tsrPath returns the trailing-slash redirect target; false when the target is unsafe.
//
// 以 "//" 或 "/\" 开头的目标会被浏览器视为协议相对 URL，拒绝以防开放重定向。
// Targets starting with "//" or "/\" are treated by browsers as protocol-relative URLs
// and are rejected to prevent open redirects.
func tsrPath(path string) (string, bool) {
	var target string
	if len(path) > 1 && path[len(path)-1] == '/' {
		target = path[:len(path)-1]
	} else {
		target = path + "/"
	}
	if len(target) > 1 && (target[1] == '/' || target[1] == '\\') {
		return "", false
	}
	return target, true
}

// tsrStatus 返回尾斜杠重定向的状态码：GET/HEAD 用 301，其他方法用 308 以保留方法与请求体。
// tsrStatus returns the TSR status code: 301 for GET/HEAD, 308 otherwise to keep method and body.
func tsrStatus(method string) int {
	if method == http.MethodGet || method == http.MethodHead {
		return http.StatusMovedPermanently
	}
	return http.StatusPermanentRedirect
}

// fixedPath 尝试修正请求路径：先用 path.Clean 清理多余的 '/'，再做大小写不敏感匹配（同时允许
// 修正尾斜杠）。返回修正后的路径；无法修正或修正结果与原路径相同时返回 false。
// HEAD 在没有对应树时使用 GET 树。
// fixedPath tries to repair the request path: it cleans redundant '/' with path.Clean,
// then does a case-insensitive lookup (also fixing the trailing slash). It returns the
// repaired path, or false when nothing can be fixed or the result equals the input. HEAD
// falls back to the GET tree.
func (r *router) fixedPath(method, p string) (string, bool) {
	root := r.trees[method]
	if root == nil && method == http.MethodHead {
		root = r.trees[http.MethodGet]
	}
	if root == nil {
		return "", false
	}
	clean := path.Clean(p)
	if clean != "/" && strings.HasSuffix(p, "/") {
		clean += "/"
	}
	fixed, ok := root.findCaseInsensitivePath(clean, true)
	if !ok || string(fixed) == p {
		return "", false
	}
	return string(fixed), true
}
