# ghttp 错误国际化设计

> 日期：2026-07-24
>
> 状态：交互设计已确认，书面规格第三轮审查通过
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

### 3.1 包依赖方向

依赖方向必须固定为：

```text
gerr ← ghttp ← ghttp/i18n
```

- `gerr` 不 import `ghttp`。
- `ghttp` 不 import `ghttp/i18n`。
- 本地化桥接接口、请求 context key 和存取函数定义在 `ghttp`。
- `ghttp/i18n` 只实现 `ghttp` 定义的接口并通过 middleware 注入。
- 第三方本地化 middleware 可以实现同一接口，不需要依赖官方子包。

`ghttp` 定义：

```go
type RequestLocalizer interface {
	Explicit() bool
	LocalizeDocument(context.Context, ErrorDocument) (LocalizationResult, error)
}

type LocalizationResult struct {
	Document ErrorDocument
	Language string
	Cache    LocaleCachePolicy
}

func WithRequestLocalizer(context.Context, RequestLocalizer) context.Context
func RequestLocalizerFromContext(context.Context) (RequestLocalizer, bool)
```

context key 由 `ghttp` 私有持有，外部只能通过上述函数读写。

本节中的 `Message`、`LocalizedMessage`、`RequestLocalizer`、`LocalizationResult` 和 `LocaleCachePolicy` 均定义在 `ghttp`。`ghttp/i18n` 只提供实现。

### 3.2 i18n 集成安装

普通 `Middleware` 函数无法携带 OpenAPI 元数据，因此官方 i18n 返回一个集成对象：

```go
type ErrorIntegration interface {
	Middleware() Middleware
	ErrorOpenAPIHeaders() map[string]map[string]any
}

func (s *Server) UseErrorIntegration(ErrorIntegration)
```

`ghttp/i18n.Integration` 实现该接口。`UseErrorIntegration` 在 freeze 前同时注册 middleware 和错误 OpenAPI header contributor；freeze 后调用 panic `ErrServerFrozen`。第三方实现也可使用该接口。官方扩展可提供便捷方法 `integration.Install(server)`，内部只调用 `UseErrorIntegration`。

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

有效 Descriptor 必须同时具有合法非空 ID 和非 `KindUnknown` Kind。

`Describe` 的完整规则：

1. 当前 error 实现 `Describer` 且 Descriptor 有效时立即返回。
2. 当前 error 是 `*gerr.Error` 但没有 ID，仅表示内部上下文，继续检查单一 cause。
3. 当前 error 实现 `Unwrap() error` 时沿该链继续。
4. 当前 error 实现 `Unwrap() []error` 时停止并返回 false；不得选择任意分支。
5. 无效 Descriptor 记录不到公共结果，继续单一 cause；若没有单一 cause则返回 false。

返回的 Params 必须 sanitize 后防御性复制。

自定义错误可以实现 `Describer`，不必使用 `*gerr.Error`。

裸 `errors.Join`、嵌套 join 和多个 Describer 不自动选择某个子错误作为公开语义。

`MultiError` API：

```go
func NewMulti(id string, kind Kind, errs []error, opts ...Option) *MultiError
func (e *MultiError) ErrorDescriptor() Descriptor
func (e *MultiError) Unwrap() []error
```

`MultiError` 的顶层 ID、Kind 和 Params 决定公开顶层语义。子错误默认只用于日志；只有 validation adapter 或显式 detail adapter 才能将子错误投影为 `ErrorDetail`。

显式 detail adapter 定义在 `ghttp`：

```go
type ErrorDetailAdapter interface {
	AdaptErrorDetails(context.Context, error) ([]ErrorDetail, bool)
}
```

Server 可配置多个 adapter，按注册顺序使用第一个返回 `ok=true` 的结果。adapter 结果仍需 message ID 校验、Params sanitize 和稳定排序。

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
	SuppressResponse bool
}
```

Renderer 只能接收 `ErrorDocument`，不能访问 Cause、Meta 或 Op。

### 5.2 ErrorNormalizer

```go
type ErrorNormalizer interface {
	NormalizeError(context.Context, error) NormalizedError
}
```

自定义 Normalizer 的返回值仍必须经过框架最终校验、status 校验、ID 校验、Params sanitize 和防御性复制。Normalizer panic 被隔离并降级为 `internal.server_error`。

默认处理顺序：

归一化分为正交的两条路径，不能按 first-match 混在一起：

1. 身份路径：框架错误 → validationStageError → `gerr.Describe` → context 错误 → 未知安全降级，得到 message ID、Kind、Args 和 Details。
2. 状态路径：框架固定 status → 最外层合法 `HTTPStatusCarrier` → KindStatusMapper，得到 HTTP status。

最后组合并统一校验。HTTPStatusCarrier 只能覆盖 status，不能独立提供错误身份。

未知错误固定输出：

```json
{
  "status": 500,
  "message_id": "internal.server_error",
  "args": {}
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
| Canceled | 不写响应，仅观察 |
| Internal/Unknown | 500 |

`context.Canceled` 和 `KindCanceled` 通常表示客户端断开或上游取消，默认生成 `SuppressResponse=true` 的 NormalizedError，不尝试写 408。读取请求超时映射 408，服务端处理 deadline 映射 504。KindStatusMapper 只映射 status，不能取消 SuppressResponse；需要改变取消语义时必须使用自定义 Normalizer。

```go
type HTTPStatusCarrier interface {
	HTTPStatus() int
}
```

HTTP status 只沿单一 error chain 从外到内查找，最外层合法状态优先；遇到 `Unwrap() []error` 停止。合法状态限定为 400–599。0、1xx、2xx、3xx 或大于 599 的值视为无效，记录诊断后继续按 Kind 映射。

HTTP adapter 可以使用 `WithStatus(err, status)` 显式表达 410、412、413、416、422、451 等精确状态。业务层不应 import `ghttp`。框架内部错误的固定 status 优先于用户 Kind 映射；显式 HTTP status 优先于业务 Kind。

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

绑定或验证阶段必须先用私有 `validationStageError` 包装 validator 返回值，Normalizer 不得仅凭 error 类型猜测错误来源。Handler 主动返回相同 validator 类型时按普通业务错误处理。

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

location 使用请求协议字段名而非 Go 字段名：优先使用 `path`、`query`、`header`、`cookie`、`json` tag，并保留嵌套和 slice index，例如 `body.items[2].email`。嵌入字段按最终协议路径展开。StructLevel 错误必须由 adapter 显式提供 location。details 按 location、message ID 稳定排序。

未知 validator tag 先转为小写 ASCII snake_case 并通过 message ID 校验，再使用 `validation.<tag>`；无法规范化时使用 `validation.unknown`。无法识别自定义 Validator 错误时返回顶层 422，不暴露原始文本。

## 6. 错误响应格式

### 6.1 ErrorRenderer

```go
type ErrorRenderer interface {
	RenderError(http.ResponseWriter, *http.Request, ErrorDocument) error
	OpenAPIDescriptor() ErrorOpenAPIDescriptor
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

用户可实现自定义 Renderer。错误 Renderer 必须写入框架提供的最大 64 KiB 隔离缓冲 writer，不能依赖 Flush、Hijack、Push 或流式语义。缓冲 writer 不实现 `Unwrap`，也不能通过 `http.ResponseController` 到达真实 writer。成功返回且未超限后，框架一次性复制 header、status 和 body 到真实 ResponseWriter；HEAD 只提交 header/status。

Renderer 返回 error、panic、写入超限或产生非法 status 时丢弃缓冲区并使用内置最小 JSON Renderer。若 Renderer 未调用 `WriteHeader`，提交时使用 `ErrorDocument.Status`；若写入的合法 status 与 `ErrorDocument.Status` 不一致，也视为 Renderer 失败。若进入错误管线前真实响应已经 committed/hijacked，则完全跳过 Renderer，只记录错误。必须测试 `http.NewResponseController(buffer).Flush/Hijack` 无法绕过隔离。

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
	Cache     LocaleCachePolicy
}
```

默认允许组合：

```text
用户偏好 → Cookie → query → Accept-Language → 默认语言
```

ResolverChain 选择第一组明确候选，再交给 LanguageMatcher 匹配支持语言。没有任何语言信号时仍可使用默认语言，但 `Explicit=false`，错误响应不增加 message。

明确语言信号定义：

- 非空且语法有效、至少包含一个 `q>0` 候选的 Accept-Language 为 Explicit。
- query/Cookie/用户偏好存在但语法非法时不算 Explicit，并继续下一个 resolver。
- 只有 wildcard 或候选均不受支持仍算 Explicit，最终匹配默认语言。
- 空 header、全部 `q=0` 或只有空白不算 Explicit。

缓存策略由 resolver 报告：

```go
type LocaleCachePolicy struct {
	Vary       []string
	Private    bool
	NoStore    bool
}
```

- Accept-Language resolver 增加 `Vary: Accept-Language`。
- Cookie resolver 至少增加 `Vary: Cookie`，默认 `Private=true`。
- 用户身份偏好默认 `Private=true`；若来源可能含敏感身份状态可设置 `NoStore=true`。
- query 已进入 URL cache key，默认不增加 Vary。
- 多个 Vary 必须去重追加，不能覆盖已有值。

缓存策略不是只取获胜 resolver，而是对所有已配置、未来可能改变选择结果的 resolver 保守聚合。`Vary` 取并集，`Private` 和 `NoStore` 使用逻辑 OR。错误管线必须在提交 Renderer 缓冲结果之前应用 LocalizationResult.Cache。

### 7.4 惰性 Session

i18n middleware 只向 context 注入惰性 Session。Session 第一次被错误管线或手动 API 使用时才解析语言、匹配 fallback 并初始化请求 Localizer。

成功请求不使用翻译时：

- 不解析完整 Accept-Language。
- 不访问 Catalog。
- 不构造 ErrorDocument。
- 不调用 Renderer 或 Observer。

Session 必须使用 `sync.Once` 或等价机制保证并发安全。

错误文档必须锁定单一实际语言：按候选与 fallback 顺序选择第一个能翻译顶层 message ID 的语言，然后所有 details 只在该语言查找；缺失 detail message 时省略该 detail 的 message，不切换成另一语言。`Content-Language` 因此始终准确表示整份错误文档。Catalog 的“未找到”与运行错误必须使用可判断 sentinel 区分；未找到可继续 fallback，运行错误记录后停止该 Catalog 并继续下一个组合 Catalog。

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

标准 `Middleware func(http.Handler) http.Handler` 无法直接返回 error，优先使用 Server 方法：

```go
func (s *Server) RespondError(http.ResponseWriter, *http.Request, error)
```

请求已经进入 Server 时也可使用便捷函数：

```go
func RespondError(http.ResponseWriter, *http.Request, error)
```

`RespondError` 必须复用当前 Server 的 Normalizer、i18n、Renderer 和 Observer。

便捷函数只在 request context 中存在当前 Server 时工作；不存在时写固定、无原始错误文本的最小 JSON 500，并记录集成错误。它不得调用 `http.Error(err.Error())`。外层 middleware 应持有 `*Server` 并调用 `server.RespondError`。

`ToHTTP` 与 `ToRaw` 自己拥有响应，框架无法捕获未返回或未传递的 error。用户应改用 `ToHTTPFunc`、调用 `RespondError`，或完全自行处理响应。

响应已提交或 hijack 后出现错误时，只观察和记录，不执行本地化和 Renderer。

## 9. Params 安全

Params 属于公共 API，输出前必须 sanitize。

扩展接口定义为：

```go
type PublicParamMarshaler interface {
	MarshalPublicParam() (any, error)
}
```

返回值必须再次经过同一个递归 sanitizer；不能信任为已安全值。

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

默认限制：最大嵌套深度 8、顶层参数 32、单个 map/slice 64 项、单个字符串 4 KiB、Args JSON 总量 32 KiB。超限单项被删除；若顶层参数超限则保留按 key 字典序排列的前 32 项；若最终 JSON 仍超 32 KiB，则整份 Args 置空并记录诊断。

map key 必须稳定排序。NaN、Inf、typed nil、非法 `json.Number` 被删除。`PublicParamMarshaler` panic/error/超大结果视为非法单项。sanitize 完成后再次深拷贝，Localizer 和 Renderer 不共享可变 map。

Message ID 正式语法为 ASCII：`^[a-z][a-z0-9_]{0,31}(\.[a-z][a-z0-9_]{0,31}){1,7}$`，总长度不超过 128 字节。保留 `http.*`、`request.*`、`validation.*`、`internal.*` 给框架使用。业务 ID 使用自己的顶层命名空间。

框架内部错误携带私有、不可伪造的来源标记。ghttp 最终校验发现普通 `gerr.Error` 或自定义 Describer 使用保留命名空间时，将其降级为 `internal.server_error` 并通知 Observer；`gerr` 本身不判断命名空间，因为其他传输层可能有不同保留规则。

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

配置允许多个 observer：

```go
func WithErrorObservers(...ErrorObserver) ServerOption
```

observer 按注册顺序同步调用，在本地化和最终 Renderer 尝试结束后、ServeHTTP 返回前执行一次，因此 observation 能包含 resolved language、renderer error 和 committed 状态。observer 耗时计入请求尾延迟；需要异步处理的实现必须自行复制数据并排队，不能保留 request 或可变 map。

规则：

- Observer 不能修改响应。
- Observer panic 必须隔离。
- 一个 Observer 失败不能阻断其他 Observer。
- 每个最终错误只观察一次。
- Cause 和 Meta 只传给 Observer，不传给 Renderer。

后续 `gotel` 可以实现 tracing、metrics 和日志关联，但本设计不直接依赖 OpenTelemetry。

## 11. OpenAPI

Renderer 不直接返回松散的 `any`，而是提供明确描述：

```go
type ErrorOpenAPIDescriptor struct {
	ContentType   string
	ComponentName string
	Schema        map[string]any
	Headers       map[string]map[string]any
}

type ErrorRenderer interface {
	RenderError(http.ResponseWriter, *http.Request, ErrorDocument) error
	OpenAPIDescriptor() ErrorOpenAPIDescriptor
}
```

compiler 按 ComponentName 去重；同名不同 schema 为启动期配置错误。简洁 JSON 和 RFC 9457 分别生成对应 component。i18n 通过 `UseErrorIntegration` 在 Server freeze 前贡献 `Content-Language` 和缓存相关 header 描述，freeze 后配置不可变。

`message` 必须标记为 optional。启用 i18n middleware 时可描述 `Content-Language`；未启用时不增加语言相关文档。

同一 status 可以声明多个 message ID；OpenAPI response description 列出这些 ID，schema 仍复用统一错误 component。框架自动错误与 `ghttp.Errors` 按 status 合并并去重。框架自动文档化可确定的 400、413、415、422 和 500。业务 Handler 可能返回的错误无法从函数体静态推断，继续通过文档元数据声明：

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

## 13. RFC 9457 映射

- `type` 为 `urn:ghttp:error:<percent-encoded-message-id>`。
- `title` 固定为 message ID，保证无语言时稳定；这是有意的扩展选择。
- 有本地化 message 时写入 `detail`，否则省略 `detail`。
- 同时保留扩展字段 `message_id` 与 `args`。
- 字段级 `errors` 项包含 location、message_id、args，以及可选 message。
- status 必须与实际 HTTP status 相同。
- `args` 是必需对象字段，空时返回 `{}`；简洁 JSON、RFC 9457 和 OpenAPI 均采用同一规则。

## 14. 兼容性与迁移

本设计按用户决定为 breaking change，不提供兼容层。

- `gerr.New(message, opts...)` 改为 `gerr.New(id, kind, opts...)`。
- `gerr.Wrap(err, message, opts...)` 改为 `gerr.Wrap(err, opts...)`，开发者文本使用 `WithMessage`。
- `Error.Code`/`WithCode` 改为 `Error.ID`/`WithID`。
- 新增公开 Params，与 Meta 严格分离。
- 现有 `ghttp.HTTPError` 被 `gerr.Error + HTTPStatusCarrier` 取代。
- 现有 `ErrorHandler`/`WithErrorHandler` 被 `ErrorNormalizer`、`ErrorRenderer`、`ErrorObserver` 取代并删除。
- Envelope 不再处理错误，只处理成功响应。
- README 和迁移文档必须提供旧新 API 对照与最小示例。

## 15. 测试策略

### 15.1 gerr

- New、Wrap、Cause、Op、Message。
- 外层 Descriptor 优先和普通 `%w` 包装。
- Params/Meta 隔离和防御性复制。
- 自定义 Describer。
- errors.Is、errors.As、Unwrap。
- 非法 ID。
- 裸 errors.Join 和显式 MultiError。

### 15.2 Normalizer

- 全部 Kind 默认映射和自定义 mapper。
- HTTPStatusCarrier 优先。
- 框架错误 ID。
- 普通 error、panic 和敏感信息安全降级。
- response committed/hijacked。
- 自定义 Normalizer 装饰。

### 15.3 validator

- required、email、min、max、oneof、len。
- path/query/header/cookie/body location。
- 多字段错误和未知 tag。
- 自定义 Validator 无 adapter。
- 原始文本和值不泄露。

### 15.4 i18n

- Accept-Language q 权重、wildcard、非法语法和区域 fallback。
- ResolverChain 优先级。
- Explicit true/false。
- 惰性初始化、并发安全和 fallback。
- 缺失 ID、模板错误、Catalog error/panic。
- Content-Language 与 Vary。
- 字段级 details 翻译。

### 15.5 Renderer 与集成

- JSON、Problem JSON 和 Custom Renderer。
- HEAD body 抑制。
- Renderer error/panic 和 fallback。
- Typed Handler、ToHTTPFunc、RespondError、普通 error、wrapped gerr、validator、panic。
- OpenAPI schema 与 content type。

### 15.6 Fuzz、race 和 benchmark

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

性能基线使用变更前同机同 commit 构建的 benchmark 输出，`go test -run '^$' -bench <name> -benchmem -count=10`，通过 benchstat 比较。CI 保存基准工件但不对噪声较大的 ns/op 做单次硬失败；allocs/op 使用精确门槛，ns/op 以统计显著且超过 5% 判定回归。

性能门槛：

- 未启用功能的成功路径不得新增分配。
- 启用惰性 middleware 但未翻译时最多增加一次 context 注入分配。
- 普通结构化错误归一化不使用反射。
- 生产 Catalog 请求期不做文件 IO 或模板编译。

## 16. 完成标准

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
