# Changelog

本仓库遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.x 阶段 API 允许破坏性变更，但每个破坏性变更必须记录在对应版本节。

## [Unreleased]

### Added

- ghttp：请求体输入契约 `InputSpec[B]`（输出侧 `OutputSpec[O]` 的对称物）——底层 `Body[B](codec)` 接受任意 `RequestDecoder`，常用格式提供泛型快捷糖 `JSONBody[B]()`/`XMLBody[B]()`/`FormBody[B]()`/`TextBody[B]()`；body 类入口（`PostBody`/`PutBody`/`PatchBody`/`PostParamsBody`/…）的 body 参数由 `RequestDecoder` 升级为类型安全的 `InputSpec[B]`。
- ghttp：typed multipart 文件上传——`Upload`（单文件）/`[]Upload`（多文件，`<input multiple>`）作为 **form 请求体结构体**的字段（`form:` tag 指定字段名），由 `FormBody[B]()` 与表单文本字段一并解码；缺文件时保留零值/nil（`Open == nil` 判空）；提供 `Upload.Save(path)`（流式落盘）、`Upload.Bytes()`（读入内存）、`Upload.Open()` 与 `Filename`/`Size`/`ContentType`/`Header` 元数据。
- ghttp：Go 1.27 泛型方法 API（Server/Group 根组动词链、`ToNoInput`/`ToNoOutput`、builder 级 `Group`、`Client.Get/Post/Put/Delete` 类型化方法）。
- ghttp：WebSocket 生产化——Timeout 中间件支持 `Hijack`/`Flush`、raw 消息、context 感知读写、子协议协商、keepalive、路由级 Origin 覆盖、升级/处理错误日志。
- ghttp：SSE `WriteJSONWithID`（Last-Event-ID 续传）。
- ghttp：client 正式实现——重试、before/after 钩子、错误模型绑定、输出文件、BasicAuth/认证 scheme、查询参数/字符串、按请求超时、client cookies、Response 访问器、调试日志。
- 仓库：golangci-lint 配置与全仓 lint 清零；CI 增加 Go 1.27 预览、race、coverage、gofmt、lint 门禁。
- 仓库：模块依赖三层分层原则设计文档（基础契约层 / 能力层 / 适配层）。
- gretry：导出 `NextDelay`/`Wait` 作为仓库唯一退避/抖动实现；ghttp client 与 gsd 重试统一复用，删除各自内联实现。
- glog：核心去除 OpenTelemetry 硬依赖，trace 字段改为可选 `WithTraceExtractor` 注入。
- ghttp：新增与 gerr 的错误互操作——`FromGerr`/`ToGerr`/`GerrStatus` 双向转换与 HTTP 状态码 ↔ `gerr.Kind` 映射。
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
- ghttp：已配置 `Consumes` 时缺失 Content-Type 返回 415（原为按 JSON 解析）。
- ghttp：CORS 只对真正的预检请求（OPTIONS + Origin + Access-Control-Request-Method）短路。
- ghttp：Group 中间件在创建子组时快照（gin 语义），与 produces/consumes 快照一致。
- ghttp：RequestID 默认上限 128，超长客户端值替换为新 ID。
- ghttp：Timeout 超时取消 context 并丢弃迟到写入；writer 支持 Hijack/Flush。
- ghttp：路径参数提取单次化，提取错误走路由级错误管线；SSE handler 错误落日志；`Server.Use` 链式化。
- ghttp：client 钩子顺序固定为 client-before → request-before → 发送 → client-after → request-after。
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

### Deprecated

- ghttp（Go 1.27 构建）：`Route[Req,Resp](target)` 兼容 shim，新代码使用动词直挂。
- ghttp：`WithStrictContentNegotiation()` / `WithStrictContentType()` no-op 别名，计划 v0.2 移除。

### Removed

- ghttp（**破坏性**）：**全面移除内置校验**——删除 `validate` struct tag 全族规则（`required`/`min`/`max`/`len`/`oneof`/`email`）及其注册期闭包编译机器（`compileFieldRules`/`fieldRule`/各 rule 函数）、请求体 `Validator` 接口与 `Validate() error` 自动校验（`bodyValidatorFor`/`runValidate`）、以及哨兵错误 `ErrValidation` 与 `ErrMissingRequired`（错误链不再产出 `validation_failed`/`missing_required` code）。params 现只做类型绑定（解析失败/越界仍报 400 `ErrInvalidInput`），请求体只做解码；required/范围/枚举等业务规则改由 handler 自行判断，返回实现 `StatusCoder` 的 error 精确映射状态码。校验体系将另行设计。删除 `ghttp/validate.go` 与 `ghttp/validate_test.go`。
- ghttp：**删除 StructInput 老 API 全族**（性能收敛）：`StructInput[T]`（结构体 tag 绑定）、`Body[T]` 惰性请求体视图、`ParseInput`、`WithBodyDecoder`、`ErrInvalidParamsUsage`、`ErrMultipleBodyFields`、`ErrBodyFieldMustBeValue` 及全部结构体 tag 解析机器（`structInfo`/`parseCompiledInput`/`bindDirectPathInput` 等）。结构体 tag 绑定路径是每请求开销的最大单笔来源；迁移方式：`MapInputs(PathInt64("id"), ...)` 显式描述器 + `InputFunc`/`JSONBody[T]`。中间件读取路径参数改用 `Server.MatchedParams(r)`。
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
