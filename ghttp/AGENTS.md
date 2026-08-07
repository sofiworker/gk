# ghttp 包设计约束

本文件只约束 `ghttp/` 目录下的改动，优先级高于仓库根 `AGENTS.md` 的通用约定（就近原则）。所有计划、评审、实现必须能论证满足下列目标；任何违背必须写入计划文档并等待评审，不得静默偏离。

## 实施优先级

- 实施优先级从高到低：
  1. **标准 HTTP 方法的 server 侧**：路由、HTTP 语义、协商、错误、能力层、OpenAPI；
  2. **WS/SSE 的 server 侧**：允许实施，但不得挤占标准 HTTP 方法相关改动；
  3. **client 侧**：不做正式实现。
- **client 侧不做正式实现**：仅允许 demo 或明确标记为延后的占位；统一 client 实现由用户后续单独安排，本目录计划不得提前展开。

## 七条设计目标

详细依据与横评见 `docs/superpowers/plans/2026-08-06-ghttp-design-goals-and-rework.md`，此处只保留可执行结论：

1. **满足 RFC 与 HTTP 规范**：HEAD、405/Allow、OPTIONS、内容协商、状态码语义、错误响应必须符合规范，不是“够用就好”。
2. **Server 统一处理、handler 能逃生**：错误、404/405、panic、协商、编码由 Server 统一负责；保留 ToHTTP/ToRaw/ToHTTPFunc/ToSSE/ToWebSocket/ToStatic 逃生终结器。
3. **泛型优先，路由匹配零分配**：公开 API 以泛型类型化为主；matcher 与参数提取热路径不得新增分配。
4. **方便的使用流程**：注册、参数、校验、错误、文档的默认流程少样板，且可预测。
5. **提供常见能力而非库封装**：日志（zap/slog 能力）、RBAC 鉴权、限流、审计等以“能力接口 + 默认实现”提供，不直接封装第三方库；适配放独立子包。
6. **显式优于隐式**：见下方强制约束。
7. **OpenAPI 自动推断**：从类型、tag、路由元数据推断文档，不使用 go-swagger 式注释/DSL；推断优先、宁缺毋滥。

## 强制约束

### 显式优于隐式

- 影响外部可观察行为的特性必须显式开启，默认值必须是保守值。
- 例：500 是否泄露内部错误默认关闭，需 `WithExposeErrorDetails` 显式开启；Envelope 默认关闭，需 `WithEnvelope`；服务器级 validator 默认关闭，需 `WithValidator`。
- 仅允许两类隐式默认：① 协议级便利（HEAD 回退 GET、缺省 Content-Type 等），必须在文档中明示为默认值；② 纯内部实现细节。
- 禁止“魔法字段/魔法行为”作为特性存在；`Body` 字段约定、自动校验等必须显式化或在文档中长期声明。

### 非 200 状态码语义

- 状态码非 200 时必须返回真实 HTTP 状态码与对应内容。
- Envelope 只改变 body 表示，不允许改写状态码；错误响应必须保留真实 4xx/5xx。
- 类型化 handler 支持显式声明成功状态码（201/202/204 等），不强制退回 `ToHTTPFunc`；不通过响应结构体字段名隐式推断状态。

### Go 1.27 泛型方法迁移

- 当前 `ghttp.Route[Req, Resp](target).METHOD(path).To(handler)` 是过渡形态，因 Go 1.27 前方法不支持类型参数。
- Go 1.27 泛型方法可用后，演进为方法链 `target.Route().METHOD(path).To[Req, Resp](handler)`（类型由 handler 推断），不再经包级函数绕层。
- **双版本共存（build tag 自动选择）**：本包同时保留两套 API，靠 `//go:build go1.27` / `//go:build !go1.27` 按工具链版本自动选择（类似 Go 标准库），使用者无需显式传 tag：
  - `builder_pre127.go`：泛型 `RouteBuilder[Req, Resp]` + 包级 `Route[Req,Resp](target)`，Go 1.27 前编译；
  - `builder_go127.go`：非泛型 `RouteBuilder` + 泛型终结方法（`To[Req,Resp]`、`ToNoInput[Resp]`、`ToNoOutput[Req]`、`ToHTTPFunc[Req]`、`ToRedirectFunc[Req]`）+ `Server/Group.GET/POST/...` 直接链式起点（根组语义，Server/Group 上**没有** `Route()` 方法，也没有小写快捷注册，统一 `动词(path).Doc(...).To(handler)`），Go 1.27+ 编译；
  - 1.27 构建中包级 `Route[Req,Resp](target)` 仅保留为 **deprecated 兼容空壳**（忽略类型参数，直接返回同一个根组 builder，供 1.27 前代码/测试原样编译），新代码禁止使用；现有测试两套工具链下都必须通过。
- 两个版本的 `RouteBuilder` 都提供 `.Group(prefix, mws...)` 分支（gin 的 `r.Group` 语义）：须在设置 method/path 之前调用，返回以当前 target 为根的 `*Group`，不转移已设置的路由级选项。
- 两套 API 共享 `routeBuilderCore`（`builder_core.go`）与所有注册/语义/OpenAPI 内部模型；任何实现不得把泛型参数固化进 Server/Group 类型；相关计划须标注迁移意图。
- 验证与格式化：Go 1.27 专用文件含泛型方法语法，**必须用 Go 1.27+ 工具链的 gofmt/gofmt 格式化**（旧 gofmt 无法解析）；两套工具链均需 `go test ./ghttp/` 与 `go vet ./ghttp/` 通过。

### 无输入 / 无输出终结器

- 无输入路由使用显式 `.ToNoInput(handler)`：不解析 body、不校验 Content-Type、不校验请求参数，handler 签名 `func(context.Context) (Resp, error)`。
- 无输出路由使用显式 `.ToNoOutput(handler)`：handler 签名 `func(context.Context, Req) error`，成功默认 204（`.Status(code)` 可覆盖），错误走统一错误管线；OpenAPI 响应无 content。
- 禁止用 `struct{}` 或“零值自动 204”等魔法代替显式终结器；`Route[Req, struct{}]` 中残余的 `struct{}` 只是 Go 1.27 前过渡形态的类型占位。

## 验证与门禁

- 行为默认值变更必须同步更新测试、README 与迁移说明，并在计划中标注破坏性。
- 影响 HTTP 语义的行为（HEAD、405/Allow、OPTIONS、协商、错误状态码）必须用真实 HTTP 请求测试验证，而非仅单测。
- 性能改动必须有 benchmark 证据；matcher 热路径不得新增分配，全链路分配变化需说明。
- 新能力以“能力接口 + 默认实现”提供；如无必要不得新增第三方依赖。

## 关联文档

- 设计目标落实与重构计划：`docs/superpowers/plans/2026-08-06-ghttp-design-goals-and-rework.md`
- 实施任务计划：`docs/superpowers/plans/2026-08-06-ghttp-server-issues-implementation.md`
- 路由设计基线：`docs/superpowers/specs/2026-07-10-ghttp-server-routing-design.md`
- API 设计基线：`docs/superpowers/specs/2026-06-29-ghttp-route-builder-api.md`
