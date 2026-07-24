# ghttp 错误国际化设计

> 日期：2026-07-24
>
> 状态：交互设计已确认，等待书面规格审查
>
> 范围：`gerr` 错误语义、`ghttp` 错误归一化与渲染、`ghttp/i18n` 请求级本地化

## 1. 目标

本设计建立一套客户端优先、服务端可选翻译的错误协议：

- 服务端始终返回稳定的 `message_id` 与结构化 `args`。
- 客户端明确提供语言偏好时，服务端作为 shim 额外返回本地化 `message`。
- Handler 保持 Go 惯用的 `return error`，不要求手动调用翻译函数。
- 框架自动处理自身错误、validator 错误、结构化业务错误及安全降级。
- `gerr` 表达跨传输层的错误身份，`ghttp` 负责 HTTP 投影，`ghttp/i18n` 负责语言解析和翻译。
- 错误语义、翻译引擎和响应格式彼此解耦，允许替换。
- 未启用功能时不增加成功请求热路径成本。

## 2. 非目标

第一阶段不包含：

- 修改 `ghttp.Client`。
- 成功响应自动国际化。
- 从 `err.Error()`、Go 类型名或拼接字符串猜测 message ID。
- 自动翻译日志、OpenAPI 文档正文或业务数据字段。
- 远程翻译平台、数据库 Catalog 或后台资源同步。
- Route 级错误 Renderer。
- 运行期错误注册表。
- 将诊断 Meta 自动暴露为客户端 Args。
- 强制业务层依赖 `ghttp`。

## 3. 总体架构

```text
业务与基础设施
      |
      | return error
      v
gerr：错误身份与分类
      |
      | Descriptor
      v
ghttp：HTTP 归一化
      |
      | ErrorDocument
      v
ghttp/i18n：可选本地化
      |
      | localized ErrorDocument
      v
ErrorRenderer：JSON / RFC 9457 / custom
```

职责边界：

```text
gerr
├── ID、Kind、Params、Meta、Op、Cause
├── errors.Is / errors.As / Unwrap
├── Descriptor、Describer、Describe
└── MultiError 显式顶层语义

ghttp
├── Kind 到 HTTP status 的映射
├── 框架错误和 validator 错误归一化
├── ErrorDocument、ErrorDetail
├── RespondError 与统一执行管线
├── JSON、RFC 9457 和自定义 Renderer
└── ErrorObserver

ghttp/i18n
├── 惰性请求 Session
├── LocaleResolver 与 ResolverChain
├── Accept-Language、Cookie、query 解析器
├── LanguageMatcher、fallback
├── Catalog 与 Localizer
└── Content-Language、Vary
```

## 4. gerr 错误语义

### 4.1 Error

`gerr.Error` 重构为：

```go
type Error struct {
	ID      string
	Kind    Kind
	Params  map[string]any
	Meta    map[string]any
	Op      string
	Message string
	Err     error
}
```

- `ID` 是稳定的机器错误身份，也是默认 `message_id`。
- `Kind` 是与传输层无关的错误分类。
- `Params` 是允许出现在客户端响应和翻译模板中的公开参数。
- `Meta` 只用于日志、Trace 和诊断。
- `Op` 标识内部操作位置。
- `Message` 是开发者文本，不进入默认客户端协议。
- `Err` 保存底层 cause。

`Params` 与 `Meta` 必须使用独立 map，不能自动合并。

### 4.2 Kind

```go
const (
	KindUnknown         Kind = ""
	KindInvalid         Kind = "invalid"
	KindNotFound        Kind = "not_found"
	KindConflict        Kind = "conflict"
	KindUnauthenticated Kind = "unauthenticated"
	KindPermission      Kind = "permission"
	KindRateLimited     Kind = "rate_limited"
	KindUnavailable     Kind = "unavailable"
	KindTimeout         Kind = "timeout"
	KindCanceled        Kind = "canceled"
	KindInternal        Kind = "internal"
)
```

`gerr` 不定义 HTTP、gRPC 或 CLI 状态码。

### 4.3 构造公开错误

```go
func New(id string, kind Kind, opts ...Option) *Error
```

示例：

```go
return gerr.New(
	"user.not_found",
	gerr.KindNotFound,
	gerr.WithParam("user_id", userID),
	gerr.WithCause(err),
)
```

`New` 用于创建具有公开语义的错误。空 ID 或非法 ID 不 panic；HTTP 投影时安全降级为 `internal.server_error`，并记录诊断信息。

### 4.4 包装内部错误

```go
func Wrap(err error, opts ...Option) error
```

只增加内部上下文：

```go
return gerr.Wrap(
	err,
	gerr.WithOp("userService.LoadProfile"),
	gerr.WithMessage("load user profile"),
	gerr.WithMeta("user_id", userID),
)
```

重新分类：

```go
return gerr.Wrap(
	err,
	gerr.WithID("user.not_found"),
	gerr.WithKind(gerr.KindNotFound),
	gerr.WithParam("user_id", userID),
)
```

规则：

- `err == nil` 返回 nil。
- 没有 ID 时只增加内部上下文。
- 只有具有 ID 的错误才能公开 Params。
- Meta 永远不进入 Descriptor。

### 4.5 Descriptor

```go
type Descriptor struct {
	ID     string
	Kind   Kind
	Params map[string]any
}

type Describer interface {
	ErrorDescriptor() Descriptor
}

func Describe(err error) (Descriptor, bool)
```

`Describe` 沿单一 `Unwrap() error` 链从外到内查找，最外层有效 Descriptor 优先。返回的 Params 必须防御性复制。

自定义错误可以实现 `Describer`，不必使用 `*gerr.Error`。

裸 `errors.Join` 不自动选择某个子错误作为公开语义。需要聚合时，`gerr.MultiError` 必须携带明确的顶层 ID 和 Kind。

## 5. ghttp 错误归一化

### 5.1 公共与内部模型

```go
type ErrorDocument struct {
	Status    int
	MessageID string
	Args      map[string]any
	Message   string
	Details   []ErrorDetail
}

type ErrorDetail struct {
	Location  string
	MessageID string
	Args      map[string]any
	Message   string
}

type NormalizedError struct {
	Document ErrorDocument
	Cause    error
	Kind     gerr.Kind
	Meta     map[string]any
	Op       string
}
```

Renderer 只能接收 `ErrorDocument`，不能访问 Cause、Meta 或 Op。

### 5.2 ErrorNormalizer

```go
type ErrorNormalizer interface {
	NormalizeError(context.Context, error) NormalizedError
}
```

默认处理顺序：

1. ghttp 框架内部错误。
2. 最外层 `HTTPStatusCarrier`。
3. `gerr.Describe`。
4. validator 适配器。
5. context timeout/cancel。
6. 未知错误安全降级。

未知错误固定输出：

```json
{
  "status": 500,
  "message_id": "internal.server_error"
}
```

原始错误只进入内部观察和日志。

### 5.3 Kind 到 HTTP 状态

```go
type KindStatusMapper interface {
	HTTPStatus(gerr.Kind) int
}
```

默认映射：

| Kind | HTTP |
|---|---:|
| Invalid | 400 |
| NotFound | 404 |
| Conflict | 409 |
| Unauthenticated | 401 |
| Permission | 403 |
| RateLimited | 429 |
| Unavailable | 503 |
| Timeout | 504 |
| Canceled | 408 |
| Internal/Unknown | 500 |

允许通过 `WithKindStatusMapper` 整体替换或覆盖。

HTTP adapter 可以使用 `WithStatus(err, status)` 显式表达 410、412、413、416、422、451 等精确状态。业务层不应 import `ghttp`。

### 5.4 框架错误 ID

| 场景 | HTTP | message_id |
|---|---:|---|
| 路由不存在 | 404 | `http.route_not_found` |
| 方法不允许 | 405 | `http.method_not_allowed` |
| 请求路径非法 | 400 | `request.invalid_path` |
| Body 无法解析 | 400 | `request.invalid_body` |
| Content-Type 不支持 | 415 | `request.unsupported_media_type` |
| Body 过大 | 413 | `request.body_too_large` |
| 请求超时 | 504 | `request.timeout` |
| 验证失败 | 422 | `request.validation_failed` |
| Handler panic | 500 | `internal.handler_panic` |
| 未知错误 | 500 | `internal.server_error` |

框架只把白名单数据加入 Args，例如 method、allowed methods、max bytes。请求体内容、堆栈、底层解析文本不得返回客户端。

### 5.5 ValidationErrorAdapter

```go
type ValidationErrorAdapter interface {
	AdaptValidationError(context.Context, error) (ValidationResult, bool)
}
```

默认适配 go-playground validator，并生成顶层 `request.validation_failed` 和字段级 details。

常见 tag 映射：

```text
required → validation.required
email    → validation.email
min      → validation.min
max      → validation.max
oneof    → validation.one_of
len      → validation.length
```

未知 tag 使用 `validation.<tag>`。无法识别自定义 Validator 错误时返回顶层 422，不暴露原始文本。

## 6. 错误响应格式

### 6.1 ErrorRenderer

```go
type ErrorRenderer interface {
	RenderError(http.ResponseWriter, *http.Request, ErrorDocument) error
	ContentType() string
	OpenAPISchema() any
}
```

错误响应与成功 Envelope 分离：

```text
成功 → Codec / Envelope
错误 → Normalizer / i18n / ErrorRenderer
```

Renderer 只允许 Server 级配置，避免同一 API 混用协议。

### 6.2 简洁 JSON

```go
ghttp.WithErrorRenderer(ghttp.JSONErrorRenderer())
```

```json
{
  "status": 404,
  "message_id": "user.not_found",
  "args": {"user_id": 123},
  "message": "用户 123 不存在"
}
```

Content-Type 为 `application/json`。

### 6.3 RFC 9457

```go
ghttp.WithErrorRenderer(ghttp.ProblemJSONRenderer())
```

```json
{
  "type": "urn:ghttp:error:user.not_found",
  "status": 404,
  "title": "user.not_found",
  "detail": "用户 123 不存在",
  "message_id": "user.not_found",
  "args": {"user_id": 123}
}
```

Content-Type 为 `application/problem+json`。字段级错误使用 `errors` 扩展字段。

用户可实现自定义 Renderer。Renderer 失败且响应未提交时，框架使用内置最小 JSON Renderer；已提交时只记录错误，不二次写入。

## 7. ghttp/i18n 中间件

### 7.1 请求协议

- 服务端始终返回 `message_id + args`。
- 客户端明确提供语言信号时，服务端额外返回 `message`。
- 真正生成 message 时设置 `Content-Language`。
- message 随 `Accept-Language` 变化时自动添加 `Vary: Accept-Language`。
- 翻译失败不得改变 HTTP status、message ID 或 Args。

### 7.2 核心本地化协议

```go
type Message struct {
	ID   string
	Args map[string]any
}

type LocalizedMessage struct {
	Text     string
	Language string
}

type MessageLocalizer interface {
	Localize(context.Context, string, Message) (LocalizedMessage, error)
}
```

### 7.3 LocaleResolver

```go
type LocaleResolver interface {
	Resolve(context.Context, *http.Request) LocaleResolution
}

type LocaleResolution struct {
	Languages []string
	Explicit  bool
}
```

默认允许组合：

```text
用户偏好 → Cookie → query → Accept-Language → 默认语言
```

ResolverChain 选择第一组明确候选，再交给 LanguageMatcher 匹配支持语言。没有任何语言信号时仍可使用默认语言，但 `Requested=false`，错误响应不增加 message。

### 7.4 惰性 Session

i18n middleware 只向 context 注入惰性 Session。Session 第一次被错误管线或手动 API 使用时才解析语言、匹配 fallback 并初始化请求 Localizer。

成功请求不使用翻译时：

- 不解析完整 Accept-Language。
- 不访问 Catalog。
- 不构造 ErrorDocument。
- 不调用 Renderer 或 Observer。

Session 必须使用 `sync.Once` 或等价机制保证并发安全。

### 7.5 Gin 风格手动入口

```go
message, err := ghttpi18n.GetMessage(ctx, "welcome", args)
message := ghttpi18n.MustGetMessage(ctx, "welcome", opts...)
```

手动入口与自动错误翻译共用同一个 Session、语言和 fallback。

### 7.6 Catalog

```go
type Catalog interface {
	Localize(context.Context, string, ghttp.Message) (ghttp.LocalizedMessage, error)
}
```

官方扩展提供：

- go-i18n adapter：plural、模板、JSON/YAML/TOML、embed.FS。
- StaticCatalog：测试和简单服务。
- 自定义 Catalog 接口。

生产环境启动时加载并编译为只读快照。开发 reload 必须显式开启。

翻译查找顺序：

```text
应用 Catalog → 框架 Catalog → 仅返回 message_id
```

框架 Catalog 仅提供英文框架错误 fallback，不内置大量语言。

## 8. 中间件和原始 Handler

Typed Handler 和 `ToHTTPFunc` 返回的 error 自动进入统一管线。

标准 `Middleware func(http.Handler) http.Handler` 无法直接返回 error，使用：

```go
func RespondError(http.ResponseWriter, *http.Request, error)
```

`RespondError` 必须复用当前 Server 的 Normalizer、i18n、Renderer 和 Observer。

`ToHTTP` 与 `ToRaw` 自己拥有响应，框架无法捕获未返回或未传递的 error。用户应改用 `ToHTTPFunc`、调用 `RespondError`，或完全自行处理响应。

响应已提交或 hijack 后出现错误时，只观察和记录，不执行本地化和 Renderer。

## 9. Params 安全

Params 属于公共 API，输出前必须 sanitize。

允许：

- nil、bool、string、整数、浮点数。
- `time.Time` 的 RFC 3339 表示。
- 固定策略表示的 `time.Duration`。
- 上述类型的 slice。
- string key 的 map。
- 实现 `PublicParamMarshaler` 的类型。

禁止自动公开：

- error、函数、channel、任意指针。
- 任意 struct、数据库模型、请求对象。
- token、credential、堆栈、SQL、路径等内部数据。
- 循环引用。

非法参数被删除并记录诊断，不能使用 `fmt.Sprint` 降级。限制嵌套深度、参数数量、字符串长度和总序列化体积。

Message ID 使用稳定层级格式，例如：

```text
user.not_found
order.stock_insufficient
validation.required
internal.server_error
```

非法 ID 对外降级为 `internal.server_error`，对内记录原始 ID。

## 10. ErrorObserver

```go
type ErrorObserver interface {
	ObserveError(context.Context, ErrorObservation)
}
```

观察内容包括 status、message ID、Kind、Op、Cause、Meta、请求语言、实际语言、本地化错误、Renderer 错误和响应提交状态。

规则：

- Observer 不能修改响应。
- Observer panic 必须隔离。
- 一个 Observer 失败不能阻断其他 Observer。
- 每个最终错误只观察一次。
- Cause 和 Meta 只传给 Observer，不传给 Renderer。

后续 `gotel` 可以实现 tracing、metrics 和日志关联，但本设计不直接依赖 OpenTelemetry。

## 11. OpenAPI

ErrorRenderer 提供错误 content type 与 schema。简洁 JSON 和 RFC 9457 分别生成对应 component。

`message` 必须标记为 optional。启用 i18n middleware 时可描述 `Content-Language`；未启用时不增加语言相关文档。

框架自动文档化可确定的 400、413、415、422 和 500。业务 Handler 可能返回的错误无法从函数体静态推断，继续通过文档元数据声明：

```go
ghttp.Errors(
	ghttp.ErrorSpec{Status: 404, MessageID: "user.not_found"},
)
```

该声明只影响 OpenAPI，不参与运行时映射，不构成错误注册表。

## 12. 翻译和渲染失败

翻译属于展示增强：

- 当前语言缺失时按 fallback 查找。
- 所有语言缺失、模板参数错误、Catalog error 或 panic 时省略 message。
- 原始 status、message ID 和 Args 保持不变。
- 失败信息进入 Observer。

Renderer 属于响应关键路径：

- 未提交时失败，使用内置最小 JSON fallback。
- 已提交时失败，只记录。
- fallback Renderer 不参与 i18n，避免递归。

## 13. 测试策略

### 13.1 gerr

- New、Wrap、Cause、Op、Message。
- 外层 Descriptor 优先和普通 `%w` 包装。
- Params/Meta 隔离和防御性复制。
- 自定义 Describer。
- errors.Is、errors.As、Unwrap。
- 非法 ID。
- 裸 errors.Join 和显式 MultiError。

### 13.2 Normalizer

- 全部 Kind 默认映射和自定义 mapper。
- HTTPStatusCarrier 优先。
- 框架错误 ID。
- 普通 error、panic 和敏感信息安全降级。
- response committed/hijacked。
- 自定义 Normalizer 装饰。

### 13.3 validator

- required、email、min、max、oneof、len。
- path/query/header/cookie/body location。
- 多字段错误和未知 tag。
- 自定义 Validator 无 adapter。
- 原始文本和值不泄露。

### 13.4 i18n

- Accept-Language q 权重、wildcard、非法语法和区域 fallback。
- ResolverChain 优先级。
- Requested true/false。
- 惰性初始化、并发安全和 fallback。
- 缺失 ID、模板错误、Catalog error/panic。
- Content-Language 与 Vary。
- 字段级 details 翻译。

### 13.5 Renderer 与集成

- JSON、Problem JSON 和 Custom Renderer。
- HEAD body 抑制。
- Renderer error/panic 和 fallback。
- Typed Handler、ToHTTPFunc、RespondError、普通 error、wrapped gerr、validator、panic。
- OpenAPI schema 与 content type。

### 13.6 Fuzz、race 和 benchmark

Fuzz message ID、Accept-Language、Params sanitizer、error chain 和 Renderer。

Race 覆盖 Catalog 并发读取、Session 初始化、ResolverChain、Observer 和 reload。

基准：

```text
BenchmarkSuccessWithoutI18n
BenchmarkSuccessWithLazyI18n
BenchmarkNormalizeGerr
BenchmarkNormalizeWrappedGerr
BenchmarkValidationErrors
BenchmarkLocalizeError
BenchmarkJSONErrorRenderer
BenchmarkProblemErrorRenderer
```

性能门槛：

- 未启用功能的成功路径不得新增分配。
- 启用惰性 middleware 但未翻译时最多增加一次 context 注入分配。
- 普通结构化错误归一化不使用反射。
- 生产 Catalog 请求期不做文件 IO 或模板编译。

## 14. 完成标准

- Handler 可以自然返回普通 error、wrapped gerr 或自定义 Describer。
- 框架错误和 validator 错误自动生成稳定 message ID。
- 无语言信号时响应不包含 message。
- 明确语言信号时响应包含正确翻译、Content-Language 和 Vary。
- 翻译失败不改变原错误语义。
- JSON、RFC 9457 和自定义 Renderer 均可选择。
- Params 与 Meta 不发生泄漏。
- response committed/hijacked 后不二次写入。
- OpenAPI 与所选 Renderer 一致。
- 未启用功能的成功路径无显著性能回归。
- `go test -race ./gerr/... ./ghttp/...` 通过。
- fuzz 和目标 benchmark 有可复现记录。

