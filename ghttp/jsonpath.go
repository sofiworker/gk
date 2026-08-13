package ghttp

import (
	"errors"
	"sync"

	jsonx "github.com/goccy/go-json"
)

// ErrNilJSONPath 表示在 nil 的 JSONPath 上执行 Extract/Unmarshal。
// ErrNilJSONPath indicates Extract/Unmarshal was called on a nil JSONPath.
var ErrNilJSONPath = errors.New("ghttp: nil JSON path")

// JSONPath 是编译好的 JSONPath,可按需从原始字节提取子值,无需全量解码。
// JSONPath is a compiled JSONPath used to extract sub-values from raw bytes
// without decoding the whole document.
// 内部基于 goccy/go-json 的 CreatePath;Extract 会复制并扫描整段输入,
// 大文档上只取少量字段时请先看设计文档中的成本数据。
// it wraps goccy/go-json CreatePath; Extract copies and scans the whole input,
// so consult the design doc's cost data for large documents.
type JSONPath struct {
	p *jsonx.Path
}

type jsonPathCacheEntry struct {
	path *JSONPath
	err  error
}

// jsonPathCache 缓存表达式 -> 编译结果。表达式由开发者代码提供而非客户端
// 输入,允许无界增长。
// jsonPathCache caches expression -> compiled path. Expressions come from
// developer code rather than client input, so unbounded growth is acceptable.
var jsonPathCache sync.Map

// CompileJSONPath 编译 JSONPath 表达式并缓存结果。
// CompileJSONPath compiles a JSONPath expression and caches the result.
// 相同表达式的重复调用返回同一个 *JSONPath。
// repeated calls with the same expression return the same *JSONPath.
func CompileJSONPath(expr string) (*JSONPath, error) {
	if cached, ok := jsonPathCache.Load(expr); ok {
		entry := cached.(*jsonPathCacheEntry)
		return entry.path, entry.err
	}

	compiled, err := jsonx.CreatePath(expr)
	entry := &jsonPathCacheEntry{}
	if err != nil {
		entry.err = err
	} else {
		entry.path = &JSONPath{p: compiled}
	}
	actual, _ := jsonPathCache.LoadOrStore(expr, entry)
	stored := actual.(*jsonPathCacheEntry)
	return stored.path, stored.err
}

// Extract 返回表达式命中的原始 JSON 片段。
// Extract returns the raw JSON fragments matched by the expression.
func (p *JSONPath) Extract(data []byte) ([][]byte, error) {
	if p == nil || p.p == nil {
		return nil, ErrNilJSONPath
	}
	return p.p.Extract(data)
}

// Unmarshal 提取表达式命中的值并解码到 v。
// Unmarshal extracts the matched value and decodes it into v.
func (p *JSONPath) Unmarshal(data []byte, v interface{}) error {
	if p == nil || p.p == nil {
		return ErrNilJSONPath
	}
	return p.p.Unmarshal(data, v)
}
