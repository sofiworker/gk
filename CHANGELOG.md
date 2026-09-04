# Changelog

本仓库遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.x 阶段 API 允许破坏性变更，但每个破坏性变更必须记录在对应版本节。

## [Unreleased]

### Added

- ghttp：**OpenAPI 3.1 文档生成**——`WithOpenAPI(OpenAPIInfo, opts...)` 开启后，注册期从 typed 入口的 params/body/output 类型收集契约，首次请求时构建一次 spec 并缓存字节，默认在 `/openapi.json` 暴露；配套 `WithOpenAPIRoute`（改路径，传 `""` 则只在内存构建不暴露端点）、`WithOpenAPIServers`、`WithOpenAPIErrorResponses`、`OpenAPIInfo`/`OpenAPIServer`/`OpenAPIOption` 与 `Server.SpecJSON()`（取字节，供写入构建产物或契约测试）。参数说明复用请求期**同一份 `BindPlan`**而非另写一遍规则，故文档与实际绑定行为不会漂移（由 `TestOpenAPI_ParametersMatchBindPlan` 守住）；spec 为手工序列化的**确定性字节**（同一路由表恒等，可做 diff review 与快照测试——若用 `map[string]any` + `json.Marshal`，Go 的 map 遍历顺序随机会让每次产出不同字节）。schema 遵循 `encoding/json` 语义（`json:"-"` 跳过、`omitempty` 不进 required、内嵌字段提升、不导出字段不出现），具名结构体提取为 `components/schemas` 并以 `$ref` 引用（自引用类型因此终止而非栈溢出），`time.Time`→`date-time`、`[]byte`→`byte`、指针→OpenAPI 3.1 的 `["T","null"]`；`NoContent` 输出声明 204 且不带 `content`，错误响应引用与错误链实际输出同形的 `Error` schema；tag 从分组前缀派生（跳过 `api` 与版本段）。未开启时不收集、不构建、不注册路由，spec 端点自身也不出现在 spec 里。
- ghttp：**参数绑定能力对齐**——`path:`/`query:`/`header:` 与请求体表单 `form:` 收敛到同一套绑定引擎（新增 `bind_value.go`），能力完全对等。除标量外新增：`*T` 指针（`nil` 表示"未提供"，可与显式零值区分）、实现 `encoding.TextUnmarshaler` 的类型（`time.Time`/`net.IP` 等，判定**先于** Kind 以免被底层类型截获）、`[]byte`（取原始字节）、`[]T`/`[N]T` 列表（重复出现 `?a=1&a=2` 与逗号分隔 `?a=1,2` 可混用）、`map[string]T`/`map[string][]T`（query 用 `filter[key]=v`，header 用前缀族或 `*` 收全部头）；无 tag 的内嵌与嵌套结构体递归展开（公共参数可抽成可复用结构体，深度上限 8 层，自引用在注册期报错而非栈溢出），tag 值 `"-"` 显式跳过。缺省语义统一：指针/切片/映射保持 `nil`，嵌套结构体指针在整块参数缺省时不被分配，因此 handler 能区分"未提供"与"提供了零值"。元素解析失败整体报 400 而非静默丢弃坏元素；不支持的形态（切片的切片、映射的映射、非字符串键映射、`path:` 绑定映射）在**注册期**报错。
- ghttp：**typed 入口矩阵补全**——新增 `DeleteNone`/`PostNone`/`PutNone`/`PatchNone`/`HeadNone`/`OptionsNone`、`HeadParams`/`OptionsParams`、`DeleteBody`/`DeleteParamsBody`。现七种 method（GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS）各有 `None` 与 `Params` 形态，可带请求体的四种（POST/PUT/PATCH/DELETE）另有 `Body` 与 `ParamsBody`；GET/HEAD/OPTIONS 不提供 `Body` 入口，因为按 RFC 9110 其请求体没有定义语义。`DeleteBody`/`DeleteParamsBody` 服务于批量删除（请求体带 id 列表）这一真实需求。
- ghttp：限流中间件 `RateLimit(RateLimitConfig)` 与 `RateLimitByRoute(rps, burst)`——分片令牌桶（允许 `Burst` 突发、长期收敛到 `RPS`，避免固定窗口的边界翻倍），默认按 `ClientIP()` 分键（遵循可信代理配置，不被伪造头绕过），`KeyFunc` 可改按用户/API key 分键且**返回空串表示豁免**；桶分 16 片以限制锁竞争，空闲超 `IdleTimeout`（默认 10 分钟）自动回收以防随机 IP 打爆内存；超限返回 429 + `Retry-After`，哨兵 `ErrRateLimitExceeded` 可 `errors.Is`，`OnLimited` 可自定义响应；`RPS <= 0` 时返回透传中间件，便于按环境开关而不改代码结构。
- ghttp：CSRF 中间件 `CSRF(CSRFConfig)`——双提交 cookie + `Origin`/`Referer` 同源校验双重防线，恒定时间比较；cookie 故意**不设** HttpOnly（前端 JS 必须读到才能回传），安全方法只补发 token 不校验，`CSRFTokenFromContext(ctx)` 供服务端模板渲染进表单；失败 403，哨兵 `ErrCSRFTokenInvalid`；配套 `DefaultCSRFCookieName`/`DefaultCSRFHeaderName`/`DefaultCSRFFieldName`。非表单请求体（如 JSON）绝不解析，以免消耗 typed 解码所需的 body。
- ghttp：CSRF 新增 `CSRFConfig.UseHostPrefixedCookie` 与常量 `CSRFHostCookiePrefix`——开启后 cookie 名加 `__Host-` 前缀，并**强制** `Secure=true`、`Path=/`、不设 `Domain`（覆盖 `Secure`/`CookiePath`/`CookieDomain` 的显式设置）。这三条是浏览器对该前缀的硬性校验，任一条不满足浏览器会丢弃整个 `Set-Cookie`，双提交会因"永远没有 cookie"而全量 403；换来的是 cookie 不可被任何子域写入或覆盖，正好补上双提交在"子域被攻破"时的弱点。代价是 cookie 不再跨子域共享且要求 HTTPS，故默认关闭。
- ghttp：安全响应头中间件 `SecureHeaders(SecureHeadersConfig)` 与 `SecureHeadersDefault()`——每字段三态（空串=安全默认、`"-"`=不写该头、其他=用该值）；默认写 `X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy` 等，**HSTS 与 CSP 默认关闭**（HSTS 一旦被浏览器记住难以撤回，误配会锁死 localhost，故须 `HSTSMaxAge>0` 显式开启且默认仅在 TLS 请求上写；任何通用 CSP 默认值都会破坏真实页面）；头列表在注册期冻结成切片，请求期只写入且**不覆盖**已存在的同名头，便于单条路由自行覆写。
- ghttp：静态资源预压缩 `WithPrecompressed()` / `WithPrecompressedEncodings(...)`——客户端 `Accept-Encoding` 接受且磁盘存在 `.br`/`.gz` 变体时直接服务，把压缩成本移到构建期（静态资源每请求 CPU 开销归零，且可用更高压缩级别）；`Content-Type` 始终按**原始扩展名**判定，写 `Content-Encoding` 并声明 `Vary: Accept-Encoding`（**即使本次未命中变体也写**，避免缓存把压缩版投给不支持的客户端）；与 Gzip 中间件安全共存（后者见到已有 `Content-Encoding` 即跳过，不二次压缩），变体缺失时静默回退原文件。
- ghttp：`RawHandle` 端点纳入 OpenAPI spec——raw 端点没有可反射的类型,但"路径与方法存在"本身是契约的一部分,漏掉会让 spec 谎报端点不存在并误导契约测试。策略为如实呈现而非编造:登记路径与方法、从路径模板推出必填 `string` 型 path 参数(否则 spec 因模板变量未声明而非法),响应只声明形状未由框架声明而不生成 schema,亦不追加 typed 绑定才产生的 400/415。静态资源与健康检查建在 `RawHandle` 上,故同样进入 spec;spec 端点自身在注册期临时关闭收集以免自我登记。
- ghttp：实时能力(SSE + WebSocket)——`Response` 实现 `http.Flusher`/`http.Hijacker`(`Flush`/`Hijack` 透传底层连接,`Unwrap` 暴露底层 writer);新增 SSE 语法糖 `NewSSEWriter`/`SSEWriter`/`SSEMessage`(`Send`/`SendEvent`/`SendMessage`/`Comment`/`Ping`,自动设头并逐事件 Flush,写错误粘滞);WebSocket 经 `github.com/gorilla/websocket` 升级,新增 `ServeWS`(一行注册升级端点)、`WSUpgrader`/`NewWSUpgrader` 及选项(`WithWSCheckOrigin`/`WithWSReadBufferSize`/`WithWSWriteBufferSize`/`WithWSSubprotocols`/`WithWSHandshakeTimeout`/`WithWSCompression`)、`WSHandlerFunc`、`Upgrade`;底层不支持 hijack 时返回哨兵 `ErrNotHijackable`。ghttp 只提供 hijack/flush 能力,WS 协议委托 gorilla。示例见 `examples/realtime`。
- ghttp：请求体输入契约 `InputSpec[B]`（输出侧 `OutputSpec[O]` 的对称物）——底层 `Body[B](codec)` 接受任意 `RequestDecoder`，常用格式提供泛型快捷糖 `JSONBody[B]()`/`XMLBody[B]()`/`FormBody[B]()`/`TextBody[B]()`；body 类入口（`PostBody`/`PutBody`/`PatchBody`/`PostParamsBody`/…）的 body 参数由 `RequestDecoder` 升级为类型安全的 `InputSpec[B]`。
- ghttp：typed multipart 文件上传——`Upload`（单文件）/`[]Upload`（多文件，`<input multiple>`）作为 **form 请求体结构体**的字段（`form:` tag 指定字段名），由 `FormBody[B]()` 与表单文本字段一并解码；缺文件时保留零值/nil（`Open == nil` 判空）；提供 `Upload.Save(path)`（流式落盘）、`Upload.Bytes()`（读入内存）、`Upload.Open()` 与 `Filename`/`Size`/`ContentType`/`Header` 元数据。
- ghttp：WebSocket 生产化——Timeout 中间件支持 `Hijack`/`Flush`、raw 消息、context 感知读写、子协议协商、keepalive、路由级 Origin 覆盖、升级/处理错误日志。
- 仓库：golangci-lint 配置与全仓 lint 清零；CI 增加 Go 1.27 预览、race、coverage、gofmt、lint 门禁。
- 仓库：模块依赖三层分层原则设计文档（基础契约层 / 能力层 / 适配层）。
- gretry：导出 `NextDelay`/`Wait` 作为仓库唯一退避/抖动实现；gsd 重试复用，删除内联实现。
- glog：核心去除 OpenTelemetry 硬依赖，trace 字段改为可选 `WithTraceExtractor` 注入。
- gotel：移除未实现的 `OTELProvider` 空壳与死代码，收敛为纯可观测性抽象（不依赖 OpenTelemetry）。
- gsql：默认日志改为标准库实现，核心不再依赖 glog（保留 `Logger` 接口，用户可注入 glog 适配器）。
- 仓库：新增 `scripts/check-deps.sh` 依赖方向检查（能力层互引即失败），接入 Makefile 与 CI。
- gresolver：`ParseResolveFile` 完整解析 nameserver/search/domain/options（含 ndots/timeout/attempts），`DefaultNameservers` 返回副本。
- gconfig：新增 `Get/GetInt/GetBool/GetDuration/GetStringSlice/GetStringMap/Set/UnmarshalKey` 类型化访问器（懒加载）。
- gcrypt：新增 AES-GCM（`AESGCMEncrypt/Decrypt`）、Ed25519（`GenerateEd25519Key`/`SignWithEd25519`/`VerifyWithEd25519`）、`RandomBytes`。
- gcompress：新增 zlib/flate 压缩（`Zlib*`/`Flate*`）。
- glog：新增 `DebugfContext/InfofContext/WarnfContext/ErrorfContext`。
- gcache：小接口契约族 `Getter`/`Setter`/`Deleter`（组合为 `KV`）与可选能力 `Exister`/`TTLReader`/`Expirer`/`Counter`/`Pinger`；后端由用户注入，本包不 import 任何客户端库。
- gcache：接口之上的函数层 `GetOrLoad`（loader 模式，取代原先在三个实现里重复的 `GetOrSet`）、`Exists`（原生 EXISTS 优先、否则回退 `Get`）、`Incr`/`Decr`、`GetJSON[T]`/`SetJSON[T]`。
- gcache：`Funcs` 闭包注入（仅覆盖 `KV`，避免可选能力被无条件满足而破坏类型断言式能力探测）。
- gcache：新增 `gcache/cachetest` 子包——按能力拆分的契约一致性套件 `RunKV`/`RunTTL`/`RunCounter`，实现不支持的能力自动跳过；仅依赖标准库 `testing`，用户注入自建后端后可 import 它自证合规。
- gcache：`MemoryCache` 的 `WithCleanupInterval` 传入非正值不再 panic，改为不启动后台清理（过期项仍在读取时剔除）。
- 仓库：`scripts/check-deps.sh` 新增零第三方依赖断言，锁死 `gcache` 只依赖标准库。
- gnet：`netinfo.Interface` 移除永不填充/错位字段（DNSServers/DHCPServer/Location/VendorID/DeviceID），新增 `BusInfo`/`DriverVersion` 正确映射 ethtool；`capture` 新增 `WithFilterInstructions`；`netinfo` 补测试与 gnet 子包文档。
- 仓库：新增文档语言规范（README 与注释统一**中英双语**、错误消息保持英文、标识符与测试名保持英文），全部包 README 统一为中文为主的双语文档。
- 仓库：注释语言规范修订为**中英双语**（中文在前、英文在后），并规定冗余注释（复述代码、无信息量标签）直接删除；首批完成 gresolver/gsql/gcache 库文件与 gretry/grx/gconfig/gresolver/gcrypt/gsd/glog 测试注释的清理。
- 仓库：第二批复述型注释清理（gcache/gsql/gcompress 测试中的纯标签删除），进度见 `docs/language-sweep.md`。
- 仓库：注释语言规范修订为**中英双语**（中文在前、英文在后），并规定冗余注释直接删除；已完成 ghttp/gnet/各包注释与根 README、小包 README 的双语化，gcache/ghttp README 待办。
- 仓库：README 双语化全部完成（根 README、各包 README 均中英双语）。
- 仓库：README 按语言拆分完成——全部包 README 拆为 `README.md`（中文）与 `README.en.md`（英文），互相链接；代码注释保持中英双语内联。

### Changed

- ghttp（**破坏性**）：请求体解码从 `RequestDecoder` 参数改为类型安全的 `InputSpec[B]`——`PostBody`/`PutBody`/`PatchBody`/`PostParamsBody`/`PutParamsBody`/`PatchParamsBody` 的 body 参数原传 `JSONBody()`/`XMLCodec()`/`FormBody()`/`TextBody()`（非泛型 `RequestDecoder`），现改传泛型 `JSONBody[B]()`/`XMLBody[B]()`/`FormBody[B]()`/`TextBody[B]()` 或 `Body[B](codec)`。表单文本字段（`form:` tag）与文件上传（`Upload`/`[]Upload`）由 params 结构体迁移到 **form 请求体结构体**，统一由 `FormBody[B]()` 解码；params 结构体（`path:`/`query:`/`header:`）不再识别 `form:` 与 `Upload` 字段。移除包级非泛型构造器 `JSONBody()`/`FormBody()`/`TextBody()`（`JSONCodec()`/`XMLCodec()` 仍在）。迁移：文件与表单文本字段移入独立 body 类型，注册处改用对应的 `FormBody[B]()`／`JSONBody[B]()`。命中与 typed 热路径性能不变。
- ghttp：404/405 miss 路径性能优化——默认脱敏渲染器下预构建错误体（`prebuiltMissBody`），请求期直接写切片，免去每请求的 JSON 拼接分配；ServeMiss 从 144ns/144B/3allocs 降到 82ns/64B/2allocs（−43% 时间、−56% 内存），命中路径无回归。自定义 `WithErrorRenderer`/`WithNotFoundHandler` 行为不变。
- ghttp：CORS 只对真正的预检请求（OPTIONS + Origin + Access-Control-Request-Method）短路。
- ghttp：Group 中间件在创建子组时快照（gin 语义），与 produces/consumes 快照一致。
- ghttp：RequestID 默认上限 128，超长客户端值替换为新 ID。
- ghttp：Timeout 超时取消 context 并丢弃迟到写入；writer 支持 Hijack/Flush。
- ghttp：路径参数提取单次化，提取错误走路由级错误管线；SSE handler 错误落日志；`Server.Use` 链式化。
- gcache：**破坏性变更**——统一为 ctx 优先的单一方法集，删除全部 `*WithContext` 孪生方法（一个完整实现的方法数由 41 降到 9）。
- gcache：**破坏性变更**——`Increment`/`Decrement` 合并为 `Add(ctx, key, delta)`（原本内部就是同一实现的两个门面）；`Incr`/`Decr` 降级为自由函数。
- gcache：**破坏性变更**——`Expire` 语义收敛：键不存在时返回 `ErrCacheMiss`（原为静默返回 nil）；`ttl <= 0` 表示清除过期时间，与 `Set` 保持一致。
- gcache：能力探测从运行期错误改为编译期/装配期的接口满足判定。
- gcache：**破坏性变更**——`LRUCache`/`LFUCache`/`TimedCache` 泛型化为 `[K comparable, V any]`，值不再是 `interface{}`（构造需给出类型参数，如 `NewLRUCache[string, any](2)`）。
- gcache：**破坏性变更**——三种本地缓存改为内建锁、默认线程安全，删除 `ThreadSafeLRUCache`/`ThreadSafeLFUCache` 及其构造函数。修复两处既有数据竞争：`ThreadSafeLRUCache.Get` 用 `RLock` 却经由 `MoveToFront` 改写共享链表（`LRUCache.Get` 现取写锁）、`TimedCache` 的 `cleanupLoop` 无锁读 `stop` 而 `Close` 持锁写它（改用 `sync.Once` 幂等关闭固定通道）。`go test -race ./gcache/...` 由红转绿。
- 仓库：修复 gresolver 测试脚手架的既有数据竞争——`testDNSServer` 的 `recordsA`/`cnames` 由 `serve` goroutine 读、测试主体写，现以 `sync.RWMutex` 保护。`go test -race ./...` 门禁恢复全绿。
- ghttp：**破坏性变更**——`Engine`/`Mux` 与 `Server` 合并为唯一顶层类型 `Server`（gin 式「engine 与 server 一体」）。`New()` 返回 `*Server`，路由注册、中间件挂载、启动与优雅关闭都在同一个对象上完成，用户不再需要认识第二个类型。`mux` 以首个嵌入字段并入 `Server`（零偏移），`ServeHTTP`/`Group`/`RawHandle` 由字段提升直接成为 `Server` 的方法，不产生手写委托层，请求热路径无额外间接；`ServeStatic`/`ServeMiss`/`ServeMW1Hit`/`ServeMW5Hit` 与合并前持平，且保持 0 B/op、0 allocs/op。
- ghttp：**破坏性变更**——`Option` 由 `func(*Engine)` 改为 `func(*Server)`，原 `ServerOption` 的全部选项（`WithAddr`/`WithReadTimeout`/`WithReadHeaderTimeout`/`WithWriteTimeout`/`WithIdleTimeout`/`WithMaxHeaderBytes`/`WithTLSConfig`/`WithBaseContext`）并入 `Option`，统一由 `New(opts...)` 接收。
- ghttp：**破坏性变更**——`IsStarted()` 语义由「曾经启动过」改为「正在运行」，关闭后返回 false。生命周期由 `started`/`closed` 两个 bool 收敛为单一三态 `state`（idle/running/closed），消除两个 bool 可表达但实际非法的状态组合。
- ghttp：**破坏性变更**——`Shutdown`/`Close` 重复调用不再短路 `return nil`，每次都下沉到标准库。原实现会在首次调用尚在排空时，给第二个调用方一个假的「已完成」信号（实测该调用方在 120–180ns 返回，而真实排空需要 400ms）。
- ghttp：**破坏性变更**——`NewReadinessGate` 的返回类型由未导出的 `*atomicReady` 改为导出的 `*ReadinessGate`；原签名触发 golint「exported func returns unexported type」，调用方无法为其声明变量类型。
- ghttp：**行为变更**——监听失败（端口被占用、权限不足、地址非法）时 `Server` 退回未启动状态，可换 addr 重试。原实现在 `ListenAndServe` 之前就把状态置为 running 且失败后不回滚，导致 `IsStarted()` 谎报正在服务、重试被 `ErrServerStarted` 挡回，`Server` 沦为僵尸对象。回滚是锁内的复合判定：仅当状态仍为 running 时才退回 idle，不覆盖并发 `Shutdown`/`Close` 已写入的终态。

### Changed

- ghttp（**破坏性**）：`File` 签名由 `File(m *Server, urlPath, name string, fsys fs.FS) error` 改为 `File(m *Server, urlPath, name string, fsys fs.FS, opts ...StaticOption) error`，以便单文件路由也能使用 `WithPrecompressed` 等静态选项。原有调用无需修改（变参可省略），仅当以函数值形式引用 `File` 时需调整类型。
- ghttp：**路由匹配提前到全局中间件链之前**。原实现把匹配放在链的终端（`dispatchTerminal`），而全局中间件包在分发器**外层**，因此中间件运行期读到的 `MatchedRoute()` 恒为空——按路由聚合的中间件（指标、按路由限流、tracing span 命名）拿不到路由维度。现由 `dispatchChained` 先完成路径校验与匹配、把结果暂存在 `Request` 上，再执行链，终端直接复用该结果（树仍只走一次）。副产物：路径参数在中间件里同样已就绪，可按资源维度做鉴权。热路径无回归（`ServeStatic` 24ns、`ServeMW5Hit` 32ns，均保持 0 B/op、0 allocs/op）。

### Fixed

- ghttp：修复池化 `Request` 的 `matchedRoute` **跨请求泄漏**——借出时未清空该字段，而它只在匹配成功后写入，导致未命中的请求向中间件报告**上一个请求**的路由模板，按路由聚合的日志/指标/限流因此串数据（404 被记到真实路由上）。两条分发路径（`dispatchChained`/`dispatchRaw`）均已在借出时清零，并补回归测试锁定"命中后紧跟未命中"这一触发序列。
- ghttp：修复 CSRF 的 multipart 表单 token **永远读不到**（所有含文件上传的表单提交被恒定误判为未提交 token → 403）。根因在标准库交互：`parsePostForm` 对 `multipart/form-data` 分支是空实现，但 `ParseForm` 随后仍把 `PostForm` 置为**非 nil 的空 map**，而 `PostFormValue` 只在 `PostForm == nil` 时才补调 `ParseMultipartForm`，于是永不补解析。现按 Content-Type 分派：urlencoded 走 `ParseForm`，multipart 显式走 `ParseMultipartForm`，其余直接返回空串（保持"非表单体绝不解析"以免消耗 typed 解码所需的 body）。
- ghttp：修复参数绑定的三处取值边界——`"a,,b"`/`","`/`"a,"` 这类逗号列表会产出空元素（现跳过空段，全空时返回 `nil` 而非空切片，使"未提供"与"提供了空值"可区分）；`filter[]` 会收集出一个空键；`query:"-"` 未被当作显式跳过。
- ghttp：修复 OpenAPI schema 漏掉**内嵌不导出结构体类型**的字段——`appendFields` 在提升检查之前先做导出性过滤，而 `encoding/json` 仍会提升这类内嵌类型的导出字段，导致 schema 与实际 JSON 不一致。
- ghttp：修复 CSRF 同源校验（第二道防线）的**两个绕过**。① `Origin: null` 原被当作"头缺失"放行，而沙箱 iframe（`<iframe sandbox>`）、`data:`/`blob:` 文档、跨源重定向后的请求发的正是它——这些恰是攻击者可构造的上下文，放行等于对全部攻击者可控来源关闭该防线，与双提交 token 未签名（未绑定会话）组合成完整绕过链；现明确**拒绝** `"null"`，同时保留"两个头完全缺失即放行"（curl／服务间调用不带 cookie，本就不受 CSRF 威胁）。② 同源判定原只比 `Host`，使 `http://example.com` 这个中间人可任意改写的明文页面对 https 站点被判为同源；现比较**带 scheme**，请求自身 scheme 复用 `isTLSRequest`（`req.TLS` + 仅在可信代理后才采信 `X-Forwarded-Proto`），与 `ClientIP`／安全头共用同一套信任模型，避免各处自行判断导致策略分叉。两个洞原被 `TestCheckCSRFOrigin` 固化为预期断言，已一并改正。
- ghttp：修复 BasicAuth 的**用户名枚举时序侧信道**——原 `exists && constantTimeEqual(...)` 在 map 查找失败后立即 401，未知用户根本不执行恒定时间比较，两条路径工作量不同，耗时差可用于枚举有效用户名（密码比较本身用 `subtle.ConstantTimeCompare` 是正确的）。现用户不存在时对固定 dummy 值执行**同样的**比较，并把 `exists` 与比较结果一起参与最终判定（既防编译器把"结果未被使用"的比较优化掉，也防未来重构误删）。可观测行为不变，故另加结构不变量测试守住这次不可观测的差异。

- ghttp（**破坏性/行为变更**）：修复 `FormBody` 声明空 Content-Type 导致**严格校验被完全绕过**——form 解码器原 `ContentType()` 返回空串，而空串在严格校验里意味着**放行**，于是把 `Content-Type: application/json` 的请求体发到 form 端点会被放行，`ParseForm` 又不认识该 CT 因此**根本不读 body**，最终静默解出**零值结构体并回 200**（实测），调用方数据被丢弃却毫无提示。根因是单值 `ContentType() string` 契约表达不了"表单接受两种合法 CT"。现新增可选小接口 `MultiContentTypeDecoder`（`ContentTypes() []string`），`formCodec` 经它声明 `application/x-www-form-urlencoded` 与 `multipart/form-data`，严格校验优先按该集合判定；`ContentType()` 改返回 urlencoded 作为单值代表（供只认单值的调用方与 OpenAPI）。**行为变更**：严格模式下 form 端点收到非表单 CT 由「静默 200 + 零值」变为 **415**（`ErrUnsupportedMediaType`）；缺省 CT 仍放行，`WithStrictContentType(false)` 下行为不变；声明了具体单值 CT 的 JSON/XML/text codec 判定逻辑与结果**完全不变**。
- ghttp：修复 `DeleteBody`/`DeleteParamsBody` + `FormBody` 组合**静默解出零值**——urlencoded 分支原依赖 `req.PostForm`，而标准库只为 POST/PUT/PATCH 读 body 填充 `PostForm`，对 DELETE 等方法**只解析查询串**；而 RFC 9110 允许 DELETE 携带请求体（本包也提供该入口）。现由 `parseURLEncodedBody` 按方法分派：POST/PUT/PATCH 仍走 `ParseForm` 以复用标准库缓存（与同请求内的 CSRF 中间件共享一次解析），其余方法自行读取并 `url.ParseQuery`（`LimitBody` 的 `MaxBytesReader` 仍在 `req.Body` 上生效，解析错误经 `decodeError` 分类以保留 413 语义）。与 `csrfFormToken` 踩过的是同一个标准库坑的另一面。
- ghttp：修复 `Body[B](nil)` **绕过 nil 检查并在注册期 nil 解引用 panic**——`Body[B](nil)` 返回的是一个**非 nil 的接口值**（内部 decoder 为 nil），注册期 `if in == nil` 判不出它，随后 `in.contentType()` 直接 panic（实测 `invalid memory address or nil pointer dereference`），已导出的哨兵 `ErrMissingCodec` 形同虚设。现 `InputSpec` 增加内部 `codecMissing()`，`registerBody`/`registerParamsBody` 两处改判 `in == nil || in.codecMissing()`，缺失解码器**返回 `ErrMissingCodec` 而非 panic**；`contentType()`/`contentTypes()` 两个访问器同时自带 nil 兜底，不依赖"注册期已先拒绝"的调用顺序假设。八个 body 入口（`Post`/`Put`/`Patch`/`Delete` × `Body`/`ParamsBody`）与 `Group` 注册路径均已覆盖，失败的注册不留下半成品路由。
- ghttp：修复 `Response.Flush` **不标记 written** 导致的状态码双观测不一致——`Flush` 会隐式向底层提交 header（标准库为未 `WriteHeader` 的响应补 200）却不设 `written`，于是 handler 先 `Flush()` 再 `return err` 时，链外 `writeError` 读到 `Written()==false` 便去补写错误状态码，标准库打印 `http: superfluous response.WriteHeader call` 并丢弃该状态：**客户端实际收到 200，而 `WithErrorHook` 记录 4xx/5xx**（实测）。现 `Flush` 在尚未提交时先置 `status=200, written=true`（与标准库隐式 200 语义一致）；底层 writer 不支持 `http.Flusher` 时什么也没送出，故**不**标记已提交，以免误让 `writeError` 放弃一个本可正常写出的错误响应。`reset()` 已清 `written`/`status`/`bytesOut`，池化卫生不受影响；`Flush` 不写响应体，故 `BytesOut()` 语义不变。
- ghttp（**行为变更**）：`WithErrorHook` 的 `status` 参数在**响应已提交**时改为上报**客户端实际收到的**状态码（`resp.Status()`），而非错误分类结果。响应头已落地时补写不可能生效，分类值只是"本应回什么"；原实现让 handler 先 `Flush`（或流式编码中途失败）的请求出现"客户端 200 / 钩子 4xx-5xx"的双观测不一致，也与 `Logger`／`metrics` 一贯采用 `resp.Status()` 的口径相矛盾。响应**未**提交时仍上报分类结果（行为不变），`err` 始终原样传出，分类语义可经 `HTTPStatus(err)` 取回，故可观测性不降级。原被 `TestMiss_CustomHandlerWrittenResponseNotRewritten` 固化为「已提交仍报 500」的断言已一并改正。

以下为按 `ghttp/REVIEW.md` 逐条核实后修复的安全与正确性缺陷（每条修复前均独立复现、修复后加回归测试并做变异验证）。

- ghttp（**安全**）：修复尾斜杠重定向（TSR）的**开放重定向**——`redirectTrailingSlash` 原把清理后的路径直接写入 `Location`，而 `//evil.com` 与 `/\evil.com` 会被浏览器解析为**协议相对 URL**，于是 `GET //evil.com/` 把访问者送到攻击者站点，且该跳转来自受信任域名（钓鱼与 OAuth `redirect_uri` 绕过的经典载体）。现新增 `isSafeRedirectTarget` 拒绝 `//`、`/\` 开头及含控制字符的目标（拒绝时按未命中处理，返回 404 而非跳转），并把目标经 `url.URL{Path:..., RawQuery:...}.String()` 重新编码，使路径中的特殊字符不能拆出额外的头部或改写查询串。`redirectTrailingSlash` 相应改为返回是否成功跳转。
- ghttp（**安全**）：修复 SSE 的**事件注入**——`SSEMessage` 的 `ID`/`Event` 与注释原样写出，而 SSE 以换行分隔字段、以空行分隔事件，因此 `ID: "1\ndata: INJECTED"` 可凭空伪造一条完整事件，用户可控内容越过了协议边界。现新增 `stripSSELineBreaks` 剥除单行字段（`id`/`event`/注释）中的 `\r` 与 `\n`；`Data` 的换行按协议折成多行 `data:` 字段，且**单独的 `\r` 也视为行终止符**（原只按 `\n` 拆分，裸 `\r` 会原样进入值、由接收端断行），`\r\n` 作为**一个**终止符消费以免多出空行提前终止事件。
- ghttp（**安全**）：修复 CORS 的**通配源 + 凭据**组合——配置 `AllowOrigins: ["*"]` 且 `AllowCredentials: true` 时原会回显具体 `Origin` 并带 `Access-Control-Allow-Credentials: true`，等于对**任意**站点开放带 cookie 的跨源读取（浏览器禁止 `*` 与凭据并用，回显具体源正是绕过该限制的做法）。现通配时**强制关闭**凭据（`credentialsDroppedForWildcard`），要带凭据必须显式列出源。同时：`Vary: Origin` 改为**所有分支无条件**写出（含被拒的源），否则共享缓存会把针对源 A 的响应连同其 `Access-Control-Allow-Origin` 回给源 B；预检判定改为同时要求 `OPTIONS` **与**非空 `Access-Control-Request-Method`，避免普通 OPTIONS 探测被误当预检短路、业务 OPTIONS 路由拿不到请求。原被 `TestCORS_CredentialsEchoOrigin` 固化为预期断言，已替换为 `TestCORS_WildcardDropsCredentials`／`TestCORS_ExplicitOriginKeepsCredentials`。
- ghttp（**安全**）：修复自定义 `StatusCoder` 返回**非法状态码导致 panic**——`classifyError` 原直接采信 `HTTPStatus()` 的返回值，`WriteHeader(0)`／`WriteHeader(999)` 会让标准库 panic，于是一个业务错误类型的实现失误就能击穿单一错误出口。现经新增的 `isValidHTTPStatus`（100–599）校验，越界回落 500，并在 `writeError` 保留一道纵深钳制。
- ghttp（**安全/可用性**）：修复 `panic` 导致 `http_requests_in_flight` **永久泄漏**——指标记账原在 `next` 返回后进行，panic 会跳过它，每次 panic 使 in-flight 永久 +1 且该请求不进 `http_requests_total`，基于该指标的告警与自动扩缩容从此长期失效（比丢一次埋点严重得多）。现全部记账移入 `defer` 并用 `completed` 标志区分正常返回与 panic，panic 计为 500。
- ghttp（**安全/可观测性**）：修复 `Logger` 把被中间件短路的请求**记成 200**——`LoggerWith` 原读 `resp.Status()`，而错误渲染发生在中间件链**之外**（`writeError` 是链外单一出口），Logger 运行时响应尚未提交、该值为 0 并被当作零值 200。于是 CSRF 403、BasicAuth 401、业务 400 在访问日志里全是 200，而 404/405 因在链内写入却是对的——同一日志流部分对部分错，比全错更难发现。现按与 metrics 相同的方式从 `error` 推断终态码（panic 推断为 500），并把记账移入 `defer` 使 panic 请求的日志不再整条丢失。
- ghttp（**安全**）：修复 `panic(http.ErrAbortHandler)` 被**当作错误吞掉**——该哨兵是标准库约定的"静默中止连接"信号，三处 recover（`safeChain`、`serve`、`Recovery` 中间件）原将其一律转成 500 并写响应体，破坏了约定语义（用于中止已劫持连接或流式响应的场景），且会污染 panic 告警。现三处均在识别后**原样重新 panic**，不调用 `onPanic`、不写 500。
- ghttp（**安全**）：修复 `ClientIP` 在首个转发头解析失败后**继续回退到更弱的头**——反代通常只写 `X-Forwarded-For` 而**透传**客户端自带的 `X-Real-IP`；当 XFF 链全部落在可信网段内（内部调用、多层自有代理）时无法推出对端，原实现会继续尝试 `X-Real-IP` 并采信客户端预置的伪造值，按 IP 的限流与审计随之被绕过。现**只认第一个存在且非空**的转发头:它由可信代理写入即为权威来源,推不出结论意味着"链上没有外部客户端",而非"该问下一个头",此时回退直连地址。
- ghttp（**破坏性/指标格式**）：修复 Prometheus 暴露格式**非法**导致抓取端整份丢弃——原实现把按状态码的计数以额外 `code` 标签挂在 `http_requests_total` 上，同一 metric family 的样本因此标签键集合不一致；HELP/TYPE 亦可能重复、同 family 样本不连续。现输出按 family 分组、HELP/TYPE 各只出现一次、样本连续，**按状态码的计数独立为新 family `http_requests_by_code_total`**（*破坏性*：抓取 `http_requests_total{code=...}` 的仪表盘与告警需改指标名），并补齐直方图必需的 `http_request_duration_seconds_count`。同时修复**标签基数无界**:`req.Method` 原被直接用作标签值,任意客户端可用随机方法名无限增长基数、撑爆抓取端内存(一条无需认证的 DoS 路径),现经 `normalizeMetricsMethod` 归一到 9 个标准方法否则 `OTHER`;标签值经 `escapePromLabel` 转义,路由模板中的引号/反斜杠不再破坏行语法。
- ghttp（**破坏性/行为变更**）：`Timeout`／`TimeoutWithMessage` 超时改为返回哨兵 `ErrRequestTimeout` 并走统一错误链,响应由 **503 `text/plain`** 变为 **504 `application/json`**(`{"error":{"code":"request_timeout",...}}`)。原实现直接 `http.Error(resp, message, 503)` 并 `return nil`,超时因此**完全绕过** `WithErrorHook`／`Logger`／`metrics` 的错误路径——最需要被观测的一类失败恰恰不可观测;504 也比 503 更准确(504 表示上游未能及时应答,503 表示本服务不可用)。新增导出哨兵 `ErrRequestTimeout` 供 `errors.Is` 分支。
- ghttp（**破坏性/安全默认值**）：`SecureHeaders` 的 `Referrer-Policy` 默认值由 `no-referrer-when-downgrade` 改为 `strict-origin-when-cross-origin`——前者在同等级跳转时发送**完整 URL**（含路径与查询串），而 URL 里常带 token、邮箱、订单号；后者跨源时只发源。这也是当代浏览器的默认值。
- ghttp（**破坏性/安全**）：`RequestID` 改为按**字符集**校验客户端提供的 ID（`[0-9A-Za-z._-]`，长度 ≤ `DefaultMaxRequestIDLength`），非法值替换为新生成的 ID。原实现只查长度，于是含 `\r\n` 的值会同时进入响应头与访问日志——这是响应头注入与日志行伪造的经典载体。
- ghttp：修复 `Server.Use` 在**服务开始后静默失效**——全局中间件链在首个请求时经 `sync.Once` 折叠一次，此后 `Use` 追加的中间件永不生效且无任何提示（"以为装上了鉴权、实际完全没跑"是最坏的失败方式）。现新增 `chainBuilt` 标志，链已折叠后 `Use` 记录一条警告日志并直接返回，不再把中间件静默收进列表。
- ghttp：修复 panic 值在错误链中**被丢弃**——recover 后原只返回哨兵 `ErrHandlerPanic`，`WithErrorHook` 拿不到 panic 的具体内容，告警里只有"发生了 panic"而无从归因。现新增内部 `panicErr` 携带原值并导出 `PanicValueOf(err) (any, bool)` 供钩子取回，`errors.Is(err, ErrHandlerPanic)` 语义不变。
- ghttp（**安全**）：修复限流器**桶数无上限**——`IdleTimeout` 回收只在**被访问的分片**上触发且须等到 idle 之后，攻击者以每请求一个全新 key（伪造 IP、遍历租户 ID）持续打入时，桶表在一个 idle 窗口内可无界增长,这本身就是一条内存耗尽路径、恰由限流组件提供。现新增 `RateLimitConfig.MaxKeys`（默认 `defaultRateLimitMaxKeys` = 100000，均摊到 16 个分片，检查留在已持有的分片锁内以免引入全局竞争点），触到上限时先强制回收一次过期桶、仍满则**放行**新 key——限流是可用性保护而非访问控制,"满了就全拒"会让一次冲刷制造全站拒绝服务。
- ghttp（**破坏性/行为变更**）：JSON 解码改为**拒绝首个值之后的尾部内容**——`json.Decoder` 流式读取，读到第一个完整值即返回，故 `{"a":1} GARBAGE` 与 `{"a":1}{"a":2}` 原都静默成功回 200。后者尤其危险:请求体里有两个对象,本端按第一个处理,链路上另一个同样宽松的组件可能取到第二个,两端对"这次请求是什么"理解不一致(请求走私一类的混淆)。现经 `dec.More()` 判定并返回 `ErrInvalidInput`（400）。同时 `io.EOF` 比较由 `==` 改为 `errors.Is`（标准库允许包装返回 EOF，`==` 会漏判而把空体当成解码失败）；空请求体仍按零值放行（RFC 允许非 GET 方法无体，是否必填交由业务判断）。
- ghttp：修复 OpenAPI 文档的**七类正确性缺陷**：① catch-all 路径键 `/files/{fp...}` 使用路由层语法而非 OpenAPI 模板语法，导致路径键与其声明的 `fp` 参数对不上、整份 spec 非法（新增 `specPathTemplate` 归一）；② 组件名 `Error` 原被硬编码，业务类型若也叫 `Error` 会**顶替框架的错误契约**，400/500 的 `$ref` 静默指向业务 schema（现由 `schemaRegistry.errorName` 预留该名，冲突的业务类型退让为 `Error2`）；③ 泛型实例名形如 `Page[pkg.User]`，含 OpenAPI 组件名非法的 `[`／`/`／`]` 且未做 URI 转义（新增 `sanitizeComponentName`）；④ `operationId` 未全局去重，`GET /users/{id}` 与 `GET /users/by-id` 都归约成 `getUsersById`，代码生成器会用后者覆盖前者、凭空丢掉一个端点（新增 `uniqueOperationID`）；⑤ 内嵌同名字段未按 `encoding/json` 的浅深度优先遮蔽规则去重，`properties` 与 `required` 同时出现两个 `name`（新增 `collectFields`）；⑥ 可空的 `$ref` 字段直接返回、`nullable` 被丢弃，指针字段在文档里表现为"必定非空"（现渲染为 `oneOf: [{$ref}, {type:"null"}]`）；⑦ `ServeWS` 绕过 `noteRoute`，WebSocket 路径从 spec 中彻底消失（现按 GET 登记，与 `RawHandle` 口径一致）。
- ghttp：修复自定义 404/405 handler 返回的 error **被静默丢弃**——`writeMiss`／`writeMissRaw` 原忽略返回值，自定义 miss handler 出错时客户端收到空 200。现转交 `m.writeError` 走统一错误链（*行为变更*：此前的隐式 200 变为 500）。
- ghttp：修复分组前缀拼接产生**双斜杠路由**——`Group("/api/")` 与 `"/v1/x"` 原拼成 `/api//v1/x`，路由树按字面匹配，该 URL 与文档、与用户预期都不一致（*行为变更*：受影响的挂载路径改变）。现由新增的 `joinRoutePath` 统一归一，`Group` 与路由文档收集共用同一实现以免二者漂移；`""` 与 `"/"` 作为分组根等价于直接注册前缀本身（`/api/` 仍按 TSR 301 到 `/api`）。
- ghttp：修复全局中间件改写 `URL.Path`/`Method` 后**路由不再生效**（"匹配提前到链前"引入的回归）——为让 metrics/限流在进入下游前读到 `MatchedRoute`，路由匹配被提前到全局链之前，副作用是 strip-prefix 网关、URL 重写、`X-HTTP-Method-Override` 类中间件改写后终端仍执行**旧路径的匹配结果**：重写目标明明已注册却 404/405，且 `writeMiss` 按新路径算 `Allow`、按旧解析判 miss，能产出"GET 收到 405 + `Allow: GET`"的自相矛盾响应。现 `resolvedRoute` 记录解析时的 path/method，终端发现二者被改写时**清空旧参数并重新解析一次**（重写是冷路径，一次额外树查找可接受）；未改写的请求仍复用链前结果，热路径零变化。
- ghttp：修复 `Gzip` 中间件**不写 `Vary: Accept-Encoding`**——同一 URL 的响应体随 `Accept-Encoding` 而变（压缩/未压缩两种形态），缺了 Vary 时共享缓存可能把 gzip 响应喂给不支持的客户端，或把未压缩响应当作唯一形态缓存。现挂载后无条件声明（**不论本次请求是否接受 gzip**，两种形态都真实存在）；新增 `ensureVary` 幂等追加（大小写不敏感、识别逗号合并列表），静态预压缩路径改用同一实现，与 `Gzip` 中间件同挂时不再产生重复 Vary 声明。
- ghttp：修正 `WSHandlerFunc` 文档与实现不符——原注释称"ctx 随请求取消而取消，可用于协调关闭"，但升级后连接已被 hijack、脱离 `http.Server` 管理，`Shutdown` 既不取消该 ctx 也不等待连接排空。注释改为如实说明该行为并给出优雅关闭建议（业务内监听外部信号）。
- ghttp：修复 `BindPlan.needQuery` **写而不读**——注释声称"请求期据此跳过 URL.Query() 解析"，实际两个 typed 执行器仍无条件调用 `req.Query()`。现 `needQuery=false`（纯 path/header 端点）时跳过 `url.ParseQuery`，绑定行为不变（query 步存在时必有 `needQuery=true`，nil query 不会被读取）；同时删除从未被消费的 `needHeader` 字段（header 读取本就是轻量的 `Header.Get`，无需预解析）。

### Deprecated

- ghttp：**修正上一版本的错误记录**——曾记为「`WithStrictContentType()` 现为 no-op 别名，计划 v0.2 移除」，与代码不符：该选项确实写入 `Server.strictContentType`（`server.go`），注册期两处 body 入口也确实读它，`WithStrictContentType(false)` 会跳过校验、把请求体直接交给解码器（旧的宽松行为）。**它未被弃用，也没有移除计划**；默认值为 `true`（不匹配返回 415）。

### Removed

- ghttp（**文档勘误**，非本批改动）：以下条目原记于 `Added`，但其描述的能力已随 legacy 实现在 `5a7b3db`（remove legacy implementation，119 文件 / -29805 行）及后续提交中删除，条目未同步，故移入本节并标注实情——**当前源码中均不存在**：HTTP client 全族（重试、before/after 钩子、错误模型绑定、输出文件、认证 scheme、client cookies、`Client.Get/Post/Put/Delete` 类型化方法及其钩子顺序约定）；与 gerr 的错误互操作 `FromGerr`/`ToGerr`/`GerrStatus`（ghttp 现完全不 import gerr）；Go 1.27 泛型方法 API 的 `ToNoInput`/`ToNoOutput`/builder 级 `Group`/动词链与 `Route[Req,Resp]` 兼容 shim；SSE `WriteJSONWithID`；`WithEnvelope`；`Consumes` 与 `WithStrictContentNegotiation()`；`Server.MatchedParams(r)`（中间件改用 `Request.Params` 与 `Request.MatchedRoute()`）；`MapInputs`/`PathInt64` 等显式描述器（typed 入口已取代）。上一条 StructInput 删除记录中提到的这些迁移出口同样已不复存在。
- ghttp（**破坏性**）：**全面移除内置校验**——删除 `validate` struct tag 全族规则（`required`/`min`/`max`/`len`/`oneof`/`email`）及其注册期闭包编译机器（`compileFieldRules`/`fieldRule`/各 rule 函数）、请求体 `Validator` 接口与 `Validate() error` 自动校验（`bodyValidatorFor`/`runValidate`）、以及哨兵错误 `ErrValidation` 与 `ErrMissingRequired`（错误链不再产出 `validation_failed`/`missing_required` code）。params 现只做类型绑定（解析失败/越界仍报 400 `ErrInvalidInput`），请求体只做解码；required/范围/枚举等业务规则改由 handler 自行判断，返回实现 `StatusCoder` 的 error 精确映射状态码。校验体系将另行设计。删除 `ghttp/validate.go` 与 `ghttp/validate_test.go`。
- ghttp：**删除 StructInput 老 API 全族**（性能收敛）：`StructInput[T]`（结构体 tag 绑定）、`Body[T]` 惰性请求体视图、`ParseInput`、`WithBodyDecoder`、`ErrInvalidParamsUsage`、`ErrMultipleBodyFields`、`ErrBodyFieldMustBeValue` 及全部结构体 tag 解析机器（`structInfo`/`parseCompiledInput`/`bindDirectPathInput` 等）。结构体 tag 绑定路径是每请求开销的最大单笔来源；迁移方式见上条勘误——最终出口是 typed 入口（`GetParams`/`PostBody` 等）配 `path:`/`query:`/`header:` tag 与 `JSONBody[B]()`。
- ghttp：删除基于结构体类型的 OpenAPI 反推死代码（`compileRouteOpenAPIMetadata`/`extractParametersFromType`/`extractBodySchema` 及 `routeDefinition.reqType/respType`）；OpenAPI 现完全由显式输入/输出描述器元数据生成。
- ghttp：旧 `openAPIBuilder` 死代码、未用的 util/form 辅助函数、client 未用字段。
- 仓库：删除基于已移除 RouteBuilder API 的陈旧示例（`example/ghttp_usage`、`example/server_review`、`example/server_review_v2`、`example/server_review_v3`）；相关评审文档（`docs/ghttp-server-review*.md`）随 typed API 正式化一并清理。
- gnet/rawcap：库内 demo `main` 文件（不做 demo，方向改为真实库）。
- gcache：**破坏性变更**——删除 Redis 与 Valkey 内置实现（`gcache/redis.go`、`gcache/valkey.go` 及其测试）、`NewRedisCache`/`NewValkeyCache`、`RedisCache`/`ValkeyCache`，并从根 `go.mod` 移除 `github.com/redis/go-redis/v9`、`github.com/valkey-io/valkey-go` 及其独有间接依赖 `github.com/cespare/xxhash/v2`、`github.com/dgryski/go-rendezvous`。本包退回纯进程内实现 + 注入式契约，远端后端由用户传入（迁移方式见 `gcache/README.md`）。实测：只用 `MemoryCache` 的程序由手写等价实现的 2.07x 降至 1.16x，单个二进制省下 2.04 MiB（原体积 43.7%）；`go list -deps ./gcache` 第三方包由 14 个降为 0。
- gcache：**破坏性变更**——删除 `Hash*`/`List*`/`Set*` 接口与方法。它们是 Redis 族独有能力，Memcached、etcd 与全部 Go 进程内库均不支持，导致 `MemoryCache` 需要 20 个 `ErrNotSupported` 桩、一致性测试需按具体类型开洞。需要 Redis 数据结构者请直接使用 Redis 客户端。
- gcache：**破坏性变更**——删除大接口 `Cache`/`CacheWithContext`/`BasicCache`/`BasicCacheWithContext` 及各能力接口的 `*WithContext` 变体，改为在调用点按需组合小接口。
- gcache：**破坏性变更**——`Options` 移除 9 个连接字段（`Address`/`Password`/`DB`/`PoolSize`/`MinIdleConns`/`DialTimeout`/`ReadTimeout`/`WriteTimeout`/`MaxRetries`）与对应 `With*`，仅保留 `CleanupInterval`。原映射本就有损（valkey 侧静默丢弃 `MinIdleConns`/`ReadTimeout`，`MaxRetries` 退化为布尔 `DisableRetry`），且无法表达 TLS/Cluster/Sentinel；改为由用户直接配置客户端。
- gcache：**破坏性变更**——删除 `Serializer`/`JSONSerializer`（包内无消费者），改用 `GetJSON[T]`/`SetJSON[T]`；删除 `ErrNotSupported`。
- ghttp：**破坏性变更**——删除 `Engine` 类型、`Mux = Engine` 类型别名、`NewServer(e *Engine, opts ...ServerOption)`、`ServerOption` 类型与 `ErrEngineNotStarted` 哨兵错误；删除 `Server.ListenAndServe`/`Server.ListenAndServeTLS`（改用 `Server.Run(addr)`/`Server.RunTLS(addr, certFile, keyFile)`）；`Static`/`StaticFS`/`File`/`Health`/`Ready` 的首参由 `*Mux` 改为 `*Server`。

## [0.1.0] - 待发布

首版公开 API 快照（见 `docs/superpowers/specs/2026-08-07-ghttp-api-freeze-v0.1.md`）。开发中，禁止生产使用。
