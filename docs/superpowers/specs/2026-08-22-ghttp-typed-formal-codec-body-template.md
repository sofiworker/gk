# ghttp typed 正式版设计定义 (codec/body/模板)

> **状态**: 已定稿并落地。typed 入口（GetParams/PostBody/PostParamsBody 等）为 ghttp 唯一 typed 入口，旧容器风格（Handle/RequestInput/InputSource）已移除。
> **前置**: bindbench 横评确认参数绑定领先、body 场景为流式 vs 池化 readAll 的架构权衡。本文件锁定正式版对外契约。

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

## 实现状态（已落地）

typed 入口为 ghttp 唯一的 typed API，旧容器风格（`Handle`/`RequestInput`/`InputSource` 及 `QueryString`/`PathInt64` 等构造器）已移除。最终入口矩阵：

| 输入组合 | 入口 | handler 签名 |
|---|---|---|
| 无输入 | `GetNone[O]` | `func(ctx) (O, error)` |
| 仅 params | `GetParams[P,O]` / `DeleteParams[P,O]` | `func(ctx, P) (O, error)` |
| 仅 body | `PostBody[B,O]` / `PutBody` / `PatchBody` | `func(ctx, B) (O, error)` |
| params+body | `PostParamsBody[P,B,O]` / `PutParamsBody` / `PatchParamsBody` | `func(ctx, P, B) (O, error)` |

- params 经 struct tag（`path:`/`query:`/`header:`）绑定，无 tag 字段静默跳过；注册期建 `BindPlan`，请求期零反射运行。
- body 入口收 `dec RequestDecoder`（`JSONBody()`/`XMLCodec()`/自定义），`dec=nil` 报 `ErrMissingCodec`。
- 输出走 `OutputSpec[O]`（`JSON[O]()` 可 `.Status(code)`/`.WithEncoder(enc)`）；`out=nil` 报 `ErrMissingOutput`。
- 需完全接管响应时用 `RawHandle`。

## 性能与 alloc 归因（bindbench 端到端）

| 场景 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| GetNone（无输入） | 197 | 64 | 3 |
| GetParams（1 path+2 query） | 683 | 528 | 8 |
| GetParams（1 path+5 query+3 header） | 915 | 673 | 11 |
| PostBody（仅 body） | 996 | 602 | 10 |
| PostParamsBody（1 path+body） | 1112 | 682 | 12 |

**alloc 归因（pprof）**：

1. **纯参数绑定全面领先 gin/echo**：query/path/header 越多优势越大，与 echo 同 alloc（注册期建计划、请求期零 tag 解析）。剩余 alloc 主要为 `net/url.parseQuery`（query 字符串解析，标准库固有）与 `reflect.unsafe_New`（params 结构体实例化），gin/echo 同源。
2. **body 场景与 echo 的差距是流式 vs 池化的架构权衡**：ghttp 用 `json.NewDecoder` 流式解码（内存恒定、永不 `io.ReadAll`，抗超大/恶意 body），每请求固定 ~512B decoder 缓冲；echo 用 `sync.Pool` 缓存 buffer + `json.Unmarshal`（小 body 更省，但大 body 内存线性膨胀）。想要 echo 式性能可实现自定义 `RequestDecoder` 单侧替换，或配 `LimitBody` 中间件兜底。
3. **encoder 池化收益为负**：Go 1.24 的 `json.NewEncoder` 已优化到 48B/2allocs，`sync.Pool` 开销反而抵消收益，故不采纳。
