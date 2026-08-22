// Package ghttp 是一个基于标准库 net/http 的、面向 Go 泛型的 typed HTTP 路由框架。
// Package ghttp is a generics-oriented, typed HTTP routing framework built on
// the standard net/http package.
//
// 入口按输入组合分函数（GetParams / PostBody / PostParamsBody 等），handler 收裸
// 参数，类型全推断，无包裹容器。params 经 struct tag（path:/query:/header:）绑定，
// 请求体经 RequestDecoder 解码，输出经 OutputSpec 编码；需完全接管响应时用 RawHandle。
// Entries are split by input shape (GetParams / PostBody / PostParamsBody, etc.);
// handlers take naked parameters with all type parameters inferred and no wrapper
// container. Params bind via struct tags (path:/query:/header:), the body is
// decoded by a RequestDecoder, and output is encoded by an OutputSpec; use
// RawHandle to fully own the response.
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
//     net.Listener）、Shutdown（优雅关闭）、Close（强制关闭）；超时、TLS、
//     BaseContext 等经 New 的 Option 配置。
//
//   - 中间件：RequestID、Logger、LimitBody 之外，另有 Recovery（panic→500 且不
//     泄露细节）、CORS（含预检）、Timeout（协作式，尊重池化生命周期）。
//
//   - 静态资源：Static / StaticFS（支持 os.DirFS 与 embed.FS）、File；默认不列
//     目录，支持目录索引、自定义索引名、SPA history 回退。
//
//   - 健康检查：Health（liveness）、Ready（readiness，多 Checker）、
//     NewReadinessGate（运行时开关，用于启动完成/开始排水）。
//
//   - Startup/shutdown: Server.Run / RunTLS (listen on an address), Serve /
//     ServeTLS (reuse an existing net.Listener), Shutdown (graceful), and Close
//     (forced); timeouts, TLS, and BaseContext are configured via New's Options.
//
//   - Middleware: besides RequestID, Logger, LimitBody, there are Recovery
//     (panic→500 without leaking details), CORS (with preflight), and Timeout
//     (cooperative, respecting the pooled lifecycle).
//
//   - Static assets: Static / StaticFS (os.DirFS and embed.FS) and File; no
//     directory listing by default, with directory index, custom index name, and
//     SPA history fallback.
//
//   - Health checks: Health (liveness), Ready (readiness with multiple Checkers),
//     and NewReadinessGate (a runtime toggle for startup-complete / draining).
package ghttp
