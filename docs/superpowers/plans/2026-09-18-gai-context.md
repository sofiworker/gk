# Context 分阶段实施

当前状态：已有功能是原型验证，不代表 SDK 公开 API 已确定。下一阶段以 [SDK 生命周期重设计](../specs/2026-09-18-gai-sdk-api-lifecycle.md) 为准，先收敛 Agent/Session 使用入口及 Hook 边界，再迁移压缩实现；暂停向 Runner 继续添加功能特判。

对应 [设计草案](../specs/2026-09-18-gai-context-design.md)。

- [x] core.ContextPolicy、TokenMeasurement、ContextSnapshot，ModelCall 关联输入快照。
- [x] gai/context 构建、完整工具交互分组、按模型解析预算与 Counter 注入。
- [x] Runner 每次请求前重建，Runtime 原子提交快照与调用意图，Store 禁止快照改写。
- [x] 验证硬超限阻止模型请求、工具执行后重新计量、输入复制及快照记录。
- [x] 可运行 SDK 示例与双语文档。
- [ ] 独立 CompactionRecord、会话级原子变更与空闲压缩占用。
- [ ] Resolver、来源版本固定与完整 ContextSnapshot 重放信息。
- [ ] 同步自动摘要、显式 Compact、压缩成本预算及无收益终止。
- [ ] 模型请求压缩、上游 context-limit 安全恢复。

当前明确限制：非零 ContextPolicy 需要 Builder；零策略未配置 Builder 时保留 unknown 计量兼容路径。软阈值标记 NeedsCompaction，配置 Compactor 和 MaxCompactions 后触发摘要。快照只提供输入摘要与来源 ID，不声称完整重放。计数器由应用/模型适配提供，不内置厂商 tokenizer。

- [x] 活动 Run 内同步自动摘要、独立摘要预算、压缩意图/激活原子提交及跨 Turn 复用。
- [x] 硬超限恢复、软失败降级、无收益/空输出拒绝、激活提交失败停止生成测试。

更新：MaxCompactions 限制每 Run 摘要调用数；每个模型步骤最多尝试一次。压缩记录暂归发起 Turn，尚未实现空闲压缩的 SessionMutation。分块摘要和准备失败记录仍未实现。
