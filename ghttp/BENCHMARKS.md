# ghttp v3 端到端基准

- 日期：2026-10-06
- 机器：AMD Ryzen 9 9950X 16-Core（GOMAXPROCS=8），Windows 11 amd64
- Go：go1.27.1
- 命令：`go test -run '^$' -bench 'SimpleGET|RawGET|StdlibMux|GETWith|POSTJSON|EarlyReturn|MiddlewareStack|Routing|NotFound|MethodNotAllowed' -benchmem -benchtime=500ms -count=5 -cpu=8 .`
- 源码：`server_bench_test.go`。所有基准经 `Server.ServeHTTP`，使用可复用的 `benchWriter`（丢弃 body）与可重置的 `benchBody`，请求对象只构造一次，因此数字只含框架和 codec 开销，不含 httptest 分配。单次运行结果有噪声，仅用于量级与相对比较。

## 结果

| 基准 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| StdlibMux（GET /ping，ServeMux 对照） | 70.4 | 16 | 1 |
| RawGET（框架最低开销） | 54.5 | 0 | 0 |
| SimpleGET（typed，返回小 struct） | 296 | 96 | 4 |
| SimpleGETParallel（8 核，ns/op 为墙钟均摊） | 150 | 96 | 4 |
| GETWithPath（1 个路径参数 + Int64） | 400 | 152 | 6 |
| GETWithQuery（3 个查询参数） | 417 | 96 | 4 |
| POSTJSON（解码 + 输出） | 1320 | 946 | 13 |
| EarlyReturn（lazy，403 提前返回） | 630 | 424 | 9 |
| EarlyReturnEager（先解码再校验，403） | 1450 | 1034 | 16 |
| MiddlewareStack（Recovery+RequestID+SecureHeaders+Observe） | 985 | 688 | 17 |
| Routing/Static（约 100 路由） | 129 | 120 | 2 |
| Routing/Param1 | 131 | 120 | 2 |
| Routing/Param2 | 142 | 120 | 2 |
| Routing/Param3Deep | 163 | 120 | 2 |
| Routing/CatchAll | 136 | 120 | 2 |
| NotFound（404） | 589 | 264 | 7 |
| MethodNotAllowed（405） | 772 | 416 | 12 |

## 分析

Raw 路径在可复用 writer 基准中为 54.5 ns / 0 allocs；typed 路径的主要成本来自 JSON 编解码，而不是路由树。静态路由已走无参数 lookup 分支，路由树在独立基准中为 129 ns / 2 allocs（含请求与响应包装）；参数路由保持 131 ns / 2 allocs，未因静态优化回归。并行版本墙钟约 132 ns/op，说明池化请求/响应和路由 lookup 没有引入全局锁竞争。

POSTJSON 的主要分配来自标准库 JSON decoder/encoder、`http.MaxBytesReader` 和 typed body 状态；当前为 930 B / 12 allocs。静态 typed GET 为 80 B / 3 allocs，其中 JSON 编码路径的反射值、编码结果和 body 拷贝仍是主要来源。路由查找自身不再为静态路径创建参数或回溯栈；参数路径仅按最大参数数分配参数切片。

GETWithQuery 使用值返回的 `QueryValue` 和惰性扫描；在当前基准中只比静态 GET 多一笔分配，避免了完整 `url.Values` map 和每个 accessor 的堆分配。MiddlewareStack 的主要成本仍来自 RequestID 的 context/request 包装和多个安全响应头；NotFound/MethodNotAllowed 的成本来自错误响应 JSON 与 Allow 列表生成。

### Lazy 收益

| 对比 | Lazy | Eager | 节省 |
|---|---:|---:|---:|
| 提前返回（403） | 630 ns / 424 B / 9 allocs | 1450 ns / 1034 B / 16 allocs | 约 57% 时间、59% 内存、7 次分配 |

lazy 在提前返回场景收益显著；完整流程需要解码时，成本主要由 JSON decoder 决定。

本轮已按 profile 落地的优化包括：Request/Response 池化、NoData 跳过 lazy body、值返回的 QueryValue、静态路由无参数 lookup，以及保存 `Input[T]` 接口避免方法值包装。JSON v2 的反射、标准库 decoder 和 header slice 分配仍需单独评估，当前没有证据支持替换编码器或改变响应 header 生命周期。
