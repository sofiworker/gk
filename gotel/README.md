# gotel

[English](README.en.md) | 中文

可观测性统一抽象（跨追踪/指标系统的接口与值类型）。

核心包只依赖标准库，提供 `Span`、`Tracer`、`Meter` 和类型明确的选项，不自动选择或初始化任何后端。`Tracer` 与 `Meter` 独立注入，`Provider` 只是两种能力与 `Shutdowner` 的可选组合。

## 可选后端与依赖隔离

| 导入路径 | 能力 | 独立 module |
| --- | --- | --- |
| `github.com/sofiworker/gk/gotel` | 公共接口、配置、错误 | 属于根 module，仅依赖标准库 |
| `github.com/sofiworker/gk/gotel/adapters/otel` | OpenTelemetry tracing / metrics | 是，仅使用此模块时引入 OTel |
| `github.com/sofiworker/gk/gotel/adapters/prometheus` | 原生 Prometheus metrics | 是，仅使用此模块时引入 Prometheus |
| `github.com/sofiworker/gk/gotel/adapters/otlp` | 托管 OTel tracing / metrics，经 OTLP HTTP/gRPC 导出到 Jaeger、Collector 等 | 是，依赖 OTel 与 OTLP exporter，不依赖 Prometheus |
| `github.com/sofiworker/gk/gotel/adapters/otelprometheus` | 托管 OTel metrics、Prometheus exporter 与抓取 handler | 是，依赖 OTel 与 Prometheus，不依赖 OTLP exporter |

与 Rust 的独立 crate 类似，每个后端目录有自己的 `go.mod` / `go.sum`。根 module 不 require 后端模块；只使用 `gotel` 的项目不会因此引入厂商 SDK。Go 的 build tag 无法实现这层隔离：`go mod tidy` 会检查带自定义 tag 的代码，所以后端采用 module 边界。

现有 SDK 可以通过 [OTel](adapters/otel/README.md) 的 `NewTracer` / `NewMeter` 借用；其生命周期仍由应用负责。需要托管 SDK 时，使用 `otel.NewProvider` 或下列开箱即用模块：

- [OTLP](adapters/otlp/README.md)：创建 HTTP/protobuf 或 gRPC exporter、批量 span processor 和周期 metric reader；Jaeger 使用 traces-only OTLP，Collector 可以接收两种信号。
- [OTel Prometheus](adapters/otelprometheus/README.md)：创建 exporter/reader、独立 Registry 与抓取 handler，应用自行挂载 HTTP 路由。
- [原生 Prometheus](adapters/prometheus/README.md)：绕过 OTel SDK，直接实现 Meter。

所有托管 Provider 共享 `WithServiceName`、`WithServiceVersion`、`WithResourceAttributes`、`WithSamplingRatio` 等选项，提供 `ForceFlush(ctx)` 和幂等的 `Shutdown(ctx)`，不修改全局 provider。应用退出时使用新的、带超时的 context 调用 Shutdown，让待发送数据完成导出。`Flusher` 是核心中的可选能力接口。

可组合 OTLP Tracer 与 OTel/原生 Prometheus Meter；同一应用分别关闭其拥有的 Provider。核心契约覆盖 tracing 和四类同步指标；native OTel span links、异步指标和 View 可通过托管 Provider 的 `TracerProvider()` / `MeterProvider()` 使用。日志信号没有纳入当前 gotel 契约。

## 指标契约

| 类型 | 操作 | 语义 |
| --- | --- | --- |
| `Counter` | `Add` / `Increment` | 非负增量，累计总数 |
| `UpDownCounter` | `Add` | 正负增量，例如活跃请求数 |
| `Gauge` | `Record` | 设置当前值 |
| `Histogram` | `Record` | 记录数值分布 |

指标与适配器支持并发调用。非有限值（NaN、Inf）被忽略；Counter 的负增量被忽略。

```go
requests, err := meter.Counter("http.requests",
    gotel.WithDescription("Completed requests"),
    gotel.WithAttributeKeys("method", "route"),
)
if err != nil {
    return err
}
requests.Increment(ctx, gotel.KV("method", "GET"), gotel.KV("route", "/users/:id"))
```

使用 `WithUnit`、`WithDescription`、`WithAttributeKeys`、`WithHistogramBoundaries` 配置指标。所有属性键须在创建时声明：未声明的属性被忽略，缺失属性的值为 `""`，重复属性取最后一个值。这让 Prometheus 的固定标签与 OTel 的属性模型保持一致；避免使用原始 URL、用户 ID 等高基数属性。`KeyValue.Value` 可使用字符串、布尔值、整数、浮点数及其切片；OTel 保留支持的值类型，其他值使用 `fmt.Sprint` 转换，Prometheus 标签统一转为字符串。

创建指标返回 `(Instrument, error)`；配置错误可通过 `errors.Is(err, gotel.ErrInvalidInstrument)` 判断。直方图桶边界必须有限且严格递增；未配置时使用后端默认值。Span 选项使用 `WithSpanKind`、`WithSpanAttributes`、`WithSpanStartTime` / `WithSpanEndTime`，不透传厂商 SDK 选项。

## 兼容性与开发

本次为破坏性 API 调整：`Meter` 各创建方法增加 `error` 返回值，增加 `UpDownCounter`；原来的任意类型 option 改为类型明确的函数选项。HTTP `Metrics` 也改为返回 `(ghttp.Middleware, error)`，必须在启动服务前处理创建错误。

根目录 `go test ./...` 不进入嵌套 module。分别检查：

```sh
go test ./gotel ./ghttp/adapters/gotel
make check-adapters
```

也可以分别进入 `gotel/adapters/otel`、`gotel/adapters/prometheus`、`gotel/adapters/otlp`、`gotel/adapters/otelprometheus` 执行 `go mod tidy`、`go vet ./...`、`go test ./...`。无需全局 `go.work`，避免未使用后端影响根 module 的依赖选择。CI 分别检查这些 module。OTLP 测试启动本地 HTTP/gRPC 接收端并解析实际 protobuf 请求；Prometheus 测试执行真实 handler 抓取，不依赖外部 Jaeger/Prometheus 服务。

后端模块当前使用 `v0.0.0` 加本地 `replace` 联调，尚不能直接通过远程版本安装。发布顺序：根模块 → `otel` 基础适配模块 → 依赖它的 `otlp` / `otelprometheus`；原生 `prometheus` 模块只依赖根模块。每步将 require 更新为已发布版本，并按 `gotel/adapters/<模块>/vX.Y.Z` 分别打 tag。下游不会继承模块中的 replace。
