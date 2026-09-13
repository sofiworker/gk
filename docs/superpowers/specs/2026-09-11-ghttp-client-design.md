# ghttp Client 设计（调研 + 方案）

> 日期：2026-09-11
> 状态：**设计待评审**（本文只做调研与设计，不含实现）
> 范围：新增 ghttp **client** 侧；不改动 server 侧既有 API
> 参考实现基准：`go-resty/resty` v2.17.2 与 v3.0.0-rc.4（源码级调研）
>
> **配套文档**（同目录，本文是决策与方案，它们是证据底稿）：
> - `2026-09-11-ghttp-client-nethttp-compat-research.md` —— net/http 兼容边界与陷阱（495 行）
> - `2026-09-11-ghttp-client-generics-research.md` —— Go 泛型可行性、限制与社区实践（352 行）

---

## 0. 结论摘要（TL;DR）

| 待决问题 | 结论 | 一句话理由 |
|---|---|---|
| **包布局** | 新增子包 `ghttp/client`（package `client`），共享内核下沉到 `ghttp/internal/*` | server 已占用 `Request`/`Response`/`Server`，同包必然命名冲突；子包又需要未导出符号，故用 internal 做同族共享 |
| **是否引入泛型** | **引入，但只是薄壳**：非泛型核心（Go 1.25 可用）+ 包级泛型函数（1.18+）+ 泛型方法糖（`//go:build go1.27`） | 与 server 侧 `typed.go` + `builder_go127.go` 的既有分层完全同构；泛型解决不了错误、状态码、重试、流式，只解决"解码目标类型" |
| **server 代码是否复用** | **不复用类型，复用内核**：严格 JSON/XML 解码、Content-Type 规范化、SSE 线格式、日志脱敏下沉为 internal；重试退避复用 `gretry` | 方向相反（server 解码请求/编码响应，client 编码请求/解码响应），直接复用类型是伪复用 |
| **是否兼容标准库 client/dialer** | **兼容是硬约束，不是加分项**：`RoundTripper` 为第一公民，可注入 `*http.Client`/`http.RoundTripper`/`*net.Dialer`/`DialContext`/`http.CookieJar`/`CheckRedirect`，并可将请求降级为 `*http.Request` | 纯 net/http 地基是仓库既定取向（见 `ghttp/doc.go`）；任何自研传输层都会破坏 HTTP/2、代理、连接池与生态中间件 |

一行 API 预览（Go 1.27）：

```go
c := client.New(client.WithBaseURL("https://api.example.com"), client.WithTimeout(5*time.Second))

var user User
err := c.R().SetQueryParam("id", "1").Into(ctx, &user) // sink 泛型，T 从 &user 推断
// 或非泛型：
resp, err := c.R().SetQueryParam("id", "1").Get("/users/1")
var u2 User
err = resp.JSON(&u2)
```

---

## 1. 问题与约束

### 1.1 需要回答的三个问题

用户提出的三个问题，本质上是三个**不可逆的架构决策**，必须在写第一行代码前定下来：

1. **泛型要不要引入、以什么形式引入？** —— 决定整个 API 的形状与最低 Go 版本策略。
2. **server 侧代码能不能复用？** —— 决定包边界，以及是否要把既有代码拆出共享层。
3. **是否兼容标准库的 `http.Client` / `net.Dialer`？** —— 决定库能不能嵌入既有工程（而不是要求用户改造基础设施）。

参考对象 `go-resty/resty` 是 Go 生态使用最广的 HTTP client 封装，但它是 2015 年的设计，**不能照抄**。本文的立场是：抄它的**功能覆盖面与调用手感**，改它的**语义默认值与类型模型**。

### 1.2 硬约束（来自仓库规范）

| 约束 | 出处 | 对 client 的影响 |
|---|---|---|
| 纯 net/http 地基，不引入 fasthttp/自研 TCP | `ghttp/doc.go` | client 必须建立在 `http.RoundTripper` 之上，不得自带连接池 |
| 能力层之间禁止互相 import；同族子包可单向引用 | `docs/superpowers/specs/2026-08-07-gk-module-dependency-policy.md` | `ghttp/client` 可 import `ghttp` 与 `ghttp/internal/*`；不得 import `gnet`/`glog`/`gsd` |
| 公共概念只实现一次：错误模型、重试退避 | 同上 §3.4 | 重试退避必须走 `gretry`，不得再写一份 backoff |
| 最低支持 Go 1.25（CI 矩阵），Go 1.27 有 preview job | `.github/workflows/go.yml` | 泛型方法只能出现在 `//go:build go1.27` 文件里 |
| 面向读者的文档双语分文件；godoc 中英双语 | `AGENTS.md` | 实现阶段的注释规范，本设计文档不适用 |
| 默认安全、显式优先 | `ghttp/doc.go`、生产化 spec | 非 2xx 默认算错误、响应体默认有上限、不静默重试不可重放的 body |
| lint 必须可用（含泛型文件） | `.golangci.yml`（启用 `staticcheck`） | **前置阻塞项**：golangci-lint v2.4.0 在 Go 1.27 下已失效（实测 9 个假阳性），需先升级到 ≥ v2.13.0 并确认 staticcheck 不因泛型方法 panic（golang/go#81188），否则新代码无法被 lint 覆盖 |

### 1.3 为什么不直接依赖 resty（"参考"与"采用"的区别）

既然 resty 成熟且稳定，一个自然的问题是：为什么不直接把 `resty.dev/v3` 作为 ghttp 的 client 实现？三个理由：

1. **语义默认值不可调和**：resty 的"4xx/5xx 不算错误"与 gk 的"安全默认、显式优先"取向正面冲突。若在其上包一层把非 2xx 转成错误，那层包装很快就会长成另一个 client——不如直接写那个 client。
2. **无法与 server 侧共享严格性**：ghttp server 在请求体解析上有大量收紧（拒绝 JSON 尾随内容、声明长度却截断判错、`*http.MaxBytesError` 分类）。client 若走 resty，同一条链路上的两端将使用两套宽松度不同的解析器，"服务端严格 / 客户端宽松"的不对称会制造出难以复现的互操作问题。
3. **依赖与风格**：gk 是"积木式"库集合，ghttp 目前**零内部依赖、零 HTTP client 依赖**。引入 resty 会让 ghttp 家族多一个第三方实现依赖，且其 `Middleware func(*Client, *Request) error` 模型与 `ghttp.Middleware func(next Handler) Handler` 的洋葱模型无法统一，用户需要学两套心智模型。

**结论**：参考 resty 的**功能覆盖面与调用手感**（§2.2.1），不采用其**类型模型与默认语义**（§2.2.2）。需要 resty 全部能力的用户仍然可以直接用 resty——gk 不试图取代它，只补齐"与 ghttp server 对称的那一层"。

---

## 2. 调研

> **调研基准的时效性说明**：`go-resty/resty` 的稳定线仍停在 **v2.17.2**（2026-02-14 发布）；GitHub 默认分支上的 v3 是 `resty.dev/v3@v3.0.0-rc.4+devrc5`，**尚未发布 v3.0.0 稳定版**。因此"抄 resty"实际上是"抄一个十年前的 v2 设计 + 一个仍在 RC 的 v3 改良"，两者都在本文中作为对照，不作为权威。

### 2.1 仓库现状盘点：哪些能复用，哪些不能

**结论先说**：ghttp 目前是一个**纯 server 包**，`go list` 显示它零内部依赖、零 `http.Client` 相关代码。能复用的是**算法内核**，不是类型。

| server 侧资产 | 位置 | client 能否复用 | 判断 |
|---|---|---|---|
| 严格 JSON 解码（拒绝尾随内容、截断检测、`*http.MaxBytesError` 分类） | `codec.go` `jsonCodec.Decode` | **能，且应当** | 这段逻辑有大量边界考量（`{"a":1}GARBAGE` 拒绝、声明长度却 EOF 判截断），重复实现必然漂移。需从 `*Request` 解耦 |
| 严格 XML 解码（同上 + 空白 CharData 处理） | `codec.go` `xmlCodec.Decode` | **能，同上** | 同上 |
| JSON/XML 编码 | `codec.go` `Encode` | 能，但价值低 | 编码只有一行 `json.NewEncoder(w).Encode(v)`，共享收益接近零 |
| `mediaType()` Content-Type 规范化 | `codec.go` | 能 | 10 行函数，但语义必须与 server 一致，否则 415 判定与 client 分派会漂移 |
| 表单/文本解码（`formCodec`/`textCodec`） | `body_decoders.go` | 部分 | client 侧表单是**编码**方向（`url.Values`→body），与 server 的反射绑定完全不同，只能共享 Content-Type 常量 |
| 参数绑定引擎 `BindPlan` + `fieldBinder` | `bind_plan.go`/`bind_value.go` | **不能直接复用** | 方向相反：server 是 `string → Go 值`（需解析、越界报 400）；client 是 `Go 值 → string`（需格式化、转义）。tag 语义可对齐，实现要另写 |
| 错误链与状态码分类 `classifyError`/`StatusCoder` | `error_chain.go` | 接口语义能复用 | `StatusCoder`（`HTTPStatus() int`）是一行接口，client 的错误类型实现同一契约即可，无需 import |
| `gerr` 互操作（`FromGerr`/`ToGerr`/`GerrStatus`） | **已不存在** | — | 依赖策略文档 §5.1 声称"已收敛"，但 ghttp 重写后这些函数全部消失（`go list` 零内部依赖）。**这是一个已登记但被重写抹掉的收敛项，client 落地时应一并补回** |
| 重试退避 | `gretry.NextDelay`/`gretry.Wait` | **必须复用** | 依赖策略硬性要求；`gretry.Do` 的 `func() error` 签名不适配 HTTP 的 `(resp, err)` 二元判定，只复用 `NextDelay`+`Wait` |
| 日志脱敏 `sanitizeLogToken` | `log_sanitize.go` | 能 | client 的 debug dump 同样会打印 `Authorization`/`Set-Cookie`/token，必须共享同一套脱敏规则 |
| SSE 写端 `SSEWriter`/`SSEMessage` | `sse.go` | 结构可对齐，实现要新写 | server 是"写事件"，client 是"解析事件流"；线格式（`data:`/`event:`/`id:`/`retry:`、空行分帧）是同一份规范，可共享编解码内核 |
| `Upload` 类型 | `upload.go` | 不能复用 | server 的 `Upload` 是"已落地的 multipart part"（含 `multipart.FileHeader`、临时文件生命周期），client 需要的是"上传源"（路径/`io.Reader`），同名不同物 |
| 中间件模型 `Middleware func(next Handler) Handler` | `middleware.go` | **应当对齐** | 洋葱式、注册期折叠、请求期零分配——client 用同一模型，用户心智不分裂 |
| `Server.Option` 的 `WithXxx` 风格 | `server.go` | 应当对齐 | 保持全仓库一致的配置手感 |

**由此得出包布局的第一条推论**：如果 client 与 server 同包，上面"能复用"的每一项都零成本；但会撞名。如果 client 独立成包，"能复用"的每一项都要经过 `internal` 或导出符号。这正是 §3 D1 要权衡的核心。

### 2.2 resty 调研：值得抄的与必须改的

调研对象：`github.com/go-resty/resty/v2` v2.17.2（`go 1.23.0`）与 v3 开发线 `3.0.0-rc.4+devrc5`（源码 clone 至 `/tmp/resty-investigate/{v2real,v3}`）。

#### 2.2.1 API 形状

```go
// Client 是可变、共享、并发安全（内部加锁）的配置容器
c := resty.New().SetBaseURL(..).SetHeader(..).SetTimeout(..).SetRetryCount(3)
// Request 由 Client.R() 派生，可变累积，fluent 返回 *Request
resp, err := c.R().
    SetQueryParam("page", "1").
    SetHeader("Accept", "application/json").
    SetBody(req).
    SetResult(&User{}).   // 非泛型：注册解码目标
    SetError(&APIError{}).// 非泛型：注册错误体目标
    Post("/users")
```

关键源码事实：

| 事实 | 位置 |
|---|---|
| `Client` 有 **40+ 个导出字段**，配置项与内部状态混在同一结构体 | `v2/client.go` `type Client struct` |
| `Client.R()` **不复制** client 级 header/query，请求期才合并；`v3` 的 `R()` 改为复制全部标量默认值 | `v2/client.go:444`、`v3/client.go:727` |
| `Request` 有 **30+ 个导出字段**（`Body`/`Result`/`Error` 都是 `interface{}`） | `v2/request.go` `type Request struct` |
| 结果是运行时反射：`SetResult(v)` 内部 `getPointer(v)` 取 `reflect.Type` | `v2/request.go:378` |
| `Response` 极薄：`{Request, RawResponse, body []byte, size, receivedAt}`，body **全量读入内存** | `v2/response.go` |
| 中间件签名 `func(*Client, *Request) error`，**不是洋葱链**，无法短路包装 | `v2/middleware.go` |
| Hook 分五类：`OnBeforeRequest`(中间件)/`OnSuccess`/`OnError`/`OnInvalid`/`OnPanic` | `v2/client.go:918-975` |
| 重试：`Backoff(fn, Retries/WaitTime/MaxWaitTime/RetryConditions/RetryHooks)`，body 重放靠 `getBodyCopy` 复制 `bodyBuf` 或 `io.ReadAll(RawRequest.Body)` 回填 | `v2/retry.go`、`v2/middleware.go:594` |
| dialer 集成只有一行：`func transportDialContext(d *net.Dialer) func(ctx,net,addr)(net.Conn,error) { return d.DialContext }` | `v3/transport_dial.go` |
| **v3 全线没有引入泛型**：全部源码里 `[T any]` 只出现一次，且在内部的 `circuit_breaker.go:157` | `grep -rn '\[T any\]' v3/*.go` |

最后一条特别值得注意：**resty 在有了 Go 泛型（1.18）之后的四五年里，v3 大版本重构仍然没有泛型化 API**。这是"成熟 HTTP client 不必泛型化"的有力反例，也是我们在 §3 D2 里选择"泛型只做薄壳"的旁证。

#### 2.2.2 resty 的公开设计缺陷（同一个坑不能踩两次）

1. **4xx/5xx 不算 `error`**。`Execute` 只在传输层失败时返回 err，HTTP 500 返回 `(resp, nil)`，用户必须记得 `resp.IsError()`。真实事故来源：把 500 的错误页当正常 JSON 解码，得到一个全零结构体继续跑。
   → **我们的取舍：非 2xx 默认返回错误**（错误携带 `*Response`，body 仍可读），并把"接受哪些状态码"变成显式配置。
2. **`SetResult`/`SetError` 靠反射 + `interface{}`**，类型错误只在运行时炸，且 `SetResult` 收到非指针时会尝试 `reflect.New` 包一层（隐蔽的类型不匹配）。
   → **我们的取舍：核心保留 `SetResult(any)` 兼容路径（运行时才知道类型的场景真实存在，如网关转发、CLI），泛型层作为类型安全的首选**。
3. **`Timeout` 是"整体超时"**，覆盖建连 + 重定向 + **读 body**，与 per-request context 语义重叠，重试时计时归属不清。
   → **我们的取舍：默认不设 `http.Client.Timeout`，改为分层显式超时**（Dial/TLS/ResponseHeader/整体 ctx），文档明确四层各自管什么。
4. **body 重放靠无差别 `io.ReadAll` 回填**（`getBodyCopy` 对 `RawRequest.Body` 全读），大 body 重试时内存放大到 2 倍以上；`io.Reader` 型 body 在重试后静默变成空。
   → **我们的取舍：优先 `GetBody`（标准库契约），其次 `io.Seeker`，再次可重建的内存 body；三者都不满足时，重试前就返回 `ErrBodyNotReplayable`，绝不静默重试一个空 body**。
5. **envelope 假设**（历史版本）：旧 ghttp 的 client 硬编码 `{code,msg,data}` 解包，而 server 默认不启用 envelope，同包的 client 与 server 默认行为对不上（记录于 `docs/ghttp-improvement.md`）。
   → **我们的取舍：client 对响应体格式零假设**。统一包装通过显式的泛型/`Decode` 目标类型表达，或经 `WithErrorDecoder` 注入服务端错误体结构。
6. **`Client`/`Request` 几十个导出字段**，配置与状态不分，`v2` 的字段直接可写（无锁保护），并发写即数据竞争。
   → **我们的取舍：字段全私有 + `WithXxx` 函数选项 + 少量只读访问器**。`Request` 明确文档化"单次请求内使用，不并发"。
7. **`Response.body` 只有一个 `[]byte`**，没有"未读流"模式（v3 补了 `Response.Body io.ReadCloser` 与 `IsRead`，是被用户逼出来的）。
   → **我们的取舍：响应体有两种模式**，默认限长读入内存，`WithStreamResponse` 走 `io.ReadCloser` 流式，二者互斥且显式。

### 2.3 竞品横向对比

（见 §2.3 表格，证据来自各库源码/文档，标注于表下）

| 维度 | resty v2/v3 | imroc/req v3 | dghubble/sling | hashicorp/go-retryablehttp | 本设计 |
|---|---|---|---|---|---|
| 构造 | `resty.New()` | `req.C()` / `req.New()` | `sling.New().Base(url)` | `retryablehttp.NewClient()` | `client.New(opts...)` |
| 链式风格 | 可变 fluent `*Request` | 可变 fluent `*Request` | 不可变 builder（每步返回新 `*Sling`） | `*retryablehttp.Request` | 可变 fluent `*Request`（文档化非并发） |
| 泛型 | 公开 API 无（v3 内部仅 `group[T any]` 一处） | 公开 API 无（v3 内部仅 `cloneSlice[T any]` 一处） | 无 | 无 | 薄壳三层（见 D2） |
| 标准库兼容 | `NewWithClient`/`NewWithDialer`/`SetTransport` | `req.Client.SetTransport`/`SetDial` | `DoWithClient` | 本身就是 `RoundTripper` 包装器 | 第一公民（见 D4） |
| 中间件 | `RequestMiddleware func(*Client,*Request) error` | `Middleware func(*Client,*Request) error` | 无 | 无（有 `CheckRetry`/`Backoff` 钩子） | 洋葱链 `func(next Handler) Handler` |
| 重试 | 内置 `Backoff` + 条件函数 | 内置 `Attempts`/`RetryCondition` | 无 | **核心卖点**（`CheckRetry`/`Backoff`/`RetryWaitMin/Max`） | 内置，退避复用 `gretry` |
| 非 2xx 语义 | 不算 error | 不算 error | 不算 error | 不算 error | **默认算 error**（可关） |
| 错误模型 | `(*Response, error)`，状态码靠 `resp` | 同 | 同 | `(*http.Response, error)` | `*client.Error` 实现 `StatusCoder`，`errors.As` 可取整个响应 |
| codec 扩展 | `AddContentTypeEncoder/Decoder` | 少量 | 无 | 无 | 按 Content-Type 注册 `Codec` |
| 响应体 | 全量内存 | 全量内存 | `*http.Response` 原样 | `*http.Response` 原样 | 默认限长内存 / 可选流式 |
| 并发安全 | Client 是，Request 否（v3 给 Request 加锁） | Client 是 | 不可变，天然安全 | Client 是 | Client 是，Request 文档化单次使用 |
| SSE | v3 有 `SSESource` | 有 | 无 | 无 | 规划 P2，事件流解析器 |

> 证据说明：resty 结论来自本机源码 `v2/`、`v3/`（路径与行号见 §2.2.1）；`go-retryablehttp` 的 `CheckRetry`/`Backoff` 是其 README 与 `client.go` 的公开契约；`sling` 的不可变 builder 来自其 `sling.go` 的 `New()`/`Base()`/`Path()` 返回新实例的签名。

**从对比中提炼的五条设计取向**：

1. **`RoundTripper` 是唯一正确的组合点**。`go-retryablehttp` 干脆把自己做成一个 `RoundTripper`（可以在别人的 `http.Client` 里用），这是最"标准库原生"的形态。我们的 client 也应当时刻能退化成一个 `RoundTripper`（见 D4 的 `Client.RoundTripper()`）。
2. **"HTTP 状态错误算不算 error" 是最大的语义分歧点**，全生态默认"不算"，而这是公认的易错源。我们必须显式做出选择并让默认值安全（选"算"），同时保留一键退回生态惯例的能力。
3. **竞品都没有在公开 API 上用泛型**（resty v3 全仓库仅内部 `group[T any]`，req v3 仅内部 `cloneSlice[T any]`，sling/retryablehttp/grequests 零泛型），说明泛型不是 HTTP client 的刚需；但 `SetResult(&v)` 的运行时反射确实让类型错误推迟到运行时。泛型薄壳可以吃掉这个痛点，而无需改变核心模型——**这使泛型成为 gk 的差异化点而非跟风**。
4. **不可重放 body 的处理，三家的做法差异极大，且只有一家做对了**：
   - `imroc/req` v3：`SetBody(io.Reader)` 把 reader 记为 `unReplayableBody`，一旦 `MaxRetries != 0 && unReplayableBody != nil` 就**在发送前直接报错拒绝**（`errRetryableWithUnReplayableBody`，`request.go:679/701`）；
   - `resty` v2：重试时对不可 seek 的 reader 报 `ErrReaderNotSeekable`（比静默好，但已经是"发完第一次"之后）；
   - `go-retryablehttp`：`io.ReadAll` 全量缓冲 body 以便重放，**无上限、内存放大**。
   → **本设计采纳 req 的立场**（发送前拒绝，见 D6），并补上"优先 `GetBody`/`Seeker` 复用标准库重放契约"这一层。
5. **链式 setter 无法返回 error，是 fluent API 的结构性缺陷**，各库只能事后补救（req 用 `r.error = errors.Join(...)` 累积到终结时吐出，`request.go:675-677`；resty 用 `invalidRequestError` 包装后中断链）。**本设计必须显式定义这条契约**（见 D7 的"错误契约"）。

> 证据说明：resty 结论来自本机源码 `v2/`、`v3/`（路径与行号见 §2.2.1）；req v3 的结论来自其 `request.go`/`middleware.go` 的 `unReplayableBody`、`errRetryableWithUnReplayableBody`、`cloneSlice[T any]` 等标识符；`go-retryablehttp` 的 `CheckRetry`/`Backoff` 来自其 README 与 `client.go`；`sling` 的不可变 builder 来自其 `New()`/`Base()`/`Path()` 返回新实例的签名。

**主流 SDK 的旁证**：`google/go-github` 的 `CheckResponse`（`github/github.go`）在 `200 <= code <= 299` 之外构造 `*ErrorResponse` 并返回，且**把错误 body 重新填回 `r.Body`**（`r.Body = io.NopCloser(bytes.NewBuffer(data))`）供调用方二次读取。k8s `client-go` 的 `Result.Error()` 同样是"非 2xx 即 error"。也就是说：**通用 client 库（resty/req/sling）倾向"4xx/5xx 不算错误"，而成熟的服务端 SDK 倾向"非 2xx 即错误"**。ghttp 的定位更接近后者，这为 D5 的默认值提供了正面依据；同时 `go-github` "错误 body 可二次读取"的做法也印证了本设计"错误里带得走整个响应"（D5）的必要性。

### 2.4 Go 泛型：能力边界与实测

#### 2.4.1 实测结论（本机 go1.27.1）

在 `/tmp/genericcheck` 实测了三种形态，**全部编译通过**：

```go
// ① 方法级类型参数（Go 1.27+）：T 只出现在返回值，必须显式实例化
func (c *Client) Get[T any](ctx context.Context, url string) (T, error) { ... }
c.Get[User](ctx, "/u")            // ✅ 合法

// ② 泛型方法返回泛型指针容器
func (c *Client) Do[T any](ctx context.Context, url string) (*Result[T], error) { ... }
r, _ := c.Do[User](ctx, "/u")     // ✅ 合法，r.Data 静态类型为 User

// ③ 泛型方法内再调用泛型方法（转发/包装）
v, err := c.Get[T](ctx, url)      // ✅ 合法
```

同时确认一条硬限制以及一条重要的推断能力：

- **硬限制**：类型参数**无法从返回值推断**。`c.Get[User](ctx, url)` 中的 `[User]` 不能省略——`Get(ctx, url)` 无法编译，因为 Go 的类型推断不看返回类型。
- **重要推断能力**：类型参数**可以从函数参数位置推断**。若泛型方法签名为 `Into[T any](ctx context.Context, dst *T) (*Response, error)`，调用 `Into(ctx, &user)` 时编译器从 `&user` 推断 `T = User`，**无需写 `[User]`**。这就是 sink 模式，它让泛型入口的书写成本降到了零方括号。

CI 侧已有先例：`.github/workflows/go.yml` 的 `preview` job 用 `GOTOOLCHAIN=go1.27rc2` 跑 `go vet`/`go test`，`ghttp/builder_go127.go` 是仓库里第一个吃泛型方法的文件（`//go:build go1.27`，链式注册 + 泛型终结方法）。

> **顺带发现（工具链风险，实测）**：本条与 client 无关，但**必须在动 client 代码前处理**，否则无法用 lint 验证新代码。
>
> 1. **preview job 跑的是 RC 不是正式版**：`.github/workflows/go.yml` 的 preview job 钉在 `GOTOOLCHAIN=go1.27rc2`，而 Go 1.27 已于 2026-08-19 正式发布（本机 go1.27.1）。注释里写着"发布后替换"，需要真的执行。
> 2. **golangci-lint v2.4.0 在 Go 1.27 下已失效（本机实测）**：对现有 `ghttp` 包执行 `golangci-lint run ./ghttp/` 报出 9 个**假阳性**，全部形如 `req.PostForm undefined (type *Request has no field or method PostForm)`、`req.URL undefined`——而 `ghttp.Request` 明明嵌入了 `*http.Request`（`ghttp/request.go:62-63`），字段/方法提升完全合法，同一目录 `go vet ./ghttp/` **exit 0 通过**。这是 unified export data 与旧版工具不兼容导致 typecheck 解析不了嵌入字段，必须升到 golangci-lint ≥ v2.13.0。
> 3. **升版后仍有 staticcheck 泛型方法的已知坑**：`golang/go#81188`（Brad Fitzpatrick / Tailscale，2026-08-28 开，Milestone Go1.28）报告 Go 1.27 的 unified export data 丢失方法顺序，导致 `staticcheck/sa4023` index out of range **直接 panic**。本仓库 `.golangci.yml` 的 `linters.enable` 含 `staticcheck`，因此在 P3 写泛型方法前，**必须先在临时分支用 v2.13.x + Go 1.27.1 跑通一次 lint**，确认不被阻断，再合并泛型代码。
>
> 换句话说：仓库已有的 `builder_go127.go` 跑在一条**当前无法被 lint 覆盖**的路径上（`//go:build go1.27` 文件在 1.25 工具链下不参与编译，在 1.27 工具链下 lint 又失效）。这是一个独立于 client 的既有技术债。

#### 2.4.4 泛型的代价边界（避免过度泛型化的证据）

本设计只用了 **1 个类型参数**（`Into[T]`）+ 1–2 个终结方法，属于"小规模泛型"。但必须记录大规模泛型化的真实代价，以防后续有人把泛型铺到整个 client：

- `golang/go#65605`（2024-02 开，至今 Open）：一个真实大型项目把 B-Tree 从 `interface{}` 迁移到泛型后，**CI 冷构建 6 分钟 → 16 分钟（+166%）、二进制 48 MB → 58 MB（+20%）、build cache 760 MB → 4.4 GB（+580%）**。
- 代价的主要项是**类型参数个数与实例化数量**，而不是"是否使用泛型"。我们的 `Into[T]` 实例化数量 = 响应类型数量级，且终结方法极少，因此评估为**低风险**；但这条证据也构成 §8"泛型不做核心"的量化依据。
- 附带确证：Go 团队自己承认泛型推断的错误信息不友好——`golang/go#60542`（griesemer 开，`NeedsFix`）、`#61685`（`BadErrorMessage`，Go1.28 里程碑，至今未修）。这也是把泛型限制在薄壳层的理由之一。
- `golang/go#79834`：用户希望类型能实现泛型方法签名以便接口断言，ianlancetaylor 回复 "That is correct. This is the compromise that was accepted"，14 分钟内关闭。**泛型方法永远无法接口化**（连 `Getter[string]` 也不行），与 §3 D2 第 6 条一致。

#### 2.4.3 sink 模式：最优雅的泛型入口

实测验证（本机 go1.27.1，`/tmp/genericcheck`）：

```go
// sink 模式：T 从 dst 推断，无需显式实例化
func (r *Request) Into[T any](dst *T) (*Response, error) { ... }
var user User
resp, err := r.Into(&user)  // ✅ T = User 自动推断，零类型参数书写
```

sink 模式的额外收益：
- **零类型参数书写**（`Into(&user)` 比 `Get[User](...)` 少一对方括号）；
- **`*T` 对指针/切片/map 一视同仁**（`var users []User; r.Into(&users)` 推断为 `[]User`）；
- **escape analysis 无额外分配**（sink 的目标指针逃逸到 heap 的是调用方已有的分配，不是泛型引入的加量）。

已有落地样本：`goforj/httpx` v2.0.1 的 `Get[Out any](url) (Out, error)` 证明了非 sink 形态的可行性；我们的 sink 形态是其进化版。

**决策**：泛型入口优先使用 sink 模式，返回式作为补充（见 D2 / §4.4）。

#### 2.4.2 泛型能解决什么、不能解决什么

| 关注点 | 泛型能否解决 | 说明 |
|---|---|---|
| 响应解码目标类型 | ✅ 能 | `Get[User]` 静态类型直达，编译期暴露类型错误 |
| 错误体结构类型 | ✅ 能 | 同上 |
| 列表/分页包装 | ✅ 能 | `Get[Page[User]]` |
| 状态码判定 | ❌ 不能 | 与类型无关，是运行时值 |
| 传输错误 vs HTTP 错误 | ❌ 不能 | 需要 `errors.As` 取结构 |
| 重试/退避/重放 | ❌ 不能 | 运行时策略 |
| 中间件、Hook | ❌ 不能 | 横切关注点 |
| 流式/上传/SSE | ❌ 不能 | 与 `io` 交互，与类型无关 |
| 运行时才知道目标类型（网关转发、CLI、动态 schema） | ❌ 不能，且**泛型会挡住这条路** | 泛型入口无法用变量做类型参数 |

最后一行是决定性的：**泛型入口不能替代非泛型入口**，只能叠加。这决定了 D2 的"三层"而非"替换"。

### 2.5 标准库兼容：边界与陷阱

封装 `net/http` 的库，成败几乎全在"有没有破坏标准库的既有契约"。以下为需要正面处理的点（证据来自本机 `GOROOT=/root/.local/share/mise/installs/go/1.27.1` 的源码）：

#### 2.5.1 `http.Client` 的可组合面

```go
type Client struct {
    Transport     RoundTripper          // 唯一真正的传输扩展点
    CheckRedirect func(req *Request, via []*Request) error
    Jar           CookieJar
    Timeout       time.Duration         // 覆盖：连接 + 重定向 + 读 body 的全过程
}
```

- **`Transport` 是必须暴露的注入点**。测试用 `httptest.Server` + 自定义 `RoundTripper`；生产用代理、mTLS、连接池调参，全都只能通过它。任何"内部 new 一个 `http.Client` 且不给出路"的封装都是不可用的。
- **`Timeout` 语义陷阱与 `knownRoundTripperImpl` 白名单**：`http.Client.Timeout` 是整条链路的墙钟上限，**包括读响应体**。把大文件下载放在设了 `Timeout` 的 client 上会在下载中途被砍。更隐蔽的是：`knownRoundTripperImpl` 白名单（`src/net/http/client.go:324-350`）只认 `*http.Transport` 与 H2 内置实现，**自定义 `RoundTripper` 会让 `Timeout` 退化为 `CancelRequest` + 一条常驻 goroutine**（`client.go:395-425`）。因此封装层**不应默认设置 `Client.Timeout`**，应改为由多层超时（见 2.5.3）与 per-request context 共同承担。大文件下载或有自定义 Transport 的场景下尤其不设它。
- **`CheckRedirect`**：默认最多 10 跳；返回 `http.ErrUseLastResponse` 表示"不跟随但不报错"；返回其他 error 时 **`resp` 与 `err` 会同时非 nil**（标准库唯一如此的情形，必须正确处理并关闭 body——否则 body 泄漏）。敏感头转发方面，标准库对同 host/subdomain 的跨 scheme 重定向**不剥离 Authorization/Cookie**（`client.go:594-599, 824-827`，`shouldCopyHeaderOnRedirect` + `isDomainOrSubdomain` 忽略端口），比 WHATWG 规范宽松。**我们的封装层默认策略应当比标准库更严**：scheme/host/port 任一变化即剥离敏感头。

#### 2.5.2 `Transport` 与连接池

`http.DefaultTransport` 的实际参数（`src/net/http/transport.go`）：

```go
var DefaultTransport RoundTripper = &Transport{
    Proxy: ProxyFromEnvironment,
    DialContext: defaultTransportDialContext(&net.Dialer{Timeout: 30s, KeepAlive: 30s}),
    ForceAttemptHTTP2:     true,
    MaxIdleConns:          100,
    IdleConnTimeout:       90 * time.Second,
    TLSHandshakeTimeout:   10 * time.Second,
    ExpectContinueTimeout: 1 * time.Second,
}
const DefaultMaxIdleConnsPerHost = 2   // 注意：每主机只有 2 条空闲连接
```

两条必须写进文档的结论：

1. **绝不修改 `http.DefaultTransport`**（它是全进程共享的全局变量，改它等于污染宿主程序）。默认应当是 `http.DefaultTransport.(*http.Transport).Clone()`。
2. **自定义 `DialContext`/`TLSClientConfig` 会静默禁用 HTTP/2**，除非显式 `ForceAttemptHTTP2: true`。这是"我加了自定义 dialer，结果 HTTP/2 没了"的经典事故。封装层要么保持 `ForceAttemptHTTP2: true`，要么在文档里明确告知。

#### 2.5.3 超时分层

| 层 | 字段 | 管什么 | 不管什么 |
|---|---|---|---|
| 拨号 | `net.Dialer.Timeout` | TCP 建连（含 DNS 解析） | 已建连后的读写 |
| TLS 握手 | `Transport.TLSHandshakeTimeout` | 握手 | 之后的一切 |
| 响应头 | `Transport.ResponseHeaderTimeout` | 请求写出到收到响应头 | 读 body |
| 整请求 | `context` deadline | 全链路，可 per-request | 不自动重试 |
| 总超时 | `http.Client.Timeout` | 全链路 + 读 body | 重试之间的等待不重置 |

封装层的正确姿势：**默认只设"合理且不伤大雅"的层（Dial/TLS/ResponseHeader），整请求超时交给 context；`Client.Timeout` 只在用户显式要求时设置**，并且明确它会影响 body 读取与下载。

#### 2.5.4 body 与连接复用

- `resp.Body` Close 时标准库的行为因 Go 版本而异，对我们的封装层意义重大：

  | Go 版本 | `body.Close()` 的 drain 行为 | 连接复用？ | 风险 |
  |---|---|---|---|
  | 1.25 / 1.26 | 同步 `io.Copy(io.Discard, body)`（`transfer.go:1020-1023` 的 default 分支），**完整读完所有剩余字节** | ✅ drain 完毕后复用 | 大 body（500MB 文件）不读而 Close 会**同步阻塞直到下载完**，是性能陷阱 |
  | 1.27 | 异步 goroutine drain，设上限 `maxPostCloseReadBytes = 256KiB` + `maxPostCloseReadTime = 50ms`（[go.dev/doc/go1.27](https://go.dev/doc/go1.27)） | 超限则放弃连接 | 存在已知并发死锁（golang/go#81404 closed，#81411 backport open） |

  CI 最低支持 Go 1.25，**封装层不能依赖任一版本的行为**。最佳实践是默认读完限长部分并及时 Close（D8 的`WithResponseBodyLimit`）；超过上限时调用 `Close()` 后不假定连接可复用，而是确认 `resp.Request.Close = true` 或由 Transport 自动收尾。

- 请求体可重放的前提是 `http.Request.GetBody` 非 nil。标准库只对 `*bytes.Buffer`/`*bytes.Reader`/`*strings.Reader` 自动设置 `GetBody`（`src/net/http/request.go:938-952`），其余 `io.Reader` 一律没有。重试与 307/308 重定向都依赖它（`request.go:1562`）。
- `Transport.DisableCompression` 为 false（默认）时，只要用户没自己设 `Accept-Encoding`，Transport 会自动加 `gzip` 并**透明解压**。**结论：client 不需要自己做 gzip 解压**，只需在用户显式设了 `Accept-Encoding` 时提供可选的手动解压（这时标准库不再代劳）。

#### 2.5.5 错误模型

- `http.Client.Do` 的错误一律是 `*url.Error`，可用 `errors.As` 取出并判断 `Timeout()`/`Temporary()`；底层网络错误在 `url.Error.Err` 里。
- 标准库**不把 4xx/5xx 当错误**。封装层若改变这一点，必须让新错误类型能被 `errors.As` 取出，并携带原始 `*http.Response`，否则用户拿不到错误详情。

---

## 3. 设计决策

### D1 包布局：`ghttp/client` 子包 + `ghttp/internal` 共享内核

**选项对比**

| 方案 | 复用成本 | 命名 | 二进制 | 文档 | 结论 |
|---|---|---|---|---|---|
| A. 同包 `ghttp`（新增 `client_*.go`） | 零成本，直接用未导出符号 | ❌ `Request`/`Response`/`Server`/`Client` 已被占用或语义冲突，只能造 `ClientRequest`/`ClientResponse` 这类丑名 | client 用户被迫链入整个 server（DCE 能去掉大部分） | 一个包里两套世界观，godoc 混乱 | 否 |
| B. 子包 `ghttp/client` | 需把共享内核下沉到 `ghttp/internal/*` | ✅ `client.Client`/`client.Request`/`client.Response` 干净 | 不 import 父包时零 server 代码 | 独立、可单独 README | **采用** |
| C. 顶层新包 `gclient` | 依赖策略禁止能力层互引 → 无法复用，只能复制 | ✅ | ✅ | ✅ | 否 |

**决策：方案 B**，并配套两条规则：

1. `ghttp/client` **不 import `ghttp` 父包**。所有需要共享的东西下沉到 `ghttp/internal/*`；`ghttp` 与 `ghttp/client` 各自 import internal。这样两个包可以独立演进，也不会形成"子包依赖父包"的隐性版本耦合。
2. 需要跨包共享的**导出名**用类型别名保留，不做重复定义：

   ```go
   // ghttp/internal/httperr
   type StatusCoder interface { error; HTTPStatus() int }

   // ghttp/error_chain.go
   type StatusCoder = httperr.StatusCoder   // 别名：两侧是同一个类型，不产生包依赖
   // ghttp/client/error.go
   type StatusCoder = httperr.StatusCoder
   ```

   别名（`=`）而非新类型定义，保证"用户在 server 侧写的 `StatusCoder` 断言代码在 client 侧原样可用"，且任一包的实现都自动满足另一侧的契约。

**理由**：命名冲突是硬伤。用户同时写 server 与 client（自测、内部服务互调）是常态，同包里出现"`ghttp.Response` 是 server 的响应、`ghttp.ClientResponse` 是 client 的响应"会持续制造误读；而 internal 下沉虽然多写一层，却把"哪些是真正共享的内核"变成了显式的、可审查的清单。

### D2 泛型策略：三层，核心非泛型，泛型只做薄壳

```
┌─ L2 泛型方法糖（//go:build go1.27）──────────────────────────┐
│  r.Into(&user)     r.As[User]()                               │  sink 主推、返回式辅助
├─ L1 包级泛型函数（Go 1.18+，无构建标签）─────────────────────┤
│  client.DecodeInto(resp, &user)                               │
├─ L0 非泛型核心（Go 1.25 起，全部功能都在这里）────────────────┤
│  resp, err := c.R().SetQueryParam(..).Get("/u"); resp.JSON(&v)│  唯一有状态的实现层
└─────────────────────────────────────────────────────────────┘
```

**sink 模式优先**：泛型入口优先使用 `T` 出现在参数位置（`Into[T any](dst *T)`），这样调用方写 `r.Into(&user)` 无需 `[User]`——因为 Go 编译器从 `&user` 自动推断 `T = User`。这一点已在本机 go1.27.1 实测验证（见 §2.4.3）。返回式 `As[T any]() (T, error)` 作为辅助，万一起点不可控（如需要单行表达式返回结果）时使用。

**理由**

1. **最低版本约束**：CI 矩阵要求 Go 1.25 可通过全部测试，泛型方法（1.27）不能出现在核心路径上。
2. **泛型的边界很清楚**（§2.4.2）：它只解决"解码目标类型"，而错误/状态码/重试/流式都与类型无关。把泛型做成核心会导致"泛型类型参数爆炸"，错误信息不可读。
3. **与 server 同构**：server 侧已经是"非泛型 `RawHandle` 逃生 + typed 入口为主"，并有 `builder_go127.go` 这一"非泛型链 + 泛型终结方法"的先例。client 沿用同一模式，用户学一次即可。
4. **resty v3 的反例**（§2.2.1）：成熟 client 无泛型也能服务海量用户，说明泛型不是刚需；但 `SetResult(&v)` 的反射确实丢类型安全，薄壳能补上这个短板且不改变模型。
5. **sink 模式抹平了书写成本**：`Into(&user)` 不比非泛型 `resp.JSON(&user)` 多任何语法负担，但获得了编译期的类型安全。
6. **泛型方法不能实现接口**（已验证：含泛型方法的方法集不满足任何接口，即使实例化后）。因此需要接口 mock（如测试、适配器）的唯一路径是非泛型层。我们的三层设计恰好使泛型只做薄壳、非泛型核心可接口抽象，两者不冲突。

**泛型层的三种形态（都能在 1.25 上跑 L1）：**

```go
// L1：包级泛型函数（Go 1.18+）
func DecodeInto[T any](resp *Response, dst *T) error   // sink 模式，T 可从 *T 推断
func GetInto[T any](ctx context.Context, c *Client, url string, dst *T, opts ...RequestOption) error
func DoInto[T any](ctx context.Context, c *Client, method, url string, dst *T, opts ...RequestOption) error

// 泛型结果容器（Go 1.18+）
type Result[T any] struct {
    Data     T
    Response *Response
}
```

```go
//go:build go1.27

// L2：泛型方法（sink 主推、返回式辅助）
func (r *Request) Into[T any](ctx context.Context, dst *T) (*Response, error)     // 主推
func (r *Request) As[T any]() (T, error)                                           // 返回式
func (c *Client) GetInto[T any](ctx context.Context, url string, dst *T, opts ...RequestOption) error
func (c *Client) PostInto[T any](ctx context.Context, url string, body any, dst *T, opts ...RequestOption) error
func (c *Client) DeleteInto[T any](ctx context.Context, url string, dst *T, opts ...RequestOption) error
```

### D3 复用边界：复用内核，不复用类型

**决策**：新建 `ghttp/internal/*` 三个共享包，仅收纳"重复实现必然漂移"的逻辑：

| internal 包 | 内容 | 服务谁 | 不收纳什么 |
|---|---|---|---|
| `ghttp/internal/codec` | 严格 JSON/XML 解码内核（`DecodeJSON(io.Reader, any) error`、`DecodeXML`）、`MediaType(string) string`、Content-Type 常量 | server 的 `jsonCodec`/`xmlCodec` 改为调用它；client 的解码路径直接用它 | 不收纳 `Request`/`Response` 相关的任何类型 |
| `ghttp/internal/logsafe` | `Token(string) string`（脱敏） | server 的 access log 与 client debug dump | 不收纳日志接口（各包自定义小接口） |
| `ghttp/internal/sse` | SSE 线格式：`WriteEvent(w, Event)` / `ReadEvent(*bufio.Reader) (Event, error)` | server 的 `SSEWriter`、client 的 SSE 消费 | 不收纳重连策略（client 侧策略另写） |
| `ghttp/internal/httperr` | `StatusCoder` 接口（`HTTPStatus() int`） | server 的 `statusErr` 分类、client 的 `*Error`；两侧经类型别名导出同一类型（见 D1） | 不收纳错误分类逻辑（方向相反）与具体错误类型 |

**不复用的部分及理由**：

- **参数绑定引擎**：方向相反（解码 vs 编码），强行做双向引擎会让两侧都变复杂。client 侧的参数编码**不引入反射**（见 D7 说明），只对齐 tag 名与分隔符语义。
- **错误类型**：server 的错误链服务于"把 handler 的 error 变成状态码"，client 的错误服务于"把状态码变成 error"。方向相反的两个映射共用一个类型只会两边别扭。但 client 的错误**实现同一个 `StatusCoder` 契约**（`HTTPStatus() int`），保证用户侧的判断代码在两侧通用。
- **重试**：复用 `gretry.NextDelay`/`gretry.Wait`（依赖策略要求），但 HTTP 特有的"哪些状态码该重试""body 能否重放"留在 client。

### D4 标准库兼容：兼容是硬约束

**决策**：client 的每一层都必须能被标准库替换或注入。

```go
// 注入点（构造期）
WithHTTPClient(hc *http.Client)        // 完全接管；此后 client 不再覆盖 Timeout/Transport/Jar/CheckRedirect
WithTransport(rt http.RoundTripper)    // 只换传输层，保留我们对其余部分的编排
WithDialer(d *net.Dialer)              // 兼容标准库 dialer（内部转 Transport.DialContext）
WithDialContext(fn DialContextFunc)    // 任意自定义拨号（绑定本地地址、走代理、走自研网络栈）
WithCookieJar(jar http.CookieJar)
WithCheckRedirect(fn func(*http.Request, []*http.Request) error)
WithProxy(url string) / WithProxyFromEnvironment()

// 导出点（读取）
func (c *Client) HTTPClient() *http.Client
func (c *Client) Transport() http.RoundTripper
func (c *Client) RoundTripper() http.RoundTripper   // 把整个 client（含重试/中间件）退化成 RoundTripper

// 逃生口（双向互转）
func (r *Request) HTTPRequest(ctx context.Context) (*http.Request, error) // 我们的请求 → 标准库请求
func (c *Client) DoHTTP(ctx context.Context, req *http.Request) (*Response, error) // 标准库请求 → 走我们的编排
func (r *Response) Raw() *http.Response
```

**红线（写进 client 的 godoc 与 README）**：

1. 不修改 `http.DefaultTransport` / `http.DefaultClient`；默认 Transport 用 `DefaultTransport.Clone()`。
2. 不吞 body：任何提前返回路径都保证 `resp.Body` 被读完或关闭。
3. 不默认设置 `http.Client.Timeout`（避免悄悄砍掉大 body 下载）；由分层超时承担。
4. 不静默禁用 HTTP/2：注入自定义 dialer/TLS 时保持 `ForceAttemptHTTP2`。
5. 不自定义传输层：不做连接池、不解析 HTTP 报文、不自研 dialer；`ghttp` 不 import `gnet`，需要自研网络栈的用户通过 `WithDialContext` 注入。

**关于"是否复用标准库 client 和 dialer"的正面回答**：不是"兼容"，而是"**以标准库为唯一实现，自己只做编排**"。我们的 `Client` 本质上是 `http.Client` 的一层编排壳（中间件 → 重试 → 编解码 → 错误映射），随时可以退化成 `http.RoundTripper` 塞进别人的 `http.Client`。

### D5 错误模型：非 2xx 默认算错误，且错误里带得走整个响应

```go
type Error struct {
    Op         string          // "Get" / "Post" ...
    Method     string
    URL        string
    StatusCode int             // 0 表示传输层失败（没有收到响应）
    Status     string
    Attempts   int             // 在重试之后仍失败时的总尝试次数
    Err        error           // 底层错误（传输错误或错误体解码错误），支持 errors.Is/As 穿透
    Response   *Response       // 非 nil 时携带完整响应（body 已按策略保留）
}

func (e *Error) Error() string
func (e *Error) Unwrap() error
func (e *Error) HTTPStatus() int   // 实现 StatusCoder 契约（与 server 同一形状）
```

**决策要点**

- **默认 `2xx` 之外返回 `*Error`**，用户不再需要记得检查 `IsError()`；错误里的 `Response` 仍可读 body、header、状态码。配置项 `WithAcceptedStatus(...)` / `WithAllowAllStatus()` 可退回生态惯例（resty/req 的"4xx/5xx 也算成功"）。
- **错误体结构化是显式注入的**：`WithErrorDecoder(func(resp *Response, e *Error) error)`，默认只把 body 保留在 `Response` 里、不做任何格式假设（**这是对旧 ghttp client 硬编码 envelope 教训的直接回应**）。
- **与 `gerr` 的关系**：`*Error` 提供 `Kind()`/`Code()` 可选映射（P1 落地），并保证 `errors.As(err, &clientErr)` 与 `gerr.AsType[*client.Error]` 都能取出。是否让 client 直接依赖 `gerr` 待评审——依赖策略允许（基础契约层），但会引入一个内部依赖。
- **哨兵错误**：`ErrNoBaseURL`、`ErrBodyTooLarge`、`ErrBodyNotReplayable`、`ErrStreamConsumed`、`ErrResponseNotReadable`。

### D6 重试：条件与重放分离，退避复用 gretry

```go
type RetryPolicy struct {
    MaxRetries   int                                  // 默认 0（不重试）
    ShouldRetry  func(resp *Response, err error) bool // 默认见下
    RetryDelay   time.Duration                        // 交给 gretry.NextDelay
    MaxDelay     time.Duration
    Strategy     gretry.RetryStrategy                 // 复用基础契约层
    Jitter       gretry.JitterType
    RespectRetryAfter bool                            // 尊重服务端 Retry-After（秒数 + HTTP-date）
    OnRetry      func(attempt int, delay time.Duration, resp *Response, err error)
}
```

**默认重试条件**（保守，防误伤）：

- method 幂等（GET/HEAD/PUT/DELETE/OPTIONS/TRACE；POST/PATCH 需显式 `WithRetryNonIdempotent`）；
- 传输层错误，或状态码 ∈ {408, 429, 500, 502, 503, 504}；
- **body 可重放**：优先 `req.GetBody`，其次 `io.Seeker`（`Seek(0,0)`），再次可重建的内存 body；都不满足 → **在发出第一次请求之前**就返回 `ErrBodyNotReplayable`（附带解释），而不是发一个空 body 的第二次请求。
  这条立场有直接先例：`imroc/req` v3 在 `MaxRetries != 0 && unReplayableBody != nil` 时**拒绝发送**（`errRetryableWithUnReplayableBody`）；resty 则是发完第一次后才发现不可 seek；`go-retryablehttp` 干脆无上限全量缓冲。**"发之前就说不行"是对调用方最友好的形态**：错误发生在可归因的配置期，而不是在一次真实副作用之后。
- 全程 ctx 感知：`gretry.Wait(ctx, delay)` 在 ctx 取消时立即返回，不再发下一次。

**为什么只复用 `gretry.NextDelay` + `Wait` 而不是 `gretry.Do`**：`gretry.Do` 的 `func() error` 签名无法表达 HTTP 重试的判定输入 `(*Response, error)`（需要同时看状态码和传输错误），用闭包捕获会更别扭且容易出错。退避算法是真正的公共概念，重试判定是 HTTP 专属，边界划在这里最自然。

### D7 请求/响应模型：可变 fluent + 显式终结，字段全私有

```go
c := client.New(client.WithBaseURL(..), client.WithTimeout(..))

resp, err := c.R().                                   // 派生新请求
    SetHeader("Accept", "application/json").
    SetQueryParam("page", "1").
    SetJSON(createUserReq).
    SetResult(&User{}).                               // 非泛型路径
    Post("/users")
```

**决策要点**

- **`Client` 不可变配置 + 并发安全**：所有配置经 `WithXxx` 在构造/`Clone()` 时确定；运行期只读。`SetXxx` 式的运行期改配置**不提供**（resty 的 40 个导出字段是反例）。
- **`Request` 可变 fluent，明确"单次请求内使用"**：`c.R()` 每次返回新对象，不共享；`Request` 上的 `SetXxx` 返回 `*Request` 便于书写。**不承诺 `Request` 并发安全**，godoc 明说（v2 的 aliasing bug 与 v3 为此补锁都说明"半并发安全"最糟）。
- **两条终结路径**：`Send()`（用先前 `SetMethod`/`SetURL` 的设置）与 `Get(url)`/`Post(url)` 等便捷方法；`Execute(method, url)` 作为通用兜底。泛型层 `To[T]()` 是第三条终结路径。
- **参数编码不做反射**（与 server 的 `BindPlan` 不同）：client 侧参数来自本地代码（可信、类型已知），`SetQueryParam(k string, v any)` 用 `strconv`/`fmt` 直接格式化即可，无需 struct tag 绑定。**代价**：不支持"一个 struct 描述全部 query 参数"；**收益**：零反射、零注册计划、错误立即暴露。若后续确有需求，可加 `SetQueryParamsFrom(any)`（复用 `BindPlan` 的 tag 语义 + 反向编码器）作为 P2 增量，不作为首版核心。
- **`Client.R()` 的默认值复制**：client 级 header/query/cookie 在**请求期合并**（server 式，避免在每个 `R()` 上复制 map）；`Request` 自身的设置优先于 client 默认值。`Request` 的 header map 必须深拷贝，绝不与 client 共享底层 slice（v2 的 aliasing bug）。

#### D7.1 fluent setter 的错误契约（结构性问题的正面回答）

链式 setter 不能返回 `error`，但 client 的设置有真实的失败点（URL 非法、JSON 序列化失败、文件打不开、body 与 method 冲突）。候选方案：

| 方案 | 行为 | 评价 |
|---|---|---|
| A. 每个 setter 返回 `(*Request, error)` | 类型安全 | ❌ 链式书写彻底报废，`if err != nil` 到处都是 |
| B. 立即 panic | 错误不会丢 | ❌ 库代码 panic 违反 `AGENTS.md`（"avoid panics in library code"） |
| C. **延迟到终结期统一返回** | `SetXxx` 在**参数校验失败**时把错误记进 `Request.err`（保留首个），后续 setter 空转，终结方法 `Send()`/`To[T]()` 返回它 | ✅ 采纳 |
| D. `errors.Join` 累积全部错误 | 一次看到所有问题 | 采纳其精神：`Request.Err()` 可取累积错误，但终结时返回链上第一个（最接近根因） |

**采纳 C + D**：`Request` 内部持有 `err error`；`SetXxx` 只在**能立即判定失败**时记录（如 URL 解析、非法 method、`SetFile` 的文件不存在），**序列化等重活延迟到终结期**（这样 `SetJSON(v)` 是一次廉价赋值，与"延迟解码"的 server 侧哲学一致）。终结方法返回时若 `req.err != nil` 直接返回，不发出任何网络请求。额外提供 `func (r *Request) Err() error` 让用户在链中主动检查（可选）。

**这条契约必须写进 `doc.go`**：fluent 链上的错误不会丢，但也不会立即暴露——终结方法一定会返回它。

### D8 响应体策略：默认限长内存，流式显式开启

| 模式 | 触发 | 行为 | 适用 |
|---|---|---|---|
| 内存（默认） | — | 读完并缓存 `[]byte`，超过 `WithResponseBodyLimit`（默认 32 MiB）返回 `ErrBodyTooLarge` | 常规 API 调用，`Decode`/`String`/`Bytes` 都可用 |
| 流式 | `WithStreamResponse()` | `resp.Body` 为 `io.ReadCloser`，`Decode` 直接流式解到目标（JSON 流式解码），不缓存 | 大文件下载、SSE、长轮询、边读边处理 |
| 落盘 | `WithOutputFile(path)` | 直接 `io.Copy` 到文件，返回后 `resp.Bytes()` 为空 | 下载 |

**决策要点**

- **默认限长**与 resty（无限制）相反。理由：client 面对的服务端也可能被攻击、被劫持、或配置错误，返回 10 GB 错误页会把内存打爆；而安全默认是仓库既定取向。上限可经 `WithUnlimitedResponseBody()` 显式解除。
- **流式模式下 `Decode` 的语义**：JSON 流式解码最多消费一个值并拒绝尾随内容（复用 internal 内核）；**流式模式下调 `Bytes()`/`String()` 返回 `ErrStreamConsumed`**，而不是静默读一个已经被读过的流。
- **`Decode` 的 Content-Type 分派**：`resp.Decode(&v)` 按响应 `Content-Type` 选注册过的 decoder；未注册的类型返回明确错误（而非静默 JSON 尝试）。显式 `resp.JSON(&v)`/`resp.XML(&v)` 可强制指定。

### D9 中间件与 Hook：洋葱链，与 server 同构

```go
type Handler func(ctx context.Context, req *Request) (*Response, error)
type RequestMiddleware func(next Handler) Handler

type ResponseHandler func(ctx context.Context, resp *Response) (*Response, error)
type ResponseMiddleware func(next ResponseHandler) ResponseHandler
```

与 resty 的 `func(*Client, *Request) error` 相比，洋葱链的收益是实打实的：中间件可以在 `next` 前后各做一段（计时、注入 header、读取响应头、恢复 panic），也可以不调 `next` 直接短路（返回缓存响应、mock）。

阶段与 Hook 的完整顺序：

```
R() 构建
  → RequestMiddleware（洋葱，按注册顺序，可短路）
    → OnBeforeRequest（观察者，不可改流程）
      → [重试循环]
          发送 → httptrace 事件 → OnRetry(每次重试前)
      → ResponseMiddleware（洋葱，可改响应/触发重试）
        → OnAfterResponse（观察者）
          → 解码（用户显式调用 resp.Decode / 泛型 Result 自动解码）
            → OnSuccess / OnError（终态通知）
```

`OnPanic` 只在用户中间件 panic 时触发，且默认**继续 panic**（不吞掉），与 server 的 `Recovery` 中间件"显式选择才恢复"的取向一致。

### D10 性能预算：热路径零反射、零重复拷贝

server 侧的性能取向（零反射 codec、池化上下文、注册期折叠中间件）在 client 侧同样适用，但目标不是刷新基准，而是**不成为调用方的瓶颈**：

| 项 | 设计 | 理由 |
|---|---|---|
| 参数与 header 编码 | 无反射（`strconv`/`fmt`/`url.Values`） | 与 server 的参数绑定不同，client 侧类型本地已知 |
| 响应解码 | `json.Unmarshal(resp.body)` 直接解到用户目标，不再套一层 `bytes.Reader` | 内存模式 body 已在内存，再包 reader 只会多一层间接 |
| 流式解码 | `json.NewDecoder(resp.Body)`，边读边解 | 大响应不整块进内存 |
| `Response.Bytes()` | 返回内部切片（约定只读），不复制 | 与标准库 `io.ReadAll` 的语义差异需在 godoc 写明 |
| body 缓冲 | `sync.Pool` 复用 `bytes.Buffer`（resty 同款做法） | 高频小请求下省掉每次分配的峰值 |
| 中间件链 | 注册期折叠成单向调用链，请求期零组装 | 与 `ghttp.Server` 完全一致 |
| 泛型层 | 仅做转发与类型包装，不引入额外分配 | `Result[T]` 是值类型，无堆分配 |

**验证**：`BenchmarkClientGetJSON` 对照 `http.Client` + `json.NewDecoder` 手写基线，要求 ≤ 1.2 倍耗时且分配数不高于基线 + 2（详见 §9）。**这条预算是设计约束而非营销数字**：若实现超出，先砍功能（如 debug 钩子），不砍安全性。

---

## 4. API 设计（签名级）

> 以下为设计稿，落地时以 godoc 双语注释为准；`Xxx` 表示待评审确认的具体名字。

### 4.0 功能清单（"需要哪些功能"的直接回答）

| # | 功能 | 优先级 | 关键取舍 |
|---|---|---|---|
| 1 | Client 构造与配置（Option + `WithXxx`） | P0 | 字段全私有；运行期不可变 |
| 2 | base URL / 路径拼接 / path 参数 | P0 | 不做反射 tag 绑定 |
| 3 | query 参数（含多值与转义） | P0 | `SetQueryParam(k, any)` + `SetQueryString(raw)` |
| 4 | header（请求级覆盖 client 级） | P0 | header map 深拷贝，杜绝 aliasing |
| 5 | 请求体编解码（JSON/XML/Form/Text/multipart） | P0 | 按 Content-Type 注册 codec，可扩展 |
| 6 | 响应解码（Content-Type 分派 + 显式 JSON/XML） | P0 | 复用 server 的严格解码内核 |
| 7 | 非泛型核心 API（fluent + `SetResult`） | P0 | 运行时才知道类型时的唯一路径 |
| 8 | 错误模型（非 2xx 默认算错误、携带响应） | P0 | `*client.Error` 实现 `StatusCoder` |
| 9 | 中间件（请求洋葱链 + 响应洋葱链） | P0 | 与 `ghttp.Middleware` 同构 |
| 10 | Hook（before/after/success/error/retry/panic） | P0 | 观察者语义，不改流程 |
| 11 | 标准库注入（Client/Transport/Dialer/DialContext/Jar/CheckRedirect） | P0 | 兼容是硬约束（D4） |
| 12 | 超时分层（Dial/TLS/ResponseHeader/整体/ctx） | P0 | 默认不设 `http.Client.Timeout` |
| 13 | 认证（Basic/Bearer/自定义 scheme） | P1 | token 不进 debug 日志（复用脱敏内核） |
| 14 | 重试（条件 + 退避 + Retry-After + body 重放判定） | P1 | 退避复用 `gretry`（D6） |
| 15 | 泛型层 L1（sink 模式 `Into[T](dst)` + 包级函数） | P1 | Go 1.18+，T 从 `*T` 自动推断，零书写负担 |
| 16 | 上传（multipart 文件/流/进度） | P1 | 流式，不整块进内存 |
| 17 | 下载（限长/流式/落盘） | P1 | 三条路径互斥且显式（D8） |
| 18 | Debug dump + 脱敏 + trace（httptrace） | P1 | body 打印有上限 |
| 19 | Cookie jar | P1 | 直接复用 `http.CookieJar` |
| 20 | 代理（显式 URL / 环境变量） | P1 | `ProxyFromEnvironment` 的缓存陷阱写进文档 |
| 21 | SSE 消费（事件流解析 + Last-Event-ID + 自动重连） | P2 | 与 server 的 `SSEWriter` 共享线格式内核 |
| 22 | 连接池调参（MaxIdleConnsPerHost 等）+ `CloseIdleConnections` | P2 | 只暴露参数与转发，不自研池 |
| 23 | 泛型层 L2（`r.Into[T]`、`r.As[T]`，Go 1.27 方法） | P3 | `//go:build go1.27`，sink 模式主推 |
| 24 | 熔断 / 限流 / 负载均衡 | 范围外 | 交给适配层或用户中间件 |
| 25 | WebSocket 客户端 | 范围外（待议） | `gorilla/websocket` 的 Dialer 已够用 |

### 4.1 构造与配置

```go
package client

func New(opts ...Option) *Client
func NewWithHTTPClient(hc *http.Client, opts ...Option) *Client // 与 resty.NewWithClient 对齐
func (c *Client) Clone(opts ...Option) *Client                  // 派生变体，共享底层 Transport
func (c *Client) CloseIdleConnections()                         // 转发到底层 Transport（若有该方法）

// 传输与网络
func WithBaseURL(u string) Option
func WithHTTPClient(hc *http.Client) Option
func WithTransport(rt http.RoundTripper) Option
func WithDialer(d *net.Dialer) Option
func WithDialContext(fn func(ctx context.Context, network, addr string) (net.Conn, error)) Option
func WithProxy(proxyURL string) Option
func WithProxyFromEnvironment() Option
func WithTLSConfig(cfg *tls.Config) Option

// 超时（分层）
func WithTimeout(d time.Duration) Option            // = http.Client.Timeout，文档警告含 body 读取
func WithDialTimeout(d time.Duration) Option
func WithTLSHandshakeTimeout(d time.Duration) Option
func WithResponseHeaderTimeout(d time.Duration) Option
func WithIdleConnTimeout(d time.Duration) Option

// 行为
func WithHeader(key, value string) Option
func WithHeaderAuthorizationKey(key string) Option  // 默认 "Authorization"
func WithQueryParam(key, value string) Option
func WithUserAgent(ua string) Option
func WithCookieJar(jar http.CookieJar) Option
func WithCheckRedirect(fn func(req *http.Request, via []*http.Request) error) Option
func WithDisableRedirects() Option                  // 内部即 CheckRedirect 返回 ErrUseLastResponse

// 编解码与错误
func WithCodec(contentType string, codec Codec) Option
func WithResponseBodyLimit(n int64) Option
func WithUnlimitedResponseBody() Option
func WithAcceptedStatus(codes ...int) Option
func WithAllowAllStatus() Option
func WithErrorDecoder(fn func(resp *Response, e *Error) error) Option

// 策略
func WithRetry(policy RetryPolicy) Option
func WithRequestMiddleware(mw ...RequestMiddleware) Option
func WithResponseMiddleware(mw ...ResponseMiddleware) Option
func WithLogger(l Logger) Option
func WithDebug(enabled bool) Option
func WithDebugBodyLimit(n int) Option
func WithTrace(enabled bool) Option
```

### 4.2 请求

```go
func (c *Client) R() *Request
func (c *Client) NewRequest() *Request // R 的别名，与 resty 对齐

func (r *Request) SetContext(ctx context.Context) *Request
func (r *Request) SetMethod(method string) *Request
func (r *Request) SetURL(url string) *Request
func (r *Request) SetPathParam(name, value string) *Request
func (r *Request) SetPathParams(params map[string]string) *Request
func (r *Request) SetHeader(key, value string) *Request
func (r *Request) SetHeaders(headers map[string]string) *Request
func (r *Request) AddHeader(key, value string) *Request
func (r *Request) SetQueryParam(key string, value any) *Request
func (r *Request) SetQueryParams(params map[string]any) *Request
func (r *Request) SetQueryString(raw string) *Request // 原样拼接到 RawQuery

// 请求体
func (r *Request) SetBody(v any) *Request                                  // 按 Content-Type 选 codec
func (r *Request) SetJSON(v any) *Request
func (r *Request) SetXML(v any) *Request
func (r *Request) SetForm(values url.Values) *Request
func (r *Request) SetBodyReader(rc io.Reader, contentType string) *Request // 流式上传
func (r *Request) SetContentLength(n int64) *Request
func (r *Request) SetMultipartFormData(fields map[string]string) *Request
func (r *Request) SetFile(field, path string) *Request
func (r *Request) SetFileReader(field, filename string, rc io.Reader) *Request
func (r *Request) SetMultipartField(field, filename, contentType string, rc io.Reader) *Request

// 认证
func (r *Request) SetBasicAuth(user, pass string) *Request
func (r *Request) SetAuthToken(token string) *Request      // 默认 scheme "Bearer"
func (r *Request) SetAuthScheme(scheme string) *Request
func (r *Request) SetHeaderAuthorizationKey(key string) *Request

// 结果与错误目标（非泛型路径）
func (r *Request) SetResult(v any) *Request
func (r *Request) SetError(v any) *Request

// 每请求覆盖
func (r *Request) SetRetry(policy RetryPolicy) *Request
func (r *Request) SetTimeout(d time.Duration) *Request
func (r *Request) SetResponseBodyLimit(n int64) *Request
func (r *Request) SetStreamResponse() *Request
func (r *Request) SetOutputFile(path string) *Request
func (r *Request) OnUploadProgress(fn func(sent, total int64)) *Request
func (r *Request) OnDownloadProgress(fn func(read, total int64)) *Request

// 终结
func (r *Request) Err() error                     // 链上累积的首个错误（可选主动检查）
func (r *Request) Send() (*Response, error)
func (r *Request) Execute(method, url string) (*Response, error)
func (r *Request) Get(url string) (*Response, error)   // POST/Put/Patch/Delete/Head/Options 同形
```

### 4.3 响应

```go
func (r *Response) StatusCode() int
func (r *Response) Status() string
func (r *Response) Header() http.Header
func (r *Response) Cookies() []*http.Cookie
func (r *Response) Bytes() ([]byte, error)
func (r *Response) String() (string, error)
func (r *Response) Decode(v any) error       // 按 Content-Type 分派
func (r *Response) JSON(v any) error
func (r *Response) XML(v any) error
func (r *Response) IsSuccess() bool          // 2xx
func (r *Response) IsStream() bool
func (r *Response) Stream() (io.ReadCloser, error)
func (r *Response) SaveToFile(path string) error
func (r *Response) Result() any              // 对应 SetResult 的目标（已填充）
func (r *Response) ErrorBody() any           // 对应 SetError 的目标
func (r *Response) Raw() *http.Response
func (r *Response) Request() *Request
func (r *Response) Duration() time.Duration
func (r *Response) Attempts() int
func (r *Response) Traces() TraceInfo        // WithTrace 开启后可用
func (r *Response) Close() error             // 流式模式下必须调用
```

### 4.4 泛型层

```go
// L1：Go 1.18+，无构建标签

// sink 模式（主推）：T 从 *T 自动推断，无需显式 [T]
func DecodeInto[T any](resp *Response, dst *T) error
func GetInto[T any](ctx context.Context, c *Client, url string, dst *T, opts ...RequestOption) error
func PostInto[T any](ctx context.Context, c *Client, url string, body any, dst *T, opts ...RequestOption) error
func DoInto[T any](ctx context.Context, c *Client, method, url string, dst *T, opts ...RequestOption) error

// 泛型结果容器
type Result[T any] struct {
    Data     T
    Response *Response
}

// 辅助：返回式（T 不可推断，需显式 [T]），用于单行表达式场景
func DecodeAs[T any](resp *Response) (T, error)
```

```go
//go:build go1.27

// L2：泛型方法

// sink 模式（主推）：r.Into(&user) — 无方括号
func (r *Request) Into[T any](ctx context.Context, dst *T) (*Response, error)
// 返回式（辅助）：result, err := r.As[User]()
func (r *Request) As[T any]() (T, error)

func (c *Client) GetInto[T any](ctx context.Context, url string, dst *T, opts ...RequestOption) error
func (c *Client) PostInto[T any](ctx context.Context, url string, body any, dst *T, opts ...RequestOption) error
func (c *Client) PutInto[T any](ctx context.Context, url string, body any, dst *T, opts ...RequestOption) error
func (c *Client) PatchInto[T any](ctx context.Context, url string, body any, dst *T, opts ...RequestOption) error
func (c *Client) DeleteInto[T any](ctx context.Context, url string, dst *T, opts ...RequestOption) error
```

`RequestOption` 是与 `Option` 分开的**每请求覆盖**类型（`WithRequestHeader`、`WithRequestQuery`、`WithRequestTimeout`），使泛型入口也能带少量参数而无需先建 `Request`。

### 4.5 中间件与 Hook

```go
type Handler func(ctx context.Context, req *Request) (*Response, error)
type RequestMiddleware func(next Handler) Handler
type ResponseHandler func(ctx context.Context, resp *Response) (*Response, error)
type ResponseMiddleware func(next ResponseHandler) ResponseHandler

func (c *Client) Use(mw ...RequestMiddleware) *Client          // 语义与 ghttp.Server.Use 对齐
func (c *Client) UseResponse(mw ...ResponseMiddleware) *Client
func (c *Client) OnBeforeRequest(fn func(ctx context.Context, req *Request) error) *Client
func (c *Client) OnAfterResponse(fn func(ctx context.Context, resp *Response) error) *Client
func (c *Client) OnSuccess(fn func(ctx context.Context, resp *Response)) *Client
func (c *Client) OnError(fn func(ctx context.Context, err error, resp *Response)) *Client
func (c *Client) OnRetry(fn func(attempt int, delay time.Duration, resp *Response, err error)) *Client
func (c *Client) OnPanic(fn func(ctx context.Context, recovered any)) *Client
```

**横切能力一律走中间件，核心不内置**：W3C `traceparent` 传播、`Idempotency-Key`、OAuth2 取 token、AWS SigV4 签名、请求 ID 注入、metrics 采集，全部是 `RequestMiddleware` 的形态（必要时配合 `ghttp/adapters/gotel` 这类适配子包），核心只提供机制不提供策略。这样做的代价是"开箱即用度"略低，收益是核心零策略负担、依赖面为零。

### 4.6 上传、下载、SSE

```go
// 上传：见 4.2 的 SetFile/SetFileReader/SetMultipartField/OnUploadProgress
// 下载：SetOutputFile / Response.SaveToFile / 流式 Response.Stream()

// SSE（P2）
func (c *Client) SSE(ctx context.Context, url string, opts ...RequestOption) (*SSEStream, error)
type SSEEvent struct{ ID, Name, Data string }
type SSEStream struct{ ... }
func (s *SSEStream) Next() (SSEEvent, error)        // io.EOF 表示流正常结束
func (s *SSEStream) LastEventID() string
func (s *SSEStream) Close() error
func (c *Client) OnSSEEvent(fn func(SSEEvent) error) *Client // 回调式，自动重连
```

### 4.7 Debug 与 Trace

```go
type Logger interface { // 与 server 的 Logger 同形（小接口，用户注入 glog 适配器）
    Debugf(format string, args ...any)
    Infof(format string, args ...any)
    Warnf(format string, args ...any)
    Errorf(format string, args ...any)
}
type DebugLogFormatter interface{ Format(dump Dump) string }
type TraceInfo struct { // 基于 net/http/httptrace
    DNSLookup, Connect, TLSHandshake, WroteRequest, TTFB, Total time.Duration
    RemoteAddr, ReusedConn string
}
```

---

## 5. 包布局与文件规划

```
ghttp/
├── internal/
│   ├── codec/      # 严格 JSON/XML 解码内核 + MediaType 规范化 + Content-Type 常量
│   ├── logsafe/    # 日志脱敏（token/cookie/authorization）
│   └── sse/        # SSE 线格式编解码（Event + ReadEvent/WriteEvent）
├── client/
│   ├── doc.go              # 包文档（中英双语）
│   ├── client.go           # Client、Option、Clone、Use
│   ├── transport.go        # Transport 构造、dialer/proxy/TLS/超时分层
│   ├── request.go          # Request、SetXxx、终结方法
│   ├── response.go         # Response、解码、流式、SaveToFile
│   ├── execute.go          # 执行引擎：中间件链 → 重试循环 → 错误映射
│   ├── retry.go            # RetryPolicy + 条件判定 + body 重放（复用 gretry 退避）
│   ├── error.go            # Error、StatusCoder 契约、哨兵错误
│   ├── codec.go            # Codec 接口 + 内置 JSON/XML/Form/Text 注册表
│   ├── middleware.go       # 中间件与 Hook 类型
│   ├── body.go             # body 源抽象（内存/Reader/Seeker/GetBody 可重放判定）
│   ├── multipart.go        # multipart 构造
│   ├── sse.go              # SSE 事件流消费（P2）
│   ├── debug.go            # dump、trace
│   ├── generic.go          # L1 包级泛型函数 + Result[T]
│   ├── generic_go127.go    # //go:build go1.27：L2 泛型方法
│   └── README.md / README.en.md
└── （server 侧既有文件，仅把 codec.go/log_sanitize.go/sse.go 的实现改为调用 internal）
```

**server 侧的改动范围**：仅"把内核换成 internal 调用"，不改任何导出 API、不改行为。改动由现有测试（`codec_test.go`、`codec_strict_test.go`、`log_sanitize_test.go`、`sse_test.go`）守护。

---

## 6. 与 server 的对称性对照

| 概念 | server（既有） | client（本设计） |
|---|---|---|
| 顶层类型 | `ghttp.Server`（`ghttp.New()`） | `client.Client`（`client.New()`） |
| 配置 | `Option` + `WithXxx` | 同 |
| 中间件 | `Middleware func(next Handler) Handler`（洋葱、注册期折叠） | 同形状（`RequestMiddleware`） |
| 入口分层 | typed 入口为主 + `RawHandle` 逃生 | fluent 核心 + 泛型薄壳（1.27） |
| codec 扩展 | `RequestDecoder`/`ResponseEncoder`/`Codec`（按 Content-Type） | 同概念，方向相反（`Encoder`/`Decoder`） |
| 严格性 | 严格 Content-Type 校验、拒绝尾随 JSON、限 body | 同（共享 internal 内核） |
| 错误 | `StatusCoder` + 哨兵 + 统一错误体 | `*client.Error` 实现 `StatusCoder` + 哨兵 |
| SSE | `SSEWriter`（写事件） | `SSEStream`（解析事件流），共享线格式内核 |
| 可观测 | `Logger`/`AccessLog`/`MetricsRegistry` | `Logger`/`Debug dump`/`TraceInfo` |
| 逃生口 | `RawHandle`、`http.Handler` 互操作 | `RoundTripper` 化、`*http.Request`/`*http.Response` 互转 |

---

## 7. 分期落地计划

| 期 | 内容 | 验收 |
|---|---|---|
| **P-1 工具链修复**（前置，与 client 解耦） | `golangci-lint` 升到 ≥ v2.13.0（当前 v2.4.0 在 Go 1.27 下 typecheck 失效，本机实测 9 个假阳性）；CI preview job 的 `GOTOOLCHAIN` 从 `go1.27rc2` 改为 `go1.27.1`；修复后确认 `golangci-lint run ./ghttp/` 干净，并验证 staticcheck 不因泛型方法 panic（golang/go#81188） | `make lint` 在 Go 1.27.1 下全绿且无假阳性；`builder_go127.go` 首次被 lint 覆盖 |
| **P0 骨架** | `internal/codec` 抽取 + server 侧改为调用（行为不变）；`client` 的 Client/Request/Response/执行引擎/错误/中间件与 Hook/内置 codec/cookie jar/分层超时；标准库注入点全套 | server 既有测试全绿；client 能对一个 `httptest.Server` 完成 GET/POST JSON 往返，非 2xx 返回 `*client.Error`；`WithHTTPClient`/`WithDialer`/`RoundTripper()` 三个注入点各有测试 |
| **P1 泛型与重试** | L1 sink 泛型函数（`DecodeInto`/`GetInto`/`DoInto`）+ `Result[T]`；`RetryPolicy`（复用 gretry）；认证；multipart 上传；debug dump；`gerr` 互操作补回 | 重试表驱动测试（幂等判定/Retry-After/body 重放/ctx 取消）；泛型入口的类型安全用例（含类型不匹配的编译失败断言） |
| **P2 流式与 SSE** | 流式响应、下载到文件、`internal/sse` 抽取 + client `SSEStream`、进度回调、trace | 与 server 的 `SSEWriter` 端到端对测；大文件流式内存不随体积增长（基准证明） |
| **P3 泛型方法糖** | `generic_go127.go`（`r.Into[T]` sink 模式 + `r.As[T]` 返回式）；CI preview job 的 `GOTOOLCHAIN` 升到 go1.27.1；`golangci-lint` 升到 ≥ v2.13.0 | `go1.27` 构建下 vet/test 全绿；`GOTOOLCHAIN=go1.25.x` 下该文件不参与编译；**合并前用 v2.13.x 确认 staticcheck 不 panic**（golang/go#81188） |

---

## 8. 明确不做（范围外）

| 不做 | 理由 |
|---|---|
| 自研 Transport / 连接池 / dialer | 纯 net/http 地基是仓库取向；自定义网络栈经 `WithDialContext` 注入 |
| 熔断器、限流器核心 | 属独立能力；P3 之后以 `ghttp/adapters/*` 或用户中间件形式提供 |
| 负载均衡 / 多 base URL 轮询 | 与 `gsd` 组合应在适配层；核心只保留"可以换 BaseURL" |
| 强制 envelope / 统一响应包装 | 旧 ghttp client 的教训：对服务端格式零假设 |
| 默认无限响应体 | 安全默认优先 |
| HTTP/3、QUIC | 超出范围；需要时用户注入支持 HTTP/3 的 `RoundTripper` |
| WebSocket 客户端 | 已依赖 `gorilla/websocket`，用户可直接用其 Dialer；是否薄封装待 P3 评审 |
| 参数 struct tag 绑定（反射） | 首版不做；`SetQueryParams(map[string]any)` 足够，确有需求再作为增量 |

---

## 9. 验证计划

1. **互操作测试**：`httptest.Server` + 自定义 `RoundTripper` mock；`WithHTTPClient` 注入带自定义 `CheckRedirect`/`Jar` 的 client；`WithDialer(&net.Dialer{LocalAddr: ...})` 绑定本地地址的真实拨号；`Client.RoundTripper()` 塞进标准 `http.Client` 后发请求。
2. **契约测试**：body 必被关闭（用一个 `io.ReadCloser` 计数器断言）；提前返回错误时连接仍可复用（同一 `httptest.Server` 连续 N 次请求的 `RemoteAddr` 不变）。
3. **严格性测试**：`{"a":1}GARBAGE` 报错；声明 Content-Length 但 body 截断报错；超限响应体返回 `ErrBodyTooLarge`；流式模式下调 `Bytes()` 返回 `ErrStreamConsumed`。
4. **重试测试**：429 + `Retry-After`；503 指数退避；POST 默认不重试；不可重放 body 立即 `ErrBodyNotReplayable`；ctx 取消后不再尝试；退避时长与 `gretry.NextDelay` 一致（同一份实现）。
5. **标准库对照**：同一请求分别用 `http.Client` 与 `client.Client` 发出，断言线上字节（用 `httptest` 记录 `*http.Request`）在无中间件时等价。
6. **基准**：`BenchmarkClientGetJSON` 与 `http.Client` + `json.NewDecoder` 的手写基线对比，目标是耗时 ≤ 手写基线的 1.2 倍、分配数 ≤ 基线 + 2（框架税上限，见 D10），并给出 `-benchmem` 明细。
7. **依赖检查**：`make check-deps` 通过（client 不得 import 其他能力层）；`go list -deps ./ghttp/client` 中不出现 `ghttp` 父包。
8. **工具链前置验证**（P-1）：升级后的 `golangci-lint` 对 `./ghttp/` 无假阳性（对照基线：v2.4.0 在 Go 1.27.1 下有 9 个 typecheck 假阳性，`go vet` 通过）；泛型方法文件（`builder_go127.go`、未来的 `client/generic_go127.go`）确实被 lint 覆盖且 staticcheck 不 panic（golang/go#81188）。
9. **跨版本矩阵**：在 `GOTOOLCHAIN=go1.25.x` 与 `go1.27.1` 两个工具链下分别跑 `go vet ./...` 与 `go test ./...`，确认泛型文件在低版本下被正确排除、高版本下正常工作。

---

## 10. 参考资料

**配套调研报告（同目录，本设计的证据底稿）**

- `2026-09-11-ghttp-client-nethttp-compat-research.md`（495 行）：封装 `net/http` 的兼容性边界与陷阱，含 25 条包装层检查清单与证据索引。§2.5 是它的摘要。
- `2026-09-11-ghttp-client-generics-research.md`（352 行）：Go 泛型方法的时间线、社区实现模式、限制实测与风格指南。§2.4 是它的摘要。

**外部与仓库内来源**

- `go-resty/resty` v2.17.2（`v2` 分支）与 v3.0.0-rc.4（`v3` 分支）源码，本机 clone 于 `/tmp/resty-investigate/`；引用行号见 §2.2.1。
- `imroc/req` v3、`hashicorp/go-retryablehttp`、`dghubble/sling`、`google/go-github`、`goforj/httpx`：竞品与对照样本，符号级引用见 §2.3。
- Go 标准库源码（本机 `GOROOT=/root/.local/share/mise/installs/go/1.27.1`）：`src/net/http/transport.go`、`request.go`、`transfer.go`、`client.go`。
- 仓库内文档：`ghttp/doc.go`、`docs/superpowers/specs/2026-08-07-gk-module-dependency-policy.md`、`docs/superpowers/plans/2026-08-24-ghttp-next-capabilities.md`（client 延后的决策记录）、`docs/ghttp-improvement.md`（旧 client 的缺陷清单）、`.github/workflows/go.yml`、`.golangci.yml`。
- 本机实测（临时验证程序，非仓库资产；关键片段已内联于 §2.4.1/§2.4.3/§2.5）：`/tmp/genericcheck`（go1.27.1 泛型 sink 模式与返回式）、`/tmp/httpcheck`（`ForceAttemptHTTP2` 与 body drain 行为）；`golangci-lint run ./ghttp/` 对现有代码的 9 个假阳性是可直接复现的证据。

---

## 11. 实现进展（2026-09-11，同批落地）

本设计已按 P-1 → P0 → P1 → P2 全部实现并验证。新增代码：`ghttp/client/*`（11 个源文件 + 5 个测试文件）、`ghttp/internal/{httperr,logsafe,codec,sse}`。

### 11.1 与设计的偏差（实现中确认的调整）

| 项 | 设计 | 实现 | 原因 |
|---|---|---|---|
| `WithAcceptRedirects()` | 无 | **新增** | `WithHTTPClient` 传入自带 `CheckRedirect` 时无法推断意图，需要显式选项；`WithDisableRedirects()` 隐含开启它（否则"禁止跟随重定向"会得到"302 是错误"的反直觉结果，该问题由测试发现） |
| 泛型入口返回值 | `GetInto(...) error` | `(*Response, error)` | 响应元信息（状态码/头）在泛型路径上同样需要，且失败时也带回响应 |
| `*Error` 与 `gerr` | 开放问题（倾向依赖） | **已依赖**：`Kind()`/`Gerr()`/`AsGerr()` | 基础契约层允许被能力层引用；Kind 语义在仓库内只实现一次（依赖策略 §3.4） |
| `Response` 落盘 | 仅 `SaveToFile` | 另有 `SetOutputFile` + `SavedTo()` | 下载场景需要"不落内存"的模式；实现中发现 `SaveToFile` 必须能从 `raw.Body` 拷贝（原实现只认已缓存 body，测试暴露） |
| 每请求超时 | `RequestOption` 提及 | `Request.SetTimeout`（经 context） | 不改动共享 Client，也不影响并发请求 |
| `max-same-issues` | 未提及 | `.golangci.yml` 显式设 `0` | 默认值 3 会让同类告警被截断（真实 66 条只显示 32 条），导致"修完仍不绿"的循环 |
| fluent 泛型终结方法 | 仅 `Request.Into`/`Request.As`（须先 `SetMethod`+`SetURL`） | **补充** `Request.{Do,Get,Post,Put,Patch,Delete,Head,Options}Into[T]` | 用户反馈的 API 缺口：泛型解码不该迫使调用方在链上手写 `SetMethod`+`SetURL`。新入口自行设好方法与 URL，链上只留参数与头；`Into` 保留给"method/URL 已另行配置"的场景，共用同一个 `prepareMethod` 前置以免逻辑漂移 |

### 11.2 P-1 工具链修复（前置项，已完成）

- 升级 `golangci-lint` v2.4.0 → **v2.13.2**（v2.4.0 在 Go 1.27 下 typecheck 失效，实测 9 个假阳性）。
- CI `lint` job：Go 1.25.x → **stable(1.27)**，使 `//go:build go1.27` 文件首次被 lint 覆盖。
- CI `preview` / `fmt` job：`GOTOOLCHAIN=go1.27rc2` → **go1.27.1**（正式版）。
- `.golangci.yml`：补 `issues.max-same-issues: 0`。
- 全仓库 lint 清理：**ghttp 66 条 + 其他包 29 条 = 95 条**（均为既有技术债，工具链修复后才暴露）；最终 `golangci-lint run ./...` → 0 issues。

> staticcheck 对泛型方法 panic 的风险（golang/go#81188）在本仓库未复现：v2.13.2 + go1.27.1 下 `ghttp/builder_go127.go` 与 `ghttp/client/generic_go127.go` 均被正常 lint。

### 11.3 P0/P1/P2 交付

**P0（骨架）**：`internal/httperr`（`StatusCoder`，server 侧改类型别名）、`internal/logsafe`（server 的 `sanitizeLogToken` 改委托）、`internal/codec`（严格 JSON/XML 解码内核，server 的 `jsonCodec`/`xmlCodec`/`mediaType`/`contentTypeIn` 全部改委托，**行为不变，server 全量测试通过**）；`client` 的 Client/Option/Request/Response/Error/执行引擎/洋葱中间件/内置 codec/传输注入点/`DoHTTP`/`HTTPRequest`/`RoundTripper()`。

**P1**：sink 泛型 L1（`GetInto`/`PostInto`/`PutInto`/`PatchInto`/`DeleteInto`/`DoInto`/`DecodeInto`/`As`/`Result[T]`/`RequestOption`）；`RetryPolicy`（`gretry` 退避 + 幂等白名单 + `Retry-After` + 发送前重放判定 + 文件部件重开）；multipart 上传；`gerr` 互操作；`WithTrace` 的 httptrace 采集（`TraceInfo`）；Basic/Bearer 认证；`Clone` 隔离。

**P2**：`internal/sse`（线格式编解码，server 侧共用同一实现）；client `SSE`/`SSEStream.Next`/`LastEventID`/`ConsumeSSE`（含退避重连与 `Last-Event-ID` 续传）。

**L2（Go 1.27 方法级泛型）**：`ghttp/client/generic_go127.go` —— `Request.Into[T](ctx, dst)`（sink，零方括号）、`Request.As[T]()`、`Client.{Get,Post,Put,Patch,Delete,Do}Into[T]`，整份文件 `//go:build go1.27`。

### 11.4 验收证据

| 命令 | 结果 |
|---|---|
| `golangci-lint run ./...`（v2.13.2） | **0 issues** |
| `go test ./...` | 全绿（client 新增 40+ 用例） |
| `GOTOOLCHAIN=go1.27.1 go vet ./...` | 通过 |
| `go build ./...` | 通过 |
| `make check-deps` | `dependency policy OK`（client 未 import 其他能力层；未 import ghttp 父包） |

### 11.5 仍未实现（留待后续）

- **WebSocket 客户端**：仍在范围外（`gorilla/websocket` 的 Dialer 可直接用）。
- **熔断 / 限流 / 负载均衡**：范围外，经 `ghttp/adapters/*` 或用户中间件提供。
- **参数 struct tag 绑定（反射）**：`SetQueryParams(map[string]any)` 已够用；确有需求时作为增量。
- **debug 的请求/响应体 dump**：目前只打印请求行、响应状态与错误，`WithDebugBodyLimit` 已就位但尚未消费 body 内容。
- **SSE 的 `retry:` 字段自动覆盖重连间隔**：`SSEEvent.Retry` 已解析并暴露，但 `ConsumeSSE` 尚未用它覆盖 `SSEReconnectPolicy.Delay`。

---

## 附：待评审确认的开放问题

1. **`ghttp/client` 是否 import `ghttp` 父包**：本设计选择"不 import，全靠 internal"。若评审认为 internal 抽取成本过高，退路是允许 import 父包并使用其导出符号（如 `ghttp.StatusCoder`），代价是 client 用户的二进制带上 server 代码。
2. **`*client.Error` 是否直接依赖 `gerr`**：倾向"依赖基础契约层做 Kind 映射"，但会引入 ghttp 家族的第一个内部依赖，需要与依赖策略维护者确认。P1 阶段再决定。
3. **默认响应体上限取值**：文中取 32 MiB。是否需要按 Content-Type 区分（JSON 小、二进制大）？
4. **`Request` 是否提供并发安全保证**：本设计选择"明确不保证 + 文档化"。若评审要求保证，需要在每个 `SetXxx` 上加锁（成本可忽略，但会让"fluent 链"的语义变复杂）。
5. **`Client.RoundTripper()` 适配口**：本设计提供此方法把 client（含中间件/重试）退化为 `http.RoundTripper`，让用户塞进任意 `http.Client`。该适配口的复杂度取决于执行流程是否可反向注入——若评审认为实现成本高于收益，可降级为 P1。
5. **泛型入口的命名**：`GetJSON[T]` 与 `Get[T]` 并存时，`Get[T]` 默认按 Content-Type 解码而 `GetJSON[T]` 强制 JSON——命名是否需要更明确的区分（如 `GetAs[T]`）？
