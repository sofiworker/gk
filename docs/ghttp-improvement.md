# ghttp 设计锐评与改进文档

> 日期：2026-07-03
> 基准数据：`go test -bench . -benchmem`（AMD Ryzen 9 9950X, Windows）+ `docs/benchmark-results.md`（bombardier 对比测试）
> 视角：以框架使用者的身份评价 API 设计合理性，以基准数据评价性能，最后给出改进路线图。

---

## 一、API 设计锐评（使用者视角）

### 1.1 做对了的地方

- **`Route[Req, Resp]` 泛型链式构建器**是正确的大方向：handler 签名 `func(ctx, Req) (Resp, error)` 干净、可测试、不碰 `http.ResponseWriter`，比 gin/echo 的 `c *Context` 面条式 API 高一个档次。
- **`Params` 作为不可变输入快照**、不持有 ResponseWriter，职责划分清晰；`Cookies() []*http.Cookie` 接口驱动的响应 cookie 写入也很优雅。
- **纯 `net/http`、Server 实现 `http.Handler`**，可嵌入、可 httptest、可被任意标准中间件包裹 —— 生态兼容性是实打实的。
- **配置错误集中到 `Run`/首次 `ServeHTTP` 时 panic** 而不是注册时到处返回 error，保住了链式调用的手感（但见 1.2-② 的代价）。
- **`Consumes`/`Produces` 三级继承（server → group → route）** 语义与 OpenAPI 对齐，415 行为明确。

### 1.2 使用者会骂的地方（按疼痛程度排序）

**① README 宣传的能力有虚假成分 —— 这是信任问题，比任何设计瑕疵都严重**

- **WebSocket 是 stub**。`buildWebSocketHandler` 直接返回 `501 Not Implemented`（builder.go:638-646），客户端 `Client.WebSocket()` 返回一个内部 conn 为 nil 的空壳（client.go:626-637），调用 `ReadJSON` 直接 nil panic。README 却把 WebSocket 列为特性并给出示例代码。
- **README 说"默认响应格式 {code,msg,data}"，实际默认 envelope 为 nil**（server.go:52-61 只从 config 取，`DefaultEnvelope` 从未被默认启用），不配 `WithEnvelope` 时返回裸 JSON。
- 更糟的是**客户端泛型 `Do[Req,Resp]` 硬编码假定服务端启用了 envelope**（client.go:545-560 先按 `{code,msg,data}` 解包）。同一个包的客户端和服务端，默认行为互相对不上。

**② 错误延迟到 Run 时 panic，但注册点信息丢失**

`To()` 返回 void，`recordSetupError` 只 `errors.Join`。当你注册了 80 条路由、其中一条 `Produces` 写错时，panic 信息里没有是哪一条路由、哪个文件哪一行注册的。使用者只能二分注释排查。链式 API 可以保留，但 error 里必须带 method+path（甚至注册处的 caller 信息）。

**③ `Responds().With().Desc().End()` 子构建器有静默陷阱**

`Desc()` 修改的是"最后一个 response"（builder.go:212-217）：先 `Desc` 后 `With` 会把描述安到上一个 response 头上或直接丢弃，编译器和运行时都不报错。子构建器应该持有自己的 spec，`End()` 时再 append。

**④ 无 tag 绑定，query/header 参数全靠手取**

`req.Query("page")` + 手动 `strconv.Atoi` + 手动默认值，是从 gin (`form:"page"`)、echo、go-restful 全都支持的 struct tag 绑定倒退回了原始时代。`Body` 字段靠魔法命名 + 反射填充，query/path/header 却不能同样声明式绑定 —— 同一个输入结构体里两套心智模型。这也直接伤害 OpenAPI：`Params` 是黑盒，参数文档必须靠 `PathSchema`/`QuerySchema` 手动补，等于让用户写两遍。

**⑤ 无 envelope 时错误响应是 text/plain**

`writeError` 落到 `http.Error`（builder.go:576-586）。一个 `Produces(JSON)` 的 API，成功返回 JSON、失败返回纯文本，客户端解析必须写两套逻辑。错误也应该走 codec，输出结构化 `{code,message}`。

**⑥ 客户端一半的 API 是装饰品**

- `Client.SetAuthToken` 设置的 token 在 `R()` 中不复制、`execute` 中不读取 —— **完全无效**。
- `Request.Timeout`、`ResultError`、`BasicAuthUser/Pass` 字段声明了但 `execute` 从未使用 —— 死字段，用户设置后静默无效。
- `R()` 复制 header/query 时直接 `r.Header[k] = v` 共享底层 slice，之后 `Add` 可能污染 client 级默认值 —— aliasing bug。
- `ClientOption` 只有 `WithBaseURL` 一个；自定义 `http.Client`/Transport/代理/连接池全都无入口。
- `Do[Req,Resp]` 只序列化 `Body` 字段，path/query 参数无法通过 `Req` 传递，`GET[Req, Resp]` 的 `Req` 类型参数纯属摆设。
- `execute` 里 `resp.BindJSON(r.Result)` 的 error 被吞掉。

**⑦ 路由语义细节不达标**

- **405 不完整**：method 不存在返回 405 但无 `Allow` header；path 存在于其他 method 时返回 404 而非 405（radix_router.go:114-133）。
- **强制 `TrimRight(path, "/")`**：`/users/` 与 `/users` 无条件等价，无 redirect-trailing-slash 选项，也无法区分注册。
- **`ANY` 包含 CONNECT/TRACE**：TRACE 有 XST 安全隐患，默认注册它是给用户挖坑。
- GET 路由不自动响应 HEAD。

**⑧ 中间件细节**

- `Timeout` 只包 context，不写 504 —— handler 不主动检查 ctx 时超时完全无感知。
- `CORS`：origin 不匹配时依然下发 `Allow-Methods/Headers`；`AllowCredentials` 与 `*` 组合不校验；不设置 `Vary: Origin`（缓存投毒风险）；预检短路发生在鉴权中间件之前还是之后取决于注册顺序，无文档说明。
- `Recoverer` 不打印堆栈（只有 panic 值），线上排障基本没法用。

**⑨ 安全默认值缺失**

请求体无大小限制 —— 全框架搜不到 `MaxBytesReader`/`LimitReader`，`json.Decoder` 直接吃整个 body，一个 10GB 的请求体就能打爆内存。multipart 有 `defaultMaxMemory` 但普通 body 没有任何防线。作为 2026 年的新框架这是不可接受的默认值。

---

## 二、性能评价（基于基准数据）

### 2.1 结论先行

**路由层是第一梯队水平，但"泛型 handler 全家桶"路径比裸路由慢一个数量级，瓶颈全在 `Params` 深拷贝和每请求反射/分配上。** bombardier 测试的 177k req/s 是绕过泛型层直接注册 raw handler 的成绩，不代表用户实际使用 `Route[Req,Resp]().To()` 时的性能。

### 2.2 分层数据

| 层 | 结果 | 评价 |
|----|------|------|
| RadixRouter 查找 | 125–170 ns/op, 160 B/3 allocs | 优秀。且这 3 次分配来自循环内的 `httptest.NewRecorder()`，路由本身接近零分配 |
| StdRouter 参数路由 | ~718 ns/op, 880 B/8 allocs | ServeMux 固有开销，作为兼容选项可接受 |
| JSON codec | marshal 1.1µs / unmarshal 1.7µs | 标准库水平，正常 |
| **ParseInput（空 body）** | **3.7 µs/op, 7.3 KB, 17 allocs** | **主要瓶颈。** 一个 GET 请求光输入绑定就烧掉 7KB |
| ParseInput（JSON body） | 6.8 µs/op, 9.9 KB, 41 allocs | 同上叠加 body 解码 |
| DefaultEnvelope | 3.5 µs/op, 6.2 KB, 21 allocs | 每请求 Accept 协商 + envelope 结构分配 + encoder 分配 |

对比参照：gin/echo 的完整"绑定+序列化"链路一般在 1–2 µs、<2 KB。ghttp 泛型路径的**每请求框架开销约 10–13 µs、15–17 KB**，是竞品的 5–8 倍分配量。

### 2.3 开销去向分析

1. **`Params` 全量深拷贝**（params.go:23-31）：每个请求无条件 `header.Clone()` + query 深拷贝 + cookies 逐个复制。快照语义是好的，但 header 通常有 8–15 个 key，Clone 一次就是几 KB。绝大多数 handler 只读 1–2 个值，为不可变性付出 100% 请求的拷贝成本，性价比极低。**这是 empty-body 也要 3.7µs/7.3KB 的直接原因。**
2. **每请求反射**：`newInputTarget` 每次 `reflect.New`（builder.go:467-481）、`parseInputWithConfigAndPathParams` 每次走 `reflect.ValueOf` 链。类型在注册时已知，这些都可以在 `To()` 时预编译成闭包。
3. **每请求 codec 查找**：`writeResponse` 每次 `codecMgr.Resolve(produces)` 带锁 map 查找（builder.go:595-604）。`resolveProduces` 在注册时已验证过 codec 存在，直接把 `Codec` 存进 route 即可归零。
4. **Envelope 路径**：每请求 `strings.Split(accept, ",")` + 排序协商 + envelope 匿名结构体逃逸。Accept 值的种类极少，一个小 LRU 或 sync.Map 缓存协商结果即可。
5. **默认 validator 无条件反射**：`playgroundValidator.StructCtx` 对无 validate tag 的类型也要遍历字段。可在首次见到类型时缓存"该类型无任何 tag"直接跳过。

### 2.4 基准方法论问题

- `BenchmarkRadixRouter` 把 `httptest.NewRecorder()` 放在循环内，160B/3allocs 全是 recorder 的分配，掩盖了路由真实数字（应循环外复用或用 no-op writer）。
- `BenchmarkFullServer` 走真实 TCP 环回（518 µs/op 是网络往返主导），测的是 Windows 环回栈不是框架；应直接 `s.ServeHTTP(recorder, req)` 测框架路径，TCP 版作为补充。
- bombardier 对比测试注册的是 raw handler，README 应注明该数字不含泛型绑定层，否则有误导性。

---

## 三、缺失能力清单

| 类别 | 缺失项 | 严重度 |
|------|--------|--------|
| 协议 | WebSocket 实现（当前 501 stub） | 高（已宣传） |
| 安全 | 请求体大小限制（MaxBytesReader） | 高 |
| 绑定 | query/path/header 的 struct tag 声明式绑定 | 高 |
| 路由 | 405 + Allow header、HEAD 自动处理、trailing-slash 策略 | 中 |
| 错误 | 结构化错误响应（无 envelope 时）、Recoverer 堆栈 | 中 |
| 中间件 | 压缩（gcompress 未集成）、限流、真正中断的 Timeout | 中 |
| 可观测 | gotel/trace 集成、request-scoped logger、路由表 dump | 中 |
| OpenAPI | swagger-ui/redoc 挂载、securitySchemes、从绑定 tag 自动生成参数文档、/openapi.json 路径可配 | 中 |
| 客户端 | retry（gretry 未集成）、Transport 注入、流式响应、SSE 自动重连/Last-Event-ID、修复死字段 | 中 |
| 静态文件 | ETag/If-None-Match、Cache-Control 配置 | 低 |
| 工程 | h2c、优雅关停钩子（OnShutdown）、TestClient 辅助 | 低 |

---

## 四、改进路线图

### P0 — 正确性与诚实（1 个迭代内）

1. **WebSocket 二选一**：要么基于 `golang.org/x/net/websocket` 或 gorilla 真正实现，要么从 README 和公开 API 中移除。stub 必须消失。
2. **统一 envelope 默认值**：要么 `New()` 默认启用 `DefaultEnvelope`（与 README、客户端 `Do` 对齐），要么改 README 并让 `Do` 探测/可配 envelope。推荐前者。
3. **修复客户端死 API**：`SetAuthToken` 生效、`Request.Timeout`/`BasicAuth`/`ResultError` 落地或删除、修复 `R()` 的 slice aliasing、`BindJSON` 错误上抛。
4. **加请求体大小限制**：`WithMaxBodyBytes(n)`（server/route 两级），默认给一个合理值（如 4MB），解析前套 `http.MaxBytesReader`。
5. **setup error 带上下文**：`recordSetupError` 包裹 `method path` 与注册 caller（`runtime.Caller`），panic 信息可定位。
6. **修复 `Responds` 子构建器**：spec 存在 `responseSpecBuilder` 自身，`End()` 时 append，消灭 `Desc` 先于 `With` 的静默丢失。

### P1 — 性能（热路径归零分配，以下收益均为实测）

> 实测方法：`ghttp/perf_probe_test.go` 把类型化链路拆成独立组件分别测量，并用手写原型（惰性 Params + 注册期缓存 codec + 无反射构造）搭出同路由、同输出的对照组。环境：Ryzen 9 9950X / Windows / in-process `ServeHTTP`（无 TCP）。

**基线与上限（GET /users/{id}?page=1，含 4 个 header + 2 个 cookie）：**

| 链路 | ns/op | B/op | allocs/op |
|------|-------|------|-----------|
| Raw handler（框架地板） | 282 | 432 | 5 |
| 当前类型化链路 `To()` | 3675 | 5445 | 29 |
| **优化原型（同功能）** | **714** | **880** | **9** |

即：类型化抽象当前每请求多付 ~3.4µs/5KB/24 allocs，优化后可压到 ~430ns/450B/4 allocs（相对 raw），**整链 5.1 倍提速、分配量 -84%**，且不改任何公开 API。

**开销分解（实测，按收益排序）：**

| # | 改动 | 当前成本（实测） | 改后成本（实测） | 单请求净收益 |
|---|------|-----------------|-----------------|-------------|
| 1 | `Params` 快照 → 惰性读取 | 构造 1399ns/2128B/17allocs；典型使用（读 1 path+1 query+1 header）共 1432ns | 构造 ~0；典型使用 300ns/432B/4allocs | **~1.1µs, ~1.7KB, 13 allocs** |
| 2 | `newInputTarget`+`inputFromTarget` 反射双拷贝 → 注册期预编译构造 | 743ns/2112B/3allocs（struct 被 reflect 分配+拷贝两次） | 一次 struct 分配，~0 开销 | **~740ns, ~2.1KB** |
| 3 | 每请求 `codecMgr.Resolve` → 注册期存入闭包 | 63ns/48B/1alloc | 0 | ~63ns（顺手改） |
| 4 | envelope 路径 `Negotiate` 缓存 | 135ns/112B/4allocs | 首次后 ~0 | 仅 envelope 路径受益 |

**实测后撤回的原有结论：**
- ~~"validator 无条件反射是开销"~~ —— 实测 playground validator 对无 tag 结构体仅 95ns/0 allocs（内部有类型缓存），**不值得做快速路径**，从计划中删除。
- 原文"empty-body 绑定 3.7µs/7.3KB"含每次循环内构造 `httptest.NewRequest` 的成本，真实的 Params 快照成本是 1.4µs/2.1KB —— 结论方向不变但数字修正。

**惰性 Params 的代价（必须写进文档的语义变化）：**
- Params 由"不可变快照"变为"handler 存活期内的视图"，handler 返回后不得留存；跨 goroutine 留存需求提供显式 `Detach()`（执行现在这种深拷贝，成本由 1% 的需要者支付）。
- `Query()` 首次访问才 parse 并缓存在视图内（`r.URL.Query()` 每次调用都重新分配，实测 235ns/432B，必须缓存）。
- 公开方法签名 `Path/Query/Header/Cookie/ClientIP` 全部不变，**对使用者是纯内部优化**。

**POST 路径补充**：含 JSON body 的整链实测 6.1µs/10.2KB/38allocs，其中 body 解码（json.Decoder ~1.7µs）不可避免，其余优化项同样适用，预计可压至 ~3µs。

**基准修正**：recorder 移出循环、`BenchmarkFullServer` 增加 in-process 变体（当前 518µs/op 是 Windows 环回 TCP 主导，测不出框架差异）、README/bench 报告注明 raw-handler 与 typed 路径分别的数字。

### P2 — 能力补齐

1. **参数获取保持 `Params` 命令式 API 为主**（设计已在 gserver DESIGN_BINDING.md 时代探索过 `bind` tag 方案并被有意放弃；简单场景下 `req.Query("page")` 比定义带 tag 的结构体字段更直接）。可选补充：`QueryInt(key) (int, error)` / `QueryIntDefault(key, def)` 等少量类型化便捷方法，消掉手写 `strconv` 样板，不引入 tag 体系。
2. **路由语义**：405 + `Allow`、GET 自动 HEAD、`WithTrailingSlashPolicy(Redirect|Strict)`、`ANY` 默认剔除 CONNECT/TRACE（另给 `ALL`）。
3. **错误链路**：无 envelope 时错误也走 codec 输出 `{code,message}`；`Recoverer` 记录堆栈；`Timeout` 超时写 504。
4. **CORS 修正**：不匹配 origin 不下发头、加 `Vary: Origin`、拒绝 credentials+`*`。
5. **生态集成**：gcompress 压缩中间件、gotel tracing 中间件、gretry 客户端重试、OpenAPI UI 挂载（`WithOpenAPIUI("/docs")`）。
6. **客户端补全**：`WithHTTPClient/WithTransport` 选项、流式 `Response.RawBody()`、SSE 重连与 Last-Event-ID。

### 验收基准（P1 完成后，in-process，对照 perf_probe 实测原型）

```
典型 GET 整链（path+query+header 各读一次）   ≤ 800 ns/op   ≤ 1 KB/op   ≤ 10 allocs/op   （当前 3675ns/5445B/29）
POST + JSON body 整链                        ≤ 3.5 µs/op   ≤ 4 KB/op                     （当前 6103ns/10.2KB/38）
Params 构造（不读任何值）                     ~0（惰性视图，无分配）                       （当前 1399ns/2128B/17）
raw handler 地板保持                          ~282 ns/op 不回归
```
