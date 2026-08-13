# ghttp Lazy Body 反序列化设计(定稿)

## 背景与目标

现状:类型化路由在进入用户 handler 前,eager 地读取请求体并用标准库
`encoding/json` 全量解码到名为 `Body` 的字段(`input.go` 的 `parseBody`)。
即使 handler 根本不读 body,每个请求都全额支付读流 + 全量反序列化 + 大对象
分配;大 body + 多并发时内存/GC 压力明显。动态请求访问(原始 request、全部
query/header)目前只能匿名嵌入 `ghttp.Params` 或降级到 `ToHTTPFunc`。

目标:body 改为“访问时才解码”,与已落地的 lazy path params 同一主线:

1. 匹配后包装标准 request,body 首次被读时缓存原始字节(共享,只读一次);
2. handler 通过任意命名的 `Body[T]` 字段(类型识别,不再靠字段名)按需 `Decode()`;
3. `Params` 增加 `Request/Method/URL/ContentType/RawBody`,匿名嵌入即可做动态访问;
4. middleware 通过 `ghttp.RawBody(r)` 共享同一份 body 字节;
5. 从不读 body 的请求零成本。

## 已确认决策

1. **输入模型(第 1 点)**:tag 绑定静态标量(保留,纯值读取零噪音)+ 匿名嵌入
   `Params` 管动态/原始访问 + 任意命名的 `Body[T]` 惰性 body。不引入新的
   `RequestView` 类型,直接把能力加进 `Params`(非破坏性)。
2. **JSON 实现**:按需访问与惰性解码的 JSON 统一用 `goccy/go-json v0.10.6`
   (纯 Go、跨平台一致、无汇编回退分支);按需取值提供 `CompileJSONPath` 薄层。
3. **一致性取舍**:接受 goccy 与 stdlib 的 int 溢出/float/UTF-8 差异并文档标注;
   `WithBodyDecoder` 作为逃生口。

## 实测依据(2026-08-13,AMD 8845HS,go1.26.2,`/root/gk-cmp`)

100KB / 700 项文档,全量解码到结构体:

| 库 | ns/op | B/op | allocs |
| --- | --- | --- | --- |
| encoding/json | 1037µs | 292KB | 2821 |
| goccy | 196µs | 272KB | 703 |
| sonic | 199µs | 267KB | 16 |
| jsoniter | 256µs | 271KB | 2814 |

100KB 文档只取一个字段(按需):

| 库/方式 | ns/op | B/op | allocs |
| --- | --- | --- | --- |
| goccy `CreatePath("$.status").Extract`(path 已编译) | 131µs | 156KB | 7 |
| gjson 每次 parse+Get | 58µs | 156KB | 1 |
| sonic.Get | 7µs | 8B | 1 |
| goccy `CreatePath` 编译一次 | 336ns | 224B | 11(一次性) |

中间件先取一字段,handler 再全量解码(100KB):

| 组合 | ns/op | B/op | allocs |
| --- | --- | --- | --- |
| goccy extract + goccy full | 333µs | 436KB | 712 |
| goccy extract + stdlib full | 1177µs | 448KB | 2829 |
| gjson parse + stdlib full | 1170µs | 448KB | 2823 |

结论:

- goccy 的 `Path.Extract` 是“按需物化”,但会**复制整段原始字节再扫描**
  (100KB 每调用 156KB 临时分配),不是 sonic.Get 那种廉价扫描;100KB 上取一个
  字段(131µs)≈ 全量 goccy 解码(196µs)的 67%。小文档(2KB)约 6µs。
- 真正收益排序:① 不读 body 的请求零成本;② 原始字节只读一次、所有消费方共享;
  ③ 全量解码 5.3× 快于 stdlib;④ 纯 Go、全平台一致。
- 沿用既有结论:**共享原始字节,不共享解析状态**。

### goccy 与 encoding/json 的解码一致性差异(实测)

| 用例 | stdlib | goccy |
| --- | --- | --- |
| 未知字段 / 大小写不敏感匹配 / 重复键 | 忽略 / 是 / 后者覆盖 | 一致 |
| `null` 文档 | 保持零值 | 一致 |
| 尾随垃圾/多余逗号/裸键名 | 报错 | 报错(文案不同) |
| `int` 溢出 | 报错 | **静默饱和为 INT64_MIN** |
| float 赋给 int | 报错 | **截断并报误导性错误** |
| 无效 UTF-8 | 替换 U+FFFD | **保留原始无效字节** |

差异只在 `Body[T].Decode()` 的 JSON 路径生效,文档必须标注;`WithBodyDecoder`
可接回 stdlib 严格语义。

## API 设计

### 1. 字段级 `Body[T]`(任意字段名,类型识别)

```go
type CreateUserReq struct {
	TenantID string                  `path:"tenantID"` // 静态来源:tag,纯值
	Debug    bool                    `query:"debug"`
	Payload  ghttp.Body[CreateUserPayload] // 惰性 body:类型识别,名字随意
	ghttp.Params                     // 匿名嵌入:动态/原始访问,方法提升
}

func(ctx context.Context, req CreateUserReq) (UserResp, error) {
	id, _ := req.PathInt("tenantID")    // Params 方法提升
	p, err := req.Payload.Decode()      // 首次:读流+解码+缓存
	raw, _ := req.Payload.Raw()         // 只读字节,不反序列化
	ip := req.ClientIP()
	_ = req.Request()                   // 原始 *http.Request
}
```

### 2. 顶层简写

```go
Route[ghttp.Body[Payload], Resp](app).POST("/x").To(
	func(ctx context.Context, body ghttp.Body[Payload]) (Resp, error) {
		p, err := body.Decode()
		...
	})
```

### 3. `Params` 扩展(替代 RequestView)

在现有 `Params` 上新增(全部非破坏性):

```go
func (p Params) Request() *http.Request     // 底层请求
func (p Params) Method() string
func (p Params) URL() *url.URL
func (p Params) ContentType() string        // 原始头值,不做归一化
func (p Params) RawBody() ([]byte, error)   // 共享 memo,与 Body[T]/RawBody(r) 同源
func (p Params) Form(key string) string            // 合并 query+表单体,静默
func (p Params) PostForm(key string) string        // 仅表单体,静默
func (p Params) FormValues() (url.Values, error)   // 合并视图,共享缓存
func (p Params) PostFormValues() (url.Values, error) // 仅表单体,共享缓存
func (p Params) MultipartForm(maxMemory int64) (*multipart.Form, error)
```

`Detach()` 语义不变(不拷贝 body,文档说明 detached 视图的 `RawBody` 返回
`(nil,nil)`);`Params` 仍只允许匿名嵌入,维持现有校验规则。

### 4. 请求级辅助(middleware)

```go
func RawBody(r *http.Request) ([]byte, error) // 首次读流并缓存到 requestState,共享
func FormValues(r *http.Request) (url.Values, error)
func PostFormValues(r *http.Request) (url.Values, error)
func MultipartForm(r *http.Request, maxMemory int64) (*multipart.Form, error)
```

表单与 multipart 的解析结果同样缓存到 `requestState`(`sync.Once`),middleware
与 handler 共享;因为缓存在 context 中,`r.WithContext` 产生的请求拷贝也命中同一份。
直接 `io.ReadAll(r.Body)` 仍按标准语义消费流,只有上述入口保证共享。

### 5. 按需 JSONPath 薄层

```go
path, err := ghttp.CompileJSONPath("$.items[0].title") // 进程内缓存编译结果
vals, err := path.Extract(raw)                          // [][]byte
err = path.Unmarshal(raw, &dst)                         // extract+goccy 解码到目标
```

### 6. 逃生口与错误语义

- `WithBodyDecoder(fn)` 复用:`Body[T].Decode()` 配置了自定义 body decoder 时
  走 `fn(bytes.Reader, contentType, target)`,一票否决下方派发;否则按非 JSON
  处理章节派发。
- 新增导出哨兵 `ErrInvalidBody`;读流/解码错误用 `fmt.Errorf("%w: %w", ...)`
  双 `%w` 包装,`errors.Is(err, ErrInvalidBody)` 与
  `errors.As(err, &maxBytesErr)` 均可用。解码发生在 handler 内,状态码由
  handler/validator 映射(文档给示例),框架无法像 eager 一样在绑定期自动写 400。
- 超过 `MaxBodyBytes` 返回 `*http.MaxBytesError`。

## 非 JSON body 处理

`Body[T].Decode()` 在首次访问时按 Content-Type 派发,规则与现有 eager
`parseBody` 对齐:

| Content-Type | 行为 |
| --- | --- |
| 缺失 | JSON(文档化便利,同 eager) |
| `application/json` | goccy(既定决策) |
| 其他已知类型(XML/plain 等) | 经 `CodecManager.Resolve` 用对应 codec 解码 |
| 显式未知类型 | `ErrInvalidBody`(路由设置了 `Consumes` 时绑定期已先 415) |
| `application/x-www-form-urlencoded` | 从共享 postForm 缓存填充,与 FormCodec 同严格性(有可绑定字段但无 `form` tag 时报错) |
| `multipart/form-data` | 从共享 multipart 缓存填充(值字段与 `FileHeader` 都支持);必须先于 `RawBody` 整读,否则 `Decode()` 返回 `ErrInvalidBody` |

绑定阶段 `inputTargetHasBody` 对 `Body[T]` 返回 true,`Consumes`/Content-Type
校验与 eager 行为一致。`Body[T]` 只支持值字段(指针字段注册期报错);一个结构体
只允许一个 body 字段(eager 的 `Body` 与惰性的 `Body[T]` 互斥,重复注册期报错)。

**破坏性变更标注(pre-v1.0)**:eager multipart 的 `fillMultipartBody` 由
“`r.FormValue`(query+表单体合并)”改为“仅表单体”(基于解析出的
`*multipart.Form`,与惰性路径同源),消除两条路径的语义差异。

## 数据流与关键实现点

1. **memo body 挂载点**:`Server.ServeHTTP` 入口(server 级 middleware 之前)把
   `r.Body` 包成 `memoBody` 挂在 `requestState`;首次 `Read` 才读底层流并追加进
   共享缓冲。不读 body 的请求只多一个接口包装,零额外分配。
2. **大小限制**:路由解析输入时把有效 `MaxBodyBytes` 写入 memo;`RawBody/Decode`
   首次 drain 时若已缓冲或即将超过限制返回 `*http.MaxBytesError`。语义与今天
   一致:限制从“路由解析点”生效,更早的 server/route 级 middleware 直接读流
   不受限(现状即如此)。表单/multipart 解析流经 memo,设限后同样被截断;
   设限前已被 middleware 整读的字节在解析后按 `checkLimit` 报错。
3. **绑定识别**:`getStructInfo` 对字段类型 `ghttp.Body[T]`(任意字段名)标记惰性
   body 位并跳过 eager `parseBody`;通过包内 `bodyFieldMarker`(类型识别)与
   `bodySourceSetter`(安装句柄)两个小接口接线。顶层 `Req = Body[T]` 同样按
   marker 接线。`Body[T]` 值语义(内部指针),`Raw/Decode` 用 `sync.Once` 缓存
   字节/结果/错误。
4. **非泛型共享源**:`bodySource`(req/config/codecMgr/memo + 字节与解码缓存)是
   类型擦除的共享状态,`Body[T]` 的泛型方法用编译期 `var v T` 解码后以 `any`
   存入缓存;字段拷贝与 validator/handler 多次调用共享同一结果。
5. **OpenAPI**:`extractBodySchema` 把 `Body[T]` 解包为 `T` 的 schema(字段级任意
   命名 + 顶层都覆盖),输出与 eager `Body SomeStruct` 形态一致。
6. **ToHTTP 澄清**:核对当前代码,`ToHTTP/ToRaw` 是 raw terminal,不解析输入;
   eager 绑定的是 `To/ToNoOutput/ToHTTPFunc/ToRedirectFunc`。本次不改 ToHTTP。

## 范围外(v1)

- 替换现有响应/输出 codec 或把 goccy 设为全框架默认 JSONCodec(现有 eager
  `Body` 字段路径与输出编码保持 stdlib 不变,整体切换另开设计);
- `Detach()` 深拷贝 body;
- sonic 可选加速注册(保持纯 Go 单实现,大文档按需字段访问成为热点时再评估)。

## 验证计划

- 单元测试(测试先行):不读 body 零成本、首次访问才读流、Raw/Decode 共享字节、
  middleware 先读后 handler 再解码、解码与错误缓存、顶层简写、字段级与 Params
  共存、任意字段名识别、重复 body/指针 body 注册期报错、Content-Type 派发
  (JSON/XML/缺失/未知/multipart)、`WithBodyDecoder` 逃生口、`ErrInvalidBody` 双
  `%w` 语义、MaxBodyBytes、零值安全、Params 扩展方法、JSONPath 编译缓存与
  Extract/Unmarshal、OpenAPI 解包、goccy 差异用例(int 溢出)按文档化行为锁定。
- 基准(worktree vs master,同一 harness):不读 body / 只 RawBody / Decode 一次 /
  middleware Raw + handler Decode / 100KB body 并发解码;与 eager 路径对照。
- `go test ./ghttp/...`、`go test -race ./ghttp/`、`go vet ./ghttp/`、`gofmt`;
  依赖离线加入 `github.com/goccy/go-json v0.10.6`(模块缓存已具备)。
- k6 smoke 复用既有场景回归。

## 约束

按仓库当前约束,本文档与实现**只落在 worktree,不提交、不推送**。

## 原型实测(lazy body,2026-08-13,同一 harness)

100KB JSON body,各场景(ghttp worktree):

| 场景 | ns/op | B/op | allocs |
| --- | --- | --- | --- |
| Body[T] 从不读取 | 5.2µs | 7.3KB | 27 |
| Body[T] 仅 Raw | 175µs | 581KB | 33 |
| Body[T] Decode 一次(goccy) | 398µs | 871KB | 741 |
| middleware Raw 后再 Decode | 400µs | 874KB | 741 |
| eager stdlib 解码(对照) | 1326µs | 1137KB | 2869 |

结论:不读 body 的请求比 eager 快 ~255×、分配少 ~156×;middleware 先读后
handler 解码只比单独解码多 ~2µs,共享字节成立;goccy 解码比 stdlib eager
路径快 ~3.3×。修复“复用请求对象时 memo 串状态”bug 后,`ServeHTTP` 改为仅在有
body 时克隆请求(不再改写调用方请求),从不读场景因此多付一次 Request 拷贝
(+1.1µs/+~700B);类型化 JSONBind 从修复前的虚假 1060µs/2.2MB 恢复为真实的
5.5µs/4.3KB。
