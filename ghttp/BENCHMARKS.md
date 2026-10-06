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
| SimpleGET (typed small struct) | 278 | 96 | 4 |
| SimpleGETParallel (8 workers) | 143 | 96 | 4 |
| GETWithPath (one path parameter) | 355 | 96 | 4 |
| GETWithQuery (one query parameter) | 391 | 96 | 4 |
| POSTJSON (decode and encode) | 1284 | 946 | 13 |
| EarlyReturn（lazy，403 提前返回） | 630 | 424 | 9 |
| EarlyReturnEager（先解码再校验，403） | 1450 | 1034 | 16 |
| MiddlewareStack（Recovery+RequestID+SecureHeaders+Observe） | 985 | 688 | 17 |
| Routing/Static（约 100 路由） | 129 | 120 | 2 |
| Routing/Param1 | 131 | 120 | 2 |
| Routing/Param2 | 142 | 120 | 2 |
| Routing/Param3Deep | 163 | 120 | 2 |
| Routing/CatchAll | 136 | 120 | 2 |
| NotFound (404) | 114 | 0 | 0 |
| MethodNotAllowed（405） | 772 | 416 | 12 |

## 分析

Raw ?????? writer ????? 54.5 ns / 0 allocs?typed ????????? JSON ??????????????? Request ??????????????????????? lookup ?? Params???????????????????

POSTJSON ???? 946 B / 13 allocs?????????? JSON decoder/encoder?http.MaxBytesReader ? typed body ????? typed GET ???? 96 B / 4 allocs?JSON ???????????

GETWithQuery ?????? QueryValue ??????????? 96 B / 4 allocs?MiddlewareStack ???????? RequestID ? context/request ???????????MethodNotAllowed ????????? JSON ? Allow ??????? 404 ????????????

### Lazy 收益

| 对比 | Lazy | Eager | 节省 |
|---|---:|---:|---:|
| 提前返回（403） | 630 ns / 424 B / 9 allocs | 1450 ns / 1034 B / 16 allocs | 约 57% 时间、59% 内存、7 次分配 |

lazy 在提前返回场景收益显著；完整流程需要解码时，成本主要由 JSON decoder 决定。

本轮已按 profile 落地的优化包括：Request/Response 池化、NoData 跳过 lazy body、值返回的 QueryValue、静态路由无参数 lookup，以及保存 `Input[T]` 接口避免方法值包装。JSON v2 的反射、标准库 decoder 和 header slice 分配仍需单独评估，当前没有证据支持替换编码器或改变响应 header 生命周期。
