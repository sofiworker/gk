# gotel/adapters/prometheus

独立 Go module：把调用方的 Prometheus Registerer 转为 `gotel.Meter`，只实现指标能力，可以与其他后端的 Tracer 组合。核心 `gotel` 不 require 本模块。

```go
import (
    prom "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/sofiworker/gk/gotel"
    promadapter "github.com/sofiworker/gk/gotel/adapters/prometheus"
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
// 将 handler 挂载到应用选择的路由，不会自动启动 HTTP 服务。
handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
_ = handler
```

| gotel instrument | Prometheus |
| --- | --- |
| Counter | CounterVec.Add / Inc |
| UpDownCounter | GaugeVec.Add |
| Gauge | GaugeVec.Set |
| Histogram | HistogramVec.Observe |

逻辑名称和标签键转换为 ASCII 字母、数字、下划线：其他字符替换为 `_`，数字开头补 `_`。Counter 名称补 `_total`；单位 `s` / `By` 分别补 `_seconds` / `_bytes`，其余单位不增加后缀。示例：`http.server.request.duration` + `s` → `http_server_request_duration_seconds`；`http.route` → `http_route`。已有对应后缀不重复添加。

属性键必须通过 `WithAttributeKeys` 预先声明，缺失值为 `""`，未声明的属性被忽略，重复值取最后一个，值使用 `fmt.Sprint` 转成标签字符串。转换后重复的标签、`__` 前缀标签及直方图的 `le` 标签返回 `gotel.ErrInvalidInstrument`。

同一个 Meter 内相同定义复用 collector；同一转换名称对应不同定义返回 `ErrInstrumentConflict`。外部 Registry 的注册冲突通过 Register 返回并保留错误链。`NewMeter(nil)` 返回 `ErrInvalidConfig`，不使用全局默认 Registry，不自动注销调用方的 collector。

本地执行 `go mod tidy`、`go vet ./...`、`go test ./...`；发布与版本要求见 [核心文档](../../README.md#兼容性与开发)。当前父 module 的 v0.0.0 + replace 仅供本地联调，发布前必须改为实际已发布版本。

This optional module implements only metrics against a caller-owned Prometheus Registerer. Names and label keys are normalized, attribute keys are declared upfront, and registration errors are returned during initialization. The application owns the registry and scrape handler; the adapter does not modify global state or start a server.
