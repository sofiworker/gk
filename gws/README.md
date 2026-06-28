# gws

`gws` 提供仓库内置的 SOAP/WSDL 能力，包含：

- 基于 WSDL/XSD 生成静态 Go 类型、operation 元数据、强类型 client、标准 `http.Handler` 服务端和 `gserver` 适配
- 运行时 SOAP 1.1 Envelope/Fault 编解码
- 服务端自动发布 `?wsdl` 和 `?xsd=...`
- 生成结果支持直接返回 `*gws.Request`，方便调用端继续设置 Header、SOAP Header、Envelope、HTTP client 或覆写 endpoint

## 兼容性矩阵

| 能力 | 当前状态 | 说明 |
| --- | --- | --- |
| SOAP 1.1 | 支持 | 生成器、客户端、服务端、低层 Envelope/Fault 编解码都按 SOAP 1.1 工作 |
| SOAP 1.2 | 不支持 | 当前没有 SOAP 1.2 namespace、binding、content-type 适配 |
| document/literal wrapped | 支持 | 当前生成代码和 runtime 的主要目标模式 |
| rpc/literal | 不支持 | 未实现 rpc 风格 wrapper 与编码路径 |
| rpc/encoded | 不支持 | 未实现 encoded 规则和相关序列化 |
| WSDL 生成静态 Go 类型 | 支持 | 通过 `cmd/gksoap` 或 `go generate` 生成 |
| 自动发布 `?wsdl` / `?xsd` | 支持 | 需要生成时启用 `-embed-wsdl`，或手动在 `ServiceDesc.WSDL` 中提供资产 |
| 标准 `net/http` 服务端 | 支持 | 核心入口是 `gws.NewHandler(...)` |
| `ghttp/gserver` 适配 | 支持 | 生成代码和适配包都已覆盖 |
| 纯手写 runtime 接入 | 支持 | 可直接手写 `ServiceDesc`、`OperationDesc`、`Request`、`Client` |
| 原始 SOAP XML 收发 | 支持 | 可使用 `SetEnvelope(...)`、`DoRaw(...)`、`DoHTTPRaw(...)` |
| SOAP Header | 支持 | 可通过 `SetSOAPHeader(...)` 或直接构造 `Envelope.Header` |
| SOAP Fault / Fault Detail | 支持 | 服务端可返回 `*gws.Fault`，客户端可接收 `*gws.FaultError` |
| 本地 `xsd:import` / `xsd:include` | 支持 | 生成器已处理本地 schema 引用 |
| 远程 schema 拉取 | 不支持 | 当前不做网络拉取和远端依赖解析 |
| `xsd:choice` | 不支持 | 当前未做 choice 到 Go 结构的映射 |

如果只看结论：

- 已有 WSDL 且是 document/literal wrapped 的 SOAP 1.1 服务，可以直接接入
- 需要 SOAP 1.2、rpc 风格或复杂 schema 特性时，当前还不适合直接依赖 `gws`

## 公共 API 速查

常用公共入口可以按下面理解：

- `gws.Request`
  负责组装一次 SOAP 调用；常用入口：`NewRequest`、`SetBody`、`SetSOAPHeader`、`SetEnvelope`、`BuildHTTPRequest`、`XMLBytes`
- `gws.Client`
  负责执行 SOAP 请求；常用入口：`NewClient`、`Do`、`DoRaw`、`DoHTTP`、`DoHTTPRaw`
- `gws.ServiceDesc`
  负责描述一个服务及其 operation；常用入口：`FindOperationByWrapper`、`WSDLAsset`、`XSDAsset`
- `gws.OperationDesc`
  负责描述单个 operation 的请求/响应工厂与调用逻辑；常用入口：`NewRequestValue`、`NewResponseValue`、`InvokeWith`
- `gws.Handler`
  负责把 `ServiceDesc + impl` 暴露为标准 `http.Handler`
- `gws.Envelope` / `gws.Body` / `gws.Header`
  负责低层 SOAP Envelope 编解码和完全手动控制请求/响应报文
- `gws.Fault` / `gws.FaultError`
  负责服务端返回 Fault 与客户端接收 Fault 的统一模型

常用辅助函数：

- `MarshalEnvelope` / `UnmarshalEnvelope`
- `MarshalFaultEnvelope`
- `ExtractFault`
- `DecodeBodyPayload`
- `UnmarshalBody`
- `SOAPNamespaces`

常用 option：

- `WithHTTPClient`
- `WithClientSOAPVersion`
- `WithServiceSOAPVersion`

生成代码场景下，对应还会导出：

- `<Operation>Operation()`
- `<Service>Desc()`
- `<Service>Client.Client()`
- `<Service>Client.Endpoint()`
- `<Service>Client.SetEndpoint(...)`
- `<Service>Client.<Operation>Raw(...)`

## 接入路径选择

优先按你的输入条件选择接入方式：

### 路径 A: 已有 WSDL/XSD，优先生成代码

适用场景：

- 已有稳定的 WSDL 合同
- 希望直接得到静态 Go 结构体、强类型 client、标准 `http.Handler`
- 希望服务端自动发布 `?wsdl` 和 `?xsd`

最小步骤：

1. 用 `go generate` 或 `go run ./cmd/gksoap ...` 生成代码
2. 服务端使用 `New<Service>Handler(...)` 或 `Register<Service>Server(...)`
3. 调用端使用 `New<Service>Client(...)`、`<Operation>(...)`、`<Operation>Raw(...)`

### 路径 B: 没有生成器输入，直接手写 runtime

适用场景：

- 只有少量 SOAP 接口，不值得维护独立 WSDL
- 需要逐步接入老系统，先跑通再补合同
- 需要完全手控 `Envelope`、Header、Fault、Raw XML

最小步骤：

1. 手写 `gws.Operation`、`gws.OperationDesc`、`gws.ServiceDesc`
2. 服务端使用 `gws.NewHandler(desc, impl)`
3. 调用端使用 `gws.NewRequest(...)`、`SetBody(...)` 或 `SetEnvelope(...)`
4. 用 `gws.NewClient().Do(...)` 或 `DoRaw(...)` 执行调用

如果你只是想尽快落地：

- 服务端优先走路径 A
- 调用端做协议探索、联调、抓原始 XML 时优先走路径 B
- 一个项目里可以同时使用两条路径

对应章节：

- 路径 A 继续看“生成代码”“服务端”“客户端”
- 路径 B 继续看“纯手写接入”

## 生成代码

推荐通过仓库内置生成器配合 `go generate` 使用：

```go
//go:generate go run github.com/sofiworker/gk/cmd/gksoap -wsdl ./contract/user.wsdl -o ./userws -pkg userws
```

也可以直接执行：

```bash
go run ./cmd/gksoap -wsdl ./gws/testdata/wsdl/echo.wsdl -o ./internal/echows -pkg echows
```

常用参数：

- `-service`
  选择目标 `wsdl:service`
- `-port`
  选择目标 `wsdl:port`
- `-type-prefix`
  为生成类型增加统一前缀
- `-client`
  控制是否生成 `client_gen.go`
- `-server`
  控制是否生成 `handler_gen.go` 和 `gserver_gen.go`
- `-embed-wsdl`
  控制是否生成 `wsdl_gen.go` 并在服务端自动发布 `?wsdl` / `?xsd`

生成目录会包含固定文件：

- `types_gen.go`
- `operations_gen.go`
- `client_gen.go`
- `handler_gen.go`
- `gserver_gen.go`
- `wsdl_gen.go`

生成后的包还会导出元数据访问器，方便第三方不经过内部约定直接消费：

- `<Operation>Operation() gws.Operation`
- `<Service>Desc() *gws.ServiceDesc`
- `<Service>Client.Client() *gws.Client`
- `<Service>Client.Endpoint() string`
- `<Service>Client.SetEndpoint(endpoint) *<Service>Client`
- `<Service>Client.<Operation>Raw(ctx, req) ([]byte, error)`

## 服务端

生成后的标准服务端以 `http.Handler` 为核心：

```go
h, err := userws.NewUserServiceHandler(impl)
if err != nil {
	return err
}

srv := &http.Server{
	Addr:    ":8080",
	Handler: h,
}
```

如需读取生成出来的服务描述：

```go
desc := userws.UserServiceDesc()
op := userws.CreateUserOperation()
_ = desc
_ = op
```

访问：

- `GET /service?wsdl`
- `GET /service?xsd=types.xsd`
- `POST /service`

## 客户端

生成后的 client 同时提供直接调用和请求对象出口：

```go
client := userws.NewUserServiceClient("http://127.0.0.1:8080/service")

resp, err := client.CreateUser(ctx, &userws.CreateUserRequest{
	Name: "alice",
})
```

如需继续控制请求：

```go
req, err := client.NewCreateUserRequest(ctx, &userws.CreateUserRequest{
	Name: "alice",
})
if err != nil {
	return err
}

req.SetHeader("X-Trace-ID", "trace-1")
req.SetEndpoint("http://127.0.0.1:8080/override")
```

如需拿到底层 runtime client 或直接读取原始 SOAP XML：

```go
client := userws.NewUserServiceClient("http://127.0.0.1:8080/service")

rawXML, err := client.CreateUserRaw(ctx, &userws.CreateUserRequest{
	Name: "alice",
})
if err != nil {
	return err
}

runtimeClient := client.Client()
_ = runtimeClient
_ = rawXML
```

同一个生成 client 也可以在运行时切换目标地址：

```go
client.SetEndpoint("http://127.0.0.1:8081/service")
```

如需配置超时、代理或自定义 Transport，可在运行时注入自定义 `http.Client`：

```go
client := gws.NewClient(
	gws.WithHTTPClient(&http.Client{
		Timeout: 5 * time.Second,
	}),
)
```

如果你不想依赖生成代码，也可以直接使用低层请求/响应 API：

```go
req := gws.NewRequest(ctx, endpoint, gws.Operation{
	Name:            "Echo",
	Action:          "urn:Echo",
	ResponseWrapper: xml.Name{Space: "urn:test", Local: "EchoResponse"},
})

req.SetEnvelope(gws.Envelope{
	Namespace: gws.SOAP11EnvelopeNamespace,
	Body: gws.Body{
		Content: &EchoRequest{Value: "hello"},
	},
})

rawXML, err := client.DoRaw(req)
if err != nil {
	return err
}

var out EchoResponse
if err := gws.UnmarshalBody(rawXML, xml.Name{Space: "urn:test", Local: "EchoResponse"}, &out); err != nil {
	return err
}
```

`*gws.Request` 同时提供只读访问器，方便第三方中间件或通用封装读取上下文和元信息：

- `Context()`
- `Endpoint()`
- `Operation()`
- `Headers()`
- `SOAPHeader()`
- `Body()`
- `Envelope()`

## 纯手写接入

如果你不想依赖 WSDL 生成器，也可以直接把 `gws` 当成普通 SOAP runtime 使用。

服务端最小接入方式是手写 `ServiceDesc` 和 `OperationDesc`：

```go
type EchoRequest struct {
	XMLName xml.Name `xml:"urn:manual EchoRequest"`
	Message string   `xml:"message"`
}

type EchoResponse struct {
	XMLName xml.Name `xml:"urn:manual EchoResponse"`
	Message string   `xml:"message"`
}

desc := &gws.ServiceDesc{
	Name: "ManualEchoService",
	Operations: []gws.OperationDesc{
		{
			Operation: gws.Operation{
				Name:            "Echo",
				Action:          "urn:manual:Echo",
				RequestWrapper:  xml.Name{Space: "urn:manual", Local: "EchoRequest"},
				ResponseWrapper: xml.Name{Space: "urn:manual", Local: "EchoResponse"},
			},
			NewRequest:  func() any { return &EchoRequest{} },
			NewResponse: func() any { return &EchoResponse{} },
			Invoke: func(ctx context.Context, impl any, req any) (any, error) {
				return impl.(EchoService).Echo(ctx, req.(*EchoRequest))
			},
		},
	},
}

h, err := gws.NewHandler(desc, EchoServiceImpl{})
if err != nil {
	return err
}
```

调用端则直接构造 `gws.Request`：

```go
client := gws.NewClient()
req := gws.NewRequest(ctx, endpoint, desc.Operations[0].Operation)
req.SetSOAPHeader(TraceHeader{TraceID: "trace-1"})
req.SetBody(&EchoRequest{Message: "soap"})

var out EchoResponse
if err := client.Do(req, &out); err != nil {
	return err
}
```

如果你需要完全控制 envelope，并希望自己处理原始 SOAP XML：

```go
req.SetEnvelope(gws.Envelope{
	Namespace: gws.SOAP11EnvelopeNamespace,
	Header: &gws.Header{
		Content: TraceHeader{TraceID: "trace-1"},
	},
	Body: gws.Body{
		Content: &EchoRequest{Message: "soap"},
	},
})

rawXML, err := client.DoRaw(req)
if err != nil {
	return err
}

var out EchoResponse
if err := gws.UnmarshalBody(rawXML, desc.Operations[0].Operation.ResponseWrapper, &out); err != nil {
	return err
}
```

如果服务端返回 SOAP Fault，客户端可以直接拿到 `*gws.FaultError`，其中 `Detail` 会保留原始 detail XML：

```go
err := client.Do(req, &EchoResponse{})
var faultErr *gws.FaultError
if errors.As(err, &faultErr) {
	log.Printf("fault code=%s detail=%v", faultErr.Fault.Code, faultErr.Fault.Detail)
}
```

可运行示例见：

- [example/soap_manual_client_server.go](/root/workspace/golang/gk/example/soap_manual_client_server.go)
- [gws/manual_usage_example_test.go](/root/workspace/golang/gk/gws/manual_usage_example_test.go)

## gserver 适配

生成代码同时提供 `gserver` 注册辅助函数：

```go
if err := userws.RegisterUserServiceServer(s, "/user", impl); err != nil {
	return err
}
```
