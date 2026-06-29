# gsoap Runtime-First Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在仓库中新增一个运行时优先的 `gsoap` 包，支持 `Code First + Contract First`、自动生成并发布 `WSDL`、构造 SOAP `server/client`，并覆盖首版约定的 `SOAP 1.1/1.2`、`document/literal wrapped`、`rpc/literal`、`WSDL/XSD import` 与基础 `WS-Security` 能力。

**Architecture:** 先建立统一的 `contract.Definition` 契约内核，再在其上实现 `soapxml`、`wsdl`、`server`、`client` 和 `security`。`Code First` 和 `Contract First` 都只负责生成或加载契约对象，运行时调用链和 WSDL 发布只消费统一契约，避免反射逻辑、XML 编解码和 HTTP 处理互相耦合。

**Tech Stack:** Go 1.24、标准库 `encoding/xml`、现有 `ghttp/gserver`、现有 `ghttp/gclient`、`testify`

---

## Spec Reference

- Spec: `docs/superpowers/specs/2026-04-18-gsoap-design.md`

## Validation Notes

- 仓库当前未发现 `Makefile` 或 `make check` 目标，执行时改用：
  - `gofmt -w <touched files>`
  - `go test ./gsoap/...`
  - `go test ./...`
  - `golangci-lint run`

## File Structure

### Root package

- Create: `gsoap/errors.go`
- Create: `gsoap/options.go`
- Create: `gsoap/service.go`
- Create: `gsoap/register.go`
- Create: `gsoap/bind.go`
- Test: `gsoap/service_test.go`

### Contract

- Create: `gsoap/contract/definition.go`
- Create: `gsoap/contract/schema.go`
- Create: `gsoap/contract/validate.go`
- Create: `gsoap/contract/type_mapper.go`
- Test: `gsoap/contract/definition_test.go`
- Test: `gsoap/contract/type_mapper_test.go`

### SOAP XML

- Create: `gsoap/soapxml/constants.go`
- Create: `gsoap/soapxml/envelope.go`
- Create: `gsoap/soapxml/header.go`
- Create: `gsoap/soapxml/fault.go`
- Create: `gsoap/soapxml/codec.go`
- Test: `gsoap/soapxml/codec_test.go`
- Test: `gsoap/soapxml/fault_test.go`

### WSDL

- Create: `gsoap/wsdl/model.go`
- Create: `gsoap/wsdl/generator.go`
- Create: `gsoap/wsdl/publisher.go`
- Create: `gsoap/wsdl/parser.go`
- Create: `gsoap/wsdl/imports.go`
- Test: `gsoap/wsdl/generator_test.go`
- Test: `gsoap/wsdl/parser_test.go`

### Server

- Create: `gsoap/server/config.go`
- Create: `gsoap/server/server.go`
- Create: `gsoap/server/dispatch.go`
- Create: `gsoap/server/wsdl_handler.go`
- Test: `gsoap/server/server_test.go`

### Client

- Create: `gsoap/client/config.go`
- Create: `gsoap/client/client.go`
- Create: `gsoap/client/call.go`
- Create: `gsoap/client/fault.go`
- Test: `gsoap/client/client_test.go`

### Security

- Create: `gsoap/security/config.go`
- Create: `gsoap/security/username_token.go`
- Create: `gsoap/security/timestamp.go`
- Create: `gsoap/security/signature.go`
- Test: `gsoap/security/security_test.go`

### Docs And Examples

- Create: `gsoap/README.md`
- Create: `example/soap_server.go`
- Create: `example/soap_client.go`
- Modify: `README.md`

---

### Task 1: 建立最小包骨架与公共错误模型

**Files:**
- Create: `gsoap/errors.go`
- Create: `gsoap/options.go`
- Create: `gsoap/service.go`
- Test: `gsoap/service_test.go`

- [ ] **Step 1: 写失败测试，锁定公共错误和基本选项 API**

```go
func TestNewServiceAppliesOptions(t *testing.T) {
	svc := NewService(
		WithName("OrderService"),
		WithTargetNamespace("urn:example:order"),
	)

	require.Equal(t, "OrderService", svc.Name())
	require.Equal(t, "urn:example:order", svc.TargetNamespace())
}

func TestExportedErrorsAreComparable(t *testing.T) {
	require.ErrorIs(t, fmt.Errorf("wrap: %w", ErrInvalidEnvelope), ErrInvalidEnvelope)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap -run "TestNewServiceAppliesOptions|TestExportedErrorsAreComparable"`

Expected: FAIL，提示 `NewService`、`WithName`、`ErrInvalidEnvelope` 未定义

- [ ] **Step 3: 仅实现最小骨架通过测试**

实现内容：
- `Service` 基本结构
- `WithName`、`WithTargetNamespace`
- 公共错误常量或变量：`ErrInvalidEnvelope`、`ErrUnknownOperation`、`ErrUnsupportedSOAPVersion`、`ErrInvalidWSDL`、`ErrSecurityFailure`

- [ ] **Step 4: 再跑测试确认转绿**

Run: `go test ./gsoap -run "TestNewServiceAppliesOptions|TestExportedErrorsAreComparable"`

Expected: PASS

- [ ] **Step 5: 格式化**

Run: `gofmt -w gsoap/errors.go gsoap/options.go gsoap/service.go gsoap/service_test.go`

---

### Task 2: 实现统一契约模型与校验器

**Files:**
- Create: `gsoap/contract/definition.go`
- Create: `gsoap/contract/schema.go`
- Create: `gsoap/contract/validate.go`
- Test: `gsoap/contract/definition_test.go`

- [ ] **Step 1: 写失败测试，固定 Definition 的最小结构和校验规则**

```go
func TestDefinitionValidateRequiresServiceAndBinding(t *testing.T) {
	def := Definition{}
	err := def.Validate()

	require.ErrorIs(t, err, ErrInvalidDefinition)
}

func TestDefinitionValidateAcceptsMinimalDocumentLiteralService(t *testing.T) {
	def := minimalDefinition()
	require.NoError(t, def.Validate())
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/contract -run "TestDefinitionValidate"`

Expected: FAIL，提示 `Definition`、`Validate`、`ErrInvalidDefinition` 不存在

- [ ] **Step 3: 实现最小契约模型**

实现内容：
- `Definition`
- `Service`
- `Port`
- `Binding`
- `Operation`
- `Message`
- `Part`
- `Schema`
- `Element`
- `Fault`
- `Header`
- `Namespace`

- [ ] **Step 4: 实现校验器**

校验最少覆盖：
- target namespace 非空
- 至少一个 service
- operation 输入输出完整
- binding 风格必须属于支持范围
- import 引用不允许空地址

- [ ] **Step 5: 跑测试并格式化**

Run: `go test ./gsoap/contract -run "TestDefinitionValidate"`

Expected: PASS

Run: `gofmt -w gsoap/contract/definition.go gsoap/contract/schema.go gsoap/contract/validate.go gsoap/contract/definition_test.go`

---

### Task 3: 实现 Go 到契约的类型映射器

**Files:**
- Create: `gsoap/contract/type_mapper.go`
- Test: `gsoap/contract/type_mapper_test.go`

- [ ] **Step 1: 写失败测试，锁定基础映射规则**

```go
func TestTypeMapperMapsStructPointerSliceAndTime(t *testing.T) {
	mapper := NewTypeMapper()
	types, err := mapper.Map(reflect.TypeFor[CreateOrderRequest]())

	require.NoError(t, err)
	require.NotEmpty(t, types.Elements)
}

func TestTypeMapperRejectsUnsupportedInterfaceRoot(t *testing.T) {
	mapper := NewTypeMapper()
	_, err := mapper.Map(reflect.TypeOf((*any)(nil)).Elem())

	require.ErrorIs(t, err, ErrUnsupportedTypeMapping)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/contract -run "TestTypeMapper"`

Expected: FAIL，提示 `NewTypeMapper` 或 `ErrUnsupportedTypeMapping` 未定义

- [ ] **Step 3: 实现最小映射逻辑**

至少支持：
- 基础标量
- `struct`
- `[]T`
- `*T`
- `time.Time`
- `[]byte`
- `xml` 标签读取

- [ ] **Step 4: 补充显式失败路径**

至少对以下输入返回稳定错误：
- 根类型 `interface{}`
- 非导出匿名字段
- 不可表达的 map 根结构

- [ ] **Step 5: 跑测试并格式化**

Run: `go test ./gsoap/contract -run "TestTypeMapper"`

Expected: PASS

Run: `gofmt -w gsoap/contract/type_mapper.go gsoap/contract/type_mapper_test.go`

---

### Task 4: 实现 SOAP Envelope、Header、Fault 编解码

**Files:**
- Create: `gsoap/soapxml/constants.go`
- Create: `gsoap/soapxml/envelope.go`
- Create: `gsoap/soapxml/header.go`
- Create: `gsoap/soapxml/fault.go`
- Create: `gsoap/soapxml/codec.go`
- Test: `gsoap/soapxml/codec_test.go`
- Test: `gsoap/soapxml/fault_test.go`

- [ ] **Step 1: 写失败测试，覆盖 SOAP 1.1/1.2 编码差异**

```go
func TestCodecEncodesSOAP11Envelope(t *testing.T) {}
func TestCodecEncodesSOAP12Envelope(t *testing.T) {}
func TestCodecDecodesFaultWithDetail(t *testing.T) {}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/soapxml -run "TestCodec|TestFault"`

Expected: FAIL，提示 `Codec`、`Envelope`、`Fault` 未定义

- [ ] **Step 3: 实现最小 Envelope/Fault 结构**

实现内容：
- SOAP 1.1/1.2 namespace 常量
- `Envelope`
- `Body`
- `Header`
- `Fault`
- `Reason`
- `Detail`

- [ ] **Step 4: 实现编解码器**

要求：
- 输入输出使用 `encoding/xml`
- 可携带原始 header 片段
- fault detail 保留原始 XML

- [ ] **Step 5: 跑测试并格式化**

Run: `go test ./gsoap/soapxml -run "TestCodec|TestFault"`

Expected: PASS

Run: `gofmt -w gsoap/soapxml/constants.go gsoap/soapxml/envelope.go gsoap/soapxml/header.go gsoap/soapxml/fault.go gsoap/soapxml/codec.go gsoap/soapxml/codec_test.go gsoap/soapxml/fault_test.go`

---

### Task 5: 实现 WSDL 生成器和发布资源模型

**Files:**
- Create: `gsoap/wsdl/model.go`
- Create: `gsoap/wsdl/generator.go`
- Create: `gsoap/wsdl/publisher.go`
- Test: `gsoap/wsdl/generator_test.go`

- [ ] **Step 1: 写失败测试，锁定最小 WSDL 生成结果**

```go
func TestGeneratorBuildsDocumentLiteralWSDL(t *testing.T) {
	def := testDefinition()
	doc, assets, err := Generate(def)

	require.NoError(t, err)
	require.Contains(t, string(doc), "wsdl:definitions")
	require.NotEmpty(t, assets)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/wsdl -run "TestGenerator"`

Expected: FAIL，提示 `Generate` 未定义

- [ ] **Step 3: 实现最小生成器**

要求：
- 从 `contract.Definition` 生成主 `WSDL`
- 生成稳定的 namespace 前缀
- 生成 `service`、`portType`、`binding`、`message`、`types`

- [ ] **Step 4: 实现发布资源模型**

要求：
- 主文档资源
- import 资源表
- 每个资源都有路径、内容类型、字节内容

- [ ] **Step 5: 跑测试并格式化**

Run: `go test ./gsoap/wsdl -run "TestGenerator"`

Expected: PASS

Run: `gofmt -w gsoap/wsdl/model.go gsoap/wsdl/generator.go gsoap/wsdl/publisher.go gsoap/wsdl/generator_test.go`

---

### Task 6: 实现 Code First 反射构建器和根包注册 API

**Files:**
- Modify: `gsoap/service.go`
- Create: `gsoap/register.go`
- Modify: `gsoap/bind.go`
- Test: `gsoap/service_test.go`

- [ ] **Step 1: 写失败测试，固定注册函数到 operation 的映射**

```go
func TestRegisterBuildsOperationFromFunction(t *testing.T) {
	svc := NewService(
		WithName("OrderService"),
		WithTargetNamespace("urn:example:order"),
	)

	err := svc.Register("CreateOrder", func(ctx context.Context, req CreateOrderRequest) (CreateOrderResponse, error) {
		return CreateOrderResponse{}, nil
	})

	require.NoError(t, err)
	require.Len(t, svc.Definition().Bindings[0].Operations, 1)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap -run "TestRegisterBuildsOperationFromFunction"`

Expected: FAIL，提示 `Register` 或 `Definition` 未实现

- [ ] **Step 3: 实现反射注册**

要求：
- 识别 `context.Context`
- 识别单请求、单响应、单错误
- 为请求和响应生成默认包装元素
- 默认绑定为 `document/literal wrapped`

- [ ] **Step 4: 实现显式覆盖入口**

至少支持：
- 自定义 operation 名
- 自定义 style
- 自定义 action
- 显式 header/fault 描述

- [ ] **Step 5: 跑测试并格式化**

Run: `go test ./gsoap -run "TestRegisterBuildsOperationFromFunction"`

Expected: PASS

Run: `gofmt -w gsoap/service.go gsoap/register.go gsoap/bind.go gsoap/service_test.go`

---

### Task 7: 实现运行时 Server 和 `?wsdl` 发布

**Files:**
- Create: `gsoap/server/config.go`
- Create: `gsoap/server/server.go`
- Create: `gsoap/server/dispatch.go`
- Create: `gsoap/server/wsdl_handler.go`
- Test: `gsoap/server/server_test.go`

- [ ] **Step 1: 写失败测试，先跑通最小服务端链路**

```go
func TestServerServesSOAPRequestAndWSDL(t *testing.T) {}
func TestServerReturnsSOAPFaultForBusinessError(t *testing.T) {}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/server -run "TestServer"`

Expected: FAIL，提示 `New`、SOAP dispatch 或 WSDL handler 缺失

- [ ] **Step 3: 实现配置与 HTTP 绑定**

要求：
- 可复用外部 `ghttp/gserver.Server`
- 支持 endpoint path
- `GET ?wsdl` 返回主文档
- import 资源单独路由

- [ ] **Step 4: 实现 dispatch**

要求：
- 识别 SOAP 版本
- 解析 `SOAPAction`
- 选择 operation
- 调业务函数
- 成功响应转 envelope
- 错误转 `SOAP Fault`

- [ ] **Step 5: 跑测试并格式化**

Run: `go test ./gsoap/server -run "TestServer"`

Expected: PASS

Run: `gofmt -w gsoap/server/config.go gsoap/server/server.go gsoap/server/dispatch.go gsoap/server/wsdl_handler.go gsoap/server/server_test.go`

---

### Task 8: 实现基于契约的 Client 调用

**Files:**
- Create: `gsoap/client/config.go`
- Create: `gsoap/client/client.go`
- Create: `gsoap/client/call.go`
- Create: `gsoap/client/fault.go`
- Test: `gsoap/client/client_test.go`

- [ ] **Step 1: 写失败测试，锁定定义驱动的调用 API**

```go
func TestClientCallEncodesRequestAndDecodesResponse(t *testing.T) {}
func TestClientCallReturnsSOAPFault(t *testing.T) {}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/client -run "TestClient"`

Expected: FAIL，提示 `Client`、`Call` 或 fault 解析逻辑缺失

- [ ] **Step 3: 实现最小 Client**

要求：
- 接收 `contract.Definition`
- 根据 operation 构造 envelope
- 调用现有 `ghttp/gclient`
- 解包正常响应
- 解包 fault

- [ ] **Step 4: 实现调用级选项**

至少支持：
- endpoint 覆盖
- header 注入
- timeout 覆盖
- action 覆盖

- [ ] **Step 5: 跑测试并格式化**

Run: `go test ./gsoap/client -run "TestClient"`

Expected: PASS

Run: `gofmt -w gsoap/client/config.go gsoap/client/client.go gsoap/client/call.go gsoap/client/fault.go gsoap/client/client_test.go`

---

### Task 9: 实现 WSDL 解析与 Contract First 绑定

**Files:**
- Create: `gsoap/wsdl/parser.go`
- Create: `gsoap/wsdl/imports.go`
- Modify: `gsoap/bind.go`
- Test: `gsoap/wsdl/parser_test.go`
- Modify: `gsoap/service_test.go`

- [ ] **Step 1: 写失败测试，锁定 WSDL 加载和绑定行为**

```go
func TestParserLoadsDefinitionWithImportedSchema(t *testing.T) {}
func TestBindAttachesImplementationToParsedOperation(t *testing.T) {}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/wsdl -run "TestParser"`

Expected: FAIL，提示 `LoadFile`、`LoadBytes`、import 解析器不存在

- [ ] **Step 3: 实现最小解析器**

要求：
- 解析主 `WSDL`
- 解析 `message/part`
- 解析 `portType/operation`
- 解析 `binding/service/port`

- [ ] **Step 4: 实现 import 处理与绑定入口**

要求：
- 支持 `wsdl:import`
- 支持 `xsd:import`
- `gsoap.Bind` 可将实现函数绑定到已解析 operation

- [ ] **Step 5: 跑测试并格式化**

Run: `go test ./gsoap/wsdl -run "TestParser"`

Expected: PASS

Run: `gofmt -w gsoap/wsdl/parser.go gsoap/wsdl/imports.go gsoap/bind.go gsoap/wsdl/parser_test.go gsoap/service_test.go`

---

### Task 10: 补齐 SOAP 1.2、`rpc/literal` 与 import 发布细节

**Files:**
- Modify: `gsoap/soapxml/constants.go`
- Modify: `gsoap/wsdl/generator.go`
- Modify: `gsoap/server/dispatch.go`
- Modify: `gsoap/client/call.go`
- Modify: `gsoap/server/server_test.go`
- Modify: `gsoap/client/client_test.go`
- Modify: `gsoap/wsdl/generator_test.go`

- [ ] **Step 1: 写失败测试，固定高阶协议边界**

```go
func TestServerDispatchesSOAP12Request(t *testing.T) {}
func TestClientCallsRPCStyleOperation(t *testing.T) {}
func TestGeneratorPublishesImportedAssetsWithStablePaths(t *testing.T) {}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/... -run "TestServerDispatchesSOAP12Request|TestClientCallsRPCStyleOperation|TestGeneratorPublishesImportedAssetsWithStablePaths"`

Expected: FAIL，提示 SOAP 1.2、`rpc/literal` 或 import 路径行为不正确

- [ ] **Step 3: 扩展实现**

要求：
- 区分 SOAP 1.1 与 SOAP 1.2 `Content-Type`
- `rpc/literal` 入参与出参包装规则独立
- import 路径稳定且可由发布器回放

- [ ] **Step 4: 跑相关测试确认转绿**

Run: `go test ./gsoap/... -run "TestServerDispatchesSOAP12Request|TestClientCallsRPCStyleOperation|TestGeneratorPublishesImportedAssetsWithStablePaths"`

Expected: PASS

- [ ] **Step 5: 格式化**

Run: `gofmt -w gsoap/soapxml/constants.go gsoap/wsdl/generator.go gsoap/server/dispatch.go gsoap/client/call.go gsoap/server/server_test.go gsoap/client/client_test.go gsoap/wsdl/generator_test.go`

---

### Task 11: 实现安全扩展点与内建 UsernameToken / Timestamp

**Files:**
- Create: `gsoap/security/config.go`
- Create: `gsoap/security/username_token.go`
- Create: `gsoap/security/timestamp.go`
- Create: `gsoap/security/signature.go`
- Test: `gsoap/security/security_test.go`
- Modify: `gsoap/client/client.go`
- Modify: `gsoap/server/server.go`

- [ ] **Step 1: 写失败测试，固定安全扩展点与内建能力**

```go
func TestUsernameTokenIsInjectedIntoHeader(t *testing.T) {}
func TestServerRejectsExpiredTimestamp(t *testing.T) {}
func TestSignatureHookIsInvoked(t *testing.T) {}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./gsoap/security -run "TestUsernameToken|TestServerRejectsExpiredTimestamp|TestSignatureHookIsInvoked"`

Expected: FAIL，提示安全配置、header 注入或校验器缺失

- [ ] **Step 3: 实现安全处理链**

要求：
- 客户端发送前可挂安全 header
- 服务端解包后可执行安全校验
- 签名逻辑先提供接口和调用点

- [ ] **Step 4: 跑测试确认转绿**

Run: `go test ./gsoap/security -run "TestUsernameToken|TestServerRejectsExpiredTimestamp|TestSignatureHookIsInvoked"`

Expected: PASS

- [ ] **Step 5: 格式化**

Run: `gofmt -w gsoap/security/config.go gsoap/security/username_token.go gsoap/security/timestamp.go gsoap/security/signature.go gsoap/security/security_test.go gsoap/client/client.go gsoap/server/server.go`

---

### Task 12: 文档、示例和最终验证

**Files:**
- Create: `gsoap/README.md`
- Create: `example/soap_server.go`
- Create: `example/soap_client.go`
- Modify: `README.md`

- [ ] **Step 1: 写失败测试或示例验证脚本前，先整理 API 示例**

至少覆盖：
- `Code First` 服务发布
- `?wsdl` 暴露
- `FromWSDL` 构造 client
- `Contract First` 绑定实现
- `UsernameToken` 配置

- [ ] **Step 2: 编写 README 与示例**

要求：
- `gsoap/README.md` 介绍能力边界和快速开始
- 根 `README.md` 增加模块入口
- `example/soap_server.go` 与 `example/soap_client.go` 可作为 smoke sample

- [ ] **Step 3: 跑相关单元测试**

Run: `go test ./gsoap/...`

Expected: PASS

- [ ] **Step 4: 跑全仓库测试与 lint**

Run: `go test ./...`

Expected: PASS

Run: `golangci-lint run`

Expected: PASS

- [ ] **Step 5: 全量格式化并复查 git 变更**

Run: `gofmt -w gsoap/*.go gsoap/contract/*.go gsoap/soapxml/*.go gsoap/wsdl/*.go gsoap/server/*.go gsoap/client/*.go gsoap/security/*.go example/soap_server.go example/soap_client.go`

Run: `git status --short`

Expected: 只出现本次计划涉及文件

---

## Suggested Commit Sequence

1. `feat: add gsoap contract model and root package skeleton`
2. `feat: add gsoap soapxml codec and wsdl generator`
3. `feat: add gsoap runtime server and client`
4. `feat: add gsoap wsdl parser and contract-first binding`
5. `feat: add gsoap security support and docs`

## Risks To Watch During Execution

- `encoding/xml` 对 namespace 前缀控制有限，生成器和编解码器要避免依赖前缀字面量而改为依赖 namespace URI
- `rpc/literal` 与 `document/literal wrapped` 不能共用同一套包装推导规则
- 反射注册阶段如果默认过多，会导致 `Code First` 和 `Contract First` 行为不一致
- import 路径一旦暴露到 `WSDL`，后续不应频繁改名
- 安全层先定接口和消息流转位置，再补算法细节，避免一次做深导致主链路迟迟跑不通

## Execution Recommendation

推荐执行顺序：

1. `Task 1` 到 `Task 8`
   - 先跑通 `Code First -> Server -> WSDL -> Client`
2. `Task 9`
   - 补 `Contract First`
3. `Task 10` 到 `Task 11`
   - 补高阶协议与安全
4. `Task 12`
   - 做文档、示例和最终验证
