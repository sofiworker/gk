# ghttp

English | [中文](README.md)

A generics-oriented, typed HTTP routing framework built on the standard `net/http`. Entries are split by input shape (`GetParams` / `PostBody` / `PostParamsBody`, etc.); handlers take naked parameters with all type parameters inferred and no wrapper container. Params bind via struct tags (`path:` / `query:` / `header:`), the body is decoded by a `RequestDecoder`, and output is encoded by an `OutputSpec`; use `RawHandle` to fully own the response.

Performance stance: a pure `net/http` foundation with no fasthttp and no self-managed TCP; the hit hot path is zero-reflection and zero-allocation (`dispatchRaw` hits at 0 alloc), with gains coming only from pooled contexts and zero-reflection codecs.

## Features

- Routing: radix tree (gin lineage) + path params + catch-all + trailing-slash redirect (TSR)
- Typed entries: generic free functions, plan built at registration (reflect once), zero reflection at request time
- Middleware: global `Use` treats hits and misses alike; a zero-overhead direct path when no global middleware
- Built-in middleware: RequestID, Logger, LimitBody, Recovery, CORS (with preflight), Timeout, BasicAuth
- Static assets: `Static` / `StaticFS` (`os.DirFS` and `embed.FS`), `File`, SPA history fallback
- Health checks: `Health` (liveness), `Ready` (readiness), `NewReadinessGate` (runtime toggle)
- Lifecycle: `Run` / `RunTLS` / `Serve` / `ServeTLS` / `Shutdown` / `Close` / `RunGraceful`

## Quick start

```go
s := ghttp.New()
s.Use(ghttp.RequestID(), ghttp.Logger(), ghttp.Recovery())

type getUserParams struct {
	ID int64 `path:"id" validate:"min=1"`
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
- otherwise framework sentinels map it (`ErrInvalidInput`/`ErrValidation`→400, `ErrUnsupportedMediaType`→415, etc.);
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

## Parameter validation

Params fields support scalar binding: `string`, `bool`, `int/8/16/32/64`, `uint/8/16/32/64`, `float32/64` (out-of-range yields 400). Declare rules via a `validate` tag (compiled to closures at registration, zero tag-parsing at request time):

| Rule | Applies to | Notes |
|---|---|---|
| `required` | all | missing yields 400 |
| `min=N` / `max=N` | numeric compares magnitude; string compares length | |
| `len=N` | string length exactly N; numeric equals N | |
| `oneof=a b c` | all | one of the space-separated candidates |
| `email` | string | simplified email check |

A request body implementing `Validator` (`Validate() error`) is checked automatically after decoding; failures normalize to `ErrValidation` (→400), or pass through the status when the returned value implements `StatusCoder`. Zero extra cost when validation is unused.

```go
type CreateOrder struct {
	Amount int `json:"amount"`
}
func (o CreateOrder) Validate() error {
	if o.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	return nil
}
```

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
