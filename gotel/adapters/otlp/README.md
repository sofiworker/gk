# gotel/adapters/otlp

独立 Go module，创建托管 OTel Provider，支持 tracing 和 metrics 的 OTLP HTTP/protobuf、gRPC 导出。使用官方 exporter，包括重试、TLS 和环境变量配置；不依赖 Prometheus SDK。

## Jaeger

Jaeger 通过 OTLP 接收 traces，无需 Jaeger 专用 exporter。对接已启用 OTLP 的 Jaeger：

```go
import (
    oteladapter "github.com/sofiworker/gk/gotel/adapters/otel"
    "github.com/sofiworker/gk/gotel/adapters/otlp"
)

provider, err := otlp.New(ctx,
    otlp.WithProtocol(otlp.ProtocolGRPC),
    otlp.WithEndpointURL("http://localhost:4317"),
    otlp.WithMetrics(false), // Jaeger 接收 tracing；不要向它发送 metrics。
    otlp.WithProviderOptions(
        oteladapter.WithServiceName("orders"),
        oteladapter.WithServiceVersion("1.2.3"),
        oteladapter.WithSamplingRatio(0.1),
    ),
)
if err != nil {
    return err
}
ctx, span := provider.Start(ctx, "create-order")
_ = ctx
span.End()
```

HTTP 方式改用 `ProtocolHTTP` 和 `http://localhost:4318`，默认路径为 `/v1/traces`。

## Collector / 同时导出指标

默认启用两种信号，协议默认 HTTP/protobuf：

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
// HTTP 分别发送到 /otel/v1/traces 和 /otel/v1/metrics。
```

Collector 的 receiver/pipeline 必须启用相应信号。`WithTraces(false)` 可只导出 metrics；两者都关闭是配置错误。HTTP/gRPC 接入均可使用 `WithTLSConfig` 配置自定义根证书或 mTLS。`http://` 显式选择明文，`https://` 选择 TLS；TLS 配置不能和明文 URL 混用。

`WithTraceEndpointURL` / `WithMetricEndpointURL` 设置各信号的完整 URL（HTTP 包括路径），优先于 `WithEndpointURL` 的基础 URL。gRPC URL 不允许自定义路径。URL 不能含用户名密码、query 或 fragment；鉴权使用 headers。未显式指定 endpoint/headers 时沿用官方 exporter 的 `OTEL_EXPORTER_OTLP_*` 环境变量配置；协议由 `WithProtocol` 选择，超时默认 10 秒、指标周期默认 60 秒。

构造不会探测接收端是否可达。后台导出失败由 SDK error handler 处理；主动 `ForceFlush` 与 `Shutdown` 返回相应错误。服务退出时使用新的 context：

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := provider.Shutdown(shutdownCtx); err != nil {
    return fmt.Errorf("shutdown telemetry: %w", err)
}
```

创建过程失败会关闭已创建的 exporter/reader。共享的服务属性、采样与 SDK 能力见 [基础 OTel 模块](../otel/README.md)。可将此 Provider 的 Tracer 与 [OTel Prometheus](../otelprometheus/README.md) 或原生 Prometheus Meter 分别注入业务，并分别关闭各自 Provider。

## 验证与发布

在本目录执行 `go mod tidy`、`go vet ./...`、`go test ./...`。测试使用本地 HTTP、HTTPS、gRPC、TLS gRPC 接收端，解析实际 OTLP protobuf 并检查资源、四类指标、鉴权头、错误及关闭流程；不依赖外部 Jaeger 服务，也不验证 Jaeger 的存储/UI。

当前使用本地 replace 联调。发布前先发布根模块和 otel 基础模块，再更新本模块的 require 并打 `gotel/adapters/otlp/vX.Y.Z` tag，见 [核心说明](../../README.md#兼容性与开发)。

This optional module exports traces and metrics using official OTLP HTTP/protobuf or gRPC exporters. Use traces-only mode for Jaeger; a Collector can receive both signals. Managed providers own batching, periodic metric collection, resource/sampling configuration and lifecycle without global registration. Tests use local protocol receivers, including TLS and failure cases; no external backend is required.
