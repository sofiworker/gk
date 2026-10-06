# ghttp

`ghttp` 提供基于 `net/http` 的路由、泛型请求绑定、中间件和 OpenAPI 工具。

## 参数访问

`Value` 同时提供返回错误的转换方法和适用于已由路由契约保证的 `Must*` 方法：

```go
id := req.PathValue("id").MustInt64()
page := req.QueryValue("page").MustInt()
enabled := req.QueryValue("enabled").MustBool()
```

`MustString`、`MustInt`、`MustInt64`、`MustFloat64` 和 `MustBool` 在参数缺失或格式不合法时 panic。外部输入应优先使用对应的错误返回方法或 `*Or` 方法。

## 性能选项

默认配置保留请求路径校验、405/`Allow` 处理和请求体上限。对于已经由可信网关完成校验的流量，可以显式使用：

```go
s := ghttp.NewServer(
    ghttp.WithUnsafeSkipPathValidation(),
    ghttp.WithMethodNotAllowed(false),
)
```

这两个选项会减少热路径工作：关闭路径校验后不再扫描请求路径，关闭 method-not-allowed 后方法不匹配直接按 404 处理。请求体限制可按路由使用 `WithUnlimitedBody()` 或 `WithBodyLimit(-1)` 关闭；仅应对可信、已限流的请求使用。

## OpenAPI 与限流

路由默认进入 `OpenAPIDocument`。内部或健康检查路由可以使用 `WithOpenAPIHidden()`，也可以用 `WithOpenAPIExpose(bool)` 显式控制是否生成文档。

限流中间件达到上限时默认走框架错误处理；需要自定义响应时可配置 `WithRateLimitRejectHandler`，`Retry-After` 仍会自动写入：

```go
ghttp.WithRateLimitRejectHandler(func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
    resp.WriteHeader(http.StatusTooManyRequests)
    _, err := resp.Write([]byte(`{"error":"rate limit"}`))
    return err
})
```


## WebSocket 路由

`WS(path, handler, opts...)` 使用与其他路由一致的“路径、处理函数、选项”参数顺序。
处理函数签名为 `func(context.Context, *ghttp.Request, *ghttp.Response) error`；
用户在函数中调用所选 WebSocket 库的 `Upgrade(resp, req.Raw, ...)`，并负责消息处理和关闭连接。

```go
route := ghttp.WS("/ws", websocketHandler,
    ghttp.WithMiddleware(auth),
    ghttp.WithDoc("Chat", "WebSocket endpoint"),
)
```

ghttp 不实现 WebSocket 协议，也不定义连接包装或升级适配器。原来的 `ghttp/ws`
包已移除；使用方需要迁移到自己选择的库。可运行的 Gorilla 用法见
`example/websocket.go`，Gorilla 仅由示例导入。

`WS` 注册 GET 路由，保留 Server、Group 和路由中间件，以及正常的错误处理链。
`WithInput` / `WithOutput` 不适用。路由元信息为 `RouteWebSocket`，OpenAPI 描述握手
成功时的 `101` 响应，不描述 WebSocket 消息协议。
压缩、超时、响应缓存等 HTTP 中间件需要由使用方避开升级路由；鉴权和限流可以正常使用。
升级后连接由 handler 管理，服务器的 HTTP 优雅关闭不会自动关闭已接管的连接。
