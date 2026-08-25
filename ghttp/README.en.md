# ghttp

English | [中文](README.md)

A generics-oriented, typed HTTP routing framework built on the standard `net/http`. Entries are split by input shape (`GetParams` / `PostParams` / `PostBody` / `PostParamsBody`, etc.); handlers take naked parameters with all type parameters inferred and no wrapper container. Params bind via struct tags (`path:` / `query:` / `header:`) and carry only transport parameters; the body is decoded by a typed `InputSpec[B]` (built-in `JSONBody[B]`/`XMLBody[B]`/`FormBody[B]`/`TextBody[B]`, or `Body[B](codec)` for a custom decoder), with form text fields and uploaded files (`Upload` / `[]Upload`) both belonging to the body and decoded together by `FormBody[B]()`; output is encoded by an `OutputSpec[O]`; use `RawHandle` to fully own the response.

Performance stance: a pure `net/http` foundation with no fasthttp and no self-managed TCP; the hit hot path is zero-reflection and zero-allocation (`dispatchRaw` hits at 0 alloc), with gains coming only from pooled contexts and zero-reflection codecs.

## Features

- Routing: radix tree (gin lineage) + path params + catch-all + trailing-slash redirect (TSR)
- Typed entries: generic free functions, plan built at registration (reflect once), zero reflection at request time
- Middleware: global `Use` treats hits and misses alike; a zero-overhead direct path when no global middleware
- Built-in middleware: RequestID, Logger, LimitBody, Recovery, CORS (with preflight), Timeout, BasicAuth, Metrics (zero-dependency Prometheus-style metrics), Gzip (conditional compression with configurable level)
- Static assets: `Static` / `StaticFS` (`os.DirFS` and `embed.FS`), `File`, SPA history fallback
- Health checks: `Health` (liveness), `Ready` (readiness), `NewReadinessGate` (runtime toggle)
- Real-time: SSE sugar (`NewSSEWriter`) and WebSocket (`ServeWS` / `WSUpgrader` via gorilla/websocket) over `RawHandle`
- Lifecycle: `Run` / `RunTLS` / `Serve` / `ServeTLS` / `Shutdown` / `Close` / `RunGraceful`

## Quick start

```go
s := ghttp.New()
s.Use(ghttp.RequestID(), ghttp.Logger(), ghttp.Recovery())

type getUserParams struct {
	ID int64 `path:"id"`
}
ghttp.GetParams(s, "/users/{id}", ghttp.JSON[User](),
	func(ctx context.Context, p getUserParams) (User, error) {
		return findUser(ctx, p.ID)
	})

_ = s.RunGraceful(":8080") // graceful stop on SIGINT/SIGTERM
```

## Unified error chain

An `error` returned by a typed handler / codec passes through a **single exit** that classifies it into an HTTP status code:

- a business error implementing `StatusCoder` (`HTTPStatus() int`) carries its own status;
- otherwise framework sentinels map it (`ErrInvalidInput`→400, `ErrUnsupportedMediaType`→415, etc.);
- with a 500 fallback.

The error body is sanitized by default (generic text only; `err.Error()` details go to logs) and emitted as JSON `{"error":{"code","message"}}` by default.

```go
s := ghttp.New(
	ghttp.WithExposeErrorDetails(true),          // return err text (debugging)
	ghttp.WithErrorRenderer(myRenderer),         // custom body (e.g. RFC 9457)
	ghttp.WithErrorHook(func(r *http.Request, status int, err error) { /* observe */ }),
)
```

404/405 also emit the unified error body and are customizable via `WithNotFoundHandler` / `WithMethodNotAllowedHandler`.

## Observability

- `Request.MatchedRoute()`: the low-cardinality matched route template (e.g. `/users/:id`), suited for the route dimension in metrics/tracing/logging.
- `Request.ClientIP()` / `RemoteIP()`: resolve the real client IP under a trusted-proxy model. **No forwarded header is trusted by default** (anti-spoofing); after `WithTrustedProxies(...)`, `X-Forwarded-For` / `X-Real-IP` are walked only when the direct peer is trusted (header names overridable via `WithForwardedHeaders`).
- `Logger` / `LoggerWith`: emit a structured `AccessLog` (with `Method`/`Path`/`Route`/`Status`/`Elapsed`/`ClientIP`/`BytesOut`/`Err`).
- `Metrics`: zero-dependency Prometheus-style metrics middleware, recording request count/latency/error rate/bytes labelled by low-cardinality `MatchedRoute`. Mount the `/metrics` endpoint via `MetricsRegistry.Handler()`, which exports runtime metrics like `go_goroutines` and `go_memstats_alloc_bytes`.
- `Gzip`: conditional compression middleware that compresses response bodies by configurable Content-Type whitelist and level (`WithGzipLevel`); automatically detects `Accept-Encoding` with q-value priority.

## Parameter binding

Params fields support scalar binding: `string`, `bool`, `int/8/16/32/64`, `uint/8/16/32/64`, `float32/64`. A missing field keeps its zero value; a parse failure or out-of-range value yields 400 (`ErrInvalidInput`).

The framework has **no built-in validation**. Business rules (required, ranges, enums, formats) are checked by the handler itself; return an error implementing `StatusCoder` to map any status (e.g. 422), otherwise it falls back to 500. A dedicated validation layer will be designed separately.

```go
func createOrder(ctx context.Context, o CreateOrder) (OrderResp, error) {
	if o.Amount <= 0 {
		return OrderResp{}, badRequest("amount must be positive") // error implements StatusCoder
	}
	// ...
}
```

## Real-time: SSE and WebSocket

`Response` implements `http.Flusher` and `http.Hijacker` (`Flush` / `Hijack` pass through to the underlying connection); real-time features build on `RawHandle`.

**Server-Sent Events**: `NewSSEWriter(resp)` writes the `text/event-stream` headers, then each `Send` / `SendEvent` / `SendMessage` / `Comment` / `Ping` encodes one SSE wire record and **flushes immediately**; a write error (a client disconnect is common) is remembered so a loop can exit via the return value or `Err()`. SSE is a plain-HTTP long connection that does not hijack, staying under middleware and graceful shutdown.

```go
m.RawHandle(http.MethodGet, "/sse/time", func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
    w := ghttp.NewSSEWriter(resp)
    for {
        select {
        case <-ctx.Done():
            return nil
        case t := <-ticker.C:
            if err := w.SendEvent("time", t.Format(time.RFC3339)); err != nil {
                return nil
            }
        }
    }
})
```

**WebSocket**: upgrades via `github.com/gorilla/websocket` — ghttp only makes `Response` hijackable, delegating the protocol to gorilla. `ServeWS` registers an upgrade endpoint in one call (handing the `*websocket.Conn` to the handler and `Close`ing it on return); for customization use `NewWSUpgrader(WithWS...)` with `Upgrade` inside a `RawHandle`.

```go
ghttp.ServeWS(m, "/ws/echo", nil, func(ctx context.Context, req *ghttp.Request, conn *websocket.Conn) error {
    for {
        mt, msg, err := conn.ReadMessage()
        if err != nil {
            return err
        }
        if err := conn.WriteMessage(mt, msg); err != nil {
            return err
        }
    }
})
```

`WSUpgrader` options: `WithWSCheckOrigin` (origin check, default gorilla same-origin), `WithWSReadBufferSize` / `WithWSWriteBufferSize`, `WithWSSubprotocols`, `WithWSHandshakeTimeout`, `WithWSCompression`. When the underlying `ResponseWriter` cannot `Hijack`, `Hijack` returns `ErrNotHijackable`. See [`examples/realtime`](../examples/realtime).

## Graceful shutdown and auth

```go
gate, checker := ghttp.NewReadinessGate("startup")
ghttp.Ready(s, "/readyz", time.Second, checker)
gate.Set(true, nil)

_ = s.RunGraceful(":8080",
	ghttp.WithReadinessGate(gate),             // drain readiness first (LB notices)
	ghttp.WithDrainDelay(3*time.Second),       // wait for LB to drain
	ghttp.WithShutdownTimeout(30*time.Second), // force-close after the drain timeout
)

// BasicAuth: constant-time comparison, authenticated user passed down via context
s.Use(ghttp.BasicAuth("Admin", map[string]string{"alice": "secret"}))
// in a handler: user := ghttp.BasicAuthUser(ctx)
```

## Breaking changes (production-readiness batches)

This repository is pre-v1.0.0; the following changes are not backward-compatible:

| # | Change | Impact | Migration |
|---|---|---|---|
| B1 | typed error no longer returns `serr.Error()` by default; generic text instead | clients relying on plaintext errors | `WithExposeErrorDetails(true)` |
| B2 | error body `text/plain` → JSON `{"error":{code,message}}` | clients parsing the error body | `WithErrorRenderer` |
| B3 | request Content-Type strictly verified by default (415 on mismatch) | old clients sending a wrong CT | `WithStrictContentType(false)` |
| B4 | `LoggerWith` signature `(method,path,status,elapsed)` → `(AccessLog)` | callers | use the struct fields |
| B5 | 404/405 now carry a JSON response body | tests asserting an empty body | update assertions |

## Status

pre-v1.0.0, under development, not for direct production use; see `DEVELOPMENT.md` at the repository root.
