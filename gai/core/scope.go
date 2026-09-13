package core

// Scope 描述 Agent 可请求的资源和能力范围；有效权限仍由可信运行时计算。
// Scope describes resources and capabilities an Agent may request; trusted runtime computes effective permission.
// 空列表不授予访问或执行能力；Capability 使用精确标识，不隐式解释通配符。
// Empty lists grant no resource or execution capabilities; capability identifiers are exact, with no implicit wildcards.
type Scope struct {
	ID              string
	Version         string
	Resources       []ScopeResource
	Capabilities    []string
	AllowDelegation bool
}

// ScopeResource 是受限资源引用，不包含凭据或具体权限决策。
// ScopeResource references a bounded resource without credentials or an authorization decision.
// ID 为精确资源标识，Mode 不决定该资源是否可以执行。
// ID is an exact resource identifier; Mode does not determine execution permission.
type ScopeResource struct {
	Kind string
	ID   string
	Mode ScopeMode
}

type ScopeMode string

const (
	ScopeRead      ScopeMode = "read"
	ScopeWrite     ScopeMode = "write"
	ScopeReadWrite ScopeMode = "read_write"
)
