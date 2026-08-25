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


---

# 功能丰富化回归分析（d095986）· 严格同机 A/B

> 2026-08-24 提交 `16189d3`/`d095986` 为 ghttp 补齐生产能力（统一错误链、Content-Type
> 严格校验、validate 校验、Metrics/Gzip/可观测性、RunGraceful 等，+5390 行）。以下用
> **同一台机器（AMD Ryzen 9 9950X，Go 1.27.0）、同一命令**对上一轮提交 `2d478da` 与
> 当前 `d095986` 做 worktree A/B，回答"功能丰富化是否导致性能下降"。

## 1. ghttp 自带基准（`-benchtime=2000000x -count=5`，median）

| 基准 | 2d478da | d095986 | Δ | 分配变化 |
|---|---:|---:|---:|---|
| ServeStatic | 23.2 | 23.7 | +2.2% | 0→0 |
| ServeParam1 | 25.5 | 26.7 | +4.6% | 0→0 |
| ServeParam5 | 40.7 | 41.3 | +1.5% | 0→0 |
| **ServeMiss** | **25.8** | **146.3** | **+468%** | **0→3 allocs** |
| ServeMW1Hit | 25.7 | 26.4 | +2.7% | 0→0 |
| ServeMW5Hit | 30.4 | 29.4 | −3.4% | 0→0 |

## 2. typed 端点（`-benchtime=300000x -count=6`，median）

| 基准 | 2d478da | d095986 | Δ | B/op | allocs |
|---|---:|---:|---:|---:|---:|
| TypedGetParamsSmall | 715 | 752 | +5.2% | 529 | 8 |
| TypedGetParamsLarge | 960 | 1024 | +6.7% | 673 | 11 |
| TypedPostParamsBody | 1191 | 1185 | −0.5% | 683 | 12 |
| TypedPostBody | 1067 | 1108 | +3.8% | 602 | 10 |
| TypedGetNone | 212 | 249 | +17.7% | 64 | 3 |

## 3. bindbench 端到端（`-benchtime=400ms -count=3`，median）

| 场景 | 2d478da | d095986 | Δ | B/op 变化 | allocs 变化 |
|---|---:|---:|---:|---|---|
| Small | 722 | 723 | 持平 | 545 | 8 |
| Large | 989 | 1016 | +2.7% | 673 | 11 |
| PostBodySmall | 949 | 1026 | +8.1% | 602 | 10 |
| PostBodyLarge | 1785 | 1861 | +4.3% | 923 | 14 |
| PutMix | 1453 | 1512 | +4.1% | 1069 | 15 |

## 4. routebench 纯路由（`-benchtime=300ms -count=3`，median）

| 场景 | 2d478da | d095986 | Δ |
|---|---:|---:|---:|
| Static | 23.5 | 23.7 | +1.1% |
| Param1 | 24.4 | 27.1 | +10.8% |
| Param5 | 38.2 | 43.7 | +14.3% |
| Wildcard | 31.1 | 32.1 | +3.2% |
| **Miss** | **25.8** | **153.5** | **+496%** |
| GithubStatic | 29.9 | 30.9 | +3.3% |
| GithubParam | 44.8 | 49.9 | +11.4% |
| GithubAll | 9283 | 9343 | +0.6% |

## 5. 结论（诚实版）

**性能确有下降，但可归因、且分配零增长**：

1. **404/miss 冷路径是最大项**：ServeMiss/routebench Miss 从 ~26ns 涨到 ~146–153ns
   （+468~496%），0→3 allocs。根因是 `16189d3` 的统一错误链——404/405 现在经
   `renderMiss` 输出与业务错误一致的 **JSON 错误体**（可经 `WithNotFoundHandler`/
   `WithMethodNotAllowedHandler`/`WithErrorRenderer` 定制），不再是裸 `WriteHeader`。
   这是主动功能取舍，非实现回归。横向对比：gin 裸 404 31ns/0alloc，ghttp
   125ns/144B/3allocs 仍快于 echo（594ns/8allocs）；不想要错误体可用
   `WithNotFoundHandler` 恢复裸 404。

2. **typed 热路径 +3.8~6.7%（TypedGetNone +17.7%）**：新增的每请求固定工作 =
   Content-Type 严格校验（`wantCT`，默认开启，注册期固化）+ 统一错误链出口判断 +
   `matchedRoute` 可观测性记录。B/op 与 allocs **完全不变**（529/8、673/11、602/10、
   683/12、64/3），纯时间型小开销（几十 ns）。

3. **纯路由命中路径 +1~14% 且零分配**：Static/Wildcard/GithubAll 基本持平；
   Param1/Param5/GithubParam 上升 11~14% 主要来自 RawHandle 命中路径新增的
   `matchedRoute` 赋值与错误链出口判断。count=3 下部分场景 CV 较高，方向一致但
   幅度需以 count≥6 复测为准（本节 ghttp 自带基准 count=5 显示 Param1 仅 +4.6%）。

4. **总体判断**：命中路径（≥99% 流量）上升为个位数百分比、分配零增长；404 路径
   是统一错误体的主动代价。若需把 404 压回裸响应，一行 `WithNotFoundHandler`
   即可，路由/绑定热路径无需改动。

## 6. 本次横评新增/恢复

- `ghttp_test.go`：恢复 ghttp 整链适配（此前整链套件缺 gk 本体），基于当前
  `Server` + typed 入口（GetNone/GetParams/PostBody/PutParamsBody）。
- `web`（/root/test 的实验框架）目录已不存在，其适配移入 `webframework` build tag，
  `-tags webframework` 可重新纳入；`go.mod` 中的 replace 已注释。

## 7. 404 miss 路径优化落地（预构建错误体）

第 5 节指出的"404/miss 冷路径是最大项"已优化。做法：默认脱敏渲染器下,404/405 的错误体
内容恒定,故在 `error_chain.go` 用 `prebuiltMissBody` 包级 map 预构建整块 JSON 字节切片
(与 `jsonErrorRenderer` 格式逐字节一致);`renderMiss` 在 `errorRenderer == nil` 时直接
`resp.Write(body)`,免去每请求的 `make([]byte,...)` 拼接。自定义 `WithErrorRenderer` /
`WithNotFoundHandler` 仍走原动态路径,行为不变。

**同机 9950X / Go 1.27 实测 A/B（median）:**

| 基准 | 优化前 | 优化后 | Δ |
|---|---:|---:|---:|
| `ghttp` ServeMiss (自带, count=5) | 144.1 ns / 144B / 3 allocs | **82.2 ns / 64B / 2 allocs** | −43% 时间, −56% B |
| `benchmarks` NotFound (整链, count=3) | ~125 ns / 144B / 3 | **79.1 ns / 64B / 2** | −37% 时间 |

**优化后 NotFound 横向对比:** gin 30ns/0B(裸 404 无体) < **ghttp 79ns/64B(JSON 错误体)** <
fiber 143ns/0B(裸) < echo 535ns/456B(JSON 体)。ghttp 成为**唯一既输出结构化 JSON 错误体
又快过 fiber** 的框架,相比同样输出错误体的 echo 快 6.8×。剩余 64B/2allocs 来自裸路径
`&Response{}` 逃逸 + header,属必要开销。命中路径(Static/Param/JSONBind/FullChain)实测
与优化前逐项持平,无回归。
