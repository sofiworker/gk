# ghttp

> **⚠️ 开发中 · 禁止直接用于生产开发**
>
> 本包属于 [gk](../README.md) 仓库的一部分，整体仍处于 pre-v1.0.0 阶段。API 可能随时破坏性变更，行为与文档尚未冻结，也未经过生产环境验证。**禁止直接用于生产开发**。

[English](README.en.md) | 中文

`ghttp` 是基于标准库 `net/http` 的 **typed HTTP 路由框架**：handler 收裸参数、类型全推断、无包裹容器；同时内置生产所需的统一错误链、优雅退出、健康检查、指标、CORS、Recovery、Gzip 等组件，开箱即可部署真实服务。

与 gin/echo 的核心差异：**typed 优先**。入口按输入形态分函数（`GetParams` / `PostBody` / `PostParamsBody` …），handler 签名即契约，注册期完成反射与绑定计划编译，请求期零反射、零额外分配。

## 目录

- [快速开始](#快速开始)
- [typed API 速览](#typed-api-速览)
- [生命周期与错误处理](#生命周期与错误处理)
- [中间件模型](#中间件模型)
- [功能清单与开关](#功能清单与开关)
- [已知差异与权衡](#已知差异与权衡)
- [迁移指南](#迁移指南)

## 快速开始

```go
package main

import (
    "context"
    "log"

    "github.com/sofiworker/gk/ghttp"
)

type EchoIn struct {
    Name string `path:"name"`
    Lang string `query:"lang"`
}

type EchoOut struct {
    Msg string `json:"msg"`
}

func main() {
    s := ghttp.New()
    s.Use(ghttp.Logger(), ghttp.Recovery())

    // GET /hello/:name?lang=zh
    _ = ghttp.GetParams[EchoIn, EchoOut](s, "/hello/:name",
        ghttp.JSON[EchoOut](),
        func(ctx context.Context, in EchoIn) (EchoOut, error) {
            return EchoOut{Msg: "hello " + in.Name + " (" + in.Lang + ")"}, nil
        },
    )

    log.Fatal(s.Run(":8080"))
}
```

```bash
$ curl -i 'http://127.0.0.1:8080/hello/world?lang=zh'
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8

{"msg":"hello world (zh)"}
```

三件事：`ghttp.New()` 构造 `*Server`；`Use` 挂全局中间件；typed 入口把 path/query/header 绑到 `EchoIn`，输出经 `ghttp.JSON[EchoOut]()` 编码。`Run(addr)` 阻塞监听。

## typed API 速览

### 路由入口按输入形态分

| 入口 | 输入形态 | handler 签名 |
|------|----------|--------------|
| `GetNone[O]` | 无 params 无 body | `func(ctx) (O, error)` |
| `GetParams[P, O]` / `DeleteParams` / `PostParams` / `PutParams` / `PatchParams` | 仅 params | `func(ctx, P) (O, error)` |
| `PostBody[B, O]` / `PutBody` / `PatchBody` | 仅 body | `func(ctx, B) (O, error)` |
| `PostParamsBody[P, B, O]` / `PutParamsBody` / `PatchParamsBody` | params + body | `func(ctx, P, B) (O, error)` |

所有类型参数均由编译器推断，调用方不手写类型参数列表。需要完全接管响应时用 `RawHandle(method, path, RawHandlerFunc)`——它与 typed 端点共享同一执行路径与中间件链，是一等公民逃生入口。

### Input struct tag

params 必须是结构体，字段用 tag 标注来源：

```go
type GetUser struct {
    ID    int64  `path:"id"`              // 路径参数，自动按字段类型解析
    Page  int     `query:"page"`          // query 参数，缺失时零值
    Trace string  `header:"X-Trace-Id"`   // 请求头
    Token string  `query:"token" validate:"required"` // 必填校验
}
```

- 支持 `path:` / `query:` / `header:` 三类来源；未打 tag 的字段静默跳过。
- 标量字段支持 `int8/16/32/64`、`uint*`、`float*`、`bool`、`string`，越界报 400。
- `validate:"..."` 支持 `required` / `min` / `max` / `len` / `oneof` / `email`，**注册期**编译为闭包，请求期零 tag 解析。
- `Upload`（单文件）/ `[]Upload`（多文件）字段自动绑定 multipart 文件，详见下节。

### 文件上传（multipart）

在 params 结构体里放 `Upload` 或 `[]Upload` 字段，用 `form:` tag 指定表单字段名，multipart 文件即自动绑定：

```go
type UploadReq struct {
    Avatar ghttp.Upload   `form:"avatar"`                    // 单文件，可选
    Docs   []ghttp.Upload `form:"docs" validate:"required"`  // 多文件，必填
    Note   string         `form:"note" query:"note"`         // 与文本字段混用
}

ghttp.PostParams(server, "/upload", ghttp.JSON[Resp](),
    func(ctx context.Context, p UploadReq) (Resp, error) {
        // 便捷落盘（流式，不整体载入内存）；path 由你决定，务必净化 Filename 防目录穿越
        if err := p.Avatar.Save("/data/" + sanitize(p.Avatar.Filename)); err != nil {
            return Resp{}, err
        }
        for _, d := range p.Docs {
            data, _ := d.Bytes()          // 或读入内存
            _ = data
        }
        return Resp{}, nil
    })
```

- **单/多文件**：`Upload` 绑定同名首个文件；`[]Upload` 绑定同名全部文件（对应 `<input multiple>`）。
- **可选 vs 必填**：默认可选——缺文件时 `Upload` 保留零值（`Open == nil` 可判空）、`[]Upload` 为 nil；标 `validate:"required"` 后缺文件返回 **400 `missing_required`**。
- **便捷方法**：`Upload.Save(path)` 流式落盘、`Upload.Bytes()` 读入内存、`Upload.Open()` 拿 `multipart.File` 自行流式处理；`Upload.Filename`/`Size`/`ContentType`/`Header` 提供元数据。
- **安全**：`Filename` 是客户端声明的不可信值，`Save` 不据它拼路径，落盘路径与净化由调用方负责。
- 单结构体可含多个不同名上传字段，且可与 params（path/query/header）、body 解码器（`PostParamsBody`）自由混用。

若只需 multipart 的**文本字段**（不含文件），用 `FormBody()` 解码器把它们绑到 body 结构体的 `form:` tag 字段。

### OutputSpec：显式声明输出契约

`(O, error)` 不默认 JSON、不默认 200——格式与状态码必须显式声明：

```go
ghttp.JSON[User]()                          // 200 + application/json
ghttp.JSON[User]().Status(201)              // 201 + application/json
ghttp.NoContent[struct{}]()                 // 204，无响应体
ghttp.JSON[User]().WithEncoder(myEncoder)   // 自定义编码器（XML、模板等）
```

默认 JSON 输出直接序列化对象本身（无外层包裹）。错误响应统一走[统一错误链](#统一错误链)，信封为 `{"error":{"code":"...","message":"..."}}`。

## 生命周期与错误处理

### Server 状态机

`Server` 是本包唯一的顶层类型——它**就是** HTTP server，内部组合路由核心（`mux`，零偏移嵌入）与 `*http.Server`。三态生命周期：

```
        Run / RunTLS / Serve / ServeTLS
idle ───────────────────────────────► running
  │                                     │
  │ 监听失败（端口占用等）              │ Shutdown / Close
  │ 退回 idle，可换 addr 重试          ▼
  └──────────────────────────────── closed（终态，不可复用）
```

| 方法 | 语义 |
|------|------|
| `Run(addr)` | 明文 HTTP，阻塞。`addr` 非空覆盖 `WithAddr`；二者皆空默认 `:8080`。正常关闭返回 `ErrServerClosed`（`errors.Is` 判定）。 |
| `RunTLS(addr, certFile, keyFile)` | HTTPS，默认 `:8443`。已注入 `TLSConfig` 时 cert/key 可空。 |
| `Serve(l net.Listener)` / `ServeTLS` | 复用已有 listener（Unix socket、限流监听、测试注入）。 |
| `Shutdown(ctx)` | 优雅关闭：先关 listener 拒新连接，再等进行中请求完成，直到 ctx 到期。**幂等**，每个并发调用者都等到真正排空完成。 |
| `Close()` | 立即强制关闭，不等待进行中请求。仅用于紧急停止。 |
| `RunGraceful(addr, opts...)` | 一体化信号编排：SIGTERM → 摘流（`ReadinessGate.Set(false)`）→ `drainDelay` → `Shutdown(timeout)` → 超时 `Close`。正常关闭返回 `nil`（把 `ErrServerClosed` 归一化）。 |

### IsStarted / IsClosed 的正确语义

- `IsStarted()` 报告 Server 是否**正在服务**（已启动且尚未关闭）。关闭后返回 `false`——"正在运行"与"已关闭"互斥。
- `IsClosed()` 报告 Server 是否**已关闭**（终态）。
- 重复 `Run` 返回 `ErrServerStarted`；关闭后再 `Run` 返回 `ErrServerNotStartable`。

### 404 / 405 统一错误体

默认情况下，未命中路由与未注册方法的响应**不是裸 `WriteHeader`**，而是输出与业务错误格式一致的 JSON 错误体：

```json
{"error":{"code":"not_found","message":"Not Found"}}
```

定制：

```go
ghttp.New(
    ghttp.WithNotFoundHandler(func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
        resp.WriteHeader(404)
        _, _ = resp.Write([]byte("custom 404"))
        return nil
    }),
    ghttp.WithMethodNotAllowedHandler(my405Handler),
)
```

### 统一错误链

typed handler / codec / 校验返回的 `error` 经统一出口分类为 HTTP 状态码：

1. **StatusCoder 优先**：业务错误实现 `HTTPStatus() int` 即自带状态码（可精确控制 404/409/422 等）。
2. **框架哨兵映射**：`ErrInvalidInput` → 400、`ErrValidation` → 400、`ErrUnsupportedMediaType` → 415、`ErrRequestEntityTooLarge` → 413、`ErrHandlerPanic` → 500 等。
3. **兜底 500**：未识别的 error 一律 500。

默认**脱敏**：响应体只回通用文案（如 `"Bad Request"`），内部细节仅进服务端日志。`WithExposeErrorDetails(true)` 开启后 `message` 字段回传 `err.Error()`（仅建议开发/内网调试用）。

错误体默认 JSON，可经 `WithErrorRenderer` 替换（例如 RFC 9457 `problem+json`）。`WithErrorHook` 注册观测钩子用于结构化日志/上报，它不写响应。

## 中间件模型

### 全局中间件（Server.Use）

`Server.Use(mws...)` 追加到全局栈，对**所有**请求生效——包括未命中路由与未注册 OPTIONS 的预检。这意味着 CORS 等横切中间件用普通 `Use` 即可正确处理预检，无需任何路由前包装。

约束：须在注册路由与开始服务前调用（gin 同款约束）；服务开始后的追加不影响已折叠的链。

### 分组中间件（Group）

`Group` 拼接公共前缀并携带一份**仅属于本组**的中间件快照。分组中间件只折叠自己那几层，与全局相加即该路由的完整链，各执行一次。

```go
api := s.Group("/api/v1", authMiddleware)
{
    users := api.Group("/users")
    _ = ghttp.GetParams[...](users, "/:id", ...)   // 路径：/api/v1/users/:id
    _ = ghttp.PostBody[...](users, "", ...)        // 路径：/api/v1/users
}
```

快照发生在创建时（gin 语义）：此后对父组的 `Use` 不影响本组。

### 零开销直连

无全局中间件时走 `dispatchRaw` 零开销路径：直接借池化上下文匹配并执行，不构造链。有全局中间件时，首个请求把链折叠一次并缓存（`sync.Once`），此后每请求零组装、零额外分配。

## 功能清单与开关

### Content-Type 严格校验

body 入口默认在解码前校验请求 `Content-Type` 与端点声明的 `RequestDecoder.ContentType()` 一致，不符即 **415**。

```go
s := ghttp.New(ghttp.WithStrictContentType(false)) // 关闭，回退旧宽松行为
```

### ErrorRenderer 与细节泄露

```go
s := ghttp.New(
    ghttp.WithExposeErrorDetails(true),   // 回传 err.Error()（调试用）
    ghttp.WithErrorRenderer(myRenderer),  // 替换默认 JSON 错误体
)
```

### 内置中间件

| 中间件 | 作用 |
|--------|------|
| `RequestID()` | 前处理：确定请求 ID（优先回显客户端 `X-Request-ID`），写响应头并经 context 下传。`RequestIDFromContext(ctx)` 读取。 |
| `Logger()` / `LoggerWith(fn)` | 后处理：记录结构化 `AccessLog`（含 Route、ClientIP、BytesOut、Err）。 |
| `Recovery()` / `RecoveryWith(handler)` | 捕获 panic → 记录堆栈 → 写 500（不泄露细节）。 |
| `CORS(cfg)` | 跨域：按 `CORSConfig` 写头并处理预检。`CORSDefault()` 提供宽松默认。 |
| `Timeout(d)` | 协作式请求超时：注入 deadline context，下游超时未提交响应补写 503。 |
| `BasicAuth(realm, accounts)` | HTTP Basic 认证（恒定时间比较）；`BasicAuthUser(ctx)` 读取认证用户。 |
| `Gzip(opts...)` | 按 Content-Type 白名单条件压缩，`gzip.Writer` 池化，未挂载零影响。 |
| `LimitBody(maxBytes)` | 请求体大小限制，超限 413。 |
| `(*MetricsRegistry).Middleware()` | 请求指标收集（计数/延迟/响应大小/在途），按 `MatchedRoute` 低基数聚合。 |

### 健康检查与就绪探针

```go
_ = ghttp.Health(s, "/healthz")                              // 存活探针，恒 200
gate, checker := ghttp.NewReadinessGate("startup")           // 运行时开关
_ = ghttp.Ready(s, "/readyz", 3*time.Second,                 // 就绪探针
    checker,
    ghttp.Checker{Name: "db", Check: db.Ping},
)
gate.Set(true, nil) // 启动完成后置为就绪
```

`RunGraceful` 收到停止信号后先 `gate.Set(false, ErrShuttingDown)` 让 LB 摘流，再排水。

### 静态资源

```go
_ = ghttp.Static(s, "/assets/", "./static")                  // 磁盘目录
_ = ghttp.StaticFS(s, "/assets/", myEmbedFS,                 // embed.FS
    ghttp.WithSPAFallback(),                                 // SPA history 回退
)
```

默认不列目录（安全默认）；`WithBrowsable()` 开启目录列表；`WithIndexFile(name)` 自定义索引名。

### 可观测性

- `Request.MatchedRoute()` 返回低基数路由模板（如 `/users/:id`），适合作为 metrics/tracing/日志的路由维度。
- `Request.ClientIP()` / `RemoteIP()` 按可信代理模型解析真实客户端 IP；默认**不信任**转发头以防伪造，经 `WithTrustedProxies` / `WithForwardedHeaders` 配置。
- `NewMetrics()` + `MetricsRegistry.Handler()` 暴露 Prometheus 文本格式 `/metrics` 端点，零第三方依赖。

### 参数校验

- params 字段 `validate:"required|min=1|max=100|oneof=a b c|email"` 注册期编译为闭包，请求期零 tag 解析。
- 请求体类型实现 `Validator` 接口（`Validate() error`）即在解码后自动校验；不实现时零额外开销。
- 校验失败统一归 `ErrValidation`（→400），除非 error 自带 `StatusCoder`。

## 已知差异与权衡

### 与 gin / httprouter 对比

- **typed 路径有固定信封/编码开销**：请求期零反射，但注册期需编译 bindPlan；纯路由场景（`PathParam1`）比 gin 慢 ~4.6×（387ns vs 83ns），命中热路径（`JSONBind`、`FullChain`）差距收窄到 1.09~1.15×。详见 [`benchmarks/RESULTS.md`](../benchmarks/RESULTS.md)。
- **404 输出结构化错误体 vs gin 裸 404**：ghttp 的 404 从 ~26ns 涨到 ~146ns（+468%），0→3 allocs，根因是统一错误链输出 JSON 错误体。横向对比仍快于 echo（594ns/8alloc）；不想要错误体可用 `WithNotFoundHandler` 一行恢复裸 404。
- **body 解码策略**：ghttp 用流式 `json.NewDecoder` 直接读 `req.Body`，内存不随请求体大小线性膨胀；echo 池化 `readAll` 在超大 body 场景内存峰值更高，但小 body 下分配更少。权衡详见 [`benchmarks/RESULTS.md`](../benchmarks/RESULTS.md)。

### 默认严格

- Content-Type 严格校验默认开启（不符 415）。旧宽松行为需 `WithStrictContentType(false)` 显式关闭。
- 转发头默认不信任（防伪造）。反向代理场景需 `WithTrustedProxies` 显式声明可信网段。
- 错误响应默认脱敏。调试场景需 `WithExposeErrorDetails(true)` 显式开启。

## 迁移指南

### Engine → Server（d098da 破坏性变更）

`Engine` / `Mux` 与 `Server` 已合并为唯一顶层类型 `*Server`。`New()` 返回 `*Server`，路由注册、中间件挂载、启动与优雅关闭都在同一个对象上完成。

| 旧 API | 新 API |
|--------|--------|
| `New()` + `NewServer(e, opts...)` | `ghttp.New(opts...)` |
| `Engine.Use` / `Mux.Use` | `Server.Use` |
| `Server.ListenAndServe()` | `Server.Run(addr)` |
| `Server.ListenAndServeTLS(...)` | `Server.RunTLS(addr, certFile, keyFile)` |
| `Static(*Mux, ...)` / `Health(*Mux, ...)` | `Static(*Server, ...)` / `Health(*Server, ...)` |
| `ErrEngineNotStarted` | `ErrServerStarted`（重复启动）/ `ErrServerNotStartable`（关闭后启动） |

`Option` 由 `func(*Engine)` 改为 `func(*Server)`，原 `ServerOption` 的全部选项（`WithAddr` / `WithReadTimeout` / ...）并入 `Option`，统一由 `New(opts...)` 接收。

### StructInput → typed entries

`StructInput[T]` / `Body[T]` / `ParseInput` 等老 API 已全族删除。迁移方式：

```go
// 旧
type In struct { ID int64 `path:"id"` }
si := ghttp.StructInput[In]()
ghttp.Handle(s, "GET", "/users/:id", si, func(ctx context.Context, in In) (Out, error) { ... })

// 新
ghttp.GetParams[In, Out](s, "/users/:id", ghttp.JSON[Out](),
    func(ctx context.Context, in In) (Out, error) { ... })
```

入口按输入形态选择 `GetNone` / `GetParams` / `PostBody` / `PostParamsBody` 等，handler 签名即契约，类型全推断。

---

> 本包遵循 [gk 模块依赖分层原则](../docs/superpowers/specs/2026-08-07-gk-module-dependency-policy.md)：作为能力层，只依赖基础契约层（`gerr` / `gretry` / `grx` 等），不引入其他能力层；日志、追踪等可观测性通过接口注入，用户按需拼接。
