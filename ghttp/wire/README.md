# ghttp/wire

> 开发中（pre-v1.0.0），API 不承诺向后兼容。

HTTP 线格式层的公共原语，由 `ghttp`（server）、`ghttp/client` 与用户代码共用。只依赖标准库。

```text
ghttp (server) ──→ ghttp/wire ←── ghttp/client
```

线格式是同一份规范，两端各写一份必然漂移，所以放在这里。**策略**（重试、重连、错误映射成哪个状态码）不在本包，留在各端。

## 内容

| 能力 | API | 说明 |
|---|---|---|
| media-type | `MediaType`、`ContentTypeIn`、`ContentType*` 常量 | 取 `;` 之前的部分，去空白并转小写；空 Content-Type 放行 |
| 严格解码 | `DecodeJSON`、`DecodeXML`、`ErrInvalidFormat` | 只接受恰好一个值，拒绝尾随内容（含顶层 `]`、`}`） |
| 解码选项 | `RequireBody()`、`DisallowUnknownFields()` | 默认宽松：空体返回 nil，未知字段放行 |
| SSE | `ReadSSEEvent`、`AppendSSEField`、`StripSSELineBreaks`、`SSEEvent` | 遵循 WHATWG 规范：多行 data、CRLF、注释心跳、id 中的 NUL |
| 日志 | `LogToken` | 转义控制字符，防止伪造日志行；不做脱敏 |
| 状态码契约 | `StatusCoder`、`StatusOf` | `ghttp.HTTPError` 与 `*client.Error` 都实现它 |

## 解码语义

```go
// client：解析响应体，默认宽松（服务端新增字段不会导致失败）
err := wire.DecodeJSON(resp.Body, &v, resp.ContentLength)

// server：解析请求体，严格
err := wire.DecodeJSON(r.Body, &v, r.ContentLength,
    wire.RequireBody(), wire.DisallowUnknownFields())
```

| 输入 | 默认 | `RequireBody()` |
|---|---|---|
| 空体，长度未知或为 0 | `nil`，目标为零值 | `ErrInvalidFormat` |
| 声明了正长度却为空（截断） | `ErrInvalidFormat` | `ErrInvalidFormat` |
| 首个值之后有非空白内容 | `ErrInvalidFormat` | `ErrInvalidFormat` |
| 超出 `http.MaxBytesReader` 限额 | 错误链中含 `*http.MaxBytesError`，**不**包装 `ErrInvalidFormat` | 同左 |

格式错误同时保留底层错误：可以用 `errors.Is(err, wire.ErrInvalidFormat)` 判断类别，也可以用 `errors.As` 取到 `*json.SyntaxError`。体量超限与格式错误刻意区分开，这样 server 能返回 413 而不是 400。

`ghttp.ReadJSON` 的映射是：媒体类型不符返回 415，超限返回 413，格式错误返回 400，底层错误保存在 `HTTPError.Cause` 中。

## 状态码契约

```go
if wire.StatusOf(err) == http.StatusNotFound { ... }
```

`StatusOf` 会沿错误链查找。契约本身不含任何策略：server 不会因为下游 client 错误实现了 `StatusCoder`，就把下游状态码透传给自己的调用方。
