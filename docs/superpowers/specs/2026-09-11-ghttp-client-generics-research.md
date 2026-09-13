# Go 泛型用于 HTTP 客户端 API 的可行性、惯用法与坑

> 日期：2026-09-11
> 状态：**调研报告**（为 `2026-09-11-ghttp-client-design.md` 提供证据基线）
> 基准：Go 1.27.1（本机工具链，`GOROOT=/root/.local/share/mise/installs/go/1.27.1`）；项目基线 `go 1.25.12` + `//go:build go1.27`
> 证据约定：源码标注 `文件:行号`，外部资料标注 URL；凡未取得一手证据的条目显式标注「未验证」。
> **结论已由主设计文档采纳**：泛型只做"终结/解码"层的薄壳，主推 sink 模式（见设计文档 §3 D2 与 §4.4）。

---

## 0. 结论速览

| 问题 | 答案 |
|---|---|
| 泛型方法是否能在 client 中用？ | 能；Go 1.27（2026-08-19）已正式支持 |
| 推荐引入吗？ | **推荐，但只限于"终结/解码"层**；链与 `Client` 保持非泛型 |
| 推荐形态 | **sink 式泛型终结方法** `Into[T any](dst *T) error`（T 从参数推断，无需显式写类型参数）为主，显式返回式 `As[T any]() (T, error)` 为辅 |
| 不做泛型是否也行？ | 是的；resty v3 / req v3 两个最流行的库都选择**不用泛型**，走 `Result() any` + 类型断言 |
| 推荐的等价零值/204 语义 | 返回哨兵错误 `ErrNoContent`（可 `errors.Is`），或由 `resp.StatusCode()` 自行判定 |

---

## 1. Go 泛型方法的官方时间线与现状

| 时间 | 事件 |
|---|---|
| 2021-10 | #49085 提案 "allow type parameters in methods" 打开（mariomac），>900 👍 |
| 2022-03 | Go 1.18 泛型发布，design 43651 明确"no parameterized methods" |
| 2024 (?) | Go FAQ 仍写 "We do not anticipate that Go will ever add generic methods" （截至 2026-09-11 go.dev/doc/faq 仍未更新，已与事实不符） |
| 2026-01-22 | #77273 "spec: generic methods for Go" 提出；标签 Proposal-Accepted、release-blocker、milestone Go1.27 |
| 2026-04-08 | #49085 标记 duplicate，被 #77273 替代 |
| 2026-05-26 | #77273 关闭（completed），全部 spec/compiler/tools 改动围绕此提案 |
| 2026-08-19 | Go 1.27.0 发布；spec 加入 `MethodDecl = "func" Receiver MethodName [ TypeParameters ] Signature [ FunctionBody ]`，注解 `[Go 1.27]` |
| 2026-08-26 | 官方博文 "Generic Methods"（Mark Freeman）发布 |
| 2026-09-01 | Go 1.27.1（本机 go1.27.1 即此版本） |

**官方来源**：
- https://go.dev/blog/go1.27 — Go 1.27 release notes
- https://go.dev/blog/generic-methods — "Generic Methods" 官方博文
- https://github.com/golang/go/issues/77273 — 已接受提案
- 本机 `go_spec.html`（GOROOT/doc/go_spec.html）：`MethodDecl = "func" Receiver MethodName [ TypeParameters ] Signature [ FunctionBody ]`

**官方博文定调**（2026-08-26）：
> "generic concrete methods are useful by themselves, even if they don't implement interface methods."

**最锋利的一处引用**——提案 #77273 正文（副标题即 "A change of view."）亲手引用自家 FAQ 并宣布改主意：

> "The Go FAQ even states that **'we do not anticipate that Go will ever add generic methods'**. Perhaps a **change of view** is in order: concrete methods are a language feature that is useful in itself, irrespective of interfaces."
> "The fact that such methods may not be invoked via an interface is an **orthogonal aspect**."

而截至 2026-09-11，**go.dev/doc/faq#generic_methods 该段仍未修改**（调研中两次独立抓取一致，HTTP 200）——属于"官方文档与已发布语言特性直接冲突"的实证。向用户解释本特性时，不要引用 FAQ 该段。

**需求热度**（同 issue 正文）：#49085（2021-10，**>900 👍**）、#50981（2022-02）。

**关键限制（官方确认）**：
1. 接口方法不能声明类型参数 → 泛型方法**不能参与接口实现**（`*C does not implement I`，已实证）。
2. 泛型方法对 `reflect` 不可见（实证：`NumMethod=1` 只计非泛型方法）。
3. 必须实例化后才能调用或作为值使用（规范原文）。
4. 工具链支持：gopls/vulncheck/vscode-go ✅（x/tools #77549）；staticcheck ✅（v0.8.0-rc.1+）；golangci-lint ✅（v2.13.0+ 首次支持）。

---

## 2. 社区泛型 HTTP client 实现模式（源码级证据）

### 2.1 核心修正：resty v3 没有泛型 `Result[T]`

**任务前提「resty v3 有 `Result[T]`/`Response[T]`」在穷尽源码验证后被推翻**。resty v3（commit `51f294a`，branch v3，tag v3.0.0-rc.4）中 `grep -rn 'Result\['` 在全部非测试源码中命中 **0 处**；实际 API 为 `Result() any` / `SetResult(v any)` + 类型断言。v2→v3 的差异在模块名、字段导出和错误 API 改名，**不在泛型**。

### 2.2 五种形态的社区样本

**形态 (a) 包级泛型函数 `func Get[T any](...) (T, error)`**

| 库 | 源码 | 调用方 |
|---|---|---|
| sunerpy/requests v0.2.0 | `GetJSON[T any](baseURL string, ...) (Result[T], error)` (`methods.go:161`) | `GetJSON[User]("https://api.example.com/users")` |
| goforj/httpx v2 | `Get[T any](client *Client, url string, opts ...Option) (T, error)` (`client.go:135`) | `httpx.Get[GetResponse](c, url)` |
| devilcove/httpclient | `GetJSON[T any, R any](...) (T, R, error)` (`httpclient.go:114`) | 双类型参数（成功体+错误体） |

**形态 (b) 泛型结果容器 `type Result[T any]`**

| 库 | 源码 |
|---|---|
| sunerpy/requests | `type Result[T any] struct { data T; response *Response; statusCode int }` (`client/result.go:12-32`) |
| Khan/genqlient | `type BaseResponse[T any] struct { Data T; ... }` + `type Response BaseResponse[any]`（`graphql/client.go:221-239`，迁移样板） |

**形态 (c) 参数化接收者类型 `type Client[T]`**

| 库 | 源码 | 特点 |
|---|---|---|
| k8s client-go gentype | `type Client[T objectWithMeta] struct { ... }` (`gentype/type.go:45`) | T = `*corev1.Pod` 等指针类型；对外接口仍是具体类型；HTTP 层保持 `runtime.Object` + 反射 |

**形态 (d) 非泛型 + `any`/ 反射**

| 库 | 源码 | 代价 |
|---|---|---|
| resty v3 | `SetResult(v any)`, `Result() any` + `.(*T)` 断言 | 编译期零检查；204 与空对象不可区分 |
| req v3 | `Into(v any) error` | 同 resty |
| retryablehttp | `Do(*Request) (*http.Response, error)` | 调用方全自理 |

**形态 (e) Go 1.27 泛型终结方法**（本调研的核心发现）

唯一找到的真实开源样本：**goforj/httpx v2.0.1**（`go 1.27.0`）

```go
// generic_methods.go:18
func (c *Client) Get[Out any](url string, opts ...Option) (Out, error)
// 调用：c.Get[GetResponse]("https://httpbin.org/get")
```

关键特征：`Client` 本身非泛型（保持连接池/配置共享），方法自行声明类型参数；`Out` 只出现在返回值位置，因此**调用点必须显式实例化**（与本文 §3.1 的推理实验完全一致）。

### 2.3 零值/204/no-content 语义对照

| 库 | 204 时的行为 | 分析 |
|---|---|---|
| resty v3 | 跳过解码，`Result()` 返回 SetResult 时分配的非 nil 零值指针 | 与 200+`{}` **不可区分**；源码 `middleware.go:545-554` |
| k8s client-go | `Into(obj)` 遇 0-length body 直接报错 `"0-length response with status code: 204"` | 多数 `gentype` 的 typed Get 走 `Do(ctx).Into(result)` → 报 error；`Delete` 类不调 Into 才正常 |
| goforj/httpx | 空 body → `T` 零值 + `nil error`；`ensureNonNil` 将 nil slice/map 规范为空 | "正常零值"语义，调用方查 `resp.StatusCode()` 区分 |
| ogen | 未验证（无 204 spec 用例） | — |

---

## 3. Go 泛型限制对 client API 的具体影响（含实测）

### 3.1 核心制约：类型推断

**`T` 仅出现在返回值位置 → 无法推断，必须显式实例化。**

```
// 实证：即使是赋值到已声明变量也失败
func (c *C) Do[Resp any](p string) (Resp, error) { var z Resp; return z, nil }
var out User; err = c.Do("/x")                 // ❌ cannot infer Resp
v, err := c.Do[User]("/x")                     // ✅ 必须显式
```

现实映射：httpx 的 `.Get[GetResponse](url)`、sunerpy 的 `GetJSON[User](url)` 都显式写类型参数。

**`T` 出现在 `*T` 参数位置 → 可成功推断。** 这就是 sink 式 `Into[T any](dst *T) error` 可行的原因（实证编译通过）。Escape analysis 显示 `dst *T` 不逃逸到堆。

**对比 server 侧：为什么 ghttp 的 `.To[P, O]` 能推断？** 因为 `O` 出现在 handler 参数 `h func(context.Context, P) (O, error)` 中 → 有参数可绑定推断。client 侧没有 handler 参数，O 只在返回值 → 必显式。

### 3.2 接口与反射

| 限制 | 实证 | 对 client API 的影响 |
|---|---|---|
| 接口方法不能有类型参数 | `vet: interface method must have no type parameters` | 不能用接口抽象泛型终结方法 |
| 泛型方法不能满足接口 | `*client2 does not implement Getter2[User]` | 无法用接口 mock 泛型方法 |
| 泛型方法对 reflect 不可见 | `NumMethod=1`（只计非泛型 `Plain`） | 依赖反射的框架（DI/OpenAPI）无法发现 |
| 参数化**类型**可以满足接口 | `var _ Getter[User] = (*client[User])(nil)` ✅ | 若必须接口化，类型参数要放在 `Client[T]` 上而非方法上 |

### 3.3 性能（benchmark 实测）

| 模式 | ns/op | allocs/op |
|---|---|---|
| concrete method (int, noinline) | 0.74 | 0 |
| generic function (int, noinline) | 0.80 | 0 |
| generic method (int, noinline) | 0.77 | 0 |
| interface method | 0.94 | 0 |
| generic function (struct 40B, noinline) | 4.26 | 0 |

结论：**泛型不引入额外分配，调用开销 ≈ 具体函数。** 接口调用略慢（动态分发），但实际 HTTP client 中解码（JSON）占主导，这些差异可忽略。官方博文也明确：boxing 会引入间接开覆盖直接调用，所以他们选择 gcshape + dictionary 而非 boxing。

### 3.4 编译时间（⚠️ 小规模实测与大项目报告结论相反，务必看两段）

**本地小规模实测**：2000 个不同类型参数的实例化 0.80s vs 具体方法 0.86s → 该规模下**统计不可区分**。

**但真实大型项目报告显示代价可观**（golang/go#65605，2024-02 开，**至今仍 Open**，标签 ToolsSpeed + binary-size）：把 B-Tree 从 `interface{}` 迁移到泛型后——

> "Cold CI build time has increased from 6 to 16 minutes."
> "CLI tool executable size has increased from 48 to 58 MB."
> "go build cache has increased from 760MB to 4.4GB (after clean build)."

即 CI 冷构建 **+167%**（6→16 min）、二进制体积 **+21%**（48→58 MB）、build cache **+479%**（760MB→4.4GB，约 5.8 倍）。另有最小复现 #70215 显示泛型 `New[T any]` 比非泛型版本大 32 字节。

**结论修正**：小文件、少量实例化场景下编译代价可忽略（本项目 client 泛型面只有 1–2 个终结方法，实例化数量 = 响应类型数量级，风险低）；但**不要宣称"泛型不增加编译成本"**——代价随实例化数量与依赖深度增长，Go 官方 FAQ 也承认 "Future releases may experiment with the tradeoff between compile time, run-time efficiency, and code size"。真正的成本项是**类型参数个数**，本设计只有 1 个，属安全区。

### 3.5 名称冲突

同一类型上不能同时存在同名的泛型方法与非泛型方法：`method C3.Name already declared`。

---

## 4. 社区真实评价与争论

### 4.1 Go 1.27 泛型方法的社区反应（HN/ GitHub）

| 来源 | 观点 |
|---|---|
| golang/go #77273 (199 comments) | Merovius: 演示了即使 `Getter[T]` 是泛型接口，泛型方法也不能满足 `Getter[byte]` — 社区最大陷阱；msaher66: "不能实现接口的泛型方法只是语法糖"；aarzilli: "这邀请非惯用代码" |
| HN (2026-08-19, 759 pts) | 混合反应：有长期等待的欢呼("can't wait to use them!")，也有反对("generics was a grievous error")；adonovan(Go team)回应了gopls兼容性疑虑；neild(Go team)用 `math/rand/v2.N` 解释实际用途 |
| HN "Generic Methods" (2026-05-27) | EdSchouten: "接口不支持泛型方法的前提下，这个特性是否值得添加？" — 核心争论 |
| HN "Go 1.27 release" (2026-08-20) | seanz3k: "FYI golangci-lint 和 gopls 在使用泛型方法时都坏了" → 实际 gopls 已修复，lint ≥v2.13.0 才支持；Rust 社区对比: masklinn 说 Rust 也有相同限制（trait 泛型方法不是 dyn-compatible） |

### 4.2 通用泛型（非方法）的社区情绪

| 来源 | 观点 |
|---|---|
| HN torginus (2026-08-02): "Go had a clear identity before generics" | 认为泛型破坏了语言一致性 |
| HN Abtinf_ (2026-08-02): "Using generics was a grievous error" | 认为任何人都能立即理解 Go 代码的优势被泛型侵蚀 |
| HN hnlmorg (2026-08-02): "can count on one hand the number of times generics saved me from duplication" | 认为泛型对自己价值有限 |
| cookiengineer (2026-08, HN): "No human knows what the resulting compile time error means" | 批评单字母类型参数的错误信息不可读 |

**类型参数爆炸的真实代码实例**（DoltHub, Nick Tobey, 2024-11-22）：作者最终被迫写出的签名——

```go
func ApplyEditsToIndex[IndexType Index, MapType Map, MutableMapType MutableMap,
    IndexContractType IndexContract[IndexType, MapType, MutableMapType]](
    contract IndexContractType, index IndexType, edits Edits)
```

原文自评："the use of generics here is starting to **infect its use sites**, adding additional boilerplate and complexity"、"it's still very easy to write **overwrought generic code**"、"Maybe generics don't actually solve problems… **just move it around**."（https://www.dolthub.com/blog/2024-11-22-are-golang-generics-simple-or-incomplete-1/）

**错误信息不一致**（golang/go#53692, sethvargo）：同一份代码换约束写法给出三种不同报错——`cannot infer V` / `does not match Cache[string, V]` / `does not implement Cache[string, any] (wrong type for method Set)`。

### 4.3 对 HTTP client 泛型化的具体评价

- **resty v3 选择不用泛型** → 间接信号：最流行的库在有能力（go 1.23）的情况下不引入泛型 API
- **req v3 选择不用泛型**（`Into(v any)` 反射路线）
- **反例：goforj/httpx** 生态空间存在——越来越多人在尝试泛型方法
- **Google style guide** + **Ian Lance Taylor** "When to use generics": 先写非泛型代码，确定需要时再泛型化

### 4.4 泛型方法的 Mock 困境

**最关键的工程约束**：泛型方法不能实现接口 → 不能通过接口 mock。已验证：
- 即使接口本身是泛型的：`type Getter[T any] interface { Get() (T, error) }`，泛型方法 `func (c *C) Get[T any]() (T, error)` 仍然**不能**满足 `Getter[string]`（详见 #77273 Merovius 分析 + 本地实证）。
- 这意味着需要 mock 的组件不能使用泛型方法。grpc/grpc-go#7594 可作为旁证：protoc-gen-go-grpc v1.5.0 引入的泛型流类型导致 mockery 无法生成 mock 文件。
- 若必须可 mocking，抽象要建在非泛型层，或用参数化类型 `client[T]`（可接口化）。

> **这不是会被上游修掉的 bug，而是明确的设计方向**：grpc/grpc-go#7594 中维护者 arjan-bal 逐字回复——"we intend to **keep the code generation using generics**… **Maybe you could ask mockery devs to support generic interfaces?**"，并计划移除回退用的旧 flag。受影响的团队只能自行修改 mock 工具。

---

## 5. 官方/风格指南建议

### Go 官方 "When to Use Generics"（Ian Lance Taylor, 2022-04-12）

> "write Go programs by writing code, not by defining types."

适用场景：
- 容器类型操作（slices/maps）— 每个 T 行为相同 ✅ → 分解码层符合
- 通用数据结构（binary tree）— 不依赖元素类型 ✅
- **相同方法的通用实现**（如 `sort.Interface`）— 方法看起来一样 ✅ → 客户端 decode 符合
- 不要替换接口类型为类型参数 → 如果只调用方法，用接口。→ **Client 的传输/重试/认证层应保留接口**
- 如果实现因类型而异，不用泛型 → 解码可用（`json.Unmarshal` 对任何指针相同），但业务 handler 不能

### Google Go Style Guide — Generics

> "Generics are allowed where they fulfill your business requirements. In many applications, a conventional approach using existing language features works just as well without the added complexity, so be wary of premature use."
> "If there is only one type being instantiated in practice, start by making your code work on that type without using generics at all."

### 社区信号（从库选择推断）

- **resty v3**（最流行的 Go HTTP client）：go.mod `go 1.23.0`，有能力用泛型但选择不用（`Result() any` + 类型断言）。这传递了务实信号：泛型对大部分用户不是瓶颈。
- **goforj/httpx** 反证：有生态空间让泛型同存，且 go 1.27 后出现了一个小型泛型客户端库的生态。

---

## 6. 对本项目的建议

### 6.1 推荐：引入泛型，但限于终结/解码层

鉴于：
- 本项目 server 侧已使用 Go 1.27 泛型方法（`//go:build go1.27`），技术债务已承担
- go 1.27.1 工具链已可靠（2026-09-01）
- 类型推断限制可被 sink 式 `Into[T any](dst *T)` 绕过，调用方无需写类型参数

**推荐 API 表面如下：**

```go
// ——— 非泛型链起点与非泛型 Client ———
type Client struct { /* 连接池、配置、中间件 —— 全部非泛型 */ }
func (c *Client) Get(url string) *Request
func (c *Client) Post(url string) *Request

// ——— 泛型终结方法(go 1.27, 与 builder_go127.go 相同门控) ———

// Into 为主推：解码响应到 dst；T 从 *T 推断。
// 204/空 body 返回 ErrNoContent。
func (r *Request) Into[T any](dst *T) error

// As 为辅：显式返回。
func (r *Request) As[T any]() (T, error)

// ——— 包级自由函数(低成本补充) ———
func Get[T any](ctx context.Context, url string) (T, error)
```

**为什么要主推 sink 式 `Into[T any](dst *T)`：**
1. `T` 从 `*T` 推断 → 调用方写 `var u User; client.Get(url).Into(&u)`，**无需写 `<类型参数>`**。
2. 与 server 侧 `To`/`ToBody`/`ToNone` 的"链上无类型参数"风格一致。
3. 204/no-content 可通过返回 `ErrNoContent`（哨兵，`errors.Is` 判定）明确表达"缺席"。
4. `dst` 不逃逸（实证 `gcflags=-m` 确认无 heap alloc）。

**零值/204 语义约定（借鉴 goforj/httpx 的 `ensureNonNil` + 哨兵 error）：**

```go
var ErrNoContent = errors.New("no content")  // 导出哨兵
// Into 在 StatusCode==204 或空 body 时返回 ErrNoContent
// 调用方：
var u User
if err := c.Get(url).Into(&u); errors.Is(err, ErrNoContent) {
    // 204 No Content
}
```

### 6.2 不做 `Result[T]` 容器

社区教训：resty 证明了 "non-nil zero pointer" 的缺陷；sunerpy 的 `Result[T]` 在 error 路径返回 `var zero Result[T]` → T 零值+ error 对空 body 不表达缺席。若必须提供，请带 `Present bool` 但增加编码成本。我们建议直接 哨兵 error。

### 6.3 不做的选项

完全不引入泛型也是合理路线（resty/req 验证）。如果用户群担心 go 1.27 依赖和工具链升级，`Into(v any) error` 非泛型版本可共存（放在无 tag 文件里，在 go < 1.27 下可编译）。

### 6.4 工程约束（已验证，可复用 server 侧模式）

**语言版本门控：** 沿用 `//go:build go1.27` + `go 1.25.12` go.mod 的策略。**已验证**：同一模块内带 tag 的文件以 go1.27 编译，不带 tag 的文件按 go.mod「go 1.25」编译 → 泛型方法只在 go 1.27 工具链下可见。

**CI 升级需求（关键、高优先级）：**

| 当前 CI | 现状 | 需要做什么 |
|---|---|---|
| lint: go 1.25.x + golangci-lint v2.4.0 | go1.27 文件被 build tag 排除，**完全不被 lint** | 升 golangci-lint ≥ **v2.13.0**（2026-08-19 首次 go1.27 支持），lint 的 go-version 升到 1.27 |
| preview job: GOTOOLCHAIN=go1.27rc2 | rc2 已过时 | 改为 `stable` 或 `1.27.1` |
| test: go 1.25.x + stable | 1.25 下 go1.27 文件不编译，stable 下编译 | 现有配置已覆盖，但建议在 stable job 中显式执行 go1.27 文件的测试 |

> ⚠️ **升级 lint 后仍可能被 staticcheck 崩溃阻断**：golang/go#81188（Brad Fitzpatrick/Tailscale，2026-08-28 开，**至今 Open**，Milestone Go1.28）——go1.27.0 的 unified export data 丢失方法顺序，破坏 objectpath，导致 staticcheck **panic**（`honnef.co/go/tools/staticcheck/sa4023.run` index out of range）。原文："We just started converting Tailscale's internal codebase to use Go 1.27's generic methods and a few rounds of conversion went well, and then we hit CI failures with staticcheck crashing on us"。本项目 `.golangci.yml` 启用了 `staticcheck`，且 CI 同时含 `go vet ./...`——**这是引入泛型 client 后最可能立刻踩到的真实阻断点**，建议先在临时分支上验证 lint 全绿再合并。

### 6.5 风险

| 风险 | 缓解 |
|---|---|
| 泛型方法不能被接口实现 → 无法用接口 mock | 抽象建在非泛型层（`Doer` 接口 + `Into` 调用）；或用参数化类型 `client[T]`（可接口化）。注意：**连已实例化的泛型接口也不满足**（`Getter[string]` 同样不行，#79834 中 ianlancetaylor 确认为"接受的妥协"） |
| reflect 不可见 → 自动发现/文档失败 | 不影响运行时，影响代码分析工具和 OpenAPI 反射提取 |
| staticcheck panic（#81188） | 升级到修复版本前，先在分支验证 lint；必要时临时禁用相关检查 |
| 调用方必须至少升级到 go 1.27 | 项目已通过 server 侧提交该门槛 |
| 编译时间/体积随实例化数量增长（#65605） | 本设计类型参数只有 1 个、实例化数 = 响应类型数，属安全区；避免为每个业务类型增加额外类型参数 |
| 错误信息因类型参数变长 | 相比 `Do[T]` 显式写 `T struct{...}` 时，类型参数数量有限（本设计只 1-2 个），恶化可控。Go 团队自己承认推断失败信息不友好（#60542 `NeedsFix`、#61685 `BadErrorMessage`） |

---

## 引用源汇总

- https://go.dev/blog/go1.27 — Go 1.27 Release Notes
- https://go.dev/blog/generic-methods — "Generic Methods" 官方博文 (2026-08-26)
- https://go.dev/blog/when-generics — "When To Use Generics" (Ian Lance Taylor, 2022-04-12)
- https://github.com/golang/go/issues/77273 — 泛型方法已接受提案
- https://github.com/golang/go/issues/49085 — 原泛型方法提案（已关闭为 duplicate）
- https://github.com/golangci/golangci-lint/releases/tag/v2.13.0 — 首个 go1.27 支持版本
- https://github.com/go-resty/resty (v3 commit 51f294a) — 非泛型 client 现实样本
- https://github.com/goforj/httpx (v2 commit 3de9dc7, go 1.27.0) — 唯一泛型方法落地样本
- https://github.com/kubernetes/client-go (commit f02d442) — 严格限域泛型的样板
- https://github.com/sunerpy/requests (tag v0.2.0) — 泛型函数 + 容器组合
- https://chrisfrewin.com/blog/golang-a-powerful-generic-function-to-make-http-requests/ — `func Do[T]` 生产使用报告
- https://github.com/princefishthrower/golang-generic-http-helper-function — `Do[T]` 通用 HTTP helper 开源
- https://www.willem.dev/articles/generic-http-handlers/ — 泛型 HTTP Handler 设计
- https://incident.io/blog/code-generation — 泛型 vs 代码生成互补观点
- https://stackoverflow.com/questions/75974073/ — 类型推断限制确认（blackgreen, 2023-04）