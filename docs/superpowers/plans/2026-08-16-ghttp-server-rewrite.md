# ghttp Server Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 以当前 `/root/test` 的单棵 radix 路由、池化请求上下文和冻结后直接 dispatch 为核心，重写 `ghttp` 的 server 热路径，同时完整保留 ghttp 当前的 typed handler、HTTP 语义和 server 能力。

**Architecture:** 注册阶段继续使用现有 `routeRegistry` 与 `routeDefinition`，freeze 阶段把定义编译为一棵 method-aware compact radix tree。树的结构匹配、当前 method 优先级和 405 方法收集分开实现；叶节点保存已编译的 middleware/terminal/error metadata。成功 raw 路由保持无状态快路径，typed/body/错误恢复等路径按需取得池化 request state 和 response writer。Go 1.27 前后的公开 builder 继续由现有 build tags 分开，运行时只消费版本无关的 route definition。

**Tech Stack:** Go `net/http`、`sync.Pool`、`atomic.Pointer`、现有 ghttp codec/validator/OpenAPI/WS/SSE 能力；不新增第三方依赖。

---

### Task 1: 建立新路由树的行为基线

**Files:**
- Create: `ghttp/server_rewrite_test.go`
- Modify: `ghttp/route_mux.go`

- [x] **Step 1: Write failing tests**
  覆盖当前 method 的参数路由优先于其他 method 的静态路由、静态优先于参数、参数优先于 catch-all、catch-all 零段、HEAD 显式/GET 回退、strict trailing slash、escaped static segment，以及结构匹配方法集合。
- [x] **Step 2: Run focused tests and verify failure**
  Run: `go test ./ghttp -run 'TestServerTree|TestRouteMux' -count=1`
  Expected: 新测试在旧 route mux 上失败，失败原因必须是 tree API/语义缺失而不是测试编译错误。
- [x] **Step 3: Implement compact method-aware tree**
  将 method map + 每 method tree 替换为单棵 tree；静态子节点使用低扇出切片和高扇出首字节索引，节点保留 parameter/catch-all 子树；在 freeze 阶段构建排序和索引。成功 lookup 不分配，405 才允许构造方法集合。
- [x] **Step 4: Run focused tests**
  Run: `go test ./ghttp -run 'TestServerTree|TestRouteMux' -count=1`
  Expected: PASS。

### Task 2: 接入 compiled dispatch 与请求状态生命周期

**Files:**
- Modify: `ghttp/server.go`
- Modify: `ghttp/route_mux.go`
- Modify: `ghttp/writer.go`
- Create: `ghttp/server_dispatch_test.go`

- [x] **Step 1: Write failing lifecycle tests**
  覆盖 raw 无状态路由不注入 request state、typed/body 路由能读取 Params/Body、原始 request 的 Body 不被改写、请求状态在 panic/error handler 后才释放、HEAD 丢弃 body、hijack 不回收状态。
- [x] **Step 2: Run focused tests and verify failure**
  Run: `go test ./ghttp -run 'TestServerDispatch|TestFastPath|TestRequestState' -count=1`
  Expected: 新的生命周期断言失败。
- [x] **Step 3: Implement dispatch integration**
  用新 tree 返回 method-aware compiled route；保留 `compiledRoute` extractor 和 lazy path source。成功 raw 路由直接执行；其余路由按需创建 state。body 包装改为浅 request copy + 独立 Body 字段，避免深 Clone；Timeout/Recovery 所需的 mutex 和 drain 语义保持不变。
- [x] **Step 4: Run focused tests and race test**
  Run: `go test ./ghttp -run 'TestServerDispatch|TestFastPath|TestRequestState' -count=1`
  Run: `go test -race ./ghttp -run 'TestServerDispatch|TestRequestState|TestTimeout' -count=1`
  Expected: 两个命令均 PASS。

### Task 3: 补齐 HTTP 方法与错误协议语义

**Files:**
- Modify: `ghttp/server.go`
- Modify: `ghttp/route_mux.go`
- Test: `ghttp/server_routing_test.go`
- Test: `ghttp/server_full_test.go`

- [x] **Step 1: Add failing integration cases**
  真实 `httptest` 请求验证 404/405/Allow、OPTIONS 不误自动短路、HEAD 显式和 GET 回退、strict trailing、错误 writer/error handler、host validation、panic recovery。
- [x] **Step 2: Run tests and verify failures**
  Run: `go test ./ghttp -run 'TestServer.*(Method|Routing|Host|Panic|Error)|TestRouteMuxHEAD' -count=1`
- [x] **Step 3: Implement protocol-preserving outcome paths**
  保留 server middleware/skip 规则、error writer、真实 status code、HEAD body suppression；404/405 快路径只在无中间件/错误模型时启用。
- [x] **Step 4: Run integration tests**
  Run: `go test ./ghttp -run 'TestServer.*(Method|Routing|Host|Panic|Error)|TestRouteMuxHEAD' -count=1`
  Expected: PASS。

### Task 4: 保留 typed/API 能力并验证两个 Go 版本入口

**Files:**
- Modify: `ghttp/builder_core.go` only when dispatch metadata needs an adapter
- Test: `ghttp/builder_test.go`
- Test: `ghttp/builder_go127_test.go`
- Test: `ghttp/builder_terminals_test.go`

- [x] **Step 1: Add regression coverage**
  覆盖 pre-1.27 的 `Route[Req,Resp]`、Go 1.27 的 Server/Group verb chain、To/ToNoInput/ToNoOutput/ToHTTPFunc/ToSSE/ToWebSocket/ToStatic 注册到同一新 tree 后的行为。
- [x] **Step 2: Run the available toolchain**
  Run: `go test ./ghttp -count=1`
  Run: `go vet ./ghttp/...`
- [ ] **Step 3: Validate Go 1.27 build when available**
  Run: `go1.27 version && go1.27 test ./ghttp -count=1` when `go1.27` exists; otherwise report the unavailable toolchain and still compile-check all version-neutral files.
- [x] **Step 4: Fix only shared route-definition/dispatch adapters**
  Do not merge the two builder files or introduce generic parameters into `Server`/`Group`.

### Task 5: Fresh performance and concurrency verification

**Files:**
- Create: `ghttp/server_rewrite_benchmark_test.go`
- Modify: `ghttp/server_routing_benchmark_test.go` only if the new tree needs updated harness setup

- [x] **Step 1: Add fresh benchmarks**
  Measure raw static/param/catch-all, typed extraction, 404/405, middleware, body decode, and parallel dispatch with fresh request state. Record ns/op, B/op, allocs/op and iterations.
- [x] **Step 2: Run baseline before optimization changes**
  Run: `go test ./ghttp -run '^$' -bench 'BenchmarkServerRewrite' -benchmem -benchtime=1s -count=8`
- [x] **Step 3: Optimize from measured regressions**
  Only change hot structures shown by the fresh benchmark/profile; retain zero-allocation matcher behavior and avoid feature-specific shortcuts that change HTTP semantics.
- [x] **Step 4: Run final verification**
  Run: `gofmt -w` on touched Go files
  Run: `go test ./ghttp -count=1`
  Run: `go test -race ./ghttp -count=1`
  Run: `go vet ./ghttp/...`
  Run: `go test ./... -count=1`
  Run: `make check`
  Expected: all available scoped checks pass; `make check` is blocked by the
  pre-existing Go 1.27 generic-method files being parsed by the current Go
  1.26.2 formatter, and that toolchain boundary is reported separately.

### Task 6: 编译参数与 OpenAPI endpoint 元数据

**Files:**
- Modify: `ghttp/input.go`
- Modify: `ghttp/openapi.go`
- Modify: `ghttp/openapi_compiler.go`
- Modify: `ghttp/route_definition.go`
- Modify: `ghttp/builder_core.go`
- Test: `ghttp/server_rewrite_test.go`

- [x] 在注册期编译 path/query/header/cookie 字段绑定，逐请求不再扫描 tag。
- [x] 在 route definition 中冻结参数、请求体和响应 schema，OpenAPI 生成不再反射请求/响应类型。
- [x] 补齐标量 Body、catch-all、转义参数、重复 OpenAPI 生成与并发 dispatch 回归。
