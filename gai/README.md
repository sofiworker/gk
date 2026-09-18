# gai

[English](README.en.md)

## SDK 示例与重构状态

新的声明与使用示例见 [examples](examples/README.md)，覆盖 Agent、Session、Sandbox 和 Hook。**这些接口仍是设计草案，尚不可运行。**

旧的手工组装示例已删除，examples 统一展示新 API。

**破坏性变更**：Runner 拒绝无消息请求、未声明工具的模型调用和非法响应；Schema 输出未配置验证器时拒绝执行。


开发中，禁止直接用于生产开发，参见 [DEVELOPMENT.md](../DEVELOPMENT.md)。

当前提供 `gai/core` 数据结构与共享工具/环境契约，以及 `gai/sandbox` 进程内内存 Sandbox，按照 [Session / Turn 设计](../docs/superpowers/specs/2026-09-13-gai-session-turn-design.md) 从头重写，不兼容旧 API 或旧存储格式。

- Session：Metadata、Turns、存储版本和时间。
- Turn：Metadata、Messages、模型调用、工具执行（含审批）、可选摘要和状态。
- Message：角色、多模态内容、结构化工具请求或结果。
- ModelCall：请求模型、实际模型、重试关联、Usage 和时间。

模型、Agent 和环境通过标识及执行版本引用，核心不包含厂商协议或凭据。`TurnNotification` 表示系统展示轮次，不能自动成为模型指令。调用结果通过消息 ID 引用历史，不重复保存消息正文。

core 结构体本身不强制只追加、不提供 CAS，也不执行权限检查；Sandbox 实现对经过其接口的文件与网络操作进行检查。现有 runtime 提供非流式执行，session/memory 提供 CAS 与提交日志；流式执行和查询分页尚未实现。切片表示已提供的有序数据，本阶段不定义部分加载语义。

## 时间约定

所有时间点使用 `int64` Unix 毫秒时间戳，通过 `time.Now().UnixMilli()` 生成；可选时间使用 `*int64`，`nil` 表示未设置，例如尚未结束或未配置到期时间。时间戳不用于消息排序或 CAS。

**破坏性变更**：时间字段从 `time.Time` / `*time.Time` 改为 `int64` / `*int64`，JSON 表达由时间字符串变为数字或 null。

**破坏性变更**：`Turn.Summaries []Summary` 改为 `Turn.Summary *Summary`，nil 表示尚无摘要。

## Agent 核心

`core.Agent` 是行为配置，包含 ID、Version、Name、Prompt、Tools 和 ContextPolicy。Tools 直接使用 `[]Tool` 保存可复用实现，执行环境通过 ToolContext 按调用注入。没有工具时用于普通 Chat。

**破坏性变更**：移除 `Agent.Model`。模型选择由 Session 提供，本轮显式选择可以覆盖会话选择，并记录在 `Turn.Metadata.Model`；两者均未指定时拒绝模型调用。模型客户端由运行时持有，Agent 不持有模型选择或模型客户端。runtime.Run 解析会话或本轮模型选择，并交由 model.Client 调用。

- **破坏性变更**：移除 Agent.Workspace、Agent.Scope、Agent.Sandbox 以及 Scope、WorkspaceRef、SandboxRef 类型。Session/Turn/Approval 改用 EnvironmentBinding，记录环境 ID、Revision、PolicyVersion 和 Dir；绑定本身不授予权限。
- **破坏性变更**：Tool.Execute 增加显式 ToolContext 参数，按调用注入受限文件、网络和资源申请接口；不通过全局环境或共享可变工具实例关联 Session。
- Prompt 保存静态系统指令及有序 Skill/Resource 来源；来源指定 system 槽位并不自动获得信任。
- Session 引用 Agent，Turn 记录采用的绑定；这些结构不执行权限判断或处理对话。

具体字段和绑定规则见设计文档第 12 节。执行入口见 [Runtime](runtime/README.md)，协议映射由独立适配器提供。

## 内存 Sandbox

详见 [Sandbox 使用说明](sandbox/README.md) 和 [设计文档](../docs/superpowers/specs/2026-09-17-gai-sandbox-design.md)。

内置 memory 实现提供自定义虚拟目录、资源绑定、文件内容版本、审计、快照、立即/显式同步及资源申请。local 显式适配宿主导入和冲突检测写回，httpgate 提供受控 HTTP。创建环境无需宿主目录、数据库或第三方 Sandbox，状态不自动落盘。

Runtime 默认按 Session 分配独立内存 Sandbox；Turn/Session 完成时由应用按策略调用 Sync。进程内自定义 Go 工具属于可信宿主代码，不受操作系统级隔离。

## 工具集与执行

**破坏性变更**：移除 ToolRef 和 Registry，Agent.Tools 改为 []Tool。使用 tool.New(agent.Tools, options...) 直接构建执行工具集，固定描述及版本并检查名称冲突；Definitions 提供模型描述，Execute 完成校验、显式授权和分发。执行记录中的 Binding.ID 使用工具名称。

NewFunction 支持类型化函数适配，环境通过每次调用的 ToolContext 注入。默认拒绝未配置授权策略的执行，不自动重试或写入 Session。详见 [工具说明](tool/README.md) 与可运行的 ExampleNew。

## Agent 与模型调用

新增基础 [agent.Runner](agent/README.md)，通过厂商无关的 [model.Client](model/README.md) 执行非流式模型/工具循环。[协议组合](adapters/modelhttp/README.md) 与 [HTTP 传输](transport/httptransport/README.md) 独立。破坏性变更：移除旧 HTTP Codec/JSONCodec 入口，调用方须显式提供协议实现。[runtime.Runtime](runtime/README.md) 已接入 Session/Turn、模型与工具事实记录、取消和默认独立 Sandbox；[session/memory](session/README.md) 提供原子提交与操作去重。磁盘持久化、审批恢复和进程重启恢复尚未实现。

模型上下文第一阶段已接入逐调用构建、预算检查和原子快照记录，见 [Context](context/README.md)。自动摘要通过独立 Compactor 显式启用；未配置 Builder 时为 unknown 预算兼容模式。
