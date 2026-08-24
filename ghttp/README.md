# ghttp

[English](README.en.md) | 中文

基于标准库 `net/http` 的、面向 Go 泛型的 typed HTTP 路由框架。入口按输入组合分函数（`GetParams` / `PostBody` / `PostParamsBody` 等），handler 收裸参数、类型全推断、无包裹容器；params 经 struct tag（`path:` / `query:` / `header:`）绑定，请求体经 `RequestDecoder` 解码，输出经 `OutputSpec` 编码；需完全接管响应时用 `RawHandle`。

性能取向：纯 `net/http` 地基，无 fasthttp、无自管 TCP；命中热路径零反射、零分配（`dispatchRaw` 命中 0 alloc），性能红利只来自池化上下文与零反射 codec。

## 特性

- 路由：radix 树（gin 同源）+ 路径参数 + catch-all + 尾斜杠重定向（TSR）
- typed 入口：泛型自由函数，注册期建计划（反射一次）、请求期零反射
- 中间件：全局 `Use` 对命中/未命中一视同仁；无全局中间件时走零开销直连
- 内建中间件：RequestID、Logger、LimitBody、Recovery、CORS（含预检）、Timeout、BasicAuth
- 静态资源：`Static` / `StaticFS`（`os.DirFS` 与 `embed.FS`）、`File`、SPA history 回退
- 健康检查：`Health`（liveness）、`Ready`（readiness）、`NewReadinessGate`（运行时开关）
- 生命周期：`Run` / `RunTLS` / `Serve` / `ServeTLS` / `Shutdown` / `Close` / `RunGraceful`

## 快速开始

```go
s := ghttp.New()
s.Use(ghttp.RequestID(), ghttp.Logger(), ghttp.Recovery())

type getUserParams struct {
	ID int64 `path:"id" validate:"min=1"`
}
ghttp.GetParams(s, "/users/{id}", ghttp.JSON[User](),
	func(ctx context.Context, p getUserParams) (User, error) {
		return findUser(ctx, p.ID)
	})

_ = s.RunGraceful(":8080") // 收到 SIGINT/SIGTERM 优雅退出
```

## 统一错误链

typed handler / codec 返回的 `error` 经**单一出口**分类为 HTTP 状态码：

- 业务错误实现 `StatusCoder`（`HTTPStatus() int`）即自带状态码；
- 否则按框架哨兵映射（`ErrInvalidInput`/`ErrValidation`→400、`ErrUnsupportedMediaType`→415 等）；
- 兜底 500。

错误体默认脱敏（只回通用文案，`err.Error()` 细节仅进日志），默认输出 JSON `{"error":{"code","message"}}`。

```go
s := ghttp.New(
	ghttp.WithExposeErrorDetails(true),          // 回传 err 明文（调试用）
	ghttp.WithErrorRenderer(myRenderer),         // 自定义错误体（如 RFC 9457）
	ghttp.WithErrorHook(func(r *http.Request, status int, err error) { /* 观测 */ }),
)
```

404/405 也输出统一错误体，可用 `WithNotFoundHandler` / `WithMethodNotAllowedHandler` 定制。

## 可观测性

- `Request.MatchedRoute()`：命中的低基数路由模板（如 `/users/:id`），适合作 metrics/tracing/日志的路由维度。
- `Request.ClientIP()` / `RemoteIP()`：按可信代理模型解析真实客户端 IP。**默认不信任任何转发头**（防伪造）；配置 `WithTrustedProxies(...)` 后，仅当直连对端可信才回溯 `X-Forwarded-For` / `X-Real-IP`（头名可用 `WithForwardedHeaders` 覆盖）。
- `Logger` / `LoggerWith`：输出结构化 `AccessLog`（含 `Method`/`Path`/`Route`/`Status`/`Elapsed`/`ClientIP`/`BytesOut`/`Err`）。

## 参数校验

params 字段支持标量绑定：`string`、`bool`、`int/8/16/32/64`、`uint/8/16/32/64`、`float32/64`（越界报 400）。可用 `validate` tag 声明规则（注册期编译为闭包，请求期零 tag 解析）：

| 规则 | 适用 | 说明 |
|---|---|---|
| `required` | 全部 | 缺失即 400 |
| `min=N` / `max=N` | 数值比大小；字符串比长度 | |
| `len=N` | 字符串长度恰为 N；数值等于 N | |
| `oneof=a b c` | 全部 | 空格分隔候选之一 |
| `email` | 字符串 | 简化邮箱校验 |

请求体实现 `Validator`（`Validate() error`）即在解码后自动校验；失败统一归 `ErrValidation`（→400），若返回值自带 `StatusCoder` 则透传其状态码。不使用校验时零额外开销。

```go
type CreateOrder struct {
	Amount int `json:"amount"`
}
func (o CreateOrder) Validate() error {
	if o.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	return nil
}
```

## 优雅退出与认证

```go
gate, checker := ghttp.NewReadinessGate("startup")
ghttp.Ready(s, "/readyz", time.Second, checker)
gate.Set(true, nil)

_ = s.RunGraceful(":8080",
	ghttp.WithReadinessGate(gate),           // 收到信号先摘流（LB 感知）
	ghttp.WithDrainDelay(3*time.Second),     // 等 LB 摘流
	ghttp.WithShutdownTimeout(30*time.Second), // 排水超时后强制关闭
)

// BasicAuth：恒定时间比较，认证用户经 context 下传
s.Use(ghttp.BasicAuth("Admin", map[string]string{"alice": "secret"}))
// handler 内：user := ghttp.BasicAuthUser(ctx)
```

## 破坏性变更（生产就绪批次）

本仓库处于 pre-v1.0.0，以下变更不保证向后兼容：

| # | 变更 | 影响面 | 迁移方式 |
|---|---|---|---|
| B1 | typed error 默认不再回传 `serr.Error()`，改通用文案 | 依赖明文错误的客户端 | `WithExposeErrorDetails(true)` |
| B2 | 错误体 `text/plain` → JSON `{"error":{code,message}}` | 解析错误体的客户端 | `WithErrorRenderer` 自定义 |
| B3 | 请求 Content-Type 默认严格校验（不符 415） | 发错 CT 的旧客户端 | `WithStrictContentType(false)` |
| B4 | `LoggerWith` 签名 `(method,path,status,elapsed)` → `(AccessLog)` | 调用方 | 改用结构体字段 |
| B5 | 404/405 默认带 JSON 响应体 | 断言空 body 的测试 | 更新断言 |

## 状态

pre-v1.0.0，开发中，禁止直接用于生产；详见仓库根 `DEVELOPMENT.md`。
