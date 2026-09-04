// Package ghttp 是一个基于标准库 net/http 的、面向 Go 泛型的 typed HTTP 路由框架。
// Package ghttp is a generics-oriented, typed HTTP routing framework built on
// the standard net/http package.
//
// 入口按输入组合分函数（GetParams / PostParams / PostBody / PostParamsBody 等），handler 收裸
// 参数，类型全推断，无包裹容器。入口命名恒为 <Method><InputShape>：七种 method（GET / POST /
// PUT / PATCH / DELETE / HEAD / OPTIONS）各有 None 与 Params 形态，可带请求体的四种
// （POST / PUT / PATCH / DELETE）另有 Body 与 ParamsBody；GET / HEAD / OPTIONS 不提供 Body
// 入口，因为按 RFC 9110 它们的请求体没有定义语义。params 经 struct tag（path:/query:/header:）
// 绑定,只承载传输层参数;请求体经 InputSpec[B] 解码(内置 JSONBody[B]/XMLBody[B]/FormBody[B]/
// TextBody[B],或 Body[B](codec) 传自定义解码器),表单文本字段(form: tag)与上传文件
// (Upload / []Upload 字段)都归请求体,由 FormBody[B]() 一并解码;输出经 OutputSpec[O] 编码;
// 需完全接管响应时用 RawHandle。
// Entries are split by input shape (GetParams / PostParams / PostBody / PostParamsBody, etc.);
// handlers take naked parameters with all type parameters inferred and no wrapper
// container. Entries are always named <Method><InputShape>: each of the seven methods
// (GET / POST / PUT / PATCH / DELETE / HEAD / OPTIONS) has None and Params shapes, and
// the four body-bearing ones (POST / PUT / PATCH / DELETE) additionally have Body and
// ParamsBody; GET / HEAD / OPTIONS expose no Body entry because per RFC 9110 their
// request bodies have no defined semantics. Params bind via struct tags
// (path:/query:/header:) and carry only transport parameters; the body is decoded by
// an InputSpec[B] (built-in JSONBody[B]/XMLBody[B]/FormBody[B]/TextBody[B], or
// Body[B](codec) for a custom decoder), with form text fields (form: tag) and
// uploaded files (Upload / []Upload fields) both belonging to the body and decoded
// together by FormBody[B](); output is encoded by an OutputSpec[O]; use RawHandle to
// fully own the response.
//
// 核心取向:单一 typed 执行模型 + 显式 RawHandler 逃生;纯 net/http 地基,
// 不引入 fasthttp 或自管 TCP;性能红利只来自池化上下文与零反射 codec。
// Core stance: a single typed execution model plus an explicit RawHandler escape
// hatch; a pure net/http foundation with no fasthttp or self-managed TCP;
// performance comes only from pooled contexts and zero-reflection codecs.
//
// # Server 与中间件模型 / Server and middleware model
//
// New 返回 *Server —— 本包唯一的顶层类型，它【就是】一个 HTTP server：内部组合路由
// 核心与 *http.Server，用户在它上面挂中间件、注册路由，然后 Run / Shutdown，不必再
// 认识第二个类型。它同时实现 http.Handler，可直接塞进 httptest 或他人的 http.Server。
//
// 全局中间件经 Server.Use 追加，在 ServeHTTP 期组装到统一分发器外层（gin 语义），
// 因此对【所有】请求生效——包括未命中路由与未注册 OPTIONS 的预检。这意味着 CORS 等
// 横切中间件用普通 Use 即可正确处理预检，无需任何路由前包装。分组中间件（Group）只
// 折叠自己那几层，与全局相加即该路由的完整链，各执行一次。无全局中间件时走零开销直连
// 路径；有则把链折叠一次并缓存，请求期零组装、零额外分配。
//
// New returns a *Server — this package's only top-level type. It IS an HTTP
// server: internally composing the routing core and an *http.Server, so users
// attach middleware, register routes, then Run / Shutdown on it without meeting a
// second type. It also implements http.Handler and drops straight into httptest or
// someone else's http.Server.
//
// Global middleware appended via Server.Use is assembled around a unified
// dispatcher at ServeHTTP time (gin semantics), so it applies to ALL requests —
// including route misses and unregistered-OPTIONS preflight. Thus cross-cutting
// middleware like CORS handles preflight via a plain Use, with no pre-routing
// wrapper. Group middleware folds only its own layers; together with the global
// stack it forms the route's complete chain, each running exactly once. With no
// global middleware it takes a zero-overhead direct path; with it, the chain is
// folded once and cached, assembling nothing and allocating nothing per request.
//
// # 生产能力 / Production capabilities
//
// 除路由与 typed 入口外，本包提供可直接部署真实服务所需的组件：
// Beyond routing and typed entries, the package ships the pieces needed to run a
// real service:
//
//   - 启动与关闭：Server.Run / RunTLS（按地址监听）、Serve / ServeTLS（复用已有
//     net.Listener）、Shutdown（优雅关闭）、Close（强制关闭）、RunGraceful（一体化
//     信号编排：SIGTERM→摘流→排水→超时强关）；超时、TLS、BaseContext 等经 New 的
//     Option 配置。
//
//   - 中间件：RequestID、Logger、LimitBody 之外，另有 Recovery（panic→500 且不
//     泄露细节）、CORS（含预检）、Timeout（协作式，尊重池化生命周期）、BasicAuth
//     （恒定时间比较，认证用户经 BasicAuthUser 下传）、Gzip（按 Content-Type 白名单
//     条件压缩，gzip.Writer 池化，未挂载零影响）、RateLimit / RateLimitByRoute
//     （分片令牌桶，默认按 ClientIP 限流，超限 429 + Retry-After，空闲桶自动回收）、
//     CSRF（双提交 cookie + 同源校验，恒定时间比较）、SecureHeaders（nosniff /
//     DENY / Referrer-Policy 安全默认，HSTS 与 CSP 需显式开启）。
//
//   - 静态资源：Static / StaticFS（支持 os.DirFS 与 embed.FS）、File；默认不列
//     目录，支持目录索引、自定义索引名、SPA history 回退；WithPrecompressed /
//     WithPrecompressedEncodings 在客户端可接受且磁盘存在 .br / .gz 变体时直接服务
//     预压缩文件（把压缩成本移到构建期，并自动声明 Vary: Accept-Encoding）。
//
//   - OpenAPI 3.1：WithOpenAPI 开启后，注册期从 typed 入口的 params/body/output 类型
//     收集契约，首次请求时构建一次 spec 并缓存字节，默认在 /openapi.json 暴露
//     （WithOpenAPIRoute 可改路径或置空只在内存构建，Server.SpecJSON 取字节）。
//     参数说明复用请求期同一份 BindPlan，故文档与实际绑定行为不会漂移；输出为确定性
//     字节（同一路由表恒等），可纳入版本控制与契约测试。未开启时不收集、不构建、
//     不注册路由，零成本。
//
//   - 健康检查：Health（liveness）、Ready（readiness，多 Checker）、
//     NewReadinessGate（运行时开关，用于启动完成/开始排水）。
//
//   - 统一错误链：typed handler / codec 返回的 error 经统一出口分类为 HTTP 状态码
//     （实现 StatusCoder 的业务错误自带状态码，否则按框架哨兵映射，兜底 500），默认
//     脱敏（只回通用文案，细节仅进日志；WithExposeErrorDetails 可开）；错误体默认
//     JSON（WithErrorRenderer 可换）；body 入口默认严格校验请求 Content-Type（不符
//     415，WithStrictContentType 可关）；404/405 输出统一错误体且可用
//     WithNotFoundHandler / WithMethodNotAllowedHandler 定制。
//
//   - 可观测性：命中后 Request.MatchedRoute 返回低基数的路由模板（如 /users/:id），
//     适合作为 metrics/tracing/日志的路由维度；Request.ClientIP / RemoteIP 按可信代理
//     模型（WithTrustedProxies / WithForwardedHeaders，默认不信任转发头以防伪造）解析
//     真实客户端 IP；Logger / LoggerWith 输出结构化 AccessLog（含 Route、ClientIP、
//     BytesOut、Err）；NewMetrics / MetricsRegistry 提供 Prometheus 文本格式的请求
//     指标（计数/延迟/响应大小/在途请求数，按 MatchedRoute 聚合），零依赖可抓取。
//
//   - 参数绑定：params 字段支持 int8/16/32/64、uint*、float*、bool、string 等标量,
//     *T 指针（nil 表示"未提供"，可与显式零值区分）、实现 encoding.TextUnmarshaler
//     的类型（time.Time、net.IP 等）、[]byte（取原始字节）、[]T / [N]T 列表（重复出现
//     ?a=1&a=2 与逗号分隔 ?a=1,2 两种风格可混用）、map[string]T / map[string][]T
//     （query 用 filter[key]=v，header 用 X-Meta- 前缀族或 `*` 收全部头）。无 tag 的
//     内嵌与嵌套结构体递归展开，公共参数可抽成可复用的结构体；tag 值 "-" 显式跳过字段。
//     path/query/header 与请求体表单（form: tag）共用同一套绑定引擎，能力完全对等。
//     按字段类型解析,越界或类型不符报 400（ErrInvalidInput）。本框架不内置校验,
//     业务规则由 handler 自行判断。
//
//   - Startup/shutdown: Server.Run / RunTLS (listen on an address), Serve /
//     ServeTLS (reuse an existing net.Listener), Shutdown (graceful), Close
//     (forced), and RunGraceful (all-in-one signal orchestration:
//     SIGTERM→drain-readiness→drain→force-close on timeout); timeouts, TLS, and
//     BaseContext are configured via New's Options.
//
//   - Middleware: besides RequestID, Logger, LimitBody, there are Recovery
//     (panic→500 without leaking details), CORS (with preflight), Timeout
//     (cooperative, respecting the pooled lifecycle), BasicAuth (constant-time
//     comparison, authenticated user passed down via BasicAuthUser), Gzip
//     (conditional compression per a Content-Type whitelist, gzip.Writer pooled,
//     zero impact when unmounted), RateLimit / RateLimitByRoute (a sharded token
//     bucket keyed by ClientIP by default, answering 429 + Retry-After over the
//     limit and reaping idle buckets), CSRF (double-submit cookie plus a
//     same-origin check, compared in constant time), and SecureHeaders (nosniff /
//     DENY / Referrer-Policy safe defaults, with HSTS and CSP opt-in).
//
//   - Static assets: Static / StaticFS (os.DirFS and embed.FS) and File; no
//     directory listing by default, with directory index, custom index name, and
//     SPA history fallback; WithPrecompressed / WithPrecompressedEncodings serve a
//     pre-compressed .br / .gz variant when the client accepts it and the file
//     exists (moving compression cost to build time and declaring
//     Vary: Accept-Encoding automatically).
//
//   - OpenAPI 3.1: once WithOpenAPI is enabled, the contract is collected at
//     registration from each typed entry's params/body/output types, the spec is
//     built once on the first request and its bytes cached, and it is exposed at
//     /openapi.json by default (WithOpenAPIRoute changes the path, or an empty path
//     builds in memory only; Server.SpecJSON returns the bytes). Parameter docs
//     reuse the very same BindPlan the request path uses, so documentation cannot
//     drift from actual binding; output is deterministic (identical bytes for one
//     route table), making it committable and contract-testable. Nothing is
//     collected, built, or registered when disabled — zero cost.
//
//   - Health checks: Health (liveness), Ready (readiness with multiple Checkers),
//     and NewReadinessGate (a runtime toggle for startup-complete / draining).
//
//   - Unified error chain: errors from typed handlers/codecs pass through a single
//     exit that classifies them into HTTP status codes (a business error
//     implementing StatusCoder carries its own status, else framework sentinels
//     map it, with a 500 fallback), sanitized by default (generic text only,
//     details to logs; opt in via WithExposeErrorDetails); the error body is JSON
//     by default (swap via WithErrorRenderer); body entries strictly verify the
//     request Content-Type by default (415 on mismatch, disable via
//     WithStrictContentType); 404/405 emit the unified error body and are
//     customizable via WithNotFoundHandler / WithMethodNotAllowedHandler.
//
//   - Observability: after a hit, Request.MatchedRoute returns the low-cardinality
//     route template (e.g. /users/:id) suited for the route dimension in
//     metrics/tracing/logging; Request.ClientIP / RemoteIP resolve the real client
//     IP under a trusted-proxy model (WithTrustedProxies / WithForwardedHeaders,
//     trusting no forwarded header by default to prevent spoofing); Logger /
//     LoggerWith emit a structured AccessLog (with Route, ClientIP, BytesOut, Err);
//     NewMetrics / MetricsRegistry provide Prometheus-text-format request metrics
//     (count, duration, response size, in-flight, aggregated by MatchedRoute),
//     zero-dep and scrapable.
//
//   - Parameter binding: params fields accept int8/16/32/64, uint*, float*, bool,
//     and string scalars; *T pointers (nil meaning "not provided", distinguishable
//     from an explicit zero); types implementing encoding.TextUnmarshaler
//     (time.Time, net.IP, …); []byte (raw bytes); []T / [N]T lists (repetition
//     ?a=1&a=2 and comma separation ?a=1,2, mixable); and map[string]T /
//     map[string][]T (filter[key]=v in a query, an X-Meta- prefix family or `*` for
//     every header). Untagged embedded and nested structs expand recursively so
//     shared parameters can be factored into reusable structs, and a "-" tag value
//     skips a field explicitly. path/query/header and body form fields (form: tag)
//     share one binding engine, so their capabilities are exactly at parity. Values
//     are parsed per field type; out-of-range or type mismatch yields 400
//     (ErrInvalidInput). The framework has no built-in validation; business rules
//     are checked by the handler itself.
//
//   - 实时能力 / Real-time: Response 实现 http.Flusher 与 http.Hijacker(经 Flush /
//     Hijack 透传底层连接)。在 RawHandle 之上,NewSSEWriter 提供 Server-Sent Events
//     语法糖(Send / SendEvent / SendMessage / Comment / Ping,自动设头并逐事件 Flush);
//     WebSocket 经 github.com/gorilla/websocket 升级——WSUpgrader 包装 Upgrader、
//     Upgrade 在 RawHandle 内握手、ServeWS 一行注册升级端点(升级失败或连接关闭时收尾)。
//     ghttp 只负责让 Response 可 Hijack,协议实现委托给 gorilla。
//     Response implements http.Flusher and http.Hijacker (Flush / Hijack pass
//     through to the underlying connection). Over RawHandle, NewSSEWriter provides
//     Server-Sent Events sugar (Send / SendEvent / SendMessage / Comment / Ping,
//     setting headers and flushing per event); WebSocket upgrades via
//     github.com/gorilla/websocket — WSUpgrader wraps Upgrader, Upgrade performs the
//     handshake inside a RawHandle, and ServeWS registers an upgrade endpoint in one
//     call (tearing down on a failed upgrade or a closed connection). ghttp only
//     makes Response hijackable; the protocol is delegated to gorilla.
package ghttp
