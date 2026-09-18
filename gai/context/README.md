# 模型上下文

[English](README.en.md)

开发中，禁止生产使用。本包实现设计第一阶段：每次请求的结构验证、完整工具交互分组、预算检查和输入摘要，不删除历史；自动摘要以独立派生记录生效。

用 `New(counter, resolver)` 构建 Builder，通过 `agent.WithContextBuilder(builder)` 注入 Runner；Runtime 使用 `runtime.WithAgentOptions(...)` 传递。Agent.ContextPolicy 配置输入硬上限及软阈值。新 API 使用示例见 [examples](../examples/README.md)，接口尚待实现。

```go
builder, err := gcontext.New(counter, gcontext.FixedBudget(gcontext.Limits{
    ModelID:       "logical-model",
    SharedWindow:  32768,
    OutputReserve: 4096,
    SafetyMargin:  1024,
}))
```

counter 由模型适配提供，Measure 接收完整 model.Request，必须包含消息、工具定义、输出格式、媒体及协议输入开销。计量必须标记 exact 或 estimated，并提供计数器 ID/Version；未知不能按零处理。SDK 没有内置厂商 tokenizer。示例中的 JSON 字节计数只是本地演示，不能用于推断真实模型窗口。

Limits 必须按逻辑模型解析，FixedBudget 拒绝其他模型。SharedWindow 表示输入输出共享窗口；InputTokens 是独立输入上限，零表示该项未知。安全余量从已知模型输入额度扣除，再与 Agent 硬上限取最小值。输出预留优先采用本次 MaxOutputTokens，否则采用 OutputReserve 并写入实际请求。输入预算和输出预留无法确定时返回 ErrBudgetUnknown。

Builder.Build 不调用模型，不写 Store；Plan 包含冻结请求、半开消息区间 Groups、输入摘要、计量、有效输入上限及 NeedsCompaction。工具请求和全部结果组成不可拆分组；系统消息、最新用户消息与最后一个完整组标记 Required。当前只标记安全边界，不裁剪。对估算计数只能提供基于估算的窗口检查，无法保证上游绝不超限。

超过软阈值设置 NeedsCompaction，超过硬预算返回 ErrOverflow。配置 Compactor 后 Runner 会尝试摘要并重新计量；成功前不发送超限请求。材料 Resolver、模型主动压缩和应用 Compact 入口尚未实现。首版 Builder 不允许改写消息、工具或已有参数，仅可补充未指定的输出预算，防止来源记录与实际请求不一致。

Runner 每次调用都重新构建，包括工具结果追加后的请求。ContextSnapshot 与 ModelCall running 一起由 Runtime 原子提交；ModelCall.ContextSnapshotID 关联快照，Store 禁止改写旧快照。快照存储源消息 ID、Agent/模型身份、实际请求 JSON 的 SHA-256、计量与预算，不包含认证信息。它不是完整请求正文备份，也不是厂商 wire 请求哈希；资源版本与完整重放留待后续阶段。

兼容边界：未配置 Builder 且 Agent.ContextPolicy 为零时，仅执行 Inspect，快照计量明确为 unknown，不提供窗口保证。声明了非零 ContextPolicy 却没有 Builder 会在构建 Agent 时失败。该兼容路径不代表设计中的严格预算模式已成为默认值。

## 自动摘要

使用 NewCompactor(summaryClient, summaryBuilder, logicalSummaryModel, maxOutputTokens, timeout)，通过 agent.WithCompactor 注入，同时设置 Agent.ContextPolicy.MaxCompactions（每次 Run 的摘要调用次数上限）。摘要客户端与主模型独立，summaryBuilder 必须能计量摘要模型；模型调用禁用工具，不递归进入 Agent。

每次主模型请求前，达到软阈值或发生硬超限时尝试选择最早连续的非 required 完整交互组。摘要请求先检查自身预算；摘要必须非空、正常结束，替换后的完整请求必须变小且满足硬预算，才能激活。每次模型请求前最多尝试一次；达到每 Run 上限后不再尝试。软阈值失败但原请求仍合法时继续原请求，硬超限失败则停止。

CompactionRecord 保存在发起执行的 Turn 中，覆盖来源可以跨轮次。它记录精确 message:/summary: 来源、摘要、独立模型调用与用量、请求摘要、计量及状态。Runtime 先保存运行中记录，再调用摘要模型；激活提交成功后才采用新视图。后续 Turn 按顺序复用 Applied 记录，原始消息不变。摘要映射为带明确前缀的历史数据消息，不授予 system 权限。

当前仅支持活动 Run 内同步自动摘要，不提供空闲会话 Compact、分块摘要、跨进程恢复或长期记忆。摘要输入超限会失败而不静默裁剪。摘要准备失败尚不生成独立尝试记录；已有模型请求会记录成功/失败。终结压缩记录不可改写。未配置 Checkpoint 的直接 Runner 只在返回 Result 中保留记录，持久化保障由 Runtime/Store 提供。
