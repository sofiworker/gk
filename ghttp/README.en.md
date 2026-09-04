# ghttp

English | [中文](README.md)

A generics-oriented, typed HTTP routing framework built on the standard `net/http`. Entries are split by input shape (`GetParams` / `PostParams` / `PostBody` / `PostParamsBody`, etc.); handlers take naked parameters with all type parameters inferred and no wrapper container. Params bind via struct tags (`path:` / `query:` / `header:`) and carry only transport parameters; the body is decoded by a typed `InputSpec[B]` (built-in `JSONBody[B]`/`XMLBody[B]`/`FormBody[B]`/`TextBody[B]`, or `Body[B](codec)` for a custom decoder), with form text fields and uploaded files (`Upload` / `[]Upload`) both belonging to the body and decoded together by `FormBody[B]()`; output is encoded by an `OutputSpec[O]`; use `RawHandle` to fully own the response.

Performance stance: a pure `net/http` foundation with no fasthttp and no self-managed TCP; the hit hot path is zero-reflection and zero-allocation (`dispatchRaw` hits at 0 alloc), with gains coming only from pooled contexts and zero-reflection codecs.

## Features

- Routing: radix tree (gin lineage) + path params + catch-all + trailing-slash redirect (TSR)
- Typed entries: generic free functions, plan built at registration (reflect once), zero reflection at request time. Entries are always named `<Method><InputShape>`: all seven methods have `None` and `Params` shapes, and the four body-bearing ones (POST/PUT/PATCH/DELETE) additionally have `Body` and `ParamsBody`
- Middleware: global `Use` treats hits and misses alike; a zero-overhead direct path when no global middleware
- Built-in middleware: RequestID, Logger, LimitBody, Recovery, CORS (with preflight), Timeout (504 through the unified error chain), BasicAuth, Metrics (zero-dependency Prometheus-style metrics), Gzip (conditional compression with configurable level), RateLimit (sharded token bucket, 429 + `Retry-After`), CSRF (double-submit cookie + origin check), SecureHeaders (safe defaults; HSTS and CSP opt-in)
- OpenAPI 3.1: `WithOpenAPI` generates a deterministic spec from the same bind plan the request path uses, so documentation cannot drift; zero cost when disabled
- Static assets: `Static` / `StaticFS` (`os.DirFS` and `embed.FS`), `File`, SPA history fallback, pre-compressed `.br` / `.gz` variants (`WithPrecompressed`)
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

- `Request.MatchedRoute()`: the low-cardinality matched route template (e.g. `/users/:id`), suited for the route dimension in metrics/tracing/logging. **Readable while global middleware runs** (including before it calls `next`): route matching completes before the middleware chain, so route-aggregating middleware (metrics, per-route rate limiting, tracing span naming) sees the template. It is always empty on a miss and never carries the previous request's value.
- `Request.ClientIP()` / `RemoteIP()`: resolve the real client IP under a trusted-proxy model. **No forwarded header is trusted by default** (anti-spoofing); after `WithTrustedProxies(...)`, `X-Forwarded-For` / `X-Real-IP` are walked only when the direct peer is trusted (header names overridable via `WithForwardedHeaders`).
- `Logger` / `LoggerWith`: emit a structured `AccessLog` (with `Method`/`Path`/`Route`/`Status`/`Elapsed`/`ClientIP`/`BytesOut`/`Err`).
- `Metrics`: zero-dependency Prometheus-style metrics middleware, recording request count/latency/error rate/bytes labelled by low-cardinality `MatchedRoute`. Mount the `/metrics` endpoint via `MetricsRegistry.Handler()`, which exports runtime metrics like `go_goroutines` and `go_memstats_alloc_bytes`.
- `Gzip`: conditional compression middleware that compresses response bodies by configurable Content-Type whitelist and level (`WithGzipLevel`); automatically detects `Accept-Encoding` with q-value priority.

## OpenAPI 3.1

```go
s := ghttp.New(
	ghttp.WithOpenAPI(ghttp.OpenAPIInfo{Title: "Orders API", Version: "1.2.0"},
		ghttp.WithOpenAPIRoute("/openapi.json"), // default; "" builds in memory only
		ghttp.WithOpenAPIServers(ghttp.OpenAPIServer{URL: "https://api.example.com"}),
	),
)
raw := s.SpecJSON() // or read the bytes directly for build artifacts / contract tests
```

The contract grows out of the code instead of being written twice. Metadata is collected at registration from each typed entry's params/body/output types; the spec is built once on the first request and its bytes cached.

- **Documentation cannot drift**: parameter docs reuse the very same `BindPlan` the request path uses, rather than re-implementing the binding rules. Change a tag and the docs follow; `TestOpenAPI_ParametersMatchBindPlan` enforces it.
- **Deterministic output**: the spec is serialized by hand so field order is fixed. With `map[string]any` + `json.Marshal`, Go's random map iteration order would make one route table emit different bytes every time, and the spec could not be diff-reviewed or snapshot-tested.
- **Shapes match reality**: a `NoContent` output declares 204 with **no** `content`; error responses reference one `Error` schema shaped exactly like what the error chain actually writes.
- **Schemas follow `encoding/json`**: `json:"-"` skipped, `omitempty` not required, embedded fields promoted, unexported fields absent; named structs are hoisted into `components/schemas` and referenced by `$ref` (so self-referential types terminate instead of overflowing the stack); `time.Time` → `date-time`, `[]byte` → `byte`, pointers → OpenAPI 3.1 `["T","null"]`.
- **`RawHandle` endpoints are recorded too**: they expose no reflectable types, but the existence of a path and method is itself part of the contract — omitting them would make the spec claim the endpoint does not exist and mislead contract tests and client generators. The strategy is to report honestly rather than invent: record the path and method, derive required `string` path parameters from the template (otherwise the spec is invalid), declare the response shape as undeclared instead of fabricating a schema, and add none of the 400/415 responses that only typed binding produces. Static assets (`Static`/`StaticFS`) and health checks (`Health`/`Ready`) build on `RawHandle` and therefore appear as well.
- **Zero cost when disabled**: nothing is collected, built, or registered, and the spec endpoint never documents itself.
- `WithOpenAPI` is a `New` option and must take effect **before** routes are registered.

## Parameter binding

`path:` / `query:` / `header:` and body form fields (`form:`) share one binding engine, so their capabilities are **exactly at parity** — every shape below works on `form:` too.

Supported field shapes: scalars (`string`, `bool`, `int/8/16/32/64`, `uint/8/16/32/64`, `float32/64`); `*T` pointers (`nil` means "not provided", distinguishable from an explicit zero); types implementing `encoding.TextUnmarshaler` (`time.Time`, `net.IP`, …); `[]byte` (raw bytes); `[]T` / `[N]T` lists (repetition `?a=1&a=2` and comma separation `?a=1,2`, mixable); `map[string]T` / `map[string][]T` (`?filter[key]=v` in a query, a header prefix family or `*` for every header). Untagged embedded and nested structs expand recursively so shared parameters can be factored into reusable structs; a `"-"` tag value skips a field explicitly.

Absence semantics: scalars keep their zero value while pointers, slices, and maps stay `nil` — and a nested struct pointer is not allocated when its whole block is absent — so a handler can tell "not provided" from "provided as zero/empty".

A failed element parse fails the whole request with 400 (`ErrInvalidInput`) rather than silently dropping the bad element, which would let callers believe the parameter took effect. Unsupported shapes (slice of slice, map of map, non-string map keys, a map bound to `path:`) are rejected **at registration**, and recursion is capped at 8 levels so a self-referential struct errors instead of overflowing the stack.

**Lazy query lookup**: the bind plan knows at registration which keys it reads, so at request time no `url.Values` map is built — keys are scanned directly in the raw query string, and a value free of `%`/`+` is referenced as a substring of the original, making lookup **allocation-free**. Semantics match `net/url.ParseQuery` exactly (including semicolon invalidation and skipping bad escapes, validated by table-driven comparison and fuzzing). Two cases fall back to building the map automatically: map-shaped fields (`query:"filter"` / `query:"*"` must enumerate every key), and queries with more than 24 keys (past which a linear scan loses to a map). Pure-path endpoints never touch the query at all.

The framework has **no built-in validation**. Business rules (required, ranges, enums, formats) are checked by the handler itself; return an error implementing `StatusCoder` to map any status (e.g. 422), otherwise it falls back to 500. A dedicated validation layer will be designed separately.

```go
func createOrder(ctx context.Context, o CreateOrder) (OrderResp, error) {
	if o.Amount <= 0 {
		return OrderResp{}, badRequest("amount must be positive") // error implements StatusCoder
	}
	// ...
}
```

## Registration forms: free functions and chaining

Typed endpoints have two entry forms sharing one registration implementation (identical binding plan, strict Content-Type check, and OpenAPI registry), and they mix freely.

### Free functions (every supported Go version)

Pass the router as the first argument; type parameters are inferred from the handler:

```go
ghttp.GetParams(s, "/items/{id}", ghttp.JSON[ItemResp](),
    func(ctx context.Context, p ItemID) (ItemResp, error) { /* … */ })

ghttp.PostBody(s, "/items", ghttp.JSONBody[CreateReq](), ghttp.JSON[ItemResp]().Status(201),
    func(ctx context.Context, in CreateReq) (ItemResp, error) { /* … */ })
```

This is the **only** entry form under Go < 1.27, and it keeps working under Go >= 1.27.

### Chained entries (**Go >= 1.27 only**)

Go 1.27 lets methods declare type parameters (generic methods), which makes a **non-generic chain whose types are inferred at the terminal** possible — something inexpressible before Go 1.27, where type parameters could only be declared at the start of the chain, forcing callers to spell out `Req`/`Resp`:

```go
// params (path/query/header bound via struct tags)
s.Get("/items/{id}").To(ghttp.JSON[ItemResp](),
    func(ctx context.Context, p ItemID) (ItemResp, error) { /* … */ })

// body only
s.Post("/items").ToBody(ghttp.JSONBody[CreateReq](), ghttp.JSON[ItemResp]().Status(201),
    func(ctx context.Context, in CreateReq) (ItemResp, error) { /* … */ })

// params + body
s.Patch("/items/{id}").ToParamsBody(ghttp.JSONBody[UpdateReq](), ghttp.JSON[ItemResp](),
    func(ctx context.Context, p ItemID, b UpdateReq) (ItemResp, error) { /* … */ })

// neither params nor body
s.Get("/healthz").ToNone(ghttp.JSON[HealthResp](),
    func(ctx context.Context) (HealthResp, error) { /* … */ })

// route-level middleware plus full response ownership
s.Get("/stream").Use(mw).ToRaw(func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error { /* … */ })
```

- Verbs: `Get`/`Post`/`Put`/`Patch`/`Delete`/`Head`/`Options`, plus `Method(verb, path)` for custom methods.
- Terminals: `To` (params), `ToBody` (body only), `ToParamsBody` (params+body), `ToNone` (no input), `ToRaw` (raw handler). All return `error` with the same semantics as the free-function entries.
- Groups work the same way: `g := s.Group("/api/v1"); g.Get("/items/{id}").To(…)`, with paths relative to the group prefix.
- `Use(...)` on the chain adds route-level middleware; the fold order is **global -> group -> route -> terminal** (matching gin).
- Chain starters are defined on the internal `mux` and promoted onto `Server` by embedding; `Group` defines its own set.

> **Version gate**: the chained API lives in `//go:build go1.27` files, which toolchains older than 1.27 exclude at the build-constraint level. Calling `s.Get(...)` there fails with `has no field or method Get` — use the free-function entries instead. `go.mod` need not declare `go 1.27` (the build tag raises that file's language version as needed).
>
> Note: files containing generic-method syntax **must be formatted with a Go 1.27+ gofmt**; older gofmt and golangci-lint v1 parsers cannot parse them (CI pins the respective toolchains).

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

## Rate limiting, CSRF, and security headers

```go
s.Use(ghttp.RateLimit(ghttp.RateLimitConfig{RPS: 100, Burst: 200}))
s.Use(ghttp.RateLimitByRoute(50, 100))   // keyed by method + route template

s.Use(ghttp.CSRF(ghttp.CSRFConfig{TrustedOrigins: []string{"https://app.example.com"}}))

s.Use(ghttp.SecureHeadersDefault())
s.Use(ghttp.SecureHeaders(ghttp.SecureHeadersConfig{
	HSTSMaxAge:            31536000,            // HSTS is off unless set
	ContentSecurityPolicy: "default-src 'self'",
	FrameOptions:          "-",                 // "-" omits the header
}))
```

- **RateLimit** uses a token bucket, not a fixed window: it allows a `Burst` while the long-run rate converges to `RPS`, avoiding the boundary doubling of fixed windows. Keyed by `ClientIP()` by default (honouring the trusted-proxy config, so a spoofed header cannot bypass it); a `KeyFunc` can key by user or API key, and **returning an empty string exempts the request**. Buckets are spread over 16 shards to bound lock contention, and buckets idle beyond `IdleTimeout` (10 minutes by default) are reaped. Reaping only fires on the shard being touched, so `MaxKeys` (100000 by default) additionally caps how many keys are tracked: beyond it a new key gets no bucket and **passes through** — rate limiting is an availability protection, not access control, and "reject everything once full" would let one key flush produce a site-wide denial of service. Over the limit it answers 429 with `Retry-After`; the error is `ErrRateLimitExceeded` (usable with `errors.Is`), and `OnLimited` customizes the response. With `RPS <= 0` it returns a pass-through middleware and does no accounting, so it can be toggled per environment without restructuring code.
- **CSRF** combines a double-submit cookie with an `Origin`/`Referer` check. The `csrf_token` cookie is deliberately **not** HttpOnly, because front-end JS must read it to echo it back in `X-CSRF-Token` or a form field; comparison is constant-time. Safe methods only issue the token, and `CSRFTokenFromContext(ctx)` lets server-side templates render it into a form. Failure yields 403 with `ErrCSRFTokenInvalid`. A non-form body (e.g. JSON) is never parsed, so the body typed decoding needs is left intact.
- **SecureHeaders** treats each field as tri-state: empty means the safe default, `"-"` omits that header, anything else is used verbatim. **HSTS and CSP are off by default** — HSTS is hard to retract once a browser remembers it and a misconfiguration locks out localhost (so it requires `HSTSMaxAge > 0` and, by default, a TLS request), while any generic CSP default would break real pages. The header list is frozen into a slice at registration, and existing headers of the same name are **never overwritten**, so a single route can override one.

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
| B5 | `File` gained variadic `opts ...StaticOption` | only function-value references to `File` | existing calls need no change |
| B5 | 404/405 now carry a JSON response body | tests asserting an empty body | update assertions |

### Security and correctness batch (from `REVIEW.md`)

A batch of verified fixes changed observable behavior. Only items needing caller or operator action are listed; the full list with per-item root causes is in `CHANGELOG.md` under `[Unreleased] / Fixed`.

| # | Change | Impact | Migration |
|---|---|---|---|
| S1 | Per-status Prometheus counts moved from `http_requests_total{code=}` to a new family `http_requests_by_code_total` | scrapers, dashboards, alerts | rename the metric; the old shape gave one family inconsistent label keys, which is an invalid exposition format |
| S2 | `Timeout` answers 504 `application/json` instead of 503 `text/plain` | clients and probes asserting the old status or body | branch on the new `ErrRequestTimeout` sentinel |
| S3 | `Referrer-Policy` default is now `strict-origin-when-cross-origin` | downstreams needing a full same-level Referer (path and query included) | set the old value explicitly |
| S4 | CORS force-drops credentials for a wildcard origin | configs pairing `*` with `AllowCredentials` | list the origins explicitly |
| S5 | CSRF rejects `Origin: null` and compares the scheme | sandboxed iframes, `data:` documents, post-redirect requests, mixed http/https deployments | unify the scheme; those origins are now refused |
| S6 | `RequestID` also validates the charset `[0-9A-Za-z._-]` | upstreams sending other characters | change the ID format |
| S7 | `LimitBody` over-limit answers 413 with the unified JSON body | clients asserting the body | update assertions |
| S8 | Group prefixes are normalized (`/api/` + `/v1/x` → `/api/v1/x`, not `/api//v1/x`) | callers depending on the double-slash URL | use the normalized path |
| S9 | An error returned by a custom 404/405 handler now yields 500 instead of a silent 200 | custom miss handlers | handle their own errors explicitly |
| S10 | A form endpoint receiving a non-form Content-Type answers 415 instead of a silent 200 with a zero-value struct | clients sending a wrong CT | fix the CT, or `WithStrictContentType(false)` |
| S11 | JSON decoding rejects trailing content after the first value | senders emitting `{"a":1} junk` or `{"a":1}{"a":2}` | send one value per body |
| S12 | `WithErrorHook`'s `status` reports the delivered status once the response is committed | hooks relying on the classified value | recover it with `HTTPStatus(err)` |
| S13 | Trailing-slash redirects refuse targets starting `//` or `/\` and targets with control characters (treated as a miss, 404) | none | this is an open-redirect fix |

New exported API: `PanicValueOf(err) (any, bool)`, `ErrRequestTimeout`, `RateLimitConfig.MaxKeys`, `CSRFConfig.UseHostPrefixedCookie`, `CSRFHostCookiePrefix`, `MultiContentTypeDecoder`.

## Status

pre-v1.0.0, under development, not for direct production use; see `DEVELOPMENT.md` at the repository root.
