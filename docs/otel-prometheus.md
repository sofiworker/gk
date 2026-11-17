# OTel Prometheus export

**English** | [简体中文](zh-CN/otel-prometheus.md) | [Documentation](README.md)

[Source](../otel/adapters/otelprometheus/) · `github.com/sofiworker/keel/otel/adapters/otelprometheus`

This independent Go 1.26.0+ module owns an OTel Prometheus exporter/reader, provider and
private registry, exposing a scrape handler. It does not depend on OTLP exporters,
start an HTTP server or modify global registries/providers.

```go
import (
    gotel "github.com/sofiworker/keel/otel"
    oteladapter "github.com/sofiworker/keel/otel/adapters/otel"
    "github.com/sofiworker/keel/otel/adapters/otelprometheus"
)

provider, err := otelprometheus.New(ctx,
    otelprometheus.WithProviderOptions(
        oteladapter.WithServiceName("orders"),
        oteladapter.WithServiceVersion("1.2.3"),
        oteladapter.WithResourceAttributes(gotel.KV("deployment.environment.name", "development")),
    ),
)
if err != nil {
    return err
}
counter, err := provider.Counter("http.requests", gotel.WithAttributeKeys("method", "route"))
if err != nil {
    return err
}
counter.Increment(ctx, gotel.KV("method", "GET"), gotel.KV("route", "/orders/:id"))
mux.Handle("/metrics", provider.Handler())
```

Configure Prometheus to scrape the application's /metrics endpoint.
Metrics collect at scrape time; ForceFlush does not push them to Prometheus.
It only flushes other flushable provider components.

WithRegistry includes an application registry. Registry() returns that application registry;
Handler() merges it with the private exporter registry.
Registry().Gather() therefore returns only application metrics.
This avoids adding non-unregisterable dynamic OTel collectors to the caller's registry.
Shutdown stops SDK collection while preserving application collectors; use a fresh deadline context.

WithExporterOptions accepts official exporter settings for namespaces, name conversion and
resource labels. The exporter registry remains private; an external WithRegisterer does not
replace it. Name/unit/label conversion follows the official exporter.
WithHandlerOptions configures OpenMetrics and scrape-error behavior.
Conflicting names between application and OTel metrics fail gathering; avoid them with namespaces.

WithProviderOptions takes shared [OTel options](otel-adapter.md), including views, sampling
and additional readers. Combine with a separate [OTLP](otlp.md) tracer, or pass an external
span exporter through provider options to share its lifecycle.
For instruments without an OTel SDK, choose [native Prometheus](prometheus.md).

Tests scrape the actual handler and check all four synchronous instruments, asynchronous
metrics, resources, OpenMetrics, registry isolation and shutdown.
Run checks in this module; see [Development](development.md#checks) and [Release process](release.md).
