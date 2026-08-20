# ghttp

[English](README.en.md) | 中文

> 开发中，禁止直接用于生产开发。
>
> ghttp 仍处于 pre-v1.0.0，公共 API、默认行为和性能实现均未冻结。本次服务端重写是破坏性变更，已移除 `Route[Req, Resp]`、`RouteBuilder` 及其链式终结器。

ghttp 是基于标准库 `net/http` 的 HTTP 客户端与服务端模块。服务端以不可变 `Operation` 为核心：方法、路径、输入、输出、文档和业务逻辑先编译成一等值，再挂载到 `Server` 或 `Group`。请求热路径不扫描结构体 tag，也不经过旧 `RouteBuilder`。

## 核心特性

- `Operation` 是可复用、copy-on-write 的 endpoint 描述，可挂载到多个 Server 或 Group。
- `Input[T]` 显式描述 path/query/header/cookie/body 如何构造业务输入。
- `Output[T]` 显式描述状态码、Content-Type、响应头、序列化和 OpenAPI schema。
- 路由注册时校验方法、路径、参数元数据、重复参数、多个请求体和输出契约。
- 内建 method-aware 冻结路由树，支持静态、`{param}` 和 `{path...}` 路径。
- OpenAPI 3.1 直接来自输入和输出契约，不依赖 handler 反射。
- JSON/path 快捷 Operation 保留直接调用和 direct-path 快路径。
- 统一支持错误管线、Problem Details、Envelope、校验、body limit 和内容协商。
- 支持原生 `http.Handler`、类型化 HTTP handler、WebSocket、SSE 和安全静态文件。

## 安装

```bash
go get github.com/sofiworker/gk
```

```go
import "github.com/sofiworker/gk/ghttp"
```

模块当前要求 Go 1.25.0 及以上。Go 1.27 文件包含泛型方法语法，按工具链版本自动选择。

## 快速开始

```go
package main

import (
    "context"
    "log"

    "github.com/sofiworker/gk/ghttp"
)

type LookupUser struct {
    ID     int64
    Locale string
}

type User struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

func main() {
    input := ghttp.MapInputs(
        ghttp.PathInt64("id", ghttp.Minimum(1)),
        ghttp.QueryString("locale", ghttp.AllowedValues("zh-CN", "en-US")),
        func(id int64, locale string) LookupUser {
            return LookupUser{ID: id, Locale: locale}
        },
    )

    getUser := ghttp.Handle(
        ghttp.Get("/users/{id}"),
        input,
        ghttp.JSONOutput[User](),
        func(ctx context.Context, lookup LookupUser) (User, error) {
            return User{ID: lookup.ID, Name: lookup.Locale}, nil
        },
    ).Doc(
        ghttp.Summary("查询用户"),
        ghttp.Tags("users"),
        ghttp.OperationID("getUser"),
    )

    server := ghttp.New(ghttp.WithOpenAPI("users", "0.1.0"))
    server.MustMount(getUser)
    log.Fatal(server.Run(":8080"))
}
```

请求 `GET /users/7?locale=zh-CN` 返回：

```json
{"id":7,"name":"zh-CN"}
```

## Operation 模型

`EndpointBuilder` 只保存 HTTP 方法和路径；`Handle` 将它与输入、输出和 handler 编译为 `Operation`：

```go
operation := ghttp.Handle(
    ghttp.Post("/users"),
    ghttp.JSONBody[CreateUser](),
    ghttp.WithResponseHeader(
        "X-Contract", "create-user",
        ghttp.WithStatus(http.StatusCreated, ghttp.JSONOutput[User]()),
    ),
    createUser,
)
```

内建起点为 `Get`、`Post`、`Put`、`Patch`、`Delete`、`Head`、`Options`、`Connect` 和 `Trace`。自定义方法使用：

```go
ghttp.Endpoint("PURGE", "/cache")
```

Operation 的配置方法全部返回副本，不修改原对象：

| 方法 | 作用 |
|---|---|
| `Doc(options...)` | 添加 summary、tags、operationId 等 OpenAPI 元数据 |
| `WithMiddleware(middlewares...)` | 添加路由级标准 `net/http` 中间件 |
| `WithMaxBodyBytes(n)` | 覆盖当前 Operation 的 body limit，`n <= 0` 表示禁用 |
| `WithErrorWriter(writer)` | 覆盖当前 Operation 的错误 writer |
| `WithProblemDetails()` | 使用 RFC 9457 `application/problem+json` |
| `WithoutServerValidation()` | 跳过服务器级 Validator，输入自身校验仍执行 |
| `WithWebSocketOriginCheck(check)` | 覆盖当前 WebSocket Operation 的 Origin 校验 |

`Method()` 与 `Path()` 用于检查描述。`Mount` 返回注册错误；`MustMount` 在程序员配置错误时 panic：

```go
server.MustMount(operationA, operationB)

api := server.Group("/api", authMiddleware)
api.MustMount(operationA.Doc(ghttp.Tags("api")))
```

同一个 Operation 可挂载到不同 Group 或 Server。组前缀、中间件和服务器配置不会写回 Operation。

## Go 1.27 前后 API

两套语法共享 `compileOperation`、输入/输出描述器和执行管线，行为不会分叉。

Go 1.27 前使用包级泛型函数：

```go
operation := ghttp.Handle(
    ghttp.Get("/users/{id}"),
    ghttp.PathInt64("id"),
    ghttp.JSONOutput[User](),
    getUser,
)
```

Go 1.27 起可使用泛型方法并自动推断类型：

```go
operation := ghttp.Get("/users/{id}").Handle(
    ghttp.PathInt64("id"),
    ghttp.JSONOutput[User](),
    getUser,
)
```

Go 1.27 仍保留包级 `Handle`，便于同一份源码跨版本迁移。Go 1.27 专用文件无法由旧版 `gofmt` 解析，应使用对应版本工具链格式化。

## 输入契约

### 内建输入

| 构造函数 | 返回类型 | 行为 |
|---|---|---|
| `NoInput()` | `Input[EmptyInput]` | 不读取请求 |
| `PathString/PathInt64/PathBool/PathFloat64` | 对应标量 | 必需路径参数 |
| `PathRemainder` | `string` | `{name...}` catch-all 参数 |
| `QueryString/QueryInt/QueryBool/QueryFloat64` | 对应标量 | 必需 query 参数 |
| `QueryIntDefault` | `int` | 缺失时使用默认值 |
| `QueryStringDefault` | `string` | 缺失时使用默认值 |
| `QueryBoolDefault` | `bool` | 缺失时使用默认值 |
| `QueryFloat64Default` | `float64` | 缺失时使用默认值 |
| `HeaderStringDefault` | `string` | 缺失时使用默认值 |
| `CookieStringDefault` | `string` | 缺失时使用默认值 |
| `QueryStrings` | `[]string` | 可重复 query 参数 |
| `HeaderString` | `string` | 必需请求头 |
| `CookieString` | `string` | 必需 Cookie |
| `JSONBody[T]` | `T` | 必需 JSON body，可追加显式 validator |
| `FormBody` | `url.Values` | urlencoded body |
| `MultipartFile` | `*FileHeader` | multipart 文件字段 |
| `HTTPRequest` | `*http.Request` | 输入侧底层请求逃生口 |
| `ValidatedInput[T]` | `T` | 包装任意输入契约并执行 endpoint 级校验 |

数值参数只接受 `NumberConstraint`：`Minimum`、`Maximum`。字符串参数只接受 `StringConstraint`：`AllowedValues`。这种拆分避免生成与运行时类型不一致的 schema。

### 组合与映射

```go
type Search struct {
    Tenant string
    Page   int
    Tags   []string
}

input := ghttp.MapInputs3(
    ghttp.HeaderString("X-Tenant"),
    ghttp.QueryIntDefault("page", 1, ghttp.Minimum(1)),
    ghttp.QueryStrings("tag"),
    func(tenant string, page int, tags []string) Search {
        return Search{Tenant: tenant, Page: page, Tags: tags}
    },
)
```

`CombineInputs`/`CombineInputs3` 返回 `InputPair`/`InputTriple`；`MapInputs`/`MapInputs3`/`MapInputs4`/`MapInputs5` 直接构造业务类型（三/四/五元版本为扁平实现，无中间 Pair 嵌套）。组合器共享一个请求状态，query、body、form 和 multipart 不会被重复解析。

任意复杂输入使用 `InputFunc`。需要 OpenAPI 时使用 `InputFuncWithMetadata`：

```go
input := ghttp.InputFuncWithMetadata(
    func(request ghttp.RequestView) (Tenant, error) {
        return Tenant{ID: request.Header().Get("X-Tenant")}, nil
    },
    ghttp.InputMetadata{Parameters: []ghttp.InputParameter{{
        Name: "X-Tenant", Location: ghttp.ParameterLocationHeader,
        Required: true, Schema: map[string]any{"type": "string"},
    }}},
)
```

`RequestView` 提供 `Context`、`HTTPRequest`、`Path`、`Query`、`Header`、`Cookie` 和 `ClientIP`。视图仅在输入构造期间有效，返回的 header/query 不应修改。

注册期会拒绝重复参数、空参数、非法位置、不存在的 path 参数、多个独立请求体、nil constructor 和 nil mapper。

## 输出契约

| 构造函数 | 输出 |
|---|---|
| `JSONOutput[T]` | JSON 200，写状态前完成序列化 |
| `CodecOutput[T]` | 使用 Server 已注册 Codec 按 `Accept` 协商 |
| `TextOutput` | `text/plain` 200 |
| `XMLOutput[T]` | XML 200，写状态前完成序列化 |
| `HTMLOutput[T]` | `html/template`，写状态前执行模板 |
| `BytesOutput` | 指定 Content-Type 的 `[]byte` |
| `NoContentOutput[T]` | 204，无响应体 |
| `RedirectOutput` | handler 返回 `RedirectResponse{Location: ...}` |
| `DownloadOutput` | 带附件文件名的字节响应 |
| `StreamOutput` | handler 返回 `func(io.Writer) error` |
| `SSEOutput` | handler 返回 `func(*SSEWriter) error` |
| `FileOutput` | 从起点发送 `io.ReadSeeker` |
| `OutputFunc` | 自定义状态码、Content-Type 和写入函数 |

输出包装器：

- `WithStatus(status, output)` 覆盖成功状态码。
- `WithResponseHeader(name, value, output)` 添加固定响应头。
- `WithResponseCookie(cookie, output)` 添加响应 Cookie。
- `WithOutputSchema(schema, output)` 覆盖成功响应 schema。
- `WithDocumentedResponses(responses, output)` 声明额外 OpenAPI responses。

非法状态码、nil 模板、nil 自定义输出函数会在 `Mount` 时返回可判断的导出错误。JSON/XML/HTML 的准备失败、文件 seek 失败和 SSE writer 能力不足都发生在响应提交前，仍可进入统一错误管线。

## 快捷 Operation

常见 JSON 和文本 endpoint 可直接创建：

```go
server.MustMount(
    ghttp.GetJSON("/users/{id}", ghttp.PathInt64("id"), getUser),
    ghttp.PostJSON("/users", ghttp.JSONBody[CreateUser](), createUser),
    ghttp.CreatedJSON("/users", ghttp.JSONBody[CreateUser](), createUser),
    ghttp.GetText("/health", ghttp.NoInput(), health),
)
```

还提供 `PutJSON`、`PatchJSON`、`DeleteJSON` 和对应 Text 版本。单 path 参数 JSON 快捷 Operation 使用直接值编译路径，避免构造通用参数容器。

## 原生 HTTP、WebSocket、SSE 与静态文件

```go
raw := ghttp.RawOperation(http.MethodGet, "/metrics", metricsHandler)

httpFunc := ghttp.HTTPFuncOperation(
    http.MethodGet, "/raw",
    func(w http.ResponseWriter, r *http.Request) error { return nil },
)

typedHTTP := ghttp.HandleHTTP(
    ghttp.Get("/files/{name}"),
    ghttp.PathString("name"),
    func(w http.ResponseWriter, r *http.Request, name string) error { return nil },
)
```

Go 1.27 起 `EndpointBuilder.HandleHTTP` 可替代包级 `HandleHTTP`。

WebSocket 使用显式输入并复用 ghttp 的连接、Origin、subprotocol、buffer、logger 和 keepalive 配置：

```go
ws := ghttp.WebSocketOperation(
    "/chat/{room}",
    ghttp.PathString("room"),
    func(ctx context.Context, room string, conn *ghttp.WebSocketConn) error {
        return nil
    },
)
```

SSE 使用普通输出契约：

```go
events := ghttp.Handle(
    ghttp.Get("/events"),
    ghttp.NoInput(),
    ghttp.SSEOutput(),
    func(ctx context.Context, _ ghttp.EmptyInput) (func(*ghttp.SSEWriter) error, error) {
        return func(stream *ghttp.SSEWriter) error {
            return stream.WriteJSON("tick", map[string]any{"ready": true})
        }, nil
    },
)
```

静态文件：

```go
server.MustMount(
    ghttp.StaticDirectory("/assets", "./public"),
    ghttp.StaticFileSystem("/embedded", http.FS(assets)),
    ghttp.StaticFile("/favicon.ico", "./favicon.ico"),
)
```

`StaticDirectory` 使用 `NewSafeFS`，拒绝目录穿越、反斜杠和符号链接逃逸。

## 错误、校验与请求体上限

handler 返回 `*HTTPError` 时保留其状态码：

```go
return User{}, ghttp.NotFound("user not found")
```

默认错误体为 `{code,message}`。服务器级 `WithProblemDetails()` 或 Operation 的 `WithProblemDetails()` 切换到 RFC 9457。`WithErrorWriter` 可安装可组合 writer。

服务器级 Validator 默认关闭：

```go
server := ghttp.New(ghttp.WithValidator(ghttp.NewDefaultValidator()))
```

Operation 在输入构造后调用服务器 Validator，失败默认返回 422。`WithoutServerValidation` 只跳过服务器 Validator；`JSONBody` validator 和 `InputFunc` 内部校验不受影响。

`WithMaxBodyBytes` 提供服务器默认值；`operation.WithMaxBodyBytes` 覆盖单路由。JSON、urlencoded、multipart 与 `RawBody` 共享 request body 状态与相同上限。超过上限返回 413。

内容协商默认严格：

- 请求 body 的 Content-Type 与输入契约不匹配时返回 415。
- `Accept` 明确排除输出类型时返回 406。
- `Accept` 为空或 `*/*` 时接受输出类型。

## OpenAPI 3.1

```go
server := ghttp.New(ghttp.WithOpenAPI("users", "0.1.0"))
server.MustMount(operation)

document, err := server.OpenAPI()
```

默认同时注册 `/openapi.json`。Operation 注册时冻结以下信息：

- path/query/header/cookie 参数及 required、format、minimum、maximum、enum；
- JSON、form、multipart 请求体 schema；
- 成功状态、Content-Type、响应 header、Cookie 和 body schema；
- 额外 responses、Problem Details schema、WebSocket/SSE 扩展；
- summary、description、tags、operationId、deprecated、sunset 和 external docs。

未显式设置时，summary 由方法和路径生成，operationId 使用规范化名称，例如 `GET /users/{id}` 生成 `get_users_by_id`。显式 `OperationID` 始终优先。

## Server 与中间件

`Server` 实现 `http.Handler`。路由在首次请求前冻结；冻结后继续注册会返回或 panic `ErrServerFrozen`。

```go
server := ghttp.New(
    ghttp.WithAddress(":8080"),
    ghttp.WithReadHeaderTimeout(5*time.Second),
    ghttp.WithIdleTimeout(60*time.Second),
)

server.Use(ghttp.RequestID(), ghttp.Recoverer())
server.SkipUse(authMiddleware, http.MethodGet, "/health")

go server.Run()
defer server.Shutdown(context.Background())
```

`Server.Use`、`Group` 和 `Operation.WithMiddleware` 均使用标准 `func(http.Handler) http.Handler`。顺序为 server、group、operation，由外向内执行。

类型安全键挂在标准 request context 上，net/http 中间件可直接读写：

```go
var requestID = ghttp.NewKey[string]("request-id") // 包级变量,类型编译期固定

ctx = requestID.Set(ctx, "abc123")
id, ok := requestID.Get(ctx)
```

## 客户端

客户端 API 保持独立：

```go
client := ghttp.NewClient()

response, err := client.R().
    SetHeader("Authorization", "Bearer token").
    SetQueryParam("lang", "zh-CN").
    Get("/users/7")

typed, err := ghttp.GET[GetUserRequest, User](client, "/users/7", nil)
```

客户端支持重试、before/after hooks、认证、Cookie、请求级超时、流式响应、错误模型绑定和自定义 `http.Client`/Transport。Go 1.27 起同时提供泛型客户端方法。

## 破坏性变更记录

`Route[Req, Resp](target).GET(path).To(...)`、Go 1.27 的 `server.GET(path).To(...)`、`RouteOption` 与全部旧终结器均已删除，不提供 deprecated shim 或测试专用兼容入口。

历史重写的破坏性变化包括：

- 唯一注册模型改为 `Operation` + `Mount/MustMount`。
- 输入改为显式 `Input[T]` 描述器；结构体绑定使用 `StructInput[T]`。
- 输出改为显式 `Output[T]` 契约；多格式响应使用 `CodecOutput[T]`。
- 响应包装器使用 `WithResponseHeader`、`WithResponseCookie`。
- 重定向由 `RedirectResponse.Location` 携带动态目标。
- 参数约束拆分为 `NumberConstraint` 和 `StringConstraint`。
- 自动 operationId 使用规范化的 `method_resource_by_parameter` 格式。

### StructInput 移除（性能收敛）

`StructInput[T]`（结构体 tag 绑定）、嵌入 `Params` 与 `Body[T]` 惰性请求体视图、
`ParseInput`、`WithBodyDecoder`、`ErrInvalidParamsUsage`、
`ErrMultipleBodyFields`、`ErrBodyFieldMustBeValue` 已全部删除，不提供 shim。

原因：结构体 tag 绑定路径是 ghttp 每请求开销的最大单笔来源（结构体反射构造 +
急切状态构建），与"显式描述器 + 零反射"的新 API 语义重复。迁移方式：

```go
// 旧：tag 绑定
type In struct {
    ID int64 `path:"id"`
}
Handle(Get("/users/{id}"), StructInput[In](), JSONOutput[User](), h)

// 新：显式描述器 + MapInputs
Handle(Get("/users/{id}"),
    MapInputs(PathInt64("id"), func(id int64) In { return In{ID: id} }),
    JSONOutput[User](), h)
```

中间件按需读取路径参数仍使用 `Server.MatchedParams(r)`（返回 `Params` 请求视图）。
任意复杂输入使用 `InputFunc`；结构体多参数使用 `MapInputs/MapInputs3..7`。

这是 pre-v1 的破坏性删除。调用方必须整体迁移，不能与旧 builder 混用。

## License

与 [gk](https://github.com/sofiworker/gk) 项目一致。
