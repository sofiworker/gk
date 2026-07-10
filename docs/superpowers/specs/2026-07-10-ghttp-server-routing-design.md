# ghttp Server、路由与 OpenAPI 重构设计

> 日期：2026-07-10
>
> 状态：设计已逐节确认，等待书面规格复核
>
> 范围：ghttp 服务端路由、RouteBuilder、生命周期、中间件、错误响应和 OpenAPI

## 1. 目标

本次重构将 ghttp 的服务端 API 收敛到一条明确、可验证的线路：

- Server 内部只保留一个路由实现，不再公开 Router 插件边界。
- 注册阶段保存统一的 routeDefinition，冻结时同时生成 matcher、handler chain 和 OpenAPI。
- 运行期路由表完全只读，不支持动态注册、重建或解冻。
- 路径语法、HEAD、OPTIONS、404、405 和中间件顺序具有稳定且可测试的语义。
- 类型化路由与自写响应路由明确区分响应所有权。
- OpenAPI 描述真实 wire response，不从占位类型或自定义 Envelope 猜测 schema。
- 所有破坏性变化通过迁移文档明确说明，不保留长期兼容层。

本设计取代此前文档中以下方向：

- 公开 Router 接口或 Router() 逃生口。
- 多个可替换的公开路由实现。
- 通过 StatusCoder 或响应结构体 Status 字段控制成功状态。
- 由独立 OpenAPI builder 维护第二份路由文档。
- 依赖注册先后决定中间件嵌套顺序。

## 2. 非目标

本次不包含：

- 运行期动态注册、热更新或路由解冻。
- Swagger UI、Scalar、Stoplight 或其他第三方文档 UI。
- 路径优先共享树重写。
- 为 404/405 预先增加辅助路径索引。
- 自定义 Router 插件兼容层。
- 对旧的 :id 或 *path 语法提供 warning 或过渡期支持。
- 为自定义 Envelope 设计通用 OpenAPI schema DSL。

轻量交互式文档页面属于后续低优先级任务。未完成前不注册半成品 /docs。

## 3. 调研和取舍依据

### 3.1 路由生命周期调研

本轮通过源码检查和实际探针验证了常见 Go HTTP 路由器的生命周期：

- net/http.ServeMux 支持并发注册与匹配，但每次匹配需要承担读锁。
- Echo 在并发请求和注册路由时可触发 data race。
- httprouter、Gorilla Mux 和 Echo 的顺序追加看似可用，但没有为并发动态注册提供完整保护。
- Fiber 启动后新增路由需要 RebuildTree，源码明确不建议在生产运行期使用，且该过程不是线程安全的。
- go-restful v3.12.2 即使开启动态路由，默认 CurlyRouter 的并发注册探针仍可触发 data race。
- Huma 的路由生命周期由底层 adapter 决定；其类型化操作与 OpenAPI 一致性同样更适合注册后冻结。

因此，ghttp 选择显式冻结，而不是为低价值的动态注册让每次请求承担锁、快照或重建成本。

### 3.2 method-first 与 path-first 性能原型

已使用相同路由集合和请求样本比较路径优先共享树与优化后的 method-first 结构。以下数据只用于同机相对取舍：

| 场景 | path-first | method-first |
|---|---:|---:|
| 静态命中 | 11.66 ns | 11.45 ns |
| 参数命中 | 39.00 ns | 43.00 ns |
| 深参数命中 | 83.10 ns | 83.31 ns |
| catch-all 命中 | 39.32 ns | 43.44 ns |
| 405 | 93.26 ns | 195.2 ns |
| 404 | 16.18 ns | 77.33 ns |
| 跨 method 回退 | 33.37 ns | 25.60 ns |

结论：

- 普通成功请求没有足以支持整体重写的决定性收益。
- path-first 的主要优势集中在 404 和 405。
- method-first 在静态命中和跨 method 回退上并不劣势。
- 保留 method-first，单独记录并优化 404/405，是风险和收益更合理的选择。

## 4. 总体架构

采用“注册定义 → 冻结编译 → 运行期只读”的两阶段架构。

~~~text
公开注册 API
    ↓
routeRegistry
    ↓ routeDefinition[]
freeze
    ├─ 编译 routeMux
    ├─ 组装 handler chains
    └─ 生成 OpenAPI JSON
    ↓
immutable compiledState
    ↓
match → PathValue → middleware → handler / ErrorHandler
~~~

### 4.1 Server 持有的状态

冻结前，Server 持有：

- 可变的 routeRegistry。
- Server 配置和中间件定义。
- Group 与 Route 的作用域元数据。
- OpenAPI 配置。
- 冻结状态锁。

冻结后，Server 发布一个不可变 compiledState，至少包含：

- routeMux。
- 已组装的 handler chain。
- OpenAPI JSON 快照。
- 内置端点信息。

运行期请求只读取 compiledState，不读取或修改注册表，也不获取路由锁。

### 4.2 冻结触发点

以下操作触发冻结：

- 首次 ServeHTTP。
- Run。
- Serve。
- TLS 服务入口。
- Server.OpenAPI()。

冻结过程只能成功执行一次。冻结后修改以下任意内容都 panic ErrServerFrozen：

- 路由和 Group。
- Server、Group、Route middleware。
- Consumes 和 Produces。
- 类型元数据。
- OpenAPI 配置。

不提供解冻、动态重建或运行期注册。

### 4.3 注册与冻结并发边界

注册和冻结使用短时状态锁：

- 注册要么在冻结前完整提交。
- 要么在冻结后明确 panic ErrServerFrozen。
- 不允许 data race、半条路由或部分 method 已注册。

运行期匹配不使用该锁。

## 5. 路由注册模型

### 5.1 唯一入口

类型化路由和原生 handler 都通过 RouteBuilder 注册：

~~~go
ghttp.Route[CreateUserReq, UserResp](server).
    POST("/users").
    To(createUser)
~~~

~~~go
ghttp.Route[struct{}, struct{}](server).
    GET("/metrics").
    ToHTTP(metricsHandler)
~~~

不新增 Server.GET、Server.POST 等快捷方法。

删除：

- Server.Handle。
- Group.Handle。
- 公开 Router 接口。
- WithRouter。
- Router() 逃生口。

### 5.2 routeDefinition

每次 To* 终结调用生成一份完整 routeDefinition。它是 matcher 和 OpenAPI 的共同输入，至少记录：

- 原始 method 值。
- 规范化路径及解析后的 segment。
- 最终 Group 路径。
- Server、父 Group、子 Group、Route middleware。
- handler 类型和终结器类型。
- Req、Resp 类型。
- Consumes、Produces。
- RouteDoc。
- 是否为内置端点。

routeDefinition 提交前完成全部注册期校验。

### 5.3 原子提交

ANY 等多 method 注册必须遵循：

1. 先规范化并验证所有 method/path。
2. 检查全部冲突。
3. 全部通过后一次性提交。

任一 method 冲突时，整个注册失败，不能留下部分路由。

配置错误在 To* 终结调用时立即 panic，并包装公开 sentinel。请求阶段不得因路由配置错误 panic。

### 5.4 RouteBuilder 生命周期

一个 RouteBuilder 只描述一条路由：

- 只能选择一次 method/path。
- To* 是无返回值终结方法。
- 终结后 builder 进入 finalized 状态。
- finalized 后继续修改或再次终结，panic ErrRouteBuilderFinalized。

该设计保留流畅链式构造，同时避免每条注册都要求调用方处理 error。

## 6. 路径语法与规范化

### 6.1 支持的语法

只支持：

- 单段参数：{id}
- catch-all：{path...}

不支持：

- :id
- *path

旧语法不提供 warning 或兼容期。

### 6.2 参数规则

- 参数必须占据完整 segment。
- catch-all 必须是最后一个完整 segment。
- catch-all 匹配零个或多个剩余 segment。
- 参数名遵循 Go 标识符规则。
- 支持驼峰、大写和下划线。
- 参数名区分大小写。
- 同一路径中禁止重复参数名。
- 共享动态节点必须使用相同参数名，该约束跨 method 生效。

示例：

~~~text
/users/{userID}       合法
/users/{UserID}       合法，与 userID 不同
/files/{path...}      合法
/users/prefix-{id}    非法
/files/{path...}/raw  非法
~~~

### 6.3 Group 拼接

- 缺少前导 / 时自动补齐。
- Group 边界的 / 可以省略。
- Group prefix 的尾斜杠不决定最终路由的尾斜杠。

~~~text
Group("/api/") + "v1"   → /api/v1
Group("/api/") + "/v1"  → /api/v1
Group("/api").GET("")   → /api
Group("/api").GET("/")  → /api/
~~~

### 6.4 尾斜杠

默认尾斜杠不敏感：

~~~text
/users
/users/
~~~

两者属于同一匹配域，重复注册视为冲突。

启用 WithStrictRouting 后，两者是不同路由，可分别注册。

catch-all 的零段匹配先作用于基础路径，尾斜杠规则再独立应用。

### 6.5 注册路径中的非法结构

以下结构在注册时非法：

- 内部 //。
- . segment。
- .. segment。
- 非法百分号转义。
- 解码后为 . 或 .. 的编码 segment。
- 非法参数语法。

错误立即 panic 并包装公开路由路径 sentinel。

### 6.6 转义路径处理

segment 边界按转义后的路径确定，然后逐段解码。

因此：

- %2F 不会增加路径层级。
- 参数写入 Request.PathValue 时使用解码值。
- 注册静态 segment 同样解码和规范化。
- /caf%C3%A9 与 /café 属于同一路由。
- 编码后的 {id} 仍是静态文本，不会变成参数。

参数语法在静态 segment 解码前判定，避免编码文本意外获得动态语义。

### 6.7 请求路径非法结构

请求路径不执行 path.Clean，也不自动重定向。

以下请求返回 400 ErrInvalidRequestPath：

- 无法解码的百分号转义。
- 内部空 segment，即 //。
- . segment。
- .. segment。
- 编码后为 . 或 .. 的 segment。

尾斜杠仍由 strict routing 规则单独处理。

非法路径在路由选择前失败，不生成 404、405 或 Allow。

## 7. 路由冲突和匹配优先级

### 7.1 固定优先级

匹配优先级固定为：

1. 静态。
2. 单段参数。
3. catch-all。

优先级只在支持当前 method 的候选中比较。

例如：

~~~text
GET  /users/new
POST /users/{id}
~~~

POST /users/new 命中 POST 参数路由，而不是因 GET 静态路由存在而失败。

### 7.2 冲突矩阵

| 注册组合 | 结果 |
|---|---|
| 同 method、同静态路径 | 冲突 |
| 同 method、{id} 与 {name} 同结构 | 冲突 |
| 不同 method、同动态结构且参数名相同 | 允许 |
| 不同 method、同动态结构但参数名不同 | 冲突 |
| 同 method、静态与参数结构 | 允许，静态优先 |
| 同 method、参数与 catch-all | 允许，参数优先 |
| 默认模式下 /x 与 /x/ | 冲突 |
| strict 模式下 /x 与 /x/ | 允许 |
| ANY 中任一 method 冲突 | 整体失败 |

禁止静默覆盖。所有冲突错误都包装 ErrRouteConflict。

## 8. HTTP method 语义

### 8.1 标准 method

标准 builder 方法使用标准大写值：

- GET
- HEAD
- POST
- PUT
- PATCH
- DELETE
- CONNECT
- OPTIONS
- TRACE

ANY 原子注册上述固定标准集合，不代表任意自定义 method。

### 8.2 CUSTOM

CUSTOM 是自定义 method 的唯一入口：

~~~go
ghttp.Route[Req, Resp](server).
    CUSTOM("PURGE", "/cache/{key}").
    To(handler)
~~~

规则：

- 不 trim。
- 不自动转大写。
- 按原始值区分大小写。
- 必须是合法 HTTP token。

### 8.3 HEAD

- 显式 HEAD 路由优先。
- 没有显式 HEAD 时回退 GET。
- 回退后执行完整 Server、Group、Route middleware 和 handler。
- body 抑制覆盖整个响应管线。
- 状态码和 header 保留。

若同一路径同时存在 HEAD 参数路由和 GET 静态路由，先在 HEAD 候选中完成匹配；只有 HEAD 完全未命中时才查找 GET。

### 8.4 OPTIONS

- 显式 OPTIONS 路由正常执行。
- 不自动生成 204 OPTIONS。
- 路径存在但未注册 OPTIONS 时返回 405 和 Allow。
- 路径不存在时返回 404。

### 8.5 Allow

Allow 汇总所有结构上可匹配的 method，而不是只汇总最高路径优先级：

- GET 自动补充 HEAD。
- OPTIONS 仅在显式注册时出现。
- 自定义 method 保留原始大小写。
- 最终按原始 method 值稳定字典序排序。

## 9. 内部 matcher

### 9.1 唯一实现

新 matcher 位于 ghttp 包内，但全部类型和构造函数保持未导出。

删除公开：

- RadixRouter 和 NewRadixRouter。
- MatchitRouter 和 NewMatchitRouter。
- CompiledRouter 和 NewCompiledRouter。
- StdRouter 和 NewStdRouter。
- MethodMatcher。
- CompressedRadixTree、CompressedRadixNode 及其公开构造函数。

ghttp 只承诺 Server 的路由行为，不承诺树结构。

### 9.2 method-first 结构

routeMux 保持 method-first：

~~~text
method
  ├─ static map
  ├─ parameter tree
  └─ catch-all tree
~~~

注册表负责跨 method 的结构约束。运行时 matcher 只处理只读匹配。

### 9.3 matchResult

matcher 是纯匹配器，不实现 http.Handler，不直接写 400、404 或 405。

一次 match 返回内部 matchResult，至少包含：

- 结果类型：found、notFound、methodNotAllowed。
- handler。
- 紧凑路径参数。
- Allow method 列表。
- HEAD body 抑制标志。

非法路径通过独立 error 返回。

Server 根据 matchResult：

1. 写入 Request.PathValue。
2. 选择成功或错误 outcome。
3. 执行中间件。
4. 调用 handler 或 ErrorHandler。

### 9.4 参数存储

常见参数数量使用内联紧凑存储，超过阈值时才使用 overflow。

运行时不得为了 Params 兼容再构造 context map。Request.PathValue 是唯一权威来源。

## 10. PathValue 与绑定

匹配成功后，Server 在所有用户中间件前调用 Request.SetPathValue。

此后：

- Request.PathValue 是路径参数的唯一权威来源。
- 类型绑定读取 PathValue。
- Params 读取 PathValue。
- 原生 handler 读取 PathValue。
- 中间件可读取或修改 PathValue。

绑定发生在 Route handler 真正执行前，因此中间件修改后的 PathValue 会进入类型绑定。

该行为必须写入中间件和参数绑定文档，避免调用方误以为绑定使用不可变的匹配快照。

## 11. 中间件

### 11.1 固定作用域顺序

冻结时按作用域统一组装：

~~~text
内建 Recovery
  → Server middleware
  → 父 Group middleware
  → 子 Group middleware
  → Route middleware
  → Handler
~~~

该顺序与注册先后无关。

这与 Gin、Fiber 等常见的顺序敏感模型不同，必须在 README、迁移文档和测试中醒目标注。

### 11.2 覆盖范围

Server middleware 覆盖：

- 成功路由。
- 非法路径 400。
- 404。
- 405。
- OpenAPI 端点。
- 错误响应。

Group 和 Route middleware 只在选中具体路由后执行。

### 11.3 不提供 Pre

不提供匹配前 Pre middleware 或内建 URL/method rewrite。

需要 rewrite 时，在 Server 外层包装标准 http.Handler：

~~~go
outer := rewriteMiddleware(server)
~~~

该边界避免路由匹配前后语义混杂。

## 12. panic 与错误响应

### 12.1 内建 Recovery

Server 最外层始终恢复请求阶段 panic：

- 记录 panic 值和堆栈。
- 未提交响应时进入 ErrorHandler，返回安全的 500。
- 已提交响应或 hijack 后只记录，不能再次改写。

配置阶段 panic 不捕获。

公开 ErrHandlerPanic 用于服务端判断 panic 类原因，具体原始原因保存在 HTTPError.Err，不直接暴露给客户端。

### 12.2 ErrorHandler

新增服务级配置 WithErrorHandler：

~~~go
type ErrorHandler func(
    http.ResponseWriter,
    *http.Request,
    *HTTPError,
)
~~~

不增加独立 NoRoute 或 NoMethod handler。

Server 在调用 ErrorHandler 前统一规范化错误：

- 400 使用 ErrInvalidRequestPath 或请求绑定错误。
- 404 使用规范化 HTTPError。
- 405 先设置 Allow，再传入规范化 HTTPError。
- 普通内部错误映射为 500。
- panic 映射为带 ErrHandlerPanic 原因的 500。

### 12.3 消息公开规则

- 调用方显式构造的 HTTPError.Message 视为公开信息，包括显式 5xx。
- 普通 error、panic 和框架内部故障只返回通用 500 消息。
- 原始原因保存在 HTTPError.Err。

默认 ErrorHandler 负责 Envelope 或 codec 输出。安装自定义 ErrorHandler 后，调用方完全拥有错误响应。

### 12.4 Envelope 与 HTTP 状态

Envelope 只改变响应 body 表示，不改变 HTTP 语义：

- 成功为 200。
- 协议、校验、鉴权错误保留真实 4xx。
- 内部错误保留真实 5xx。

自定义 Envelope 若需要所有 HTTP 响应固定为 200，可以在自身实现中明确选择，但这不是框架默认语义。

## 13. RouteBuilder 响应所有权

### 13.1 To

To(handler) 由框架负责：

- 参数绑定。
- 校验。
- 调用 handler。
- 内容协商。
- 编码。
- 写响应。

成功状态固定为 200。

不增加 .Status()。

删除：

- StatusCoder。
- 响应结构体 Status int 的反射识别。
- statusFieldCache 及相关魔法行为。

struct{}、空结构体或零值响应仍按普通 200 编码，不自动推断 204。

### 13.2 自写响应

以下终结器由调用方或专用实现拥有 HTTP 状态和 body：

- ToHTTP。
- ToRaw。
- ToHTTPFunc。
- ToRedirect。
- ToRedirectFunc。
- ToHTML。
- ToSSE。
- ToWebSocket。
- ToStatic、ToStaticFS、ToStaticFile。

ToHTTPFunc 仍由框架绑定 Req；若函数返回 error，则进入统一错误管线。

需要 201、202、204 或按请求动态改变状态时，使用自写响应终结器。

### 13.3 Doc 不改变运行时

DocOption 永远不改变运行时状态、编码或响应体。

Success(Code(...), Message(...)) 只描述业务码和消息，不能充当 HTTP status 配置。

## 14. OpenAPI

### 14.1 唯一生成入口

删除公开：

- OpenAPI builder 类型。
- NewOpenAPI。
- AddRoute。
- Build。

OpenAPI compiler 变为内部组件，只消费冻结后的 routeDefinition。

Server.OpenAPI 是唯一获取入口：

~~~go
func (s *Server) OpenAPI() ([]byte, error)
~~~

行为：

- 调用时冻结 Server。
- 返回不可变 JSON 快照的副本。
- 未启用时返回 ErrOpenAPIDisabled。

### 14.2 HTTP 端点

启用 OpenAPI 后：

- 默认端点为 /openapi.json。
- WithOpenAPIPath(path) 可修改。
- 空 path 关闭 HTTP 暴露，但不关闭文档生成和 Server.OpenAPI。
- 内置端点自动 Hidden。
- 内置路径与用户路由冲突时 panic ErrRouteConflict。
- 端点经过 Server middleware。

### 14.3 普通类型化响应

普通 To 路由：

- 无 Envelope：200 response 使用 Resp schema。
- 内置默认 Envelope：200 response 使用真实包装 schema，data 属性使用 Resp schema。
- 自定义 Envelope：默认不推断响应 schema，避免文档与 wire body 不一致。

### 14.4 自写响应文档

ToHTTP、ToHTTPFunc、ToRaw 默认进入 OpenAPI，但只生成无 schema 的 default response。

提供纯文档 API：

~~~go
HTTPResponse[T](status, ...)
DefaultHTTPResponse[T](...)
~~~

规则：

- 只修改 OpenAPI，不修改运行时。
- T 用于冻结时生成 schema，不创建样例对象。
- NoBody 是显式无响应体标记。
- NoBody 不自动推断 204，状态仍由用户填写。
- 显式 DefaultHTTPResponse 替换自动生成的 schema-less default 占位。
- 一条路由可以声明多个具体 HTTP 响应。
- 非法或重复声明在注册期作为配置错误处理。

自定义 Envelope 用户可以用 HTTPResponse[实际WireType] 明确描述真正响应。

### 14.5 原生 handler

原生 handler 默认进入 OpenAPI：

~~~go
ghttp.Route[struct{}, struct{}](server).
    GET("/metrics").
    ToHTTP(handler)
~~~

占位泛型不得生成虚假 request 或 response schema。

Doc(Hidden()) 可隐藏任意路由。框架内置端点自动隐藏。

### 14.6 特殊终结器

- Redirect：声明具体 3xx 和 Location header。
- SSE：声明成功 200 和 text/event-stream。
- WebSocket：声明 101，并增加 x-ghttp-websocket 扩展。
- Static：使用 default response，不猜测 200、301、304、404。
- catch-all：OpenAPI path 使用普通 {path}，并增加 x-ghttp-catch-all: true。

只自动推导确定信息，不为可能发生的状态制造虚假文档。

### 14.7 交互式文档

未来 /docs：

- 自研轻量 HTML、CSS 和原生 JavaScript。
- 展示路由、参数、schema 和响应。
- 支持在页面上直接发送 HTTP 请求。
- 不依赖 Swagger UI、Scalar、Stoplight、CDN或大型第三方资源。

该任务优先级低，不阻塞本次路由重构。

## 15. 临时 legacyrouter 基线

当前公开的 Radix、Matchit、Compiled、Std 实现暂时移动到：

~~~text
ghttp/internal/legacyrouter
~~~

用途仅限：

- 正确性对照。
- 路由性能基线。
- 新实现回归分析。

约束：

- Server 生产代码不得依赖 legacyrouter。
- 该包不形成公开 API 或兼容承诺。
- 新旧实现通过相同适配层、路由表和请求样本比较。
- benchmark 胜出不会自动删除该包。
- 只有项目负责人明确敲定新实现成为唯一实现后，才删除该包。
- 删除包后必须保留版本化 benchmark 报告。

## 16. 性能策略

### 16.1 成功路径

重点场景：

- 静态。
- 单参数。
- 深参数。
- catch-all。
- HEAD 显式命中。
- HEAD 回退 GET。
- 跨 method 结构回退。

要求：

- 常见成功路径不得新增 allocation。
- 参数内联容量以内保持零额外参数容器分配。
- 不为可替换 Router 或动态注册引入运行期锁。

### 16.2 404 和 405

当前阶段：

- 保留 method-first。
- 不增加路径优先辅助索引。
- 避免不必要的临时 map/slice。
- Allow 使用紧凑列表。

后续专项优化目标：

- 去除 allowedMethods 临时 map/slice 和已观察到的约 64B 分配。
- 减少无效 method 遍历。
- 是否增加辅助索引必须由 benchmark 证明。

### 16.3 回归门槛

- 使用多轮 benchstat。
- allocations/op 不得增加。
- 显著下降超过 10% 阻止合入。
- 显著下降 5%～10% 必须给出原因和取舍说明。
- 404/405 优化单独记录，不能以牺牲普通成功请求为代价。

## 17. 测试策略

### 17.1 单元测试

每个新增或调整的独立函数都需要对应单元测试。

表格测试至少覆盖：

- 路径解析和规范化。
- 百分号转义。
- Unicode。
- 编码斜杠。
- 参数名规则。
- catch-all 零段和多段。
- Group 拼接。
- strict routing。
- method token。
- 冲突矩阵。
- 原子多 method 注册。
- Allow 排序。

### 17.2 matcher 测试

覆盖：

- 静态、参数、catch-all 优先级。
- 只在当前 method 候选中比较优先级。
- HEAD 显式路由和 GET 回退。
- OPTIONS。
- 404。
- 405。
- 自定义 method 大小写。
- 路径参数值和 PathValue。

### 17.3 黑盒实际使用

使用外部测试包按调用方方式使用 ghttp：

- 创建 Server 和嵌套 Group。
- 注册类型化 route。
- 注册原生 http.Handler。
- 使用 Server、Group、Route middleware。
- 启用 Envelope。
- 配置 ErrorHandler。
- 启用 OpenAPI。
- 通过 httptest.Server 发出真实 HTTP 请求。

该层验证 API 可用性，而不仅是内部函数正确性。

### 17.4 并发与 race

覆盖：

- 多请求并发触发首次冻结。
- 冻结后的并发只读匹配。
- 注册与冻结边界。
- 冻结后所有配置修改 panic。
- race detector 无数据竞争。

### 17.5 fuzz

针对路径 parser 和 matcher fuzz：

- 异常 %。
- . 和 ..。
- //。
- Unicode。
- 编码斜杠。
- 超长 segment。
- 大量参数。
- catch-all。

目标是不 panic、不越界、不产生不一致规范化结果。

### 17.6 OpenAPI 测试

结构化断言和稳定 JSON 测试覆盖：

- 普通 Resp。
- 默认 Envelope。
- 自定义 Envelope 无虚假 schema。
- HTTPResponse[T]。
- DefaultHTTPResponse[T]。
- NoBody。
- raw handler default response。
- Redirect、SSE、WebSocket、Static。
- catch-all 扩展。
- Hidden。
- 自定义 OpenAPI path 和冲突。
- OpenAPI() 返回副本并触发冻结。

### 17.7 验证命令

实现阶段至少运行：

~~~text
go fmt ./...
go mod tidy
make check
go test ./...
go test -race ./ghttp/...
相关 benchmark 与 benchstat
~~~

本次设计不需要新增外部依赖。

## 18. 迁移和文档

本次作为明确的破坏性重构直接切换，不保留 deprecated wrapper。

迁移文档必须包含：

| 旧行为/API | 新行为/API |
|---|---|
| Router、WithRouter | 删除，Server 使用唯一内部 matcher |
| RadixRouter 等具体类型 | 删除公开 API；临时只存在 internal 基线 |
| Server.Handle、Group.Handle | Route[struct{}, struct{}](...).ToHTTP(...) |
| :id | {id} |
| *path | {path...} |
| StatusCoder | 自定义状态使用 ToHTTPFunc/ToHTTP/ToRaw |
| Status int | 自定义状态使用自写响应终结器 |
| 运行期注册 | 首次服务或 OpenAPI 后冻结 |
| 注册先后决定 middleware | 固定 Server → Group → Route → Handler |
| 独立 OpenAPI builder | Server.OpenAPI() |
| 硬编码 /openapi.json | WithOpenAPIPath，可关闭 HTTP 暴露 |

README 必须醒目标注：

- 与常见 Go 框架不同的中间件顺序。
- CUSTOM method 不进行 trim 或大小写转换。
- OPTIONS 不自动生成。
- catch-all 可匹配零段。
- PathValue 可被中间件修改并影响绑定。
- 默认尾斜杠不敏感，strict 模式差异。
- Envelope 不把错误 HTTP 状态统一改为 200。
- DocOption 不改变运行时。

发布说明使用 breaking changes 标识，并说明当前仓库没有兼容过渡承诺。

## 19. 公开 sentinel

设计至少需要下列可判断错误：

- ErrServerFrozen。
- ErrRouteConflict。
- ErrRouteBuilderFinalized。
- ErrInvalidRequestPath。
- ErrOpenAPIDisabled。
- ErrHandlerPanic。

其他注册错误按小而明确的类别暴露，并使用 errors.Is 可判断。错误必须包含 method、path 和必要上下文。

## 20. 完成标准

设计实现完成必须同时满足：

- 所有规范性路径和 method 测试通过。
- 404、405、HEAD 和 OPTIONS 行为与本文一致。
- Server middleware 覆盖所有 Server outcome。
- 请求阶段 panic 不泄露内部细节。
- matcher 与 OpenAPI 只消费同一 routeDefinition。
- OpenAPI 描述真实 wire schema。
- 冻结后运行期无路由锁和 data race。
- 新旧 benchmark 报告完整。
- 性能满足回归门槛。
- README、迁移指南和 breaking release note 完成。
- 项目负责人明确确认后，才可删除 internal/legacyrouter。
