package core

import "encoding/json"

// Turn 归属于外层 Session；Messages 按追加顺序保存完整消息。
// Turn belongs to its enclosing Session; Messages retain complete messages in append order.
type Turn struct {
	ID          string
	Kind        TurnKind
	Metadata    TurnMetadata
	Messages    []Message
	ModelCalls  []ModelCall
	ToolCalls   []ToolExecution
	Contexts    []ContextSnapshot
	Compactions []CompactionRecord
	Summary     *Summary // nil 表示尚无摘要；nil means no summary is available.
	Status      TurnStatus
	Error       *Failure
	StartedAt   int64  // Unix 毫秒；Unix milliseconds.
	EndedAt     *int64 // Unix 毫秒，nil 表示未设置；Unix milliseconds, nil means unset.
}

type TurnKind string

const (
	TurnInteraction  TurnKind = "interaction"
	TurnNotification TurnKind = "notification"
)

type TurnStatus string

const (
	TurnRunning         TurnStatus = "running"
	TurnWaitingApproval TurnStatus = "waiting_approval"
	TurnCompleted       TurnStatus = "completed"
	TurnFailed          TurnStatus = "failed"
	TurnCancelled       TurnStatus = "cancelled"
)

// TurnMetadata 保存本轮采用的绑定，不随 Session.Metadata 后续修改而改变。
// TurnMetadata retains bindings adopted for this turn independently of later session metadata changes.
type TurnMetadata struct {
	Agent          Binding
	Model          ModelSelection
	Environment    EnvironmentBinding
	RetryOf        *TurnRef
	RetryMessageID string
	Extra          map[string]json.RawMessage
}

// Summary 是派生内容；来源消息和生成调用必须可追溯，不能替代原始历史。
// Summary is derived content with traceable source messages and generation calls, never a replacement for history.
type Summary struct {
	ID               string
	SourceMessageIDs []string
	ModelCallID      string
	Text             string
	CreatedAt        int64 // Unix 毫秒；Unix milliseconds.
}

// Failure 使用可持久化的错误信息，Message 保持英文。
// Failure carries persistable error information with an English Message.
type Failure struct {
	Code    string
	Message string
}
