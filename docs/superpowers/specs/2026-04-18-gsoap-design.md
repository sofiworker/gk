# gsoap Design Spec

## Goal

在仓库中新增一个 `gsoap` 包，提供接近 Java JAX-WS / CXF 常见能力的 SOAP 基础设施，首版同时支持：

- `Code First`
- `Contract First`
- `SOAP 1.1`
- `SOAP 1.2`
- `document/literal wrapped`
- `rpc/literal`
- `WSDL 1.1`
- `WSDL/XSD import`
- 多 namespace
- 自动生成并发布 `WSDL`
- 运行时构造 `server` 和 `client`
- `WS-Security` 扩展能力与基础内建实现

首版优先提供运行时能力，后续可在相同契约模型上补充代码生成入口。

## Non-Goals

首版不纳入以下范围：

- `SOAP encoded use`
- 完整对齐所有 Java 框架扩展注解
- 全量 `WS-Security` 算法矩阵
- `WS-ReliableMessaging`、`WS-Policy`、`WS-Addressing` 完整实现
- 从任意复杂 XSD 自动恢复为完美 Go 类型

## Existing Project Context

仓库已具备以下可复用基础：

- `ghttp/gserver`：HTTP 服务端能力
- `ghttp/gclient`：HTTP 客户端能力
- `gcodec/xml_codec`：XML 编解码基础

`gsoap` 应复用现有 HTTP 和 XML 基础设施，避免重复建设独立的网络栈或序列化栈。

## Architecture

推荐新增如下包结构：

- `gsoap`
  - 面向使用者的聚合入口
- `gsoap/contract`
  - 内部统一契约模型
- `gsoap/wsdl`
  - `WSDL 1.1` 生成、解析、导入导出
- `gsoap/soapxml`
  - `Envelope`、`Header`、`Body`、`Fault` 编解码
- `gsoap/server`
  - 基于 `ghttp/gserver` 的 SOAP 服务端
- `gsoap/client`
  - 基于 `ghttp/gclient` 的 SOAP 客户端
- `gsoap/security`
  - `WS-Security` 扩展点与内建能力

核心原则是所有入口最终统一到 `contract.Definition`。`Code First` 与 `Contract First` 只负责生成或加载契约；运行时服务端、客户端、WSDL 发布器都只依赖统一契约模型。

## Contract Model

`gsoap/contract` 负责定义稳定的中间模型，建议最少包含：

- `Definition`
- `Service`
- `Port`
- `PortType`
- `Binding`
- `Operation`
- `Message`
- `Part`
- `Schema`
- `SchemaType`
- `Element`
- `Fault`
- `Header`
- `Namespace`

该模型需要同时表达：

- 服务名与目标 namespace
- 端口与地址
- `SOAP 1.1/1.2` 绑定信息
- `document/literal wrapped` 与 `rpc/literal`
- 输入输出消息
- 头信息
- 错误模型
- import 的依赖关系

后续所有能力都围绕该模型扩展，避免运行时逻辑直接依赖反射细节或 XML 解析细节。

## Code First Design

`Code First` 入口负责将 Go 服务定义转换成 `contract.Definition`。首版建议支持以下注册方式：

- 注册单个方法
- 注册一个结构体实例上的一组公开方法
- 注册显式描述的操作元数据

示意 API：

```go
svc := gsoap.NewService(
    gsoap.WithName("OrderService"),
    gsoap.WithTargetNamespace("urn:example:order"),
    gsoap.WithSOAP11(),
    gsoap.WithSOAP12(),
    gsoap.WithAddress("/soap/order"),
)

svc.Register("CreateOrder", handler.CreateOrder)
svc.Register("QueryOrder", handler.QueryOrder)
```

方法签名约束建议如下：

- 阻塞型操作首参优先支持 `context.Context`
- 允许 1 个请求参数
- 允许 1 个响应值
- 允许 1 个 `error`
- Header、Fault、扩展元数据通过选项或描述器补充

反射阶段需要完成：

- 操作名推导
- 请求/响应包装类型推导
- namespace 推导
- XSD 类型推导
- `document/literal wrapped` 与 `rpc/literal` 绑定方式选择

当自动推导无法稳定表达时，必须允许调用方显式覆盖，而不是做不可解释的隐式猜测。

## Contract First Design

`Contract First` 入口负责从本地文件或远程内容加载 `WSDL/XSD` 并构造成 `contract.Definition`。首版建议支持：

- 从文件系统加载 `WSDL`
- 从字节流加载 `WSDL`
- 解析 `wsdl:import`
- 解析 `xsd:import`
- 绑定运行时实现到契约中的 operation

示意 API：

```go
def, err := wsdl.LoadFile("order.wsdl")
svc, err := gsoap.Bind(def,
    gsoap.WithImplementation("CreateOrder", handler.CreateOrder),
)
```

对于复杂 schema，如果无法自动恢复为自然的 Go 结构，应允许：

- 使用 `map[string]any` / DOM 风格中间表示
- 使用用户自定义类型映射器
- 在调用层直接传入显式 XML 载荷结构

## Server Runtime

`gsoap/server` 基于 `ghttp/gserver` 暴露 SOAP 服务。每个服务至少需要提供：

- `POST /service-path`
- `GET /service-path?wsdl`
- import 资源地址，例如 `/service-path/xsd/{id}.xsd`

服务端职责：

- 按 `Content-Type` 区分 `SOAP 1.1` 与 `SOAP 1.2`
- 解析 `SOAPAction`
- 解析 `Envelope/Header/Body`
- 根据 operation 元数据进行分发
- 将业务错误映射为 `SOAP Fault`
- 自动发布 `WSDL`
- 自动发布 import 的 `XSD/WSDL`
- 允许中间件、鉴权、日志与 tracing 扩展

服务端建议提供：

- 基于统一定义构造服务
- 基于 `Code First` 直接构造服务
- 基于 `Contract First` 绑定实现后构造服务

## Client Runtime

`gsoap/client` 基于 `ghttp/gclient` 封装 SOAP 调用。建议支持三种入口：

- `FromDefinition`
- `FromWSDL`
- `New` + 手工配置 endpoint 和 operation

示意 API：

```go
cli, err := gsoapclient.FromWSDL("http://localhost:8080/soap/order?wsdl")
err = cli.Call(ctx, "CreateOrder", req, &resp)
```

客户端职责：

- 根据契约组装 `Envelope`
- 自动选择 `SOAP 1.1` 或 `SOAP 1.2`
- 填充 `SOAPAction`
- 发送 Header
- 解析正常响应
- 解析 `SOAP Fault`
- 应用安全头
- 允许每次调用覆盖 endpoint、header、timeout 和 security 配置

## WSDL Generation And Publishing

`gsoap/wsdl` 需要同时支持生成与解析。

生成侧要求：

- 从 `contract.Definition` 稳定生成 `WSDL 1.1`
- 为相同定义生成稳定、可预测的 namespace 和 import 路径
- 可选择内联 schema 或拆分 schema
- 自动生成 `service`、`portType`、`binding`、`message`、`types`

发布侧要求：

- `?wsdl` 返回主文档
- import 资源返回正确内容类型
- 文档中的地址可根据请求 Host、反向代理头或显式配置修正

解析侧要求：

- 解析 `definitions`
- 解析 `types`
- 解析 `message/part`
- 解析 `portType/operation`
- 解析 `binding/service/port`
- 递归处理 import

## SOAP Encoding Rules

首版需要明确支持以下绑定：

- `document/literal wrapped`
- `rpc/literal`

首版不支持：

- `document/encoded`
- `rpc/encoded`

`soapxml` 层需要提供：

- `Envelope` 编码解码
- namespace 管理
- `Header` 序列化
- `Fault` 编码解码
- `SOAP 1.1/1.2` 差异处理

## Type Mapping Rules

Go 与 XSD 的映射需要可预测，建议采用以下原则：

- 基础标量映射到标准 `xsd` 类型
- `struct` 映射 `complexType`
- `[]T` 映射重复元素
- `*T` 映射 `minOccurs=0`
- 字段标签优先使用 `xml`，必要时新增 `soap` 标签
- 对不确定映射一律要求显式声明

复杂映射风险点包括：

- 匿名结构
- 多层嵌套 slice
- `interface{}`
- `time.Time`
- `[]byte`
- 自定义枚举

这些类型需要有单元测试和明确文档，避免不同入口下行为不一致。

## Fault Model

错误模型分为三层：

- 传输错误
- 协议错误
- 业务错误

建议暴露稳定错误类型或常量，方便调用方做判断，例如：

- `ErrInvalidEnvelope`
- `ErrUnknownOperation`
- `ErrUnsupportedSOAPVersion`
- `ErrInvalidWSDL`
- `ErrSecurityFailure`

业务错误需要可映射为标准 `SOAP Fault`，包括：

- `Code`
- `Subcode`
- `Reason`
- `Detail`

客户端需要支持从响应中解析标准 fault 结构并保留 detail 原文。

## Security Model

首版安全能力按“高阶可扩展”定义，范围包括：

- 内建 `UsernameToken`
- 内建 `Timestamp`
- 签名/验签挂点
- 统一的客户端和服务端安全处理链

建议采用拦截器式设计：

- 客户端在发送前附加安全头
- 服务端在解包后校验安全头

首版不追求覆盖全部算法与规范细节，但接口应允许后续扩展：

- digest/password text
- body 签名
- header 签名
- 证书材料注入

## Public API Expectations

对外 API 需要保持简单，避免把内部契约模型复杂度直接暴露给普通用户。

推荐保留两类入口：

- 简单入口
  - 适合 `Code First` 快速发布服务
  - 适合按 WSDL 快速构造 client
- 高阶入口
  - 适合显式控制 operation、header、fault、namespace、binding

需要优先提供 `WithFunc` 风格选项，贴合仓库现有约定。

## Compatibility And Extensibility

首版设计必须为后续能力预留位置：

- `go generate` 或 CLI 代码生成
- 更完整的 `WS-Security`
- `WS-Addressing`
- 自定义 serializer
- 自定义 schema 类型映射器

因此不建议把反射结果、WSDL 节点结构和 HTTP 细节直接耦合到一个大对象中。

## Testing Strategy

测试必须与实现同目录放置，并以单元测试为主。建议至少覆盖以下层级：

- `contract`
  - 契约模型构造与校验测试
- `wsdl`
  - 生成 golden 测试
  - 解析测试
  - import 递归测试
- `soapxml`
  - `Envelope/Header/Fault` 编解码测试
  - namespace 处理测试
- `server`
  - `httptest` 回环测试
  - `?wsdl` 发布测试
  - `SOAP 1.1/1.2` 分发测试
- `client`
  - 正常调用测试
  - fault 解析测试
  - header/security 注入测试
- `security`
  - `UsernameToken`
  - `Timestamp`
  - 验签挂点行为测试

必须补一组端到端测试，验证同一份契约可同时满足：

- `Code First` 发布
- 自动发布 `WSDL`
- 客户端按 `WSDL` 加载
- 成功调用 operation
- 正确处理 fault

## Implementation Phasing

建议分阶段推进：

1. 打基础
   - `contract`
   - `soapxml`
   - 最小 `wsdl` 生成
2. 跑通主链路
   - `Code First`
   - `server`
   - `client`
   - `?wsdl`
3. 补契约优先
   - `WSDL/XSD` 解析
   - import 处理
4. 补高阶能力
   - `SOAP 1.2`
   - `rpc/literal`
   - `security`

虽然首版目标功能较大，但交付路径应优先保证一条完整主链路可运行，再逐步补齐扩展能力。

## Open Decisions For Implementation Plan

实现计划阶段需要进一步落定以下细节：

- `gsoap` 是否作为聚合入口同时 re-export 子包类型
- `rpc/literal` 与 `document/literal wrapped` 的默认选择规则
- import 资源 URL 的命名规则
- 复杂 XSD 到 Go 类型的最低支持边界
- `UsernameToken` 的默认校验策略
- `SOAPAction` 的默认生成策略

这些问题不影响总体架构成立，但需要在实现计划中具体化为可执行步骤。
