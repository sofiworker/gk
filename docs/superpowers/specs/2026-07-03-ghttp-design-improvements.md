# ghttp 改进计划

> 日期：2026-07-03
> 前置讨论：本文取代同日早先版本。早先版本把"通用框架最佳实践"套在 ghttp 上，提出了接口协议归一、双入口 builder 等与本框架设计承诺相悖的方案，已全部撤回（撤回记录见 §5）。
> 本文原则：**只列有实测缺陷支撑、且不动现有设计承诺的改动**。每项都标注证据与修法。
> 数据来源：`ghttp/perf_probe_test.go`（可复跑）、实际运行探针测试、测试与 example 的使用统计。

---

## 0. 设计现状的确认（先说不改什么）

以下是框架的既有设计承诺，本计划**全部保持不动**：

| 设计承诺 | 出处 |
|---------|------|
| `func(ctx, Req) (Resp, error)` handler 签名，无 gin 式 Context | a9c36d1 BREAKING CHANGE 有意移除 Context |
| `Params` 命令式输入，不做 struct tag 绑定 | gserver DESIGN_BINDING.md 时代探索过 tag 方案后有意放弃 |
| go-restful 风格单链 builder，`Route[Req,Resp](s).GET(...).To(...)` | builder.go:34 注释明示 |
| 状态码经 `StatusCoder` 接口或 `Status int` 字段 | output.go 现有双机制 |
| 结构化输出经 envelope；无 envelope 时错误为 `http.Error` 纯文本 | 有意的最小默认行为 |
| `ToRedirect`/`ToSSE`/`ToStatic*` 等独立终结器 | 现有 API，使用中 |

使用统计（测试 + example 全量，~90 处注册）：`.To()` 占终结器使用 75%，泛型链是绝对主路径；`Route[struct{}, struct{}]` 占 ~22%，是少数场景的噪音但不构成重构理由。

---

## 1. 性能优化（收益已实测，API 零变化）

### 1.1 数据

整链（GET /users/{id}?page=1，4 header + 2 cookie，in-process）：

| 链路 | ns/op | B/op | allocs/op |
|------|-------|------|-----------|
| raw handler（框架地板） | 282 | 432 | 5 |
| 当前 `To()` 类型化链路 | 3675 | 5445 | 29 |
| 优化原型（同功能，见 perf_probe_test.go） | **714** | **880** | **9** |

**5.1× 提速、分配 -84%，公开 API 与语义面向使用者零变化。**

### 1.2 改动项（按收益排序）

| # | 改动 | 单请求实测收益 | 说明 |
|---|------|---------------|------|
| 1 | `Params` 惰性化：持 `*http.Request` + 路由参数，Query/Cookie 首次访问解析并缓存 | ~1.1µs / 1.7KB / 13 allocs | 现状为无条件深拷贝全部 header/query/cookie（1399ns/2128B/17allocs），绝大多数 handler 只读 1–2 个值 |
| 2 | 输入构造注册期预编译：`To()` 时按 `reflect.Type` 生成构造闭包，消灭每请求 `newInputTarget`/`inputFromTarget` 反射双拷贝 | ~740ns / 2.1KB | 双拷贝 = reflect.New 一次 + `Elem().Interface()` 再整体拷贝一次 |
| 3 | codec 注册期缓存：`resolveProduces` 已在注册时验证 codec 存在，直接存进 handler 闭包 | ~63ns / 48B | 现状每请求带锁 map 查找 |
| 4 | Accept 协商结果缓存（仅 envelope 路径） | ~135ns / 112B | Accept 值种类极少，sync.Map 缓存即可 |

**实测否决，不做：** validator 快速路径——playground validator 对无 tag 结构体仅 95ns/0 allocs（内部有类型缓存），不值得。

### 1.3 Params 惰性化的语义代价（唯一需要拍板的点）

- 语义从"不可变快照"变为"handler 存活期内的视图"，handler 返回后不得留存。
- 跨 goroutine 留存提供显式 `Detach() Params`（执行现在的深拷贝，成本由需要者支付）。
- `Path/Query/Header/Cookie/ClientIP` 方法签名全部不变。
- README 中"`Params` 是请求输入快照"一句需同步改为视图语义。

### 1.4 验收基准

```
典型 GET 整链   ≤ 800 ns/op   ≤ 1 KB/op   ≤ 10 allocs/op   （当前 3675/5445/29）
POST+JSON 整链  ≤ 3.5 µs/op   ≤ 4 KB/op                    （当前 6103/10214/38）
raw handler     ~282 ns/op 不回归
```

基准配套修正：benchmark_test.go 中 recorder 移出循环（当前 160B/3allocs 全是 recorder 分配）；`BenchmarkFullServer` 增加 in-process 变体（当前 518µs/op 是 Windows 环回 TCP 主导，测不出框架差异）。

---

## 2. 缺陷修复（有实验或代码证据，均不改公开 API）

### 2.1 204/304 仍写 body（RFC 9110 违规）

实测：

```
Resp{Status: 204}  →  204, Content-Type: application/json, body "{}\n"   ← 违规
```

**修法**：`writeResponse` 中 status ∈ {204, 304} 或 1xx 时跳过 body 写出与 Content-Type 设置。envelope 路径同样适用。几行判断，零新增 API——204 的表达方式就用现有的 `StatusCoder` 或 `Status` 字段，无需哨兵类型/新终结器。

### 2.2 `Responds().With().Desc()` 静默错位

`Desc` 修改"最后一个 response"（builder.go:212-217）：先 `Desc` 后 `With` 时描述安到上一个 response 或丢弃，无报错。

**修法**：spec 暂存于 `responseSpecBuilder` 自身，`End()`（或下一个终结器）时提交。链式写法、公开 API 不变。

### 2.3 WebSocket 是 stub

服务端 `buildWebSocketHandler` 直接 501（builder.go:638-646）；客户端 `Client.WebSocket()` 返回内部 conn 为 nil 的空壳，调用即 panic（client.go:626-637）。README 却列为特性。

**修法二选一（待定）**：真实现（自写 RFC 6455 或引依赖），或从 README/公开 API 移除直到实现。不允许 stub 挂着。

### 2.4 客户端死 API

代码证据（client.go）：

- `Client.SetAuthToken` 设置的字段在 `R()` 不复制、`execute` 不读取——完全无效。
- `Request.Timeout` / `BasicAuthUser/Pass` / `ResultError` 声明后从未被读取。
- `R()` 复制 header/query 用 `r.Header[k] = v` 共享底层 slice，后续 `Add` 污染 client 级默认值。
- `execute` 中 `resp.BindJSON(r.Result)` 错误被吞。

**修法**：逐个落地或删除字段；aliasing 改为逐值拷贝；BindJSON 错误上抛。

### 2.5 服务端/客户端 envelope 假设不一致

服务端默认 `envelope == nil`（裸 JSON），客户端 `Do[Req,Resp]` 却先按 `{code,msg,data}` 解包（client.go:545-560）。同一个包的两端默认行为对不上。README"默认响应格式 {code,msg,data}"与服务端实际默认也不符。

**修法（方向待定）**：对齐两端默认值——要么服务端默认启用 `DefaultEnvelope`，要么客户端 `Do` 不预设 envelope 并让 README 如实描述。

### 2.6 请求体无大小限制

全包无 `MaxBytesReader`/`LimitReader`，`json.Decoder` 直接消费整个 body。

**修法**：`WithMaxBodyBytes(n)`（server 级，route 级可覆盖），解析前套 `http.MaxBytesReader`，默认值建议 4MB。

### 2.7 setup error 无法定位注册点

`recordSetupError` 仅 `errors.Join`，几十条路由中一条配置错误时 panic 信息无 method/path。

**修法**：错误包裹 `method path`（builder 里现成有），可选加 `runtime.Caller` 记录注册文件行号。

---

## 3. 小改进（低成本，顺手做）

| 项 | 证据 | 修法 |
|----|------|------|
| 405 无 `Allow` header；他 method 命中时返回 404 而非 405 | radix_router.go:114-133 | lookup 失败后扫其余 method matcher |
| CORS：origin 不匹配仍下发 Allow-* 头、无 `Vary: Origin`、credentials+`*` 不校验 | middleware.go:70-109 | 按 Fetch 标准修正 |
| Recoverer 不记录堆栈 | middleware.go:157-175 | `debug.Stack()` 进日志 |
| Timeout 仅包 context，超时不写 504 | middleware.go:186-195 | 参考 `http.TimeoutHandler` 语义 |
| `Params` 补 `QueryInt(key)` / `QueryIntDefault(key, def)` 等便捷方法 | 分页等场景手写 strconv 样板 | 纯增量 API，不引入 tag 体系 |

---

## 4. 执行顺序

```
批次 1（并行，无相互依赖）
  ├─ §1 性能优化（惰性 Params → 预编译 → codec 缓存 → 基准修正）
  └─ §2.2 / §2.6 / §2.7（Responds 修位、body 限制、setup error 定位）

批次 2（需先拍板方向）
  ├─ §2.1 204 修复（跟随 §1 的 writeResponse 改动一起落，避免两次动同一函数）
  ├─ §2.3 WebSocket：真实现 or 摘除
  └─ §2.5 envelope 默认值：对齐方向二选一

批次 3
  ├─ §2.4 客户端清理（可能伴随字段删除的破坏性变更，单独一个提交）
  └─ §3 小改进
```

每批独立可交付。批次 1 无任何待决问题，随时可开工。

---

## 5. 撤回记录（为什么早先版本的方案不成立）

留档防止同类方案再次被提出：

| 撤回项 | 撤回理由 |
|--------|---------|
| `HeaderWriter` 接口 | 全部测试与 example 中无一处需要从类型化 handler 设自定义 header；YAGNI。真有需求时 `ToHTTPFunc` 逃生舱已覆盖 |
| 双入口 builder（泛型/非泛型分离） | 使用统计 75% 走 `.To()`；为消掉 22% 场景的 `[struct{}, struct{}]` 噪音需复制整套 method 动词+元数据方法，成本收益倒挂，且违背 go-restful 单链承诺 |
| `ToRedirect` 归一到接口协议 | 现有实现能用、语义清晰，归一后对使用者可见变化为零，纯概念洁癖 |
| 错误响应强制走 codec | envelope 就是框架的结构化输出机制；无 envelope 时纯文本是有意的最小默认行为，不是缺陷 |
| struct tag 参数绑定 | gserver 时代已探索并有意放弃；简单场景 `req.Query("page")` 更直接 |
| validator 快速路径 | 实测 95ns/0 allocs，无优化空间 |
| `Returns()` 压平 Responds 链 | Responds 使用极少（example 1 处），修掉 §2.2 陷阱后收益趋近零；降级为可选 |
| 双状态码机制二选一（砍 `Status int` 字段） | 两机制共存无实测危害（反射有缓存）；撞名属理论风险，无真实案例；不值得破坏性变更 |
