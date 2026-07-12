# ghttp Server 路由迁移

> **破坏性发布说明：** 本次路由重构是 breaking release。升级时必须完成下表全部替换；不会提供 deprecated wrapper、旧 `Router` 公开 API 或旧路径语法兼容层。请在升级前完成注册代码迁移，并在首次服务入口前注册全部路由。

| 旧用法或行为 | 新行为 |
|---|---|
| Router、WithRouter、Server.Router | 已删除；Server 始终使用唯一内部 matcher |
| RadixRouter、CompiledRouter、MatchitRouter、StdRouter | 已删除公开 API；只保留 ghttp/internal/legacyrouter 作为临时 benchmark 基线 |
| :id、*path | 使用 {id}、{path...}；旧语法立即 panic |
| 运行期或首次服务后注册 | 首次服务入口冻结；之后 panic ErrServerFrozen |
| 终结后或启动时才发现配置错误 | 任一 To 终结调用立即 panic |
| StatusCoder 或响应 Status int | To 固定 200；自定义状态使用 ToHTTPFunc 或其他自写响应终结器 |
| Router 注册时机影响 middleware | 固定 Server、Group ancestry、Route、Handler 顺序 |
| 公开 OpenAPI builder | Server.OpenAPI 返回文档字节 |
| 手写 /openapi.json 注册 | WithOpenAPI 默认注册内部 /openapi.json；WithOpenAPIPath 空路径关闭 HTTP 暴露 |

## 逐项迁移示例

以下 `before` 示例仅描述旧版本用法，不能与本次发布的 API 混用。

### 1. 移除 Router、WithRouter 和 Server.Router

```go
// before
router := ghttp.NewRadixRouter()
app := ghttp.New(ghttp.WithRouter(router))
_ = app.Router().Register(http.MethodGet, "/health", handler)

// after
app := ghttp.New()
ghttp.Route[struct{}, struct{}](app).GET("/health").ToHTTP(handler)
```

### 2. 移除具体 Router 实现

```go
// before
app := ghttp.New(ghttp.WithRouter(ghttp.NewRadixRouter()))
// CompiledRouter、MatchitRouter 和 StdRouter 也可在旧版本中传入。

// after
app := ghttp.New() // 始终使用唯一的内部 matcher
```

`ghttp/internal/legacyrouter` 只用于临时 benchmark 基线，不是可导入的应用 API。

### 3. 替换旧路径参数语法

```go
// before
ghttp.Route[Req, Resp](app).GET("/users/:id").To(handler)
ghttp.Route[Req, Resp](app).GET("/files/*path").To(handler)

// after
ghttp.Route[Req, Resp](app).GET("/users/{id}").To(handler)
ghttp.Route[Req, Resp](app).GET("/files/{path...}").To(handler)
```

旧语法在终结调用处立即 panic；`{path...}` 必须位于路径末尾。

### 4. 在首次服务前完成注册

```go
// before
go app.Run(":8080")
ghttp.Route[Req, Resp](app).GET("/late").To(handler)

// after
ghttp.Route[Req, Resp](app).GET("/ready").To(handler)
go app.Run(":8080")
```

`ServeHTTP`、`Run`、`Serve`、`ListenAndServeTLS`、`ServeTLS` 和其他 TLS 服务入口都会冻结路由表；之后注册或修改路由会 panic `ghttp.ErrServerFrozen`。

### 5. 在 To 终结调用处处理配置错误

```go
// before: 错误可能延后到启动时才报告
ghttp.Route[Req, Resp](app).GET("/users/:id").To(handler)
app.Run(":8080")

// after: 错误在 To 处立即报告；修正后再继续
ghttp.Route[Req, Resp](app).GET("/users/{id}").To(handler)
```

这同样适用于 `ToHTTP`、`ToRaw`、`ToHTTPFunc` 和其他终结方法。

### 6. 使用自写响应终结器设置状态码

```go
// before
type created struct {
    Status int
    ID     string `json:"id"`
}

ghttp.Route[Req, created](app).POST("/users").To(func(context.Context, Req) (created, error) {
    return created{Status: http.StatusCreated, ID: "42"}, nil
})

// after
ghttp.Route[Req, struct{}](app).POST("/users").ToHTTPFunc(func(w http.ResponseWriter, _ *http.Request, input Req) error {
    w.WriteHeader(http.StatusCreated)
    _, err := w.Write([]byte(`{"id":"42"}`))
    return err
})
```

类型化 `To` 成功响应固定为 200；不要再依赖 `StatusCoder` 或响应结构体的 `Status` 字段。

### 7. 按固定层级注册中间件

```go
// before: Group.Use 的实际影响可能依赖路由注册时机
api := app.Group("/api")
ghttp.Route[Req, Resp](api).GET("/users").To(handler)
api.Use(auth)

// after
app.Use(serverMiddleware)
api := app.Group("/api", auth)
ghttp.Route[Req, Resp](api).GET("/users").Use(routeMiddleware).To(handler)
```

运行时顺序固定为 Server、父 Group、子 Group、Route、Handler；将某层 middleware 加入其所属层级即可。

### 8. 从 Server 获取 OpenAPI 快照

```go
// before
documenter := ghttp.NewOpenAPI("Accounts", "1.0.0")
documenter.AddRoute(http.MethodGet, "/users/:id", reqType, respType, doc, nil, nil)
document := documenter.Build()

// after
app := ghttp.New(ghttp.WithOpenAPI("Accounts", "1.0.0"))
ghttp.Route[Req, Resp](app).GET("/users/{id}").To(handler)
document, err := app.OpenAPI()
```

`Server.OpenAPI` 返回调用时刻的 best-effort 快照，不会冻结路由表或改变运行时响应。

### 9. 由 WithOpenAPI 暴露 OpenAPI HTTP 端点

```go
// before
ghttp.Route[struct{}, struct{}](app).GET("/openapi.json").ToHTTP(openAPIHandler)

// after: 默认内部注册 /openapi.json
app := ghttp.New(ghttp.WithOpenAPI("Accounts", "1.0.0"))

// after: 保留 Server.OpenAPI，但不暴露 HTTP 端点
app = ghttp.New(
    ghttp.WithOpenAPI("Accounts", "1.0.0"),
    ghttp.WithOpenAPIPath(""),
)
```

## 路由差异

- 默认尾斜杠不敏感；WithStrictRouting 下 /x 与 /x/ 不同。
- OPTIONS 不自动返回 204；未显式注册时，存在路径返回 405。
- HEAD 先匹配显式 HEAD，否则执行 GET 并抑制 response body。
- CUSTOM 保留原始大小写，不 trim。
- {path...} 可以匹配零段，零段值为空字符串。
- 请求路径不会清洗或重定向。双斜杠、dot segment 和非法转义返回 400。

## Raw Handler

ToHTTP 与 ToRaw 收到未经 ghttp 参数注入的原始 request。ghttp 不设置 Request.PathValue；需要绑定 Params 的自写响应 handler 使用 ToHTTPFunc。

## OpenAPI

OpenAPI 是附属能力。缺失或无法推导的字段会省略，不能改变路由注册、请求匹配、响应状态、Envelope 或 raw handler 语义。当前不提供 Swagger UI 或 docs 页面。
