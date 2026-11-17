# OTLP export

**English** | [简体中文](zh-CN/otlp.md) | [Documentation](README.md)

[Source](../otel/adapters/otlp/) · `github.com/sofiworker/keel/otel/adapters/otlp`

This Go 1.26.0+ module owns official OTLP HTTP/protobuf or gRPC exporters,
batch span processing, metric readers and SDK lifecycle. It does not register global providers.

## Jaeger tracing

Jaeger accepts traces over OTLP; a Jaeger-specific exporter is unnecessary:

```go
import (
    oteladapter "github.com/sofiworker/keel/otel/adapters/otel"
    "github.com/sofiworker/keel/otel/adapters/otlp"
)

provider, err := otlp.New(ctx,
    otlp.WithProtocol(otlp.ProtocolGRPC),
    otlp.WithEndpointURL("http://localhost:4317"),
    otlp.WithMetrics(false),
    otlp.WithProviderOptions(
        oteladapter.WithServiceName("orders"),
        oteladapter.WithServiceVersion("1.2.3"),
        oteladapter.WithSamplingRatio(0.1),
    ),
)
if err != nil {
    return err
}
_, span := provider.Start(ctx, "create-order")
span.End()
```

For HTTP, select ProtocolHTTP and port 4318. The default trace path is /v1/traces.

## Collector and metrics

Both signals are enabled by default and HTTP/protobuf is the default protocol:

```go
provider, err := otlp.New(ctx,
    otlp.WithEndpointURL("https://collector.example.com/otel"),
    otlp.WithHeaders(map[string]string{"Authorization": "Bearer " + token}),
    otlp.WithTimeout(10*time.Second),
    otlp.WithMetricInterval(30*time.Second),
    otlp.WithProviderOptions(oteladapter.WithServiceName("orders")),
)
if err != nil {
    return err
}
```

HTTP sends to /otel/v1/traces and /otel/v1/metrics. Enable matching receiver/pipelines.
WithTraces(false) selects metrics only; disabling both signals is invalid.
WithTLSConfig supports custom roots and mTLS. http:// selects plaintext; https:// selects TLS.
Do not combine a TLS config with a plaintext URL.

WithTraceEndpointURL/WithMetricEndpointURL override the base URL with complete signal URLs.
HTTP includes the path; gRPC forbids custom paths. URLs cannot contain user information,
query strings or fragments; use headers for authentication.
Official OTEL_EXPORTER_OTLP_* settings supply endpoints/headers when not explicitly set.
WithProtocol selects protocol. Timeout defaults to ten seconds and metric interval to 60 seconds.

Construction does not test receiver reachability. The SDK error handler handles background failures;
ForceFlush and Shutdown return their errors. Shut down with a fresh context:

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := provider.Shutdown(shutdownCtx); err != nil {
    return fmt.Errorf("shutdown telemetry: %w", err)
}
```

Construction failure closes resources it has already created. See the
[base adapter](otel-adapter.md) for shared options. Compose an OTLP tracer with
[OTel Prometheus](otel-prometheus.md) or [native Prometheus](prometheus.md) metrics
and close each owned provider.

## Validation and publishing

Tests decode actual requests at local HTTP, HTTPS, gRPC and TLS gRPC receivers.
They check resources, all four instrument types, headers, failures and shutdown;
they require no external Jaeger and do not verify its storage or UI.
Run checks in the module directory; the root test command excludes it.
See [Development](development.md#checks) and [Release process](release.md) for module ordering
and the `otel/adapters/otlp/vX.Y.Z` tag prefix.
