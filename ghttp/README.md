# ghttp

> 开发中（pre-v1.0.0），API 不承诺向后兼容，**禁止直接用于生产**。

类型化的 HTTP server：Gin 风格 radix tree 路由，handler 输入全部惰性按需访问，默认 JSON 编解码。只依赖标准库与仓库内基础包（`gerr`、`ghttp/wire`）。

```text
ghttp                  server（本包）
ghttp/wire             与 client 共享的线格式层：严格解码、media-type、SSE、日志单行化、StatusCoder
ghttp/client           HTTP 客户端（不依赖本包）
ghttp/ws               WebSocket（RFC 6455 服务端，只依赖标准库，不依赖本包）
ghttp/adapters/gotel   基于 gotel 的 tracing / metrics 中间件
```

性能数据见 [BENCHMARKS.md](BENCHMARKS.md)。

实现进度见 [SERVER_PLAN.md](SERVER_PLAN.md)。设计文档见 `docs/superpowers/specs/2026-10-03-ghttp-v3-http-server-design.md`。

## 快速开始

```go
type CreateUser struct{ Name string `json:"name"` }
type User struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

func getUser(ctx context.Context, req ghttp.RequestOf[ghttp.NoDataType]) (User, error) {
    id, err := req.PathValue("id").Int64() // 转换失败自动映射为 400
    if err != nil {
        return User{}, err
    }
    return User{ID: id}, nil
}

func createUser(ctx context.Context, req ghttp.RequestOf[CreateUser]) (ghttp.Reply[User], error) {
    in, err := req.Data(ctx) // 首次调用才解码 body；格式错误 400，超限 413，媒体类型不符 415
    if err != nil {
        return ghttp.Reply[User]{}, err
    }
    return ghttp.Reply[User]{
        Status:  http.StatusCreated,
        Headers: http.Header{"Location": {"/users/1"}},
        Body:    User{ID: 1, Name: in.Name},
    }, nil
}

func main() {
    srv := ghttp.NewServer(ghttp.WithAddr(":8080"))
    srv.Use(ghttp.Recovery(), ghttp.RequestID(), ghttp.AccessLog())

    api := srv.Group("/api/v1")
    if err := api.Register(
        ghttp.Get("/users/:id", getUser),
        ghttp.Post("/users", createUser),
    ); err != nil {
        log.Fatal(err)
    }

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()
    if err := srv.RunContext(ctx, ""); err != nil { // ctx 结束后优雅关闭
        log.Fatal(err)
    }
}
```

## 路由

| 写法 | 含义 |
|---|---|
| `/users` | 静态 |
| `/users/:id`（或 `/users/{id}`） | 单段参数 |
| `/files/*path`（或 `/files/{path...}`） | 捕获剩余路径 |

- 优先级：静态 > 参数 > catch-all。同一层不能同时注册 catch-all 和静态路由（与 Gin 一致）。
- 尾斜杠重定向：GET/HEAD 返回 301，其他方法返回 308（保留方法与 body），查询串保留，不会生成 `//host` 形式的开放重定向。
- 路径存在但方法不匹配时返回 405，并带 `Allow` 头；`OPTIONS` 自动应答 204 并带 `Allow`。
- HEAD 在没有显式路由时回退到 GET。
- 请求路径含 `.`、`..` 段或控制字符时返回 400；`WithStrictPath()` 额外拒绝空段 `//`。

### 注册入口

| 构造函数 | handler 形态 | 成功响应 |
|---|---|---|
| `Get/Post/Put/Patch/Delete/Head/Options`、`Method` | `func(ctx, RequestOf[T]) (O, error)` | 按 Output 写出（默认 JSON 200） |
| `HandleAction` | `func(ctx, RequestOf[T]) error` | 204 |
| `HandleProcedure` | `func(ctx) error` | 204 |
| `Raw` | `func(ctx, *Request, *Response) error` | handler 自行写出 |

`Register` 先校验并编译整批路由，再逐条安装：method token、路径、handler、中间件、Input/Output 类型不匹配都在注册期报错。首个请求或 `Run/Serve` 之后，注册返回 `ErrRegistrationAfterStart`，`Use`/`With` 会被忽略并告警。

### 选项与中间件顺序

```go
srv.Use(globalMW)                            // 全局：包裹整个分发，404/405/TSR 也经过
srv.With(ghttp.WithBodyLimit(1 << 20))       // 之后注册的路由与之后创建的 Group 的默认选项
g := srv.Group("/api", ghttp.WithGroupMiddleware(authMW))
v1 := g.Group("/v1").Use(v1MW)               // 子组复制创建时的快照
v1.Register(ghttp.Get("/x", h, ghttp.WithMiddleware(routeMW)))
// 执行顺序：globalMW → Server.With → authMW → v1MW → routeMW → handler
```

全局中间件执行时路由已经匹配，可用 `req.Route()` 读取路由模板（未匹配时为空）。

## 请求

- `PathValue/QueryValue/HeaderValue/CookieValue(name)` 返回 `*Value`，提供 `String/Int/Int64/Float64/Bool` 和 `...Or(default)` 方法。缺失或转换失败时返回包装了 `ErrInvalidInput` 的错误（映射为 400，错误信息含参数名）。
- `QueryValues(name)` 返回 `*Values`，提供 `Strings/IntSlice/Int64Slice/Float64Slice`。
- `req.Sources()`：原始字符串访问；`req.Request()`：取底层 `*Request`（含 `Raw *http.Request`、`Route()`、`Query()`）。
- `req.Data(ctx)`：惰性解码 body，`sync.Once` 保证只解码一次；`T` 为 `NoDataType` 时不读取 body。
- 请求体默认上限 32 MiB（`WithMaxBodyBytes`，路由级 `WithBodyLimit`，`-1` 不限），超限返回 413。

### 输入输出格式

```go
ghttp.Post("/x", h, ghttp.WithInput(ghttp.XMLInput[T]()))   // 也可用 InputFunc 自定义
ghttp.Get("/y", h, ghttp.WithOutput(ghttp.TextOutput()))     // JSONOutput / XMLOutput / OutputFunc
```

`WithInput`/`WithOutput` 只能在路由级使用；泛型类型与 handler 不一致时注册失败。

表单：`WithInput(ghttp.FormInput[T]())` 支持 urlencoded 与 multipart，按 `form` 标签映射，文件字段用 `*multipart.FileHeader`。

### 校验

```go
type Order struct{ Qty int `json:"qty"` }
func (o Order) Validate() error { ... }                  // 自动调用（也支持 Validate(ctx) error）

srv.With(ghttp.WithValidator(myValidator))               // 也可用于 Group 或单个路由
```

校验在 `Data()` 解码成功后执行（不调用 `Data()` 就不校验）。失败返回 400；validator 返回的 `HTTPError` 保留其状态码。`Server.With` 的校验器作用于所有路由，实现时应按类型分支。

## 响应

默认把返回值序列化为 JSON 200（先序列化再写出，失败时不会留下半个响应）。返回以下类型（值或指针）时按其语义写出：

| 类型 | 行为 |
|---|---|
| `Reply[T]` | 自定义 Status（默认 200）、Headers、Cookies；204/304 不写 body |
| `FileReply` | `http.ServeContent`：Range、条件请求 |
| `StreamReply` | 流式写出 |
| `RedirectReply` | 重定向（默认 302） |
| `NoContentReply` | 204 |

`*Response` 实现了 `http.ResponseWriter`、`http.Flusher`、`http.Hijacker` 和 `Unwrap`，可以交给 `http.NewResponseController` 使用。连接被接管（如 WebSocket 升级）后，handler 返回的错误只记日志，不再写响应。

## 错误映射

handler 返回 error，由统一错误处理器写出 JSON：`{"error":"...","status":N,"code":"..."}`。

| 错误 | 状态 |
|---|---:|
| 链上的 `HTTPError` | 其 `Status`（`errors.Is(err, ErrNotFound)` 按状态码匹配） |
| `*http.MaxBytesError` | 413 |
| `wire.ErrInvalidFormat`、`ErrInvalidInput` | 400 |
| `*gerr.Error`：invalid / not_found / conflict / permission / unavailable / timeout / canceled | 400 / 404 / 409 / 403 / 503 / 504 / 499 |
| `context.DeadlineExceeded` / `context.Canceled` | 504 / 499 |
| 其他 | 500 |

只有 `HTTPError.Message` 会原样返回给客户端，其他错误只返回状态文本，内部错误字符串不会泄露。`*gerr.Error` 的 `Code` 作为 `code` 返回。`StatusFromError`、`ErrorResponseOf` 可供自定义 `WithErrorHandler` 复用；中间件计算最终状态码用 `FinalStatus(resp, err)`。

与 gerr 互转：`FromGerr(err)`（Message 会返回给客户端）、`ToGerr(err)`、`GerrStatus(kind)`、`KindFromStatus(status)`。

## 日志

`Logger` 接口（`Debugf/Infof/Warnf/Errorf`，与 `ghttp/client` 相同，一个实现可两端共用），适配器 `NewSlogLogger`、`NewStdLogger`、`NopLogger`。`WithLogger` 接收框架告警，未设置 `WithErrorLog` 时也接收 net/http 的内部错误。

## 内置中间件与组件

| 能力 | API |
|---|---|
| panic 恢复 | `Recovery()`、`RecoveryWithWriter`、`RecoveryWithHandler` |
| 访问日志 | `AccessLog()`、`AccessLogWithWriter`、`AccessLogWithLogger`、`AccessLogWithConfig`（记录路由模板与客户端 IP；写入 Logger 时 5xx 用 Errorf、4xx 用 Warnf；颜色默认只在终端开启） |
| 观测钩子 | `Observe(func(RequestInfo))` |
| 请求 ID | `RequestID()`、`RequestIDFrom(ctx)` |
| 超时 | `Timeout(d)`（handler 必须尊重 ctx） |
| 真实客户端 IP | `RealIP(WithTrustedProxies(...))`、`ClientIP(req)` |
| 限流 | `RateLimit(rate, burst)`，429 + `Retry-After` |
| Basic 认证 | `BasicAuth(validate)`、`BasicAuthAccounts`、`BasicAuthUser(ctx)` |
| 安全响应头 | `SecureHeaders(WithHSTS(...), WithCSP(...))` |
| CSRF | `CSRF(...)`，基于 `http.CrossOriginProtection` |
| CORS | `NewCORS(CORSConfig{...})` / `CORS(...)` |
| Gzip | `Gzip(...)` |
| 静态文件 | `Static(prefix, fs.FS, ...)`，默认不列目录，支持预压缩文件 |
| 健康检查 | `NewHealth()`、`AddCheck`、`SetReady`、`Routes(live, ready)` |
| SSE | `NewSSEWriter(resp)`、`Send`、`Comment`、`KeepAlive` |
| 过载保护 | `MaxInFlight(n)`，503 + `Retry-After`，可选排队等待 |
| 请求体解压 | `Decompress()`（gzip/deflate，限制解压后大小，应作为全局中间件） |
| ETag | `ETag()`，GET/HEAD 的 200 响应生成弱 ETag，`If-None-Match` 命中返回 304 |
| OpenAPI | `OpenAPI(srv)`、`OpenAPIRoute(path, srv)`，基于 `Server.Routes()` 与 `WithDoc`/`WithTags`/`WithOperationID`/`WithDeprecated` |
| 路由元数据 | `Server.Routes()` |

## Server 选项

`WithAddr`、`WithReadTimeout`、`WithReadHeaderTimeout`（默认 10s）、`WithWriteTimeout`、`WithIdleTimeout`、`WithShutdownTimeout`（默认 30s）、`WithMaxHeaderBytes`、`WithMaxBodyBytes`（默认 32 MiB）、`WithStrictPath`、`WithRedirectFixedPath`（清理 `//` 与大小写不敏感匹配后重定向，默认关闭）、`WithTLSConfig`、`WithProtocols`、`WithH2C`、`WithHTTP2Config`、`WithBaseContext`、`WithLogger`、`WithErrorLog`、`WithErrorHandler`。

生命周期：`Run`/`RunTLS`/`Serve`/`ServeTLS`/`RunContext`/`ServeContext`/`Shutdown`/`Close`。`Shutdown`/`Close` 之后再启动会返回 `ErrServerClosed`。`OnShutdown(fn)` 注册关闭钩子，在途请求结束后按注册的逆序执行一次。

## 破坏性变更（相对此前的 ghttp）

- 旧 server 实现整体移除，由 v3 核心替代。
- 默认错误响应改为 JSON `ErrorResponse`，并且不再回显内部错误文本；`Recovery` 默认改走统一错误链。
- `ReadJSON`：body 超限返回 413，错误文案改变；`RequestOf` 默认解码返回 `HTTPError`（拒绝空体、未知字段、媒体类型不符）；`isJSONContentType` 已删除。
- `Value` 的错误包装 `ErrInvalidInput`（映射为 400），不再是裸字符串错误（以前映射为 500）。
- `HeaderValue` 的存在性按头是否出现判断，空值头也视为存在。
- 同一层不能同时注册 catch-all 与静态路由（`/files/*p` + `/files/public`）。
- `Allow` 头包含自动应答的 `OPTIONS`。
- `CORSConfig` 字段重新设计（`AllowOrigins []string`、`MaxAge time.Duration` 等），`*` 与 credentials 同时配置时报错。
- `GroupOption` 改为 `func(*Group)`；`Route` 改为在注册时编译。
- 删除临时的 `Context`、`HandlerFunc`、`HandlersChain` 类型。
- 访问日志中间件改名：`Logger()`/`LoggerWithWriter`/`LoggerWithConfig`/`LoggerConfig`/`LogFormatterParams` → `AccessLog()`/`AccessLogWithWriter`/`AccessLogWithConfig`/`AccessLogConfig`/`AccessLogParams`；`Logger` 现在是日志接口。默认格式记录客户端 IP，颜色默认只在终端开启。
