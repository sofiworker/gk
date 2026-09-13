package core

// Agent 描述一次执行所需的行为和边界；它不是运行实例，也不持有 Session 或连接。
// Agent describes behavior and boundaries for execution; it is not a runtime instance and owns no session or connection.
// Chat 是 Tools 为空的 Agent 配置，不另设 Chat 类型。
// Chat is an Agent configuration with no Tools and has no separate Chat type.
type Agent struct {
	ID        string
	Version   string
	Name      string
	Prompt    Prompt
	Model     ModelSelection
	Workspace WorkspaceRef
	Scope     Scope
	Sandbox   SandboxRef
	Tools     []ToolRef
}

// ToolRef 引用已注册工具；引用本身不授予执行权限。
// ToolRef references a registered tool; the reference itself grants no execution permission.
type ToolRef struct {
	ID      string
	Version string
}
