package builtin

// 以下名称与参数对应基础文件工具，仍处于开发中。
// These names and arguments describe basic file tools under development.
const (
	ListDirName    = "list_dir"
	ReadFileName   = "read_file"
	FindFilesName  = "find_files"
	SearchTextName = "search_text"
	WriteFileName  = "write_file"
	EditFileName   = "edit_file"
)

// ListDirArgs 仅列举直接子项；Limit 默认为 200，上限为 1000。
// ListDirArgs lists direct children only; Limit defaults to 200 and is capped at 1000.
type ListDirArgs struct {
	Path  string `json:"path"`
	Limit int    `json:"limit,omitempty"`
}

// ReadFileArgs 使用从 1 开始的行号；StartLine 默认为 1，MaxLines 默认为 200。
// ReadFileArgs uses one-based line numbers; StartLine defaults to 1 and MaxLines to 200.
type ReadFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	MaxLines  int    `json:"max_lines,omitempty"`
}

// FindFilesArgs 按路径模式递归查找；Pattern 使用 Go path.Match 语法，不接收 shell 表达式。
// FindFilesArgs recursively matches paths; Pattern uses Go path.Match syntax without shell expressions.
type FindFilesArgs struct {
	Path       string `json:"path"`
	Pattern    string `json:"pattern"`
	MaxResults int    `json:"max_results,omitempty"`
}

// SearchTextArgs 默认字面量搜索，Regex 显式启用 Go 正则语法，不承诺 ripgrep 参数兼容。
// SearchTextArgs defaults to literal search; Regex selects Go regexp syntax without ripgrep flag compatibility.
type SearchTextArgs struct {
	Path         string `json:"path"`
	Pattern      string `json:"pattern"`
	Glob         string `json:"glob,omitempty"`
	Regex        bool   `json:"regex,omitempty"`
	IgnoreCase   bool   `json:"ignore_case,omitempty"`
	ContextLines int    `json:"context_lines,omitempty"`
	MaxResults   int    `json:"max_results,omitempty"`
}

// WriteFileArgs 写入文本；存在时覆盖，父目录须已存在。
// WriteFileArgs writes text; existing files are overwritten and parents must exist.
type WriteFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// EditFileArgs 表达精确文本替换；默认要求唯一匹配，ReplaceAll 才允许替换多处。
// EditFileArgs describes exact text replacement; a unique match is required unless ReplaceAll is enabled.
// ExpectedContentID 可选；编辑始终以读取内容摘要执行原子条件写入。
// ExpectedContentID is optional; edits always perform atomic writes conditioned on the read content digest.
type EditFileArgs struct {
	Path              string `json:"path"`
	OldText           string `json:"old_text"`
	NewText           string `json:"new_text"`
	ReplaceAll        bool   `json:"replace_all,omitempty"`
	ExpectedContentID string `json:"expected_content_id,omitempty"`
}
