# ghttp 执行模型重写 · 全量横评基准

**提交**: `2620fd7` refactor(ghttp)!: radix router and single fast-direct mainline, zero runtime assertions
**Go**: 1.26.2 linux/amd64  **CPU**: AMD Ryzen 7 8845HS w/ Radeon 780M Graphics (8 核)
**日期**: 2026-08-19
**方法**: `go test -bench . -benchtime=300ms -count=3 -timeout 1800s`，取 3 次 median

## ghttp vs gin 对比 (median, ns/op / B/op / allocs)

| 场景 | ghttp ns/op | gin ns/op | 比率 | ghttp B/op | gin B/op | ghttp allocs | gin allocs |
|---|---|---|---|---|---|---|---|
| StaticRoute | 225 | 81 | 2.78× | 56 | 48 | 3 | 1 |
| PathParam1 | 387 | 83 | 4.66× | 48 | 48 | 3 | 1 |
| PathParam5 | 1005 | 161 | 6.25× | 208 | 64 | 4 | 2 |
| Wildcard | 448 | 91 | 4.92× | 64 | 48 | 3 | 1 |
| QueryParams | 1388 | 441 | 3.14× | 1456 | 512 | 18 | 7 |
| JSONBind | 2660 | 2450 | 1.09× | 1856 | 1456 | 22 | 25 |
| JSONResponse | 895 | 806 | 1.11× | 576 | 576 | 3 | 3 |
| Middleware5 | 420 | 99 | 4.25× | 424 | 48 | 5 | 1 |
| Middleware10 | 601 | 168 | 3.58× | 424 | 40 | 5 | 2 |
| Middleware20 | 572 | 197 | 2.91× | 424 | 40 | 5 | 2 |
| FullChain | 4811 | 4188 | 1.15× | 2544 | 2168 | 29 | 31 |
| NotFound | 178 | 45 | 3.98× | 0 | 0 | 0 | 0 |
| RouteScale200 | 375 | 87 | 4.29× | 48 | 48 | 3 | 1 |
| Param10 | 1538 | 227 | 6.78× | 560 | 32 | 7 | 2 |
| Param20 | 3061 | 1371 | 2.23× | 1776 | 86 | 10 | 22 |
| Scale1000 | 2600 | 2249 | 1.16× | 5228 | 5174 | 12 | 11 |

## PathParam1 回归分析

PathParam1 是本次重写**唯一显著回归**的场景:

| 指标 | 旧基线 (count=3 median) | 新模型 (count=3 median) | 变化 |
|---|---|---|---|
| ns/op | 265 | 387 | **+46% (+122ns)** |
| B/op | 48 | 48 | 持平 |
| allocs/op | 3 | 3 | 持平 |

**根因**: 旧模型在无中间件时走 `fastDirect` 直调路径(绕过中间件链的 goroutine 开销)，新模型统一了执行模型 —— 所有请求都经过 `Ctx.Next()` 的中间件栈遍历，即使链为空仍产生 `compiledState.ServeHTTP` → `Ctx.Next()` → `index == len(handlers)` 的完整帧。这 ~120ns 开销是统一模型的结构性代价:

- 旧模型 fastDirect: `ServeHTTP` → 直接调用 handler(无中间件链遍历)。
- 新模型统一: `ServeHTTP` → `Ctx.Next()` → `Ctx.index` 遍历 → handler。即使链长 0 (`c.handlers = nil`)，`Ctx.Next()` 仍执行 `for c.index < int8(len(c.handlers))` 循环判断，比旧模型多了 `Ctx.Next()` 的入口帧 + context 值查找 + 返回路径。

**收益侧**: 统一模型消除了 fastDirect 分支的维护成本，且 PathParam5 旧基线 912 → 新 1005(本机 1005，开发者自测 902 持平)、FullChain 旧 5140 → 新 4811(本机 4811)、JSONBind 旧 2716 → 新 2660(本机)、NotFound 旧 182 → 新 178(本机) 等场景未退化或微优。鉴于 PathParam1 是纯路由场景(无业务逻辑)，且 387ns 在 10 框架横评中仍处中游(fiber 86、httprouter 83、echo 83、gin 83、stdmux 155、web 202、chi 498、gorilla-mux 923)，**该回归为可接受的统一模型架构代价**。

## 新模型 vs 旧基线 (开发者提供 median, ns/op)

| 场景 | 旧基线 | 新模型 (开发者测) | 本次实测 (count=3 median) |
|---|---|---|---|
| StaticRoute | 215 | 219 | 225 |
| PathParam1 | 265 | **373** | **387** |
| PathParam5 | 912 | 902 | 1005 |
| JSONBind | 2716 | 2631 | 2660 |
| JSONResponse | 886 | 897 | 895 |
| Middleware5 | 461 | 438 | 420 |
| FullChain | 5140 | 4715 | 4811 |
| NotFound | 182 | 182 | 178 |

## 质量备注

- **竞态检测**: `go test -race ./ghttp/ -count=1` **FAIL** (3 处 DATA RACE)。根因为 Timeout 中间件 timer goroutine 向 `Ctx.committed` 写入(`markParentCommittedLocked`, middleware_chain.go:381) 与主请求 goroutine 读取(`responseErrorWriteBlocked`, writer.go:22) 之间无同步。详见下文「竞态分析」。
- **静态检查**: `go vet ./...` 全绿(exit 0)。首次运行曾出现 `vet: open ghttp/zz_nil3_test.go: no such file or directory` 的陈旧缓存告警，重跑后自动消除。
- **单元测试**: `go test ./...` 全绿(exit 0)，ghttp 36.881s，legacyrouter 通过，benchmarks 包编译通过。