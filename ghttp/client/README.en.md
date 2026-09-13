# ghttp/client

English | [中文](README.md)

`ghttp`'s HTTP client: an **orchestration layer** on top of the standard library's `net/http`, sharing the ghttp server's semantics and extension model.

It does **not** implement a transport — it only provides middleware, codecs, error mapping, retries and observability, and always delegates transport to an `http.RoundTripper`.

```go
c := client.New(
    client.WithBaseURL("https://api.example.com"),
    client.WithTimeout(5*time.Second),
    client.WithRetry(client.RetryPolicy{MaxRetries: 3}),
)

var user User
resp, err := c.R().SetQueryParam("id", 1).SetResult(&user).Get("/users")
```

---

## Design stance

| Stance | Meaning |
|---|---|
| **Non-2xx is an error by default** | Returns an `*Error` that carries the whole `*Response` (its body stays readable). `WithAllowAllStatus()` restores the ecosystem habit of judging the status yourself |
| **Response bodies are size-limited by default** | 32 MiB, beyond which `ErrBodyTooLarge` is returned; lift it with `WithUnlimitedResponseBody()` or stream mode |
| **Unreplayable bodies are refused** | With retries enabled and a body that cannot be replayed, `ErrBodyNotReplayable` is returned **before the first attempt** — never a silent resend of an empty body |
| **Zero assumptions about the server's shape** | No implicit envelope; structured error bodies are wired in explicitly via `WithErrorDecoder` |
| **The standard library comes first** | Inject `*http.Client` / `http.RoundTripper` / `*net.Dialer` / `DialContext` / `http.CookieJar` / `CheckRedirect`, or degrade this Client into an `http.RoundTripper` |

---

## Quick start

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/sofiworker/gk/ghttp/client"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    c := client.New(
        client.WithBaseURL("https://api.example.com"),
        client.WithHeader("Accept", "application/json"),
    )

    // Option 1: fluent + SetResult (for a type known only at runtime)
    var user User
    resp, err := c.R().SetQueryParam("id", 1).SetResult(&user).Get("/users")
    if err != nil {
        var ce *client.Error
        if errors.As(err, &ce) {
            fmt.Println("status:", ce.StatusCode, "attempts:", ce.Attempts)
        }
        return
    }
    fmt.Println(resp.StatusCode(), user)

    // Option 2: generic sink (Go 1.18+, zero brackets)
    var user2 User
    if _, err := client.GetInto(context.Background(), c, "/users", &user2,
        client.WithRequestQuery("id", 1)); err != nil {
        return
    }
}
```

---

## Standard-library interop

```go
// Inject an existing *http.Client: full takeover of transport, jar, redirects, timeout
c := client.NewWithHTTPClient(myHTTPClient, client.WithBaseURL(base))

// Replace only the transport
c = client.New(client.WithTransport(myRoundTripper))

// Custom dialing (bind a local address, use a bespoke network stack)
c = client.New(client.WithDialer(&net.Dialer{LocalAddr: localAddr}))
c = client.New(client.WithDialContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
    return myDialer.DialContext(ctx, network, addr)
}))

// Use this Client as a RoundTripper (note: no middleware/error mapping; see godoc)
external := &http.Client{Transport: c.RoundTripper()}

// This package's request -> standard-library request
httpReq, err := c.R().SetMethod(http.MethodGet).SetURL("/x").HTTPRequest(ctx)

// Standard-library request -> this package's orchestration
resp, err := c.DoHTTP(ctx, httpReq)
```

**Hard rules**: this package never mutates `http.DefaultTransport` (it clones it), never sets `http.Client.Timeout` by default (it also times body reads), and never silently disables HTTP/2 when a custom dialer/TLS config is injected.

---

## Error handling

Non-2xx responses and transport failures both collapse into `*Error`:

```go
resp, err := c.R().Get("/users/404")
var ce *client.Error
if errors.As(err, &ce) {
    ce.StatusCode      // 0 means a transport failure (no response arrived)
    ce.Attempts        // total attempts
    ce.Response        // non-nil: the body is still readable
    errors.Is(err, client.ErrUnexpectedStatus) // was the status unacceptable?
}
```

Structured error bodies:

```go
type APIError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

var apiErr APIError
// Option 1: register a decode target with SetError
c.R().SetError(&apiErr).Get("/x")

// Option 2: a global error decoder (for one uniform error shape)
c = client.New(client.WithErrorDecoder(func(resp *client.Response, e *client.Error) error {
    return resp.JSON(&apiErr)
}))
```

`gerr` interop: `(*Error).Kind()` returns a `gerr.Kind`, and `AsGerr(err)` moves the error into the gerr world.

---

## Middleware and hooks

Middleware is an onion chain shaped exactly like `ghttp.Server.Use`:

```go
c.Use(func(next client.Handler) client.Handler {
    return func(ctx context.Context, req *client.Request) (*client.Response, error) {
        req.SetHeader("X-Request-ID", newID()) // pre-processing
        resp, err := next(ctx, req)
        logLatency(err)                        // post-processing
        return resp, err
    }
})
```

Not calling `next` short-circuits (cached response, mock, a failed local check).

Hooks are read-only observers: `OnBeforeRequest` / `OnAfterResponse` / `OnSuccess` / `OnError` / `OnRetry` / `OnPanic`.
`OnPanic` only observes and **re-panics by default**, matching the server's opt-in `Recovery`.

---

## Retries

```go
c := client.New(
    client.WithBaseURL(base),
    client.WithRetry(client.RetryPolicy{
        MaxRetries:        3,
        RetryDelay:        100 * time.Millisecond,
        RespectRetryAfter: true,          // honor the server's Retry-After (seconds or HTTP-date)
        OnRetry: func(attempt int, d time.Duration, resp *client.Response, err error) {
            log.Printf("retry %d in %s", attempt, d)
        },
    }),
)
```

Only **idempotent methods** (GET/HEAD/PUT/DELETE/OPTIONS/TRACE) are retried by default, on transport errors or `{408,429,500,502,503,504}`. POST/PATCH require an explicit `RetryNonIdempotent: true` — retrying them after a timeout duplicates side effects.

Backoff is reused from the base-contract layer `gretry`; the decision conditions stay here (HTTP-specific).

Body replayability:

| Body source | Retryable |
|---|---|
| `SetJSON` / `SetForm` / `SetBody` (in-memory carriers) | ✅ re-encoded per attempt |
| `SetBodyReader` + `io.Seeker` | ✅ rewound via `Seek(0,0)` |
| `SetBodyReader` + plain `io.Reader` | ❌ `ErrBodyNotReplayable` before sending |
| multipart file path (`SetFile`) | ✅ reopened per attempt |
| multipart non-seekable reader | ❌ refused before sending |

---

## Uploads, downloads and streaming

```go
// multipart upload
_, err := c.R().
    SetMultipartFormData(map[string]string{"title": "report"}).
    SetFile("doc", "/path/to/file.pdf").
    Post("/upload")

// streaming upload
req.SetBodyReader(file, "application/octet-stream").SetContentLength(size)

// download straight to disk (no in-memory copy)
c.R().SetOutputFile("/tmp/out.bin").Get("/big-file")

// streaming response (large files, process as it arrives)
resp, err := c.R().SetStreamResponse().Get("/stream")
defer resp.Close()
body, err := resp.Body()   // io.ReadCloser
```

The three body modes are mutually exclusive: **memory (default)** / **stream** (`SetStreamResponse`) / **file** (`SetOutputFile`).
In stream mode `Bytes()` / `String()` return `ErrStreamConsumed` rather than silently reading a consumed stream.

---

## Server-Sent Events

```go
stream, err := c.SSE(ctx, "/events")
if err != nil {
    return err
}
defer stream.Close()

for {
    ev, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    fmt.Println(ev.Name, ev.Data)
}
```

Automatic reconnection (backoff after a drop, optionally resuming from `Last-Event-ID`):

```go
err := c.ConsumeSSE(ctx, "/events", func(ev client.SSEEvent) error {
    fmt.Println(ev.Data)
    return nil
}, client.SSEReconnectPolicy{
    MaxRetries:            5,
    Delay:                 time.Second,
    ResumeFromLastEventID: true,
})
```

Heartbeat comment lines (starting with `:`) produce no event; bound your `ctx` if you need to observe them.

---

## Generic entries

Generics are a **thin shell** — status decisions, errors, retries and streaming all stay in the non-generic layer.

```go
// Package-level generic functions (Go 1.18+): T is inferred from *T, no brackets at the call site
var users []User
_, err := client.GetInto(ctx, c, "/users", &users)
_, err = client.PostInto(ctx, c, "/users", createReq, &user)

// When a response already exists
var u User
err = client.DecodeInto(resp, &u)

// Result form (T appears only in the return position, so it must be explicit)
result, err := client.As[User](resp)
```

```go
// Method-level sugar in //go:build go1.27 files
resp, err := c.GetInto(ctx, "/users", &user)                                 // Client method, zero brackets
resp, err = c.R().SetQueryParam("id", 1).GetInto(ctx, "/users", &user)       // fluent chain + method shortcut
resp, err = c.R().SetHeader("X-A", "1").PostInto(ctx, "/users", body, &user) // same family: Post/Put/Patch/Delete/Head/Options
result, err := c.R().SetQueryParam("id", 1).As[User]()                      // result form needs brackets
```

> **You never hand-write `SetMethod`**: `r.GetInto` / `r.PostInto` / `r.PutInto` / `r.PatchInto` / `r.DeleteInto` / `r.HeadInto` / `r.OptionsInto` set the method and URL themselves, so the chain carries only parameters and headers. For an arbitrary method (or a DELETE with a body) use `r.DoInto(ctx, method, url, &dst)`, and reserve `r.Into(ctx, &dst)` for when method/URL were configured separately.

> Go's inference ignores return types, so `As[User]()` needs brackets while the sink form (`GetInto(ctx, url, &user)`) does not. That is why sink is the primary style.

---

## Observability

```go
c := client.New(
    client.WithDebug(true),
    client.WithLogger(myLogger),   // implements client.Logger (same shape as the server's)
    client.WithTrace(true),
    client.WithDebugBodyLimit(2048),
)

resp, _ := c.R().Get("/x")
ti := resp.Traces()   // DNSLookup / Connect / TLSHandshake / WroteRequest / TTFB / Total
```

URLs in debug logs go through the same single-line sanitization as the server (log-injection safe).

---

## Concurrency and lifetime

- A `Client` is read-only after construction and therefore **safe for concurrent use**; call `Use` / `OnXxx` at construction time.
- A `Request` is mutable and **not safe for concurrent use**: build it, terminate it, discard it within one request.
- A streaming response must be `Close`d (otherwise the connection and goroutine stay alive).
- `Client.CloseIdleConnections()` closes idle connections in the pool.

---

## Boundaries and caveats

- **`WithTimeout` is a whole-request wall-clock cap**, including body reads; use a `context` or `WithResponseHeaderTimeout` for large responses/downloads.
- **`WithHTTPClient` means full takeover**: this package's transport-level options no longer apply afterwards.
- **`WithDisableRedirects()` implies accepting 3xx** (no longer an error), since refusing to follow redirects means handling them yourself; with a hand-rolled `CheckRedirect`, add `WithAcceptRedirects()` explicitly.
- **`WithProxyFromEnvironment` reads the environment once** (a standard-library package-level cache); use `WithProxy` for a runtime-variable proxy.
- Response decoding shares the server's strict kernel: trailing JSON content is rejected (`{"a":1}GARBAGE`), and a declared length that yields truncation is an error.
