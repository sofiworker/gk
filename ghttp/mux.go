package ghttp

import (
	"context"
	"net/http"
	"strings"
	"sync"
)

// Engine 是框架的多路复用器与执行引擎:按 method 持有路由树、池化每请求上下文,并在
// ServeHTTP 期把全局中间件链组装到统一分发器外层。路由的匹配与语义完全对齐 gin
// (含 TSR 尾斜杠重定向)。
// Engine is the framework multiplexer and execution engine: it holds per-method
// routing trees, pools per-request contexts, and assembles the global middleware
// chain around a unified dispatcher at ServeHTTP time. Routing matching and
// semantics fully align with gin (including TSR trailing-slash redirects).
type Engine struct {
	trees []*methodTree
	pool  sync.Pool

	// mws 是全局中间件栈。与 gin 一致:在 ServeHTTP 期(而非注册期)组装到分发器外层,
	// 因此对【所有】请求生效——包括未命中路由、未注册 OPTIONS 的预检(CORS 得以工作)。
	// 须在开始服务前经 Use 追加(gin 同款约束);服务开始后的追加不影响已折叠的链。
	// mws is the global middleware stack. Like gin, it is assembled around the
	// dispatcher at ServeHTTP time (not registration time), so it applies to ALL
	// requests — including route misses and unregistered-OPTIONS preflight (making
	// CORS work). Append via Use before serving (gin's constraint); appends after
	// serving do not affect the already-folded chain.
	mws []Middleware

	// globalChain 是 chain(dispatch, mws) 的一次性折叠结果,经 chainOnce 在首个请求时
	// 固化,此后每请求零组装、零额外分配。无全局中间件时保持 nil,ServeHTTP 直连
	// dispatch 走零开销路径。
	// globalChain is the one-time fold of chain(dispatch, mws), fixed on the first
	// request via chainOnce; thereafter each request assembles nothing and
	// allocates nothing extra. It stays nil with no global middleware so ServeHTTP
	// dispatches directly for a zero-overhead path.
	globalChain Handler
	chainOnce   sync.Once

	// strictPath 决定请求路径校验强度。默认 false(快速模式):仅拦截 dot 段
	// (防路径遍历),用一次 SIMD 字节扫描粗筛,空段放行交给匹配(对齐 gin)。
	// 置 true(严格模式):完整校验 dot 段 + 空段,任一非法即 400。
	// strictPath selects request-path validation strength. Default false (fast
	// mode): only dot segments are rejected (path-traversal guard) via a single
	// SIMD byte scan, empty segments pass through to matching (aligned with gin).
	// Set true (strict mode): full validation of dot and empty segments, any
	// illegal one yields 400.
	strictPath bool

	// srvMu 保护 activeServer:一体化 Run/RunTLS 设置它,Shutdown 读取它(见 engine.go)。
	// srvMu guards activeServer: the all-in-one Run/RunTLS set it, Shutdown reads it
	// (see engine.go).
	srvMu        sync.Mutex
	activeServer *Server
}

// Mux 是 Engine 的兼容别名。历史代码与外部调用方(如以 *ghttp.Mux 为类型)可无缝沿用;
// 新代码建议直接用 Engine。
// Mux is a compatibility alias for Engine. Existing code and external callers
// (e.g. typed as *ghttp.Mux) keep working unchanged; new code should use Engine.
type Mux = Engine

// Option 配置 Engine。
// Option configures an Engine.
type Option func(*Engine)

// WithStrictPath 开启严格路径校验:dot 段与空段均返回 400。默认关闭(快速模式,
// 仅拦 dot 段以防路径遍历,空段交给匹配,与 gin 一致的高性能取舍)。
// WithStrictPath enables strict path validation: both dot and empty segments
// yield 400. Off by default (fast mode: only dot segments are rejected as a
// path-traversal guard, empty segments pass to matching — the gin-aligned
// high-performance tradeoff).
func WithStrictPath(strict bool) Option {
	return func(m *Engine) { m.strictPath = strict }
}

// New 构造一个空的 Engine;可选 Option 调整行为。
// New constructs an empty Engine; optional Options adjust behavior.
func New(opts ...Option) *Engine {
	m := &Engine{}
	m.pool.New = func() any { return &Request{} }
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// treeFor 返回 method 对应的路由树,不存在则创建。
// treeFor returns the routing tree for method, creating it if absent.
func (m *Engine) treeFor(method string) *methodTree {
	if t := m.findTree(method); t != nil {
		return t
	}
	t := &methodTree{method: method, root: &routeNode{}}
	m.trees = append(m.trees, t)
	return t
}

// findTree 返回 method 对应的路由树,不存在返回 nil。
// findTree returns the routing tree for method, or nil if absent.
func (m *Engine) findTree(method string) *methodTree {
	for _, t := range m.trees {
		if t.method == method {
			return t
		}
	}
	return nil
}

// Use 向全局中间件栈追加中间件。须在注册路由与开始服务前调用(gin 同款约束)。
// 返回自身以便链式调用。
// Use appends middleware to the global stack. Call before registering routes and
// before serving (gin's constraint). Returns itself for chaining.
func (m *Engine) Use(mws ...Middleware) *Engine {
	m.mws = append(m.mws, mws...)
	return m
}

// register 实现 router:直接以 method + path 注册【裸终端】。全局中间件不再于此折叠——
// 它们在 ServeHTTP 期统一组装到分发器外层(gin 语义),因此对未命中路由亦生效。
// register implements router: it registers the BARE terminal at method + path.
// Global middleware is no longer folded here — it is assembled around the
// dispatcher at ServeHTTP time (gin semantics), so it applies to route misses too.
func (m *Engine) register(method, path string, terminal Handler) error {
	return m.handle(method, path, terminal)
}

// handle 将一个已编译的 handler 注册到 method + path。path 先由模板语法翻译为 gin
// 形式(:name / *name)再插入。
// handle registers a compiled handler for method + path. The path is first
// translated from template syntax into gin form (:name / *name) before insertion.
func (m *Engine) handle(method, path string, h compiledHandler) error {
	ginPath, err := translateTemplate(path)
	if err != nil {
		return err
	}
	return m.treeFor(method).insert(ginPath, h)
}

// ServeHTTP 实现 http.Handler。若配置了全局中间件,首个请求时把它们折叠到统一分发器
// (dispatch)外层并缓存,此后每请求零组装;否则直连 dispatch 走零开销路径。分发器内
// 完成路径校验、匹配、命中执行与 404/405/TSR 处理——因均在中间件链【内】,故中间件
// 对命中与未命中一视同仁。
// ServeHTTP implements http.Handler. With global middleware, the first request
// folds it around the unified dispatcher and caches the result, so later requests
// assemble nothing; otherwise it dispatches directly for a zero-overhead path. The
// dispatcher performs path validation, matching, hit execution, and 404/405/TSR
// handling — all INSIDE the chain, so middleware treats hits and misses alike.
func (m *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.chainOnce.Do(m.buildChain)
	if m.globalChain == nil {
		m.dispatchRaw(w, r)
		return
	}
	m.dispatchChained(w, r)
}

// buildChain 在无全局中间件时保持 globalChain 为 nil(零开销直连);否则把 mws 折叠到
// 一个「以池化上下文调用路由终端 / miss 处理」的分发终端外层,只做一次。
// buildChain leaves globalChain nil with no global middleware (zero-overhead
// direct path); otherwise it folds mws around a dispatch terminal that invokes the
// route terminal / miss handler with pooled contexts, done once.
func (m *Engine) buildChain() {
	if len(m.mws) == 0 {
		return
	}
	m.globalChain = chain(m.dispatchTerminal, m.mws)
}

// dispatchChained 走全局链:借一个池化 Request/Response 承载 w/r,执行缓存的
// globalChain(其终端是 dispatchTerminal),再归还。miss 与命中共用这套上下文,使
// 404/405 也能被中间件观测与改写。
// dispatchChained runs the global chain: borrow a pooled Request/Response to carry
// w/r, execute the cached globalChain (whose terminal is dispatchTerminal), then
// return it. Miss and hit share this context so 404/405 are observable/rewritable
// by middleware.
func (m *Engine) dispatchChained(w http.ResponseWriter, r *http.Request) {
	req := m.pool.Get().(*Request)
	req.Request = r
	req.queryCache = nil
	req.Params.reset()
	req.skipped = req.skipped[:0]
	req.resp.reset()
	req.resp.ResponseWriter = w

	serr := m.safeChain(r.Context(), req, &req.resp)
	written := req.resp.Written()

	req.resp.reset()
	req.Request = nil
	m.pool.Put(req)

	if serr != nil && !written {
		// TODO(stage-4): 接入统一错误链(415 / 业务错误 / panic)。
		// TODO(stage-4): route into the unified error chain.
		http.Error(w, serr.Error(), http.StatusInternalServerError)
	}
}

// safeChain 以最外层兜底 recover 执行全局链:未被任何 Recovery 中间件拦截的 panic
// 在此收敛为一个 error,防止 net/http 断开连接式的崩溃。装了 Recovery 中间件时,panic
// 已在链内被处理并返回 nil,不会到达这里。
// safeChain runs the global chain under an outermost safety-net recover: a panic
// not caught by any Recovery middleware collapses here into an error, preventing a
// net/http connection-tearing crash. With a Recovery middleware installed, the
// panic is handled inside the chain and returns nil, never reaching here.
func (m *Engine) safeChain(ctx context.Context, req *Request, resp *Response) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// TODO(stage-4): 结构化 panic → 500,记录堆栈。
			// TODO(stage-4): structured panic → 500 with stack.
			err = ErrHandlerPanic
			_ = rec
		}
	}()
	return m.globalChain(ctx, req, resp)
}

// dispatchTerminal 是全局链的终端(Handler 签名):在【已借到】的池化上下文上做路径
// 校验、匹配,命中则以 recover 包裹执行路由终端,未命中则处理 404/405/TSR。它不自行
// 管理池——池由外层 dispatchChained 负责,以保证中间件在同一 Request/Response 上运行。
// dispatchTerminal is the global chain's terminal (Handler signature): on the
// already-borrowed pooled context it validates the path, matches, executes the
// route terminal under recover on a hit, or handles 404/405/TSR on a miss. It does
// not manage the pool itself — the outer dispatchChained does, so middleware runs
// on the same Request/Response.
func (m *Engine) dispatchTerminal(ctx context.Context, req *Request, resp *Response) error {
	r := req.Request
	path := r.URL.Path
	if err := validateRequestPath(path, m.strictPath); err != nil {
		resp.WriteHeader(http.StatusBadRequest)
		return nil
	}

	t := m.findTree(r.Method)
	if t == nil {
		m.writeMiss(resp, r, path, false)
		return nil
	}

	v := t.root.getValue(path, &req.Params, &req.skipped)
	if v.handler == nil {
		m.writeMiss(resp, r, path, v.tsr)
		return nil
	}

	// 命中:直接调用裸终端。此处【不】做 recover——panic 应自然冒泡穿过全局中间件链,
	// 让用户装的 Recovery 中间件得以捕获(拿到 PanicInfo)。未装 Recovery 时,由最外层
	// dispatch 的兜底 recover 防止服务崩溃。
	// Hit: call the bare terminal directly. NO recover here — a panic should bubble
	// naturally through the global middleware chain so a user's Recovery middleware
	// can catch it (with PanicInfo). Absent Recovery, the outermost dispatch's
	// safety-net recover keeps the server from crashing.
	return v.handler.serve(ctx, req, resp)
}

// dispatchRaw 是无全局中间件时的零开销分发:直接借池化上下文匹配并执行,不构造链。
// 与 dispatchChained + dispatchTerminal 的行为等价,只是省去链的一层间接。
// dispatchRaw is the zero-overhead dispatch when there is no global middleware: it
// borrows the pooled context, matches, and executes directly without building a
// chain. Equivalent to dispatchChained + dispatchTerminal minus the chain's
// indirection.
func (m *Engine) dispatchRaw(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if err := validateRequestPath(path, m.strictPath); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	t := m.findTree(r.Method)
	if t == nil {
		m.writeMissRaw(w, r, path, false)
		return
	}

	req := m.pool.Get().(*Request)
	req.Request = r
	req.queryCache = nil
	req.Params.reset()
	req.skipped = req.skipped[:0]

	v := t.root.getValue(path, &req.Params, &req.skipped)
	if v.handler == nil {
		tsr := v.tsr
		req.Request = nil
		m.pool.Put(req)
		m.writeMissRaw(w, r, path, tsr)
		return
	}

	req.resp.reset()
	req.resp.ResponseWriter = w
	serr := m.serve(v.handler, r.Context(), req, &req.resp)

	req.resp.reset()
	req.Request = nil
	m.pool.Put(req)

	if serr != nil {
		http.Error(w, serr.Error(), http.StatusInternalServerError)
	}
}

// writeMiss 在池化 Response 上处理未命中:TSR(301/308)优先,其次 405+Allow,否则 404。
// writeMiss handles a miss on the pooled Response: TSR (301/308) first, then
// 405 + Allow, otherwise 404.
func (m *Engine) writeMiss(resp *Response, r *http.Request, path string, tsr bool) {
	if tsr && path != "/" {
		redirectTrailingSlash(resp, r, path)
		return
	}
	if allow := m.allowedMethods(path); allow != "" {
		resp.Header().Set("Allow", allow)
		resp.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	resp.WriteHeader(http.StatusNotFound)
}

// writeMissRaw 是 writeMiss 的裸 ResponseWriter 版本,用于无全局中间件的零开销路径。
// writeMissRaw is the bare-ResponseWriter version of writeMiss for the
// zero-overhead path without global middleware.
func (m *Engine) writeMissRaw(w http.ResponseWriter, r *http.Request, path string, tsr bool) {
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
// (与 gin 的 redirectTrailingSlash 行为一致)。GET 用 301,其它安全起见用 308 保留方法。
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
func (m *Engine) allowedMethods(path string) string {
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
func (m *Engine) serve(h compiledHandler, ctx context.Context, req *Request, resp *Response) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// TODO(stage-4): 结构化 panic → 500,记录堆栈。
			// TODO(stage-4): structured panic → 500 with stack.
			err = ErrHandlerPanic
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
func (m *Engine) RawHandle(method, path string, fn RawHandlerFunc) error {
	return m.register(method, path, Handler(fn))
}
