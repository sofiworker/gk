# ghttp 显式响应格式设计(定稿)

## 背景

请求侧已落地 `Body[T]` 惰性解码 + `DecodeJSON/DecodeXML/DecodeForm` 显式格式
(见 `2026-08-13-ghttp-lazy-body-design.md`)。响应侧仍只有「Accept 头 + Produces
自动协商」,格式藏在请求头里,handler 写代码时看不出最终输出 JSON 还是 XML,
与请求侧改动前的 `Decode()` 同一毛病。

目标:对称地给响应侧一个「显式声明格式」的可选能力,不改变 handler 签名骨架
(`func(ctx, Req) (Resp, error)` 不变),不固化形态。

## 已确认决策

1. **载体**:`Render[T]` 响应包装 + 工厂函数 `RenderJSON/RenderXML/RenderBytes`
   (对称请求侧 `DecodeJSON/DecodeXML/DecodeForm`)。零值 `Render[T]{}` 等价裸 Resp,
   走 Accept 协商,故是可选叠加而非替换。
2. **Accept 硬约束**:显式格式覆盖「Produces 默认协商」,但**不无视 Accept**——
   客户端 `Accept` 明确排除该格式时返回 406(RFC 9110)。显式格式不参与
   `WithLenientContentNegotiation` 回退(lenient 只作用自动协商路径)。
3. **`[]byte` 语义不变**:裸 `Resp = []byte` 保持 Go 标准 base64 语义(不破坏性变更);
   原始字节直出显式用 `RenderBytes(data, contentType)`(对应 gin `c.Data` / echo
   `c.Blob` / axum `Bytes`)。
4. **与 Envelope 正交**:Render 定「格式」,Envelope 定「形状」。解包 Render[T] 得到
   Data 后,Envelope 包 Data(不是包包装本身);`RenderJSON` + Envelope 输出
   `{"code":0,"msg":"success","data":{...}}`。
5. **StatusCoder/ResponseHeaderWriter/OpenAPI** 都作用于解包后的 Data,经
   `renderTypeArg`(反射 `Render[T].Data` 字段)还原 T。
6. **produces 校验放宽**:`Resp` 是 `Render[T]`(实现包内 `renderUnwrapper`)时,
   `resolveProduces` 的「produces 缺失」豁免(显式格式不依赖 produces 列表);
   produces 含未注册类型仍报 `ErrRouteProducesUnsupported`。

## 实现要点

- `render_response.go`:`Render[T]`、`renderFormat`(auto/json/xml/raw)、工厂函数、
  `renderUnwrapper` 类型擦除接口。文件名避开已被 HTML 模板 `GoRenderer` 占用的
  `render.go`。
- `builder_core.go`:`writeTypedResponse` 解包 Render[T] 后,StatusCoder/
  ResponseHeaderWriter 检查 data;`resolveExplicitFormat` 按 format 选 codec
  (json/xml 用 `selectResponseCodec` 限定单候选以保留 Accept 语义,raw 直写字节)。
- `openapi.go` / `openapi_compiler.go`:`renderTypeArg` 解包 Render[T]→T,
  响应 schema 用解包后的类型。
- 测试 `render_response_test.go`:强制 JSON/XML、Accept 406、raw 直出、StatusCoder
  透传、Envelope 包 Data、renderTypeArg 解包。

## 验证

`go test ./ghttp/ -count=1` 全绿;`go vet ./ghttp/` 干净。
