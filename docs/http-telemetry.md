# HTTP telemetry

**English** | [简体中文](zh-CN/http-telemetry.md) | [Documentation](README.md)

[Source](../http/adapters/gotel/) · `github.com/sofiworker/keel/http/adapters/gotel`

This adapter converts portable Tracer/Meter interfaces into HTTP middleware.
Its declared package is `ghttpotel`; the directory retains the nested name
`gotel`. Composition stays in the adapter rather than coupling the two core packages.

```go
import (
    ghttp "github.com/sofiworker/keel/http"
    ghttpotel "github.com/sofiworker/keel/http/adapters/gotel"
)

server := ghttp.NewServer()
metrics, err := ghttpotel.Metrics(meter)
if err != nil {
    return err
}
server.Use(
    ghttpotel.Tracing(tracer, ghttpotel.WithTraceIDHeader("X-Trace-Id")),
    metrics,
)
```

## Tracing

- Extract upstream headers using HeaderCarrier, start a span, and pass its context to the request and handler.
- Span names are `<METHOD> <route>`, or `<METHOD> unmatched`.
- Attributes include method, matched route, sanitized URL path, status code and server address.
- Handler errors and final statuses of 500 or greater mark the span as an error; handler errors are recorded.
- Place tracing toward the outside of the Use chain. An optional trace-ID response header is written before the handler.

## Metrics

| Name | Instrument | Unit |
| --- | --- | --- |
| http.server.request.duration | Histogram | seconds |
| http.server.active_requests | UpDownCounter | active count |
| http.server.request.body.size | Histogram | bytes; unknown Content-Length is omitted |
| http.server.response.body.size | Histogram | bytes written |

Attributes are method, matched route and status code; active requests omit the status.
Unmatched routes omit route attributes. Missing declared keys become empty strings in the backend.

Metrics registers instruments during construction and returns
`(ghttp.Middleware, error)`; handle errors before serving.
Names follow OTel conventions and are normalized by the selected backend, not the middleware.

## Options

| Option | Behavior |
| --- | --- |
| WithTraceIDHeader(name) | Optional response header; disabled by default |
| WithServerAddress(addr) | Fixed server address; otherwise Host without port |
| WithBodySizeMetrics(bool) | Body-size metrics; enabled by default |
| WithActiveRequests(bool) | Active-request counter; enabled by default |

A nil tracer or meter makes its corresponding middleware pass through.
See [Telemetry](telemetry.md) for interfaces and backend selection.
