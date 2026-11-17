# Native Prometheus metrics

**English** | [简体中文](zh-CN/prometheus.md) | [Documentation](README.md)

[Source](../otel/adapters/prometheus/) · `github.com/sofiworker/keel/otel/adapters/prometheus`

This separate Go 1.25.12+ module implements Meter using a caller-owned Prometheus Registerer.
It supplies metrics only and can be combined with another backend's tracer.

```go
import (
    prom "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    gotel "github.com/sofiworker/keel/otel"
    promadapter "github.com/sofiworker/keel/otel/adapters/prometheus"
)

registry := prom.NewRegistry()
meter, err := promadapter.NewMeter(registry)
if err != nil {
    return err
}
requests, err := meter.Counter("http.requests", gotel.WithAttributeKeys("method", "route"))
if err != nil {
    return err
}
requests.Increment(ctx, gotel.KV("method", "GET"), gotel.KV("route", "/users/:id"))
handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
_ = handler
```

The application mounts the handler; no HTTP server starts automatically.

| Portable instrument | Prometheus |
| --- | --- |
| Counter | CounterVec.Add / Inc |
| UpDownCounter | GaugeVec.Add |
| Gauge | GaugeVec.Set |
| Histogram | HistogramVec.Observe |

Names and label keys normalize to ASCII letters, digits and underscores; other characters
become underscores and leading digits gain an underscore. Counters gain _total.
Units s/By gain _seconds/_bytes; existing suffixes are not duplicated.
For example, http.server.request.duration with unit s becomes
http_server_request_duration_seconds.

Declare attribute keys at construction. Missing values become empty strings, undeclared keys
are ignored, repeated values use the last, and values stringify with fmt.Sprint.
Duplicate normalized labels, __ prefixes and the histogram's le label return ErrInvalidInstrument.

Identical definitions on one Meter reuse collectors. Different definitions sharing a normalized
name return ErrInstrumentConflict. Registry registration errors retain their causes.
NewMeter(nil) returns ErrInvalidConfig. The adapter does not use the global registry or
unregister caller-owned collectors.

See [Telemetry](telemetry.md), [Development](development.md#checks) and
[Release process](release.md). Local v0.0.0 replacements must become published versions
before downstream installation.
