# ghttp 生产就绪能力补齐设计（错误链 / 可观测容错 / 参数校验 / 运维便利）

> 日期：2026-08-23
>
> 状态：**设计草案，等待评审**（本文只做设计，不含实现；评审通过后再拆任务实施）
>
> 关联文档：
> - `docs/superpowers/specs/2026-08-22-ghttp-typed-formal-codec-body-template.md`（typed 正式版入口契约，本文的上游基线）
> - `docs/superpowers/specs/2026-08-21-ghttp-handler-middleware-decisions.md`（handler/middleware 执行模型，本文错误链的挂载依据）
> - `ghttp/doc.go`（当前对外能力清单，本文完成后需同步更新）
>
> 约束（本轮用户明确）：
> 1. **零新增第三方依赖**：只用 Go 标准库；校验、协商等自实现，符合仓库离线原则。
> 2. **允许破坏性变更**：pre-v1.0.0，可调整现有导出签名；破坏点须在提交说明与 README 显式标注。
> 3. **不得降低性能**：热路径（匹配、池化、零中间件快路径）现有分配数与 ns/op 不得回退；新增能力默认关闭或零成本旁路。
> 4. 本轮**只交付设计文档，禁止编码、构建、提交**。

---

## 0. 背景与现状锚点

ghttp 当前是一次从零重写后的 **typed 极简核心**：泛型自由函数入口（`GetParams`/`PostBody`/`PostParamsBody`…）+ struct tag 绑定（注册期建 `BindPlan`、请求期零反射）+ 纯 `net/http` + 池化上下文。路由引擎为 gin v1.12.0 tree 的完整移植。

对照 gin 与真实生产标准，**已完备**的能力（本文不动）：radix 路由 + 参数 + catch-all + TSR、405+Allow（默认开启）、Recovery、CORS（含预检）、Timeout（协作式）、RequestID、LimitBody、静态资源、健康检查（liveness/readiness + `ReadinessGate`）、三态生命周期优雅关闭。

本文针对四组**真实生产缺口**给出设计，全部来自代码事实核对（引用行号见各节）：

- **P0｜统一错误链**：`ghttp/mux.go` 三处 `TODO(stage-4)`（`dispatchChained` 161–165、`safeChain` 178、`serve` 348）——typed handler 返回的 error 一律 `http.Error(w, serr.Error(), 500)`，**泄露内部错误串**且**无 4xx 分类**；`RequestDecoder.ContentType()` 已定义但无人消费，**415 无校验**；`dispatchRaw`（`mux.go:267`）缺 `!written` 判断存在 **500 双写** 隐患。
- **P1｜可观测与容错**：无命中路由模板标识（`nodeValue` 无 `fullPath`）、无 ClientIP/可信代理、404/405 无响应体且不可自定义（`writeMiss` 只 `WriteHeader`）。
- **P1｜参数校验**：`BindPlan` 无 required/范围/枚举校验，连必填都没有。
- **P2｜运维便利**：无优雅退出信号编排、访问日志字段贫弱（`LoggerWith` 仅 4 字段）、无 BasicAuth。

---

## 1. 总体设计原则

1. **错误链是地基，先行落地**：其余三组能力（尤其校验、404/405 体）都要复用错误→响应的统一出口，故 P0 是所有工作的前置。
2. **性能红线：默认零成本**。新增字段/分支必须满足下列任一：
   - 只在**注册期**产生成本（如校验计划编译、fullPath 字符串驻留）；
   - 只在**冷路径**（miss、error、panic）产生成本，不碰命中热路径；
   - 命中热路径新增的字段仅做**指针/整型赋值**（如 fullPath 传递），不引入分配或哈希。
3. **显式优于隐式**：对外行为默认保守。错误细节泄露、可信代理、校验，都要显式开启或显式声明。
4. **小接口组合**：错误分类走接口（`StatusCoder`）而非大 switch；校验走小接口，不强绑某个库。
5. **破坏性变更集中**：本文涉及的签名调整（`LoggerWith` 扩参、`register` 传 metadata）一次性完成并在 README/CHANGELOG 标注。

---

## 2. P0 ｜统一错误链（HTTP 错误协议）

### 2.1 目标

- typed handler / codec / 校验返回的 error → **映射到正确的 HTTP 状态码**（400/413/415/500…）。
- **默认不泄露**内部错误细节：客户端只收到规范、稳定的错误体；细节仅进服务端日志。
- 补齐 **415**（请求 Content-Type 与端点声明的 `RequestDecoder.ContentType()` 不符）。
- 修复 `dispatchRaw` 的 **500 双写**。
- 错误体走**结构化 JSON**（成功/失败格式一致），可自定义。

### 2.2 错误分类：`StatusCoder` 小接口 + 哨兵映射

错误状态码来源有二，按优先级：

```go
// StatusCoder 让业务错误自带 HTTP 状态码；实现它即参与错误链状态映射。
// StatusCoder lets a business error carry its own HTTP status.
type StatusCoder interface {
    error
    HTTPStatus() int
}
```

1. **实现了 `StatusCoder` 的 error**：直接用 `HTTPStatus()`（业务可精确控制 404/409/422…）。
2. **框架哨兵**（`errors.Is` 判定，已存在于 `errors.go`）：
   - `ErrInvalidInput` / `ErrInvalidRequestPath` / `ErrMissingRequired` → **400**
   - `ErrUnsupportedMediaType`（**新增**）→ **415**
   - `http.MaxBytesError`（`LimitBody` 触发）/ `ErrRequestEntityTooLarge` → **413**
   - `ErrHandlerPanic` → **500**
   - 其它未分类 error → **500**（保守兜底）

> 设计选择：**不引入 RFC 9457 problem+json 作为默认**（保持 `{code,message}` 简单形态），但错误体构造走可替换的 `ErrorRenderer`，用户可自行换成 problem+json。理由：problem+json 会牵出 `type`/`instance` 等字段与内容协商，属可选增强，不进核心默认。

### 2.2a 顺带修复：自定义 encoder 丢失 Content-Type（正确性 bug）

**问题**：`output.go` 的 `jsonOutput.encode`（第 47–49 行）在 `WithEncoder` 分支只 `WriteHeader(status)` + `enc.Encode(...)`，**从不 `Set("Content-Type", ...)`**；仅默认 JSON 分支（第 51 行）会设。结果：用 `JSON[T]().WithEncoder(XMLCodec())` 输出 XML 时，响应缺正确 `Content-Type`（客户端按错误类型解析）。

**设计**：`ResponseEncoder` 已声明 `ContentType()`（`codec.go:29`），修复即接线——`WithEncoder` 分支在 `WriteHeader` 前 `resp.Header().Set("Content-Type", o.enc.ContentType())`（若 encoder 未自行设置）。**破坏性极小**（仅补齐缺失头），归入 P0 批次一并做。

> **性能红线**：一次 header set，仅在有自定义 encoder 的端点发生，不碰默认 JSON 路径。

### 2.3 错误体形态与脱敏

默认错误体（稳定契约）：

```json
{"error": {"code": "invalid_input", "message": "invalid request"}}
```

- `code`：稳定机器可读串，由哨兵/StatusCoder 决定（`invalid_input`/`unsupported_media_type`/`not_found`/`method_not_allowed`/`internal`…）。
- `message`：**默认是与状态码绑定的通用文案**（如 500→`"internal server error"`），**不含** `serr.Error()` 的内部细节。
- **`WithExposeErrorDetails(true)`（新增 Option，默认 false）**：仅当显式开启，`message` 才带上 `serr.Error()`（开发/内网调试用）。这直接堵住当前 `http.Error(w, serr.Error(), 500)` 的泄露口。

错误渲染可替换：

```go
// ErrorRenderer 把一个已分类的错误写成响应体。默认实现输出 {"error":{code,message}} JSON。
type ErrorRenderer interface {
    RenderError(resp *Response, status int, code, message string)
}
func WithErrorRenderer(r ErrorRenderer) Option // 覆盖默认 JSON 渲染
func WithExposeErrorDetails(expose bool) Option // 默认 false
```

### 2.4 挂载点：收敛到单一出口 `writeError`

当前有两条分发路径（`dispatchChained` 有全局中间件、`dispatchRaw` 零中间件快路径）。错误出口收敛为一个函数，两条路径都调它：

```go
// writeError 是错误链唯一出口：分类 → 选状态码 → 脱敏 → 渲染。
// 已提交(resp.Written())则只记日志不改写，避免双写。
func (m *mux) writeError(resp *Response, r *http.Request, err error)
```

- `dispatchChained`：`if serr != nil && !written { m.writeError(...) }`（保持 written 判断）。
- `dispatchRaw`：**改为** `if serr != nil && !req.resp.Written() { m.writeError(...) }` —— **修复双写 bug**（当前缺 written 判断）。
- `safeChain`/`serve` 的兜底 recover：panic 收敛为 `ErrHandlerPanic` 后同样走 `writeError`（500 + 通用文案，堆栈只进日志）。

> **性能红线**：`writeError` 只在 **error≠nil 的冷路径** 触发。成功命中路径完全不经过它，热路径零新增成本。分类用 `errors.As`（StatusCoder）+ 少量 `errors.Is`，只在出错时发生。

### 2.5 415 校验：注册期携带 ContentType，请求期比对

- **注册期**：`registerBody`/`registerParamsBody` 已持有 `dec RequestDecoder`。把 `dec.ContentType()` 存入 compiled 执行器（如 `compiledBody.wantCT`）。
- **请求期**：在 `dec.Decode` 之前比对 `req.Header.Get("Content-Type")` 的 media-type 部分与 `wantCT`：
  - 不匹配 → 返回 `fmt.Errorf("%w: got %q want %q", ErrUnsupportedMediaType, got, want)` → 错误链映射 415。
  - 匹配（或请求无 body 语义）→ 继续解码。
- **宽松开关**：`WithStrictContentType(false)`（默认建议 **true**，即严格 415）——关闭时回退当前"不校验直接解码"行为。此为破坏性变更（默认从"不校验"变"415"），须标注。

> **性能红线**：Content-Type 比对是一次字符串前缀比较（media-type 到 `;` 为止），无分配；只在 body 端点发生，不碰无 body 的 GET 热路径。

### 2.6 新增哨兵（`errors.go`）

```go
ErrUnsupportedMediaType   = errors.New("ghttp: unsupported media type")     // → 415
ErrRequestEntityTooLarge  = errors.New("ghttp: request entity too large")   // → 413（LimitBody 复用）
```

### 2.7 破坏性变更清单（P0）

| 变更 | 旧行为 | 新行为 | 缓解 |
|---|---|---|---|
| typed error 出口 | 一律 500 + `serr.Error()` 明文 | 分类状态码 + 通用文案 | `WithExposeErrorDetails(true)` 恢复明文 |
| 请求 Content-Type | 不校验 | 默认 415 | `WithStrictContentType(false)` 恢复 |
| 错误体 | text/plain | JSON `{"error":{...}}` | `WithErrorRenderer` 自定义 |

---

## 3. P1 ｜可观测与容错

### 3.1 命中路由模板 `fullPath`（可观测性）

**问题**：`nodeValue`（`route_tree.go:336`）与 `routeNode` 均无 fullPath，命中后无法知道"匹配了哪条模板"，metrics/tracing/日志只能用高基数的原始 path（如 `/users/12345`）当标签，无法按路由聚合。

**设计**：

- **注册期**：`insertChild` 落 handler 时，把已知的 `fullPath`（gin 形式，如 `/users/:id`）存到叶节点 `routeNode.fullPath string`。这是注册期一次性成本。
- **匹配期**：`getValue` 命中时把 `n.fullPath` 写入 `nodeValue.fullPath`（**一次指针/字符串头赋值，无分配**）。
- **暴露**：`Request` 增加只读访问器 `func (r *Request) MatchedRoute() string`（返回命中的模板；miss 时空串）。middleware（如日志）与业务均可读。

> **性能红线**：fullPath 是驻留字符串（注册期已存在），命中路径只多一次 `value.fullPath = n.fullPath` 赋值，零分配、无哈希。这是 gin `c.FullPath()` 的等价能力，gin 亦为此在节点存 fullPath，成本模型一致。

### 3.2 ClientIP / 可信代理（安全 + 可观测）

**问题**：反代/LB 后只能拿 `RemoteAddr`；盲信 `X-Forwarded-For` 又可被伪造。

**设计**（对齐 gin 的可信代理模型，纯标准库 `net/netip`）：

```go
// Server 配置
func WithTrustedProxies(cidrs ...string) Option    // 声明可信代理 CIDR；默认空=不信任任何 XFF
func WithForwardedHeaders(names ...string) Option  // 默认 ["X-Forwarded-For","X-Real-IP"]

// 访问器（挂在 Request，惰性、无缓存热路径成本）
func (r *Request) ClientIP() string   // 依可信代理策略解析真实 IP
func (r *Request) RemoteIP() string   // 纯 RemoteAddr 的 IP 部分
```

- **默认安全**：未配置 `WithTrustedProxies` 时，`ClientIP()` **只返回 `RemoteIP()`**，绝不采信 XFF（避免伪造）。
- 配置后：仅当直连对端 IP ∈ 可信 CIDR，才按 `ForwardedHeaders` 顺序回溯取第一个非可信 IP。
- 解析用 `net/netip`（Go 1.18+ 标准库，零第三方）。

> **性能红线**：`ClientIP()` 是**访问器**，只有调用时才解析；命中热路径若不调用则零成本。可信 CIDR 在注册期解析为 `[]netip.Prefix`。

### 3.3 404 / 405 可自定义 + 默认响应体

**问题**：`writeMiss`/`writeMissRaw`（`mux.go:275`/`291`）只 `WriteHeader`，body 为空，无注册点。生产 API 需要统一的 404/405 JSON。

**设计**：

```go
func WithNotFoundHandler(h RawHandlerFunc) Option        // 自定义 404
func WithMethodNotAllowedHandler(h RawHandlerFunc) Option // 自定义 405（Allow 头仍由框架先设置）
```

- 默认行为改为：走 **§2.3 的 ErrorRenderer** 输出 `{"error":{"code":"not_found",...}}` / `{"code":"method_not_allowed",...}`，与业务错误体一致。
- 自定义 handler 存在时优先调用；框架仍先设好 `Allow` 头（405）。
- 404/405 仍在全局中间件链内（现有语义保留），中间件可观测。

> **性能红线**：miss 是冷路径，新增渲染不影响命中。默认体渲染只在 miss 发生时执行。

### 3.4 访问日志字段增强（顺带，属可观测）

现 `LoggerWith(func(method, path string, status int, elapsed))` 字段太少。**破坏性调整**为结构化参数：

```go
// AccessLog 是一次请求的可观测快照，传给日志 sink。
type AccessLog struct {
    Method    string
    Path      string        // 原始 path（高基数）
    Route     string        // 命中模板（低基数，§3.1）—— 指标聚合用
    Status    int
    Elapsed   time.Duration
    ClientIP  string        // §3.2
    BytesOut  int           // 响应体字节数（需 Response 记账，见下）
    Err       error         // 错误链分类前的原始 error（可 nil）
}
func LoggerWith(record func(AccessLog)) Middleware
func Logger() Middleware // 默认实现调用 LoggerWith
```

- `Response` 增加 `bytesOut int` 记账：在 `Write`/`WriteString` 累加 `n`（**一次整型加法，无分配**）。当前 `Response` 已拦截 Write，只多一个 `+=`。
- `Route` 复用 §3.1 的 `MatchedRoute()`；`ClientIP` 复用 §3.2。

> **性能红线**：`bytesOut` 累加是热路径上一次 `int += n`，可忽略；`AccessLog` 结构体按值传给 sink，仅在装了 Logger 中间件时构造（且日志本就是每请求成本，不属"新增热路径开销"）。

---

## 4. P1 ｜参数校验（零依赖，注册期编译）

### 4.1 目标与非目标

- **目标**：给 struct tag 绑定补 **required / 范围 / 枚举 / 长度 / 非空** 等常见校验，错误经错误链映射 **400**（复用 `ErrInvalidInput` 语义，或细分 `ErrValidation`）。
- **非目标**：不引入 `go-playground/validator`（第三方依赖，违约束）；不做跨字段复杂规则的完整 DSL（留给业务在 handler 内做）。
- **风格对齐**：延续现有"注册期反射建计划、请求期零反射跑计划"（`BindPlan`），校验规则同样**注册期编译**进 plan，请求期只跑闭包。

### 4.1a 前置：扩展可绑定标量类型（校验的基础）

**问题**：`buildBindPlan`（`bind_plan.go:73–78`）当前只支持 `string/int/int64/bool` 四种 kind，其它类型（`uint*`/`int8/16/32`/`float32/64`/`time.Duration`/`time.Time`）在**注册期**直接 `ErrInvalidParam`。校验规则（`min/max`）作用于数值，若数值类型太窄，校验价值受限。

**设计**：把标量转换扩展到常见类型，仍走注册期 kind 分派 + 请求期 `strconv` 系列（零反射、零依赖）：

| 新增 kind | 解析方式（标准库） |
|---|---|
| `int8/int16/int32` | `strconv.ParseInt` + 位宽校验 |
| `uint/uint8/…/uint64` | `strconv.ParseUint` |
| `float32/float64` | `strconv.ParseFloat` |
| `time.Duration` | `time.ParseDuration` |

- 解析失败仍归 `ErrInvalidInput` → 错误链 400（与现有一致）。
- `time.Time` 暂缓（格式歧义大，需约定 layout，单列）。

> **性能红线**：仅注册期多几个 `case` 分支，请求期是既有 `strconv` 直调，无反射、无分配增量。这是校验落地的**前置**，与 §4.2/§4.3 同批实施。

### 4.2 Tag 语法：`validate:"..."`（子集，自实现）

```go
type CreateUser struct {
    Name  string `json:"name"  validate:"required,min=1,max=64"`
    Age   int    `query:"age"  validate:"min=0,max=150"`
    Role  string `query:"role" validate:"oneof=admin user guest"`
    Email string `json:"email" validate:"required"`
}
```

支持规则集（首批，纯标准库可实现）：

| 规则 | 适用类型 | 语义 |
|---|---|---|
| `required` | 所有 | 零值即失败（string 空、数值 0 需配 `min` 区分，见下注） |
| `min=N` / `max=N` | 数值 | 数值范围；string/slice 时为长度 |
| `len=N` | string/slice | 精确长度 |
| `oneof=a b c` | string/数值 | 枚举白名单 |
| `email` | string | 轻量格式（含 `@` 且有域，标准库 `net/mail.ParseAddress`） |

> `required` 与数值 0 的歧义：数值字段的"必填"用 `min`/`oneof` 表达更精确；`required` 主要服务 string/指针/slice。文档需明示这一取舍（与 gin/validator 的已知痛点一致）。

### 4.3 两条落地路径

**路径 A（推荐首选）｜tag 内建校验**：`buildBindPlan`（`bind_plan.go:45`）解析字段时**同时解析 `validate` tag**，为每个字段编译一个 `[]fieldRule`（各 rule 是闭合了阈值的校验闭包）。请求期在 `BindPlan.apply` 写入字段值后立即跑该字段的 rules，失败返回 `ErrValidation`。

- **body 校验**：body 经 `RequestDecoder` 解码后不走 BindPlan。为覆盖 body，定义可选接口：

```go
// Validator 由 body 结构体自行实现，解码后自动调用。
type Validator interface { Validate() error }
```

  `compiledBody`/`compiledParamsBody` 在 `dec.Decode` 后，若 `b` 实现了 `Validator` 则调用 `b.Validate()`，失败进错误链。这把 body 深层/跨字段校验交给业务（零反射、零框架魔法），而 params 的浅层规则由 tag 覆盖。

**路径 B（可选增强）｜结构体级 tag 校验器**：若日后要对 body 也做 tag 驱动校验，提供 `ValidateStruct(v any) error` 独立函数（反射一次），业务在 `Validate()` 内调用。**首批不做**，避免 body 热路径引入反射。

### 4.4 与错误链的衔接

- 新增哨兵 `ErrValidation = errors.New("ghttp: validation failed")` → 映射 **400**。
- 校验错误消息默认脱敏（同 §2.3）：客户端收 `{"error":{"code":"validation_failed","message":"..."}}`；字段级细节仅在 `WithExposeErrorDetails(true)` 时附带（或经 `ErrorRenderer` 自定义为字段级数组）。

> **性能红线**：
> - **无 `validate` tag 的字段/结构体：零成本**（plan 里 rules 为空，apply 不进校验分支）——这是保证现有基准不回退的关键。
> - 有 rules 的字段：请求期跑的是注册期已编译的闭包，无 tag 再解析、无反射查 tag。与现有 BindPlan 同源模型。
> - body 的 `Validator` 是类型断言 + 直接方法调用，无反射。

---

## 5. P2 ｜运维便利

### 5.1 优雅退出信号编排

**问题**：`Shutdown`/`ReadinessGate` 已具备，但 SIGTERM→摘流→排水 需用户手写。

**设计**（一体化 helper，不改现有 `Run`/`Shutdown` 语义，纯组合）：

```go
// RunGraceful 启动并阻塞，收到 SIGINT/SIGTERM 时：
//   1) 若绑定了 ReadinessGate，先 Set(false) 让 LB 摘流；
//   2) 等待 drainDelay（给 LB 感知时间）；
//   3) 以 shutdownTimeout 调 Shutdown 排水；
//   4) 超时则 Close 强制关闭。
// 纯标准库 os/signal + signal.NotifyContext 实现。
func (s *Server) RunGraceful(addr string, opts ...GracefulOption) error

type GracefulOption func(*gracefulConfig)
func WithDrainDelay(d time.Duration) GracefulOption       // 摘流后等待，默认 0
func WithShutdownTimeout(d time.Duration) GracefulOption  // 排水超时，默认 30s
func WithReadinessGate(g *ReadinessGate) GracefulOption   // 摘流门闸，默认无
func WithSignals(sig ...os.Signal) GracefulOption         // 默认 SIGINT,SIGTERM
```

- 完全构建在现有 `Run`+`Shutdown`+`ReadinessGate` 之上，是**可选便利层**，用户仍可继续手接。
- `signal.NotifyContext`（Go 1.16+ 标准库）实现，零第三方。

> **性能红线**：仅生命周期方法，不在请求路径，无性能影响。

### 5.2 BasicAuth 中间件

gin 核心有 `BasicAuth`，ghttp 缺。补一个标准库实现：

```go
// BasicAuth 校验 Authorization: Basic；失败返回 401 + WWW-Authenticate。
// accounts 为 user->password；比较用 crypto/subtle 恒定时间，防时序侧信道。
func BasicAuth(realm string, accounts map[string]string) Middleware
```

- `crypto/subtle.ConstantTimeCompare` 恒定时间比较（安全细节，gin 同款）。
- 401 时设 `WWW-Authenticate: Basic realm="..."`。
- 认证信息经 `context` 下传（`BasicAuthUser(ctx) string`）。

> **性能红线**：仅在挂载该中间件的路由生效，是标准中间件成本；未挂载零影响。

---

## 6. 性能验证策略（对应"不得降低性能"红线）

评审实现阶段须提供 benchstat 对比，门槛：

1. **命中热路径回归门槛**：
   - `GetNone`（无输入）、`GetParams`（参数绑定）、`RawHandle` 命中：**allocs/op 与 ns/op 不得高于当前基线**（当前基线见 `2026-08-22` 文档表：GetNone 3 allocs、GetParams 8 allocs 等）。
   - 零中间件快路径（`dispatchRaw` 命中）：**保持 0 alloc**。
2. **新能力默认关闭/旁路验证**：不声明 `validate` tag、不调 `ClientIP()`、不装 Logger 时，基准与现状**逐项持平**（证明新字段/分支零成本）。
3. **fullPath 传递**：单独基准证明命中路径新增的 `value.fullPath = n.fullPath` 不产生分配。
4. **错误链**：仅在 error 路径基准（不影响成功基准）。
5. 工具：`go test -bench . -benchmem` + `benchstat`；`go test -race ./ghttp/...` 全绿；`make check`。

---

## 7. 破坏性变更总清单（须在 CHANGELOG / README 标注）

| # | 变更 | 影响面 | 迁移方式 |
|---|---|---|---|
| B1 | typed error 默认不再回传 `serr.Error()`，改通用文案 | 依赖明文错误的客户端 | `WithExposeErrorDetails(true)` |
| B2 | 错误体 text/plain → JSON `{"error":{code,message}}` | 解析错误体的客户端 | `WithErrorRenderer` 自定义 |
| B3 | 请求 Content-Type 默认严格校验（415） | 发错 CT 的旧客户端 | `WithStrictContentType(false)` |
| B4 | `LoggerWith` 签名 `(method,path,status,elapsed)` → `(AccessLog)` | 调用方 | 改用结构体字段（一次性） |
| B5 | 404/405 默认带 JSON 响应体 | 断言空 body 的测试 | 预期内，更新断言 |

> B1–B3 是 P0 的核心价值（安全 + 语义正确），破坏性可控且有开关回退；B4 是字段增强的必要代价。

---

## 8. 实施批次建议（评审通过后）

- **批次 1（P0，前置）**：错误分类 + `StatusCoder` + `writeError` 单一出口 + 脱敏开关 + 415 + dispatchRaw 双写修复 + JSON 错误体。**这是"能否生产使用"的分水岭。**
- **批次 2（P1 可观测容错）**：fullPath/`MatchedRoute` + ClientIP/可信代理 + 404/405 可定制 + 日志字段增强（含 `bytesOut`）。
- **批次 3（P1 校验）**：`validate` tag 子集 + `Validator` 接口 + `ErrValidation` 接入错误链。
- **批次 4（P2 运维）**：`RunGraceful` + `BasicAuth`。
- 每批：单测 + httptest 真实请求 + 相关 race/fuzz + benchstat 回归 + 同步更新 `doc.go` 与 README（中英分文件）。

---

## 9. 明确不做（本轮范围外）

- RFC 9457 problem+json 作为默认错误体（保留为 `ErrorRenderer` 可选实现）。
- 内容协商 `Negotiate`（Accept → 多格式响应）、YAML/TOML/ProtoBuf/MsgPack 编解码（需第三方或大量自实现，违零依赖或超范围）。
- 完整响应渲染器族（IndentedJSON/SecureJSON/JSONP/PureJSON/AsciiJSON/String/Data/HTML/SSE/Stream 等，gin `render/` 有 20 个）——现有 `ResponseEncoder` 接口已允许用户自实现，核心不铺开；`RenderBytes`/`Redirect` 等便捷器可后续独立评估。
- 数组/map/form-urlencoded/多文件上传（`QueryArray`/`PostFormMap`/`SaveUploadedFile`）与 `DefaultQuery`/`DefaultPostForm`、body 重读（`ShouldBindBodyWith`）——属绑定能力扩展，单列计划；本轮仅做 §4.1a 的标量类型扩展与 §4 校验。
- Gzip/限流/CSRF/安全头中间件（gin 核心亦无，属生态；可后续独立计划）。
- RedirectFixedPath / UseRawPath / RemoveExtraSlash（路径修复类容错，独立评估）。
- 自动 HEAD/OPTIONS（gin 核心亦无，非相对 gin 的缺口）。
- 客户端（client）能力。
- 全局中间件的路径参数可见性改造（涉及"匹配前 vs 匹配后"执行模型调整，风险高，单列评估）。
