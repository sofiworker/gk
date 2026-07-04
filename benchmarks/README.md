# webbench — ghttp vs 主流 Go Web 框架基准测试

本目录是一个**独立 Go module**（不污染主仓库依赖），使用 Go 标准 `testing.B`
基准框架 + [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
对比方法论，参照社区通行的
[julienschmidt/go-http-routing-benchmark](https://github.com/julienschmidt/go-http-routing-benchmark)
与各框架官方 benchmark 的写法（进程内 `ServeHTTP` + 可复用 mock writer），
评估 `ghttp` 的路由与整体请求链路相对其它框架的性能差距。

## 参评框架

| 名称 | 版本 | 类型 | 说明 |
|------|------|------|------|
| `ghttp-radix` | 本仓库 | 泛型类型化 | ghttp 默认 RadixRouter |
| `ghttp-std` | 本仓库 | 泛型类型化 | ghttp + Go 1.22 ServeMux 适配路由器 |
| `stdmux` | 标准库 | 基线 | `net/http` ServeMux（Go 1.22+ 方法/模式路由）|
| `gin` | v1.12 | 传统 | 最流行的 Go Web 框架 |
| `echo` | v4.15 | 传统 | labstack/echo |
| `fiber` | v3.4 | fasthttp | 非 net/http 栈，见下方公平性说明 |
| `hertz` | v0.10 | netpoll | 字节跳动 CloudWeGo，见下方公平性说明 |
| `chi` | v5.3 | 传统 | 最贴近标准库的路由器 |
| `gorilla-mux` | v1.8 | 传统 | 经典正则路由 |
| `httprouter` | v1.3 | 路由器 | gin 等框架的 radix 树鼻祖 |
| `bunrouter` | v1.0 | 现代 | uptrace 出品的零分配路由器 |
| `go-restful` | v3.13 | 资源化 | Kubernetes apiserver 使用的 REST 框架 |
| `huma` | v2.38 | 现代 OpenAPI | 类型化 handler + JSON Schema 校验（humago/ServeMux 适配）|
| `fuego` | v0.19 | 现代 OpenAPI | 泛型 handler，架构上与 ghttp 最接近的对照组 |

精确版本以 `go.mod` 为准。所有框架注册**完全相同的路由表**，handler 语义一致，
并由 `TestScenarioSanity` 逐场景断言状态码，保证对比的是等价工作量。

## 测试点位（13 组基准）

| 基准 | 场景 | 考察点 |
|------|------|--------|
| `BenchmarkStaticRoute` | `GET /ping` | 静态路由匹配 + 最小响应 |
| `BenchmarkPathParam1` | `GET /users/{id}` | 单路径参数提取 |
| `BenchmarkPathParam5` | `GET /orgs/{org}/.../perms/{perm}` | 深层 5 参数路由 |
| `BenchmarkWildcard` | `GET /files/{path...}` | 通配符/catch-all 匹配 |
| `BenchmarkQueryParams` | `GET /search?q=&page=&limit=` | Query 解析（3 个）|
| `BenchmarkJSONBind` | `POST /users` + JSON body | 请求体解码 + JSON 响应 |
| `BenchmarkJSONResponse` | `GET /profile` | 中等结构体（12 字段嵌套）序列化 |
| `BenchmarkMiddleware5` | `GET /mw/ping` | 5 层空中间件链开销 |
| `BenchmarkFullChain` | `PUT /api/v1/users/{id}/orders?...` | **整体链路**：路径参数 + 2 Query + 2 Header + JSON 绑定 + JSON 响应 |
| `BenchmarkRouteScale200` | 200 条注册路由混合命中 | 路由表规模化后的查找成本 |
| `BenchmarkNotFound` | 未注册路径 | 404 未命中路径 |
| `BenchmarkStaticRouteParallel` | 同 static | `RunParallel` 并发争用 |
| `BenchmarkFullChainParallel` | 同 full_chain | 并发下的整体链路 |

基准按**场景分组、框架为子项**命名（`BenchmarkFullChain/gin-8`），
同一场景内逐行直接可比。

## 纯路由匹配套件（routing_test.go）

除整链场景外，`BenchmarkRouting*` 系列单独测**纯路由匹配**：空 handler、无绑定无序列化，
只计路由查找 + 参数捕获 + 分发，方法论与
[julienschmidt/go-http-routing-benchmark](https://github.com/julienschmidt/go-http-routing-benchmark) 一致，
并使用其权威的 **GitHub API 203 条路由表**（`routing_github_test.go`）：

| 基准 | 说明 |
|------|------|
| `BenchmarkRoutingStatic` / `Param1` / `Param5` / `Wildcard` / `Miss` | 4 条微型路由表上的基本匹配形态 |
| `BenchmarkRoutingGithubStatic` / `GithubParam` | 203 条表上的单点命中（经典点位） |
| `BenchmarkRoutingGithubAll` | 每 op 扫全 203 条请求（ns/op 含 203 次匹配） |

```bash
go test -run TestRoutingSanity -v          # 正确性
go test -run xxx -bench BenchmarkRouting -benchmem
```

注意：hertz 经 `ut.PerformRequest`（每请求分配 recorder）、fiber 经 fasthttp
RequestCtx 直调，与 net/http 系的 mock-writer 路径存在固定差异，横向解读时
同整链套件的公平性说明。

## 运行

```bash
cd benchmarks

# 正确性检查（先跑这个）
go test -run TestScenarioSanity -v

# 全量基准
go test -run xxx -bench . -benchmem -count 1

# 只跑某个场景
go test -run xxx -bench 'BenchmarkFullChain$' -benchmem

# 只看 ghttp vs gin vs fiber
go test -run xxx -bench 'BenchmarkFullChain$/(ghttp|gin|fiber)' -benchmem

# 严谨对比：多次采样 + benchstat
go test -run xxx -bench . -benchmem -count 10 > new.txt
go install golang.org/x/perf/cmd/benchstat@latest
benchstat new.txt          # 或 benchstat old.txt new.txt 对比改动前后
```

仓库根目录也提供了快捷方式：`make webbench`、`make webbench-sanity`。

## 方法论与公平性说明

**进程内压测**：net/http 系框架直接调用 `handler.ServeHTTP(mockWriter, req)`，
请求对象在循环外预构建、body 通过 `bytes.Reader.Reset` 复用，响应写入可复用的
丢弃型 writer——与 go-http-routing-benchmark 一致，测出的是纯框架开销
（路由匹配、上下文分配、绑定、序列化），不含网络与 HTTP 解析。

以下差异是**框架设计使然**，解读数字时需要知道：

- **fiber**（fasthttp）：无法走 `http.Handler`，基准直接以复用的
  `fasthttp.RequestCtx` 调用 `app.Handler()`，这也正是它生产环境的快路径；
  它省掉了 net/http 的请求/响应模型，天然占优。
- **hertz**（netpoll）：走官方 `ut.PerformRequest` 进程内测试通道，该助手
  **每次请求都会分配一个响应 recorder**，其 ns/op 与 allocs/op 含有这部分
  固定放大，横向比较时对 hertz 需宽容看待；真实网络性能请以
  [hertz-benchmark](https://github.com/cloudwego/hertz-benchmark) 为准。
- **类型化框架做的事更多**：`ghttp`、`huma`、`fuego`、`go-restful` 的设计
  就包含输入绑定/校验、类型化输出、（huma）JSON Schema 校验等，per-request
  工作量天然大于裸路由器（httprouter/bunrouter/chi）。这正是本套件想量化的
  "抽象税"。
- **ghttp 默认 envelope**：ghttp 响应默认包一层 `{code,msg,data}` JSON
  信封，比其它框架多序列化 ~30 字节，属于其默认链路的一部分，按默认行为测。
- **进程内 ≠ 端到端**：本套件不含网络栈。若要评估真实吞吐（fiber/hertz 的
  网络层优势才会体现），请另行用 `wrk`/`hey`/`bombardier` 压真实端口，或参考
  [smallnest/go-web-framework-benchmark](https://github.com/smallnest/go-web-framework-benchmark)、
  [TechEmpower](https://www.techempower.com/benchmarks/)。

其它控制项：gin 处于 ReleaseMode；fuego 关闭了默认请求日志中间件（slog +
UUID request-id）；hertz 日志级别调至 Error；所有 JSON 均使用各框架默认的
序列化器（gin=sonic/encoding-json 视构建标签、hertz=默认 json，其余多为
标准库 encoding/json）。

## 新增框架

1. 新建 `<name>_test.go`，按共享路由表注册全部场景（抄任意现有适配文件）；
2. net/http 系框架直接 `register(&httpTarget{n: "name", h: handler}, nil)`；
   非 net/http 栈实现 `target` 接口（参考 `fiber_test.go`/`hertz_test.go`）；
3. 某场景不支持时通过 `register` 第二参数 `map[场景key]原因` 标记跳过；
4. 跑 `go test -run TestScenarioSanity -v` 确认全绿后再看基准数字。
