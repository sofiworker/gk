package core

// Prompt 保存静态系统指令与有序来源；动态内容在执行时解析，不回填此配置。
// Prompt holds static system instructions and ordered sources; dynamic content is resolved at execution without mutating configuration.
type Prompt struct {
	Version string
	System  string
	Sources []PromptSource
}

// PromptSource 表示可解析的提示词来源；指定系统槽不代表来源内容已获信任。
// PromptSource identifies a resolvable prompt source; selecting the system slot does not establish trust.
type PromptSource struct {
	ID      string
	Version string
	Kind    PromptSourceKind
	Slot    PromptSlot
}

type PromptSourceKind string

const (
	PromptSkill    PromptSourceKind = "skill"
	PromptResource PromptSourceKind = "resource"
)

type PromptSlot string

const (
	PromptSystemSlot  PromptSlot = "system"
	PromptContextSlot PromptSlot = "context"
)
