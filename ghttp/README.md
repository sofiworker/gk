# ghttp

通用 Go HTTP 框架 —— 基于 Go 1.24+ 泛型和标准库 `net/http`，整合服务端与客户端，内建 OpenAPI 3.1 文档生成、内容协商、模板渲染、WebSocket/SSE 等能力。

---

## 特性

- **泛型优先 API** — `Route[Req,Resp]` 链式构建器，编译期类型安全
- **显式输入读取** — `Params` 读取 path/query/header/cookie，`Body` 字段解析请求体
- **内容协商** — `Accept` 驱动响应 Codec，`Consumes` 约束请求 `Content-Type`
- **灵活输出** — 支持响应体结构体和自定义 Envelope 包装（code/msg/data 模式）
- **路由器可插拔** — 内建高性能 Radix 树路由器和 Go 1.22+ `http.ServeMux` 适配
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
        Produces(ghttp.MIMEJSON).
        Doc("返回个性化的问候消息").
        Reads(GreetInput{}).
        Responds(200).With(GreetOutput{}).Desc("成功").
        End().
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
```

---

## API 概览

### 路由注册

| 函数 | 说明 |
|------|------|
| `Route[Req,Resp](target)` | 链式构建器起始，`target` 可以是 `*Server` 或 `*Group` |
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
| `.Doc("描述")` | 操作描述 |
| `.Reads(input)` | 请求体类型（用于 OpenAPI） |
| `.Consumes(contentTypes...)` | 声明可自动解析的请求 Content-Type，可在 server/group/route 上声明 |
| `.Produces(contentType)` | 自动响应编码的 Content-Type，可在 server/group/route 上声明 |
| `.Responds(code)` | 响应状态码 |
| `.With(output)` | 响应体类型 |
| `.Desc("说明")` | 响应说明 |
| `.Tags("标签")` | OpenAPI 标签 |
| `.End()` | 结束方法声明，等待 `To()` |
| `.To(handler)` | 注册处理函数，配置错误会在 `Run` / `Serve` / 首次 `ServeHTTP` 时 panic |

### 路由参数

路由路径推荐使用 Go 标准库和 OpenAPI 一致的 `{param}` 语法：

- `{param}` — 命名参数
- `{path...}` — 通配符（匹配剩余路径）

```go
ghttp.Route[Req, Resp](s).GET("/users/{id}").To(handler)      // RadixRouter
ghttp.Route[Req, Resp](s).GET("/files/{path...}").To(handler)  // 通配符
```

旧的 `:param` / `*path` 语法仍然兼容，但注册时会输出 warning；新代码应统一使用 `{param}` / `{path...}`。

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
    Produces(ghttp.MIMEJSON).
    To(createUser)
```

`Consumes` 只约束带 `Body` 的自动解析路由。请求 `Content-Type` 为空时仍按默认 JSON 解析；显式传入不匹配的媒体类型会返回 `415 Unsupported Media Type`。

`Params` 是请求输入快照，不持有 `ResponseWriter`，也不负责中断请求或写响应；cookie 写入通过输出对象完成。

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

`Route[*ghttp.Params, Resp]`、`Params ghttp.Params` 命名字段、`*ghttp.Params` 匿名指针字段、间接嵌入 `Params` 都会在 `Run` / `Serve` / 首次 `ServeHTTP` 时 panic，panic error 可用 `errors.Is(err, ghttp.ErrInvalidParamsUsage)` 判断。

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

### 中间件

```go
// 注入结构化 logger，glog.Default() 可直接满足 ghttp.Logger。
s := ghttp.New(ghttp.WithLogger(glog.Default()))

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
```

### WebSocket

```go
ghttp.Route[struct{}, struct{}](s).GET("/ws").ToWebSocket(func(ctx context.Context, params ghttp.Params, conn *ghttp.WebSocketConn) error {
    for {
        var msg map[string]interface{}
        err := conn.ReadJSON(&msg)
        if err != nil { return err }
        conn.WriteJSON(msg)
    }
})
```

### SSE

```go
ghttp.Route[struct{}, struct{}](s).GET("/events").ToSSE(func(ctx context.Context, params ghttp.Params, w *ghttp.SSEWriter) error {
    for i := 0; i < 10; i++ {
        w.WriteEvent("message", fmt.Sprintf("event %d", i))
        time.Sleep(time.Second)
    }
    return nil
})
```

### 静态文件

```go
ghttp.Route[struct{}, struct{}](s).GET("/static").ToStatic("./public")
ghttp.Route[struct{}, struct{}](s).GET("/assets").ToStaticFS(http.FS(embeddedAssets))
ghttp.Route[struct{}, struct{}](s).GET("/favicon.ico").ToStaticFile("./favicon.ico")
```

### 模板渲染

```go
renderer := ghttp.NewRenderer("./templates/*.html")
s = ghttp.New(ghttp.WithRenderer(renderer))

// 处理函数中
ghttp.Route[NoInput, NoOutput](s).GET("/page").Produces(ghttp.MIMEJSON).To(func(ctx context.Context, req NoInput) (NoOutput, error) {
    return NoOutput{}, nil
})
```

### OpenAPI 文档

自动生成，通过 `To()` 方法收集路由元数据构建。

```go
// 挂载 OpenAPI JSON 端点
ghttp.Route[struct{}, struct{}](s).GET("/openapi.json").ToHTTP(handler)
```

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

---

## 路由引擎

`ghttp` 支持两种路由器实现：

| 路由器 | 说明 |
|--------|------|
| `RadixRouter`（默认） | 三阶段 Radix 树（静态/参数/通配符），高性能 |
| `StdRouter` | 适配 Go 1.22+ 标准库 `http.ServeMux` |

通过 `WithRouter(RadixRouter)` / `WithRouter(StdRouter)` 切换。

---

## 配置选项

```go
s := ghttp.New(
    ghttp.WithAddress(":8080"),                              // 监听地址
    ghttp.WithRouter(ghttp.NewRadixRouter()),                // 路由器
    ghttp.WithValidator(myValidator),                        // 验证器
    ghttp.WithEnvelope(myEnvelope),                          // Envelope 函数
    ghttp.WithConsumes(ghttp.MIMEJSON),                      // 默认请求 Content-Type
    ghttp.WithBodyDecoder(func(r io.Reader, contentType string, target interface{}) error { // 自定义 Body 解码
        return customDecoder.Decode(r, target)
    }),
    ghttp.WithRenderer(myRenderer),                          // 模板渲染器
    ghttp.WithReadHeaderTimeout(5*time.Second),              // 请求头读取超时
    ghttp.WithReadTimeout(30*time.Second),                   // 读取超时
    ghttp.WithWriteTimeout(30*time.Second),                  // 写入超时
    ghttp.WithIdleTimeout(60*time.Second),                   // keep-alive 空闲超时
    ghttp.WithMaxHeaderBytes(1<<20),                         // 最大请求头
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
├── radix.go          # Radix 树实现
├── radix_router.go   # RadixRouter
├── render.go         # 模板渲染
├── router.go         # Router 接口
├── schema.go         # JSON Schema 生成
├── server.go         # Server 核心
├── sse.go            # SSE 支持
├── static.go         # 静态文件服务
├── std_router.go     # StdRouter（net/http ServeMux）
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
