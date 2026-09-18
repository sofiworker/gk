# 模型契约

[English](README.en.md)

开发中，禁止生产使用。公共契约与厂商无关：Info 描述逻辑模型能力，Parameters 描述本次生成参数，Request/Response 描述模型调用，Catalog 提供可选的信息查询。未指定参数与显式零值通过指针区分。

Agent 定义不持有模型；执行器持有 Client，每次调用传入逻辑 ModelSelection。厂商模型名、部署地址、凭据、协议字段和专属参数由独立协议实现持有，不进入公共参数结构。适配器应拒绝不支持的选项，不得静默丢弃。

Message 不含会话消息 ID 和持久化时间；复用 core.Content 表达内容，协议实现须显式转换，不得直接序列化为厂商请求。Response 提供逻辑模型身份、用量与结束原因。Streaming 能力信息不代表 SDK 已提供流式调用接口；当前仅实现 Generate。

HTTP 组合入口见 [modelhttp](../adapters/modelhttp/README.md)。

Request.Validate 校验角色、唯一内容载荷、工具声明、ToolChoice 和调用结果配对。Response.Validate 校验响应角色、结束原因、工具名称及 ID、非负用量；JSON 输出验证 JSON 语法，Schema 输出必须提供 SchemaValidator。该回调负责实际 Schema 或业务约束，SDK 不内置完整 JSON Schema 引擎。校验不估算上下文 token，也不推断具体厂商的能力。直接调用 model.Client 或 modelhttp.Client 时由调用方使用这些校验入口，Runner 自动执行。ClientFunc 可将应用函数适配为 Client。
