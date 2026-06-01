# ghttp

HTTP Client and Server implementations.

## Client

Flexible HTTP client with middleware, retry, and streaming support.

## Server

High-performance HTTP server wrapping `fasthttp` with routing and middleware.

## Usage

```go
import "github.com/sofiworker/gk/ghttp/gclient"
import "github.com/sofiworker/gk/ghttp/gserver"
```

### Server response builder

Handlers can write JSON responses directly with `Ok`, or use `Resp()` when the
response needs a status code, headers, audit payload, or a non-JSON body.

```go
server.GET("/users/:id", func(ctx *gserver.Context) {
	ctx.Ok(map[string]string{"id": ctx.Param("id")})
})

server.POST("/users", func(ctx *gserver.Context) {
	ctx.Resp().
		Audit(map[string]string{"trace": "t-1"}).
		AuditAction("user.create").
		AuditUserID("u-1").
		AuditResource("user", "u-2").
		CreatedStatus().
		Header("X-Request-ID", "req-1").
		Ok(map[string]string{"status": "created"})
})

server.DELETE("/users/:id", func(ctx *gserver.Context) {
	ctx.Resp().
		AuditAction("user.delete").
		AuditResource("user", ctx.Param("id")).
		NoContent()
})
```

Audit fields are stored on the request context. Middleware can call
`ctx.AuditEvent()` after `ctx.Next()` to read built-in fields such as action,
user ID, resource, trace ID, and the custom payload. `ctx.AuditPayload()` remains
available when middleware only needs the user-defined payload.
