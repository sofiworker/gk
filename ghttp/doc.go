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
// # 引擎与中间件模型 / Engine and middleware model
//
// New 返回 *Engine（Mux 为其兼容别名）。Engine 既是 http.Handler，又自带一步式
// Run / RunTLS / Shutdown（gin 风格），无需显式构造 Server；需要精细控制底层
// http.Server 时再用 NewServer(engine, opts...)。全局中间件经 Use 追加，在 ServeHTTP
// 期组装到统一分发器外层（gin 语义），因此对【所有】请求生效——包括未命中路由与未
// 注册 OPTIONS 的预检。这意味着 CORS 等横切中间件用普通 Use 即可正确处理预检，无需
// 任何路由前包装。无全局中间件时走零开销直连路径；有则把链折叠一次并缓存，请求期零组装。
// New returns an *Engine (Mux is its compatibility alias). Engine is both an
// http.Handler and carries one-liner Run / RunTLS / Shutdown (gin-style), needing
// no explicit Server; use NewServer(engine, opts...) for fine-grained http.Server
// control. Global middleware appended via Use is assembled around a unified
// dispatcher at ServeHTTP time (gin semantics), so it applies to ALL requests —
// including route misses and unregistered-OPTIONS preflight. Thus cross-cutting
// middleware like CORS handles preflight via a plain Use, with no pre-routing
// wrapper. With no global middleware it takes a zero-overhead direct path; with it,
// the chain is folded once and cached, assembling nothing per request.
//
// # 生产能力 / Production capabilities
//
// 除路由与 typed 入口外，本包提供可直接部署真实服务所需的组件：
// Beyond routing and typed entries, the package ships the pieces needed to run a
// real service:
//
//   - 启动与关闭：Engine.Run / RunTLS（一步式，超时等经 ServerOption 透传）、
//     Engine.Shutdown（优雅关闭）；或 NewServer 走底层 http.Server 精细控制
//     （ListenAndServeTLS / Serve / ServeTLS / Close / WithTLSConfig 等）。
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
//   - Startup/shutdown: Engine.Run / RunTLS (one-liner, timeouts via ServerOption)
//     and Engine.Shutdown (graceful); or NewServer for fine-grained http.Server
//     control (ListenAndServeTLS / Serve / ServeTLS / Close / WithTLSConfig, ...).
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
