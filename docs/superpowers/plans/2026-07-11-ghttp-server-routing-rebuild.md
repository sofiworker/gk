# ghttp Server Routing Rebuild Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 将 ghttp 服务端重构为注册定义、一次冻结编译、运行期只读的单一内部路由系统，并将 OpenAPI 降级为完全旁路的 best-effort 能力。

**Architecture:** Server 在注册期持有受锁保护的 routeDefinition；首次服务入口把全部定义、内置 OpenAPI 端点和中间件 scope 编译为不可变 compiledState。新 routeMux 是 ghttp 包内未导出的 method-first matcher，只选择 compiledRoute；类型化终结器通过 route 自身的 extractor 在 middleware 后读取路径参数，raw handler 始终拿到原始 request。旧的 Router 实现迁入 internal/legacyrouter，仅用于正确性和性能基线。

**Tech Stack:** Go 标准库、现有 ghttp codec/validator/render/SSE/WebSocket/VFS 组件、testing、httptest、fuzz 和 benchmark；不新增依赖。

---

## 全局约束

- 用户明确要求：本计划全部实施完成前禁止任何 git commit、push、PR、merge 或删除 worktree。所有改动只保留在 .worktrees/ghttp-server-routing 供其 review。
- 只用 apply_patch 编辑文件；每次修改 Go 文件后运行 gofmt。
- 先写测试并确认失败，再添加最小实现；任何独立运行时行为必须有对应单元测试。
- 不触碰主工作区，也不触碰主工作区未跟踪的 a.txt。
- 不新增第三方依赖，不使用网络。
- 生产 Server 不得 import internal/legacyrouter，也不得再暴露 Router 或任意具体路由树。
- OpenAPI 的不确定性只能导致文档字段省略，绝不能改变注册、冻结、matcher、handler 或响应语义。
- 实施中使用任务复选框跟踪；本计划中来自通用 skill 的“提交”步骤由用户的禁止提交指令明确替代为“检查 git diff，确认未提交”。

## 文件结构与职责

- Create: ghttp/route_path.go
  - 注册路径和请求 escaped path 的解析、逐段解码、合法性检查、Group 拼接和 strict routing 规范化。
- Create: ghttp/route_definition.go
  - 终结器种类、routeDefinition、已编译 route、extractor 和 immutable compiledState 数据模型。
- Create: ghttp/route_registry.go
  - 注册事务、跨 method 结构冲突、definition snapshot 和冻结前写入边界。
- Create: ghttp/route_mux.go
  - 未导出的 method-first matcher、HEAD 回退、405 Allow 和零段 catch-all。
- Create: ghttp/error_handler.go
  - ErrorHandler、默认错误写出、提交/hijack 感知 response writer 和最外层 recovery。
- Create: ghttp/openapi_compiler.go
  - 从 definition snapshot 生成最小 OpenAPI 3.1 JSON 的内部 compiler。
- Create: ghttp/internal/legacyrouter/
  - 迁出的 Radix、Compiled、Matchit、Std 基线以及只供 benchmark/对照的适配层。
- Create: ghttp/server_routing_test.go
  - Server 注册、冻结、错误 outcome、middleware、HEAD/OPTIONS 的黑盒和并发测试。
- Create: ghttp/route_path_test.go
  - 路径、转义、Group 拼接、参数规则和 strict routing 表驱动测试。
- Create: ghttp/route_registry_test.go
  - 冲突矩阵、ANY 原子性、RouteBuilder 终结状态测试。
- Create: ghttp/route_mux_test.go
  - 纯 matcher、Allow、extractor 和 raw request 边界测试。
- Create: ghttp/error_handler_test.go
  - 错误消息、response 已提交、hijack、panic 和 ErrorHandler panic 测试。
- Create: ghttp/openapi_compiler_test.go
  - OpenAPI snapshot、最小 JSON、端点路径和非干扰测试。
- Create: ghttp/server_routing_external_test.go
  - package ghttp_test 的真实调用方 API 回归测试。
- Create: ghttp/server_routing_fuzz_test.go
  - 路径 parser 和 matcher fuzz target。
- Modify: ghttp/server.go, ghttp/group.go, ghttp/builder.go, ghttp/config.go, ghttp/error.go, ghttp/output.go, ghttp/path_param.go, ghttp/params.go, ghttp/benchmark_test.go, ghttp/README.md, ghttp/doc.go。
- Delete after equivalent tests migrate: ghttp/router.go, ghttp/radix.go, ghttp/radix_router.go, ghttp/compiled_router.go, ghttp/matchit_router.go, ghttp/std_router.go, ghttp/openapi.go，以及仅测试已删除公开 Router/OpenAPI builder/StatusCoder 的测试。
- Create or modify: docs/ghttp-server-routing-migration.md、docs/benchmark-results/ghttp-server-routing-rebuild.md 和根 README（若根 README 列出了 ghttp 服务端注册 API）。

## 终结器矩阵

冻结期为每个 routeDefinition 记录 extractor 是否必要，不能通过 handler 的动态 type assertion 推断：

| 终结器 | matcher 后 extractor | 原始 request | 错误返回进入 ErrorHandler |
|---|---|---|---|
| To | 是 | 否 | 是 |
| ToHTTP | 否 | 是 | 不适用，handler 自写 |
| ToRaw | 否 | 是 | 不适用，handler 自写 |
| ToHTTPFunc | 是 | 是 | 是 |
| ToRedirect | 是 | 否 | 是 |
| ToRedirectFunc | 是 | 否 | 是 |
| ToHTML | 否 | 是 | 是 |
| ToSSE | 是 | 是 | 是 |
| ToWebSocket | 是 | 是 | 是 |
| ToStatic、ToStaticFS、ToStaticFile | 否 | 是 | 仅专用实现返回前可写错误 |

## Task 1: 建立公开错误、配置与冻结状态机骨架

**Files:**

- Modify: ghttp/error.go
- Modify: ghttp/config.go
- Modify: ghttp/server.go
- Create: ghttp/server_routing_test.go

- [ ] **Step 1: 写失败的独立冻结原语和配置 API 测试**

覆盖公开 sentinel、公开 `ErrorHandler` 函数类型、WithStrictRouting、WithOpenAPIPath、WithErrorHandler，以及不依赖路由编译的冻结状态原语：单一编译者、并发等待、成功发布、失败终止、失败后所有等待者唤醒并得到同一错误。首次 ServeHTTP、Run/Serve/TLS、OpenAPI 不冻结、Group/Route 的完整黑盒生命周期测试放到 Task 5 和 Task 9，因为它们依赖 compiledState、Group 和 definition registry。

- [ ] **Step 2: 运行失败测试**

运行：

~~~text
go test ./ghttp -run 'TestServer(Freeze|OpenAPI|Concurrent)' -count=1
~~~

预期：因公开 sentinel、配置字段和冻结状态原语尚未实现而失败。

- [ ] **Step 3: 添加最小状态机和 sentinel**

在 error.go 增加 ErrServerFrozen、ErrRouteConflict、ErrRouteBuilderFinalized、ErrInvalidRequestPath、ErrOpenAPIDisabled、ErrHandlerPanic 及路径/方法配置类别 sentinel，并声明规格约定的公开 `ErrorHandler func(http.ResponseWriter, *http.Request, *HTTPError)` 类型。在 Config 添加 strictRouting、openAPIPath、errorHandler；新增 WithStrictRouting、WithOpenAPIPath、WithErrorHandler。创建可独立测试的冻结状态原语：唯一编译者发布成功值或失败错误，其他调用者等待并得到同一结果。Task 5 将它接入 Server 和 compiledState，Task 6 才提供默认实现及错误管线。

- [ ] **Step 4: 运行绿色测试并格式化**

运行：

~~~text
gofmt -w ghttp/error.go ghttp/config.go ghttp/server.go ghttp/server_routing_test.go
go test ./ghttp -run 'TestServer(Freeze|OpenAPI|Concurrent)' -count=1
~~~

预期：通过；冻结失败不产生死锁，所有等待者均被唤醒。

- [ ] **Step 5: 检查工作区，不提交**

运行 git status --short，确认只有 worktree 内未提交修改。

## Task 2: 实现路径规范化、参数语法和 Group 拼接

**Files:**

- Create: ghttp/route_path.go
- Create: ghttp/route_path_test.go
- Modify: ghttp/group.go
- Modify: ghttp/server.go

- [ ] **Step 1: 写失败的表驱动 parser 测试**

覆盖缺少前导斜杠、根路径、Group("/api/")+ "v1"、Group("/api")+ ""、Group("/api")+ "/"、默认尾斜杠域和 strict 域。覆盖 Unicode、/caf%C3%A9 和 /café 等价、编码斜杠不改变层级、编码 {id} 是静态文本。覆盖请求和注册中的 //、.、..、编码 dot segment、错误百分号转义。覆盖 {id}、{path...}、Go 标识符（含 UserID、URL、_id）、大小写、重复名称、非法嵌入参数和非末尾 catch-all。

- [ ] **Step 2: 确认 parser 测试失败**

运行：

~~~text
go test ./ghttp -run 'Test(RoutePath|GroupPath|RouteParameter)' -count=1
~~~

预期：因 parser 不存在或旧 :/* 语法仍被接受而失败。

- [ ] **Step 3: 实现不可变 pathPattern**

定义 parsedSegment（static、parameter、catchAll）和 pathPattern。注册期按 escaped segment 切分后逐段解码，参数判定只在解码前的完整文本上进行；请求期按同一算法解析并返回 ErrInvalidRequestPath。实现 canonical key，使默认模式去除非根尾斜杠、严格模式保留；实现 Group prefix 与 route 尾斜杠的独立拼接。不要使用 path.Clean，也不要重定向。

- [ ] **Step 4: 让 Group 的可变 API 受 freeze gate 保护**

Group 保存父 Group 与原始 prefix 作用域；Use、Consumes、Produces、Group 和向其注册路由均调用 owner 的冻结检查。不要在 Group 创建时把父 middleware 展平，保持可用于固定 scope 编译的链路。

- [ ] **Step 5: 格式化并运行路径测试**

运行：

~~~text
gofmt -w ghttp/route_path.go ghttp/route_path_test.go ghttp/group.go ghttp/server.go
go test ./ghttp -run 'Test(RoutePath|GroupPath|RouteParameter)' -count=1
~~~

预期：通过，所有非法注册路径只在注册阶段失败，所有非法请求路径可供 Server 映射 400。

## Task 3: 定义 routeDefinition、注册事务和 RouteBuilder 生命周期

**Files:**

- Create: ghttp/route_definition.go
- Create: ghttp/route_registry.go
- Create: ghttp/route_registry_test.go
- Modify: ghttp/builder.go
- Modify: ghttp/group.go
- Modify: ghttp/server.go

- [ ] **Step 1: 写失败的注册事务测试**

测试同 method 静态冲突、同结构不同参数名冲突、跨 method 同结构同名允许、跨 method 不同名拒绝、静态/参数/catch-all 共存、默认尾斜杠冲突和 strict 并存。启用 OpenAPI HTTP endpoint 时，测试用户对保留端点路径的 `To*` 立即 panic ErrRouteConflict。测试 ANY 包含一个冲突 method 时零条 method 被提交。测试 CUSTOM 原样接受 PURGE、区分大小写、拒绝空白/无效 token。测试 To 后再 Use/METHOD/To 立即 panic ErrRouteBuilderFinalized，缺 method、非法路径、nil 终结器和 produces 配置错误在 To 调用处 panic 且 errors.Is 可判断。

- [ ] **Step 2: 运行失败测试**

运行：

~~~text
go test ./ghttp -run 'Test(RouteRegistry|RouteBuilder)(Conflict|Atomic|Finalized|Custom)' -count=1
~~~

预期：因旧 builder 延迟收集 setupErr、逐 method 注册和 CUSTOM 转换大小写而失败。

- [ ] **Step 3: 实现 definition 与事务性 registry**

routeDefinition 保存规范化 method/pathPattern、Group scope、Route middleware、终结器类型、Req/Resp reflect.Type、codec/validator 预编译输入和文档快照。创建 Server 时，若启用非空 OpenAPI HTTP path，则由 registry 预留该内部端点的规范化路径；任何用户 `To*` 对该路径的注册在此阶段立即以 ErrRouteConflict 失败。registry 在同一锁内先验证全部 definitions 与跨 method structural key，再一次 append；失败绝不改变 definitions。definition snapshot 必须深拷贝所有 slice/map 和 RouteDoc。注册错误用包含 method/path 的上下文包装 sentinel。

- [ ] **Step 4: 重写 RouteBuilder 的终结和 method 选择**

METHOD 选择只能调用一次。标准方法写固定值，ANY 写固定标准列表，CUSTOM 不 trim/不转换。每个 To* 统一通过 finalize 创建完整 definition 并调用 registry；终结器无返回值，运行时配置错误立即 panic。删除 recordSetupError 延迟模型和 build 时注册。保留现有 Doc/Consumes/Produces/Validate/SkipValidation/MaxBodyBytes 作为运行时配置，任何文档缺失都不可导致 panic。

- [ ] **Step 5: 格式化并运行注册测试**

运行：

~~~text
gofmt -w ghttp/route_definition.go ghttp/route_registry.go ghttp/route_registry_test.go ghttp/builder.go ghttp/group.go ghttp/server.go
go test ./ghttp -run 'Test(RouteRegistry|RouteBuilder)(Conflict|Atomic|Finalized|Custom)' -count=1
~~~

预期：通过；ANY 无部分提交，To 调用就是唯一配置错误边界。

## Task 4: 实现未导出 method-first routeMux 与 compiled extractor

**Files:**

- Create: ghttp/route_mux.go
- Create: ghttp/route_mux_test.go
- Modify: ghttp/route_definition.go

- [ ] **Step 1: 写失败 matcher 和 extractor 测试**

分别测试当前 method 下 static 优先于 param、param 优先于 catch-all，以及另一 method 的静态路由不影响当前 method 参数选择。测试 catch-all 对 /files 和 /files/ 返回存在的 path 参数且值为空、对多段值逐段解码。测试 HEAD 显式优先、HEAD 完整未命中才回退 GET，且 matchResult 的 suppressBody 为真；实际 ResponseWriter body 抑制放到 Task 6。OPTIONS 仅显式命中，否则 path 存在为 405。测试 404/405、Allow 按原始 method 稳定字典序、GET 自动加入 HEAD、OPTIONS 仅显式、CUSTOM 原样。测试 extractor 在 middleware 改写 method/path 后不重匹配，提取失败返回 ErrInvalidRequestPath。

- [ ] **Step 2: 确认 matcher 测试失败**

运行：

~~~text
go test ./ghttp -run 'TestRouteMux|TestCompiledExtractor' -count=1
~~~

预期：routeMux 与 extractor 尚不存在。

- [ ] **Step 3: 实现只读 routeMux**

冻结时从 definitions 构建按 method 分组的静态 map、参数节点和 catch-all 节点。match 返回 found/notFound/methodNotAllowed、compiledRoute、Allow 和 suppressBody，不写 HTTP 响应也不存参数。405 为每个 method 独立结构匹配并使用紧凑列表去重排序；当前阶段不创建 path-first 辅助索引。为每个 typed terminal 生成线性 compiled extractor，使用同一 escaped parser 构造 pathParamList；raw terminal 不生成 extractor。

- [ ] **Step 4: 运行绿色测试并进行 allocation 检查**

运行：

~~~text
gofmt -w ghttp/route_mux.go ghttp/route_mux_test.go ghttp/route_definition.go
go test ./ghttp -run 'TestRouteMux|TestCompiledExtractor' -count=1
go test ./ghttp -run '^$' -bench 'BenchmarkRouteMux' -benchmem -count=1
~~~

预期：测试通过；成功匹配无 matcher 临时 map 分配。

## Task 5: 编译成功和错误 outcome chain，迁移 Server/Group middleware

**Files:**

- Modify: ghttp/server.go
- Modify: ghttp/group.go
- Modify: ghttp/middleware.go
- Modify: ghttp/route_definition.go
- Modify: ghttp/server_routing_test.go

- [ ] **Step 1: 写失败 middleware scope 测试**

用事件序列验证内建 Recovery → Server → 外层 Group → 内层 Group → Route → handler 的 enter/exit 嵌套顺序与注册先后无关。验证 Server middleware 覆盖成功、400、404、405 和 handler 普通 error；Group/Route middleware 只覆盖命中路由。覆盖首次 ServeHTTP、Run、Serve、TLS 服务入口均冻结，以及冻结后 Server、Group、Route 上的路由、子 Group、middleware、Consumes、Produces 和类型元数据写入均 panic ErrServerFrozen。OpenAPI endpoint 走 Server middleware 的用例放到 Task 9。验证 middleware factory 每个 compiled chain 仅在冻结时调用一次，跨 route 共享状态需由闭包外部持有。验证 panic 后用户 middleware 不重跑。

- [ ] **Step 2: 运行失败测试**

运行：

~~~text
go test ./ghttp -run 'TestServer(Middleware|Outcome|Recovery)' -count=1
~~~

预期：旧实现每请求构建 group chain，且错误 outcome 未走 Server middleware。

- [ ] **Step 3: 在 freeze compiler 中构建 chain**

将 Server、完整 Group ancestry、Route middleware 和终结器 handler 按固定 scope 编译成每条 compiledRoute 的 handler。为 400、404、405 和 OpenAPI 内置 route 分别预编译只包含 Server middleware 的 chain。最外层 recovery 不作为用户 Middleware 插入；它包住 compiledState dispatch 并直接进入 error handling。ServeHTTP 仅读取已发布 compiledState、解析 request path、match 并调用相应 chain。

- [ ] **Step 4: 格式化并运行绿色测试**

运行：

~~~text
gofmt -w ghttp/server.go ghttp/group.go ghttp/middleware.go ghttp/route_definition.go ghttp/server_routing_test.go
go test ./ghttp -run 'TestServer(Middleware|Outcome|Recovery)' -count=1
~~~

预期：通过；请求热路径不再组装 middleware。

## Task 6: 实现统一 ErrorHandler、响应提交保护和 200 响应所有权

**Files:**

- Create: ghttp/error_handler.go
- Create: ghttp/error_handler_test.go
- Modify: ghttp/error.go
- Modify: ghttp/output.go
- Modify: ghttp/builder.go
- Modify: ghttp/writer.go

- [ ] **Step 1: 写失败错误和状态所有权测试**

测试默认 400/404/405，405 的 Allow 在 ErrorHandler 前设置。测试显式 HTTPError 的 4xx/5xx Message 对客户端公开；普通 error 与 panic 只输出通用 500，HTTPError.Err 保留原因且包含 ErrHandlerPanic。测试 ErrorHandler 每请求至多一次、其 panic 使用最小 500 但不递归。测试 handler 写 header/body 后返回 error 不二次写，hijack 后只记录。测试 HEAD 显式命中与 GET 回退分别对 typed、自写和 ErrorHandler 产生的响应抑制全部 body，同时保留 status/header。测试 To 不再识别 StatusCoder 或 Status int，任何成功 typed response 固定 200；空 struct 仍编码 200；DefaultEnvelope 对 error 仍保留实际 HTTP code。

- [ ] **Step 2: 运行失败测试**

运行：

~~~text
go test ./ghttp -run 'Test(ErrorHandler|TypedResponse|DefaultEnvelope)' -count=1
~~~

预期：旧 writeError 泄露普通 error，resolveStatusCode 生效，且没有统一 handler。

- [ ] **Step 3: 添加写入状态跟踪与默认 ErrorHandler**

使用包装 ResponseWriter 跟踪 header 已提交、Write、Flush 和 Hijack，不破坏现有可选接口；当 matchResult 指明 HEAD body 抑制时，将该包装置于整个已编译成功/错误 chain 外层，使 typed、raw、自写和 ErrorHandler 的 Write 均被抑制而 Header/WriteHeader 保持有效。将类型化绑定、校验和自写终结器的返回错误统一交给 Server 的 error pipeline。默认 ErrorHandler 只使用公开 HTTPError.Message；普通内部 error 映射安全文案。删除 StatusCoder、statusFieldCache、resolveStatusCode 和相关公开 API；To 成功总是传入 200。

- [ ] **Step 4: 逐项接入所有终结器**

按终结器矩阵确认 To、ToHTTPFunc、ToRedirect、ToRedirectFunc、ToSSE、ToWebSocket 使用 extractor；ToHTTP/ToRaw、HTML、Static 使用原始 request。ToHTTP/ToRaw 不调用 SetPathValue、不写 context 参数，外层已存在的 PathValue 保留。专用 handler 返回 error 时用提交/hijack 状态决定是否进入 ErrorHandler。

- [ ] **Step 5: 格式化并运行绿色测试**

运行：

~~~text
gofmt -w ghttp/error_handler.go ghttp/error_handler_test.go ghttp/error.go ghttp/output.go ghttp/builder.go ghttp/writer.go
go test ./ghttp -run 'Test(ErrorHandler|TypedResponse|DefaultEnvelope)' -count=1
~~~

预期：通过；不再存在响应成功状态魔法。

## Task 7: 保留原始 request 边界并完成类型化参数绑定

**Files:**

- Modify: ghttp/path_param.go
- Modify: ghttp/params.go
- Modify: ghttp/input.go
- Modify: ghttp/builder.go
- Modify: ghttp/route_mux_test.go
- Modify: ghttp/server_routing_test.go

- [ ] **Step 1: 写失败 raw 与 typed 参数边界测试**

外层 http.ServeMux 设置 PathValue 后，ToHTTP 读取到原值而非 ghttp 参数；ToHTTP 自身不能读取 ghttp parameter。类型化 To、ToHTTPFunc、RedirectFunc、SSE 和 WebSocket 使用 Params 或 path tag 获得 extractor 值。catch-all 零段在 Params 中存在并为 ""。middleware 对 request.URL.Path 或 Method 的改写绝不触发二次 match，typed extractor 失败返回 400。测试 16 个以内参数不分配 map，超出时保持正确。

- [ ] **Step 2: 确认失败并实施最小接入**

运行：

~~~text
go test ./ghttp -run 'Test(RawRequest|TypedPathParams|PathValue|MiddlewareRewrite)' -count=1
~~~

预期：旧 pathParamHandler/context 注入模型使 raw handler 看见非原始参数。

然后删除 requestWithPathParams 和 pathParamHandler 运行时依赖，Params 仅从 extractor 提供的内部 pathParamList 构造；保留 pathParamList 的内联与 overflow 行为。重跑同一命令，预期通过。

## Task 8: 将旧路由实现迁入 internal/legacyrouter 并清除公开路由 API

**Files:**

- Create: ghttp/internal/legacyrouter/radix.go
- Create: ghttp/internal/legacyrouter/compiled.go
- Create: ghttp/internal/legacyrouter/matchit.go
- Create: ghttp/internal/legacyrouter/std.go
- Create: ghttp/internal/legacyrouter/legacy_test.go
- Modify: ghttp/benchmark_test.go
- Modify or delete: ghttp/router_test.go, ghttp/compiled_router_test.go, ghttp/matchit_router_test.go
- Delete: ghttp/router.go, ghttp/radix.go, ghttp/radix_router.go, ghttp/compiled_router.go, ghttp/matchit_router.go, ghttp/std_router.go
- Modify: ghttp/config.go, ghttp/server.go

- [ ] **Step 1: 写 legacy adapter 的失败基准/对照测试**

建立相同路由生成器和请求样本，比较 internal/legacyrouter 的 Radix 与新 routeMux 的 static、param、deep-param、catch-all、HEAD explicit、HEAD GET fallback、method fallback、404、405。legacy 仅需选择 handler 并作为基线；不向生产 Server 回灌参数或依赖。

- [ ] **Step 2: 运行失败测试**

运行：

~~~text
go test ./ghttp -run 'TestLegacyRouterParity' -count=1
~~~

预期：internal/legacyrouter 尚不存在。

- [ ] **Step 3: 迁入冻结的历史算法**

把当前 Radix、Compiled、Matchit、Std 的实现及所需最小 path/参数工具复制到 internal/legacyrouter，移除与 ghttp 的公开 API 耦合，保持算法冻结。创建适配器使它们可被 ghttp 内 benchmark 使用。迁入测试后删除旧公开 Router interface、WithRouter、Server.Router 和所有具体路由器导出符号；更新受影响测试为新 Server 黑盒测试，不能保留 deprecated wrapper。

- [ ] **Step 4: 格式化并运行迁移验证**

运行：

~~~text
gofmt -w $(rg --files ghttp/internal/legacyrouter -g '*.go') ghttp/benchmark_test.go
go test ./ghttp/... -run 'TestLegacyRouterParity|TestRouteMux' -count=1
~~~

预期：通过；rg 搜索 ghttp 公开包不再出现 Router、WithRouter、NewRadixRouter、NewCompiledRouter、NewMatchitRouter、NewStdRouter。

## Task 9: 将 OpenAPI 改造成 definition snapshot 的旁路 compiler

**Files:**

- Create: ghttp/openapi_compiler.go
- Create: ghttp/openapi_compiler_test.go
- Modify: ghttp/server.go
- Modify: ghttp/config.go
- Modify: ghttp/builder.go
- Delete: ghttp/openapi.go
- Modify or delete: ghttp/openapi_test.go

- [ ] **Step 1: 写失败 OpenAPI 非干扰测试**

测试未启用 OpenAPI 返回 ErrOpenAPIDisabled；冻结前 OpenAPI 返回当时 snapshot 且后续仍可注册；冻结后可缓存且每次返回独立 bytes。测试 raw handler 没有声明仍注册服务；缺失/无效 Doc 不阻断。验证输出可 json.Unmarshal 且 openapi 为 3.1.0。验证默认 /openapi.json、WithOpenAPIPath 自定义、空路径只关闭 HTTP endpoint、与用户真实路由冲突为 ErrRouteConflict、端点走 Server middleware。

- [ ] **Step 2: 确认失败**

运行：

~~~text
go test ./ghttp -run 'TestOpenAPI(Snapshot|Disabled|Endpoint|BestEffort)' -count=1
~~~

预期：旧公开 builder、Build 和冻结时硬编码端点不符合行为。

- [ ] **Step 3: 实现内部 compiler 与 Server.OpenAPI**

compiler 只接收 definition snapshot，锁外构造 JSON。普通 To 在可确定 Produces 时生成 200；raw terminal 生成最小 operation/default response；catch-all 变为普通 OpenAPI 参数并加 x-ghttp-catch-all。无法确定 schema/content/status 时省略。CONNECT/CUSTOM 写入 x-ghttp-methods；不要创建非法 method key。OpenAPI HTTP endpoint 使用 Task 3 已预留的内部端点定义在 freeze 时编译；compiler 排除该 internal definition，且不能把端点插入推迟到首次服务入口。

- [ ] **Step 4: 删除公开 builder 并运行绿色测试**

运行：

~~~text
gofmt -w ghttp/openapi_compiler.go ghttp/openapi_compiler_test.go ghttp/server.go ghttp/config.go ghttp/builder.go
go test ./ghttp -run 'TestOpenAPI(Snapshot|Disabled|Endpoint|BestEffort)' -count=1
~~~

预期：通过；公开包不再导出 OpenAPI、NewOpenAPI、AddRoute 或 Build。

## Task 10: 外部真实使用、fuzz、并发与兼容性清理

**Files:**

- Create: ghttp/server_routing_external_test.go
- Create: ghttp/server_routing_fuzz_test.go
- Modify: ghttp/integration_test.go, ghttp/scenario_test.go, ghttp/bad_case_test.go
- Delete or rewrite: 仅验证旧 :id、*path、Router 和 StatusCoder 的测试

- [ ] **Step 1: 写 ghttp_test 外部调用方测试**

用 httptest.Server 从 package ghttp_test 创建嵌套 Group，注册 typed、ToHTTP、ToHTTPFunc、Envelope、ErrorHandler、OpenAPI 和 middleware。验证实际请求的 200/400/404/405、HEAD、OPTIONS、path parameter、raw request、提交后错误和 panic 边界，确保新 API 在调用方不可见内部类型。

- [ ] **Step 2: 写并确认 fuzz/race 相关测试失败**

Fuzz parser/matcher 输入异常百分号、dot、双斜杠、Unicode、编码斜杠、超长 segment、大量参数和 catch-all；测试要求无 panic、没有不一致规范化。添加并发注册与首次 ServeHTTP 的协调测试，确认合法竞态结果只能是完整注册后冻结或 ErrServerFrozen，绝无半注册。

- [ ] **Step 3: 实现测试暴露的问题并回归**

运行：

~~~text
gofmt -w ghttp/*_test.go
go test ./ghttp -run 'TestExternalServerRouting|TestServerRegistrationFreeze' -count=1
go test ./ghttp -run '^$' -fuzz '^FuzzRoutePath$' -fuzztime=5s
go test ./ghttp -run '^$' -fuzz '^FuzzRouteMux$' -fuzztime=5s
go test -race ./ghttp/... -run 'TestServer(Freeze|Registration)'
~~~

预期：通过；fuzz 未发现 panic 或失配，race 无报告。

## Task 11: 重建 benchmark harness 并记录同分支基线结果

**Files:**

- Modify: ghttp/benchmark_test.go
- Create: docs/benchmark-results/ghttp-server-routing-rebuild.md

- [ ] **Step 1: 写 benchmark harness 的编译性测试**

定义固定 16、128、1024、8192 路由生成器、相同请求样本和无分配 response writer。每个规模覆盖 static、param、deep-param、catch-all、HEAD explicit、HEAD GET fallback、method fallback、typed extraction、404、405；同一 BenchmarkRouteComparison 名称通过 GHTTP_ROUTER_IMPL 环境变量选择 new routeMux/compiledState 或 internal/legacyrouter Radix，以便 old/new 输出能被 benchstat 按完整 benchmark 名称配对。增加 freeze benchmark，变量为 route 数和 middleware 数，记录编译时间、allocs 和 chain 数。

- [ ] **Step 2: 检查 benchstat 并运行可复现的多轮基准**

运行：

~~~text
command -v benchstat
GHTTP_ROUTER_IMPL=legacy go test ./ghttp -run '^$' -bench '^BenchmarkRouteComparison$' -benchmem -count=5 > /tmp/ghttp-route-legacy.txt
GHTTP_ROUTER_IMPL=new go test ./ghttp -run '^$' -bench '^BenchmarkRouteComparison$' -benchmem -count=5 > /tmp/ghttp-route-new.txt
benchstat /tmp/ghttp-route-legacy.txt /tmp/ghttp-route-new.txt
go test ./ghttp -run '^$' -bench 'BenchmarkRouteFreeze' -benchmem -count=5
~~~

如果 command -v 未找到 benchstat，不新增网络依赖或下载工具；保留两份原始输出，记录该外部工具缺失，并向用户报告性能阈值无法按规格完成判定，不能宣称满足门槛。若存在 benchstat，预期所有案例可运行，成功路径和 extractor 的分配可与 legacy 基线直接比较。

- [ ] **Step 3: 写 benchmark 报告**

报告记录命令、Go 版本、机器信息、每个规模及场景的结果、allocation 差异、freeze 时间/内存/chain 数，以及 404/405 暂不引入 path-first 索引的说明。若成功路径任一显著退化超过 10%，停止本计划并向用户报告数据；5% 到 10% 写明原因和取舍。不把未实测的数字写入报告。

- [ ] **Step 4: 不提交**

检查报告仅包含本次真实输出，执行 git diff --check 和 git status --short。

## Task 12: 完成迁移文档、README 和最终验证

**Files:**

- Create: docs/ghttp-server-routing-migration.md
- Modify: ghttp/README.md
- Modify: ghttp/doc.go
- Modify: README.md（仅当其中列出 ghttp 的旧路由 API）
- Modify: docs/benchmark-results/ghttp-server-routing-rebuild.md

- [ ] **Step 1: 写文档回归检查**

用 rg 断言 README 与迁移文档明确包含旧新 API 映射：Router/WithRouter、具体 Router、Handle、:id、*path、StatusCoder、Status int、运行期注册、中间件顺序、独立 OpenAPI builder、OpenAPI path。还必须醒目标注与常见 Go 框架不同的固定 middleware scope 顺序、CUSTOM 原样、OPTIONS 无自动 204、catch-all 零段、ToHTTP/ToRaw 没有 ghttp PathValue、默认/strict 尾斜杠、Envelope 不把错误改 200、Doc 不改变运行时。

- [ ] **Step 2: 编写迁移与 breaking release 文档**

给每项破坏性改动提供最小 before/after 示例。说明 internal/legacyrouter 临时保留、非公开、不代表兼容承诺，且只有项目负责人确认新 matcher 唯一后才可删除。不要实现或注册 /docs UI。

- [ ] **Step 3: 运行全部验证**

运行：

~~~text
go fmt ./...
go mod tidy
make check
go test ./...
go test -race ./ghttp/...
go test ./ghttp -run '^$' -bench 'Benchmark(ServerRouting|RouteFreeze)' -benchmem -count=3
git diff --check
git status --short
~~~

预期：所有命令退出 0；git status 只显示本 worktree 中未提交实现、测试、文档和基准报告。绝不执行 git commit。

- [ ] **Step 4: 对照完成标准逐项复核**

逐项核对规格第 20 节：路径/method、404/405、middleware 覆盖、panic 隐私、OpenAPI 旁路、未启用成本、冻结无锁、benchmark、README/迁移文档以及 legacyrouter 保留。若任一项没有可运行证据，不宣称完成。
