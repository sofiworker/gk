// Package core 定义 Session、Turn、Message 及其执行事实；不包含执行或持久化实现。
// Package core defines sessions, turns, messages, and execution facts without runtime or persistence implementations.
package core

import "encoding/json"

// Session 的 Turns 按追加顺序排列；Version 供存储进行原子并发检查。
// Session turns follow append order; Version supports atomic concurrency checks by storage.
// 结构体本身不强制只追加，也不自动提供 CAS。
// The struct itself enforces neither append-only behavior nor CAS.
type Session struct {
	ID        string
	Version   uint64
	Metadata  SessionMetadata
	Turns     []Turn
	CreatedAt int64 // Unix 毫秒；Unix milliseconds.
	UpdatedAt int64 // Unix 毫秒；Unix milliseconds.
}

// SessionMetadata 保存会话信息和后续执行的选择，不代表历史执行事实。
// SessionMetadata holds conversation information and future selections, not historical execution facts.
type SessionMetadata struct {
	Title       string
	Agent       Binding
	Model       ModelSelection
	Environment EnvironmentBinding
	Parent      *TurnRef
	Extra       map[string]json.RawMessage
}

// Binding 只引用配置或资源版本，不携带凭据或授予访问权限。
// Binding references a configuration or resource version without carrying credentials or granting access.
type Binding struct {
	ID      string
	Version string
}

// TurnRef 用于子会话和重新生成，明确引用所属会话内的轮次。
// TurnRef identifies a turn within its session for child conversations and regeneration.
type TurnRef struct {
	SessionID string
	TurnID    string
}
