package ghttp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
)

// Mux 是框架的多路复用器:按 method 持有路由树,并池化每请求上下文。路由的匹配与
// 语义完全对齐 gin(含 TSR 尾斜杠重定向)。
// Mux is the framework multiplexer: it holds per-method routing trees and pools
// per-request contexts. Routing matching and semantics fully align with gin
// (including TSR trailing-slash redirects).
type Mux struct {
	trees []*methodTree
	pool  sync.Pool

	// mws 是全局中间件栈,注册路由时折叠进链;须在注册路由前经 Use 追加(gin 同款约束)。
	// mws is the global middleware stack, folded into the chain at route
	// registration; append via Use before registering routes (gin's constraint).
	mws []Middleware

	// strictPath 决定请求路径校验强度。默认 false(快速模式):仅拦截 dot 段
	// (防路径遍历),用一次 SIMD 字节扫描粗筛,空段放行交给匹配(对齐 gin)。
	// 置 true(严格模式):完整校验 dot 段 + 空段,任一非法即 400。
	// strictPath selects request-path validation strength. Default false (fast
	// mode): only dot segments are rejected (path-traversal guard) via a single
	// SIMD byte scan, empty segments pass through to matching (aligned with gin).
	// Set true (strict mode): full validation of dot and empty segments, any
	// illegal one yields 400.
	strictPath bool
}

// Option 配置 Mux。
// Option configures a Mux.
type Option func(*Mux)

// WithStrictPath 开启严格路径校验:dot 段与空段均返回 400。默认关闭(快速模式,
// 仅拦 dot 段以防路径遍历,空段交给匹配,与 gin 一致的高性能取舍)。
// WithStrictPath enables strict path validation: both dot and empty segments
// yield 400. Off by default (fast mode: only dot segments are rejected as a
// path-traversal guard, empty segments pass to matching — the gin-aligned
// high-performance tradeoff).
func WithStrictPath(strict bool) Option {
	return func(m *Mux) { m.strictPath = strict }
}

// New 构造一个空的 Mux;可选 Option 调整行为。
// New constructs an empty Mux; optional Options adjust behavior.
func New(opts ...Option) *Mux {
	m := &Mux{}
	m.pool.New = func() any { return &Request{} }
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// treeFor 返回 method 对应的路由树,不存在则创建。
// treeFor returns the routing tree for method, creating it if absent.
func (m *Mux) treeFor(method string) *methodTree {
	if t := m.findTree(method); t != nil {
		return t
	}
	t := &methodTree{method: method, root: &routeNode{}}
	m.trees = append(m.trees, t)
	return t
}

// findTree 返回已存在的 method 树,没有则返回 nil。
// findTree returns the existing tree for method, or nil.
func (m *Mux) findTree(method string) *methodTree {
	for _, t := range m.trees {
		if t.method == method {
			return t
		}
	}
	return nil
}

// Use 追加全局中间件,按追加顺序执行(先加先执行)。须在注册路由前调用:中间件在
// 注册期折叠进每条路由的链,已注册的路由不受其后 Use 影响(gin 同款语义)。
// Use appends global middleware, executed in append order (first added runs
// first). Call it before registering routes: middleware is folded into each
// route's chain at registration time, so routes already registered are
// unaffected by a later Use (gin's semantics).
func (m *Mux) Use(mws ...Middleware) *Mux {
	m.mws = append(m.mws, mws...)
	return m
}

// register 实现 router:把 terminal 折叠上全局中间件栈后注册到 method + path。
// register implements router: fold terminal with the global middleware stack,
// then register at method + path.
func (m *Mux) register(method, path string, terminal Handler) error {
	return m.handle(method, path, chain(terminal, m.mws))
}

// handle 将一个已编译的 handler 注册到 method + path。path 先由模板语法翻译为 gin
// 形式(:name / *name)再插入。
// handle registers a compiled handler for method + path. The path is first
// translated from template syntax into gin form (:name / *name) before insertion.
func (m *Mux) handle(method, path string, h compiledHandler) error {
	ginPath, err := translateTemplate(path)
	if err != nil {
		return err
	}
	return m.treeFor(method).insert(ginPath, h)
}

// ServeHTTP 实现 http.Handler:校验路径 → 匹配(getValue)→ 命中则取还池化上下文并
// 执行;未命中时按 gin 语义处理 TSR(301)、405(+Allow)、404。
// ServeHTTP implements http.Handler: validate the path → match (getValue) → on a
// hit borrow and return a pooled context and execute; on a miss handle TSR (301),
// 405 (+Allow), and 404 per gin semantics.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 在【已解码】的 URL.Path 上匹配(与 gin 一致):%XX 已被标准库解码,故 %62 天然
	// 等于字面量 "b",indices 首字节表可直接用。已知取舍:参数值中的 %2F 被当作段
	// 分隔符(与 gin 相同)。
	// Match on the DECODED URL.Path (like gin): %XX is decoded by the stdlib, so
	// %62 equals literal "b" and the indices table applies directly. Known
	// tradeoff: a %2F inside a param value is treated as a separator (as in gin).
	path := r.URL.Path
	if err := validateRequestPath(path, m.strictPath); err != nil {
		// 路径非法(dot 段;严格模式下还含空段) → 400,仅写状态码(零分配)。
		// Invalid path (dot segment; also empty segments in strict mode) → 400,
		// status only (allocation-free).
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	t := m.findTree(r.Method)
	if t == nil {
		m.handleMiss(w, r, path, false)
		return
	}

	req := m.pool.Get().(*Request)
	req.Request = r
	req.Params.reset()
	req.skipped = req.skipped[:0]

	v := t.root.getValue(path, &req.Params, &req.skipped)
	if v.handler == nil {
		tsr := v.tsr
		req.Request = nil
		m.pool.Put(req)
		m.handleMiss(w, r, path, tsr)
		return
	}

	// 复用随 Request 池化的 Response,免去每请求 &Response{} 堆分配;
	// 先 reset 清掉上一请求残留的 status/written,再挂上本请求的 writer。
	// Reuse the pooled Response, avoiding a per-request &Response{} heap alloc;
	// reset the leftover status/written first, then attach this request's writer.
	req.resp.reset()
	req.resp.ResponseWriter = w
	serr := m.serve(v.handler, r.Context(), req, &req.resp)

	req.resp.reset()
	req.Request = nil
	m.pool.Put(req)

	if serr != nil {
		// TODO(stage-4): 接入统一错误链(415 / 业务错误 / panic)。
		// TODO(stage-4): route into the unified error chain.
		http.Error(w, serr.Error(), http.StatusInternalServerError)
	}
}

// handleMiss 处理当前 method 未命中:优先 TSR(301 到 增删尾斜杠的等价路径,与 gin
// 一致),其次 405+Allow(其它 method 有同路径),否则 404。
// handleMiss handles a miss for the current method: prefer TSR (301 to the
// equivalent path with the trailing slash toggled, as in gin), then 405 + Allow
// (another method has the same path), otherwise 404.
func (m *Mux) handleMiss(w http.ResponseWriter, r *http.Request, path string, tsr bool) {
	if tsr && path != "/" {
		redirectTrailingSlash(w, r, path)
		return
	}
	if allow := m.allowedMethods(path); allow != "" {
		w.Header().Set("Allow", allow)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// redirectTrailingSlash 对存在等价路由的路径发 301:有尾斜杠则去掉,无则补上
// (与 gin 的 redirectTrailingSlash 行为一致)。GET 用 301,其它安全起见也用 301。
// redirectTrailingSlash issues a 301 to the equivalent route: strip the trailing
// slash if present, otherwise append one (matching gin's redirectTrailingSlash).
func redirectTrailingSlash(w http.ResponseWriter, r *http.Request, path string) {
	target := path
	if len(path) > 1 && path[len(path)-1] == '/' {
		target = path[:len(path)-1]
	} else {
		target = path + "/"
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	code := http.StatusMovedPermanently // 301
	if r.Method != http.MethodGet {
		code = http.StatusPermanentRedirect // 308,保留非 GET 方法与请求体
	}
	http.Redirect(w, r, target, code)
}

// allowedMethods 返回所有能匹配 path 的其它 method,以 ", " 连接(按 trees 顺序);
// 无则空串。仅在当前 method 已 miss 时调用,不在命中热路径上。
// allowedMethods returns the other methods matching path, joined by ", " (tree
// order); empty string if none. Called only after a miss, off the hit hot path.
func (m *Mux) allowedMethods(path string) string {
	var b strings.Builder
	for _, t := range m.trees {
		if t.hasPath(path) {
			if b.Len() > 0 {
				b.WriteString(", ")
			}
			b.WriteString(t.method)
		}
	}
	return b.String()
}

// serve 以单一 recover 包裹 handler 执行,把 panic 收敛为一个错误。
// serve wraps handler execution in a single recover, collapsing a panic into one
// error.
func (m *Mux) serve(h compiledHandler, ctx context.Context, req *Request, resp *Response) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// TODO(stage-4): 结构化 panic → 500,记录堆栈。
			// TODO(stage-4): structured panic → 500 with stack.
			err = errors.New("ghttp: handler panicked")
			_ = rec
		}
	}()
	return h.serve(ctx, req, resp)
}

// RawHandle 注册一个原始处理器(完全接管响应),作为一等公民逃生入口。它与 typed
// 端点共享同一执行路径,并同样经过全局/分组中间件链。
// RawHandle registers a raw handler (full response ownership) as a first-class
// escape hatch. It shares the same execution path as typed endpoints and is
// likewise wrapped by the global/group middleware chain.
func (m *Mux) RawHandle(method, path string, fn RawHandlerFunc) error {
	return m.register(method, path, Handler(fn))
}
