# ghttp Error i18n Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `ghttp` 建立客户端优先、服务端可选翻译的结构化错误管线，并以 breaking change 重构 `gerr` 错误语义。

**Architecture:** `gerr` 只表达跨传输层的 ID、Kind、公开 Params 与私有诊断信息；`ghttp` 负责归一化、HTTP 状态、渲染和观察；`ghttp/i18n` 通过请求 context 注入惰性本地化 Session。依赖方向固定为 `gerr <- ghttp <- ghttp/i18n`，不建立全局错误注册表，也不解析 `err.Error()`。

**Tech Stack:** Go 标准库、现有 `go-playground/validator/v10`、`net/http`、OpenAPI 3.1、Go testing/httptest/fuzz/benchmark。

---

## 文件结构

- `gerr/error.go`：新 Error、Kind、Option、构造与包装。
- `gerr/descriptor.go`：Message ID 校验、Descriptor/Describer/Describe。
- `gerr/multi.go`：显式顶层语义的 MultiError。
- `gerr/*_test.go`：错误链、隔离和 breaking API 测试。
- `ghttp/error_model.go`：ErrorDocument、NormalizedError、扩展接口。
- `ghttp/error_params.go`：公开参数 sanitizer 和防御性复制。
- `ghttp/error_normalizer.go`：默认归一化、Kind/status、框架错误身份。
- `ghttp/error_validation.go`：validation stage 标记和 validator adapter。
- `ghttp/error_renderer.go`：JSON、RFC 9457 和 64 KiB 隔离渲染。
- `ghttp/error_pipeline.go`：RespondError、本地化、fallback、Observer。
- `ghttp/error_context.go`：Server 与 RequestLocalizer context 桥。
- `ghttp/config.go`, `ghttp/server.go`, `ghttp/writer.go`：新配置和统一错误入口。
- `ghttp/openapi*.go`：Renderer schema、错误声明和集成 headers。
- `ghttp/i18n/integration.go`, `resolver.go`, `session.go`, `catalog.go`：Integration、Resolver、Session、Catalog 和手动 API。
- `ghttp/i18n/goi18n/catalog.go`, `catalog_test.go`：go-i18n plural/template 与 embed.FS 只读快照 adapter。
- `ghttp/README.md`, `gerr/README.md`：迁移、示例与 breaking change。

### Task 1: 重构 gerr 错误身份

**Files:**
- Modify: `gerr/error.go`
- Create: `gerr/descriptor.go`
- Modify: `gerr/error_test.go`
- Create: `gerr/descriptor_test.go`

- [ ] 写失败测试：覆盖 `New(id, kind)`、新增 Kind、Params/Meta 分离、Wrap 内部上下文、`errors.Is/As`。
- [ ] 运行 `go test ./gerr -run 'Test(New|Wrap|Error)'`，确认因新 API/字段缺失而失败。
- [ ] 最小实现 `Error`、Option、ID 校验、构造/包装和防御性 map 复制。
- [ ] 写失败测试：Describe 外层优先、`fmt.Errorf("%%w")`、自定义 Describer、join 停止、非法 descriptor。
- [ ] 实现 Descriptor/Describer/Describe 并运行 `go test ./gerr` 至通过。
- [ ] 运行 `rg -n '\b(Code|WithCode|IsCode)\b|gerr\.New\(|gerr\.Wrap\(' --glob '!docs/**' --glob '!vendor/**' --glob '!.git/**' .`，逐个迁移全仓旧 gerr API；运行 `go test ./gerr ./ghttp/...` 验证受影响包编译通过。
- [ ] `gofmt -w gerr`，提交 `feat(gerr): add structured error descriptors`。

### Task 2: 重构 MultiError 与检查 API

**Files:**
- Modify: `gerr/multi.go`
- Modify: `gerr/multi_test.go`
- Modify: `gerr/inspect.go`
- Modify: `gerr/README.md`

- [ ] 写失败测试：`NewMulti(id, kind, errs, opts...)` 提供显式顶层 descriptor，普通 join 不被 Describe 自动选枝。
- [ ] 运行 `go test ./gerr -run 'Test(Multi|Describe)'` 验证 RED。
- [ ] 实现 MultiError，并将 `IsCode` breaking 替换为 `IsID`。
- [ ] 更新 README 新旧 API 对照并运行 `go test ./gerr`。
- [ ] 提交 `feat(gerr): define explicit multi error semantics`。

### Task 3: 公共错误模型与 Params 安全边界

**Files:**
- Create: `ghttp/error_model.go`
- Create: `ghttp/error_params.go`
- Create: `ghttp/error_params_test.go`
- Modify: `ghttp/error.go`

- [ ] 写编译期/行为失败测试定义 ErrorDocument、NormalizedError 与小接口期望 API，再写表驱动 sanitizer 测试：允许标量、RFC3339 time、固定字符串 duration、slice/map/Marshaler；拒绝 error、任意指针/typed nil、struct、循环、NaN/Inf、非法 json.Number。
- [ ] 覆盖 Marshaler panic/error/递归超限、深度 8、单容器 64、字符串 4 KiB、顶层 key 字典序截断 32、最终 JSON 超 32 KiB 时整份 Args 清空并返回诊断标志。
- [ ] 运行 `go test ./ghttp -run 'TestSanitizePublicParams'` 验证 RED。
- [ ] 最小实现 ErrorDocument、ErrorDetail、NormalizedError 及 Normalizer/Mapper/Adapter/Observer 接口，再实现 sanitizer、稳定 key 顺序语义和防御性深拷贝。
- [ ] 运行 `go test ./ghttp -run 'TestSanitizePublicParams'` 至 GREEN，再运行 `go test ./ghttp`。
- [ ] 提交 `feat(ghttp): add safe public error model`。

### Task 4: 默认错误归一化

**Files:**
- Create: `ghttp/error_normalizer.go`
- Create: `ghttp/error_normalizer_test.go`
- Modify: `ghttp/error.go`

- [ ] 写失败测试：全部 Kind 映射、未知 error 安全 500、context canceled 抑制、deadline、最外层合法 HTTPStatusCarrier、join 停止、保留命名空间拒绝。
- [ ] 运行 `go test ./ghttp -run 'TestDefaultErrorNormalizer'` 验证 RED。
- [ ] 实现默认 Normalizer、`WithStatus` 和最终结果校验；panic 降级为 `internal.server_error`。
- [ ] 写失败测试覆盖自定义 mapper/normalizer 仍经过最终安全校验。
- [ ] 运行相关测试和 `go test ./ghttp`，提交 `feat(ghttp): normalize structured errors`。

### Task 5: validation 错误适配

**Files:**
- Create: `ghttp/error_validation.go`
- Create: `ghttp/error_validation_test.go`
- Modify: `ghttp/validate.go`
- Modify: `ghttp/input.go`

- [ ] 写失败测试：只有绑定/验证阶段错误会被适配；required/email/min/max/oneof/len 和未知 tag；协议字段 location、嵌套及 slice index；不泄露 value/message。
- [ ] 运行 `go test ./ghttp -run 'TestValidationErrorAdapter'` 验证 RED。
- [ ] 实现私有 stage wrapper、默认 playground adapter、稳定 details 排序及自定义 adapter 链。
- [ ] 将解析与验证路径接入 stage wrapper，运行相关测试和 `go test ./ghttp`。
- [ ] 提交 `feat(ghttp): adapt request validation errors`。

### Task 6: JSON 与 RFC 9457 Renderer

**Files:**
- Create: `ghttp/error_renderer.go`
- Create: `ghttp/error_renderer_test.go`
- Modify: `ghttp/config.go`

- [ ] 写失败测试：两种 content type/字段、空 args 为对象、details、percent-encoded type、HEAD 无 body；先断言两种 Renderer 的 OpenAPIDescriptor 内容。
- [ ] 运行 `go test ./ghttp -run 'Test(JSON|Problem)ErrorRenderer'` 验证 RED。
- [ ] 实现 JSONErrorRenderer、ProblemJSONRenderer 和 OpenAPI descriptor。
- [ ] 写失败测试：自定义 renderer error/panic/非法 status/超 64 KiB，ResponseController Flush/Hijack 无法逃逸，fallback 安全。
- [ ] 实现隔离 buffer writer 和最小 JSON fallback；运行 renderer 测试及 `go test ./ghttp`。
- [ ] 提交 `feat(ghttp): render isolated structured errors`。

### Task 7: 统一 Server 错误管线与 Integration 安装点

**Files:**
- Create: `ghttp/error_context.go`
- Create: `ghttp/error_pipeline.go`
- Create: `ghttp/error_pipeline_test.go`
- Create: `ghttp/error_integration_test.go`
- Modify: `ghttp/config.go`
- Modify: `ghttp/server.go`
- Modify: `ghttp/writer.go`
- Modify: `ghttp/route_mux.go`
- Modify: `ghttp/error_handler_test.go`
- Modify: `ghttp/builder_test.go`
- Modify: `ghttp/middleware_test.go`
- Modify: `ghttp/output_test.go`
- Modify: `ghttp/server_routing_external_test.go`

- [ ] 写失败集成测试：typed handler、ToHTTPFunc、路由 404/405、非法路径、body decode/size/media type、panic、RespondError 都产生稳定 ID。
- [ ] 运行 `go test ./ghttp -run 'Test.*Error(Pipeline|Response|Route)'` 验证 RED。
- [ ] 实现配置项、Server/便捷 RespondError、框架错误内部标记和所有入口统一管线；删除 HTTPError、ErrorHandler、WithErrorHandler，并使 Envelope 只处理成功响应。
- [ ] 将旧测试逐文件迁移到 ErrorNormalizer/Renderer/Observer；明确 `ToHTTP`/`ToRaw` 仍完全自管响应，只有 `ToHTTPFunc` 返回的 error 自动进入管线。
- [ ] 写失败测试：committed/hijacked 不二次写、本地化接口仅显式信号运行；observer 按注册顺序且恰好一次、一个 panic 不阻断后续、渲染完成后收到语言/renderer/committed 状态、Cause/Meta 不进入 Renderer、Observation map 为防御性复制。
- [ ] 运行 `go test ./ghttp -run 'Test(ErrorPipelineCommitted|ErrorPipelineHijacked|ErrorObserver|RequestLocalizer)'`，确认新增行为测试以预期原因失败。
- [ ] 写失败测试：`ErrorIntegration`、`Server.UseErrorIntegration`、freeze 保护和 headers contributor。
- [ ] 运行 `go test ./ghttp -run 'Test(ErrorIntegration|UseErrorIntegration)'` 确认 RED；再实现接口、安装和存储能力，使下一任务可直接安装 i18n；重复命令确认 GREEN。
- [ ] 实现 RequestLocalizer context、Localizer/Renderer 各获独立 map、缓存 header 合并和 observer；运行 `go test ./ghttp`。
- [ ] 运行 `rg -n '\b(HTTPError|ErrorHandler|WithErrorHandler)\b' --glob '!docs/**' --glob '!vendor/**' --glob '!.git/**' .`，预期生产 Go 符号为零；运行 `go test ./ghttp ./ghttp/...` 验证所有调用点完成 breaking migration。
- [ ] 提交 `feat(ghttp): unify server error pipeline`。

### Task 8: ghttp/i18n 惰性集成

**Files:**
- Create: `ghttp/i18n/integration.go`
- Create: `ghttp/i18n/resolver.go`
- Create: `ghttp/i18n/session.go`
- Create: `ghttp/i18n/catalog.go`
- Create: `ghttp/i18n/i18n_test.go`

- [ ] 写失败测试：Accept-Language q/wildcard/q=0/非法语法/区域 fallback；query/cookie/custom resolver 优先级和保守 CachePolicy 聚合。
- [ ] 运行 `go test ./ghttp/i18n` 验证 RED。
- [ ] 实现 Resolver、ResolverChain 和 LanguageMatcher。
- [ ] 写失败测试：成功请求不访问 resolver/catalog；首次错误惰性初始化且并发安全；顶层语言锁定；missing/runtime error/panic fallback。
- [ ] 运行 `go test ./ghttp/i18n -run 'Test(Session|Lazy|Fallback|Catalog)'`，确认新增测试以预期原因失败。
- [ ] 实现 Session、StaticCatalog、Catalog 链、Integration middleware 和 `GetMessage`/`MustGetMessage`。
- [ ] 写 Content-Language/Vary/private/no-store 与 details 翻译的失败集成测试。
- [ ] 运行 `go test ./ghttp/i18n -run 'Test(LocalizationHeaders|LocalizeDetails)'` 确认 RED；再实现 header/details 行为并重复命令确认 GREEN。
- [ ] 运行 `go test -race ./ghttp/i18n` 和 `go test ./ghttp/...`，提交 `feat(ghttp): add lazy error localization`。

### Task 9: go-i18n Catalog adapter

**Files:**
- Create: `ghttp/i18n/goi18n/catalog.go`
- Create: `ghttp/i18n/goi18n/catalog_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] 写失败测试：从 embed.FS 加载 JSON/YAML/TOML；模板变量和 plural；缺失消息 sentinel；启动后源数据变化不影响只读快照；显式 Reload 原子替换快照且并发读取安全。
- [ ] 运行 `go test ./ghttp/i18n/goi18n`，确认因 adapter/API 缺失而失败。
- [ ] 先用 `go env GOMODCACHE` 检查离线模块缓存；引入 `github.com/nicksnyder/go-i18n/v2`。JSON 用标准库，YAML/TOML 使用仓库已锁定的 `gopkg.in/yaml.v3` 与 `github.com/pelletier/go-toml/v2`（由 indirect 转 direct），不再引入其他解析库；实现编译期加载、Catalog adapter、显式 Reload，请求期间不做文件 IO 或模板编译。若 go-i18n 不在离线缓存则停止并报告依赖阻塞，不静默联网。
- [ ] 运行 `go mod tidy`、`go test -race ./ghttp/i18n/goi18n` 和 `go test ./ghttp/i18n/...`。
- [ ] 提交 `feat(ghttp): adapt go-i18n catalogs`。

### Task 10: OpenAPI 与迁移文档

**Files:**
- Modify: `ghttp/openapi.go`
- Modify: `ghttp/openapi_compiler.go`
- Modify: `ghttp/openapi_test.go`
- Modify: `ghttp/route_definition.go`
- Modify: `ghttp/README.md`
- Modify: `gerr/README.md`

- [ ] 写失败测试：选定 Renderer component/content type、optional message、Errors 合并去重、Integration headers 和同名 schema 冲突（Renderer descriptor 本身已在 Task 6 测试）。
- [ ] 运行 `go test ./ghttp -run 'TestOpenAPI.*Error'` 验证 RED。
- [ ] 仅实现 OpenAPI compiler 对 Task 6 的 ErrorOpenAPIDescriptor、Task 7 的 Integration header contributor 和 Errors 元数据的消费与合并；不重复定义接口或安装方法。
- [ ] 更新 README：gerr 构造、自动错误 i18n、JSON/RFC 9457、自定义组件，以及 HTTPError/ErrorHandler/WithErrorHandler、Envelope 错误分支、ToHTTP/ToRaw/ToHTTPFunc 的完整迁移表。
- [ ] 运行 OpenAPI 测试及 `go test ./ghttp/...`，提交 `docs(ghttp): document structured error migration`。

### Task 11: 稳健性、性能与最终验证

**Files:**
- Create: `gerr/descriptor_fuzz_test.go`
- Create: `ghttp/error_params_fuzz_test.go`
- Create: `ghttp/error_normalizer_fuzz_test.go`
- Create: `ghttp/error_renderer_fuzz_test.go`
- Create: `ghttp/i18n/resolver_fuzz_test.go`
- Create: `ghttp/error_benchmark_test.go`
- Create: `ghttp/i18n/i18n_benchmark_test.go`

- [ ] 添加 fuzz seeds，并逐条运行：`go test ./gerr -run '^$' -fuzz FuzzMessageID -fuzztime 10s`；`go test ./ghttp -run '^$' -fuzz FuzzPublicParams -fuzztime 10s`；`go test ./ghttp -run '^$' -fuzz FuzzNormalizeErrorChain -fuzztime 10s`；`go test ./ghttp -run '^$' -fuzz FuzzErrorRenderer -fuzztime 10s`；`go test ./ghttp/i18n -run '^$' -fuzz FuzzAcceptLanguage -fuzztime 10s`。
- [ ] 添加 `BenchmarkSuccessWithoutI18n`、`BenchmarkSuccessWithLazyI18n`、`BenchmarkNormalizeGerr`、`BenchmarkNormalizeWrappedGerr`、`BenchmarkValidationErrors`、`BenchmarkLocalizeError`、`BenchmarkJSONErrorRenderer`、`BenchmarkProblemErrorRenderer`。
- [ ] 性能比较分两组：对变更前已存在且可比的 ServeHTTP 成功热路径，在 baseline/current worktree 都运行现有 `BenchmarkServerRouting*` harness 并用 `benchstat` 比较；新 8 个错误/i18n benchmark 只在当前分支建立绝对基线。若要跨提交比较新 API，先把同一份仅依赖公开兼容 API 的 harness 复制到两个临时 worktree，再分别运行，禁止直接在缺少新 API 的旧 commit 编译新 harness。未启用成功路径 allocs/op 增量必须为 0，惰性中间件最多增加 1；ns/op 仅在统计显著且回归超过 5% 时失败，两份原始输出保存为 CI 工件。
- [ ] 对全部 touched Go 文件执行 `gofmt -w`：使用 `git diff --name-only --diff-filter=ACM HEAD -- '*.go'` 得到精确清单并逐文件传给 gofmt；再运行 `go mod tidy` 和 `git diff --check`，确保生产、测试、fuzz、benchmark 文件均覆盖。
- [ ] 运行 `go test -race ./gerr/... ./ghttp/...`，race tests 必须显式并发覆盖 Session、Catalog read/reload、ResolverChain、Observer 顺序/隔离。
- [ ] 运行 `make check` 与 `go test ./...`；任何失败先定位并修复再重跑。
- [ ] 使用 `superpowers:requesting-code-review` 做实现审查，修复后重复最终验证。
- [ ] 提交 `test(ghttp): verify error i18n robustness`。
