# gotel/adapters/otelprometheus

独立 Go module：创建 OTel Prometheus exporter/reader、托管 Provider、独立 Registry 与抓取 handler。不依赖 OTLP exporter，不自动启动 HTTP 服务，不修改全局 Registry 或 OTel provider。

```go
import (
    "github.com/sofiworker/gk/gotel"
    oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
    "github.com/sofiworker/gk/gotel/adapters/otelprometheus"
)

provider, err := otelprometheus.New(ctx,
    otelprometheus.WithProviderOptions(
        oteladapter.WithServiceName("orders"),
        oteladapter.WithServiceVersion("1.2.3"),
        oteladapter.WithResourceAttributes(gotel.KV("deployment.environment.name", "production")),
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
// 挂载到应用已有的 HTTP mux，或者把 handler 接入其他 HTTP 框架。
mux.Handle("/metrics", provider.Handler())
```

Prometheus 配置抓取应用的 `/metrics`。指标通过抓取时的 reader 收集，`ForceFlush` 不会主动向 Prometheus 推送；它仅触发 Provider 中可 flush 的其他组件。

使用 `WithRegistry(registry)` 将已有应用 Registry 纳入抓取；`Registry()` 返回用于注册其他 collector 的应用 Registry。OTel exporter 使用私有 Registry，`Handler()` 合并两者的指标，直接调用 `Registry().Gather()` 只会得到应用注册的指标。这样无需向调用方 Registry 注册不可注销的 OTel 动态 collector。Shutdown 停止 SDK reader 的采集，应用 Registry 中原有 collector 保持可用。退出时使用新的、带超时的 context 调用 `provider.Shutdown(ctx)`。

`WithExporterOptions(...)` 接收官方 exporter 选项，可配置 namespace、名称转换、资源标签等；模块使用私有 exporter Registry，外部 `WithRegisterer` 不覆盖它。名称、单位和标签转换遵守官方 exporter 规则。`WithHandlerOptions(promhttp.HandlerOpts{...})` 配置 OpenMetrics、错误处理等抓取行为。应用 collector 与 OTel 指标名称发生冲突时，抓取 handler 返回收集错误，须通过 namespace 等配置避免冲突。

`WithProviderOptions(...)` 使用 [基础 OTel 模块](../otel/README.md) 的服务属性、View、采样、额外 reader 等配置。需要 Jaeger tracing 时，可以组合独立 [OTLP](../otlp/README.md) Provider 的 Tracer；也可通过 WithProviderOptions 传入外部 span exporter，统一由本 Provider 管理生命周期。

这是使用 OTel SDK 的 Prometheus 路线；需要直接使用原生 instrument 时使用 [原生 Prometheus adapter](../prometheus/README.md)。

本目录执行 `go mod tidy`、`go vet ./...`、`go test ./...`。测试实际抓取 HTTP handler，验证四类同步指标、异步指标、resource、OpenMetrics、应用 Registry 隔离和关闭行为。当前使用本地 replace，发布顺序与版本要求见 [核心说明](../../README.md#兼容性与开发)。

This optional module owns an OTel Prometheus reader/provider and exposes a scrape handler. The exporter uses a private registry; the handler merges it with an application registry without adding unchecked collectors to that registry. Registry().Gather() returns only application metrics. Shutdown stops OTel collection while preserving application collectors. The application mounts the handler and uses a fresh deadline context for shutdown. OTLP exporters remain a separate dependency.
