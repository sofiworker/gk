# Changelog

本仓库遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.x 阶段 API 允许破坏性变更，但每个破坏性变更必须记录在对应版本节。

## [Unreleased]

### Added

- ghttp：Go 1.27 泛型方法 API（Server/Group 根组动词链、`ToNoInput`/`ToNoOutput`、builder 级 `Group`、`Client.Get/Post/Put/Delete` 类型化方法）。
- ghttp：WebSocket 生产化——Timeout 中间件支持 `Hijack`/`Flush`、raw 消息、context 感知读写、子协议协商、keepalive、路由级 Origin 覆盖、升级/处理错误日志。
- ghttp：SSE `WriteJSONWithID`（Last-Event-ID 续传）。
- ghttp：client 正式实现——重试、before/after 钩子、错误模型绑定、输出文件、BasicAuth/认证 scheme、查询参数/字符串、按请求超时、client cookies、Response 访问器、调试日志。
- 仓库：golangci-lint 配置与全仓 lint 清零；CI 增加 Go 1.27 预览、race、coverage、gofmt、lint 门禁。
- 仓库：模块依赖三层分层原则设计文档（基础契约层 / 能力层 / 适配层）。
- gretry：导出 `NextDelay`/`Wait` 作为仓库唯一退避/抖动实现；ghttp client 与 gsd 重试统一复用，删除各自内联实现。
- glog：核心去除 OpenTelemetry 硬依赖，trace 字段改为可选 `WithTraceExtractor` 注入。
- ghttp：新增与 gerr 的错误互操作——`FromGerr`/`ToGerr`/`GerrStatus` 双向转换与 HTTP 状态码 ↔ `gerr.Kind` 映射。
- gotel：移除未实现的 `OTELProvider` 空壳与死代码，收敛为纯可观测性抽象（不依赖 OpenTelemetry）。
- gsql：默认日志改为标准库实现，核心不再依赖 glog（保留 `Logger` 接口，用户可注入 glog 适配器）。
- 仓库：新增 `scripts/check-deps.sh` 依赖方向检查（能力层互引即失败），接入 Makefile 与 CI。
- gresolver：`ParseResolveFile` 完整解析 nameserver/search/domain/options（含 ndots/timeout/attempts），`DefaultNameservers` 返回副本。
- gconfig：新增 `Get/GetInt/GetBool/GetDuration/GetStringSlice/GetStringMap/Set/UnmarshalKey` 类型化访问器（懒加载）。
- gcrypt：新增 AES-GCM（`AESGCMEncrypt/Decrypt`）、Ed25519（`GenerateEd25519Key`/`SignWithEd25519`/`VerifyWithEd25519`）、`RandomBytes`。
- gcompress：新增 zlib/flate 压缩（`Zlib*`/`Flate*`）。
- glog：新增 `DebugfContext/InfofContext/WarnfContext/ErrorfContext`。
- gcache：新增 `GetOrSet`/`GetOrSetWithContext`（loader 模式，未命中自动加载并写入），Memory/Redis/Valkey 均实现。
- gnet：`netinfo.Interface` 移除永不填充/错位字段（DNSServers/DHCPServer/Location/VendorID/DeviceID），新增 `BusInfo`/`DriverVersion` 正确映射 ethtool；`capture` 新增 `WithFilterInstructions`；`netinfo` 补测试与 gnet 子包文档。
- 仓库：新增文档语言规范（README 与注释统一**中英双语**、错误消息保持英文、标识符与测试名保持英文），全部包 README 统一为中文为主的双语文档。
- 仓库：注释语言规范修订为**中英双语**（中文在前、英文在后），并规定冗余注释（复述代码、无信息量标签）直接删除；首批完成 gresolver/gsql/gcache 库文件与 gretry/grx/gconfig/gresolver/gcrypt/gsd/glog 测试注释的清理。
- 仓库：第二批复述型注释清理（gcache/gsql/gcompress 测试中的纯标签删除），进度见 `docs/language-sweep.md`。
- 仓库：注释语言规范修订为**中英双语**（中文在前、英文在后），并规定冗余注释直接删除；已完成 ghttp/gnet/各包注释与根 README、小包 README 的双语化，gcache/ghttp README 待办。
- 仓库：README 双语化全部完成（根 README、各包 README 均中英双语）。
- 仓库：README 按语言拆分完成——全部包 README 拆为 `README.md`（中文）与 `README.en.md`（英文），互相链接；代码注释保持中英双语内联。

### Changed

- ghttp：已配置 `Consumes` 时缺失 Content-Type 返回 415（原为按 JSON 解析）。
- ghttp：CORS 只对真正的预检请求（OPTIONS + Origin + Access-Control-Request-Method）短路。
- ghttp：Group 中间件在创建子组时快照（gin 语义），与 produces/consumes 快照一致。
- ghttp：RequestID 默认上限 128，超长客户端值替换为新 ID。
- ghttp：Timeout 超时取消 context 并丢弃迟到写入；writer 支持 Hijack/Flush。
- ghttp：路径参数提取单次化，提取错误走路由级错误管线；SSE handler 错误落日志；`Server.Use` 链式化。
- ghttp：client 钩子顺序固定为 client-before → request-before → 发送 → client-after → request-after。

### Deprecated

- ghttp（Go 1.27 构建）：`Route[Req,Resp](target)` 兼容 shim，新代码使用动词直挂。
- ghttp：`WithStrictContentNegotiation()` / `WithStrictContentType()` no-op 别名，计划 v0.2 移除。

### Removed

- ghttp：**删除 StructInput 老 API 全族**（性能收敛）：`StructInput[T]`（结构体 tag 绑定）、`Body[T]` 惰性请求体视图、`ParseInput`、`WithBodyDecoder`、`ErrInvalidParamsUsage`、`ErrMultipleBodyFields`、`ErrBodyFieldMustBeValue` 及全部结构体 tag 解析机器（`structInfo`/`parseCompiledInput`/`bindDirectPathInput` 等）。结构体 tag 绑定路径是每请求开销的最大单笔来源；迁移方式：`MapInputs(PathInt64("id"), ...)` 显式描述器 + `InputFunc`/`JSONBody[T]`。中间件读取路径参数改用 `Server.MatchedParams(r)`。
- ghttp：删除基于结构体类型的 OpenAPI 反推死代码（`compileRouteOpenAPIMetadata`/`extractParametersFromType`/`extractBodySchema` 及 `routeDefinition.reqType/respType`）；OpenAPI 现完全由显式输入/输出描述器元数据生成。
- ghttp：旧 `openAPIBuilder` 死代码、未用的 util/form 辅助函数、client 未用字段。
- 仓库：删除基于已移除 RouteBuilder API 的陈旧示例（`example/ghttp_usage`、`example/server_review`、`example/server_review_v2`、`example/server_review_v3`）；评审结论已沉淀在 `docs/ghttp-server-review*.md`。
- gnet/rawcap：库内 demo `main` 文件（不做 demo，方向改为真实库）。

## [0.1.0] - 待发布

首版公开 API 快照（见 `docs/superpowers/specs/2026-08-07-ghttp-api-freeze-v0.1.md`）。开发中，禁止生产使用。
