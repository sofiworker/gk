package ghttp

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"sync"
)

// mux 是 Server 内部的路由核心:按 method 持有路由树、池化每请求上下文,并在 ServeHTTP
// 期把全局中间件链组装到统一分发器外层。它不导出——对外只有 Server 一个顶层类型,mux
// 以嵌入字段的形式成为 Server 的一部分,其 ServeHTTP/RawHandle/Group/register 等方法
// 经 Go 的字段提升直接成为 Server 的方法,不产生额外委托层。路由的匹配与语义完全对齐
// gin(含 TSR 尾斜杠重定向)。
// mux is the routing core inside Server: it holds per-method routing trees, pools
// per-request contexts, and assembles the global middleware chain around a unified
// dispatcher at ServeHTTP time. It is unexported — Server is the only top-level
// type, and mux is embedded into it so ServeHTTP/RawHandle/Group/register are
// promoted onto Server by Go's field promotion with no delegation layer. Routing
// matching and semantics fully align with gin (including TSR trailing-slash
// redirects).
type mux struct {
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

	// --- 错误链配置(经 Option 写入,请求期只读) / error-chain config (Option-set, read-only at request time) ---

	// exposeErrorDetails 为 true 时错误响应体回传 err.Error();默认 false(脱敏)。
	// exposeErrorDetails, when true, leaks err.Error() into the error body;
	// default false (sanitized).
	exposeErrorDetails bool
	// errorRenderer 自定义错误体渲染;nil 时用 defaultErrorRenderer(JSON)。
	// errorRenderer customizes error-body rendering; nil uses defaultErrorRenderer.
	errorRenderer ErrorRenderer
	// onError 错误观测钩子(分类后调用,不写响应);nil 时不观测。
	// onError is the error observation hook (post-classification, no write); nil skips.
	onError func(r *http.Request, status int, err error)
	// notFoundHandler / methodNotAllowedHandler 自定义 404/405;nil 时走默认错误体。
	// custom 404/405 handlers; nil falls back to the default error body.
	notFoundHandler         RawHandlerFunc
	methodNotAllowedHandler RawHandlerFunc

	// strictContentType 为 true 时,body 入口在解码前校验请求 Content-Type 与
	// 端点声明的 RequestDecoder.ContentType() 一致,不符则 415。默认 true(严格)。
	// strictContentType, when true, makes body entries verify the request
	// Content-Type against the endpoint's declared RequestDecoder.ContentType()
	// before decoding, yielding 415 on mismatch. Default true (strict).
	strictContentType bool

	// --- 可信代理 / 客户端 IP 配置(经 Option 写入,请求期只读) ---
	// --- trusted-proxy / client-IP config (Option-set, read-only at request time) ---

	// trustedProxies 是可信代理网段;仅当直连对端 IP 落入其一,才采信转发头解析真实
	// 客户端 IP。为空(默认)则完全不信任转发头,ClientIP() 退回 RemoteIP()。
	// trustedProxies are trusted proxy networks; forwarded headers are honored
	// only when the direct peer IP falls within one. Empty (default) trusts no
	// forwarded header and ClientIP() falls back to RemoteIP().
	trustedProxies []netip.Prefix
	// forwardedHeaders 是回溯真实 IP 时按序检查的头名。默认 X-Forwarded-For、X-Real-IP。
	// forwardedHeaders are the header names checked in order when resolving the
	// real IP. Default: X-Forwarded-For, X-Real-IP.
	forwardedHeaders []string
}

// treeFor 返回 method 对应的路由树,不存在则创建。
// treeFor returns the routing tree for method, creating it if absent.
func (m *mux) treeFor(method string) *methodTree {
	if t := m.findTree(method); t != nil {
		return t
	}
	t := &methodTree{method: method, root: &routeNode{}}
	m.trees = append(m.trees, t)
	return t
}

// findTree 返回 method 对应的路由树,不存在返回 nil。
// findTree returns the routing tree for method, or nil if absent.
func (m *mux) findTree(method string) *methodTree {
	for _, t := range m.trees {
		if t.method == method {
			return t
		}
	}
	return nil
}

// use 向全局中间件栈追加中间件。导出入口是 Server.Use(返回 *Server 以便链式调用)。
// use appends middleware to the global stack. The exported entry is Server.Use
// (returning *Server for chaining).
func (m *mux) use(mws ...Middleware) {
	m.mws = append(m.mws, mws...)
}

// register 实现 router:直接以 method + path 注册【裸终端】。全局中间件不再于此折叠——
// 它们在 ServeHTTP 期统一组装到分发器外层(gin 语义),因此对未命中路由亦生效。
// register implements router: it registers the BARE terminal at method + path.
// Global middleware is no longer folded here — it is assembled around the
// dispatcher at ServeHTTP time (gin semantics), so it applies to route misses too.
func (m *mux) register(method, path string, terminal Handler) error {
	return m.handle(method, path, terminal)
}

// owner 实现 router:mux 就是自身的 owner。
// owner implements router: a mux owns itself.
func (m *mux) owner() *mux { return m }

// handle 将一个已编译的 handler 注册到 method + path。path 先由模板语法翻译为 gin
// 形式(:name / *name)再插入。
// handle registers a compiled handler for method + path. The path is first
// translated from template syntax into gin form (:name / *name) before insertion.
func (m *mux) handle(method, path string, h compiledHandler) error {
	ginPath, err := translateTemplate(path)
	if err != nil {
		return err
	}
	return m.treeFor(method).insert(ginPath, h)
}

// ServeHTTP 实现 http.Handler(经嵌入提升为 Server.ServeHTTP)。若配置了全局中间件,
// 首个请求时把它们折叠到统一分发器外层并缓存,此后每请求零组装;否则直连分发走零开销
// 路径。分发器内完成路径校验、匹配、命中执行与 404/405/TSR 处理——因均在中间件链【内】,
// 故中间件对命中与未命中一视同仁。
// ServeHTTP implements http.Handler (promoted to Server.ServeHTTP via embedding).
// With global middleware, the first request folds it around the unified dispatcher
// and caches the result, so later requests assemble nothing; otherwise it
// dispatches directly for a zero-overhead path. The dispatcher performs path
// validation, matching, hit execution, and 404/405/TSR handling — all INSIDE the
// chain, so middleware treats hits and misses alike.
func (m *mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
func (m *mux) buildChain() {
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
func (m *mux) dispatchChained(w http.ResponseWriter, r *http.Request) {
	req := m.pool.Get().(*Request)
	req.Request = r
	req.queryCache = nil
	req.Params.reset()
	req.skipped = req.skipped[:0]
	req.resp.reset()
	req.resp.ResponseWriter = w

	serr := m.safeChain(r.Context(), req, &req.resp)

	// 统一错误链出口:writeError 内部判断是否已提交,已提交则只记录不改写。
	// Unified error-chain exit: writeError checks Written() and only records
	// (no rewrite) when the response is already committed.
	if serr != nil {
		m.writeError(&req.resp, r, serr)
	}

	req.resp.reset()
	req.Request = nil
	m.pool.Put(req)
}

// safeChain 以最外层兜底 recover 执行全局链:未被任何 Recovery 中间件拦截的 panic
// 在此收敛为一个 error,防止 net/http 断开连接式的崩溃。装了 Recovery 中间件时,panic
// 已在链内被处理并返回 nil,不会到达这里。
// safeChain runs the global chain under an outermost safety-net recover: a panic
// not caught by any Recovery middleware collapses here into an error, preventing a
// net/http connection-tearing crash. With a Recovery middleware installed, the
// panic is handled inside the chain and returns nil, never reaching here.
func (m *mux) safeChain(ctx context.Context, req *Request, resp *Response) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// panic 收敛为 ErrHandlerPanic,交统一错误链映射为 500(脱敏响应体);
			// 堆栈由用户装的 Recovery 中间件负责记录,此兜底仅防崩溃。
			// Collapse the panic into ErrHandlerPanic for the unified error chain
			// to map to 500 (sanitized body); stack logging belongs to a user's
			// Recovery middleware — this safety net only prevents a crash.
			err = ErrHandlerPanic
			_ = rec
		}
	}()
	return m.globalChain(ctx, req, resp)
}

// dispatchTerminal 是全局链的终端(Handler 签名):在【已借到】的池化上下文上做路径
// 校验、匹配,命中则执行路由终端,未命中则处理 404/405/TSR。它不自行管理池——池由外层
// dispatchChained 负责,以保证中间件在同一 Request/Response 上运行。
// dispatchTerminal is the global chain's terminal (Handler signature): on the
// already-borrowed pooled context it validates the path, matches, executes the
// route terminal on a hit, or handles 404/405/TSR on a miss. It does not manage the
// pool itself — the outer dispatchChained does, so middleware runs on the same
// Request/Response.
func (m *mux) dispatchTerminal(ctx context.Context, req *Request, resp *Response) error {
	r := req.Request
	path := r.URL.Path
	if err := validateRequestPath(path, m.strictPath); err != nil {
		// 非法请求路径 → 交统一错误链(400 + 规范错误体)。
		// Illegal request path → unified error chain (400 + canonical body).
		return err
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
	if v.fullPath != nil {
		req.matchedRoute = *v.fullPath
	}

	// 命中:直接调用裸终端。此处【不】做 recover——panic 应自然冒泡穿过全局中间件链,
	// 让用户装的 Recovery 中间件得以捕获(拿到 PanicInfo)。未装 Recovery 时,由 safeChain
	// 的兜底 recover 防止服务崩溃。
	// Hit: call the bare terminal directly. NO recover here — a panic should bubble
	// naturally through the global middleware chain so a user's Recovery middleware
	// can catch it (with PanicInfo). Absent Recovery, safeChain's safety-net recover
	// keeps the server from crashing.
	return v.handler.serve(ctx, req, resp)
}

// dispatchRaw 是无全局中间件时的零开销分发:直接借池化上下文匹配并执行,不构造链。
// 与 dispatchChained + dispatchTerminal 的行为等价,只是省去链的一层间接。
// dispatchRaw is the zero-overhead dispatch when there is no global middleware: it
// borrows the pooled context, matches, and executes directly without building a
// chain. Equivalent to dispatchChained + dispatchTerminal minus the chain's
// indirection.
func (m *mux) dispatchRaw(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if err := validateRequestPath(path, m.strictPath); err != nil {
		m.writeErrorRaw(w, r, err)
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
	if v.fullPath != nil {
		req.matchedRoute = *v.fullPath
	}

	req.resp.reset()
	req.resp.ResponseWriter = w
	serr := m.serve(v.handler, r.Context(), req, &req.resp)

	// 统一错误链出口:必须在 reset 之前调用,writeError 依据 resp.Written() 判断
	// 是否已提交——已提交则只记录不改写,修复此前无条件 http.Error 的双写。
	// Unified error-chain exit: call BEFORE reset so writeError can read
	// resp.Written(); a committed response is only recorded, not rewritten —
	// fixing the previous unconditional http.Error double write.
	if serr != nil {
		m.writeError(&req.resp, r, serr)
	}

	req.resp.reset()
	req.Request = nil
	m.pool.Put(req)
}

// writeMiss 在池化 Response 上处理未命中:TSR(301/308)优先,其次 405+Allow,否则 404。
// 404/405 走统一错误体(可经 WithNotFoundHandler/WithMethodNotAllowedHandler 定制)。
// writeMiss handles a miss on the pooled Response: TSR (301/308) first, then
// 405 + Allow, otherwise 404. Both 404 and 405 emit the unified error body
// (customizable via WithNotFoundHandler/WithMethodNotAllowedHandler).
func (m *mux) writeMiss(resp *Response, r *http.Request, path string, tsr bool) {
	if tsr && path != "/" {
		redirectTrailingSlash(resp, r, path)
		return
	}
	if allow := m.allowedMethods(path); allow != "" {
		resp.Header().Set("Allow", allow)
		if m.methodNotAllowedHandler != nil {
			_ = m.methodNotAllowedHandler(r.Context(), &Request{Request: r}, resp)
			return
		}
		m.renderMiss(resp, r, http.StatusMethodNotAllowed)
		return
	}
	if m.notFoundHandler != nil {
		_ = m.notFoundHandler(r.Context(), &Request{Request: r}, resp)
		return
	}
	m.renderMiss(resp, r, http.StatusNotFound)
}

// writeMissRaw 是 writeMiss 的裸 ResponseWriter 版本,用于无全局中间件的零开销路径。
// writeMissRaw is the bare-ResponseWriter version of writeMiss for the
// zero-overhead path without global middleware.
func (m *mux) writeMissRaw(w http.ResponseWriter, r *http.Request, path string, tsr bool) {
	if tsr && path != "/" {
		redirectTrailingSlash(w, r, path)
		return
	}
	resp := &Response{ResponseWriter: w}
	if allow := m.allowedMethods(path); allow != "" {
		resp.Header().Set("Allow", allow)
		if m.methodNotAllowedHandler != nil {
			_ = m.methodNotAllowedHandler(r.Context(), &Request{Request: r}, resp)
			return
		}
		m.renderMiss(resp, r, http.StatusMethodNotAllowed)
		return
	}
	if m.notFoundHandler != nil {
		_ = m.notFoundHandler(r.Context(), &Request{Request: r}, resp)
		return
	}
	m.renderMiss(resp, r, http.StatusNotFound)
}

// renderMiss 用配置的 ErrorRenderer(默认 JSON 错误体)渲染 404/405,使 miss 响应与
// 业务错误体格式一致。仅在 miss 冷路径调用。
// renderMiss renders a 404/405 via the configured ErrorRenderer (default JSON
// error body) so miss responses match the business error body. Miss cold path only.
func (m *mux) renderMiss(resp *Response, r *http.Request, status int) {
	if m.onError != nil {
		m.onError(r, status, statusError(status))
	}
	renderer := m.errorRenderer
	if renderer == nil {
		renderer = defaultErrorRenderer
	}
	renderer.RenderError(resp, status, codeForStatus(status), genericMessage(status))
}

// writeErrorRaw 在无池化 Response 的冷路径(裸 w)上走统一错误链渲染一个 error。
// 仅用于非法请求路径等匹配前的错误。
// writeErrorRaw renders an error through the unified chain on a bare-w cold path
// without a pooled Response, used for pre-match errors like an illegal path.
func (m *mux) writeErrorRaw(w http.ResponseWriter, r *http.Request, err error) {
	resp := &Response{ResponseWriter: w}
	m.writeError(resp, r, err)
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
func (m *mux) allowedMethods(path string) string {
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
func (m *mux) serve(h compiledHandler, ctx context.Context, req *Request, resp *Response) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// panic 收敛为 ErrHandlerPanic → 统一错误链 → 500(脱敏)。
			// Collapse the panic into ErrHandlerPanic → unified error chain → 500 (sanitized).
			err = ErrHandlerPanic
			_ = rec
		}
	}()
	return h.serve(ctx, req, resp)
}

// RawHandle 注册一个原始处理器(完全接管响应),作为一等公民逃生入口。它与 typed
// 端点共享同一执行路径,并同样经过全局/分组中间件链。经嵌入提升为 Server.RawHandle。
// RawHandle registers a raw handler (full response ownership) as a first-class
// escape hatch. It shares the same execution path as typed endpoints and is
// likewise wrapped by the global/group middleware chain. Promoted to
// Server.RawHandle via embedding.
func (m *mux) RawHandle(method, path string, fn RawHandlerFunc) error {
	return m.register(method, path, Handler(fn))
}
