package core

import (
	"context"
	"encoding/json"
)

// Tool 分离参数校验与执行；Validate 不得产生外部副作用。
// Tool separates validation from execution; Validate must not produce external side effects.
// Execute 不隐式重试，返回 error 不表示外部副作用尚未发生。
// Execute must not retry implicitly; an error does not prove external effects did not occur.
// 运行时负责校验、授权和审批后再执行；注册工具本身不授予权限。
// The runtime validates and authorizes execution with required approvals; registration grants no permission.
type Tool interface {
	Definition() ToolDefinition
	Validate(context.Context, json.RawMessage) error
	Execute(context.Context, ToolContext, ToolCall) (ToolOutput, error)
}

// ToolContext 由可信运行时按调用构造；能力可以为空，不包含环境管理权限。
// ToolContext is constructed per call by the trusted runtime; capabilities may be nil and exclude environment administration.
type ToolContext struct {
	Call      Call
	Files     Files
	Network   Network
	Resources Requester
}

// ToolDefinition 是交给模型的描述，Name 在同一 Agent 内唯一。
// ToolDefinition is model-visible metadata; Name is unique within an Agent.
// InputSchema 使用 JSON Schema；支持范围由实现明确声明。
// InputSchema uses JSON Schema with implementation-declared support.
type ToolDefinition struct {
	Name        string
	Version     string
	Description string
	InputSchema json.RawMessage
}

// ToolOutput 不分配历史消息或执行 ID，由运行时关联到工具请求及执行记录。
// ToolOutput assigns no history message or execution IDs; the runtime associates it with the request and execution record.
type ToolOutput struct {
	Content []Content
	Error   *Failure
}

// ToolExecution 是执行事实；CallID 引用消息中的工具请求，RetryOf 引用前次执行。
// ToolExecution records execution facts; CallID references a message tool request and RetryOf a preceding execution.
type ToolExecution struct {
	ID              string
	CallID          string
	RetryOf         string
	Tool            Binding
	Approvals       []Approval
	ResultMessageID string
	Status          CallStatus
	Error           *Failure
	StartedAt       *int64 // Unix 毫秒，nil 表示未设置；Unix milliseconds, nil means unset.
	EndedAt         *int64 // Unix 毫秒，nil 表示未设置；Unix milliseconds, nil means unset.
}

// Approval 绑定工具、参数及资源摘要；决定不扩大环境授权。
// Approval binds tool, argument, and resource digests without expanding environment authorization.
type Approval struct {
	ID              string
	Tool            Binding
	ArgumentsDigest string
	ResourcesDigest string
	Environment     EnvironmentBinding
	RequestedAt     int64  // Unix 毫秒；Unix milliseconds.
	ExpiresAt       *int64 // Unix 毫秒，nil 表示未设置；Unix milliseconds, nil means unset.
	Decision        ApprovalDecision
	DecidedBy       string
	DecidedAt       *int64 // Unix 毫秒，nil 表示未设置；Unix milliseconds, nil means unset.
	Reason          string
}

type ApprovalDecision string

const (
	ApprovalPending  ApprovalDecision = "pending"
	ApprovalApproved ApprovalDecision = "approved"
	ApprovalDenied   ApprovalDecision = "denied"
	ApprovalExpired  ApprovalDecision = "expired"
)
