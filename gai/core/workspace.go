package core

// WorkspaceRef 标识 Agent 使用的工作资源边界，不等同于操作系统当前目录。
// WorkspaceRef identifies the Agent's work-resource boundary and is not an operating-system cwd.
type WorkspaceRef struct {
	ID      string
	Version string
}

// SandboxRef 标识执行隔离策略；它不授予资源访问权限。
// SandboxRef identifies an execution-isolation policy and grants no resource access.
type SandboxRef struct {
	ID      string
	Version string
}
