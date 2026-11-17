# Telemetry

**English** | [简体中文](zh-CN/telemetry.md) | [Documentation](README.md)

[Source](../otel/) · `github.com/sofiworker/keel/otel`

Unified observability abstractions (interfaces and value types across tracing/metrics systems).

The core package imports only the standard library. `Tracer` and `Meter` are independently injectable; `Provider` is an optional composition with `Shutdowner`. No backend is selected or initialized implicitly.

## Optional backend modules

| Import | Capabilities | Dependency boundary |
| --- | --- | --- |
| `github.com/sofiworker/keel/otel` | Interfaces, typed options, errors | Root module; standard library imports only |
| `github.com/sofiworker/keel/otel/adapters/otel` | OTel tracing and metrics | Separate `go.mod` / `go.sum` |
| `github.com/sofiworker/keel/otel/adapters/prometheus` | Native Prometheus metrics | Separate `go.mod` / `go.sum` |
| `github.com/sofiworker/keel/otel/adapters/otlp` | Managed traces/metrics over OTLP HTTP or gRPC | Separate module; no Prometheus dependency |
| `github.com/sofiworker/keel/otel/adapters/otelprometheus` | Managed OTel Prometheus exporter and scrape handler | Separate module; no OTLP exporter dependency |

Like separate Rust crates, adapters are opt-in modules. The root module does not require them, so using gotel does not add vendor SDK dependencies. Build tags cannot provide this isolation because `go mod tidy` examines code with custom build tags too.

Use [OTLP](otlp.md) to export traces to Jaeger or both signals to a Collector, or [OTel Prometheus](otel-prometheus.md) for an exporter/reader and scrape handler. These managed providers configure SDK resources, sampling and batching and own flush/shutdown. They do not set global providers or start HTTP servers. Use `WithProviderOptions(otel.WithServiceName(...), ...)` for shared options. Shut down with a fresh context and a deadline.

The base [OTel adapter](otel-adapter.md) also supports borrowing caller-owned APIs with NewTracer/NewMeter, or creating a managed SDK with NewProvider. Native links, asynchronous instruments and views are available through TracerProvider()/MeterProvider(); logs are outside the current gotel contract. OTLP tracing can be combined with OTel or [native Prometheus](prometheus.md) metrics. `Flusher` is an optional core interface.

## Contracts

`Counter.Add` accepts nonnegative increments; `UpDownCounter.Add` accepts signed increments. `Gauge.Record` sets the current value and `Histogram.Record` observes a distribution. Instruments support concurrent use. NaN and infinite observations, and negative Counter increments, are ignored.

All creation methods return `(Instrument, error)`. Use `WithUnit`, `WithDescription`, `WithAttributeKeys` and `WithHistogramBoundaries`. Declare the complete attribute schema at creation: undeclared attributes are ignored, missing values become `""`, and duplicate values use the last occurrence. OTel preserves supported scalar/slice types and stringifies unsupported values with `fmt.Sprint`; Prometheus stringifies all label values. Avoid high-cardinality values. Explicit histogram boundaries must be finite and strictly increasing; omitted boundaries use backend defaults. Invalid portable configuration wraps `ErrInvalidInstrument`.

Span options are backend-independent: `WithSpanKind`, `WithSpanAttributes`, `WithSpanStartTime` and `WithSpanEndTime`.

## Migration and development

The root module and the native `prometheus` adapter require Go 1.25.12+. The `otel`, `otlp` and `otelprometheus` adapters require Go 1.26.0+ for OpenTelemetry v1.47. CI checks each adapter on its minimum supported Go version and the latest stable release.

Breaking changes: Meter creation methods now return errors, Meter adds UpDownCounter, and untyped options become typed function options. HTTP `Metrics` now returns `(ghttp.Middleware, error)`; handle registration errors before serving.

Root `go test ./...` excludes nested modules. Run `go test ./otel ./http/adapters/gotel` and `make check-adapters`, or run `go mod tidy`, `go vet ./...`, `go test ./...` in each of the four adapter directories. CI checks them separately. OTLP tests decode actual requests at local HTTP/gRPC receivers; Prometheus tests scrape the handler, without external backend services. No global go.work is required.

The adapters currently use v0.0.0 and local replace directives and are not remotely installable yet. Publish and pin actual versions in dependency order: root, base otel adapter, then otlp/otelprometheus; native prometheus only depends on root. Tag each module with `otel/adapters/<module>/vX.Y.Z`. Consumers do not inherit replace directives.
