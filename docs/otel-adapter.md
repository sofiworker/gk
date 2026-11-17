# OpenTelemetry adapter

**English** | [简体中文](zh-CN/otel-adapter.md) | [Documentation](README.md)

[Source](../otel/adapters/otel/) · `github.com/sofiworker/keel/otel/adapters/otel`

An independent Go module requiring Go 1.26.0+ for OTel v1.47.
It can borrow existing trace/metric API objects or own an SDK provider.
It does not depend on OTLP or Prometheus exporters.

## Borrow existing APIs

```go
import (
    gotel "github.com/sofiworker/keel/otel"
    oteladapter "github.com/sofiworker/keel/otel/adapters/otel"
)

tracer, err := oteladapter.NewTracer(traceProvider.Tracer("my-service"))
if err != nil {
    return err
}
meter, err := oteladapter.NewMeter(meterProvider.Meter("my-service"))
if err != nil {
    return err
}
requests, err := meter.Counter("http.requests", gotel.WithAttributeKeys("method"))
if err != nil {
    return err
}
ctx, span := tracer.Start(ctx, "request", gotel.WithSpanKind(gotel.SpanKindServer))
defer span.End()
requests.Increment(ctx, gotel.KV("method", "GET"))
```

The application owns borrowed SDK objects; NewTracer/NewMeter do not shut them down.
W3C Trace Context and Baggage are the default propagation, without changing globals.
WithPropagator configures alternatives. Serialize emits traceparent/tracestate;
explicit injection from another adapter uses portable IDs and sampling, with full tracestate
retained only by native wrapped contexts.

Counter, UpDownCounter, Histogram and Gauge map to OTel Float64 instruments.
Logical names, units, descriptions, explicit boundaries and fixed attribute schemas are preserved.

## Managed provider

```go
provider, err := oteladapter.NewProvider(ctx,
    oteladapter.WithServiceName("orders"),
    oteladapter.WithServiceVersion("1.2.3"),
    oteladapter.WithResourceAttributes(gotel.KV("deployment.environment.name", "development")),
    oteladapter.WithSamplingRatio(0.1),
    oteladapter.WithTraceExporter(spanExporter),
    oteladapter.WithMetricReader(metricReader),
)
if err != nil {
    return err
}
```

NewProvider merges the default resource and supplied attributes; explicit service name/version win.
Defaults are parent-based 100% sampling, a five-second batch wait and a 30-second export timeout.
WithSamplingRatio accepts [0,1]; WithSampler overrides it. WithBatchOptions configures batching;
WithMetricOptions configures views; WithProviderPropagator configures propagation.
Global providers remain unchanged.

After successful construction, the provider owns the supplied exporter/readers.
On construction failure, ownership stays with the caller.
ForceFlush and Shutdown attempt both signals and combine errors. Shutdown is idempotent;
concurrent calls share its result. Signals without exporters/readers retain SDK APIs and propagation
but do not export. Use a fresh deadline context when shutting down.

TracerProvider()/MeterProvider() expose native links, asynchronous instruments, callbacks and views.
Logs are outside the portable contract. Do not assign the same SDK object's lifetime to multiple owners.
Ready-made exporters are in [OTLP](otlp.md) and [OTel Prometheus](otel-prometheus.md).

Configuration errors match ErrInvalidConfig, invalid injected IDs match ErrInvalidSpanContext,
and portable instrument errors match gotel.ErrInvalidInstrument; SDK causes remain wrapped.
See [development checks](development.md#checks), [installation](installation.md) and
[release process](release.md) for local replacements and module versions.
