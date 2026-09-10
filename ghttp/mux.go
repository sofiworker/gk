package ghttp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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
	// chainBuilt 记录 globalChain 是否已折叠。用原子量而非普通 bool:它在首个请求的
	// 服务路径上被写,而 Use 可能从另一个 goroutine 读(误用场景本身就是"服务已开始"),
	// 普通 bool 会构成数据竞争。
	// chainBuilt records whether globalChain has been folded. It is atomic rather than a
	// plain bool because it is written on the first request's serving path while Use may
	// read it from another goroutine (the misuse case is precisely "serving already
	// started"), which a plain bool would turn into a data race.
	chainBuilt atomic.Bool

	// strictPath 决定请求路径校验强度。默认 false(快速模式):仅拦截 dot 段
	// (防路径遍历),用一次 SIMD 字节扫描粗筛,空段放行交给匹配(对齐 gin)。
	// 置 true(严格模式):完整校验 dot 段 + 空段,任一非法即 400。
	// strictPath selects request-path validation strength. Default false (fast
	// mode): only dot segments are rejected (path-traversal guard) via a single
	// SIMD byte scan, empty segments pass through to matching (aligned with gin).
	// Set true (strict mode): full validation of dot and empty segments, any
	// illegal one yields 400.
	strictPath bool
	autoHEAD   bool

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

	// --- OpenAPI 文档收集(注册期写入,请求期只读)/ OpenAPI doc collection ---

	// collectDocs 为 true 时 noteRoute 才登记路由元数据。未开启 WithOpenAPI 时恒为
	// false,登记函数立即返回,保证"不用则零成本"。
	// collectDocs gates noteRoute's recording of route metadata. It stays false
	// without WithOpenAPI, so the recorder returns immediately, keeping "unused
	// means zero cost".
	// serving 标记服务是否已开始接收请求。register/handle 据此拒绝运行期注册。
	// serving marks whether the server has started accepting requests; register
	// and handle refuse runtime registration based on it.
	serving     atomic.Bool
	collectDocs bool
	// docs 是注册期收集的 typed 端点元数据,仅供 spec 构建使用。
	// docs holds typed endpoint metadata collected at registration, used only for
	// spec construction.
	docs []routeEntry
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
	// 中间件链在首个请求时被 chainOnce 一次性折叠(为的是让命中路径零组装开销)。此后
	// 追加的中间件永远不会进入那个已折叠的链——静默 no-op 是最坏的失败方式:代码看起来
	// 装上了鉴权/限流,实际完全没生效。这里显式告警而不是 panic:进程已在服务流量,为一处
	// 误用杀掉整个服务的代价高于漏装一个中间件,而日志足以让误用暴露。
	// The middleware chain is folded once by chainOnce on the first request (so the hit
	// path assembles nothing). Middleware appended afterwards never enters that folded
	// chain, and silently no-op'ing is the worst failure mode: the code looks like auth
	// or rate limiting is installed while nothing actually runs. This warns explicitly
	// rather than panicking — the process is already serving traffic, and killing it over
	// one misuse costs more than the missing middleware, while the log makes it visible.
	if m.chainBuilt.Load() {
		log.Printf("ghttp: Use called after serving started; %d middleware ignored "+
			"(the global chain is folded once on the first request — call Use before serving)", len(mws))
		return
	}
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
	// 注册守卫：路由树是裸 map + 无锁读，服务已开始后注册会与匹配路径构成数据竞争，
	// race detector 可直接判死。宁可显式拒绝，也不留下"多数时候能用、压测时随机崩"的
	// 陷阱；确有需要热注册请走 COW 或另建实例。
	// Registration guard: the route tree is a plain map read without locks, so
	// registering after serving began races with matching and the race detector
	// rightly fails. Refusing explicitly beats a "works until it doesn't" trap; use
	// copy-on-write or a second instance if hot registration is genuinely needed.
	if m.serving.Load() {
		return fmt.Errorf("%w: cannot register %s %s after the server started serving", ErrRegistrationAfterStart, method, path)
	}
	// 方法必须是合法 token 且非小写变体："get" 永远匹配不到 GET，却会建出一棵树，
	// 让 MatchRoute 对真实请求返回空串（观测层彻底丢失路由归属）。
	// The method must be a valid token and not a lowercase variant: "get" can never
	// match GET yet still builds a tree, and MatchRoute then returns an empty route
	// for the real request, losing route attribution in the observability layer.
	if err := validateMethodToken(method); err != nil {
		return err
	}
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
	// 折叠标记必须无条件置位,包括没有全局中间件的情形:此后再 Use 同样不会生效
	// (globalChain 已被 chainOnce 定格为 nil,ServeHTTP 直连 dispatch),必须一样告警。
	// The folded flag is set unconditionally, including when there is no global
	// middleware: a later Use is just as ineffective (chainOnce has fixed globalChain at
	// nil and ServeHTTP dispatches directly), so it must warn all the same.
	m.chainBuilt.Store(true)
	if len(m.mws) == 0 {
		return
	}
	m.globalChain = chain(m.dispatchTerminal, m.mws)
}

// dispatchChained 走全局链:借一个池化 Request/Response 承载 w/r,先完成路由匹配(使
// 中间件能读到 MatchedRoute),再执行缓存的 globalChain(其终端是 dispatchTerminal),
// 最后归还。miss 与命中共用这套上下文,使 404/405 也能被中间件观测与改写。
//
// 匹配必须在链【之前】完成:全局中间件包在分发器外层,若把匹配留到终端才做,按路由聚合
// 的中间件(metrics、按路由限流、tracing span 命名)在自己运行时就读不到路由模板。
// 匹配结果暂存在 Request 上,终端直接复用,不重复走树。
// dispatchChained runs the global chain: it borrows a pooled Request/Response to
// carry w/r, resolves the route FIRST (so middleware can read MatchedRoute), then
// executes the cached globalChain (whose terminal is dispatchTerminal), and finally
// returns the context. Miss and hit share this context so 404/405 are
// observable/rewritable by middleware.
//
// Matching must happen BEFORE the chain: global middleware wraps the dispatcher, so
// deferring the match to the terminal would leave route-aggregating middleware
// (metrics, per-route rate limiting, tracing span names) with no route template at
// the time it runs. The match result is parked on the Request and reused by the
// terminal, so the tree is walked once.
func (m *mux) dispatchChained(w http.ResponseWriter, r *http.Request) {
	req := m.pool.Get().(*Request)
	// 借出与归还都走同一个 reset()：请求侧字段的清理清单只有一份，不会两处分叉漏字段
	// （历史上正是这种分叉导致 matchedRoute 跨请求泄漏）。reset() 不清 resp，故 resp 仍
	// 单独重置。
	// Borrow and return share one reset(): the request-side clearing list lives in a
	// single place and cannot fork into two divergent lists that drop a field (such a
	// fork previously leaked matchedRoute across requests). reset() leaves resp alone,
	// so it is reset separately.
	req.reset()
	req.Request = r
	req.resp.reset()
	req.resp.ResponseWriter = w

	// 预解析:路径校验 + 树匹配。结果(含错误与 miss 信息)暂存,由 dispatchTerminal 消费。
	// Pre-resolve: path validation plus the tree match. The result (including an
	// error or miss info) is parked for dispatchTerminal to consume.
	req.resolved = m.resolve(req)

	serr := m.safeChain(r.Context(), req, &req.resp)

	// 统一错误链出口:writeError 内部判断是否已提交,已提交则只记录不改写。
	// Unified error-chain exit: writeError checks Written() and only records
	// (no rewrite) when the response is already committed.
	if serr != nil {
		m.writeError(&req.resp, r, serr)
	}

	req.resolved = resolvedRoute{}
	req.reset()
	m.pool.Put(req)
}

// resolvedRoute 承载"链执行前已完成的路由解析结果",供链后的终端复用。path/method 记录
// 解析时刻的输入,供终端检测中间件是否改写了 URL.Path 或 Method(URL 重写、方法覆写),
// 改写后须重新解析,否则终端会执行旧路径的匹配结果。
// resolvedRoute carries the route resolution completed before the chain runs, for
// the post-chain terminal to reuse. path/method record the inputs at resolution
// time so the terminal can detect middleware rewriting URL.Path or Method (URL
// rewrites, method overrides); a rewrite requires re-resolution, or the terminal
// would execute the stale match of the old path.
type resolvedRoute struct {
	// err 是路径校验错误(非法请求路径);非 nil 时不做匹配。
	// err is a path validation error (illegal request path); no match is attempted when set.
	err error
	// handler 为 nil 表示未命中(需 404/405/TSR 处理)。
	// A nil handler means a miss (404/405/TSR handling required).
	handler compiledHandler
	// tsr 表示"仅差一个尾斜杠",用于 301/308 重定向。
	// tsr means "off by one trailing slash", driving a 301/308 redirect.
	tsr bool
	// path/method 是本次解析所依据的请求路径与方法。
	// path/method are the request path and method this resolution was based on.
	path   string
	method string
}

// resolve 完成路径校验与树匹配,把参数写入 req.Params,并在命中时写入 matchedRoute。
// 返回值总是带上本次解析依据的 path/method,供终端做改写检测。
// resolve performs path validation and the tree match, writing params into
// req.Params and, on a hit, matchedRoute. The result always carries the
// path/method it was based on, for the terminal's rewrite detection.
func (m *mux) resolve(req *Request) resolvedRoute {
	r := req.Request
	path := r.URL.Path
	res := resolvedRoute{path: path, method: r.Method}
	if err := validateRequestPath(path, m.strictPath); err != nil {
		res.err = err
		return res
	}
	t := m.findTree(r.Method)
	if t != nil {
		v := t.root.getValue(path, &req.Params, &req.skipped)
		if v.handler != nil {
			if v.fullPath != nil {
				req.matchedRoute = *v.fullPath
			}
			res.handler = v.handler
			return res
		}
		res.tsr = v.tsr
	}
	// autoHEAD 回退(冷路径):按路径回退到 GET 树,语义见 headFallback。
	// autoHEAD fallback (cold path): per-path fallback to the GET tree; see
	// headFallback for the semantics.
	if m.autoHEAD && r.Method == http.MethodHead {
		v, ok := m.headFallback(path, req)
		if ok {
			if v.fullPath != nil {
				req.matchedRoute = *v.fullPath
			}
			res.handler = v.handler
			res.tsr = false
			return res
		}
		res.tsr = res.tsr || v.tsr
	}
	return res
}

// headFallback 是 autoHEAD 的按【路径】回退:HEAD 树未命中该 path 时改查 GET 树。
// 按路径而非仅在整棵 HEAD 树缺失时回退——Health/Ready/Static/File 都注册 HEAD 路由,
// 按树回退会在用户用了其中任何一个后对全站其余路径整体失效(HEAD /x → 405)。
// 显式 HEAD 命中由调用方先行返回,优先级不变;回退命中的 GET handler 直接跑在原始
// writer 上——net/http 对 HEAD 自动丢弃响应体并据写入字节计算 Content-Length、做
// Content-Type 嗅探,自吞字节的包装层反而会把这两个头一起丢掉(违反 RFC 9110
// §9.3.2:HEAD 的元数据应与 GET 一致)。
// headFallback is autoHEAD's PER-PATH fallback: when the HEAD tree misses this
// path, the GET tree is consulted. Per path rather than only when the whole HEAD
// tree is missing — Health/Ready/Static/File all register HEAD routes, so the
// per-tree fallback went dead for every other path once the user mounted any of
// them (HEAD /x → 405). An explicit HEAD hit returns in the caller first, so
// precedence is unchanged; a fallback GET handler runs on the original writer —
// net/http discards a HEAD response body itself while still deriving
// Content-Length and sniffing Content-Type from the written bytes, whereas a
// byte-swallowing wrapper dropped both headers (violating RFC 9110 §9.3.2: HEAD
// metadata should match GET).
func (m *mux) headFallback(path string, req *Request) (nodeValue, bool) {
	gt := m.findTree(http.MethodGet)
	if gt == nil {
		return nodeValue{}, false
	}
	// 失败的 HEAD 匹配可能已写入部分参数,清掉再按 GET 树匹配,防止两次匹配混叠。
	// The failed HEAD match may have written partial params; clear them before
	// matching the GET tree so the two matches cannot interleave.
	req.Params.reset()
	req.skipped = req.skipped[:0]
	v := gt.root.getValue(path, &req.Params, &req.skipped)
	return v, v.handler != nil
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
			if rec == http.ErrAbortHandler {
				// 标准库约定:panic(http.ErrAbortHandler) 表示"静默放弃这个响应",
				// net/http 会据此断连且不打日志(ReverseProxy 等依赖该约定)。把它收敛成
				// 500 会既篡改语义又刷屏,故原样向上传播。
				// Stdlib contract: panic(http.ErrAbortHandler) means "abandon this
				// response silently"; net/http drops the connection without logging
				// (ReverseProxy and others rely on it). Collapsing it into a 500 would
				// both distort the semantics and spam logs, so re-panic as-is.
				panic(rec)
			}
			// panic 收敛为 ErrHandlerPanic,交统一错误链映射为 500(脱敏响应体);
			// 堆栈由用户装的 Recovery 中间件负责记录,此兜底仅防崩溃。
			// panic 值包进错误,否则不挂 Recovery 时 onError 只见一句 "handler panicked",
			// 排障时既无原因也无堆栈。
			// Collapse the panic into ErrHandlerPanic for the unified error chain to map
			// to 500 (sanitized body); stack logging belongs to a user's Recovery
			// middleware — this safety net only prevents a crash. The panic value is
			// wrapped into the error, since otherwise onError sees only "handler
			// panicked" with no cause or stack when no Recovery is installed.
			err = panicError(rec)
		}
	}()
	return m.globalChain(ctx, req, resp)
}

// dispatchTerminal 是全局链的终端(Handler 签名):消费 dispatchChained 已完成的路由
// 解析结果,命中则执行路由终端,未命中则处理 404/405/TSR。它不自行管理池——池由外层
// dispatchChained 负责,以保证中间件在同一 Request/Response 上运行且能读到 MatchedRoute。
// 匹配通常也复用链前的结果;仅当中间件改写了 URL.Path/Method 时按新值重新解析一次。
// dispatchTerminal is the global chain's terminal (Handler signature): it consumes
// the route resolution dispatchChained already performed, executing the route
// terminal on a hit or handling 404/405/TSR on a miss. It does not manage the pool
// — the outer dispatchChained owns it, so middleware runs on the same
// Request/Response and can read MatchedRoute. The match is normally reused from
// before the chain too; only when middleware rewrote URL.Path/Method does it
// re-resolve once with the new values.
func (m *mux) dispatchTerminal(ctx context.Context, req *Request, resp *Response) error {
	res := req.resolved
	// 中间件改写了 URL.Path 或 Method(strip-prefix 网关、URL 重写、方法覆写):链前的
	// 解析结果已作废,必须按新值重新解析。否则终端会执行旧路径的匹配结果——重写目标
	// 明明已注册却 404/405,且 writeMiss 按新路径算 Allow、按旧解析判 miss,能产出
	// "GET 收到 405 + Allow: GET"这类自相矛盾的响应。重写是冷路径,一次额外树查找可接受。
	// Middleware rewrote URL.Path or Method (strip-prefix gateways, URL rewrites,
	// method overrides): the pre-chain resolution is stale and must be redone with
	// the new values. Otherwise the terminal executes the old path's match — the
	// rewrite target 404s/405s although registered, and writeMiss computes Allow
	// from the new path while judging the miss from the old resolution, producing
	// self-contradictory responses like "GET gets 405 + Allow: GET". Rewrites are a
	// cold path; one extra tree lookup is acceptable.
	if req.URL.Path != res.path || req.Method != res.method {
		// 旧匹配可能已写入参数与路由模板,先清空再解析,防止新旧匹配的数据混叠。
		// The stale match may have written params and the route template; clear
		// them before re-resolving so old and new match data cannot interleave.
		req.Params.reset()
		req.skipped = req.skipped[:0]
		req.matchedRoute = ""
		res = m.resolve(req)
		req.resolved = res
	}
	if res.err != nil {
		// 非法请求路径 → 交统一错误链(400 + 规范错误体)。
		// Illegal request path → unified error chain (400 + canonical body).
		return res.err
	}
	if res.handler == nil {
		m.writeMiss(resp, req.Request, req.URL.Path, res.tsr)
		return nil
	}

	// 命中:直接调用裸终端。此处【不】做 recover——panic 应自然冒泡穿过全局中间件链,
	// 让用户装的 Recovery 中间件得以捕获(拿到 PanicInfo)。未装 Recovery 时,由 safeChain
	// 的兜底 recover 防止服务崩溃。
	// HEAD(含 autoHEAD 回退)不包装 writer:net/http 自动丢弃 HEAD 响应体并据写入
	// 字节计算 Content-Length / 嗅探 Content-Type,包装吞字节反而丢这两个头(见 resolve)。
	// Hit: call the bare terminal directly. NO recover here — a panic should bubble
	// naturally through the global middleware chain so a user's Recovery middleware
	// can catch it (with PanicInfo). Absent Recovery, safeChain's safety-net recover
	// keeps the server from crashing.
	// HEAD (including the autoHEAD fallback) gets no writer wrapping: net/http
	// drops a HEAD body itself while still deriving Content-Length / sniffing
	// Content-Type from the written bytes; a swallowing wrapper lost both (see
	// resolve).
	return res.handler.serve(ctx, req, resp)
}

// dispatchRaw 是无全局中间件时的零开销分发:直接借池化上下文匹配并执行,不构造链。
// 与 dispatchChained + dispatchTerminal 的行为等价,只是省去链的一层间接。
// dispatchRaw is the zero-overhead dispatch when there is no global middleware: it
// borrows the pooled context, matches, and executes directly without building a
// chain. Equivalent to dispatchChained + dispatchTerminal minus the chain's
// indirection.
func (m *mux) dispatchRaw(w http.ResponseWriter, r *http.Request) {
	req := m.pool.Get().(*Request)
	// 与 dispatchChained 同理：借出与归还共用一个 reset()，清理清单只有一份。
	// Same as dispatchChained: borrow and return share one reset(), one clearing list.
	req.reset()
	req.Request = r

	path := r.URL.Path
	if err := validateRequestPath(path, m.strictPath); err != nil {
		req.reset()
		m.pool.Put(req)
		m.writeErrorRaw(w, r, err)
		return
	}

	// 热路径内联主树匹配(避免 resolvedRoute 的按值搬运);miss 才进 autoHEAD 回退等
	// 冷路径,其语义与 resolve 共享同一实现(headFallback),两条分发路径不会漂移。
	// The hot path inlines the primary-tree match (avoiding by-value resolvedRoute
	// copies); only a miss enters cold paths like the autoHEAD fallback, whose
	// semantics share one implementation (headFallback) with resolve, so the two
	// dispatch paths cannot drift.
	var v nodeValue
	if t := m.findTree(r.Method); t != nil {
		v = t.root.getValue(path, &req.Params, &req.skipped)
	}
	if v.handler == nil && m.autoHEAD && r.Method == http.MethodHead {
		if fv, ok := m.headFallback(path, req); ok {
			v = fv
		} else {
			v.tsr = v.tsr || fv.tsr
		}
	}
	if v.handler == nil {
		tsr := v.tsr
		req.reset()
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

	req.reset()
	m.pool.Put(req)
}

// writeMiss 在池化 Response 上处理未命中:TSR(301/308)优先,其次 405+Allow,否则 404。
// 404/405 走统一错误体(可经 WithNotFoundHandler/WithMethodNotAllowedHandler 定制)。
//
// 自定义 handler 返回的 error 必须交给 writeError,而不能丢弃:handler 若返回 error 且
// 尚未写响应,丢弃会让 net/http 隐式回 200 空体,错误彻底消失。writeError 内部按
// Written() 判断,已写响应的 handler 只被记录不被改写,故两种写法都安全。
// writeMiss handles a miss on the pooled Response: TSR (301/308) first, then
// 405 + Allow, otherwise 404. Both 404 and 405 emit the unified error body
// (customizable via WithNotFoundHandler/WithMethodNotAllowedHandler).
//
// An error returned by a custom handler must reach writeError rather than be
// discarded: when such a handler returns an error without having written a response,
// discarding it makes net/http emit an implicit empty 200 and the error vanishes.
// writeError checks Written(), so a handler that did write is only recorded, never
// rewritten — both styles stay safe.
func (m *mux) writeMiss(resp *Response, r *http.Request, path string, tsr bool) {
	if tsr && path != "/" && redirectTrailingSlash(resp, r, path) {
		return
	}
	if allow := m.allowedMethods(path); allow != "" {
		resp.Header().Set("Allow", allow)
		if m.methodNotAllowedHandler != nil {
			m.writeError(resp, r, m.methodNotAllowedHandler(r.Context(), m.missRequest(r), resp))
			return
		}
		m.renderMiss(resp, r, http.StatusMethodNotAllowed)
		return
	}
	if m.notFoundHandler != nil {
		m.writeError(resp, r, m.notFoundHandler(r.Context(), m.missRequest(r), resp))
		return
	}
	m.renderMiss(resp, r, http.StatusNotFound)
}

// writeMissRaw 是 writeMiss 的裸 ResponseWriter 版本,用于无全局中间件的零开销路径。
// 它自行把 w 包成一个 Response,自定义 handler 与错误链共用这一个实例(为何不能改用
// writeErrorRaw,见函数内注释)。
// writeMissRaw is the bare-ResponseWriter version of writeMiss for the
// zero-overhead path without global middleware. It wraps w into one Response shared
// by the custom handler and the error chain (see the in-function comment for why
// writeErrorRaw must not be used instead).
func (m *mux) writeMissRaw(w http.ResponseWriter, r *http.Request, path string, tsr bool) {
	if tsr && path != "/" && redirectTrailingSlash(w, r, path) {
		return
	}
	resp := &Response{ResponseWriter: w}
	if allow := m.allowedMethods(path); allow != "" {
		resp.Header().Set("Allow", allow)
		if m.methodNotAllowedHandler != nil {
			// 已有本地 resp 包住 w,直接用 writeError 复用它;若改调 writeErrorRaw 会另建
			// 一个 Response,其 written 恒为 false,handler 已写响应时将被误判为未提交而双写。
			// A local resp already wraps w, so reuse it via writeError; calling
			// writeErrorRaw would build a second Response whose written is always false,
			// misjudging a handler that did write as uncommitted and double-writing.
			m.writeError(resp, r, m.methodNotAllowedHandler(r.Context(), m.missRequest(r), resp))
			return
		}
		m.renderMiss(resp, r, http.StatusMethodNotAllowed)
		return
	}
	if m.notFoundHandler != nil {
		m.writeError(resp, r, m.notFoundHandler(r.Context(), m.missRequest(r), resp))
		return
	}
	m.renderMiss(resp, r, http.StatusNotFound)
}

// missRequest 为 miss 冷路径构造临时 *Request。owner 必须置为 m,否则自定义 404/405
// handler 里的 ClientIP()、fromTrustedProxy() 等访问器会因 owner==nil 短路,退化成直连
// IP 而【不遵循 WithTrustedProxies】——同一个请求命中时返回真实客户端 IP、miss 时返回代理
// IP,按 IP 的限流与审计随之在 miss 路径上失准。
//
// 这里不走 m.pool:池化对象的归还需要与 Response 生命周期配对,而 miss 是冷路径(不在命中
// 热路径上),一次小分配换取无归还时序风险更划算。
// missRequest builds the transient *Request for the miss cold path. owner MUST be m,
// or accessors like ClientIP() and fromTrustedProxy() inside a custom 404/405 handler
// short-circuit on owner==nil and degrade to the direct peer IP, IGNORING
// WithTrustedProxies — the same request would report the real client IP on a hit and
// the proxy IP on a miss, skewing per-IP rate limiting and auditing on the miss path.
//
// It deliberately avoids m.pool: returning a pooled object must be paired with the
// Response lifetime, and a miss is a cold path (off the hit hot path), so one small
// allocation is a better trade than the risk of mistimed returns.
func (m *mux) missRequest(r *http.Request) *Request {
	return &Request{Request: r, owner: m}
}

// renderMiss 用配置的 ErrorRenderer(默认 JSON 错误体)渲染 404/405,使 miss 响应与
// 业务错误体格式一致。仅在 miss 冷路径调用。默认渲染器下的 404/405 走预构建体,零分配。
// renderMiss renders a 404/405 via the configured ErrorRenderer (default JSON
// error body) so miss responses match the business error body. Miss cold path only.
// Under the default renderer, 404/405 use a prebuilt body at zero alloc.
func (m *mux) renderMiss(resp *Response, r *http.Request, status int) {
	if m.onError != nil {
		m.onError(r, status, statusError(status))
	}
	// 默认渲染器 + 已预构建的状态码(404/405) → 直接写预构建切片,免拼接免分配。
	// Default renderer + a prebuilt status (404/405) → write the prebuilt slice
	// directly, skipping assembly and allocation.
	if m.errorRenderer == nil {
		if body, ok := prebuiltMissBody[status]; ok {
			resp.Header().Set("Content-Type", "application/json; charset=utf-8")
			resp.WriteHeader(status)
			_, _ = resp.Write(body)
			return
		}
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
// 返回 false 表示目标不安全、未发送任何响应,调用方应继续走正常的 miss 处理。
// redirectTrailingSlash issues a 301 to the equivalent route: strip the trailing
// slash if present, otherwise append one (matching gin's redirectTrailingSlash).
// Returning false means the target was unsafe and nothing was written, so the
// caller should fall through to normal miss handling.
func redirectTrailingSlash(w http.ResponseWriter, r *http.Request, path string) bool {
	target := path
	if len(path) > 1 && path[len(path)-1] == '/' {
		target = path[:len(path)-1]
	} else {
		target = path + "/"
	}
	if !isSafeRedirectTarget(target) {
		return false
	}
	// 用 url.URL 重新编码而不是拼接解码后的字符串:path 已被标准库解码,若直接拼进
	// Location,其中的 %3F/%23 会还原成裸 ? 与 #,把路径的一部分变成查询串或 fragment
	// (/a%3Fx=1/b/ → /a?x=1/b)。EscapedPath 会把它们重新转义回去。
	// Re-encode via url.URL instead of concatenating the decoded string: path has
	// already been decoded by the stdlib, so splicing it straight into Location would
	// turn any %3F/%23 back into a bare ? or #, converting part of the path into a
	// query string or fragment (/a%3Fx=1/b/ → /a?x=1/b). EscapedPath re-escapes them.
	u := url.URL{Path: target, RawQuery: r.URL.RawQuery}
	code := http.StatusMovedPermanently // 301
	if r.Method != http.MethodGet {
		code = http.StatusPermanentRedirect // 308,保留非 GET 方法与请求体
	}
	http.Redirect(w, r, u.String(), code)
	return true
}

// isSafeRedirectTarget 拒绝会被浏览器解读为跨站地址的 Location 目标。
//
// 路由树按 / 分段匹配,空段会被忽略,因此裸 TCP 请求 `GET //evil.com/` 能命中 /{a}/{b}
// 这类路由并触发 TSR;若原样回写,Location: //evil.com 是协议相对 URL,浏览器会跳到
// https://evil.com —— 一个开放重定向。`/\evil.com` 同理:浏览器把反斜杠等同斜杠处理。
// 这两种前缀都不可能是本服务的合法路径,故直接拒绝重定向而非尝试改写。
// isSafeRedirectTarget rejects Location targets a browser would read as cross-site.
//
// The router matches per / segment and skips empty ones, so a raw request
// `GET //evil.com/` can match a route like /{a}/{b} and trigger TSR; echoing it back
// as Location: //evil.com is a protocol-relative URL and the browser navigates to
// https://evil.com — an open redirect. `/\evil.com` behaves the same because browsers
// treat a backslash as a slash. Neither prefix can be a legitimate path of this
// service, so the redirect is refused rather than rewritten.
func isSafeRedirectTarget(target string) bool {
	if !strings.HasPrefix(target, "/") {
		return false
	}
	if len(target) > 1 && (target[1] == '/' || target[1] == '\\') {
		return false
	}
	return true
}

// allowedMethods 返回所有能匹配 path 的其它 method,以 ", " 连接(按 trees 顺序);
// 无则空串。仅在当前 method 已 miss 时调用,不在命中热路径上。
// autoHEAD 开启且 GET 可匹配时补报 HEAD(除非已有显式 HEAD 树匹配):405 的 Allow 是
// 对客户端的能力承诺,漏掉实际可服务的 HEAD 会让按 Allow 探测的客户端误判。
// allowedMethods returns the other methods matching path, joined by ", " (tree
// order); empty string if none. Called only after a miss, off the hit hot path.
// With autoHEAD on and GET matching, HEAD is reported too (unless an explicit
// HEAD tree already matches): the 405 Allow header is a capability promise, and
// omitting a servable HEAD misleads clients probing via Allow.
func (m *mux) allowedMethods(path string) string {
	var b strings.Builder
	headListed := false
	getMatched := false
	for _, t := range m.trees {
		if t.hasPath(path) {
			if b.Len() > 0 {
				b.WriteString(", ")
			}
			b.WriteString(t.method)
			switch t.method {
			case http.MethodHead:
				headListed = true
			case http.MethodGet:
				getMatched = true
			}
		}
	}
	if m.autoHEAD && getMatched && !headListed {
		b.WriteString(", ")
		b.WriteString(http.MethodHead)
	}
	return b.String()
}

// serve 以单一 recover 包裹 handler 执行,把 panic 收敛为一个错误。
// serve wraps handler execution in a single recover, collapsing a panic into one
// error.
func (m *mux) serve(h compiledHandler, ctx context.Context, req *Request, resp *Response) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			if rec == http.ErrAbortHandler {
				// 见 safeChain 中的同一约定。/ Same contract as in safeChain.
				panic(rec)
			}
			// panic 收敛为 ErrHandlerPanic → 统一错误链 → 500(脱敏),并带上 panic 值。
			// Collapse the panic into ErrHandlerPanic → unified error chain → 500
			// (sanitized), carrying the panic value.
			err = panicError(rec)
		}
	}()
	return h.serve(ctx, req, resp)
}

// RawHandle 注册一个原始处理器(完全接管响应),作为一等公民逃生入口。它与 typed
// 端点共享同一执行路径,并同样经过全局/分组中间件链。经嵌入提升为 Server.RawHandle。
//
// 开启 OpenAPI 收集时它同样登记路由:raw 端点没有可反射的类型,但"路径与方法存在"本身
// 就是契约的一部分——漏掉它们会让 spec 谎报这些端点不存在,契约测试反而被误导。
// RawHandle registers a raw handler (full response ownership) as a first-class
// escape hatch. It shares the same execution path as typed endpoints and is
// likewise wrapped by the global/group middleware chain. Promoted to
// Server.RawHandle via embedding.
//
// With OpenAPI collection enabled it is recorded too: a raw endpoint has no
// reflectable types, but the existence of its path and method is itself part of the
// contract — omitting it would make the spec claim the endpoint does not exist and
// mislead contract tests.
func (m *mux) RawHandle(method, path string, fn RawHandlerFunc) error {
	if err := m.register(method, path, Handler(fn)); err != nil {
		return err
	}
	m.noteRoute(m, method, path, routeDoc{raw: true})
	return nil
}

// MustRawHandle registers a raw route and panics when registration fails.
func (m *mux) MustRawHandle(method, path string, fn RawHandlerFunc) {
	if err := m.RawHandle(method, path, fn); err != nil {
		panic(err)
	}
}
