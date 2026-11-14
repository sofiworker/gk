# gotel/adapters/otel

独立 Go module：可以借用调用方已有的 OpenTelemetry `trace.Tracer` / `metric.Meter`，也可以创建拥有 SDK 生命周期的 Provider。只有显式使用此 module 才引入 OTel 依赖；核心 `gotel` 不依赖本模块，本模块不依赖 OTLP/Prometheus exporter。

## 借用现有 SDK

```go
import (
    "github.com/sofiworker/gk/gotel"
    oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
)

// traceProvider / meterProvider 由应用初始化，可以配置任意 exporter / reader。
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

默认使用 W3C Trace Context + Baggage 传播，不修改全局 provider / propagator。可以用 `WithPropagator` 注入其他传播协议。`SpanContext.Serialize` 输出 W3C traceparent / tracestate；跨 adapter 的显式 Inject 使用公共 TraceID / SpanID / IsSampled，只有原生包装上下文保留完整 tracestate。

Counter、UpDownCounter、Histogram、Gauge 分别映射到 OTel Float64 instrument，保留逻辑名称、单位、描述和显式桶边界。属性遵守核心的固定键结构。`NewTracer` / `NewMeter` 不关闭传入的 SDK。

## 托管 Provider

```go
provider, err := oteladapter.NewProvider(ctx,
    oteladapter.WithServiceName("orders"),
    oteladapter.WithServiceVersion("1.2.3"),
    oteladapter.WithResourceAttributes(gotel.KV("deployment.environment.name", "production")),
    oteladapter.WithSamplingRatio(0.1),
    oteladapter.WithTraceExporter(spanExporter),
    oteladapter.WithMetricReader(metricReader),
)
if err != nil {
    return err
}
// provider 同时实现 gotel.Provider / gotel.Flusher。
// 提交数据后，可用 provider.ForceFlush(ctx) 主动触发导出。
// 退出时用新的、带超时的 context 调用 provider.Shutdown(ctx)。
```

`NewProvider` 从默认 OTel resource 开始合并自定义属性，显式服务名/版本优先；不修改全局 providers。默认使用 parent-based 100% 采样、5 秒批量等待与 30 秒导出超时。`WithSamplingRatio` 取值为 [0,1]，`WithSampler` 可以直接指定 SDK 采样器；`WithBatchOptions` 配置批量等待、队列和批量大小；`WithMetricOptions` 配置 View 等 SDK 能力；`WithProviderPropagator` 配置传播协议，默认仍是 W3C Trace Context + Baggage。

构造成功后 Provider 拥有传入的 span exporter 和所有 metric reader；构造失败时所有权仍属于调用方。`ForceFlush` 与 `Shutdown` 均尝试两种信号并合并错误。Shutdown 幂等，并发调用共享同一结果；即使某个信号失败，也会尝试关闭另一个。没有配置 exporter/reader 的信号保留 SDK API 与上下文传播，但不导出。

`TracerProvider()` / `MeterProvider()` 暴露 native SDK，支持 span links、异步 instrument、回调和 View 等能力，不必在核心契约里复制全部 OTel API。此处不包含 logs 接口。传入和返回的 SDK 对象不得由多个 owner 重复管理生命周期。

需要现成 exporter 时使用独立的 [OTLP](../otlp/README.md)（Jaeger/Collector）或 [OTel Prometheus](../otelprometheus/README.md) module。

初始化参数错误可判断 `errors.Is(err, oteladapter.ErrInvalidConfig)`；显式 Inject 的非法 ID 可判断 `ErrInvalidSpanContext`；指标配置错误沿用 `gotel.ErrInvalidInstrument`，SDK 创建错误保留错误链。

本地执行 `go mod tidy`、`go vet ./...`、`go test ./...`；发布与版本要求见 [核心文档](../../README.md#兼容性与开发)。当前父 module 的 v0.0.0 + replace 仅供本地联调，发布前必须改为实际已发布版本。

This optional module borrows API objects through NewTracer/NewMeter or owns an SDK through NewProvider. Managed providers merge resources, use parent-based sampling and batch processing, and implement force-flush and idempotent shutdown. They do not modify global providers. Native SDK access supports links, views and asynchronous metrics. Exporter integrations remain separate optional modules. Run checks in this directory; root `go test ./...` excludes nested modules.
