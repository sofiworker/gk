package core

import "encoding/json"

// Message 的会话和轮次归属由外层结构表达；不重复存储历史集合。
// Message ownership is expressed by its enclosing session and turn without duplicating history collections.
type Message struct {
	ID        string
	Role      Role
	Content   []Content
	CreatedAt int64 // Unix 毫秒；Unix milliseconds.
}

// Role 表达对话语义，不代表授权身份。
// Role expresses conversation semantics, not an authorization identity.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentKind string

const (
	ContentText       ContentKind = "text"
	ContentImage      ContentKind = "image"
	ContentAudio      ContentKind = "audio"
	ContentVideo      ContentKind = "video"
	ContentFile       ContentKind = "file"
	ContentToolCall   ContentKind = "tool_call"
	ContentToolResult ContentKind = "tool_result"
)

// Content 的 Kind 指定唯一有效载荷；多模态内容通过 Artifact 引用。
// Content Kind selects the sole active payload; multimodal content uses Artifact references.
type Content struct {
	Kind       ContentKind
	Text       string
	Artifact   *ArtifactRef
	ToolCall   *ToolCall
	ToolResult *ToolResult
}

// ArtifactRef 标识外部内容，解析和访问控制由资源服务负责。
// ArtifactRef identifies external content; resource services own resolution and access control.
type ArtifactRef struct {
	ID        string
	MediaType string
	Name      string
	Digest    string
}

// ToolCall 是模型提出的调用内容，不表示工具已经执行。
// ToolCall is a model-requested invocation, not proof of tool execution.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// ToolResult 通过 CallID 配对请求；ExecutionID 区分同一请求的执行尝试。
// ToolResult pairs with its request through CallID; ExecutionID distinguishes execution attempts.
type ToolResult struct {
	CallID      string
	ExecutionID string
	Content     []Content
	Error       *Failure
}
