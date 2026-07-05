# ghttp Server 使用者视角锐评（第二轮）

> 日期：2026-07-04
> 方法：创建 `example/ghttp_usage` 示例包，从零编写一个完整的博客 API 服务器（CRUD + SSE + Cookie + CORS + OpenAPI + 验证器 + 静态文件），通过实际编码和测试发现真实的设计缺陷和易用性问题。
> 说明：已知的 stub（WebSocket 等）不在评价范围内。前一轮 `ghttp-improvement.md` 已记录的问题若已修复则不再重复，若未修复则标注"仍未修复"。

---

## 一、实际使用中确认的问题（附测试证据）

### 1.1 错误响应格式与成功响应不一致 —— 仍未修复，实测确认

**现象**：无 `WithEnvelope` 时，成功响应是 `application/json`，但所有错误响应（404、422、500）都是 `text/plain; charset=utf-8`。

**实测证据**（来自 `example/ghttp_usage/main_test.go`）：

```
TestGetPost:
  【发现问题】无 envelope 时错误响应 Content-Type = "text/plain; charset=utf-8"
  错误响应体: post not found: 999

TestValidation:
  【发现问题】验证错误响应 Content-Type = "text/plain; charset=utf-8"
  Validation error body: Key: 'CreatePostInput.Body.Content' Error:Field validation for 'Content' failed on the 'required' tag

TestRecoverer:
  【发现问题】Recoverer 错误响应 Content-Type = "text/plain; charset=utf-8"
  Recoverer error body: Internal Server Error
```

**使用者痛点**：客户端必须写两套解析逻辑——成功走 JSON 解码，失败走纯文本。一个声明了 `Produces(application/json)` 的 API，失败时却返回 text/plain，这是 API 一致性的严重缺陷。

**根因**：`writeError` 在无 envelope 时落到 `http.Error(w, err.Error(), code)`（builder.go:661），完全绕过了 codec。

**建议**：无 envelope 时也应通过 codec 输出结构化错误体 `{"code":404,"message":"post not found: 999"}`，Content-Type 设为路由的 `produces`。

---

### 1.2 405 响应缺少 Allow header —— 实测确认

**现象**：对已注册路径发未注册的方法时，返回 405 但无 `Allow` header。

**实测证据**：
```
TestMethodNotAllowed:
  【发现问题】405 响应缺少 Allow header
```

**使用者痛点**：HTTP 规范要求 405 响应必须包含 `Allow` header，告知客户端该路径支持哪些方法。缺失 `Allow` 使客户端无法自动适配可用方法，违反 RFC 9110 §15.5.5。

**根因**：`RadixRouter.ServeHTTP` 在 method 不匹配时直接 `w.WriteHeader(405)` 返回（radix_router.go:123-125），不检查其他方法是否注册了该路径，也不设置 `Allow`。

**建议**：维护一个 path → methods 的反向索引，405 时列出所有已注册方法。

---

### 1.3 `Responds(code)` 纯文档，不影响实际状态码 —— 实测确认

**现象**：`Responds(201).With(output{}).Desc("created")` 声明了 201，但实际返回 200。

**实测证据**：
```
TestStatusCodeFromResponse:
  【发现问题】声明了 Responds(201) 但实际返回 200，因为响应结构体没有 Status 字段也未实现 StatusCoder
```

**使用者痛点**：`Responds(201)` 让使用者以为框架会自动返回 201，但实际上它只影响 OpenAPI 文档。要返回 201，使用者必须额外实现 `StatusCoder` 接口或添加 `Status int` 字段。这两套机制（文档声明 vs 实际行为）之间没有关联，是认知陷阱。

**根因**：`Responds` 只写入 `responseSpec`（用于 OpenAPI），而 `resolveStatusCode` 只看 `StatusCoder` 接口和 `Status` 字段。两者完全独立。

**建议**：考虑让 `Responds(code)` 的 code 成为默认状态码（当响应未实现 `StatusCoder` 且无 `Status` 字段时使用），或至少在文档中明确说明 `Responds` 纯文档用途。

---

### 1.4 CORS 缺少 Vary: Origin —— 实测确认

**现象**：CORS 响应不设置 `Vary: Origin` header。

**实测证据**：
```
TestCORS:
  【发现问题】CORS 响应缺少 Vary: Origin header（缓存投毒风险）
```

**使用者痛点**：没有 `Vary: Origin`，CDN/代理可能缓存一个 Origin 的 CORS 响应并返回给另一个 Origin，导致 CORS 配置失效或跨站数据泄漏。

---

### 1.5 path/query 参数全部是 string，缺少类型化便捷方法

**现象**：`Params.Query("page")` 和 `Params.Path("id")` 只返回 `string`，使用者需要手动 `strconv.Atoi`。

**实测代码**（来自 `main.go` 的 `ListPosts` handler）：
```go
// 使用者必须手写这样的样板代码
page := 1
if p := req.Query("page"); p != "" {
    var n int
    if _, err := fmt.Sscanf(p, "%d", &n); err == nil && n > 0 {
        page = n
    }
}
```

**使用者痛点**：每个需要整数/布尔 query 参数的 handler 都要重复写解析+默认值+错误处理逻辑。gin/echo/chi 都有 `c.QueryInt("page", 1)` 之类的便捷方法。

**建议**：在 `Params` 上增加少量类型化方法：
```go
func (p Params) QueryInt(key string) (int, error)
func (p Params) QueryIntDefault(key string, def int) int
func (p Params) QueryBool(key string) (bool, error)
func (p Params) QueryBoolDefault(key string, def bool) bool
// Path 同理
```

---

## 二、编码过程中发现的设计缺陷

### 2.1 `Reads()` 方法命名严重误导

**问题**：`Reads(input Req)` 的名字暗示"声明输入读取方式"，让使用者以为必须调用它才能解析请求体。但实际上它**只用于 OpenAPI 文档生成**——真正的输入解析完全由泛型类型参数 `Req` 驱动。

**证据**：`To()` 调用 `buildHandler()` 使用 `b.input.newTarget()`（来自 `compileInput[Req]()`），完全不依赖 `b.reqType`。`b.reqType` 仅在 `register()` → `addRouteSpec()` → `OpenAPI.AddRoute()` 中使用。

**使用者痛点**：我在编写示例时一度困惑是否需要调用 `Reads()`——不调用时输入解析正常，但 OpenAPI 文档缺少 request body schema。调用时需要传入一个 `Req` 类型的零值，仅用于反射获取类型信息，而类型信息已经存在于泛型参数中。

**建议**：
1. 用 `reflect.TypeFor[Req]()` 自动获取类型，去掉 `Reads()` 方法
2. 或将 `Reads()` 重命名为 `RequestBodySchema()` / `DocReads()` 明确其文档用途

---

### 2.2 SSE/WebSocket/Static/HTML 路由需要无意义的泛型类型参数

**问题**：`Route[Req, Resp]` 要求两个类型参数，但 `ToSSE`、`ToWebSocket`、`ToStatic`、`ToHTML` 等方法完全不使用 `Req` 和 `Resp`。

**示例**：
```go
// SSE handler 签名是 func(ctx, Params, *SSEWriter) error，与 Req/Resp 无关
ghttp.Route[ghttp.Params, struct{}](v1).GET("/events").ToSSE(func(ctx, params, stream) error { ... })

// Static 完全不需要类型参数
ghttp.Route[struct{}, struct{}](s).GET("/static").ToStatic("./public")
```

**使用者痛点**：类型参数是噪音——不携带任何信息，只是满足编译器的要求。SSE 的 handler 签名固定为 `SSEHandler = func(ctx, Params, *SSEWriter) error`，不使用 `Req`/`Resp`。

**建议**：为这些特殊路由提供不需要类型参数的独立方法：
```go
s.SSE("/events", handler)
s.Static("/static", "./public")
s.HTML("/page", status, name, data)
```

---

### 2.3 `PathSchema`/`QuerySchema` 与 `Params` 读取重复

**问题**：`Params` 通过 `req.Path("id")`、`req.Query("page")` 命令式读取参数，但 OpenAPI 文档需要通过 `PathSchema(input)`/`QuerySchema(input)` 单独声明参数类型。同一个参数定义了两遍。

**示例**：
```go
// handler 中读取 path 参数
id := req.Path("id")

// OpenAPI 中又要声明一遍
type pathParams struct {
    ID string `path:"id" doc:"帖子ID"`
}
ghttp.Route[Req, Resp](s).GET("/posts/{id}").PathSchema(pathParams{})
```

**使用者痛点**：参数名、类型、描述在代码中出现两次，维护时容易不同步。

**根因**：`Params` 是命令式 API，没有 struct tag 绑定，框架无法从输入结构体自动推导参数文档。

---

### 2.4 Group 中间件的时序陷阱

**问题**：`Group.Use()` 在路由注册后调用不会生效。

**根因**：`Group.handleRoute()` 在 `To()` 时读取 `g.middlewares` 并组装成最终 handler chain。之后调用 `Group.Use()` 只修改了 `g.middlewares` slice，但已注册的路由不会重新构建 chain。

**使用者痛点**：
```go
api := s.Group("/api")
// 此时注册路由 — 中间件尚未添加
ghttp.Route[Req, Resp](api).GET("/users").To(handler)
// 添加中间件 — 不生效！
api.Use(authMiddleware) // 太晚了
```

**建议**：在文档中明确强调"中间件必须在路由注册前添加"，或者改为在 `Run()`/`ServeHTTP()` 时统一组装中间件链。

---

### 2.5 无公开方法获取已注册路由列表

**问题**：`Server` 和 `Router` 接口都没有提供获取已注册路由的方法。

**使用者痛点**：无法在启动时打印路由表用于调试，无法实现路由健康检查，无法自动化测试路由覆盖率。

**建议**：在 `Router` 接口或 `Server` 上增加 `Routes() []RouteInfo` 方法。

---

### 2.6 `normalizeRoutePath` 使用 `log.Printf` 而非框架 Logger

**问题**：路径参数语法警告使用标准库 `log.Printf`，而非框架的 `Logger` 接口。

```go
// util.go:75
log.Printf("[ghttp] WARN route path %q uses deprecated :param syntax; use {param} syntax instead", path)
```

**使用者痛点**：配置了结构化 logger（如 zap）的用户仍然会在 stderr 看到非结构化的警告日志，日志收集系统无法统一处理。

---

### 2.7 无内置优雅关停信号处理

**问题**：`Server.Run()` 是阻塞调用，没有内置信号监听和优雅关停。使用者需要自己写信号监听 + `Shutdown(ctx)` 的样板代码。

**使用者痛点**：每个使用 ghttp 的项目都要重复写这段代码：
```go
go func() {
    if err := s.Run(); err != nil { ... }
}()
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
s.Shutdown(ctx)
```

**建议**：提供 `Server.RunGracefully(ctx)` 或 `WithGracefulShutdown()` 选项。

---

### 2.8 `DefaultEnvelope` 未默认启用，与 README 示例矛盾

**问题**：README 展示了"默认响应格式 `{code, msg, data}`"，但实际不调用 `WithEnvelope(ghttp.DefaultEnvelope)` 时返回裸 JSON。

**使用者痛点**：使用者按照 README 期望默认有 envelope 包装，但实际没有。客户端 `Do[Req,Resp]` 硬编码按 envelope 解包，与不配 envelope 的服务端不兼容。

---

## 三、路由引擎细节问题

### 3.1 RadixRouter 不检测路由冲突

**问题**：同一 method + path 重复注册时静默覆盖前一个 handler，不报错也不警告。

**使用者痛点**：路由拼写错误或复制粘贴导致的重复注册不会被发现，运行时表现为"后注册的 handler 生效"，极难排查。

**建议**：`Register` 时检测冲突，返回 error 或 panic。

---

### 3.2 trailing slash 强制等价

**问题**：`RadixRouter.ServeHTTP` 中 `path = strings.TrimRight(path, "/")`，导致 `/posts/` 和 `/posts` 无条件等价。

**使用者痛点**：某些 API 设计需要区分 `/posts/`（列表）和 `/posts`（不同语义），或需要 trailing slash redirect（SEO 场景）。当前行为不可配置。

---

### 3.3 `ANY` 包含 CONNECT/TRACE

**问题**：`ANY` 注册所有 9 个标准 HTTP 方法，包括 CONNECT 和 TRACE。

**使用者痛点**：TRACE 有 Cross-Site Tracing (XST) 安全隐患，默认注册它是在给用户挖坑。CONNECT 用于代理隧道，在 API 服务器上无意义。

**建议**：`ANY` 默认排除 CONNECT/TRACE（只注册 GET/HEAD/POST/PUT/PATCH/DELETE/OPTIONS），另提供 `ALL()` 包含全部。

---

### 3.4 GET 路由不自动响应 HEAD

**问题**：注册 GET 路由后，HEAD 请求返回 405。

**使用者痛点**：HTTP 语义约定 HEAD 应返回与 GET 相同的 header 但无 body。使用者需要手动注册 HEAD 路由，违反 DRY 原则。

---

## 四、中间件细节问题

### 4.1 CORS origin 不匹配时仍下发 Allow-Methods/Headers

**问题**：`CORS` 中间件在 origin 不匹配任何 `AllowOrigins` 时，仍然设置 `Access-Control-Allow-Methods` 和 `Access-Control-Allow-Headers`。

**根因**（middleware.go:78-108）：origin 检查和 header 设置是独立的 if 块，不是 if-else 关系。

**使用者痛点**：origin 不匹配时不应下发任何 CORS 头，否则可能泄露 API 支持的方法和头信息。

---

### 4.2 `Recoverer` 不记录堆栈

**问题**：panic 恢复只记录 panic 值（`"panic", rec`），不记录调用堆栈。

**使用者痛点**：线上 panic 只能看到 `"panic recovered" panic="nil pointer dereference"`，无法定位是哪行代码导致的 panic。

**建议**：在 Error 级别日志中附加 `debug.Stack()` 输出。

---

### 4.3 `Timeout` 不写 504 响应

**问题**：`Timeout` 中间件只给 request context 设置超时，不主动写 504 响应。

**使用者痛点**：handler 不主动检查 `ctx.Err()` 时，超时后 handler 仍然继续执行，只是最终写入可能被 `http.Server` 的 `WriteTimeout` 截断。使用者得不到明确的 504 反馈。

---

## 五、OpenAPI 生成问题

### 5.1 wildcard 路径在 OpenAPI 中丢失语义

**问题**：`*wildcard` 和 `:param` 在 OpenAPI 中都被转换为 `{param}`，无法区分。

**根因**（openapi.go:137-147）：
```go
if strings.HasPrefix(part, ":") {
    parts[i] = "{" + part[1:] + "}"
} else if strings.HasPrefix(part, "*") {
    parts[i] = "{" + part[1:] + "}"  // 丢失了 ... 通配符语义
}
```

**使用者痛点**：`/files/{path...}` 在 OpenAPI 中变成 `/files/{path}`，客户端生成的代码会按单段参数处理，而非多段通配符。

---

### 5.2 `ToStatic` 修改路径影响 OpenAPI

**问题**：`ToStaticFS` 将路径从 `/static` 修改为 `/static/*path`，OpenAPI 中显示的是修改后的路径。

**使用者痛点**：OpenAPI 文档中出现的 `/static/*path` 不是标准 OpenAPI 路径语法，可能导致工具链不兼容。

---

### 5.3 OpenAPI 端点路径不可配置

**问题**：`finalizeRoutes()` 硬编码 OpenAPI 端点为 `/openapi.json`，使用者无法自定义。

**使用者痛点**：如果使用者的 API 已经有 `/openapi.json` 路径，或者想用 `/docs/openapi.json`、`/swagger.json` 等路径，无法配置。

---

## 六、性能相关

### 6.1 `ServeHTTP` 路径每请求重建 handler chain

**问题**：`Server.ServeHTTP()` 每次调用都会 `buildServerHandler()` → `buildHandlerChain()` → `Wrap()`，重新构建中间件链。

**根因**（server.go:80-83）：
```go
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    s.finalizeRoutes()
    s.buildServerHandler().ServeHTTP(w, r) // 每请求重建
}
```

**使用者痛点**：当 Server 作为子 handler 嵌入到其他 mux 时，每个请求都要重新构建 handler chain，产生不必要的分配。`Run()` 路径没有此问题（handler chain 在 `prepareHTTPServer` 中构建一次）。

**建议**：缓存 handler chain，仅在中间件列表变化时重建。

---

### 6.2 `joinStrings` 和 `itoa` 手写而非用标准库

**问题**：`middleware.go` 中 `joinStrings` 用 `+=` 拼接字符串，`itoa` 手写整数转字符串。

**使用者痛点**：性能不如 `strings.Join` 和 `strconv.Itoa`，且增加了维护成本和 bug 风险。虽然不是热点路径，但作为 2026 年的框架不应有这种代码。

---

## 七、覆盖使用场景不足

### 7.1 无压缩中间件集成

**问题**：项目有 `gcompress` 包，但 `ghttp` 没有提供压缩中间件。

**使用者痛点**：使用者需要自己写 gzip 中间件或引入第三方库，无法利用项目内的 `gcompress`。

---

### 7.2 无限流中间件

**问题**：框架不提供限流中间件。

**使用者痛点**：API 服务器基本都需要限流，使用者需要自己实现或引入第三方库。

---

### 7.3 无 tracing 集成

**问题**：项目有 `gotel` 包，但 `ghttp` 没有提供 OpenTelemetry tracing 中间件。

**使用者痛点**：需要手动从 request context 创建 span，手动注入 trace ID 到响应头。

---

### 7.4 无请求日志格式定制

**问题**：`RequestLogger()` 中间件的日志格式固定（method, path, status, size, duration），不可定制。

**使用者痛点**：不同的日志收集系统需要不同的字段名和格式（如 JSON vs logfmt），使用者无法自定义日志格式。

---

### 7.5 无 Swagger UI / ReDoc 挂载

**问题**：OpenAPI JSON 端点有了，但没有内置的 Swagger UI 或 ReDoc 挂载。

**使用者痛点**：使用者需要自己搭建 Swagger UI 服务或引入第三方中间件。

---

### 7.6 无 h2c（HTTP/2 cleartext）支持

**问题**：`Run()` 只支持 HTTP/1.1 和 HTTP/2 over TLS，不支持 h2c。

**使用者痛点**：在内部网络中使用 h2c 可以获得 HTTP/2 的多路复用能力而无需 TLS 证书，但当前框架不支持。

---

### 7.7 静态文件缺少 ETag / Cache-Control

**问题**：`ToStatic` 使用标准库 `http.FileServer`，不额外设置 ETag 或 Cache-Control header。

**使用者痛点**：无法配置缓存策略，每次请求都重新传输文件内容。

---

## 八、问题汇总表

| # | 问题 | 严重度 | 类别 |
|---|------|--------|------|
| 1.1 | 错误响应 text/plain 而非 JSON | 高 | 错误处理 |
| 1.2 | 405 缺少 Allow header | 中 | 路由语义 |
| 1.3 | Responds(code) 不影响实际状态码 | 中 | API 设计 |
| 1.4 | CORS 缺少 Vary: Origin | 中 | 中间件 |
| 1.5 | 缺少类型化 query/path 方法 | 中 | API 易用性 |
| 2.1 | Reads() 命名误导 | 中 | API 设计 |
| 2.2 | 特殊路由需要无意义泛型参数 | 中 | API 设计 |
| 2.3 | PathSchema/QuerySchema 与 Params 重复 | 中 | API 设计 |
| 2.4 | Group 中间件时序陷阱 | 中 | API 易用性 |
| 2.5 | 无路由列表查询 | 低 | 可观测性 |
| 2.6 | log.Printf 而非框架 Logger | 低 | 一致性 |
| 2.7 | 无内置优雅关停 | 中 | 使用场景 |
| 2.8 | DefaultEnvelope 未默认启用 | 高 | 一致性 |
| 3.1 | 路由冲突不检测 | 中 | 路由引擎 |
| 3.2 | trailing slash 强制等价 | 低 | 路由引擎 |
| 3.3 | ANY 包含 CONNECT/TRACE | 中 | 安全 |
| 3.4 | GET 不自动响应 HEAD | 低 | 路由语义 |
| 4.1 | CORS origin 不匹配仍下发头 | 中 | 中间件 |
| 4.2 | Recoverer 不记录堆栈 | 中 | 中间件 |
| 4.3 | Timeout 不写 504 | 中 | 中间件 |
| 5.1 | OpenAPI wildcard 语义丢失 | 中 | OpenAPI |
| 5.2 | ToStatic 修改路径影响 OpenAPI | 低 | OpenAPI |
| 5.3 | OpenAPI 端点路径不可配置 | 低 | OpenAPI |
| 6.1 | ServeHTTP 每请求重建 chain | 低 | 性能 |
| 6.2 | 手写 joinStrings/itoa | 低 | 代码质量 |
| 7.1 | 无压缩中间件 | 中 | 使用场景 |
| 7.2 | 无限流中间件 | 中 | 使用场景 |
| 7.3 | 无 tracing 集成 | 中 | 使用场景 |
| 7.4 | 请求日志格式不可定制 | 低 | 使用场景 |
| 7.5 | 无 Swagger UI 挂载 | 低 | 使用场景 |
| 7.6 | 无 h2c 支持 | 低 | 使用场景 |
| 7.7 | 静态文件缺少 ETag/Cache-Control | 低 | 使用场景 |

---

## 九、改进优先级建议

### P0 — 正确性与一致性（必须修复）

1. **错误响应走 codec 输出 JSON**（1.1）：无 envelope 时也用路由的 produces codec 序列化错误体
2. **DefaultEnvelope 默认启用**（2.8）：或修改 README 明确说明默认返回裸 JSON
3. **405 添加 Allow header**（1.2）

### P1 — 易用性提升（高收益）

4. **增加类型化 query/path 方法**（1.5）：`QueryInt`, `QueryBool`, `PathInt` 等
5. **`Reads()` 改为自动获取类型或重命名**（2.1）
6. **特殊路由去掉泛型参数要求**（2.2）：提供 `s.SSE()`, `s.Static()` 等便捷方法
7. **CORS 修正**（1.4, 4.1）：加 `Vary: Origin`，origin 不匹配不下发头
8. **Recoverer 记录堆栈**（4.2）
9. **路由冲突检测**（3.1）

### P2 — 能力补齐

10. **ANY 排除 CONNECT/TRACE**（3.3）
11. **GET 自动响应 HEAD**（3.4）
12. **OpenAPI wildcard 语义保留**（5.1）
13. **OpenAPI 端点路径可配置**（5.3）
14. **压缩中间件集成**（7.1）
15. **tracing 中间件集成**（7.3）
16. **优雅关停内置**（2.7）
17. **路由列表查询**（2.5）

### P3 — 细节打磨

18. `joinStrings`/`itoa` 替换为标准库（6.2）
19. `log.Printf` 替换为框架 Logger（2.6）
20. `ServeHTTP` 缓存 handler chain（6.1）
21. Group 中间件时序文档说明（2.4）
22. 静态文件 ETag/Cache-Control（7.7）
