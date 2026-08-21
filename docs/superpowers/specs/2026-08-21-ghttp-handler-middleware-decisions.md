# ghttp handler / middleware 执行模型拍板（阶段 1 前置）

- 状态：**已拍板**，据此实现。取代 `2026-08-19-ghttp-internal-new-typed-rewrite.md` §10 的 T1–T4「待评审」，并补齐该设计 §5.2 只一句话带过的**中间件类型契约**。
- 背景：地基（阶段 0/0.5，提交 `451463f`）已落地纯 net/http + gin 算法路由，执行签名收敛为
  `serve(ctx context.Context, req *Request, resp *Response) error`，**已无 `*Ctx`**。本文据此
  定死 handler 与 middleware 的类型模型，避免倒退回被删的 gin 式 `*Ctx`+`Next()` 双模型。

## T1｜输入容器：采用 `RequestInput[P,Q,B]`

```go
type RequestInput[P, Q, B any] struct { Path P; Query Q; Body B }
type (NoPath  struct{}; NoQuery struct{}; NoBody  struct{})
```

- 采用**具名字段容器**，而非扁平多参 `func(ctx, p, q, b)`。
- 理由：可扩展（日后加 Header/Cookie 组不改 handler 参数个数）；容器在 `compiled.serve`
  内栈上构造、按值同步传入业务函数，不逃逸（设计 §9.2 实测零额外成本）；具名字段可读。
- 复核点：阶段 5 逃逸分析；若显著上堆再切扁平。

## T2｜路由语法：对外只暴露 `{name}` / `{name...}`

- 用户只写花括号语法；`translateTemplate` 在注册期翻译为 gin 内部形式（`:name`/`*name`）。
- gin 式 `:id`/`*path` 仅为内部表示，不对外，避免两套写法。

## T3｜泛型壳只活在注册期（确认，非取舍）

- `Handle[P,Q,B,O]` 注册期编译出 `compiled[P,Q,B,O]`（实现 `compiledHandler`）进树。
- 请求期只做**一次接口分派**，热路径无泛型实例化、无反射。

## T4｜handler 形态：typed + RawHandler + 扩展形态

- **本项经用户评审裁决,采纳与初始推荐不同的方案**:除纯 typed 与 RawHandler 外,**保留
  扩展形态** `func(ctx, *Request, in) (O, error)`。
- 纯 typed：`func(ctx, RequestInput[P,Q,B]) (O, error)`（`Handle`/`Get`/...）。
- 扩展形态：`func(ctx, *Request, RequestInput[P,Q,B]) (O, error)`（`HandleReq`）——业务函数在
  typed 输入之外额外拿到 `*Request`(读原始头、访问底层 http.Request 能力),输出仍走
  OutputSpec。它与 Handle 共享输入绑定与类型擦除(`compiledReq`),进同一棵树、走同一条分派。
- RawHandler：`func(ctx, *Request, *Response) error`（完全接管响应,一等公民逃生入口)。
- 三者层次:typed(仅契约) < 扩展形态(契约 + 原始请求) < RawHandler(完全接管)。
- 初始推荐曾主张"扩展形态被 RawRequest() 输入源吸收、不单独实现",评审否决:显式扩展
  形态手感更直接,故第一批即实现 `HandleReq`。为控制 API 面,扩展形态先只提供带 method
  参数的 `HandleReq`(不铺开 5×2 个动词薄封装),后续按需再加。

## 中间件 / handler 统一执行模型（本轮实现重点）

### 类型契约：洋葱式闭包链，无 `*Ctx`

```go
// Handler 是链中一环,与 compiledHandler.serve 同签名。
type Handler func(ctx context.Context, req *Request, resp *Response) error

// Middleware 洋葱式包裹下一环;不调用 next 即短路(echo 式)。
type Middleware func(next Handler) Handler
```

- `Handler` 加 `serve` 方法即满足 `compiledHandler` → typed 终端与 raw 终端**同型进树**。
- 链在**注册期**折叠：`chain(terminal, mws)` = `mws[0](mws[1](…terminal))`，`mws[0]` 最外、
  最先执行。请求期无迭代状态（调用栈即状态），零每请求分配。
- 短路 = 不调 `next` 并自行写响应；错误 = `next` 返回值上冒统一错误链；后处理 = 调 `next`
  后继续。相较被删的 gin 式 `c.Abort()`/`c.Errors` 更简洁，且天然携带错误返回值。
- 传值用 `next(context.WithValue(ctx,k,v), req, resp)`，取代旧 `GetValue[string](c,key)` store。

### 挂载点：全局 `Use` + `Group`（不做 route 级 mw 变参）

- `mux.Use(mws...)`：全局中间件，**须在注册路由前调用**（注册期折叠，gin 同款约束）。
- `mux.Group(prefix, mws...)`：前缀 + 中间件；子组在创建时**快照**父组前缀与中间件
  （gin 语义，对齐 CHANGELOG「创建子组时快照」）。
- typed `Handle` 签名（设计 §4.1）不含 mw 变参，故不设 route 级中间件；单路由需要专属
  中间件时用一次性 `Group("", mw)`。保持 typed 签名纯净。

### Response 增强：status / written 追踪

```go
type Response struct { http.ResponseWriter; status int; written bool }
func (r *Response) Status() int   // 已写状态码
func (r *Response) Written() bool // 是否已提交
```

- 中间件（访问日志读状态码）与错误链（判断"是否已写"以决定能否写 500）都需要它。
- `WriteHeader`/`Write` 拦截记录 status/written；池化复用前 `reset`。

### 注册与请求链路（单一路径不变）

```
注册: Handle[P,Q,B,O](r, method, path, pSrc, qSrc, bSrc, out, h)
  → compiled[P,Q,B,O]{decodeP,decodeQ,decodeB,out,h}   // compiledHandler
  → terminal := Handler(compiled.serve)
  → folded := chain(terminal, r 的中间件栈)              // 注册期折叠
  → mux.handle(method, prefix+path, folded)             // 进树

请求: ServeHTTP → 匹配 → 池化 Request/Response → folded.serve(ctx,req,resp)
  → mw1 → next → mw2 → next → 终端:
       compiled.serve: decode P/Q/B → h(ctx,in) → out.encode(resp,o)
  → error 上冒 → (本轮)未写则 500;统一错误链留阶段 4
```

### `router` 密封接口统一 Mux 与 Group

```go
type router interface { register(method, path string, terminal Handler) error } // 包内密封
func (m *Mux)   register(...) { return m.handle(method, path, m.chainFor(terminal)) }
func (g *Group) register(...) { return g.mux.handle(method, g.prefix+path, chain(terminal, g.mws)) }
func Handle[P,Q,B,O any](r router, ...) error   // Mux/Group 通用,泛型自由函数
func Get/Post/... [P,Q,B,O any](r router, ...)  // 薄封装固定 method
```

## 本轮实现范围（"主要是 middleware 和 handler"）

- **完整**：`Handler`/`Middleware`/`chain` 折叠、`Response` status/written、`Use`/`Group`、
  `router` 密封接口、把 middleware 接入 `RawHandle` 与 typed `Handle`、内置中间件
  `RequestID`（前处理 + context 传值）与 `Logger`（后处理 + 读 status）。
- **最小真实纵切**：typed `Handle[P,Q,B,O]` + `InputSource`/`OutputSpec`/`Codec` 接口 +
  `PathString`/`PathInt64`/`NoInput`/`Body[T](JSON())` + `JSON[O]().Status()`/`NoContent()`，
  仅为证明 typed 终端与 middleware 端到端贯通。
- **不在本轮**：完整输入/输出源族（Query/Header/Cookie/Form/Multipart/XML/Text/Stream）、
  零反射 codec、OpenAPI 描述、统一错误链（400/415/Problem）——属阶段 3/4。

## 实现落地状态（2026-08-21，本轮完成）

**已完成**：
- `request.go`：`Response` 增加 status/written 追踪 + `WriteHeader`/`Write`/`WriteString`
  拦截 + `reset`；`mux.go` 池化复用处 reset。
- `middleware.go`：`Handler`/`Middleware`/`chain`（注册期洋葱折叠）/`router` 密封接口。
- `mux.go`：`Mux.Use`/`register`；`RawHandle` 改经 `register`（走中间件链）。删除冗余
  `rawHandler` 适配器（`Handler` 直接实现 `compiledHandler`）。
- `group.go`：`Group`（前缀拼接 + 中间件快照，含嵌套）、`Group.Use`/`RawHandle`。
- `typed.go`：`RequestInput[P,Q,B]`、`InputSource`/`OutputSpec`、类型擦除 `compiled[...]`、
  `Handle[P,Q,B,O]` + `Get/Post/Put/Patch/Delete` 薄封装、扩展形态 `HandleReq[P,Q,B,O]`
  （评审后裁决增加，`compiledReq` 承载）（`endpointSpec` 避让测试内
  `routeSpec`）。
- `input.go`：`NoInput`/`PathString`/`PathInt64`/`Body[T]` + `Codec`/`JSONBody()`。
- `output.go`：`JSON[O]().Status()`/`NoContent().Status()`（`JSONBody` 让名给输出 `JSON[O]`）。
- `middleware_builtin.go`：`RequestID`（前处理 + context 传值 + `RequestIDFromContext`）、
  `Logger`/`LoggerWith`（后处理 + 读 status）。
- 测试：middleware（顺序/短路/后处理读status/context传值/Group快照/嵌套）、typed（端到端/
  中间件贯通/NoContent/缺Output/解码错误/Put-Patch/注册错误）、Response（追踪/池化卫生/
  并发）、扩展形态（TestHandleReq* 4 用例：到手态/与 middleware 贯通/缺 Output/解码错）。`go test -race` 全绿，覆盖率 **89.0%**，gofmt/vet 干净。

**性能验证（隔离基准）**：
- RawHandle 无中间件命中 **21.6ns / 0 alloc**（路由地基零分配特性保持）。
- 3 层空中间件链 **24.2ns / 0 alloc** —— 证明**链折叠机制本身零分配**，每层仅 ~1ns
  纯闭包调用成本。
- RequestID 中间件的 6 allocs 来自其 `crypto/rand`+`hex`+`context.WithValue` 业务逻辑,
  与链机制无关。

**遗留 TODO（阶段 3/4，代码中已标注）**：
- `mux.go serve`：panic → 结构化 500；typed 终端返回的 error 目前顶层以 500 兜底,
  阶段 4 接统一错误链（400/415/Problem，据 `ErrInvalidInput`/`ErrMissingCodec` 映射）。
- `endpointSpec` 的 method/path 字段与 `Codec.contentType` 现仅记录，阶段 3 供 OpenAPI
  与 415 校验消费。
```

## 阶段 3 · Phase A：Query / Header 输入源族（2026-08-21）

### 三项设计裁决（经用户评审）

- **解析方式 = 显式字段声明（零反射）**。否决反射 struct tag（违背 §2.2 消灭反射的北极星）
  与"仅组合器"（单参数也要包结构体）。采用"值类型在闭包内擦除的字段绑定器"：
  `QInt/QStr/...` 各闭合自己的值类型与 setter，故不同类型字段能放进同一个
  `[]queryField[Q]`，`Query[Q](...)` 组合成一个 `InputSource[Q]`，零反射、编译期类型安全。
- **零反射 codec = 本阶段不做**。继续用 `encoding/json`；codec 优化留到有基准数据指向它
  是瓶颈时再做（符合"性能只在有数据支撑时优化"）。
- **本轮范围 = 先 Query + Header**，验证"显式字段声明"模式正确后再做 Form/Multipart。

### 两项细节裁决（经用户评审）

- **提供单值便捷源**：`QueryInt("n")`/`HeaderString("X-Token")` 直接返回 `InputSource[标量]`,
  单参数场景免包结构体；与组合器共用同一批解析原语（`toInt/toInt64/toBool`）。
- **支持 `.Required()`**：`QStr(name,set).Required()` 值接收器返回副本,缺失即
  `ErrMissingRequired`（新增哨兵,与 `ErrInvalidInput` 分开便于用户侧与阶段 4 区分
  "缺失"与"类型不符"）。单值源不支持 `.Required()`（需要必填用组合器）,保持单值路径最简。

### 已落地

- `input_convert.go`：共享标量转换器 `toInt/toInt64/toBool`（Query/Header/未来 Form 复用,
  统一 400 级错误消息, `errors.Is(err, ErrInvalidInput)` 可判）。
- `input_query.go`：`queryField[Q]` + `QStr/QInt/QInt64/QBool` + `.Required()` +
  `Query[Q](...)` 组合器 + 单值源 `QueryString/QueryInt/QueryInt64/QueryBool`。
- `input_header.go`：与 query 对称的 `headerField[H]` + `HStr/HInt/HInt64/HBool` +
  `Header[H](...)` + 单值源 `HeaderString/HeaderInt/HeaderInt64/HeaderBool`。取值走
  `req.Header.Values`（CanonicalMIMEHeaderKey 规范化,大小写无关）。
- `errors.go`：新增 `ErrMissingRequired`。
- 关键设计约束：**presence/required 判断收敛到 bind 循环, parse 闭包只处理"值已存在"的情形**,
  故 `.Required()` 值副本语义干净、可选字段零污染;空字段名统一在 bind 期返回
  `ErrInvalidParam`（与 `pathString` 一致,不 panic,全包错误通道统一）。
- 测试：组合器多字段端到端、单值源、`.Required()` 缺失/副本语义、类型解析失败（500 兜底）、
  header 大小写无关、与 middleware/typed 贯通、注册期空名、各类型解析错误分支、空组合器
  退化为 `NoInput`。`go test -race` 全绿，覆盖率 **90.9%**，gofmt/vet 干净。

### 待续（Phase B/C，未做）

- **Form**（`application/x-www-form-urlencoded`）：可复用 query 的字段绑定器模式,取值走
  `req.PostForm`（需先 `ParseForm`）。
- **Multipart**（`multipart/form-data`）：字段 + 文件流,需 `ParseMultipartForm` 与内存/临时
  文件阈值决策——形态更复杂,单列一轮。
- 待模式在 Phase A 验证无误后再动手（本轮已验证:显式字段绑定器 + bind 循环收敛
  presence 的模式可直接平移到 Form）。
