# ghttp

English | [中文](README.md)

A general-purpose Go HTTP framework built on Go 1.24+ generics and the standard library `net/http`, with server/client support, OpenAPI 3.1 generation, content negotiation, template rendering, WebSocket/SSE and more.

---

## Features

- **Generic-first API** — typed route chains with compile-time safety
- **Explicit input** — `Params` for path/query/header/cookie and a `Body` field for request bodies
- **Content negotiation** — `Accept`-driven response codecs and `Consumes`-constrained request types
- **Flexible output** — response structs and custom Envelope wrapping
- **Built-in routing** — a single method-first matcher with static, parameter and catch-all paths
- **OpenAPI 3.1** — inferred JSON Schema and OAS documents from route metadata

---

## Installation

```bash
go get github.com/sofiworker/gk
```

```go
import "github.com/sofiworker/gk/ghttp"
```

---

## Quick Start

### Server

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

    // 链式构建器（支持 OpenAPI 元数据）；
    // chain builder (with OpenAPI metadata).
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

### Client

```go
client := ghttp.NewClient()

// 泛型调用；
// typed generic call.
resp, err := ghttp.GET[GreetInput, GreetOutput](client, "/hello/world", nil)

// 链式调用（go-resty 风格）；
// chain call (go-resty style).
result, err := client.R().
    SetHeader("Authorization", "Bearer token").
    SetQueryParam("lang", "zh").
    Get("/hello/world")

// 结构化请求；
// structured request.
input := &GreetInput{}
resp, err := ghttp.Do[GreetInput, GreetOutput](client, "POST", "/hello", input)

// 自定义底层客户端或 Transport；
// custom underlying client or transport.
client = ghttp.NewClient(
    ghttp.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
    ghttp.WithTransport(customTransport),
)

// 流式响应由调用方关闭 RawBody；
// streaming responses are closed by the caller via RawBody.
streamResp, err := client.R().SetStreamResponse(true).Get("/download")
if err != nil { return err }
defer streamResp.RawBody().Close()
_, err = io.Copy(dst, streamResp.RawBody())
```

Client capabilities (inspired by go-resty / imroc/req):

- **Retry** — client/request level with default transport-error or >= 500 conditions.
- **Hooks** — before/after request lifecycle hooks at client and request level.
- **Error binding** — automatic non-2xx body binding via `SetError`.
- **Auth** — token/basic auth actually sent on the wire.
- **Output** — write response bodies to files.
- **Query** — params, values and raw query strings.
- **Timeout/context** — per-request timeout and context.
- **Cookie**：`client.SetCookie(s)` / `client.R().SetCookies(...)`。
- **Cookies** — client and request level.
- **Response accessors** — Time/ReceivedAt/Size/Cookies/Unmarshal/Error/Result.
- **Debug** — request summaries via logger.
- **Go 1.27+ typed methods** — `client.Get/Post/Put/Delete[...]`.

---

## API Overview

### Route Registration

| Function | Description |
|------|------|
| `Route[Req,Resp](target)` | Chain start (`*Server` or `*Group`); deprecated shim on Go 1.27+. |
| `.GET(path)` | Register a GET route. |
| `.POST(path)` | Register a POST route. |
| `.PUT(path)` | Register a PUT route. |
| `.DELETE(path)` | Register a DELETE route. |
| `.PATCH(path)` | Register a PATCH route. |
| `.ANY(path)` | Register all standard methods. |
| `.CUSTOM(method, path)` | Register a custom method token. |

### Chain Builder Methods

| Method | Description |
|------|------|
| `.GET(path)` | Set GET method and path. |
| `.POST(path)` | Set POST method and path. |
| `.PUT(path)` | Set PUT method and path. |
| `.DELETE(path)` | Set DELETE method and path. |
| `.PATCH(path)` | Set PATCH method and path. |
| `.ANY(path)` | Set all standard methods and path. |
| `.CUSTOM(method, path)` | Set a custom method and path. |
| `.Doc(ghttp.Summary("..."), ghttp.Tags("..."))` | Operation documentation. |
| `.Consumes(contentTypes...)` | Declare accepted request Content-Types. |
| `.MaxBodyBytes(n)` | Override body size limit; <= 0 disables it. |
| `.Produces(contentTypes...)` | Declare response Content-Types. |
| `.Group(prefix, mws...)` | Branch into a sub-group; call before setting method/path. |

### Terminal Methods

| Method | Description |
|------|------|
| `.To(handler)` | Register a typed handler; setup errors panic. |
| `.ToNoInput(handler)` | Register a handler with no request input. |
| `.ToNoOutput(handler)` | Register an error-only handler; 204 by default. |
| `.ToHTTP(handler)` / `.ToRaw(handler)` | Raw escape hatches. |
| `.ToHTTPFunc(handler)` | Parsed-input handler writing its own response. |
| `.ToRedirect(code, location)` / `.ToRedirectFunc(...)` | Redirects. |
| `.ToSSE` / `.ToWebSocket` / `.ToStatic*` / `.ToHTML` | Specialized terminals. |

> No-input/no-output are explicit terminals, never `struct{}` magic: `.ToNoOutput` defaults to 204; `.ToNoInput` skips request parsing entirely.

### Go 1.27 Generic Methods

Both APIs ship in the same module and are selected automatically by toolchain version (like stdlib `//go:build` constraints); users pass no build tags:

- Go < 1.27: package-level type parameters.
- Go >= 1.27: generic terminal methods inferred from handlers; Server/Group act as root groups; no `.Route()` method; `Route[Req,Resp]` remains a deprecated shim.

```go
// Go 1.27+ 写法；
// Go 1.27+ style.
s.GET("/hello/{name}").Doc(ghttp.Summary("问候")).
    To(func(ctx context.Context, req *GreetInput) (*GreetOutput, error) {
    return &GreetOutput{Message: "Hello, " + req.Path("name")}, nil
})

// 分组与 Server 一致；
// groups behave like the server.
api := s.Group("/api")
api.GET("/users/{id}").To(func(ctx context.Context, req *GetUserReq) (*GetUserResp, error) {
    return &GetUserResp{ID: req.ID}, nil
})
api.POST("/users").Status(http.StatusCreated).To(func(ctx context.Context, req *CreateUserReq) (*CreateUserResp, error) {
    return &CreateUserResp{ID: "u-1"}, nil
})

// 显式起点：自定义方法或全方法路由直接链式；
// explicit starts: custom or all-method chains.
s.ANY("/health").ToNoInput(func(ctx context.Context) (*HealthResp, error) { ... })
s.CUSTOM("PURGE", "/cache").ToNoOutput(func(ctx context.Context, req *PurgeReq) error { ... })
```

Both APIs share one implementation (`routeBuilderCore`). Note: Go 1.27-only files use generic-method syntax, so `go fmt`/`gofmt` must be Go 1.27+.

### Route Parameters

Route paths use stdlib/OpenAPI-compatible `{param}` syntax:

- `{param}` — named parameter
- `{path...}` — wildcard (matches the remaining path)

```go
ghttp.Route[Req, Resp](s).GET("/users/{id}").To(handler)      // 命名参数；named parameter.
ghttp.Route[Req, Resp](s).GET("/files/{path...}").To(handler)  // 通配符；wildcard.
```

Legacy `:param` and `*path` syntax is removed; use `{param}` and `{path...}`.

### Input Structs

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

Request media types are declared with `Consumes` (matching `Content-Type`); response media types use `Produces` (matching response `Content-Type` and client `Accept`):

```go
ghttp.Route[CreateUserInput, UserOutput](s).
    POST("/users/{id}").
    Consumes(ghttp.MIMEJSON, ghttp.MIMEXML).
    Produces(ghttp.MIMEJSON, ghttp.MIMEXML).
    To(createUser)
```

`Consumes` only constrains routes with a `Body`. When configured, missing or mismatched `Content-Type` returns 415; without it, missing `Content-Type` defaults to JSON.

`application/x-www-form-urlencoded` forms bind via explicit `form:"name"` tags; a bindable struct without any `form` tag returns 400 instead of silently binding nothing.

### Defaults at a Glance

| Scenario | Default | Override |
|------|----------|----------|
| Explicit `Accept` with no match | `406 Not Acceptable` | `WithLenientContentNegotiation()` falls back to the first `Produces` |
| Empty or `*/*` `Accept` | first `Produces` | — |
| Unknown explicit request `Content-Type` | `415 Unsupported Media Type` | `WithLenientContentType()` decodes as JSON |
| Missing `Content-Type` with `Consumes` configured | `415 Unsupported Media Type` | JSON without `Consumes` |
| Codec cannot decode the target type | `400 Bad Request` | — |
| Error body | `{code,message}` | `WithProblemDetails()` / `.ProblemDetails()` use RFC 9457; `WithErrorWriter` / `.ErrorWriter` fully custom |

`WithStrictContentNegotiation()` / `WithStrictContentType()` remain as no-op compatibility aliases.

`Params` is a lazy view of request inputs: query/cookies/client IP parse on first access and are cached, headers read through; it never holds a `ResponseWriter`.

The view is valid for the handler lifetime and must be used from a single goroutine; call `Detach()` to retain it beyond the handler or share it across goroutines:

```go
func handler(ctx context.Context, p ghttp.Params) (Out, error) {
    snapshot := p.Detach() // 深拷贝，安全跨 goroutine / 超生命周期使用；deep copy, safe across goroutines/lifetime.
    go audit(snapshot)
    return Out{}, nil
}
```

When only path/query/header/cookie inputs are needed, use the value type `ghttp.Params` directly as the input:

```go
ghttp.Route[ghttp.Params, UserOutput](s).
    GET("/users/{id}").
    To(func(ctx context.Context, params ghttp.Params) (UserOutput, error) {
    return UserOutput{
            ID: params.Path("id"),
    }, nil
    })
```

For request bodies, embed `Params` anonymously:

```go
type Input struct {
    ghttp.Params `json:"-"`
    Body struct {
    Name string `json:"name"`
    } `json:"body"`
}
```

`Route[*ghttp.Params, Resp]`, named `Params` fields, pointer embeddings and indirect embeddings are setup errors: the terminal call panics with `ErrInvalidParamsUsage`.

### Output

```go
type UserOutput struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}
```

When the response implements `Cookies() []*http.Cookie`, the framework writes `Set-Cookie` headers before the response:

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

Default response format:
```json
{
    "code": 0,
    "msg": "success",
    "data": { "id": 1, "name": "Alice", "email": "alice@example.com" }
}
```

Error response:
```json
{
    "code": 40001,
    "msg": "invalid parameter: name is required"
}
```

Customize the wrapping format with `WithEnvelope(fn)`.

`EnvelopeFunc` must not change the HTTP status code. Error responses return only status text by default; `WithExposeErrorDetails()` enables internal messages.

`WithProblemDetails()` switches errors to RFC 9457 `application/problem+json`; the explicit `WithErrorHandler` has the highest priority.

Error models are per-route: `.ProblemDetails()` affects only that route; `.ErrorWriter(fn)` installs a route-level writer. `ErrorWriter` returns true when handled, false to fall through, so `ChainErrorWriters` composes them:

```go
ghttp.Route[Req, Resp](s).GET("/users/{id}").
    ProblemDetails().
    To(handler)

s := ghttp.New(ghttp.WithErrorWriter(ghttp.ChainErrorWriters(
    logErrorWriter,            // 返回 false，继续；returns false, falls through.
    problemWriter,             // 返回 true，结束；returns true, stops.
)))
```

Typed responses can declare status/headers via `StatusCode()`/`WriteResponseHeaders` or builder `.Status()`/`.ResponseHeader()`. 204/304/1xx skip the body automatically.

### Middleware

```go
// 注入结构化 logger，glog.Default() 可直接满足 ghttp.Logger；
// inject a structured logger; glog.Default() satisfies ghttp.Logger.
s := ghttp.New(ghttp.WithLogger(glog.Default()))
// 或使用标准库 slog 适配；
// or use the standard library slog adapter.

s.Use(ghttp.RequestID())
s.Use(ghttp.CORS(ghttp.CORSConfig{
    AllowOrigins: []string{"*"},
}))
s.Use(ghttp.RequestLogger())
s.Use(ghttp.Recoverer())
s.Use(ghttp.Timeout(5 * time.Second))

// 分组路由添加中间件；
// add middleware to a group.
group := s.Group("/api")
group.Use(authMiddleware)

// 中间件中读取已匹配的路径参数（显式访问器，默认不注入 context）；
// read matched path params in middleware (explicit accessor).
s.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    id := s.MatchedParams(r).Path("id")
    _ = id
    next.ServeHTTP(w, r)
    })
})

// RBAC 能力：Authorizer 接口 + 默认 RBAC 实现；
// RBAC capability: Authorizer interface + default implementation.
s.Use(ghttp.RBACMiddleware(authz,
    func(r *http.Request) string { return r.Header.Get("X-User") },
    func(r *http.Request) string { return "users:read" },
    func(r *http.Request) string { return r.URL.Path },
))
```

- `RequestID` echoes incoming IDs up to 128 chars; longer values are replaced.
- `CORS` short-circuits only true preflights; ordinary OPTIONS reach the router.
- `Timeout` returns 504, cancels the context and discards late writes; its writer supports Hijack/Flush for WS/SSE.

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

WebSocket defaults to same-origin checks (missing Origin is allowed); override globally with `WithWebSocketOriginChecker` or per-route with `.WebSocketCheckOrigin`.

`WebSocketConn` provides:


Server WebSocket options (disabled by default, opt-in):

```go
s := ghttp.New(
    ghttp.WithServerWebSocketSubprotocols([]string{"chat", "json"}), // 握手子协议；handshake subprotocols.
    ghttp.WithServerWebSocketReadBufferSize(4096),
    ghttp.WithServerWebSocketWriteBufferSize(4096),
    ghttp.WithServerWebSocketPingPeriod(30*time.Second), // keepalive ping
    ghttp.WithServerWebSocketPongWait(60*time.Second),   // pong 等待上限；pong wait cap.
)
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

// SSEWriter 还提供 WriteJSON / WriteJSONWithID / WriteEventWithID / WriteComment / Retry；
// SSEWriter also offers WriteJSON/WriteJSONWithID/WriteEventWithID/WriteComment/Retry.
// data 含换行时会按规范拆成多行 data: 字段。
// newlines in data are split into multiple data: lines per spec.

stream, err := client.SSE("/events", ghttp.SSEConfig{
    Reconnect:     true,
    RetryInterval: time.Second,
    MaxRetries:    3,
    LastEventID:   "optional-last-id",
})
if err != nil { return err }
defer stream.Close()
```

### Static Files

```go
// 使用服务器级 VFS 根目录：/a.txt 会解析到 /srv/files/a.txt；
// uses the server-level VFS root: /a.txt resolves to /srv/files/a.txt.
s := ghttp.New(ghttp.WithVFSPath("/srv/files"))
ghttp.Route[struct{}, struct{}](s).GET("/").ToStatic()

// 也可以为单条静态路由显式指定根目录；
// or set an explicit root for a single static route.
ghttp.Route[struct{}, struct{}](s).GET("/static").ToStatic("./public")
ghttp.Route[struct{}, struct{}](s).GET("/assets").ToStaticFS(http.FS(embeddedAssets))
ghttp.Route[struct{}, struct{}](s).GET("/favicon.ico").ToStaticFile("./favicon.ico")
```

`ToStatic` uses a safe VFS: all request paths resolve relatively beneath the static root.
Traversal via `../`, URL-encoded paths and Windows backslashes is rejected.

### Template Rendering

```go
renderer := ghttp.NewRenderer("./templates/*.html")
s = ghttp.New(ghttp.WithRenderer(renderer))

// 处理函数中；
// inside the handler.
ghttp.Route[NoInput, NoOutput](s).GET("/page").Produces(ghttp.MIMEJSON, ghttp.MIMEXML).To(func(ctx context.Context, req NoInput) (NoOutput, error) {
    return NoOutput{}, nil
})
```

### OpenAPI Documentation

Enable optional best-effort docs with WithOpenAPI. `Server.OpenAPI` generates JSON from the current route snapshot without freezing the Server; the default endpoint is /openapi.json.

```go
document, err := s.OpenAPI()
```

`WithOpenAPIServers` and `WithOpenAPISecurity` declare document-level servers/security; envelope mode generates the `{code,msg,data}` schema automatically.

### Validation

```go
import "github.com/sofiworker/gk/ghttp"

type validator struct{}

func (v *validator) Validate(i any) error {
    // 自定义验证逻辑；
    // custom validation logic.
    return nil
}

s = ghttp.New(ghttp.WithValidator(&validator{}))
```

The server-level validator is **disabled by default**; enable it with `WithValidator(v)` or restore struct-tag checks with `NewDefaultValidator()`. Route-level `.Validate`/`.SkipValidation` are unaffected.

---

## Routing Engine

The server uses a single method-first matcher; routes are registered only through the builder chain.

The route table freezes at the first serve. Trailing slashes are insensitive by default (strict via WithStrictRouting); OPTIONS is not automatic; HEAD falls back to GET with the body suppressed.

Only `{param}` and `{path...}` are supported; paths are not cleaned or redirected, and invalid escapes return 400.

Middleware order is fixed; group middleware snapshots at child creation (gin-like); server middleware also covers 400/404/405 and the OpenAPI endpoint.

Typed handlers support explicit status/headers; raw terminals get the raw request, and middleware reads path params via `Server.MatchedParams(r)`.

---

## Configuration Options

```go
s := ghttp.New(
    ghttp.WithAddress(":8080"),                              // 监听地址；listen address.
    ghttp.WithValidator(myValidator),                        // 验证器；validator.
    ghttp.WithEnvelope(myEnvelope),                          // Envelope 函数；envelope function.
    ghttp.WithConsumes(ghttp.MIMEJSON),                      // 默认请求 Content-Type；default request Content-Type.
server 级 validator **默认关闭**：`New()` 不自动安装任何 validator，需要 `WithValidator(v)` 显式启用（破坏性变更）；`ghttp.NewDefaultValidator()` 可恢复内置 struct-tag 校验。路由级 `.Validate(fn)` 与 `.SkipValidation()` 不受影响。

    ghttp.WithBodyDecoder(func(r io.Reader, contentType string, target interface{}) error { // 自定义 Body 解码；custom body decoder.
    return customDecoder.Decode(r, target)
    }),
    ghttp.WithRenderer(myRenderer),                          // 模板渲染器；template renderer.
    ghttp.WithReadHeaderTimeout(5*time.Second),              // 请求头读取超时；read header timeout.
    ghttp.WithReadTimeout(30*time.Second),                   // 读取超时；read timeout.
    ghttp.WithWriteTimeout(30*time.Second),                  // 写入超时；write timeout.
    ghttp.WithIdleTimeout(60*time.Second),                   // keep-alive 空闲超时；idle timeout.
    ghttp.WithMaxHeaderBytes(1<<20),                         // 最大请求头；max header bytes.
    ghttp.WithMaxBodyBytes(ghttp.DefaultMaxBodyBytes),        // 自动解析请求体大小上限，默认 4 MiB；max decoded body, default 4 MiB.
    ghttp.WithTLSConfig(tlsConfig),                          // TLS 配置；TLS config.
    ghttp.WithBaseContext(baseContext),                      // 底层 Server BaseContext；underlying BaseContext.
    ghttp.WithConnContext(connContext),                      // 连接级 Context；per-connection context.
    ghttp.WithErrorLog(errorLog),                            // 底层 Server 错误日志；underlying server error log.
)
```

```go
ln, err := net.Listen("tcp", "127.0.0.1:0")
if err != nil {
    return err
}
go s.Serve(ln)
fmt.Println(s.Addr()) // 包含 :0 自动分配后的真实端口；includes the real port allocated for :0.

err = s.ListenAndServeTLS(":8443", "server.crt", "server.key")
```

## Project Structure

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

### Documentation and Verification

Repository documentation and verification notes live under `docs/`; see [docs/language-sweep.md](docs/language-sweep.md) and `docs/superpowers/` for design and plan documents.



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
