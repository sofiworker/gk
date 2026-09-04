# ghttp

> **⚠️ 开发中 · 禁止直接用于生产开发**
>
> 本包属于 [gk](../README.md) 仓库的一部分，整体仍处于 pre-v1.0.0 阶段。API 可能随时破坏性变更，行为与文档尚未冻结，也未经过生产环境验证。**禁止直接用于生产开发**。

[English](README.en.md) | 中文

`ghttp` 是基于标准库 `net/http` 的 **typed HTTP 路由框架**：handler 收裸参数、类型全推断、无包裹容器；同时内置生产所需的统一错误链、优雅退出、健康检查、指标、CORS、Recovery、Gzip、限流、CSRF、安全头、OpenAPI 3.1 生成等组件，开箱即可部署真实服务。

与 gin/echo 的核心差异：**typed 优先**。入口按输入形态分函数（`GetParams` / `PostBody` / `PostParamsBody` …），handler 签名即契约，注册期完成反射与绑定计划编译，请求期零反射、零额外分配。因为契约就在类型里，**OpenAPI 文档可以直接从同一份绑定计划生成**，不需要另写注解或 YAML。

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

    // GET /hello/{name}?lang=zh
    _ = ghttp.GetParams[EchoIn, EchoOut](s, "/hello/{name}",
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

入口命名恒为 `<Method><InputShape>`。七种 method 各有 `None` 与 `Params` 形态；可带请求体的四种（POST / PUT / PATCH / DELETE）另有 `Body` 与 `ParamsBody`。GET / HEAD / OPTIONS 不提供 `Body` 入口——按 RFC 9110 它们的请求体没有定义语义，提供入口只会诱导错误用法。

| 输入形态 | handler 签名 | 可用 method |
|----------|--------------|-------------|
| 无 params 无 body | `func(ctx) (O, error)` | `GetNone` `PostNone` `PutNone` `PatchNone` `DeleteNone` `HeadNone` `OptionsNone` |
| 仅 params | `func(ctx, P) (O, error)` | `GetParams` `PostParams` `PutParams` `PatchParams` `DeleteParams` `HeadParams` `OptionsParams` |
| 仅 body | `func(ctx, B) (O, error)` | `PostBody` `PutBody` `PatchBody` `DeleteBody` |
| params + body | `func(ctx, P, B) (O, error)` | `PostParamsBody` `PutParamsBody` `PatchParamsBody` `DeleteParamsBody` |

`DeleteBody` / `DeleteParamsBody` 存在是因为批量删除（请求体里带 id 列表）是真实需求；`HeadNone` / `Options*` 便于手写条件请求探测与 CORS 预检端点。

所有类型参数均由编译器推断，调用方不手写类型参数列表。需要完全接管响应时用 `RawHandle(method, path, RawHandlerFunc)`——它与 typed 端点共享同一执行路径与中间件链，是一等公民逃生入口。

### Input struct tag

params 必须是结构体，字段用 tag 标注来源：

```go
type GetUser struct {
    ID    int64  `path:"id"`              // 路径参数，自动按字段类型解析
    Page  int     `query:"page"`          // query 参数，缺失时零值
    Trace string  `header:"X-Trace-Id"`   // 请求头
    Token string  `query:"token"`         // query 参数
}
```

- 支持 `path:` / `query:` / `header:` 三类**传输层**来源；未打 tag 的字段静默跳过，tag 值 `"-"` 显式跳过。
- 本框架**不内置校验**：required/范围/枚举等业务规则由 handler 自行判断（返回带 `StatusCoder` 的 error 即可映射任意状态码）。
- 表单文本字段（`form:` tag）与上传文件（`Upload` / `[]Upload`）都属于**请求体**，用 `FormBody[B]()` 解码，见下节；params 结构体不再承载 `form:`/文件字段。纯 path/query/header 端点不解析请求体，零解析开销。

#### 支持的字段形态

path/query/header 与请求体表单共用同一套绑定引擎，**能力完全对等**——下面每种形态在 `form:` tag 上同样可用。

```go
type Search struct {
    Pagination                          // 无 tag 的内嵌结构体：字段提升到外层，公共参数可复用
    Filters                             // 具名嵌套同样递归展开（无需 tag）
    Opt      *Options                   // 嵌套指针：整块缺省时保持 nil

    ID       int64                       `path:"id"`       // 标量：int*/uint*/float*/bool/string
    Limit    *int                        `query:"limit"`   // 指针：nil = 未提供，可与显式 0 区分
    Tags     []string                    `query:"tags"`    // 列表：?tags=a&tags=b 或 ?tags=a,b（可混用）
    Top      [3]int                      `query:"top"`     // 定长数组：多余丢弃，不足留零值
    Filter   map[string]string           `query:"filter"`  // 映射：?filter[status]=active
    Multi    map[string][]string         `query:"multi"`   // 映射到列表：?multi[k]=x&multi[k]=y
    Since    time.Time                   `query:"since"`   // 实现 TextUnmarshaler 的类型
    Addr     net.IP                      `query:"addr"`    // 同上（优先于底层 Kind）
    Raw      []byte                      `query:"raw"`     // 取原始字节，不做 base64 解码
    Meta     map[string]string           `header:"X-Meta"` // 头前缀族：X-Meta-Region → key "region"
    AllHead  map[string]string           `header:"*"`      // 收全部头，键统一小写
    Skipped  string                      `query:"-"`       // 显式跳过
}
```

- **缺省语义**：标量保留零值，指针/切片/映射保持 `nil`，因此 handler 能区分"未提供"与"提供了零值/空列表"。
- **元素解析失败整体报 400**（`ErrInvalidInput`），不静默丢弃坏元素——静默丢弃会让调用方误以为参数已生效。
- **不支持的形态在注册期报错**：切片的切片、映射的映射、非字符串键的映射等在传输层没有公认编码，注册期拒绝比运行期猜测更诚实。`path:` 不能绑定映射（path 段是单值语义）。
- 递归展开有深度上限（8 层），自引用结构体在注册期即报错而非栈溢出。
- **query 惰性取值**：绑定计划在注册期就知道要读哪些键，因此请求期**不构建 `url.Values` 映射**，直接在原始 query 串上按键扫描——值不含 `%`/`+` 时直接引用原串子串，**零分配**。语义与 `net/url.ParseQuery` 完全一致（含分号作废、非法转义跳过等边界，由表驱动对照与模糊测试校验）。两种情况自动回退到建映射：映射形态字段（`query:"filter"` / `query:"*"` 必须枚举全部键），以及 query 键数超过 24（线性扫描在此之后会输给建映射）。纯 path 端点则完全不碰 query。

### 入口形态：自由函数与链式并存

类型化端点有两套入口，注册期共用同一套实现（绑定计划、严格 Content-Type 校验、OpenAPI 登记完全一致），可自由混用：

#### 自由函数入口（所有支持的 Go 版本）

把 router 作为首参传入，类型参数由 handler 推断：

```go
ghttp.GetParams(s, "/items/{id}", ghttp.JSON[ItemResp](),
    func(ctx context.Context, p ItemID) (ItemResp, error) { /* … */ })

ghttp.PostBody(s, "/items", ghttp.JSONBody[CreateReq](), ghttp.JSON[ItemResp]().Status(201),
    func(ctx context.Context, in CreateReq) (ItemResp, error) { /* … */ })
```

这是 Go < 1.27 下的**唯一**入口；Go ≥ 1.27 下同样继续可用。

#### 链式入口（**仅 Go ≥ 1.27**）

Go 1.27 允许方法声明类型参数（泛型方法），因此可以做到**链本身非泛型、类型只在终结方法处由 handler 推断**——这在 Go 1.27 之前无法表达（类型参数只能声明在链的起点，会迫使调用方手写 `Req`/`Resp`）：

```go
// params（path/query/header 经 struct tag 绑定）
s.Get("/items/{id}").To(ghttp.JSON[ItemResp](),
    func(ctx context.Context, p ItemID) (ItemResp, error) { /* … */ })

// 仅 body
s.Post("/items").ToBody(ghttp.JSONBody[CreateReq](), ghttp.JSON[ItemResp]().Status(201),
    func(ctx context.Context, in CreateReq) (ItemResp, error) { /* … */ })

// params + body
s.Patch("/items/{id}").ToParamsBody(ghttp.JSONBody[UpdateReq](), ghttp.JSON[ItemResp](),
    func(ctx context.Context, p ItemID, b UpdateReq) (ItemResp, error) { /* … */ })

// 无 params 无 body
s.Get("/healthz").ToNone(ghttp.JSON[HealthResp](),
    func(ctx context.Context) (HealthResp, error) { /* … */ })

// 路由级中间件 + 完全接管响应
s.Get("/stream").Use(mw).ToRaw(func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error { /* … */ })
```

- 动词：`Get`/`Post`/`Put`/`Patch`/`Delete`/`Head`/`Options`，以及自定义方法 `Method(verb, path)`。
- 终结方法：`To`（params）、`ToBody`（仅 body）、`ToParamsBody`（params+body）、`ToNone`（无输入）、`ToRaw`（原始处理器）。均返回 `error`，错误语义与自由函数入口一致。
- 分组同样可用：`g := s.Group("/api/v1"); g.Get("/items/{id}").To(…)`，路径相对分组前缀。
- 链上 `Use(...)` 挂路由级中间件，折叠顺序为**全局 → 分组 → 路由 → 终端**（与 gin 一致）。
- 链起点定义在内部 `mux` 上，经嵌入提升为 `Server` 的方法；`Group` 单独定义一份。

> **版本门槛**：链式 API 位于 `//go:build go1.27` 文件中，Go < 1.27 的工具链会在构建约束层直接排除它，此时调用 `s.Get(...)` 会报 `has no field or method Get`——请改用上面的自由函数入口。`go.mod` 无需声明 `go 1.27`（构建标签会按需提升该文件的语言版本）。
>
> 注意：含泛型方法语法的文件**必须用 Go 1.27+ 的 gofmt 格式化**，旧版 gofmt 与 golangci-lint v1 的解析器无法解析它（CI 已分别固定工具链版本）。

### 请求体：InputSpec[B] 解码

请求体经类型化的输入契约 `InputSpec[B]` 解码，与输出侧的 `OutputSpec[O]` 对称。底层是 `Body[B](codec)`，常用格式各有快捷糖：

| 构造器 | 解码为 | Content-Type |
|--------|--------|--------------|
| `JSONBody[B]()` | JSON → B | `application/json` |
| `XMLBody[B]()` | XML → B | `application/xml` |
| `FormBody[B]()` | urlencoded / multipart（含文件）→ B | 两者皆可 |
| `TextBody[B]()` | 纯文本 → B（B 为 `string` 或 `[]byte`） | `text/plain` |
| `Body[B](codec)` | 自定义 `RequestDecoder` → B | 由 codec 决定 |

```go
type CreatePost struct {
    Title string `json:"title"`
    Body  string `json:"body"`
}

// POST /users/{id}/posts —— path 参数(params) + JSON 请求体(body)
ghttp.PostParamsBody(server, "/users/{id}/posts",
    ghttp.JSONBody[CreatePost](),   // 请求体：JSON 解码为 CreatePost
    ghttp.JSON[PostResp](),         // 输出契约
    func(ctx context.Context, p PathID, b CreatePost) (PostResp, error) {
        return PostResp{ID: 1, UserID: p.ID, Title: b.Title}, nil
    })
```

### 文件上传（multipart）

文件属于**请求体**：在 form 请求体结构体里放 `Upload`（单文件）或 `[]Upload`（多文件）字段，用 `form:` tag 指定表单字段名，交给 `FormBody[B]()` 解码——文本字段与文件在同一个结构体里一并解出：

```go
type UploadReq struct {
    Note   string         `form:"note"`     // 表单文本字段
    Avatar ghttp.Upload   `form:"avatar"`   // 单文件
    Docs   []ghttp.Upload `form:"docs"`     // 多文件（<input multiple>）
}

ghttp.PostBody(server, "/upload", ghttp.FormBody[UploadReq](), ghttp.JSON[Resp](),
    func(ctx context.Context, in UploadReq) (Resp, error) {
        // 便捷落盘（流式，不整体载入内存）；path 由你决定，务必净化 Filename 防目录穿越
        if in.Avatar.Open != nil { // 可选字段：Open==nil 表示未上传
            if err := in.Avatar.Save("/data/" + sanitize(in.Avatar.Filename)); err != nil {
                return Resp{}, err
            }
        }
        for _, d := range in.Docs {
            data, _ := d.Bytes()          // 或读入内存
            _ = data
        }
        return Resp{}, nil
    })
```

- **单/多文件**：`Upload` 绑定同名首个文件；`[]Upload` 绑定同名全部文件（对应 `<input multiple>`）。
- **可选**：缺文件时 `Upload` 保留零值（`Open == nil` 可判空）、`[]Upload` 为 nil。
- **便捷方法**：`Upload.Save(path)` 流式落盘、`Upload.Bytes()` 读入内存、`Upload.Open()` 拿 `multipart.File` 自行流式处理；`Upload.Filename`/`Size`/`ContentType`/`Header` 提供元数据。
- **安全**：`Filename` 是客户端声明的不可信值，`Save` 不据它拼路径，落盘路径与净化由调用方负责。
- 需要 path/query/header + 表单/文件混用时,用 `PostParamsBody`：params 拿传输层参数，`FormBody[B]()` 拿表单体（含文件）。

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

typed handler / codec 返回的 `error` 经统一出口分类为 HTTP 状态码：

1. **StatusCoder 优先**：业务错误实现 `HTTPStatus() int` 即自带状态码（可精确控制 404/409/422 等）。
2. **框架哨兵映射**：`ErrInvalidInput` → 400、`ErrUnsupportedMediaType` → 415、`ErrRequestEntityTooLarge` → 413、`ErrHandlerPanic` → 500 等。
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
    _ = ghttp.GetParams[...](users, "/{id}", ...)   // 路径：/api/v1/users/{id}
    _ = ghttp.PostBody[...](users, "", ...)        // 路径：/api/v1/users
}
```

快照发生在创建时（gin 语义）：此后对父组的 `Use` 不影响本组。

### 零开销直连

无全局中间件时走 `dispatchRaw` 零开销路径：直接借池化上下文匹配并执行，不构造链。有全局中间件时，首个请求把链折叠一次并缓存（`sync.Once`），此后每请求零组装、零额外分配。

## 功能清单与开关

### Content-Type 严格校验

body 入口默认在解码前校验请求 `Content-Type` 与端点 `InputSpec[B]` 声明的 Content-Type 一致，不符即 **415**（`FormBody` 同时接受 urlencoded 与 multipart，故不参与该校验）。

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
| `Timeout(d)` | 协作式请求超时：注入 deadline context，下游超时未提交响应时返回 `ErrRequestTimeout`，经统一错误链输出 504 JSON。 |
| `BasicAuth(realm, accounts)` | HTTP Basic 认证（恒定时间比较）；`BasicAuthUser(ctx)` 读取认证用户。 |
| `Gzip(opts...)` | 按 Content-Type 白名单条件压缩，`gzip.Writer` 池化，未挂载零影响。 |
| `LimitBody(maxBytes)` | 请求体大小限制，超限 413。 |
| `RateLimit(cfg)` / `RateLimitByRoute(rps, burst)` | 分片令牌桶限流，默认按 `ClientIP` 分键；超限 429 + `Retry-After`。 |
| `CSRF(cfg)` | 双提交 cookie + 同源校验，恒定时间比较；`CSRFTokenFromContext(ctx)` 取 token 供模板渲染。 |
| `SecureHeaders(cfg)` / `SecureHeadersDefault()` | 安全响应头（nosniff / `X-Frame-Options: DENY` / `Referrer-Policy: strict-origin-when-cross-origin` 等）；HSTS 与 CSP 需显式开启。 |
| `(*MetricsRegistry).Middleware()` | 请求指标收集（计数/延迟/响应大小/在途），按 `MatchedRoute` 低基数聚合。 |

#### 限流（`RateLimit`）

```go
s.Use(ghttp.RateLimit(ghttp.RateLimitConfig{
    RPS:   100,                              // 每秒补充的令牌数
    Burst: 200,                              // 桶容量（允许的突发量），默认 = RPS
}))

s.Use(ghttp.RateLimitByRoute(50, 100))       // 按 method + 路由模板分别限流
```

- **令牌桶**而非固定窗口：允许短时突发（`Burst`），长期速率收敛到 `RPS`，比固定窗口的边界翻倍问题更可控。
- 默认按 `ClientIP()` 分键（遵循可信代理配置，不会被伪造头绕过）；`KeyFunc` 可改为按用户 ID、API key 等分键，**返回空串表示豁免**（如内部健康检查）。
- 桶按 key 分 16 片存放，锁竞争限制在片内；空闲桶超过 `IdleTimeout`（默认 10 分钟）自动回收。回收只发生在被访问的分片上，故另有 `MaxKeys`（默认 100000）硬性封顶跟踪的 key 数：达到上限后新 key 不建桶而**直接放行**——限流是可用性保护而非访问控制，"满了就全拒"会让一次 key 冲刷制造全站拒绝服务。
- 超限返回 429 与 `Retry-After`（秒），错误为 `ErrRateLimitExceeded`，可用 `errors.Is` 判断；`OnLimited` 可自定义响应。
- `RPS <= 0` 时直接返回**透传中间件**，不做任何记账——便于按环境开关而无需改代码结构。

#### CSRF

```go
s.Use(ghttp.CSRF(ghttp.CSRFConfig{
    TrustedOrigins: []string{"https://app.example.com"},
}))
```

- **双提交 cookie**：写一个非 HttpOnly 的 `csrf_token` cookie，要求非安全方法（POST/PUT/PATCH/DELETE）在 `X-CSRF-Token` 头或表单字段里回传同值，恒定时间比较。cookie 故意**不设** HttpOnly——前端 JS 必须读到它才能回传。
- 同时校验 `Origin`/`Referer` 是否属于可信来源，双重防线。
- GET/HEAD/OPTIONS 视为安全方法，只补发 token 不校验；`CSRFTokenFromContext(ctx)` 供服务端模板把 token 渲染进表单。
- 失败返回 403，错误为 `ErrCSRFTokenInvalid`。

#### 安全响应头（`SecureHeaders`）

```go
s.Use(ghttp.SecureHeadersDefault())          // 安全默认

s.Use(ghttp.SecureHeaders(ghttp.SecureHeadersConfig{
    HSTSMaxAge:            31536000,          // 显式开启 HSTS（默认关闭）
    ContentSecurityPolicy: "default-src 'self'",
    FrameOptions:          "-",               // "-" 表示不写这个头
}))
```

- 每个字段三态：**空串 = 用安全默认**，`"-"` = 明确不写该头，其他值 = 用该值。
- **HSTS 默认关闭**（`HSTSMaxAge` 为 0）：它一旦被浏览器记住就难以撤回，误配在开发环境会锁死 localhost，因此必须显式开启；且默认只在 TLS 请求上写（`HSTSOnlyWhenTLS`）。
- **CSP 默认不写**：任何通用默认值都会破坏真实页面，必须由使用方按站点内容制定。
- 头部列表在注册期就冻结成切片，请求期只做写入；且**不覆盖**已存在的同名头，便于单条路由自行覆写。


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

#### 预压缩（`WithPrecompressed`）

```go
_ = ghttp.Static(s, "/assets/", "./dist",
    ghttp.WithPrecompressed(),                                    // 优先 .br，回退 .gz
)
_ = ghttp.Static(s, "/assets/", "./dist",
    ghttp.WithPrecompressedEncodings("gzip"),                     // 只用 .gz
)
```

开启后，请求 `app.js` 时若客户端 `Accept-Encoding` 接受且磁盘上存在 `app.js.br` / `app.js.gz`，则直接服务该变体：

- **压缩成本移到构建期**：不像 Gzip 中间件那样每请求压缩一遍，静态资源的 CPU 开销归零，且可以用更高压缩级别（构建期慢一次换取长期带宽收益）。
- `Content-Type` 始终按**原始扩展名**判定（`app.js.br` 仍是 `application/javascript`），并写 `Content-Encoding` 与 `Vary: Accept-Encoding`；即使本次未命中变体也会写 `Vary`，避免缓存把压缩版投给不支持的客户端。
- 与 Gzip 中间件安全共存：后者见到已有 `Content-Encoding` 会跳过，不会二次压缩。
- 变体不存在时静默回退原文件，无需为每个资源都准备压缩版。

### OpenAPI 3.1 生成

```go
s := ghttp.New(
    ghttp.WithOpenAPI(ghttp.OpenAPIInfo{
        Title:   "Orders API",
        Version: "1.2.0",
    },
        ghttp.WithOpenAPIRoute("/openapi.json"),                  // 默认值，置 "" 则只在内存构建
        ghttp.WithOpenAPIServers(
            ghttp.OpenAPIServer{URL: "https://api.example.com", Description: "prod"},
        ),
        ghttp.WithOpenAPIErrorResponses(true),                    // 默认 true，附带 400/500 错误响应
    ),
)

// 之后注册的 typed 路由自动进入 spec
g := s.Group("/api/v1/orders")
_ = ghttp.GetParams(g, "/{id}", ghttp.JSON[Order](), getOrder)
_ = ghttp.PostBody(g, "", ghttp.JSONBody[CreateOrder](), ghttp.JSON[Order]().Status(201), createOrder)

raw := s.SpecJSON()   // 也可直接取字节，写进构建产物或契约测试
```

**契约从代码里长出来，而不是另写一份。** 注册期从每个 typed 入口的 params / body / output 类型收集元数据，首次请求时构建一次 spec 并缓存字节。

- **文档不会漂移**：参数说明复用请求期**同一份 `BindPlan`**——不是照着绑定规则再实现一遍。绑定支持的形态（列表、映射、指针可选、内嵌提升、`TextUnmarshaler`）在 spec 里就是对应的 `array` / `object+additionalProperties` / 非 required / 提升后的平铺参数 / `string`。改了 tag，文档跟着变；这一点由 `TestOpenAPI_ParametersMatchBindPlan` 守住。
- **输出确定性**：spec 是手工序列化的，字段顺序固定。若用 `map[string]any` + `json.Marshal`，Go 的 map 遍历顺序随机会让同一份路由表每次产出不同字节，spec 就无法做 diff review 或契约快照测试。
- **响应体形状与实际一致**：`NoContent` 输出声明 204 且**不带** `content`；错误响应引用统一的 `Error` schema，与错误链真正写出的 `{"error":{"code","message"}}` 同形。
- **schema 生成**：具名结构体提取为 `components/schemas` 并以 `$ref` 引用（自引用类型因此能终止，不会栈溢出）；遵循 `encoding/json` 语义——`json:"-"` 跳过、`omitempty` 不进 required、内嵌结构体字段提升、不导出字段不出现；`time.Time` → `date-time`、`[]byte` → `byte`、指针 → OpenAPI 3.1 的 `["T","null"]`。
- **tag 自动派生**：`/api/v1/orders/...` 归到 `orders` 标签（跳过 `api` 与版本段这类无信息量的前缀）。
- **`RawHandle` 端点也会登记**：它们没有可反射的类型，但"路径与方法存在"本身就是契约的一部分——漏掉会让 spec 谎报端点不存在，反而误导契约测试与客户端生成。策略是**如实呈现而非编造**：登记路径与方法、从路径模板推出必填的 `string` 型 path 参数（否则 spec 非法），响应只声明"形状未由框架声明"而不生成 schema，也不追加 typed 绑定才会产生的 400/415。静态资源（`Static`/`StaticFS`）与健康检查（`Health`/`Ready`）建在 `RawHandle` 上，因此同样出现在 spec 里。
- **零成本**：未调用 `WithOpenAPI` 时不收集元数据、不构建、不注册路由；spec 端点自身也不出现在 spec 里。
- 注意 `WithOpenAPI` 是 `New` 的 Option，必须**先于**路由注册生效——在它之前注册的路由收集不到。

### 可观测性

- `Request.MatchedRoute()` 返回低基数路由模板（如 `/users/:id`），适合作为 metrics/tracing/日志的路由维度。**全局中间件运行时即可读到**（含进入 `next` 之前）：路由匹配在中间件链之前完成，因此按路由聚合的中间件（指标、按路由限流、tracing span 命名）都能拿到模板；未命中时恒为空串，不会串上一个请求的值。
- `Request.ClientIP()` / `RemoteIP()` 按可信代理模型解析真实客户端 IP；默认**不信任**转发头以防伪造，经 `WithTrustedProxies` / `WithForwardedHeaders` 配置。
- `NewMetrics()` + `MetricsRegistry.Handler()` 暴露 Prometheus 文本格式 `/metrics` 端点，零第三方依赖。


### 参数校验

- 本框架**不内置校验**。params 只做类型绑定（解析失败/越界报 400 `ErrInvalidInput`），请求体只做解码。
- required、范围、枚举、格式等业务规则由 handler 自行判断；返回的 error 实现 `StatusCoder` 即可精确映射状态码（如 422），否则兜底 500。校验体系将另行设计。

### 实时能力：SSE 与 WebSocket

`Response` 实现了 `http.Flusher` 与 `http.Hijacker`（`Flush` / `Hijack` 透传底层连接），实时能力都建在 `RawHandle` 之上。

**Server-Sent Events**：`NewSSEWriter(resp)` 自动写好 `text/event-stream` 等响应头，之后每次 `Send` / `SendEvent` / `SendMessage` / `Comment` / `Ping` 都按 SSE 线格式编码并**立即 Flush**；写出错误（客户端断开常见）被记住，可据返回值或 `Err()` 退出循环。SSE 是纯 HTTP 长连接，不接管连接，仍受中间件与优雅关闭管理。

```go
m.RawHandle(http.MethodGet, "/sse/time", func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
    w := ghttp.NewSSEWriter(resp)
    for {
        select {
        case <-ctx.Done():
            return nil // 客户端断开 / 服务器关闭
        case t := <-ticker.C:
            if err := w.SendEvent("time", t.Format(time.RFC3339)); err != nil {
                return nil // 写失败=客户端已走
            }
        }
    }
})
```

**WebSocket**：经 `github.com/gorilla/websocket` 升级——ghttp 只负责让 `Response` 可 `Hijack`，协议实现委托 gorilla。`ServeWS` 一行注册升级端点（升级成功后把 `*websocket.Conn` 交给业务处理器，返回后自动 `Close`）；需要自定义时用 `NewWSUpgrader(WithWS...)` 配 `Upgrade` 在 `RawHandle` 内手动升级。

```go
ghttp.ServeWS(m, "/ws/echo", nil, func(ctx context.Context, req *ghttp.Request, conn *websocket.Conn) error {
    for {
        mt, msg, err := conn.ReadMessage()
        if err != nil {
            return err // 客户端关闭 → 正常退出
        }
        if err := conn.WriteMessage(mt, msg); err != nil {
            return err
        }
    }
})
```

- `WSUpgrader` 选项：`WithWSCheckOrigin`（跨源校验，默认 gorilla 安全同源策略）、`WithWSReadBufferSize` / `WithWSWriteBufferSize`、`WithWSSubprotocols`、`WithWSHandshakeTimeout`、`WithWSCompression`。
- 底层 `ResponseWriter` 不支持 `Hijack`（被不透传的中间件包裹等）时返回 `ErrNotHijackable`。
- 完整示例见 [`examples/realtime`](../examples/realtime)。

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

### 安全与正确性修复带来的行为变更（破坏性变更）

按 `REVIEW.md` 逐条核实后的一批修复改变了若干可观测行为。下表只列**需要调用方或运维配合调整**的项；完整清单与每项的根因分析见 `CHANGELOG.md` 的 `[Unreleased] / Fixed`。

| 变更 | 旧行为 | 新行为 | 需要做什么 |
| --- | --- | --- | --- |
| Prometheus 按状态码计数 | `http_requests_total{method,route,code}` | 独立 family `http_requests_by_code_total{method,route,code}`；`http_requests_total` 不再带 `code` | **改仪表盘与告警的指标名**。原样式让同一 family 的样本标签键集合不一致，属非法暴露格式，抓取端可能整份丢弃 |
| `Timeout` 超时响应 | 503 `text/plain` | 504 `application/json`（`{"error":{"code":"request_timeout",...}}`） | 改断言状态码/响应体的客户端与探针；可用新哨兵 `ErrRequestTimeout` 做 `errors.Is` 分支 |
| `Referrer-Policy` 默认值 | `no-referrer-when-downgrade` | `strict-origin-when-cross-origin` | 依赖同等级跳转能收到完整 Referer（含路径与查询串）的下游需显式配置回旧值 |
| CORS 通配源 + 凭据 | 回显具体 `Origin` 并带 `Allow-Credentials: true` | 通配时**强制关闭**凭据 | 要带凭据必须把源显式列进 `AllowOrigins` |
| CSRF 同源校验 | `Origin: null` 放行；只比 `Host` | 拒绝 `null`；比较**带 scheme** | 沙箱 iframe / `data:` 文档 / 跨源重定向后的请求会被拒；混用 http 与 https 的部署需统一 scheme |
| `RequestID` 客户端值 | 只查长度 | 另按字符集 `[0-9A-Za-z._-]` 校验，非法值替换 | 使用 UUID 以外字符集（如带 `:` 或空格）的上游需改 ID 格式 |
| `LimitBody` 超限 | 413 空体 / 400 | 413 + 统一 JSON 错误体 | 改断言响应体的客户端 |
| 分组前缀归一 | `Group("/api/")` + `"/v1/x"` → `/api//v1/x` | → `/api/v1/x` | 检查是否有调用方依赖了带双斜杠的 URL |
| 自定义 404/405 handler 返回 error | 静默 200 空体 | 走统一错误链输出 500 | 自定义 miss handler 应显式处理自身错误 |
| form 端点收到非表单 Content-Type | 静默 200 + 零值结构体 | 415 `ErrUnsupportedMediaType` | 校正客户端的 `Content-Type`；或 `WithStrictContentType(false)` 保留旧宽松行为 |
| JSON 请求体尾部多余内容 | 静默接受首个值 | 400 `ErrInvalidInput` | 修正发送方（`{"a":1} 垃圾`、`{"a":1}{"a":2}` 均不再被接受） |
| `WithErrorHook` 的 `status` | 恒为错误分类值 | 响应**已提交**时报客户端实际收到的状态码 | 依赖分类值的钩子改用 `HTTPStatus(err)` 取回 |
| 尾斜杠重定向目标 | 直接写入 `Location` | 拒绝 `//`、`/\` 开头及含控制字符的目标（按未命中处理返回 404） | 无需调整；这是开放重定向修复 |

新增导出 API：`PanicValueOf(err) (any, bool)`（从 panic 错误取回原值）、`ErrRequestTimeout`、`RateLimitConfig.MaxKeys`、`CSRFConfig.UseHostPrefixedCookie`、`CSRFHostCookiePrefix`、`MultiContentTypeDecoder`。

### `File` 新增变参选项（破坏性变更）

`File` 的签名新增变参，以便单文件路由也能使用静态选项（如预压缩）：

```go
// 旧
func File(m *Server, urlPath, name string, fsys fs.FS) error
// 新
func File(m *Server, urlPath, name string, fsys fs.FS, opts ...StaticOption) error
```

**既有调用无需修改**（变参可省略）。仅当以函数值形式引用 `File`（如赋给变量或作为参数传递）时需调整类型声明。

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
ghttp.Handle(s, "GET", "/users/{id}", si, func(ctx context.Context, in In) (Out, error) { ... })

// 新
ghttp.GetParams[In, Out](s, "/users/{id}", ghttp.JSON[Out](),
    func(ctx context.Context, in In) (Out, error) { ... })
```

入口按输入形态选择 `GetNone` / `GetParams` / `PostBody` / `PostParamsBody` 等，handler 签名即契约，类型全推断。

---

> 本包遵循 [gk 模块依赖分层原则](../docs/superpowers/specs/2026-08-07-gk-module-dependency-policy.md)：作为能力层，只依赖基础契约层（`gerr` / `gretry` / `grx` 等），不引入其他能力层；日志、追踪等可观测性通过接口注入，用户按需拼接。
