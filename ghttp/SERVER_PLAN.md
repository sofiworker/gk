# ghttp server 实现计划

> 依据：`docs/superpowers/specs/2026-10-03-ghttp-v3-http-server-design.md`（下称"设计文档"）与 2026-10-05 的现状盘点。
> 状态：`[ ]` 未开始 · `[~]` 进行中 · `[x]` 完成 · `[-]` 暂缓（需决策）
> 原则：pre-v1，不承诺兼容；破坏性变更在完成后汇总到 README。所有新增能力都要有单测，`go test -race ./ghttp/...` 通过。

## 阶段 1：主链路正确性（P0）

按设计文档写代码必须得到正确结果。

- [x] **1.1 typed handler 输出分派**：handler 返回 `Reply[T]`、`FileReply`、`StreamReply`、`RedirectReply`、`NoContentReply`（值或指针）时按各自语义写出，而不是被当作普通结构体 JSON 序列化。`Reply` 未设 Status 时为 200；204/304 不写 body。
- [x] **1.2 错误到状态码的映射**：
  - `HTTPError` 按状态码参与 `errors.Is`，`errors.Is(err, ErrNotFound)` 对任意 404 成立；
  - 新增 `ErrInvalidInput`（400），`Value` 的缺失与类型转换错误都包装它；
  - `wire.ErrInvalidFormat` → 400，`*http.MaxBytesError` → 413，`gerr.Kind` → 对应状态，`context.Canceled` → 499，`context.DeadlineExceeded` → 504，其余 → 500；
  - 导出 `StatusFromError(err) int`。
- [x] **1.3 统一错误响应体与脱敏**：默认错误处理器写 JSON `ErrorResponse`；只有 `HTTPError.Message` 会返回给客户端，其余错误只返回状态文本，底层错误字符串不外泄。
- [x] **1.4 请求体限额**：`WithMaxBodyBytes(n)`（Server 级，默认 32 MiB，`-1` 不限）与路由级 `WithBodyLimit(n)`；超限返回 413。
- [x] **1.5 全局 middleware 包裹分发终端**：匹配先于全局链执行，middleware 可以读取路由模板；404、405、TSR 也经过全局链，404/405 走统一错误链。全局链在首个请求时固化，同时修复"首个请求与 Register 并发"的竞态。
- [x] **1.6 Action / Procedure 注册入口**：`HandleAction[T]`、`HandleProcedure`，成功时返回 204。

## 阶段 2：设计文档中缺失的项（P1）

- [x] **2.1 请求访问**：`RequestInput.Request()` 取底层请求；`Request.Route()` 返回匹配的路由模板；`Request.Query()` 惰性解析并缓存；`Sources()` 提供原始 string / []string 访问；Header 的存在性判断改为按值是否存在。
- [x] **2.2 路径**：
  - 注册期把模板写法 `{id}`、`{path...}` 转成 `:id`、`*path`；
  - 请求路径含 `.`/`..` 段时返回 400；`WithStrictPath()` 额外拒绝空段；
  - Group 前缀拼接规范化（不产生 `//`，保留尾斜杠语义）。
- [x] **2.3 注册期校验**：校验 method token；拒绝 nil middleware；整批路由先全部预校验，再逐条安装。
- [x] **2.4 选项合并**：Route 改为在 Register 时编译；实现 `Server.With`、`Group.With`、`GroupOption`（`WithGroupMiddleware`、`WithGroupOptions`）。合并顺序：Server.With → 父组 → 子组 → Route。`WithInput`/`WithOutput` 只允许在路由级使用。
- [x] **2.5 输入输出扩展**：
  - `Input[T]` / `Output[O]` 接口，`WithInput` / `WithOutput`，类型不匹配在注册期报错；
  - 内置 `JSONInput`、`XMLInput`、`JSONOutput`、`XMLOutput`、`TextOutput`，默认用 JSON。
- [x] **2.6 生命周期与 Server 选项**：
  - 启动后调用 `Use` 时经 `WithErrorLog` 告警；
  - `WithReadHeaderTimeout`（默认 10s，防慢速攻击）、`WithBaseContext`、`WithErrorLog`；
  - `Run("")` 使用 `WithAddr`；
  - `RunContext(ctx, addr)`：ctx 结束时按 `WithShutdownTimeout` 优雅关闭。
- [x] **2.7 Response 能力**：`Unwrap()`（支持 `http.NewResponseController` 的 Flush/Hijack）、`Written()`、`Size()`。

## 阶段 3：生产能力（独立文件，可并行）

- [x] **3.1 RequestID**：沿用或生成 `X-Request-ID`，写入 ctx，`RequestIDFrom(ctx)`。
- [x] **3.2 Timeout**：给请求 ctx 设截止时间，超时且未写响应时返回 503。
- [x] **3.3 SecureHeaders**：nosniff、frame-options、referrer-policy、可选 HSTS 和 CSP。
- [x] **3.4 CSRF**：基于标准库 `http.CrossOriginProtection`。
- [x] **3.5 BasicAuth**：常量时间比较，401 时带 `WWW-Authenticate`。
- [x] **3.6 RateLimit**：内存令牌桶，按 key 限流（默认按 ClientIP），超限返回 429 并带 `Retry-After`，空闲桶自动清理。
- [x] **3.7 ClientIP**：`ClientIP(req)`，只在 `WithTrustedProxies` 配置的可信代理链内解析 `X-Forwarded-For` / `X-Real-IP`。
- [x] **3.8 Gzip**：按 `Accept-Encoding` 协商，跳过已编码或过小的响应，追加 `Vary`。
- [x] **3.9 静态文件**：`Static(prefix, fs.FS)` 生成 GET/HEAD 路由，默认不列目录。
- [x] **3.10 健康检查**：liveness / readiness 路由，readiness 支持注册检查项。
- [x] **3.11 SSE 写端**：基于 `wire.AppendSSEField` 并逐条 Flush，支持心跳与 ctx 取消。
- [x] **3.12 观测钩子**：`Observe(func(RequestInfo))` 中间件，信息包含路由模板、方法、状态码、耗时、字节数；Logger 改为记录路由模板。
- [x] **3.13 CORS 加固**：origin 白名单，`*` 与 credentials 互斥，追加 `Vary: Origin`，只处理真正的预检请求。

## 阶段 4：测试基线与文档

- [x] **4.1** 覆盖设计文档第 11 节的全部测试基线（Group 嵌套与顺序、Reply、413、Raw 与 typed 共用错误链、Shutdown 等待在途请求、并发 ServeHTTP 的 race 测试）。
- [x] **4.2** `ghttp/README.md`：用法、选项、错误映射、破坏性变更说明。
- [x] **4.2b** 设计文档中与实现不一致的地方同步修订（如 GroupOption 签名、`ErrInvalidInput` 位于根包、Allow 含 OPTIONS、同层 catch-all 与静态冲突）。
- [x] **4.3** `go vet`、`gofmt` 全部通过，根包覆盖率 ≥ 85%。

## 阶段 5：第二轮补齐（2026-10-05 盘点）

核心（改动核心文件，顺序实现）：

- [x] **5.1 输入校验**：`Validator` 接口与 `ValidatorFunc`、`WithValidator`（可用于路由、Group、Server.With 各级）；body 类型实现 `Validate() error` 或 `Validate(ctx) error` 时自动调用；`Data()` 解码后执行，失败返回 400（包装 `ErrInvalidInput`；validator 返回的 HTTPError 原样保留）。
- [x] **5.2 日志接口注入**：`Logger` 接口（Debugf/Infof/Warnf/Errorf，与 client 一致，同一实现可服务两端）、`NewSlogLogger`、`WithLogger`；框架告警走 Logger。访问日志中间件更名为 `AccessLog`/`AccessLogWithConfig`（让出 `Logger` 名字），支持输出到 Logger，颜色可关闭且默认只在输出到 io.Writer 时开启；`RecoveryWithLogger`。
- [x] **5.3 gerr 互转**：`FromGerr`、`ToGerr`、`GerrStatus`、`KindFromStatus`。
- [x] **5.4 路径修正重定向**：`WithRedirectFixedPath()`，404 前尝试清理路径与大小写不敏感匹配，接入 tree.go 中原为死代码的 `findCaseInsensitivePath`。
- [x] **5.5 关闭钩子**：`OnShutdown(func(ctx) error)`，在途请求结束后按注册逆序执行，错误合并返回。
- [x] **5.6 HTTP/2 / h2c**：`WithProtocols`、`WithH2C()`、`WithHTTP2Config`。
- [x] **5.7 路由元数据**：`Server.Routes() []RouteInfo`（方法、路径、body/结果类型、文档信息），`WithDoc(summary, description)`、`WithTags`、`WithOperationID`，供 OpenAPI 使用。

扩展（只新增文件，可并行）：

- [x] **5.8 表单输入**：`FormInput[T]`（urlencoded + multipart），`form` 标签，文件字段 `*multipart.FileHeader`，内存上限选项。
- [x] **5.9 过载保护**：`MaxInFlight(n)`，超限 503 + `Retry-After`。
- [x] **5.10 请求体解压**：`Decompress(...)`，支持 gzip/deflate，限制解压后大小（防解压炸弹）。
- [x] **5.11 ETag**：`ETag()` 中间件，为 GET/HEAD 的 200 响应生成弱 ETag，处理 `If-None-Match` → 304。
- [x] **5.12 WebSocket**：`ghttp/ws` 子包，RFC 6455 服务端：握手、分片、掩码、ping/pong、close、消息大小上限、Origin 校验；不引入第三方依赖。
- [x] **5.13 OpenAPI 3.1**：基于 `Server.Routes()` 与反射生成文档，`OpenAPIRoute(path)` 提供 JSON。
- [x] **5.14 观测适配**：`ghttp/adapters/gotel`，基于 gotel 抽象的 tracing 与 metrics 中间件（按路由模板打点）。
- [x] **5.15 端到端基准**：设计文档 12.4 的 SimpleGET / GETWithPath / GETWithQuery / POSTJSON / EarlyReturn，与中间件栈基准。
- [x] **5.16 文档**：设计文档同步（即 4.2b），README 补充本阶段内容。

## 暂缓（需维护者决策）

- [-] client 合并进根包：已决定暂缓。
- [-] golangci-lint：本机未安装且离线，无法执行。
