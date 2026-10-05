# ghttp v3 端到端基准

- 日期：2026-10-05
- 机器：AMD Ryzen 9 9950X 16-Core（GOMAXPROCS=8），Windows 11 amd64
- Go：go1.27.1
- 命令：`go test -run '^$' -bench 'SimpleGET|RawGET|StdlibMux|GETWith|POSTJSON|EarlyReturn|MiddlewareStack|Routing|NotFound|MethodNotAllowed' -benchmem -benchtime=200ms .`
- 源码：`server_bench_test.go`。所有基准经 `Server.ServeHTTP`，使用可复用的 `benchWriter`（丢弃 body）与可重置的 `benchBody`，
  请求对象只构造一次，因此数字只含框架 + codec 开销，不含 httptest 分配。未使用访问日志中间件。
  单次运行结果有噪声（约 5-10%），仅用于量级与相对比较。

## 结果

| 基准 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| StdlibMux（GET /ping，ServeMux 对照） | 106.9 | 16 | 1 |
| RawGET（框架最低开销） | 235.3 | 200 | 3 |
| SimpleGET（typed，返回小 struct） | 771.8 | 344 | 8 |
| SimpleGETParallel（8 核，ns/op 为墙钟均摊） | 354.4 | 344 | 8 |
| GETWithPath（1 个路径参数 + Int64） | 991.6 | 376 | 9 |
| GETWithQuery（3 个查询参数） | 2309 | 936 | 16 |
| POSTJSON（解码 + 输出） | 2366 | 1171 | 17 |
| EarlyReturn（lazy，403 提前返回） | 1448 | 648 | 13 |
| EarlyReturnEager（先解码再校验，403） | 3560 | 1259 | 20 |
| MiddlewareStack（Recovery+RequestID+SecureHeaders+Observe） | 4001 | 936 | 21 |
| Routing/Static（约 100 路由） | 302.0 | 296 | 4 |
| Routing/Param1 | 275.9 | 296 | 4 |
| Routing/Param2 | 319.0 | 296 | 4 |
| Routing/Param3Deep | 315.5 | 296 | 4 |
| Routing/CatchAll | 326.8 | 296 | 4 |
| NotFound（404） | 953.5 | 488 | 11 |
| MethodNotAllowed（405） | 985.0 | 616 | 15 |

## 分析

### 框架 vs stdlib
- Raw 路径 235 ns / 3 allocs，约为 `http.ServeMux`（107 ns / 1 alloc）的 2.2 倍。多出的 2 个分配是每请求堆分配的 `Request` 与 `Response`，
  外加路由匹配（含 Raw 路径 4 allocs 的 Routing 基准，参数路由与静态路由开销几乎相同）。绝对值仍很低。
- typed 路径比 Raw 多约 540 ns / 5 allocs，这是 codec（JSON 序列化）与 lazy 包装的开销（见下）。
- 路由规模对查找影响很小：约 100 条路由下静态/参数/catch-all 均在 275-330 ns（含 Request/Response 分配），路由树本身查找为数十 ns 量级。
- 并行版本墙钟 354 ns/op，说明无明显全局锁竞争；分配数与串行一致。

### 分配来源（memprofile 归因）
SimpleGET 的 8 次分配：
1. `ServeHTTP` 中 `&Request{}`、`&Response{}`（2 次）
2. `router.lookup`（1 次，路由匹配结果/参数）
3. `newRequestOf` 的 `lazyBodyData`（1 次，即使 T 为 NoData）
4. `reflect.unsafe_New`（1 次，输出路径反射创建值，如 Output 序列化时 `reflect.New`）
5. `json.Marshal`（约 2 次，含返回切片）+ `bytes.Clone`（1 次，对 Marshal 结果再拷贝）
6. `Header().Set("Content-Type", ...)`（1 次，`[]string` 切片）

其他场景：
- GETWithQuery：`url.ParseQuery`（约 5 次：map、各 key/value 切片）与每个 `QueryValue` 的 `*Value`（`newNamedValue`，3 次）占大头，共比 SimpleGET 多 8 次分配。
- MiddlewareStack（多 13 次分配、约 3.2 us）：`RequestID`（`context.WithValue`、`http.Request.WithContext` 浅拷贝、hex 编码、
  Header.Set + canonicalMIMEHeaderKey）和 `SecureHeaders`（多次 `Header.Set`，每次一个 `[]string`）是主要来源。
- NotFound/MethodNotAllowed：错误路径需要构造 `ErrorResponse` + JSON 序列化；405 还需 `allowed()` 遍历各 method 树与 `strings.Join`。

### Lazy 收益
| 对比 | Lazy | Eager | 节省 |
|---|---:|---:|---:|
| 提前返回（403） | 1448 ns / 648 B / 13 allocs | 3560 ns / 1259 B / 20 allocs | 约 59% 时间、48% 内存、7 次分配 |

lazy 在提前返回场景收益显著（高于设计文档 12.2 的 24% 估算，因为本基准不含 httptest 开销，body 解码占比更大）。完整流程（POSTJSON）两者持平，因为都要解码一次。
解码本身约 1.1 us / 2KB 量级（EarlyReturnEager - EarlyReturn）。

## 可优化的分配热点（仅分析，未改实现）
1. **Request/Response 池化**：用 `sync.Pool` 复用 `Request` 与 `Response`（注意 handler 逃逸风险与 reset 语义），可省 2 allocs / 约 150 B 每请求，Raw 路径 3 -> 1。
2. **`bytes.Clone` 多余拷贝**：JSON 输出路径对 `json.Marshal` 结果再 Clone；可直接使用 Marshal 结果（或用池化 buffer + `MarshalWrite`/`Encoder` 直接写 Response），省 1 alloc 与一次拷贝。
3. **`reflect.unsafe_New`**：输出/编码路径中的反射创建值可按类型在编译期（`compileEndpoint`）预先决定，避免每请求反射分配。
4. **`lazyBodyData` 分配**：T 为 `NoDataType` 时无需分配 `lazyBodyData`（`lazyBody` 置 nil，`Data()` 做 nil 检查），或把 lazyBodyData 内嵌在栈上的 `RequestOf` 值内（`sync.Once` 可改用普通 bool，handler 内通常单线程）；可省 1 alloc。
5. **Header 写入**：`Header().Set("Content-Type", ...)` 每次分配 `[]string{v}`。可预先持有静态 `[]string` 切片并直接赋值 `h["Content-Type"] = ctJSON`（需保证不被修改），对 SecureHeaders 的多个固定头同理，可预计算一批 `[]string` 共享，预计 MiddlewareStack 省 5-7 allocs。
6. **RequestID 中间件**：`context.WithValue` + `WithContext`（浅拷贝 `http.Request`，约 250 B）无法避免，但可用 `Request` 自身字段存 ID 避开 WithContext；hex 编码可用 `[N]byte` 栈缓冲 + `string()` 单次分配；已有合法 ID 时应直接复用（不重新生成）。
7. **Query 解析**：`url.ParseQuery` 生成 `map[string][]string`。对 typed 单值读取，可实现一次扫描 `RawQuery` 的惰性查找器（不建 map），并让 `Value` 以值类型返回（避免 `*Value` 每次堆分配，3 次 -> 0）；预计 GETWithQuery 减少约 8 allocs。
8. **`router.lookup` 参数分配**：参数切片可使用定长小数组（如 `[4]Param`）内嵌在 `Request` 中，避免每请求分配；静态路由无参数时应保证 0 分配。
9. **错误路径**：404/405 可对常见错误响应缓存预序列化的 JSON 字节（`ErrNotFound`/`ErrMethodNotAllowed` 的固定 body），省去序列化与多次分配；405 的 `allowed()` + `strings.Join` 可在注册后按 path 缓存。
