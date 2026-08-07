# ghttp

通用 Go HTTP 框架 —— 基于 Go 1.24+ 泛型和标准库 `net/http`，整合服务端与客户端，内建 OpenAPI 3.1 文档生成、内容协商、模板渲染、WebSocket/SSE 等能力。

---

## 特性

- **泛型优先 API** — `Route[Req,Resp]` 链式构建器（Go 1.27 起自动切换到 `Server.Route().To[Req,Resp]` 泛型方法形态），编译期类型安全
- **显式输入读取** — `Params` 读取 path/query/header/cookie，`Body` 字段解析请求体
- **内容协商** — `Accept` 驱动响应 Codec，`Consumes` 约束请求 `Content-Type`
- **灵活输出** — 支持响应体结构体和自定义 Envelope 包装（code/msg/data 模式）
- **内建路由匹配** — 唯一的 method-first matcher，支持静态、参数与 catch-all 路径
- **OpenAPI 3.1** — 从路由元数据自动生成 JSON Schema 和 OAS 文档
- **WebSocket / SSE** — 服务端推送和双向通信
- **模板渲染** — 集成 Go 模板引擎，路由级 `ToHTML()`
- **静态文件服务** — 路由级 `ToStatic*()`
- **中间件系统** — 内置 RequestID、CORS、日志、Recover、超时控制
- **HTTP 客户端** — go-resty 风格链式调用 + 泛型端点方法
- **纯标准库** — 基于 `net/http`，无 fasthttp 依赖，无第三方路由依赖

---

## 安装

```bash
go get github.com/sofiworker/gk
```

```go
import "github.com/sofiworker/gk/ghttp"
```

---

## 快速开始

### 服务端

```go
package main

import (
    "context"
    "github.com/sofiworker/gk/ghttp"
)

type GreetInput struct {
    ghttp.Params `json:"-"`
}

type GreetOutput struct {
    Message string `json:"message"`
}

func main() {
    s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))

    // 链式构建器（支持 OpenAPI 元数据）
    ghttp.Route[GreetInput, GreetOutput](s).
        GET("/hello/{name}").
        Produces(ghttp.MIMEJSON, ghttp.MIMEXML).
        Doc(ghttp.Summary("返回个性化的问候消息")).
        To(func(ctx context.Context, req GreetInput) (GreetOutput, error) {
            return GreetOutput{Message: "Hello, " + req.Path("name")}, nil
        })

    s.Run(":8080")
}
```

### 客户端

```go
client := ghttp.NewClient()

// 泛型调用
resp, err := ghttp.GET[GreetInput, GreetOutput](client, "/hello/world", nil)

// 链式调用（go-resty 风格）
result, err := client.R().
    SetHeader("Authorization", "Bearer token").
    SetQueryParam("lang", "zh").
    Get("/hello/world")

// 结构化请求
input := &GreetInput{}
resp, err := ghttp.Do[GreetInput, GreetOutput](client, "POST", "/hello", input)

// 自定义底层客户端或 Transport
client = ghttp.NewClient(
    ghttp.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
    ghttp.WithTransport(customTransport),
)

// 流式响应由调用方关闭 RawBody
streamResp, err := client.R().SetStreamResponse(true).Get("/download")
if err != nil { return err }
defer streamResp.RawBody().Close()
_, err = io.Copy(dst, streamResp.RawBody())
```

---

## API 概览

### 路由注册

| 函数 | 说明 |
|------|------|
| `Route[Req,Resp](target)` | 链式构建器起始，`target` 可以是 `*Server` 或 `*Group`；Go 1.27 起该函数仅作兼容 shim，类型参数被忽略 |
| `.GET(path)` | 注册 GET 路由 |
| `.POST(path)` | 注册 POST 路由 |
| `.PUT(path)` | 注册 PUT 路由 |
| `.DELETE(path)` | 注册 DELETE 路由 |
| `.PATCH(path)` | 注册 PATCH 路由 |
| `.ANY(path)` | 注册所有标准 HTTP 方法，常用于测试或兜底 API |
| `.CUSTOM(method, path)` | 注册自定义 HTTP 方法，`method` 必须是合法 token |

### 链式构建器方法

| 方法 | 说明 |
|------|------|
| `.GET(path)` | 设置 GET 方法和路由路径 |
| `.POST(path)` | 设置 POST 方法和路由路径 |
| `.PUT(path)` | 设置 PUT 方法和路由路径 |
| `.DELETE(path)` | 设置 DELETE 方法和路由路径 |
| `.PATCH(path)` | 设置 PATCH 方法和路由路径 |
| `.ANY(path)` | 设置所有标准 HTTP 方法和路由路径 |
| `.CUSTOM(method, path)` | 设置自定义 HTTP 方法和路由路径 |
| `.Doc(ghttp.Summary("..."), ghttp.Tags("..."))` | 操作描述 |
| `.Consumes(contentTypes...)` | 声明可自动解析的请求 Content-Type，可在 server/group/route 上声明 |
| `.MaxBodyBytes(n)` | 覆盖当前路由自动解析请求体的大小上限；`n <= 0` 表示不限制 |
| `.Produces(contentTypes...)` | 自动响应编码的 Content-Type，可在 server/group/route 上声明 |

### 终结方法

| 方法 | 说明 |
|------|------|
| `.To(handler)` | 注册类型化处理函数，配置错误在终结调用处立即 panic |
| `.ToNoInput(handler)` | 注册无请求输入的类型化处理函数（不解析 body、不校验 Content-Type） |
| `.ToNoOutput(handler)` | 注册只返回 error 的处理函数，成功默认 204，可用 `.Status(code)` 覆盖 |
| `.ToHTTP(handler)` / `.ToRaw(handler)` | 原始 `http.Handler` / `RawHandler` 逃生口 |
| `.ToHTTPFunc(handler)` | 解析输入后由 handler 自己写响应的逃生口 |
| `.ToRedirect(code, location)` / `.ToRedirectFunc(...)` | 重定向 |
| `.ToSSE` / `.ToWebSocket` / `.ToStatic*` / `.ToHTML` | 专用终结器 |

> 无输入/无输出都是显式终结器，不使用 `struct{}` 魔法：`.ToNoOutput` 成功默认 204，错误照常走统一错误管线；`.ToNoInput` 完全跳过请求解析。

### Go 1.27 泛型方法版本

模块内同时保留两套 API，编译时按工具链版本**自动选择**（类似 Go 标准库的 `//go:build` 版本约束），使用者不需要传任何 build tag：

- Go < 1.27：`ghttp.Route[Req, Resp](target).GET(path).To(handler)`，类型参数在包级函数上（Go 1.27 前方法不支持类型参数）；
- Go ≥ 1.27：`server.Route().GET(path).To[Req, Resp](handler)`，类型参数在终结方法上并由 handler 自动推断；同时提供 `server.Get/Post/Put/Patch/Delete/Head/Options(path, handler)` 快捷注册。

```go
// Go 1.27+ 写法
s.Route().GET("/hello/{name}").To(func(ctx context.Context, req *GreetInput) (*GreetOutput, error) {
    return &GreetOutput{Message: "Hello, " + req.Path("name")}, nil
})

// 快捷注册
s.Get("/users/{id}", func(ctx context.Context, req *GetUserReq) (*GetUserResp, error) {
    return &GetUserResp{ID: req.ID}, nil
})

// 分组与 Server 一致
api := s.Group("/api")
api.Route().GET("/users/{id}").To(func(ctx context.Context, req *GetUserReq) (*GetUserResp, error) {
    return &GetUserResp{ID: req.ID}, nil
})
api.Post("/users", func(ctx context.Context, req *CreateUserReq) (*CreateUserResp, error) {
    return &CreateUserResp{ID: "u-1"}, nil
})
```

两套 API 共享同一内部实现（`routeBuilderCore`），行为完全一致。注意：Go 1.27 专用文件包含泛型方法语法，`go fmt`/`gofmt` 需使用 Go 1.27+ 工具链（旧工具链的 gofmt 无法解析该文件）。

### 路由参数

路由路径推荐使用 Go 标准库和 OpenAPI 一致的 `{param}` 语法：

- `{param}` — 命名参数
- `{path...}` — 通配符（匹配剩余路径）

```go
ghttp.Route[Req, Resp](s).GET("/users/{id}").To(handler)      // 命名参数
ghttp.Route[Req, Resp](s).GET("/files/{path...}").To(handler)  // 通配符
```

旧的 :param 和 *path 语法已删除；使用 {param} 和 {path...}。

### 输入结构体

```go
type CreateUserInput struct {
    ghttp.Params `json:"-"`

    Body struct {
        Name   string `json:"name"`
        Age    int    `json:"age"`
        Active bool   `json:"active"`
    } `json:"body"`
}

func createUser(ctx context.Context, req CreateUserInput) (UserOutput, error) {
    id := req.Path("id")
    role := req.DefaultQuery("role", "guest")
    token := req.Header("Authorization")
    sessionID := req.Cookie("session_id")
    _ = role
    _ = token
    _ = sessionID
    return UserOutput{ID: id, Name: req.Body.Name}, nil
}
```

请求体媒体类型使用 `Consumes` 声明，语义对应请求头 `Content-Type`；响应媒体类型继续使用 `Produces`，语义对应响应 `Content-Type` 和客户端 `Accept`：

```go
ghttp.Route[CreateUserInput, UserOutput](s).
    POST("/users/{id}").
    Consumes(ghttp.MIMEJSON, ghttp.MIMEXML).
    Produces(ghttp.MIMEJSON, ghttp.MIMEXML).
    To(createUser)
```

`Consumes` 只约束带 `Body` 的自动解析路由。请求 `Content-Type` 为空时仍按默认 JSON 解析；显式传入不匹配的媒体类型会返回 `415 Unsupported Media Type`。

`application/x-www-form-urlencoded` 表单支持显式 `form:"name"` tag 绑定到 `Body` 结构体字段（标量类型；重复 key 取第一个值）。目标 struct 有可绑定字段但完全没有 `form` tag 时返回 `400 Bad Request`，避免静默空值。

### 默认值速查

| 场景 | 默认行为 | 显式覆盖 |
|------|----------|----------|
| `Accept` 明确列出但无匹配 | `406 Not Acceptable`（huma/go-restful 风格） | `WithLenientContentNegotiation()` 回退第一个 `Produces` |
| `Accept` 为空或 `*/*` | 第一个 `Produces` | — |
| 显式但未注册的请求 `Content-Type` | `415 Unsupported Media Type`（huma/go-restful 风格） | `WithLenientContentType()` 按 JSON 解析 |
| 缺失请求 `Content-Type` | 按 JSON 解析（gin 风格协议便利） | — |
| 已注册 codec 无法解码目标类型 | `400 Bad Request`（codec 报错，不静默吞掉） | — |
| 错误响应体 | `{code,message}` | `WithProblemDetails()` / 路由级 `.ProblemDetails()` 使用 RFC 9457 `application/problem+json`；`WithErrorWriter` / 路由级 `.ErrorWriter` 完全自定义 |

`WithStrictContentNegotiation()` / `WithStrictContentType()` 保留为兼容别名（当前默认已是严格行为，调用无额外效果）。

`Params` 是请求输入的**惰性视图**：query/cookie/客户端 IP 在首次访问时解析并缓存，header 直接透读请求，构造本身几乎零开销。它不持有 `ResponseWriter`，也不负责中断请求或写响应；cookie 写入通过输出对象完成。

视图在 handler 存活期内有效，且应在单个 goroutine 中使用；如需在 handler 返回后留存、或跨 goroutine 传递，先调用 `Detach()` 获得不再引用底层请求的不可变快照：

```go
func handler(ctx context.Context, p ghttp.Params) (Out, error) {
    snapshot := p.Detach() // 深拷贝，安全跨 goroutine / 超生命周期使用
    go audit(snapshot)
    return Out{}, nil
}
```

只有 path/query/header/cookie 参数、没有请求体时，可以直接使用值类型 `ghttp.Params` 作为输入类型：

```go
ghttp.Route[ghttp.Params, UserOutput](s).
    GET("/users/{id}").
    To(func(ctx context.Context, params ghttp.Params) (UserOutput, error) {
        return UserOutput{
            ID: params.Path("id"),
        }, nil
    })
```

需要请求体时，使用匿名值嵌入：

```go
type Input struct {
    ghttp.Params `json:"-"`
    Body struct {
        Name string `json:"name"`
    } `json:"body"`
}
```

`Route[*ghttp.Params, Resp]`、`Params ghttp.Params` 命名字段、`*ghttp.Params` 匿名指针字段、间接嵌入 `Params` 都属于路由配置错误：对应 `To*` 终结调用会立即 panic，panic error 可用 `errors.Is(err, ghttp.ErrInvalidParamsUsage)` 判断。

### 输出

```go
type UserOutput struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}
```

响应对象实现 `Cookies() []*http.Cookie` 时，框架会在写响应前统一设置 `Set-Cookie`：

```go
type LoginOutput struct {
    Token string `json:"token"`
}

func (o LoginOutput) Cookies() []*http.Cookie {
    return []*http.Cookie{
        {
            Name:     "session_id",
            Value:    o.Token,
            Path:     "/",
            HttpOnly: true,
            Secure:   true,
            SameSite: http.SameSiteLaxMode,
        },
        ghttp.DeleteCookie("old_session"),
    }
}
```

默认响应格式：
```json
{
    "code": 0,
    "msg": "success",
    "data": { "id": 1, "name": "Alice", "email": "alice@example.com" }
}
```

错误响应：
```json
{
    "code": 40001,
    "msg": "invalid parameter: name is required"
}
```

可通过 `WithEnvelope(fn)` 自定义包装格式。

`EnvelopeFunc` 签名为 `func(w http.ResponseWriter, r *http.Request, statusCode int, resp interface{}, err error, contentType string, codec Codec)`；`contentType`/`codec` 是路由协商结果，envelope 只允许包装 body，不得改写 HTTP 状态码。错误响应默认只返回 HTTP 状态文本，`WithExposeErrorDetails()` 开启后才返回内部错误信息（显式 `HTTPError` 消息始终返回）。

`WithProblemDetails()` 开启后，错误响应改用 RFC 9457 `application/problem+json`（`type/title/status/detail/instance`），错误场景优先于 envelope；显式 `WithErrorHandler` 优先级最高。

错误模型可按路由选择：`.ProblemDetails()` 只影响该路由；`.ErrorWriter(fn)` 安装路由级 writer。`ErrorWriter` 的契约是“返回 true 表示已处理，返回 false 则落到下一个 writer/框架默认”，因此可用 `ChainErrorWriters(w1, w2, ...)` 组合（例如先记录日志再写响应）：

```go
ghttp.Route[Req, Resp](s).GET("/users/{id}").
    ProblemDetails().
    To(handler)

s := ghttp.New(ghttp.WithErrorWriter(ghttp.ChainErrorWriters(
    logErrorWriter,            // 返回 false，继续
    problemWriter,             // 返回 true，结束
)))
```

类型化响应可显式声明状态码与响应头：响应对象实现 `StatusCode() int` 与/或 `WriteResponseHeaders(http.Header)`，或在 builder 上用 `.Status(code)`/`.ResponseHeader(name, value)` 声明固定值。204/304/1xx 自动不写 body；动态状态（`StatusCode()`）无法静态推断，OpenAPI 以 builder `Status` 为准。

### 中间件

```go
// 注入结构化 logger，glog.Default() 可直接满足 ghttp.Logger。
s := ghttp.New(ghttp.WithLogger(glog.Default()))
// 或使用标准库 slog 适配：s := ghttp.New(ghttp.WithLogger(ghttp.NewSlogLogger(slog.Default())))

s.Use(ghttp.RequestID())
s.Use(ghttp.CORS(ghttp.CORSConfig{
    AllowOrigins: []string{"*"},
}))
s.Use(ghttp.RequestLogger())
s.Use(ghttp.Recoverer())
s.Use(ghttp.Timeout(5 * time.Second))

// 分组路由添加中间件
group := s.Group("/api")
group.Use(authMiddleware)

// 中间件中读取已匹配的路径参数（显式访问器，默认不注入 context）
s.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        id := s.MatchedParams(r).Path("id")
        _ = id
        next.ServeHTTP(w, r)
    })
})

// RBAC 能力：Authorizer 接口 + 默认 RBAC 实现
s.Use(ghttp.RBACMiddleware(authz,
    func(r *http.Request) string { return r.Header.Get("X-User") },
    func(r *http.Request) string { return "users:read" },
    func(r *http.Request) string { return r.URL.Path },
))
```

### WebSocket

```go
ghttp.Route[struct{}, struct{}](s).GET("/ws/{room}").ToWebSocket(func(ctx context.Context, params ghttp.Params, conn *ghttp.WebSocketConn) error {
    var msg map[string]string
    if err := conn.ReadJSON(&msg); err != nil {
        return err
    }
    msg["room"] = params.Path("room")
    return conn.WriteJSON(msg)
})
```

WebSocket 默认执行同源校验：同源或缺 `Origin` 放行，跨源返回 403。可用 `WithWebSocketOriginChecker(fn)` 替换默认策略。

### SSE

```go
ghttp.Route[struct{}, struct{}](s).GET("/events").ToSSE(func(ctx context.Context, params ghttp.Params, w *ghttp.SSEWriter) error {
    for i := 0; i < 10; i++ {
        w.WriteEvent("message", fmt.Sprintf("event %d", i))
        time.Sleep(time.Second)
    }
    return nil
})

// SSEWriter 还提供 WriteEventWithID / WriteComment / Retry；
// data 含换行时会按规范拆成多行 data: 字段。

stream, err := client.SSE("/events", ghttp.SSEConfig{
    Reconnect:     true,
    RetryInterval: time.Second,
    MaxRetries:    3,
    LastEventID:   "optional-last-id",
})
if err != nil { return err }
defer stream.Close()
```

### 静态文件

```go
// 使用服务器级 VFS 根目录：/a.txt 会解析到 /srv/files/a.txt
s := ghttp.New(ghttp.WithVFSPath("/srv/files"))
ghttp.Route[struct{}, struct{}](s).GET("/").ToStatic()

// 也可以为单条静态路由显式指定根目录
ghttp.Route[struct{}, struct{}](s).GET("/static").ToStatic("./public")
ghttp.Route[struct{}, struct{}](s).GET("/assets").ToStaticFS(http.FS(embeddedAssets))
ghttp.Route[struct{}, struct{}](s).GET("/favicon.ico").ToStaticFile("./favicon.ico")
```

`ToStatic` 默认使用安全 VFS，所有请求路径都会作为相对路径解析到静态根目录下。
`../`、URL 编码后的路径穿越、Windows 反斜杠分隔符等越界访问会被拒绝。

### 模板渲染

```go
renderer := ghttp.NewRenderer("./templates/*.html")
s = ghttp.New(ghttp.WithRenderer(renderer))

// 处理函数中
ghttp.Route[NoInput, NoOutput](s).GET("/page").Produces(ghttp.MIMEJSON, ghttp.MIMEXML).To(func(ctx context.Context, req NoInput) (NoOutput, error) {
    return NoOutput{}, nil
})
```

### OpenAPI 文档

使用 WithOpenAPI 启用可选的 best-effort 文档。Server.OpenAPI 从当前 routeDefinition 快照生成独立 JSON 字节，不会冻结 Server 或改变运行时语义。默认 HTTP endpoint 是 /openapi.json；WithOpenAPIPath 空路径只关闭 HTTP 暴露。CONNECT 和 CUSTOM 使用 x-ghttp-methods 扩展。

```go
document, err := s.OpenAPI()
```

`WithOpenAPIServers(urls...)` 与 `WithOpenAPISecurity(requirements...)` 可声明文档级 servers/security；envelope 启用时自动生成 `{code,msg,data}` 包装 schema，类型化路由附带 404/405 错误响应。

### 验证器

```go
import "github.com/sofiworker/gk/ghttp"

type validator struct{}

func (v *validator) Validate(i any) error {
    // 自定义验证逻辑
    return nil
}

s = ghttp.New(ghttp.WithValidator(&validator{}))
```

server 级 validator **默认关闭**：`New()` 不自动安装任何 validator，需要 `WithValidator(v)` 显式启用（破坏性变更）；`ghttp.NewDefaultValidator()` 可恢复内置 struct-tag 校验。路由级 `.Validate(fn)` 与 `.SkipValidation()` 不受影响。

---

## 路由引擎

Server 使用唯一的未导出 method-first matcher。没有 Router、WithRouter 或 Server.Router；路由只通过 Route[Req, Resp](serverOrGroup).METHOD(path).To 注册。

首次服务入口冻结路由表。默认尾斜杠不敏感，WithStrictRouting 下尾斜杠不同；OPTIONS 不自动返回 204；HEAD 优先匹配显式 HEAD，否则回退 GET 并抑制 body。

只支持 {param} 与 {path...}。旧 :param 和 *path 路径立即报配置错误。请求路径不清洗、不重定向；双斜杠、dot segment 和非法百分号转义返回 400。{path...} 可以匹配零段。

中间件顺序固定为内建 Recovery、Server、父 Group、子 Group、Route、Handler。这与 Gin 和 Fiber 等按注册时机嵌套的常见模型不同。Server middleware 同时覆盖成功、400、404、405 和 OpenAPI endpoint。

类型化 To 支持显式状态码与响应头（见“输出”一节）。ToHTTP 与 ToRaw 得到原始 request；ghttp 不设置 PathValue，middleware 通过 `Server.MatchedParams(r)` 读取路径参数。

---

## 配置选项

```go
s := ghttp.New(
    ghttp.WithAddress(":8080"),                              // 监听地址
    ghttp.WithValidator(myValidator),                        // 验证器
    ghttp.WithEnvelope(myEnvelope),                          // Envelope 函数
    ghttp.WithConsumes(ghttp.MIMEJSON),                      // 默认请求 Content-Type
server 级 validator **默认关闭**：`New()` 不自动安装任何 validator，需要 `WithValidator(v)` 显式启用（破坏性变更）；`ghttp.NewDefaultValidator()` 可恢复内置 struct-tag 校验。路由级 `.Validate(fn)` 与 `.SkipValidation()` 不受影响。

    ghttp.WithBodyDecoder(func(r io.Reader, contentType string, target interface{}) error { // 自定义 Body 解码
        return customDecoder.Decode(r, target)
    }),
    ghttp.WithRenderer(myRenderer),                          // 模板渲染器
    ghttp.WithReadHeaderTimeout(5*time.Second),              // 请求头读取超时
    ghttp.WithReadTimeout(30*time.Second),                   // 读取超时
    ghttp.WithWriteTimeout(30*time.Second),                  // 写入超时
    ghttp.WithIdleTimeout(60*time.Second),                   // keep-alive 空闲超时
    ghttp.WithMaxHeaderBytes(1<<20),                         // 最大请求头
    ghttp.WithMaxBodyBytes(ghttp.DefaultMaxBodyBytes),        // 自动解析请求体大小上限，默认 4 MiB
    ghttp.WithTLSConfig(tlsConfig),                          // TLS 配置
    ghttp.WithBaseContext(baseContext),                      // 底层 Server BaseContext
    ghttp.WithConnContext(connContext),                      // 连接级 Context
    ghttp.WithErrorLog(errorLog),                            // 底层 Server 错误日志
)
```

```go
ln, err := net.Listen("tcp", "127.0.0.1:0")
if err != nil {
    return err
}
go s.Serve(ln)
fmt.Println(s.Addr()) // 包含 :0 自动分配后的真实端口

err = s.ListenAndServeTLS(":8443", "server.crt", "server.key")
```

---

## 项目结构

```
ghttp/
├── builder.go        # RouteBuilder 链式 API
├── client.go         # HTTP 客户端
├── codec*.go         # Codec 接口与实现（JSON/XML/Plain/Form）
├── config.go         # 配置与选项
├── constants.go      # MIME 类型常量
├── error.go          # HTTP 错误类型
├── form.go           # multipart/form 解析
├── handler.go        # HandlerFunc 类型定义
├── input.go          # 输入绑定引擎
├── logger.go         # Logger 接口
├── middleware.go     # 内建中间件
├── openapi.go        # OpenAPI 3.1 生成
├── output.go         # 响应输出与 Envelope
├── render.go         # 模板渲染
├── schema.go         # JSON Schema 生成
├── server.go         # Server 核心
├── sse.go            # SSE 支持
├── static.go         # 静态文件服务
├── internal/legacyrouter # 临时性能基线，不属于公开 API
├── upload.go         # FileHeader（文件上传）
├── util.go           # 工具函数
├── validate.go       # Validator 接口
├── writer.go         # ResponseWriter 包装
├── *_test.go         # 测试文件
└── README.md         # 本文件
```

---

## License

Same as [gk](https://github.com/sofiworker/gk) project.

### 文档与验证

`Doc(...)` 是唯一的路由文档入口，`Req` 自动推导 path/query/header/cookie 参数和 `Body` 请求体，`Resp` 自动推导成功响应的 data schema：

```go
ghttp.Route[CreateUserReq, UserDTO](app).
    POST("/users").
    Consumes(ghttp.MIMEJSON, ghttp.MIMEXML).
    Produces(ghttp.MIMEJSON, ghttp.MIMEXML).
    Doc(
        ghttp.Summary("Create user"),
        ghttp.Tags("users"),
        ghttp.OperationID("createUser"),
        ghttp.Success(ghttp.Code(0), ghttp.Message("created")),
        ghttp.Errors(ErrInvalidInput),
        ghttp.Deprecated("use /v2/users instead"),
        ghttp.Sunset(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
        ghttp.ExternalDocs("migration guide", "https://example.com/migrate-users"),
    ).
    Validate(validateCreateUser).
    To(createUser)
```

`Deprecated`、`Sunset`、`ExternalDocs` 只影响 OpenAPI 文档，不改变路由匹配、状态码或 handler 执行。

`Validate(fn)` 默认把错误当作参数验证失败返回；需要覆盖错误响应时使用 `Validate(fn, ghttp.ValidationError(err))`。`SkipValidation()` 只跳过 server 级 validator，不跳过 route 自己声明的 `Validate(fn)`。
