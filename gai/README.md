# gai

[English](README.en.md)

开发中，禁止直接用于生产开发，参见 [DEVELOPMENT.md](../DEVELOPMENT.md)。

当前提供 `gai/core` 数据结构与执行接口契约，按照 [Session / Turn 设计](../docs/superpowers/specs/2026-09-13-gai-session-turn-design.md) 从头重写，不兼容旧 API 或旧存储格式。

- Session：Metadata、Turns、存储版本和时间。
- Turn：Metadata、Messages、模型调用、工具执行（含审批）、可选摘要和状态。
- Message：角色、多模态内容、结构化工具请求或结果。
- ModelCall：请求模型、实际模型、重试关联、Usage 和时间。

模型、Agent、Workspace、Scope 和 Sandbox 通过标识及版本引用，核心不包含厂商协议或凭据。`TurnNotification` 表示系统展示轮次，不能自动成为模型指令。调用结果通过消息 ID 引用历史，不重复保存消息正文。

结构体本身不强制只追加、不提供 CAS，也不执行权限检查。当前未实现运行时、Store、流式执行、查询分页或状态更新日志。切片表示已提供的有序数据，本阶段不定义部分加载语义。

## 时间约定

所有时间点使用 `int64` Unix 毫秒时间戳，通过 `time.Now().UnixMilli()` 生成；可选时间使用 `*int64`，`nil` 表示未设置，例如尚未结束或未配置到期时间。时间戳不用于消息排序或 CAS。

**破坏性变更**：时间字段从 `time.Time` / `*time.Time` 改为 `int64` / `*int64`，JSON 表达由时间字符串变为数字或 null。

**破坏性变更**：`Turn.Summaries []Summary` 改为 `Turn.Summary *Summary`，nil 表示尚无摘要。

## Agent 核心

`core.Agent` 是声明式配置，包含 Workspace、Scope、Sandbox、Prompt、Model 和 Tools。Tools 使用 `ToolRef` 引用工具，不保存可执行实例。没有工具时用于普通 Chat。

- Workspace 和 Sandbox 通过 ID、Version 引用；未设置不代表允许宿主资源访问或无隔离执行。
- Scope 描述精确资源范围和能力标识；空列表不授予能力，不隐式支持通配符。
- Prompt 保存静态系统指令及有序 Skill/Resource 来源；来源指定 system 槽位并不自动获得信任。
- Session 引用 Agent，Turn 记录采用的绑定；这些结构不执行权限判断或处理对话。

具体字段和绑定规则见设计文档第 12 节。当前不包含 Agent 执行接口、模型请求协议或执行循环。
