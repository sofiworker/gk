package core

// ModelSelection 使用通用模型标识，实际部署及协议由网关或 adapter 解析。
// ModelSelection uses a generic model identifier resolved to deployments and protocols by gateways or adapters.
type ModelSelection struct {
	ID string
}

// ModelCall 记录单次模型请求；重试追加新记录并引用前次 ID。
// ModelCall records one model request; retries append records referencing the previous ID.
// 输出消息通过 ID 关联，避免在调用与对话历史中各保存一份正文。
// Output messages are referenced by ID to avoid duplicating bodies in calls and conversation history.
type ModelCall struct {
	ContextSnapshotID string
	ID                string
	RetryOf           string
	RequestedModel    ModelSelection
	ActualModel       Binding
	RequestID         string
	OutputMessageIDs  []string
	Usage             Usage
	FinishReason      FinishReason
	Status            CallStatus
	Error             *Failure
	StartedAt         int64  // Unix 毫秒；Unix milliseconds.
	FirstContentAt    *int64 // Unix 毫秒，nil 表示未设置；Unix milliseconds, nil means unset.
	EndedAt           *int64 // Unix 毫秒，nil 表示未设置；Unix milliseconds, nil means unset.
}

// FinishReason 表达归一化后的生成结束原因，不保存厂商原始枚举。
// FinishReason expresses normalized generation completion without vendor-specific enums.
type FinishReason string

const (
	FinishStop     FinishReason = "stop"
	FinishTools    FinishReason = "tool_calls"
	FinishLength   FinishReason = "length"
	FinishFiltered FinishReason = "filtered"
	FinishUnknown  FinishReason = "unknown"
)

type CallStatus string

const (
	CallPending   CallStatus = "pending"
	CallRunning   CallStatus = "running"
	CallCompleted CallStatus = "completed"
	CallFailed    CallStatus = "failed"
	CallCancelled CallStatus = "cancelled"
	CallUnknown   CallStatus = "unknown"
)

// Usage 保留计量修订；零 Tokens 与未知用量通过 Source 区分，不包含价格。
// Usage retains metering revisions; Source distinguishes zero tokens from unknown usage, without prices.
type Usage struct {
	Revision uint64
	Metrics  map[UsageMetric]TokenCount
}

type UsageMetric string

const (
	UsageInput      UsageMetric = "input"
	UsageOutput     UsageMetric = "output"
	UsageCacheRead  UsageMetric = "cache_read"
	UsageCacheWrite UsageMetric = "cache_write"
	UsageReasoning  UsageMetric = "reasoning"
)

// TokenCount.IncludedIn 声明此项已计入哪个指标，避免重复累计；空值不代表可以推导总量。
// TokenCount.IncludedIn identifies an enclosing metric to prevent double counting; empty does not imply a derivable total.
type TokenCount struct {
	Tokens     int64
	Source     UsageSource
	IncludedIn UsageMetric
}

type UsageSource string

const (
	UsageUnknown   UsageSource = ""
	UsageReported  UsageSource = "reported"
	UsageEstimated UsageSource = "estimated"
)
