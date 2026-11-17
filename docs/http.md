# HTTP server

**English** | [简体中文](zh-CN/http.md) | [Documentation](README.md)

[Source](../http/) · `github.com/sofiworker/keel/http`

The currently declared package is `ghttp`. It provides routing, typed request binding,
middleware and OpenAPI support on top of the standard library's `net/http`.
Start with the complete [handler example](usage.md#http-handler).

## Parameter access

`Value` provides conversions returning errors and `Must*` methods for inputs
already guaranteed by a route contract:

```go
id := req.PathValue("id").MustInt64()
page := req.QueryValue("page").MustInt()
enabled := req.QueryValue("enabled").MustBool()
```

`MustString`, `MustInt`, `MustInt64`, `MustFloat64` and
`MustBool` panic when the value is missing or malformed.
Use error-returning conversions or `*Or` methods for unchecked external input.

## Performance options

Path validation, 405/Allow handling and body limits are enabled by default.
For traffic validated by a trusted gateway, these options reduce hot-path work:

```go
server := ghttp.NewServer(
    ghttp.WithUnsafeSkipPathValidation(),
    ghttp.WithMethodNotAllowed(false),
)
```

Disabling method-not-allowed handling makes unmatched methods return 404.
`WithUnlimitedBody()` or `WithBodyLimit(-1)` disables a route body limit;
use only for trusted, rate-limited input.

## OpenAPI and rate limiting

Routes participate in `OpenAPIDocument` by default. Use `WithOpenAPIHidden()`
or `WithOpenAPIExpose(bool)` for internal and health routes.

Rate limiting uses the framework error handler by default. Supply
`WithRateLimitRejectHandler` for a custom response; Retry-After is still written:

```go
ghttp.WithRateLimitRejectHandler(func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
    resp.WriteHeader(http.StatusTooManyRequests)
    _, err := resp.Write([]byte(`{"error":"rate limit"}`))
    return err
})
```

Here `http` denotes the standard library and `ghttp` the Keel package.

## WebSocket routes

`WS(path, handler, opts...)` uses the same argument order as other routes.
The handler is `func(context.Context, *ghttp.Request, *ghttp.Response) error`.
It invokes the chosen WebSocket library's upgrade function and owns the resulting connection:

```go
route := ghttp.WS("/ws", websocketHandler,
    ghttp.WithMiddleware(auth),
    ghttp.WithDoc("Chat", "WebSocket endpoint"),
)
```

Keel does not implement the WebSocket protocol or provide an upgrade wrapper.
The former WebSocket wrapper package was removed; choose a protocol library in your application.
Route behavior is covered by [WebSocket tests](../http/websocket_test.go).

WS registers a GET route with Server, Group and route middleware plus the normal error chain.
`WithInput`/`WithOutput` do not apply. Route metadata is `RouteWebSocket`;
OpenAPI describes the 101 handshake, not message protocols.
Exclude incompatible compression, timeout or response-cache middleware.
Authentication and rate limiting can apply. HTTP server shutdown does not automatically
close hijacked connections; the handler owns their shutdown.
