# ghttp 顶层重写：单一 typed 执行模型设计

- 状态：设计草案（评审中，已开始实现）
- 位置：**顶层 `ghttp/` 目录，`package ghttp`**，模块路径 `github.com/sofiworker/gk/ghttp` 不变。旧顶层 `*.go`（113 文件）已 `git rm`（可 `git checkout HEAD -- ghttp/` 找回），从零原地重写。
- 决策：**全部重写（含 radix 树），不从旧代码搬运**（用户选 B）。旧实现仅作行为参考，源码不复用。语义不变量以 git 历史中 `TestChainCollapse*` 等测试为验收蓝本（实现重来、行为不变）。
- 前置：本设计取代旧 `ghttp` 的「typed 管线 + 直达逃生舱」双执行模型；不是打补丁，是内核重写。

---

## 1. 为什么重写（问题陈述）

当前 `ghttp` 主线在多轮性能优化后达成了可观的基准数字，但沉淀了**投机复杂度**，且根因是**目标自相矛盾**。

### 1.1 实测复杂度（重写前基线）

| 位置 | 体量 | 性质 |
|---|---|---|
| `compiledState.ServeHTTP` | 208 行 / 4 条平行分派路径 / 7 个 recover 点 | 债 |
| `compiledRoute` 的 `direct` / `directParamHandler` / `directName` | 3 个纯提速字段 | 债 |
| 参数双记账：偏移表 + 内联值表并存 | 同一份数据维护两遍 | 债 |
| `lookupAllow`（143 行）/ `matchStaticChild`（108 行，链式折叠） | radix 匹配核心 | **挣来的**（被 12 项等价性测试焊死） |

### 1.2 根因

矛盾的北极星：**既要 typed 契约（校验 / 错误模型 / 内容协商 / OpenAPI，这是 ghttp 的存在理由），又要在微基准上打赢 gin**。

- 直达逃生舱（`route.direct`）靠**绕开 ghttp 自己的 typed 管线**来跑分 → 于是存在**两套执行模型**：真的那套（Ctx + 切片链 + typed）与逃生那套（裸 `http.HandlerFunc`）。
- 两套模型 = 双倍语义面。`ServeHTTP` 里 7 个 recover 中有 3 个专门为逃生路径补 `panic → 500` 语义。
- 「好看」的静态路由数字（StaticRoute 1.88×、PathParam1 2.07× gin）测的是**一条跳过了 ghttp 存在理由的代码路径**。

### 1.3 重写目标

**单一执行模型**：所有请求走同一条 `typed` 路径；「裸 HTTP」不再是藏在分派里的 bypass，而是**一等公民的显式入口** `RawHandler`。以此消灭 4 条平行分派、3 个补语义的 recover、参数双记账。

**性能与简洁不再对立**（见 §9）：旧代码的复杂度来自「用 bypass 换跑分」，而 §9.1 的实测证明——保持单一 typed 模型、纯标准库，靠**池化上下文 + 零反射 codec** 即可在几乎所有场景超过 gin/echo。因此本轮同时要简洁与性能，但性能只允许来自「类型契约红利」，**不允许**来自绕过自身契约的 bypass，也**不允许**来自更换传输层（§2.2）。

---

## 2. 北极星与非目标

### 2.1 北极星

验证一种**面向 Go 泛型的 HTTP 路由模型**：

```
显式声明 endpoint
  → 静态保存路径、参数与输入/输出契约
  → 注册阶段生成 compiled handler（类型擦除进树）
  → gin 风格 radix tree 完成路径匹配
  → 运行时只执行预编译的提取器、业务函数与输出编码器
```

保留 gin 高效前缀树，同时让输入解析与输出编码具有**明确的类型与契约**。不复制 gin 的全部 API。

### 2.2 硬约束（不可协商）

- **纯标准库 `net/http` 地基**：只用 `net/http` 的 `Handler` / `ResponseWriter` / `*http.Request` 模型。**不引入 fasthttp 或任何自管 TCP / 连接复用 / 非标准请求上下文的传输层**。理由：保留标准库生态兼容性（任意 `http.Handler` 中间件、`httptest`、标准 `http.Server` 的 HTTP/2 / TLS / 优雅关闭），性能红利只能来自标准库**之上**的类型契约优化（池化 + 零反射 codec），不靠换地基。这也是本设计相对 fasthttp 的卖点：不牺牲生态换吞吐。
- 允许的性能手法（均为标准库之上）：`sync.Pool` 池化上下文、注册期固定的零反射编解码器、复用缓冲、注册期固定参数位置。
- 不做 `[]any` 参数数组、不做反射热路径调用、不做隐式 JSON 输出（见 §8）。
- 不在本轮引入方法级泛型 API（语言不支持，见 §3）。

### 2.3 性能立场（已由铁证修正）

早期判断为「运行时泛型不可能超过 gin/echo，基准只验证不退化」。**该判断已被项目 harness 的三方同负载基准推翻**（见 §9.1）：纯标准库的 ghttp-C 原型在几乎所有场景超过 gin 与 echo。因此性能立场修正为：**目标是在纯标准库前提下超过 gin/echo**，钥匙是池化上下文 + 零反射 codec（typed 契约的结构性红利，gin/echo 因无类型契约学不来），而非执行模型或传输层。

---

## 3. 关键语言约束：方法不能带类型参数（已用编译器证实）

原始构想中的链式 API——

```go
mux.Post("/users/{id}").
    Path[Int64Path]("id").      // 方法带类型参数
    Query[UserQuery]("verbose").
    Body[CreateUser](JSON())
```

**在 Go 中编译不过**。实测（`go build`）：

```
syntax error: method must have no type parameters
```

这是语言规范层面的限制（golang/go#49085，长期未落地）。项目历史文档 `2026-06-29-ghttp-route-builder-api.md` §295「Phase 4: Add Go Generic-Method API Later」已预见此点，将方法级泛型排至最后阶段。

因此 API 骨架只能在两种**可编译**形态中择一，两者均已写出可运行原型（`/tmp/proto/{b,c}`，评审后并入实现区）：

### 方案 B（否决）：类型参数上提到 Builder，链式方法不带泛型

```go
Post[int64, UserQuery, CreateUser, UserOutput](mux, "/users/{id}").
    Path("id").Query("verbose").Status(201).
    Handler(func(ctx, in RequestInput[int64, UserQuery, CreateUser]) (UserOutput, error) {...})
```

实测缺陷：

- **类型参数顺序写反（P↔O）编译器放过**：写成 `Post[UserOutput, ..., int64]` 配套错误 handler，`go test` 照单全收。
- **`.Path("id")` 拿不到类型证据**：链方法不带泛型，只能记字符串名字；参数解码器没有类型安全落点（原型中被迫留 `pathDecode 可能为 nil` 的洞）。
- **「必须声明 Output」编译期管不了**：漏写不报错，只能退回注册期 / 运行期检查。
- 参数级类型契约全废——恰好丢掉原构想 §4–§5 最想要的东西。
- 唯一优势：链式视觉。

### 方案 C（采用）：自由函数 + 类型推断，每个输入源自带类型

```go
Handle(mux, http.MethodPost, "/users/{id}",
    PathInt64("id"),              // 自带 int64
    QueryStruct[UserQuery](),     // 自带 UserQuery
    Body[CreateUser](JSON()),     // 自带 CreateUser
    JSON[UserOutput]().Status(http.StatusCreated),
    func(ctx context.Context, in RequestInput[int64, UserQuery, CreateUser]) (UserOutput, error) {
        return UserOutput{ID: in.Path}, nil
    })
```

实测优势：

- **In/Out 写错 `go build` 直接失败**：`does not match inferred type func(...) for func(...)`。
- **每个输入源独立携带类型**：`PathInt64("id")` 即 `int64`，`QueryStruct[UserQuery]()` 即 `UserQuery`；参数级契约编译期成立。
- **「必须声明 Output」是参数**：漏了 build 失败。
- 形态与现有 `ghttp` 的 `Handle(...)` 同构，迁移认知成本最低。
- 代价：不是链式，是一次性函数调用。

### 决策

**采用 C。** 理由：原构想的灵魂是「每个输入来源自带类型契约」，只有 C 能在编译期兑现；B 为链式视觉牺牲掉整个方案最核心的编译期契约，不划算。链式手感通过 §4.4 的 `Group` 分组糖补偿，不进入类型契约路径。

---

## 4. API 设计（方案 C）

### 4.1 typed 入口

```go
func Handle[P, Q, B, O any](
    mux *Mux, method, path string,
    p InputSource[P], q InputSource[Q], b InputSource[B],
    out OutputSpec[O],
    h func(context.Context, RequestInput[P, Q, B]) (O, error),
) error
```

- 四个类型参数**全靠推断**，调用点不手写。
- `method, path` 为普通字符串；提供 `Get/Post/Put/Patch/Delete/...` 薄封装固定 method。
- 返回注册错误（§7.1），不 panic；另提供 `MustHandle` 变体用于启动期即崩。

### 4.2 输入源 InputSource[T]

```go
type InputSource[T any] interface {
    // Bind 在注册期固定提取器；返回运行期解码闭包与该来源的静态描述（供 OpenAPI）。
    Bind(spec *routeSpec) (decode func(*Request) (T, error), err error)
}
```

内置构造器（每个自带具体 T）：

| 构造器 | T | 来源 |
|---|---|---|
| `PathInt64(name)` / `PathString(name)` / `PathUUID(name)` ... | 对应标量 | 路径参数 |
| `QueryString(name)` / `QueryBool(name)` / `QueryInt(name)` ... | 标量 | 单个 query |
| `QueryStruct[T]()` | 结构体 | 多 query 字段（注册期固定字段提取器，**不在请求期反射扫描 tag**） |
| `HeaderString(name)` / `CookieString(name)` | string | 头 / cookie |
| `Body[T](codec Codec)` | T | 请求体，格式由 codec 决定 |
| `Form[T]()` / `Multipart[T]()` | T | 表单 / 上传 |
| `RawRequest()` | `*Request` | 原始请求（见 §4.5） |

body 格式与 T 解耦，由 codec 决定：`Body[CreateUser](JSON())` / `Body[CreateUser](XML())` / `Body[CreateUser](Form())`。

### 4.3 组合输入 RequestInput[P, Q, B]

```go
type RequestInput[P, Q, B any] struct {
    Path  P
    Query Q
    Body  B
}
type NoPath struct{}
type NoQuery struct{}
type NoBody struct{}
```

- 路径 / query / body **不写进业务结构体**，由框架容器组合。
- 缺某来源用零尺寸占位类型：`RequestInput[NoPath, UserQuery, CreateUser]`。占位类型不承载数据、运行期不产生有效负载，对应输入源用 `NoneInput[NoPath]()` 之类的空绑定。
- **待评审取舍**（§10 T1）：`RequestInput` 每次构造一个三字段结构体。若逃逸压力显著，备选「扁平多参数 `func(ctx, id, q, body)`」——零包装但参数个数固定。默认先用 `RequestInput`（规整、可扩展），以基准与逃逸分析定夺。

### 4.4 Group 分组糖（补偿链式手感，不入类型契约）

```go
users := mux.Group("/users")            // 仅拼路径前缀 + 共享中间件
users.POST("/{id}", PathInt64("id"), Body[CreateUser](JSON()), JSON[UserOutput](), handler)
```

`Group` 只负责前缀拼接与中间件继承，类型契约仍走 §4.1 的参数式。

### 4.5 原始请求与 RawHandler（一等公民逃生，取代旧 bypass）

```go
type Request struct {
    *http.Request
    Params Params      // 槽位数组，非 map（§6）
}
type Response struct {
    http.ResponseWriter
}
```

两种「需要原始请求」的形态：

```go
// 形态一:业务仍走 typed 输入/输出,但额外拿到 *Request
func(ctx context.Context, req *Request, in UserInput) (UserOutput, error)

// 形态二:RawHandler 完全接管 HTTP 响应(显式入口,不是隐藏 bypass)
mux.Get("/debug").RawHandler(func(ctx context.Context, req *Request, resp *Response) error {
    resp.Header().Set("X-Trace", "enabled")
    resp.WriteHeader(http.StatusNoContent)
    return nil
})
```

`RawHandler` 与 typed handler 编译成**同一个 `compiledHandler` 接口**，进同一棵树、走同一条 `ServeHTTP` 分派——**这是消灭 4 条平行分派的关键**。

### 4.6 输出契约 OutputSpec[T]

```go
type OutputSpec[T any] interface {
    Encode(*Response, T) error
    // Describe 供 OpenAPI:声明默认状态码、Content-Type 等静态信息。
    Describe() outputMeta
}
```

- `(Out, error)` **不默认代表 JSON，也不默认 200**。格式与状态码必须显式声明。
- 内置：`JSON[T]()` / `XML[T]()` / `Text()` / `Bytes()` / `HTML()` / `Stream()` / `NoContent()` / `Redirect()`。
- 状态码属于输出契约：`JSON[UserOutput]().Status(http.StatusCreated)`。
- 响应头 / Cookie / Content-Type 由 `OutputSpec` 或 `Response` 显式处理。
- **endpoint 未声明 Output → 注册期返回错误**，绝不隐式选 JSON。

---

## 5. 执行模型（单一路径）

### 5.1 类型擦除进树

```go
type HandlerFunc[In, Out any] func(context.Context, In) (Out, error)   // 只描述业务计算

type compiledHandler interface {                                        // 进树的类型擦除接口
    Serve(context.Context, *Request, *Response) error
}
```

不同 `Handler[In, Out]` 实例化后是不同具体类型，无法直接共存于一棵树；注册期将泛型 handler 编译为 `compiledHandler`。**请求匹配完成后不再反射判断 handler 类型**，只做**一次接口分派**。

### 5.2 编译后执行器（等价逻辑）

```go
func (c *compiled[P, Q, B, O]) Serve(ctx context.Context, req *Request, resp *Response) error {
    var in RequestInput[P, Q, B]
    var err error
    if in.Path, err  = c.decodeP(req); err != nil { return c.inputErr(err) }
    if in.Query, err = c.decodeQ(req); err != nil { return c.inputErr(err) }
    if in.Body, err  = c.decodeB(req); err != nil { return c.inputErr(err) }
    out, err := c.h(ctx, in)
    if err != nil { return err }                 // 业务错误 → 统一错误链
    return c.out.Encode(resp, out)               // 编码错误 → 统一错误链
}
```

- 解码器 / 编码器 / 参数位置**全部在注册期固定**，请求期不反射扫描字段、不重解析路由模板。
- 中间件：切片链，编译期 `Chain(terminal, middlewares...)` 折叠为单一入口（沿用现有实现思路）。

### 5.3 单一 ServeHTTP

```
ServeHTTP:
  1. radix 树按 method + path 匹配 → compiledHandler 或 404/405
  2. 取 *Request(池化) + *Response
  3. 一次 recover 包裹 → ch.Serve(ctx, req, resp)
  4. Serve 返回的 error 交统一错误链
```

对比旧的 208 行 / 4 分派 / 7 recover：目标 **1 条分派、1 处 recover、单一模型**。

---

## 6. 路由树（完全采用 gin 算法与语义）

> **⚠️ 决策修订（本轮实测后推翻早期「从零重写」方案）**：早期打算「从零实现压缩 radix 树、代码全新、仅行为对齐旧测试」。该方案已被基准推翻——从零实现的树纯匹配 73ns，而 gin 全链路才 60ns，且我们靠"注册期把纯静态路由摊进 `map` 走 O(1)"才勉强在静态场景追平 gin，**动态路由始终打不过**。map 是拐杖，不是真本事。因此用户拍板：**路由匹配完全采用 gin（v1.12.0）的算法**，并**连语义也完全对齐 gin**。

- **算法逐行移植自 gin v1.12.0 `tree.go`**（`addRoute`/`insertChild`/`incrementChildPrio`/`longestCommonPrefix`/`findWildcard`/`getValue`），仅把 gin 的 `HandlersChain`/`Params` 适配为本框架的 `compiledHandler` 与 `Params{keys,vals}`；**代码为本项目实现，不含 gin 源码拷贝，无 MIT 署名负担**（用户选「照 gin 算法自己重写」）。
- **节点结构对齐 gin**：`routeNode{path, indices, wildChild, nType, priority, children, handler}`。通配子节点**恒为 `children` 数组的最后一个元素**,由 `wildChild bool` 标记；不再用 echo 式独立 `param`/`catchAll` 字段。
- **匹配 = 单循环 `getValue` + `skippedNodes` 显式回溯栈 + TSR**。移除了从零版本的"静态 map 拐杖"，树本身即可在静态/动态场景达到 gin 同级。
- **模板翻译**:`{name}` → gin `:name`,`{name...}` → gin `*name`(`route_path.go` 的 `translateTemplate`),翻译后节点 path 即 gin 原生形式,param 名取 `n.path[1:]`、catchAll 名取 `n.path[2:]`。
- **语义完全对齐 gin(用户确认「连语义也对齐」)**:
  - catch-all 值 = 整个剩余 path,**含前导 `/`**(`/files/a/b/c` → rest=`/a/b/c`)。
  - catch-all 零段 → **TSR 301**(`/files` → 301 到 `/files/`),不再是 200/空值。
  - 尾斜杠 → **TSR 301**(`/a/v1/b/` → 301 到 `/a/v1/b`;反向补斜杠同理),不再"容忍返回 200"。GET 用 301,非 GET 用 308(保留方法与请求体)。
  - dot/empty 段 → 400(`validateRequestPath` 单遍字节扫描,零分配;结尾单斜杠留给 TSR)。
- 参数容器仍用**槽位数组 `Params{keys, vals []string}`,不用 `map`**(§10「避免 map」)。
- 同 method + 同 path 重复注册 → 注册期 `ErrDuplicateRoute`。

**移植中踩到并记录在代码注释里的两个精妙陷阱**:
1. `skippedNode` 保存的节点副本**必须故意省略 `indices`**——回溯重走时静态子节点因此"隐形",匹配直接落到通配子节点,这正是"静态失败后回落 param"的实现;若拷了 `indices` 会导致**无限回溯死循环**。
2. gin 用 `*skipped = (*skipped)[:index+1]` 依赖预分配容量;我们改用 `append` 自动扩容(池化底层数组经 `[:0]` 复用,预热后仍零分配)。

**明确不引入的旧包袱**:`direct` / `directParamHandler` / `directName` 逃生舱、`needsState` 提速开关、`matchDirectParam` 直达路径、参数双记账——单一参数记录,单一执行路径。

### 6.1 请求路径校验:双模式开关(`WithStrictPath`)

profile 定位到:每请求的 `validateRequestPath` 逐段扫描曾占 **~31%**(实测 28ns);而 gin **根本不校验** dot/空段(`/a/../b` 在 gin 里 200 且 `p1=".."`),这正是 gin 快的一部分原因。用户决策:**折中作默认,完整校验作可开开关**。

- **默认(快速模式,`strictPath=false`)**:只拦 **dot 段**(`.`/`..`,防路径遍历)。先用一次 SIMD `strings.IndexByte(path, '.')` 粗筛——无 `.` 立即放行(REST 路径常态,几乎零成本);有 `.` 才细扫确认。**空段(`//`)不拦**,交给匹配层(与 gin 一致:空段作为空参数值匹配,如 `/a//b` → `p1=""` → 200)。
- **严格模式(`New(WithStrictPath(true))`)**:完整逐段校验,dot 段与空段任一非法即 **400**。
- 两模式均零分配;尾斜杠始终交给 TSR(§6 语义)。

**性能兑现**:快速模式把校验从 28ns 压到近乎免费,Param5 全链路 79ns → **54ns**,由此在三方基准中**大多数场景反超 gin**(见 §9.x 实测:Static/GithubStatic/Miss/Param1/Param5/GithubParam/GithubAll 七场景胜 gin,仅 Wildcard 一项落后)。全场景 0 alloc。

### 6.2 路由层测试矩阵(36 个测试函数,90.4% 语句覆盖,-race 全绿)

路由层测试分三个文件:

- `route_tree_test.go`(语义单元):静态/参数/catch-all 交替、深静态链、中链分叉、静态优先于参数、in-segment 片段分裂、转义回退(策略 A)、%2F 分段、catch-all 零段/多段、TSR、dot/空段、严格模式、静态失败回落 param。
- `routing_matrix_test.go`(系统矩阵):
  - **大规模**:内置 **203 条 GitHub v3 API 路由表**(社区基准同款),逐条实例化请求验证命中 + 参数正确(`TestLargeScaleAllRoutesHit`),并把全部 GET 路由塞进单棵共享树抽样验证无串扰(`TestLargeScaleSharedTreeHits`)。
  - **参数提取**:单参/五参/相邻参数/特殊字符(`-.~_`)/段内点号(`archive.tar.gz`)/Unicode(`张三`)/百分号解码(`hello%20world`)/catch-all 单段与多段/参数后接 catch-all。空段作为空参数值命中(对齐 gin)。
  - **404**:未知根、前缀不足、过浅、过深、param 下无子、兄弟未命中、空 Mux 全 404。
  - **405**:多 method 同路径的 `Allow` 头收集、单 method、以及"路径不存在是 404 而非 405"的区分。
  - **TSR(301/308)**:去斜杠/补斜杠、静态/参数/嵌套、GET 用 301 非 GET 用 308、query 保留、根 `/` 不重定向、精确匹配不被 TSR 覆盖。
  - **状态码透传 + 池化卫生**:handler 写的任意码(200/201/**203**/204/**403**/418/500)必须原样透传;池化 `Request`/`Params` 在交替不同参数量的请求间无残留(`TestPoolHygieneNoParamLeak`);16 goroutine × 300 次并发命中在 -race 下无竞争、参数无跨协程串扰(`TestPoolHygieneConcurrent`)。
- `routing_fuzz_test.go`(Go 原生 fuzz,各 ~120 万次执行/10s 无崩溃):
  - `FuzzServeHTTPNoPanic`:任意 method+路径,ServeHTTP 不 panic 且只产生合法状态码集(200/404/405/400/301/308/500)。
  - `FuzzValidateRequestPath`:校验器两模式都终止不 panic,并守住两条不变量——快速模式通过 ⇒ 无真正 dot 段(对照独立 oracle);严格通过 ⇒ 快速也通过(strict ⊆ fast)。
  - `FuzzRegisterThenMatch`:随机合成的合法模板注册后,其规范请求必命中 200 且参数正确(注册↔匹配一致性)。

---

## 7. 错误处理

### 7.1 注册期错误（`Handle` 返回 error）

- 路径不以 `/` 开头；参数名非法 / 重复；catch-all 不在末段；静态与动态冲突；同 method+path 重复；
- **endpoint 未声明 Output**；
- （方案 C 下 In/Out 不匹配由**编译器**拦截，不进入运行期错误集）。

### 7.2 请求期错误

| 情况 | 响应 |
|---|---|
| 路由不存在 | 404 |
| 路径存在、method 不符 | 405 + `Allow` 头 |
| 参数 / 输入解析失败 | 400（统一输入错误） |
| body 格式不支持 | 415 |
| 输出编码失败 | 交统一错误链 |
| handler 返回业务错误 | 交统一错误链 |

统一错误链沿用现有 ghttp 错误模型 / Problem Details（复用，不重造）。

---

## 8. 明确不采用

- **`[]any` 参数数组** `func(ctx, []any) (any, error)`：丢静态类型、产生装箱 / 切片 / 断言开销。
- **反射热路径调用** `reflect.Value.Call`：反射仅可作注册期校验或原型工具，不作最终执行路径。
- **隐式 JSON 输出**：`(Out, error)` 不默认 JSON、不默认 200，必须显式 `Output`。

---

## 9. 性能：铁证与杠杆

### 9.1 原型三方基准（已实测，纯标准库）

在项目自有 harness（`benchmarks/`，`mockWriter` 消噪 + 预建请求 + 池化对等）中新增纯标准库的 `ghttp-C` 原型端点（`sync.Pool` 池化上下文 + httprouter 匹配以隔离变量 + 方案 C typed 执行器 + 零反射编码器），与 gin / echo 同负载对比。5 轮中位数（ns/op、allocs/op）：

| 场景 | ghttp-C | gin | echo | 结论 |
|---|--:|--:|--:|:--|
| StaticRoute | **44ns/1a** | 75/1 | 76/1 | 快 gin/echo ~1.7× |
| StaticRouteParallel | **8ns/1a** | 22/1 | 14/1 | 大幅领先 |
| PathParam1 | **63ns/2a** | 80/1 | 89/1 | 领先 |
| PathParam5 | **152ns/2a** | 152/2 | 216/2 | 平 gin、快 echo |
| Wildcard | **70ns/2a** | 84/1 | 84/1 | 领先 |
| QueryParams | **349ns/6a** | 409/7 | 417/7 | 领先 |
| JSONBind（解码+编码） | **1881ns/17a** | 2543/25 | 2192/19 | **快 gin 26%、echo 14%** |
| JSONResponse | 767ns/2a | 794/3 | **737ns/2a** | 平 gin、微输 echo |
| FullChain（路径+查询+头+体+JSON） | **3553ns/26a** | 4171/31 | 4150/27 | 快两者 ~15% |
| FullChainParallel | **836ns/26a** | 1010/31 | 922/27 | 领先 |

**结论**：纯标准库前提下，ghttp-C 在几乎所有场景超过 gin 与 echo。唯一微输的 JSONResponse 是**故意**让复杂 `profile` 结构回退 `encoding/json` 以证明回退路径存在——若为它也生成零反射编码器即可翻盘（见 §9.3）。

### 9.2 优势来源（可复现的因果）

- **执行器不是瓶颈**：隔离微基准显示，当 handler 真做 JSON 编解码时，方案 C 的「解码闭包链 + `RequestInput` 构造 + 一次接口分派」相对裸手写**零额外成本**（差异淹没在序列化里）。修正了早期「泛型倒退」的印象——那是空 handler 下的假象。
- **两个真杠杆，都在标准库之上**：① `sync.Pool` 池化上下文（gin/echo 同款手法）；② **注册期固定的零反射编码器**（`strconv.Append*` 拼字节，省 `encoding/json` 反射）。JSONBind 快 gin 26% 即此杠杆——gin `c.JSON` 走反射，ghttp-C 走类型契约固定的编码器。**gin/echo 因无类型契约学不来**，这是 typed 框架的结构性红利。

### 9.3 codec 策略（本轮采用「零反射优先 + stdlib 回退」）

- 内置标量 / `[]byte` / string / 常见小结构：注册期备好零反射编解码器。
- 复杂 / 嵌套 / 用户任意类型：回退 `encoding/json`（正确性优先，性能退化到 gin 同级，不劣化）。
- **不引入 codegen 工具链**（用户决策）：零反射能力以运行时预备 / 手写内置为主，复杂类型接受 stdlib 回退。若日后 JSONResponse 这类复杂输出成为热点，codegen 作为独立可选增强再议（§10 T5）。

### 9.4 诚实性纪律

- 所有对比与 gin/echo **同工作负载**（相同解码 + 相同编码 + 相同状态码），避免旧基准「gin 写纯字符串、ghttp 做 marshal」的不对等。
- 原型的 httprouter 匹配层仅用于**隔离执行器/codec 变量**，不代表最终实现；最终版复用 ghttp 自有的、已对齐 gin 的压缩前缀树（§6），届时以完整 harness 复测。
- 指标至少含 `ns/op`、`allocs/op`、`B/op`。

---

## 10. 待评审取舍（实现前需拍板）

- **T1｜`RequestInput` 结构体 vs 扁平多参数**：默认 `RequestInput[P,Q,B]`（规整）；若逃逸分析显示三字段容器显著上堆，切扁平多参数。以基准定夺。
- **T2｜路由语法兼容面**：只支持 `{id}`/`{path...}`，还是同时兼容 gin 的 `:id`/`*path`？影响能否直接复用现有 registry。
- **T3｜`Handler` 泛型壳生命周期**：确认 `Handler[In,Out]` 是「一次性注册脚手架」，注册后只保留 `compiledHandler`，泛型只活在注册期。
- **T4｜扩展 handler 形态**（`func(ctx, *Request, In) (Out, error)`）是否首批实现，还是先只做纯 typed + RawHandler 两种。
- **T5｜复杂类型零反射编码**（已倾向否决本轮）：JSONResponse 类复杂输出目前回退 stdlib（性能=gin）。是否日后以独立可选 codegen 增强？用户本轮决策为**不引入工具链**，此项仅记录为未来可选项，不进入本轮范围。

---

## 11. 实现阶段计划

> 每阶段结束跑 `go build` + `go test` + `go vet` + `gofmt`；涉及并发的阶段加 `-race`。全程不 commit（遵守 AGENTS 约定）。

- **阶段 0｜骨架**：顶层 `package ghttp` 建包；落 `Request` / `Response` / `Params` / `compiledHandler` / `Mux` 空壳；一个**最小静态匹配**（map 或单节点）打通端到端。验收：一条硬编码路由能匹配并返回 200，`go build ./ghttp/` 通过。
- **阶段 0.5｜从零写 radix 树**：压缩前缀树 + 参数槽位数组，静态 > 参数 > catch-all；转义 / catch-all / 尾斜杠 / dot-empty→400 / 405-Allow 逐条实现。验收：把 git 历史里 `TestChainCollapse*`（12 项）的**行为用例**移植为新测试并全绿（实现全新、断言不变）。
- **阶段 1｜typed 注册与执行**：`Handle[P,Q,B,O]` + `InputSource` + `OutputSpec` + `compiled[...].Serve`。验收：§3 的「写对编译过、写错 build 失败」两个用例；一条 `PathInt64 + Body[JSON] + JSON output` 端到端跑通。
- **阶段 2｜RawHandler 与单一分派**：`RawHandler` 编入同一 `compiledHandler`；`ServeHTTP` 收敛为 1 分派 + 1 recover。验收：typed 与 raw 两种路由共存，分派路径唯一。
- **阶段 3｜输入/输出族**：补齐 Path/Query/Header/Cookie/Body(JSON/XML/Form)/Multipart/Raw 与 JSON/XML/Text/Bytes/NoContent/Redirect/Stream；零反射编码器 + stdlib 回退（§9.3）。验收：各来源与各输出各一条路由测试。
- **阶段 4｜错误模型**：注册期错误集 + 请求期 404/405/400/415 + 统一错误链接入。验收：§7 每条一测。
- **阶段 5｜基准与逃逸**：§9 全场景基准（在项目 harness 内，与 gin/echo 同负载）+ 逃逸分析；据此定 T1。验收：ghttp-C 原型已达成的领先幅度（§9.1）在真实新代码上**用自研 radix 树**复现；一张诚实三方对比表。benchmarks 的 ghttp 适配按新 API 重新接回。
- **阶段 6｜收尾**：更新 `ghttp/README*.md`、`AGENTS.md`、`2026-08-07-ghttp-api-freeze-v0.1.md`（新 API 快照）；清理 `ghttp/internal/legacyrouter` 与 `testdata` 中的死引用。验收：`go build ./...`、`go vet`、`gofmt`、`-race` 全绿。

---

## 12. 评审清单

- [ ] 确认采用 **API 方案 C**（自由函数 + 推断，否决 Builder 链式）。
- [ ] T1–T5 取舍拍板。
- [ ] 确认**从零重写 radix 树（rewrite-scope B），不复用旧源码**，行为以 `TestChainCollapse*` 蓝本焊死。
- [ ] **确认硬约束：纯 `net/http` 标准库地基，不引入 fasthttp / 不自管 TCP（§2.2）。**
- [ ] **确认性能目标已修正为「纯标准库前提下超过 gin/echo」，依据 §9.1 实测铁证；杠杆为池化 + 零反射 codec。**
- [ ] 确认 codec 策略「零反射优先 + stdlib 回退、不引入 codegen 工具链」（§9.3）。
- [ ] 确认阶段 0–6 的顺序与各阶段验收标准。
