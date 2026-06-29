# ghttp

通用 Go HTTP 框架 —— 基于 Go 1.24+ 泛型和标准库 `net/http`，整合服务端与客户端，内建 OpenAPI 3.1 文档生成、内容协商、模板渲染、WebSocket/SSE 等能力。

---

## 特性

- **泛型优先 API** — `Route[Req,Resp]` 链式构建器，编译期类型安全
- **四段输入绑定** — `Path`, `Query`, `Header`, `Body` 自动解析到嵌套结构体
- **内容协商** — `Accept`/`Content-Type` 驱动 Codec（JSON、XML、Plain、Form），可扩展
- **灵活输出** — 支持响应体结构体和自定义 Envelope 包装（code/msg/data 模式）
- **路由器可插拔** — 内建高性能 Radix 树路由器和 Go 1.22+ `http.ServeMux` 适配
- **OpenAPI 3.1** — 从路由元数据自动生成 JSON Schema 和 OAS 文档
- **WebSocket / SSE** — 服务端推送和双向通信
- **模板渲染** — 集成 Go 模板引擎（类似 Gin 的 `HTML()`）
- **静态文件服务** — `Static()` / `StaticFS()` / `StaticFile()` 一行挂载
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
    ghttp.Path
    ghttp.Query
    ghttp.Header
    ghttp.Body

    Name string `path:"name"`
}

type GreetOutput struct {
    Message string `json:"message"`
}

func main() {
    s := ghttp.New()

    ghttp.Get[GreetInput, GreetOutput](s, "/hello/{name}", func(ctx context.Context, req *GreetInput) (*GreetOutput, error) {
        return &GreetOutput{Message: "Hello, " + req.Name}, nil
    })

    // 链式构建器（支持 OpenAPI 元数据）
    ghttp.Route[GreetInput, GreetOutput](s, "/hello/{name}").
        GET("获取问候").
        Doc("返回个性化的问候消息").
        Reads(GreetInput{}).
        Responds(200).With(GreetOutput{}).Desc("成功").
        End().
        To(func(ctx context.Context, req *GreetInput) (*GreetOutput, error) {
            return &GreetOutput{Message: "Hello, " + req.Name}, nil
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
input := &GreetInput{Name: "World"}
resp, err := ghttp.Do[GreetInput, GreetOutput](client, "POST", "/hello", input)
```

---

## API 概览

### 路由注册

| 函数 | 说明 |
|------|------|
| `Route[Req,Resp](s, path)` | 链式构建器起始 |
| `Get[Req,Resp](s, path, handler)` | 快捷 GET |
| `Post[Req,Resp](s, path, handler)` | 快捷 POST |
| `Put[Req,Resp](s, path, handler)` | 快捷 PUT |
| `Delete[Req,Resp](s, path, handler)` | 快捷 DELETE |
| `Patch[Req,Resp](s, path, handler)` | 快捷 PATCH |

### 链式构建器方法

| 方法 | 说明 |
|------|------|
| `.GET("摘要")` | 注册 GET 方法及摘要 |
| `.POST("摘要")` | 注册 POST 方法及摘要 |
| `.PUT("摘要")` | 注册 PUT 方法及摘要 |
| `.DELETE("摘要")` | 注册 DELETE 方法及摘要 |
| `.PATCH("摘要")` | 注册 PATCH 方法及摘要 |
| `.Doc("描述")` | 操作描述 |
| `.Reads(input)` | 请求体类型（用于 OpenAPI） |
| `.Responds(code)` | 响应状态码 |
| `.With(output)` | 响应体类型 |
| `.Desc("说明")` | 响应说明 |
| `.Tag("标签")` | OpenAPI 标签 |
| `.End()` | 结束方法声明，等待 `To()` |
| `.To(handler)` | 注册处理函数 |

### 路由参数

路由路径支持三种参数语法：

- `{param}` — 命名参数（RadixRouter）
- `:param` — 命名参数（StdRouter）
- `*` — 通配符（匹配剩余路径）

```go
ghttp.Get[Req, Resp](s, "/users/{id}", handler)    // RadixRouter
ghttp.Get[Req, Resp](s, "/users/:id", handler)      // StdRouter
ghttp.Get[Req, Resp](s, "/files/{path:.*}", handler) // 通配符
```

### 输入结构体

```go
type CreateUserInput struct {
    ghttp.Path
    ghttp.Query
    ghttp.Header
    ghttp.Body

    ID     int    `path:"id"`              // URL 路径参数
    Role   string `query:"role"`           // 查询参数
    Token  string `header:"Authorization"` // 请求头
    Name   string `json:"name"`            // 请求体字段
    Age    int    `json:"age"`
    Active bool   `json:"active"`
}
```

### 输出

```go
type UserOutput struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
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
s.Upgrade("/ws", func(ctx *ghttp.WSContext) error {
    for {
        msg, err := ctx.ReadMessage()
        if err != nil { return err }
        ctx.WriteMessage(msg)
    }
})
```

### SSE

```go
s.SSE("/events", func(ctx context.Context, w *ghttp.SSEWriter) error {
    for i := 0; i < 10; i++ {
        w.WriteEvent(&ghttp.SSEEvent{Data: []byte(fmt.Sprintf("event %d", i))})
        time.Sleep(time.Second)
    }
    return nil
})
```

### 静态文件

```go
s.Static("/static", "./public")
s.StaticFS("/assets", http.FS(embeddedAssets))
s.StaticFile("/favicon.ico", "./favicon.ico")
```

### 模板渲染

```go
renderer := ghttp.NewRenderer("./templates/*.html")
s = ghttp.New(ghttp.WithRenderer(renderer))

// 处理函数中
ghttp.Get[NoInput, NoOutput](s, "/page", func(ctx context.Context, req *NoInput) (*NoOutput, error) {
    s.Render(ctx, 200, "index.html", gin.H{"title": "Hello"})
    return nil, nil
})
```

### OpenAPI 文档

自动生成，通过 `To()` 方法收集路由元数据构建。

```go
// 挂载 OpenAPI JSON 端点
s.GET("/openapi.json", handler)
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
    ghttp.WithBodyDecoder(func(io.ReadCloser, any) error {   // 自定义 Body 解码
        return customDecoder.Decode(r, v)
    }),
    ghttp.WithRenderer(myRenderer),                          // 模板渲染器
    ghttp.WithReadTimeout(30*time.Second),                   // 读取超时
    ghttp.WithWriteTimeout(30*time.Second),                  // 写入超时
    ghttp.WithMaxHeaderBytes(1<<20),                         // 最大请求头
)
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
├── context.go        # Context 接口
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
