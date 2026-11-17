package core

// ContextPolicy 限制模型输入视图，不持有消息、计数器或模型客户端。
// ContextPolicy constrains model input views without owning messages, counters or clients.
type ContextPolicy struct {
	MaxInputTokens  int64
	SoftInputTokens int64
	MaxCompactions  int
}

type MeasurementKind string

const (
	MeasurementUnknown   MeasurementKind = "unknown"
	MeasurementEstimated MeasurementKind = "estimated"
	MeasurementExact     MeasurementKind = "exact"
)

// TokenMeasurement 的 Tokens 是完整请求输入量；未知不能解释为零。
// TokenMeasurement counts the complete request input; unknown must not be interpreted as zero.
type TokenMeasurement struct {
	Tokens  int64
	Kind    MeasurementKind
	Counter Binding
}

// ContextSnapshot 记录发送前的冻结输入摘要；重放仍需要保留来源正文。
// ContextSnapshot records a frozen input digest before dispatch; replay still needs retained source content.
type ContextSnapshot struct {
	InputSourceIDs   []string
	ID               string
	Agent            Binding
	Model            ModelSelection
	SourceMessageIDs []string
	RequestDigest    string
	Measurement      TokenMeasurement
	InputLimit       int64
	OutputReserve    int64
	SafetyMargin     int64
	NeedsCompaction  bool
	CreatedAt        int64
}

// CompactionRecord 的来源使用 message:/summary: 前缀，精确引用可见节点。
// CompactionRecord sources use message:/summary: prefixes to identify visible nodes precisely.
// 所属 Turn 记录执行归属；覆盖来源可以跨轮次，原始消息不变。
// The enclosing Turn owns execution; covered sources may span turns without changing original messages.
type CompactionRecord struct {
	ID               string
	SourceIDs        []string
	Summary          string
	Status           CallStatus
	Applied          bool
	Before, After    TokenMeasurement
	ModelCall        ModelCall
	RequestDigest    string
	InputMeasurement TokenMeasurement
	Error            *Failure
}
