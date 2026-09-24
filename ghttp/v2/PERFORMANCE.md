# v1/v2 性能对照（2026-09-23）

2026-09-24 新增 [六条实现路径扩展报告](../../benchmarks/FACADE_V2_RESULTS.md)，涵盖多个长 path/query、POST/PUT/PATCH/DELETE、表单、大 JSON 和 multipart 文件。新矩阵重置请求及响应状态并包含内容摘要工作，与下文旧基准的绝对耗时不可直接比较。

同机 linux/amd64、AMD Ryzen 7 8845HS，Go benchmark -8。五轮各 300ms 的中位数；原始输出见 benchmark_results.txt。

| 场景 | v1 ns/op | v2 ns/op | v2 耗时变化 | v1 B/op | v2 B/op | v1/v2 allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 无输入 GET + JSON | 266.3 | 350.4 | +31.6% | 64 | 72 | 3 / 3 |
| path + query GET + JSON | 562.7 | 941.9 | +67.4% | 96 | 536 | 4 / 8 |
| JSON POST body | 1455 | 1561 | +7.3% | 601 | 737 | 10 / 13 |
| 建 server 并注册一条参数路由 | 1279 | 1451 | +13.4% | 1664 | 1728 | 22 / 23 |

## 测量边界

comparison_bench_test.go 将两个版本放入同一个基准，使用相同底层路由器、成功请求及响应数据、可复用丢弃 writer。计时不含请求对象与 recorder 构造；POST 两边均在每轮构造 Body reader，包含该开销。测试先验证状态码及响应字节一致。计时为单 goroutine 串行 ServeHTTP，不含真实网络、TLS、数据库和并发竞争。

默认 codec 和框架行为仍非完全等价：v2 的请求体限制、输入绑定、JSON 编解码路径与 v1 不同；v1 还包含自己的响应缓冲与路由元数据。因此这是默认端点执行成本对比，不是纯泛型门面开销比较。注册场景包含 server 创建和路由挂载，不是纯反射成本。

v2 现有 BenchmarkRouteExecution 包含每轮 httptest recorder 分配，不能直接和 v1 原来的 discard writer 测试相比。此次使用专门配对基准纠正该差异。

参数场景分配明显增加。源码上 v2 使用通用 query 解析及反射字段写入，v1 有绑定计划和 query 优化；这些是候选优化方向，尚未用 profile 证明每项开销占比。没有通过删校验或改变输出协议进行性能优化。

## 复现

```sh
go test ./ghttp/v2 -run TestComparisonContracts -count=1
go test ./ghttp/v2 -run '^$' -bench '^BenchmarkV1V2' -benchmem -count=5 -benchtime=300ms
```

不同时运行其他基准。此结果仅覆盖三种常见请求和一项注册场景，未覆盖文件上传、XML、Group、middleware 或业务 handler。

## 输入执行链调整后的复测

Go 1.27.1 linux/amd64，同一机器，五轮各 500ms 中位数。原始数据见 benchmark_input_results.txt。先执行 TestComparisonContracts 验证成功响应一致，基准与上次使用同一套配对场景，没有并行运行其他基准。

| 场景 | 当前 v1 ns/op | 当前 v2 ns/op | v2 耗时增加 | v1/v2 B/op | v1/v2 allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| 无输入 GET | 267.2 | 343.4 | 28.5% | 64 / 72 | 3 / 3 |
| path/query GET | 529.1 | 867.9 | 64.0% | 96 / 552 | 4 / 9 |
| JSON POST | 1337 | 1561 | 16.8% | 601 / 737 | 10 / 13 |
| 创建服务并注册参数路由 | 1298 | 1639 | 26.3% | 1664 / 2000 | 22 / 27 |

相对上一份记录，v2 参数 GET 中位数从 941.9 降至 867.9 ns/op，但 v1 本次也更快，不能把全部变化归因于优化。更明确的是 v2 该场景增加了 16 B 和一次分配（536/8 → 552/9），当前改造尚未实现减少分配的目标。

注册成本增加符合新增编译闭包与输入/字段执行计划的方向，但尚未 profile 定量归因。此前至今还包含 Group 注册改造，不能视为只改一个变量的严格前后实验。此次注册基准使用单 Route.Mount，不测 Group.Register 批量编译。请求基准只覆盖默认自动绑定，不测 DecodeWith 手写 decoder 或指针输入场景。

现阶段只能确认架构职责更集中，不能声称性能整体改善。下一步需用 CPU/alloc profile 定位参数绑定中的 query 分配、输入逃逸和闭包调用成本，再决定优化。

## DecodeWith 手写绑定对照

针对 path ID + query page/filter 的同一请求，五轮各 500ms 中位数：

| 路径 | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| v1 自动绑定 | 523.1 | 96 | 4 |
| v2 自动绑定 | 889.4 | 552 | 9 |
| v2 DecodeWith 手写绑定 | 782.9 | 504 | 7 |

DecodeWith 相比本轮 v2 自动绑定耗时下降 12.0%，少 48 B 和 2 次分配；仍比 v1 慢 49.7%。手写 decoder 使用与 v2 自动绑定相同的 req.Query 解析，保留整数校验，并真正读取 ID、page 和 filter，没有省略字段或预缓存请求值。对照契约测试验证响应一致，额外验证 page 读取和非法整数报错。

这证明手写绑定路径有收益，但未消除 query 解析、编码和执行器成本。结论只适用于此值类型参数场景，不外推到指针输入或 JSON Body。原始输出见 benchmark_decodewith_results.txt。

复现：`go test ./ghttp/v2 -run '^$' -bench '^BenchmarkDecodeWithComparison$' -benchmem -count=5 -benchtime=500ms`。

## DecodeWith 分配根因定位

分别采集 v1 与 DecodeWith 的 2 秒 CPU/alloc profile，避免用采样过程的耗时替代之前无 profile 的对照数据。DecodeWith 分配字节约 84.5% 来自 Request.Query → URL.Query → ParseQuery；该链累计占本次 CPU 样本约 30.4%（累计占比不可与其子节点相加）。

独立 BenchmarkQueryAllocation 使用相同 `page=2&filter=golang`，三轮结果 262–269 ns/op、432 B/op、4 allocs/op。v1 的 typed params 执行器使用 query_lazy.go 中 newQuerySource/lazyQueryFirst，在这个短 query 场景不构造完整 map；DecodeWith 手写函数仍调用 req.Query，因此没有继承这个优化。

DecodeWith 全链 504 B/7 次中，query 自身就是 432 B/4 次；其余 72 B/3 次与 JSON 输出路径一致。alloc profile 进一步定位 contracts.go encodeJSON：any(value) 的 Reply 接口判断和 Encoder.Encode(value) 都产生分配，JSON 内部还有 reflect.New。故不能把剩余分配归因于输入闭包。

对 v1 的差额应理解为净值：query 增加 432 B，但其他路径净少 24 B，最后表现为 504−96=408 B。v1 还有输入实例、JSON 输出和 Header.Set 等分配，而默认输出路径与 v2 不相同。

结论：DecodeWith 去掉了自动字段写入的反射和部分临时分配，但没有替换 query 数据结构和输出路径，所以总耗时只下降约 12%。下一步应优先复用 v1 经语义测试的惰性 query 能力，再把 Reply 类型判断移到注册期；不建议首先围绕闭包做优化。当前未修改运行时实现。

注意配对 benchmark 复用 writer header；v2 条件设置 Content-Type 的路径因此摊销掉后续 Header.Set，v1 每次设置。结果适合解释该基准，不代表真实网络请求分配的完整上界。

## 优化后：惰性 query 与注册期 Reply 分派

新增 Request.QueryFirst/QueryValues 复用 v1 已有 lazy query 实现；超过既有键数阈值回退到缓存 map。DecodeWith 示例改为 QueryFirst；v2 自动绑定改为 QueryValues。JSONOutput 在构造时判定类型是否实现 Reply 协议，普通输出不再进行该接口装箱。

五轮 500ms 中位数，原始输出 benchmark_decodewith_optimized.txt：

| 路径 | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| v1 | 521.7 | 96 | 4 |
| v2 自动绑定 | 629.7 | 128 | 6 |
| v2 DecodeWith | 445.1 | 48 | 2 |

DecodeWith 对比优化前 782.9ns/504B/7 次，耗时下降 43.1%，字节下降 90.5%，少 5 次分配；同轮比 v1 耗时低 14.7%。仅限当前短 query、值类型 DTO 的基准，不代表所有路由。默认输出缓冲与响应头行为差异仍适用前述测量边界。此次同时调整了 query 策略和输出类型分派，不能将全部收益归给其中单项。

QueryFirst 保留缺失/空值、首个有效重复值及标准库转义语义；已补标准库对照测试，包含非法转义、分号、Unicode、重复键、缓存与大 query 回退。

## RequestInput 同口径验证

此前约 490–527 ns 的独立基准只读取 header 和 query，并没有调用 Sources；不能把跨轮波动解释成 Sources 优化收益。

本次 BenchmarkRequestInputComparison 所有分支均读取 id/page/filter，转换两个整数，输出全部三个字段；每次清空响应头。请求构造在计时外，三轮 300ms。契约测试核对完整响应。仅覆盖值类型 RequestInput 和短 query，不外推到指针、cookie 或长 query。

注册时直接选择 RequestInput 构造函数，替代通用 Decoder 的临时目标对象，分配由 88 B/4 次降到 80 B/3 次。没有关闭验证，也未引入 unsafe 或代码生成。

| 路径 | 中位数 ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| DTO 自动绑定 | 630.2 | 112 | 4 |
| DecodeWith | 546.0 | 80 | 3 |
| RequestInput 值 | 562.3 | 80 | 3 |
| RequestInput 值 + Sources | 543.9 | 80 | 3 |
| RequestInput 值 + 强制 Query map | 847.4 | 512 | 7 |

Sources 与直接访问差异不应解释为显著优势；它们复用相同底层访问器。两个 query 字段时，强制 map 没有收益，故未将其设为默认或新增 QueryIndex API。原始结果见 benchmark_request_view_before.txt 与 benchmark_request_view_after.txt。

## 普通 HTTP 主线补全后矩阵

三轮 200ms 中位数（同轮比较），完整输出在 ../../benchmarks/results/facade-mainline-2026-09-24.txt。保持原矩阵负载与契约：含 JSON 摘要、每次重置响应头，Gin/Echo 列仍为共用编解码器适配而非原生 Bind。本轮恢复所有默认 multipart 清理，新增 validator/内容协商分支默认不启用。

| 场景 | v1 ns/op | v2 ns/op | DecodeWith ns/op | v2 B/op / allocs |
| --- | ---: | ---: | ---: | ---: |
| 短 path/query | 3585 | 3584 | 3133 | 1608 / 17 |
| 24 对 query | 3767 | 3827 | 3610 | 1608 / 17 |
| POST JSON | 2875 | 3151 | 2998 | 1776 / 25 |
| POST 表单 | 2523 | 2213 | 1949 | 1720 / 22 |
| 1 MiB JSON | 4116507 | 4095952 | 3993577 | 6728126 / 4114 |
| 1 MiB 文件 | 1330839 | 1396688 | 1344192 | 4238645 / 88 |

默认 v2 在本轮短参数与 24 对 query 上接近 v1；POST 表单更快，小 JSON 仍慢约 10%，文件成本主要来自标准 multipart。不能将不同轮次的绝对时间变化完全归因于代码修改；40 MiB 的 200ms 采样只有少数操作，本轮不据其排名。
