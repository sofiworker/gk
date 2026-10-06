package ghttp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sofiworker/gk/ghttp/wire"
)

// Server 是 v3 HTTP 服务器，实现 http.Handler。
// Server is the v3 HTTP server; it implements http.Handler.
//
// 生命周期：先 Use/With/Register/Group 完成配置，再 Run/Serve 或直接作为 http.Handler 使用。
// 首个请求（或 Run/Serve）会固化全局中间件链与路由树，之后的注册返回 ErrRegistrationAfterStart。
// Lifecycle: configure with Use/With/Register/Group first, then Run/Serve or use it as an
// http.Handler. The first request (or Run/Serve) freezes the global middleware chain and
// the router; later registrations return ErrRegistrationAfterStart.
type Server struct {
	// router 存储路由树（按 HTTP method 分组）
	// router stores route trees (grouped by HTTP method)
	router *router

	// middleware 存储全局中间件链
	// middleware stores the global middleware chain
	middleware []Middleware

	// routes 是已安装路由的元数据
	// routes is the metadata of installed routes
	routes []RouteInfo

	// defaults 是经 With 设置、对之后注册的路由生效的默认选项
	// defaults are options set via With, applied to routes registered afterwards
	defaults []Option

	// config 存储服务器配置
	// config stores server configuration
	config serverConfig

	// handler 是固化后的全局处理链（全局中间件包裹分发终端）
	// handler is the frozen global chain (global middleware around the dispatch terminal)
	handler Handler

	// freezeOnce 保证处理链只固化一次
	// freezeOnce guarantees the chain is frozen once
	freezeOnce sync.Once

	// started 标记处理链已固化（原子操作）
	// started marks that the chain has been frozen (atomic)
	started atomic.Bool

	// mu 保护注册与固化
	// mu guards registration and freezing
	mu sync.Mutex

	// srvMu 保护 httpServer 与 closed
	// srvMu guards httpServer and closed
	srvMu sync.Mutex

	// httpServer 是底层的 http.Server
	// httpServer is the underlying http.Server
	httpServer *http.Server

	// closed 标记已调用 Shutdown/Close，之后的启动直接返回 ErrServerClosed
	// closed marks that Shutdown/Close was called; later starts return ErrServerClosed
	closed bool

	// hooks 是 OnShutdown 注册的关闭钩子，受 srvMu 保护
	// hooks are the OnShutdown hooks, guarded by srvMu
	hooks []func(context.Context) error

	// hooksOnce 保证关闭钩子只执行一次
	// hooksOnce guarantees the hooks run once
	hooksOnce sync.Once

	// hooksErr 是关闭钩子的合并错误
	// hooksErr is the joined error of the hooks
	hooksErr error
}

// reqRespPair 将 Request 与 Response 成对池化：两者生命周期完全一致（同一次请求中
// 成对获取与归还，见 ServeHTTP），单池把每请求的池操作从 2 Get + 2 Put 减为一组，
// 且两对象内存相邻。
// reqRespPair pools Request and Response together: their lifecycles are identical
// (acquired and released as one pair per request, see ServeHTTP), so a single pool
// halves the pool operations and keeps the two objects adjacent in memory.
type reqRespPair struct {
	req  Request
	resp Response
}

var reqRespPool = sync.Pool{New: func() any { return new(reqRespPair) }}

// ErrRegistrationAfterStart 表示服务启动后尝试注册路由。
// ErrRegistrationAfterStart indicates attempting to register routes after server start.
var ErrRegistrationAfterStart = errors.New("ghttp: cannot register routes after server started")

// ErrServerClosed 表示服务器已关闭。
// ErrServerClosed indicates the server is closed.
var ErrServerClosed = http.ErrServerClosed

// ErrInvalidRequestPath 表示请求路径非法（含 "."/".." 段、控制字符，或严格模式下的空段），映射为 400。
// ErrInvalidRequestPath marks an invalid request path ("."/".." segments, control
// characters, or empty segments in strict mode) and maps to 400.
var ErrInvalidRequestPath = HTTPError{Status: http.StatusBadRequest, Message: "invalid request path"}

// NewServer 创建新的 HTTP 服务器。
// NewServer creates a new HTTP server.
func NewServer(opts ...ServerOption) *Server {
	config := defaultServerConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}
	if config.errorHandler == nil {
		config.errorHandler = defaultErrorHandler
	}
	return &Server{
		router: newRouter(),
		config: config,
	}
}

// Use 添加全局中间件。全局中间件包裹整个分发过程，404/405/TSR 也会经过它们。
// 启动后调用被忽略并经 WithErrorLog 告警；nil 中间件被忽略并告警。
// Use adds global middleware. It wraps the whole dispatch, so 404/405/TSR pass through
// it as well. Calls after start are ignored with a warning via WithErrorLog; nil
// middleware is ignored with a warning.
func (s *Server) Use(middleware ...Middleware) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started.Load() {
		s.logf("ghttp: Use called after the server started; %d middleware ignored", len(middleware))
		return s
	}
	for _, m := range middleware {
		if m == nil {
			s.logf("ghttp: nil middleware passed to Use; ignored")
			continue
		}
		s.middleware = append(s.middleware, m)
	}
	return s
}

// With 追加对之后注册的路由（以及之后创建的 Group）生效的默认路由选项，如 WithMiddleware、
// WithBodyLimit。WithInput/WithOutput 只能用于路由级，放在这里会使注册失败。
// With appends default route options applied to routes registered (and groups created)
// afterwards, such as WithMiddleware and WithBodyLimit. WithInput/WithOutput are
// route-level only and make registration fail here.
func (s *Server) With(opts ...Option) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started.Load() {
		s.logf("ghttp: With called after the server started; options ignored")
		return s
	}
	s.defaults = append(s.defaults, opts...)
	return s
}

// Register 注册路由。整批路由先全部校验与编译，再逐条安装；安装期冲突不会回滚已安装的路由。
// 服务启动后调用返回 ErrRegistrationAfterStart。
// Register registers routes. The whole batch is validated and compiled first, then
// installed one by one; a conflict during installation does not roll back routes already
// installed. Returns ErrRegistrationAfterStart after the server started.
func (s *Server) Register(routes ...Route) error {
	s.mu.Lock()
	defaults := append([]Option(nil), s.defaults...)
	s.mu.Unlock()
	return s.register("", defaults, routes)
}

// Group 创建路由分组，分组继承此刻 Server.With 设置的默认选项。
// Group creates a route group inheriting the Server.With defaults set so far.
func (s *Server) Group(prefix string, opts ...GroupOption) *Group {
	s.mu.Lock()
	defaults := append([]Option(nil), s.defaults...)
	s.mu.Unlock()
	return newGroup(s, prefix, defaults, opts)
}

// preparedRoute 是校验并编译完成、等待安装的路由。
// preparedRoute is a validated, compiled route awaiting installation.
type preparedRoute struct {
	method  string
	path    string
	handler Handler
	info    RouteInfo
}

// register 合并默认选项、校验并编译整批路由，然后安装。
// register merges defaults, validates and compiles the whole batch, then installs it.
func (s *Server) register(prefix string, defaults []Option, routes []Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started.Load() {
		return ErrRegistrationAfterStart
	}

	prepared := make([]preparedRoute, 0, len(routes))
	for _, r := range routes {
		p, err := s.prepare(prefix, defaults, r)
		if err != nil {
			return fmt.Errorf("ghttp: route %s %s: %w", r.Method, joinPaths(prefix, r.Path), err)
		}
		prepared = append(prepared, p)
	}
	for _, p := range prepared {
		route := Route{Method: p.method, Path: p.path, compiledHandler: p.handler}
		if err := s.router.addRoute(route); err != nil {
			return fmt.Errorf("ghttp: failed to register route %s %s: %w", p.method, p.path, err)
		}
		s.routes = append(s.routes, p.info)
	}
	return nil
}

// prepare 校验单个路由并按"默认选项 → 路由选项"的顺序编译处理器。
// prepare validates one route and compiles its handler with defaults first, then route options.
func (s *Server) prepare(prefix string, defaults []Option, r Route) (preparedRoute, error) {
	if err := r.Err(); err != nil {
		return preparedRoute{}, err
	}
	if !validMethod(r.Method) {
		return preparedRoute{}, fmt.Errorf("invalid HTTP method %q", r.Method)
	}
	path, err := canonicalRoutePath(joinPaths(prefix, r.Path))
	if err != nil {
		return preparedRoute{}, err
	}

	var o routeOptions
	o.apply(defaults)
	if o.input != nil || o.output != nil {
		return preparedRoute{}, errors.New("WithInput/WithOutput are route-level options")
	}
	o.apply(r.opts)
	if o.err != nil {
		return preparedRoute{}, o.err
	}

	h := r.compiledHandler
	if r.build != nil {
		if h, err = r.build(o); err != nil {
			return preparedRoute{}, err
		}
	}
	if h == nil {
		return preparedRoute{}, errors.New("nil handler")
	}
	for i := len(o.middleware) - 1; i >= 0; i-- {
		if h = o.middleware[i](h); h == nil {
			return preparedRoute{}, errors.New("middleware returned a nil handler")
		}
	}

	limit := o.bodyLimit
	if limit == 0 {
		limit = s.config.maxBodyBytes
	}
	if limit > 0 && routeMayReadBody(r.meta, o) {
		h = limitBody(limit, h)
	}
	info := RouteInfo{Method: r.Method, Path: path, Kind: r.meta.kind, BodyType: r.meta.bodyType,
		ResultType: r.meta.resultType, Doc: o.doc}
	if info.Kind == "" {
		info.Kind = RouteRaw
	}
	return preparedRoute{method: r.Method, path: path, handler: h, info: info}, nil
}

// limitBody 用 http.MaxBytesReader 限制请求体；读取超限返回 *http.MaxBytesError（映射为 413）。
// 只包装 body，不提前读取，保持 lazy 解码语义。
// limitBody caps the request body with http.MaxBytesReader; reading past it yields
// *http.MaxBytesError (mapped to 413). It only wraps the body and never reads ahead,
// keeping decoding lazy.
func limitBody(limit int64, next Handler) Handler {
	return func(ctx context.Context, req *Request, resp *Response) error {
		if b := req.Raw.Body; b != nil && b != http.NoBody {
			req.Raw.Body = http.MaxBytesReader(resp.Writer, b, limit)
		}
		return next(ctx, req, resp)
	}
}

// routeMayReadBody 报告该路由的处理链是否可能读取请求体。typed NoData 端点与 Procedure
// 从不读取 body（见 newRequestOf：lazyBody 为空），而 MaxBytesReader 只在读取时才生效，
// 因此对这些路由跳过 limitBody 包装层是行为等价的。注意 RFC 9110 §9.3.1：GET 请求体是
// SHOULD NOT 而非禁止，语义未定义，所以不能按方法跳过——POST/PUT 上同样存在不读 body 的
// NoData 端点，这里按"是否可证明不读"判断，与方法无关。Raw 路由可能直接读 Raw.Body，
// 带路由级中间件时中间件也可能读 body，两者一律保守地保留包装。
// routeMayReadBody reports whether the route's handler chain may read the request body.
// Typed NoData endpoints and Procedures never read it (see newRequestOf: lazyBody is nil),
// and MaxBytesReader only takes effect on read, so skipping the limitBody wrapper is
// behavior-equivalent for them. Note RFC 9110 §9.3.1: a GET body is SHOULD NOT, not
// forbidden, with no defined semantics — so the decision is made per route's provable
// reads, not per method (NoData endpoints exist on POST/PUT too). Raw routes may read
// Raw.Body directly, and route-level middlewares may read the body as well; both keep
// the wrapper conservatively.
func routeMayReadBody(meta routeMeta, o routeOptions) bool {
	if meta.kind == RouteRaw || meta.kind == RouteWebSocket || len(o.middleware) > 0 {
		return true
	}
	if meta.kind == RouteProcedure {
		return false
	}
	return meta.bodyType != reflect.TypeFor[NoDataType]()
}

// frozenHandler 固化并返回全局处理链；首次调用时标记服务已启动。
// frozenHandler freezes and returns the global chain, marking the server started on first call.
func (s *Server) frozenHandler() Handler {
	s.freezeOnce.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		h := Handler(s.dispatch)
		for i := len(s.middleware) - 1; i >= 0; i-- {
			if h = s.middleware[i](h); h == nil {
				// 不能跳过该中间件（它可能是鉴权），因此让所有请求失败并告警
				// The middleware cannot be skipped (it may be auth), so fail every request and warn
				s.logf("ghttp: global middleware #%d returned a nil handler; all requests will fail with 500", i)
				h = func(context.Context, *Request, *Response) error {
					return HTTPError{Status: http.StatusInternalServerError, Message: ErrInternalServerError.Message,
						Cause: errors.New("ghttp: global middleware returned a nil handler")}
				}
				break
			}
		}
		s.handler = h
		s.started.Store(true)
	})
	return s.handler
}

// ServeHTTP 实现 http.Handler：先校验路径并匹配路由（使中间件可读取 Request.Route），
// 再执行全局处理链，最后把返回的错误交给错误处理器。
// ServeHTTP implements http.Handler: it validates the path and matches the route first
// (so middleware can read Request.Route), runs the global chain, and hands any returned
// error to the error handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := s.frozenHandler()

	p := reqRespPool.Get().(*reqRespPair)
	req, resp := &p.req, &p.resp
	paramsBuf := req.paramsBuf[:0]
	*req = Request{Raw: r, paramsBuf: paramsBuf}
	*resp = Response{Writer: w}
	defer func() {
		// 归还前清空字段，避免池内对象持有上一次请求的引用（Raw、query 等）；
		// 保留 paramsBuf 容量供下次复用。
		// Clear fields before returning to the pool so it retains no references from
		// the last request (Raw, query, ...); keep the params buffer capacity.
		p.req = Request{paramsBuf: p.req.paramsBuf[:0]}
		p.resp = Response{}
		reqRespPool.Put(p)
	}()

	if !s.config.validatePath || validRequestPath(r.URL.Path, s.config.strictPath) {
		req.match = s.router.findWithParams(r.Method, r.URL.Path, &req.paramsBuf)
		if req.match.params != nil {
			req.Params = req.match.params
		} else {
			req.Params = req.paramsBuf[:0]
		}
		req.matched = req.match.fullPath
	} else {
		req.badPath = true
	}

	ctx := r.Context()
	if err := h(ctx, req, resp); err != nil {
		s.handleError(ctx, req, resp, err)
	}
}

// dispatch 是全局链的终端：执行匹配的路由，否则依次处理非法路径、TSR、自动 OPTIONS、405、404。
// dispatch is the terminal of the global chain: it runs the matched route, otherwise
// handles bad paths, TSR, automatic OPTIONS, 405 and 404 in that order.
func (s *Server) dispatch(ctx context.Context, req *Request, resp *Response) error {
	if h := req.match.handler; h != nil {
		return h(ctx, req, resp)
	}
	if req.badPath {
		return ErrInvalidRequestPath
	}

	r := req.Raw
	if req.match.tsr && r.Method != http.MethodConnect {
		if target, ok := tsrPath(r.URL.Path); ok {
			redirectTrailingSlash(resp, r, target)
			return nil
		}
	}
	if s.config.fixPath && r.Method != http.MethodConnect {
		if target, ok := s.router.fixedPath(r.Method, r.URL.Path); ok && safeRedirectPath(target) {
			redirectTrailingSlash(resp, r, target)
			return nil
		}
	}
	if s.config.methodNotAllowed {
		if allow := s.router.allowed(r.URL.Path); len(allow) > 0 {
			resp.Header().Set("Allow", strings.Join(allowWithOptions(allow), ", "))
			if r.Method == http.MethodOptions {
				resp.WriteHeader(http.StatusNoContent)
				return nil
			}
			return ErrMethodNotAllowed
		}
	}
	if h := s.config.notFoundHandler; h != nil {
		return h(ctx, req, resp)
	}
	return errRouteNotFound
}

// allowWithOptions 在 Allow 列表中补上自动应答的 OPTIONS。
// allowWithOptions adds the automatically answered OPTIONS to the Allow list.
func allowWithOptions(allow []string) []string {
	for _, m := range allow {
		if m == http.MethodOptions {
			return allow
		}
	}
	return append(allow, http.MethodOptions)
}

// safeRedirectPath 报告 p 是否可作为站内重定向目标（不会被浏览器当作协议相对 URL）。
// safeRedirectPath reports whether p is a safe same-site redirect target (not a
// protocol-relative URL to browsers).
func safeRedirectPath(p string) bool {
	return len(p) > 0 && p[0] == '/' && (len(p) == 1 || (p[1] != '/' && p[1] != '\\'))
}

// redirectTrailingSlash 执行站内路径重定向（尾斜杠或路径修正），保留查询串；GET/HEAD 用 301，
// 其他方法用 308。
// redirectTrailingSlash performs a same-site path redirect (trailing slash or fixed path),
// keeping the query string; 301 for GET/HEAD, 308 otherwise.
func redirectTrailingSlash(resp *Response, r *http.Request, target string) {
	u := *r.URL
	u.Path = target
	u.RawPath = ""
	u.Scheme, u.Host, u.User = "", "", nil
	http.Redirect(resp, r, u.String(), tsrStatus(r.Method))
}

// handleError 处理请求执行过程中的错误。响应已提交时无法再改写，只记录日志。
// handleError handles errors during request execution. Once the response is committed it
// can no longer be rewritten, so the error is only logged.
func (s *Server) handleError(ctx context.Context, req *Request, resp *Response, err error) {
	if resp.hijacked {
		s.logf("ghttp: %s %s: error after the connection was hijacked: %s",
			wire.LogToken(req.Raw.Method), wire.LogToken(req.Raw.URL.Path), wire.LogToken(err.Error()))
		return
	}
	if resp.written {
		s.logf("ghttp: %s %s: error after response was committed: %s",
			wire.LogToken(req.Raw.Method), wire.LogToken(req.Raw.URL.Path), wire.LogToken(err.Error()))
		return
	}
	s.config.errorHandler(ctx, req, resp, err)
}

// logf 输出框架内部告警：优先 WithLogger（Warnf），其次 WithErrorLog，最后 log 包。
// logf emits framework warnings: WithLogger (Warnf) first, then WithErrorLog, then the log package.
func (s *Server) logf(format string, args ...any) {
	if s.config.logger != nil {
		s.config.logger.Warnf(format, args...)
		return
	}
	if s.config.errorLog != nil {
		s.config.errorLog.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// defaultErrorHandler 是默认的错误处理器：按 StatusFromError 选择状态码，写出 JSON ErrorResponse。
// defaultErrorHandler is the default error handler: it picks the status via
// StatusFromError and writes a JSON ErrorResponse.
func defaultErrorHandler(_ context.Context, _ *Request, resp *Response, err error) {
	if err == nil || resp.written {
		return
	}
	if errors.Is(err, errRouteNotFound) {
		_ = defaultNotFoundHandler(nil, nil, resp)
		return
	}
	body := ErrorResponseOf(err)
	_ = writeJSON(resp, body.Status, body)
}

var (
	errRouteNotFound        = fmt.Errorf("%w: route not found", ErrNotFound)
	defaultNotFoundBody     = []byte("404 page not found")
	defaultPlainContentType = []string{"text/plain; charset=utf-8"}
)

// defaultNotFoundHandler writes the compact plain-text response used by the
// default NoRoute path. The body and header value are immutable package data.
func defaultNotFoundHandler(_ context.Context, _ *Request, resp *Response) error {
	resp.Header()["Content-Type"] = defaultPlainContentType
	resp.WriteHeader(http.StatusNotFound)
	_, err := resp.Write(defaultNotFoundBody)
	return err
}
