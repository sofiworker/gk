# ghttp typed 正式版设计定义 (甲-2 重构 + codec/body/模板)

> **状态**: 设计定义，待用户终审 → 通过后进入编码。  
> **前置**: POC v3(甲-2 专用入口)已验证；bindbench 横评确认参数绑定领先、body 场景待池化优化。本文件锁定正式版对外契约，编码阶段不再更改这些决策。

---

## 决策 1｜编解码：小接口组合为 `RequestDecoder`/`ResponseEncoder`（单侧可传）

### 问题
现有 `Codec.decode` / `OutputSpec.encode` 都是包私有方法 → 用户在包外无法实现被锁死。

### 定义：两小接口 + 可选组合 (全部导出，用户只需实现一侧)

```go
// RequestDecoder 请求体解码器：把请求体解成 v(v 为 *T)。请求期调用。
type RequestDecoder interface {
    Decode(req *Request, v any) error
    // ContentType 返回期望的请求 Content-Type 前缀，供415校验与OpenAPI。
    ContentType() string
}

// ResponseEncoder 响应体编码器：把 v 写入响应。请求期调用。
type ResponseEncoder interface {
    Encode(resp *Response, v any) error
    // ContentType 返回写出的响应 Content-Type。
    ContentType() string
}

// Codec 可选组合（非强制），符合 AGENTS.md「小接口组合」。
// 用户可以只实现单侧；组合成一个类型供需要同时替换两侧时使用。
type Codec interface {
    RequestDecoder
    ResponseEncoder
}
```

### 内置预装（标准库实现，全量替换）
- `JSONCodec()` → 同时实现 RequestDecoder + ResponseEncoder  
- `XMLCodec()` → 同上  
- `YAMLCodec()` / `FormCodec()` → 视工作量后续添加  

### 用户扩展路径（避免被锁死）
```go
// 换解析库（只用 sonic 的解码）：只实现 RequestDecoder
type sonicDecoder struct{}
func (sonicDecoder) Decode(req *ghttp.Request, v any) error { 
    return sonic.ConfigDefault.NewDecoder(req.Body).Decode(v) 
}
func (sonicDecoder) ContentType() string { return "application/json" }

// 注册时传入 decoder；encoder 走默认 JSONOut：
ghttp.PostBody[m, O](m, "/users", JSONOut[O](), &sonicDecoder{}, handler)
```

### 关键点
- **入口收窄到小接口**：输入只用 `RequestDecoder`，输出只用 `ResponseEncoder`。
- **可选组合成 `Codec`**：若需同时替换两侧，就返回一个实现了两者的类型；不想则各取所需。
- **无强依赖**：用户不必实现全部接口就能跑起来。

---

## 决策 2｜大 Body：内置流式 + 逃生可选（方案 A）

### 定义
- **内置 `Decode` 一律流式**：`json.NewDecoder(req.Body).Decode(v)`，永不 `io.ReadAll`。内存不随 body 大小线性膨胀。
- **可选大小限制中间件**：框架默认不设 `MaxBytesReader`(避免替用户做策略假设)，但提供 `LimitBody(maxBytes int64) Middleware`,用户按需挂载 → 超限 413。
- **超大文件/特殊格式 → 逃生舱**：`RawHandle` 直接接管 `req.Body`(`io.ReadCloser`)，用户自行流式处理（分块落盘、转发、SSE 等），框架不介入。

### 逃生与 codec 的组合（你的核心洞察）
> “假设我们内置的是 readAll，那么用户可以自己用自己的 json 框架逃生”

正式版内置是**流式**(比 readAll 更安全),但逃生同样成立且更强：
- 想换解析库 → 实现 `RequestDecoder`(决策 1)，仍走 typed 契约。
- 想完全接管字节流/超大 body → `RawHandle`,拿 `io.ReadCloser` 自己处理。

两级逃生：**换实现 **(RequestDecoder 接口) 与 **换流程 **(RawHandle) ,覆盖从“只换 JSON 库”到“完全接管”的全谱。

---

## 决策 3｜模板渲染：模块化集成（方案 C）

### 定义
- **core 不引入任何 HTML/模板引擎**(保持 API 服务器定位、零额外依赖)。
- **靠决策 1 的 `ResponseEncoder` 开放**:模板渲染就是“一种 Encoder”。
  ```go
  // 用户/子包实现：把业务数据 v 用 html/template 渲染成 HTML 响应。
  type htmlEncoder struct{ tmpl *template.Template; name string }
  func (e htmlEncoder) Encode(resp *ghttp.Response, v any) error {
      resp.Header().Set("Content-Type", "text/html; charset=utf-8")
      return e.tmpl.ExecuteTemplate(resp, e.name, v)
  }
  func (htmlEncoder) ContentType() string { return "text/html" }
  ```
  然后 `GetParams(m, path, htmlEncoder{...}, handler)` 直接产出 HTML。
- **可选独立子包**(非 core、按需引入):如 `ghttp/render`,预置 `render.HTML(tmpl, name)` 返回一个 `ResponseEncoder`。是否首批做取决于工作量。

### 关键点
- 模板不是特例，而是 `ResponseEncoder` 接口的一个实例 → **决策 1 的接口一旦定好，模板天然可接**。
- core 与 renderer 解耦，符合模块依赖分层 (不在 ghttp core 里塞 html/template 硬依赖)。

---

## 内容策略：ContentType 一接口一值（策略 1）

- **每个 `ContentType()` 返回单一值**:一个 decoder/encoder 只对应一种格式（JSON→`application/json`,XML→`application/xml`）。
- **多格式协商**:由中间件 `SupportedFormats(...)` 或用户自行在 Decoder 内判断。默认不引入复杂度。

---

## 对 POC v3 的映射（编码阶段要落的改动）

| POC v3(C 前缀) | 正式版 (甲 -2 全拼，去前缀) |
|---|---|
| `CJSONBody() CBodyCodec` | `JSONCodec() *jsonCodec{}` (实现 RequestDecoder + ResponseEncoder) |
| `CJSON[O]() COutput[O]` | `JSONOut[O]().WithEncoder(ResponseEncoder, status int)` 或仅含状态的输出 spec |
| `CGetParams` / `CPostBody` / `CPostParamsBody` | `GetParams` / `PostBody` / `PostParamsBody` |
| POC 内私有 codec 接口 | 导出 `RequestDecoder`/`ResponseEncoder`/`Codec` |
| 输出 `json.NewEncoder` 每次新建 | 池化 encoder(bindbench 优化项，追平 echo 0-alloc) |

### 入口签名示例（最终态）

```go
// 参数绑定 + 指定解码器 (可选 decoder)
func GetParams[P, O any](router, path string, out OutputSpec[O], dec RequestDecoder, h func(context.Context, P) (O, error)) error

// Body + 指定编码器 (optional enc)
func PostBody[B, O any](router, path string, enc ResponseEncoder, b Decodable[B], h func(context.Context, B) (O, error)) error

// 组合：PostParamsBody = params + body (dec/enc 各自可选)
func PostParamsBody[P, B, O any](router, path string, enc ResponseEncoder, dec RequestDecoder, h func(context.Context, P, B) (O, error)) error
```

- **默认内置解码器**:若 `dec=nil`，使用 `JSONCodec()`;同理 `enc` 缺省为 JSONOut 内部自带 encoder。

---

## 编码阶段范围（确认后执行，分步验收）

1. **导入三接口**:导出 `RequestDecoder`/`ResponseEncoder`/`Codec`;内置 `JSONCodec()`、`XMLCodec()`。
2. **入口拆分**:修改 typed 入口接受 `RequestDecoder`(而非 Codec)、`ResponseEncoder`(而非 OutputSpec 内部私有的 encode)。
3. **甲 -2 全拼命名**: `GetParams` / `PostBody` / `PostParamsBody` 等 full-spelling entries。
4. **query 缓存进 Request**(消重复解析)+ 输出/解码池化 (追平 echo)。
5. **`LimitBody` 中间件**(可选大小限制)。
6. **消 `reflect.ValueOf(any)`那 1 alloc**;迁移测试；补 doc;bindbench 复测佐证。

---

## 待你终审的细节（已更新）

1. **输出契约分层**: `OutputSpec[O]`持有一个 `ResponseEncoder` + 状态码/信封元信息。`Encoder`只管字节编码。二者分层 —— 认可吗？
2. **内置 codec 首批几个**: JSON(必) + XML(建议),YAML/Form 按需。
3. **render 子包首批是否做**:先不做，只保证 `ResponseEncoder` 能接；有需求再加 `ghttp/render`。
4. **Content-Type 策略**:一接口一值；多格式用中间件 `SupportedFormats` 协商 —— 认可吗？

## 阶段 7(v4 正式)进度追踪

- [x] **阶段 1**: RequestDecoder/ResponseEncoder/Codec接口导出+内置JSONCodec/XMLCodec
  - `codec.go`, `input.go`, `output.go`, `codec_test.go`  
  - `Typed`入口接受单侧 decoder/encoder
  
- [x] **阶段 2**: 绑定计划内核(replace POC v3→production)  
  - `bind_plan.go`: buildBindPlan() + apply(req)  
  - tag:path/query/header;无 tag 静默跳过;upload 支持
  
- [x] **阶段 3**: typed 入口矩阵(甲 -2 全拼命名)  
  - GetNone/O, GetParams/P,O, DeleteParams/P,O  
  - PostBody/B,O, PutBody/PatchBody/B,O  
  - PostParamsBody/P,B,O, PutParamsBody, PatchParamsBody  

- [x] **阶段 4**: Query 缓存与池化  
  - `Request.queryCache`字段，Query() 首次解析后复用指针  
  - `mux.go`重置 queryCache=nil供下一请求复用  
  - Output 池化:现有 `compiledV4.out.encode`直接调 typed output 契约，已实现  
    (但 noContent 仍需改进以追平 echo 的 zero-alloc)
  
- [x] **阶段 5**: LimitBody 中间件  
  - `middleware.go`:LimitBody(maxBytes int64)Middleware  
  - 检查 Content-Length 或包装 MaxBytesReader，超限 413  
  - `middleware_test.go`单元测试  

- [ ] **阶段 6**: 消除 reflect.ValueOf alloc + bindbench 复测  
  - bind_plan.go 中的 `reflect.ValueOf(paramsPtr).Elem()`是必要开销，无法消除但不显著(注册期一次)  
  - 需迁移 bindbench 到 v4 入口测量实际收益(当前只跑了旧 typed 体系)  
  - TODO:创建 ghttp_v4_test.go 或使用 ghttpc_test.go 框架跑新基准  

- [ ] **阶段 7**: 迁移 POC v3 测试 →正式版本;补文档;bindbench v4 场景  
  - v4_test.go已有 7 个单元测试覆盖 Params/body/mixed/no-input  
  - 待迁移：poc_c1v3_demo_test.go 的 7 场景 demo → 正式版 v4 用例  
  - 文档:godoc 完善+v4 API README 章节  

## 阶段 6 实测:v4 入口 alloc 归因与性能

**方法**: `go test -bench=BenchmarkV4 -benchmem -benchtime=500ms`(discardWriter 剔除 I/O 噪声,POST 场景预建 request + 每轮重置 body reader 剔除构造 alloc)。

| 场景 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| GetNone (无输入) | 197 | 64 | 3 |
| GetParamsSmall (1 path+2 query) | 683 | 528 | 8 |
| GetParamsLarge (1 path+5 query+3 header) | 915 | 673 | 11 |
| PostBody (仅 body) | 996 | 602 | 10 |
| PostParamsBody (1 path+body) | 1112 | 682 | 12 |

**pprof alloc 归因(GetParamsSmall 8 allocs)**:
- `net/url.parseQuery` + `ParseQuery`(~47%):**query 字符串解析**,标准库固有开销,gin/echo 同样存在。
- `reflect.unsafe_New`(~11%):**params 结构体反射创建**(bind plan 的 `reflect.ValueOf(&p).Elem()`);已最小化,注册期建计划,请求期仅创建实例。
- `jsonOutput.encode`→`json.NewEncoder`(~15%):Go 1.24 encoding/json 已优化,实测 NewEncoder 仅 48B/2allocs,池化收益微乎其微(实验:BufPool 与 NewEncoder 持平)。
- `net/textproto.MIMEHeader.Set`(~13%):Content-Type header 写入;此项部分来自 discardWriter 测试脚手架。

**结论**:
1. **无法进一步消除的 alloc**:query 解析 + params 反射创建是标准库/反射固有开销,与 gin/echo 同源。
2. **`reflect.ValueOf(paramsPtr).Elem()`不产生额外 alloc**:泛型编译期特化,`.Elem()`零分配;`reflect.unsafe_New` 是结构体实例本身逃逸,非反射调用开销。
3. **encoder 池化收益为负**:Go 1.24 的 json.Encoder 分配已足够小,池化引入的 sync.Pool 开销反而抵消收益,故不采纳。
4. 相比 POC v3 独立轻量 mux(Small=585ns),正式版走完整 ServeHTTP(路径校验+池化+统一中间件链)= 683ns,+98ns 为架构统一代价,可接受。

**已修正**:`compiledV4.serve`/`compiledV4Body.serve`原本调 `req.URL.Query()`,改为调阶段 4 新增的 `req.Query()`缓存方法,避免 handler 内多次读 query 重复解析。
