# Agent 执行器

[English](README.en.md)

开发中，禁止生产使用。`New(definition, client, options...)` 构建可复用 Runner；Agent 定义不持有模型，Runner 持有 model.Client。

`Run(ctx, Input{Model: ..., Messages: ..., Tools: ...})` 执行非流式模型—工具循环。每次运行明确提供模型 ID、历史及 ToolContext；默认最多 8 次模型请求、32 次工具调用。WithLimits 修改预算，WithToolOptions 接入现有工具授权策略，缺少授权默认拒绝。

工具串行执行，响应中的重复调用 ID 拒绝。工具错误停止循环并返回部分结果，不自动重试。Result 包含新增消息、模型响应与工具执行结果；调用方负责持久化、Session/Turn 版本检查、审批及同步。不得把失败后的整个 Run 直接重试当作无副作用操作。

目前支持静态 Prompt.System；Prompt.Sources 明确拒绝，等待来源解析器。尚无流式响应、模型回退、自动会话存储与任务恢复。模型客户端、工具实现及注入能力均属于可信代码，须支持并发与取消。固定到单个 ToolCallID 的 Sandbox View 应由调用方按需重新构造；跨多工具调用可以使用会话/轮次级能力视图。

模型 HTTP 入口见 [httptransport](../transport/httptransport/README.md)。

需要会话执行入口时使用 [Runtime](../runtime/README.md)。Runner.Input.Capabilities 可按调用创建能力视图，Checkpoint 在外部调用前后保存增量事实的完整快照；回调失败立即停止。Result.ModelRecords/ToolRecords 保存含 ID、时间和失败状态的记录，消息中的 ExecutionID 与工具执行事实关联。Runtime 负责失败终态保存，调用记录中的 FinishReason 保留归一化结束原因。

WithOutputValidator 注入结构化输出验证器。Runner 发送前执行 Request.Validate，返回后执行 Response.Validate；Schema 输出缺少验证器时在模型调用前拒绝。新 API 用法见 [SDK 示例](../examples/README.md)，接口尚待实现。

WithContextBuilder 注入逐调用构建和计量，Agent.ContextPolicy 声明软/硬输入上限；详情见 [Context](../context/README.md)。每次模型调用的 ContextSnapshot 经由 Checkpoint 交给 Runtime 保存。
