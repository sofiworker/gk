# ghttp/adapters/gotel

把 `gotel` 的 `Tracer` / `Meter` 抽象适配为 `ghttp` 中间件。能力层之间不直接互相依赖，拼接放在本适配层。

包名为 `ghttpotel`（避免与被导入的 `gotel` 包冲突）：

```go
import (
    "github.com/sofiworker/gk/ghttp"
    ghttpotel "github.com/sofiworker/gk/ghttp/adapters/gotel"
)

s := ghttp.NewServer()
s.Use(
    ghttpotel.Tracing(tracer, ghttpotel.WithTraceIDHeader("X-Trace-Id")),
    ghttpotel.Metrics(meter),
)
```

## Tracing

- 从请求头 Extract 上游上下文（内置 `HeaderCarrier`），Start span，并把 span ctx 写回 `req.Raw` 且传给 handler。
- span 名 `"<METHOD> <route>"`；未匹配路由为 `"<METHOD> unmatched"`（低基数）。
- 属性：`http.request.method`、`http.route`（未匹配时省略）、`url.path`（经 `wire.LogToken` 净化）、`http.response.status_code`、`server.address`。
- handler 返回错误或最终状态码 >= 500 时 `SetStatus(Error)`；有错误时 `RecordError`。
- 注意：Tracing 应放在 `Use` 链中靠外层，以便覆盖其后中间件；`WithTraceIDHeader` 在 handler 之前写入响应头。

## Metrics

| 指标 | 类型 | 说明 |
| --- | --- | --- |
| `http.server.request.duration` | Histogram（秒） | 请求耗时 |
| `http.server.active_requests` | Counter（+1/-1） | gotel 的 Gauge 只能 Record，故用 Counter 加减 |
| `http.server.request.body.size` | Histogram（字节） | 取 Content-Length，未知（-1）时不记录 |
| `http.server.response.body.size` | Histogram（字节） | 已写出的响应体字节数 |

属性仅含 `http.request.method`、`http.route`（未匹配省略）、`http.response.status_code`；active_requests 不含状态码。

## 选项

- `WithTraceIDHeader(name)`：把 trace id 写入响应头（默认不写）。
- `WithServerAddress(addr)`：固定 `server.address`（默认取 Host 去端口）。
- `WithBodySizeMetrics(bool)`：开关请求/响应体大小指标（默认开）。
- `WithActiveRequests(bool)`：开关活跃请求数指标（默认开）。

`tracer` / `meter` 为 nil 时对应中间件透传。
