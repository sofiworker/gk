package ghttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

// ErrNilJSONPath 表示在 nil 的 JSONPath 上执行 Extract/Unmarshal。
// ErrNilJSONPath indicates Extract/Unmarshal was called on a nil JSONPath.
var ErrNilJSONPath = errors.New("ghttp: nil JSON path")

type jsonPathToken struct {
	key   string
	index int
	isKey bool
}

// JSONPath 是编译好的 JSONPath；当前支持根节点、对象字段和数组下标。
// JSONPath is a compiled JSONPath supporting the root, object fields, and array indexes.
type JSONPath struct {
	tokens []jsonPathToken
}

type jsonPathCacheEntry struct {
	path *JSONPath
	err  error
}

// jsonPathCache 缓存表达式到编译结果的映射。
// jsonPathCache stores compiled expressions by their source string.
var jsonPathCache sync.Map

// CompileJSONPath 编译 JSONPath 表达式并缓存结果。
// CompileJSONPath compiles a JSONPath expression and caches the result.
func CompileJSONPath(expr string) (*JSONPath, error) {
	if cached, ok := jsonPathCache.Load(expr); ok {
		entry := cached.(*jsonPathCacheEntry)
		return entry.path, entry.err
	}
	path, err := parseJSONPath(expr)
	entry := &jsonPathCacheEntry{path: path, err: err}
	actual, _ := jsonPathCache.LoadOrStore(expr, entry)
	stored := actual.(*jsonPathCacheEntry)
	return stored.path, stored.err
}

func parseJSONPath(expr string) (*JSONPath, error) {
	if len(expr) == 0 || expr[0] != '$' {
		return nil, fmt.Errorf("invalid JSON path %q", expr)
	}
	path := &JSONPath{}
	for pos := 1; pos < len(expr); {
		switch expr[pos] {
		case '.':
			start := pos + 1
			pos = start
			for pos < len(expr) && isJSONPathNameByte(expr[pos]) {
				pos++
			}
			if start == pos {
				return nil, fmt.Errorf("invalid JSON path %q", expr)
			}
			path.tokens = append(path.tokens, jsonPathToken{key: expr[start:pos], isKey: true})
		case '[':
			start := pos + 1
			pos = start
			for pos < len(expr) && expr[pos] >= '0' && expr[pos] <= '9' {
				pos++
			}
			if start == pos || pos >= len(expr) || expr[pos] != ']' {
				return nil, fmt.Errorf("invalid JSON path %q", expr)
			}
			index, err := strconv.Atoi(expr[start:pos])
			if err != nil {
				return nil, fmt.Errorf("invalid JSON path %q: %w", expr, err)
			}
			path.tokens = append(path.tokens, jsonPathToken{index: index})
			pos++
		default:
			return nil, fmt.Errorf("invalid JSON path %q", expr)
		}
	}
	return path, nil
}

func isJSONPathNameByte(value byte) bool {
	return value == '_' || value == '-' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

// Extract 返回表达式命中的原始 JSON 片段。
// Extract returns the raw JSON fragment matched by the expression.
func (p *JSONPath) Extract(data []byte) ([][]byte, error) {
	if p == nil {
		return nil, ErrNilJSONPath
	}
	current := json.RawMessage(append([]byte(nil), data...))
	if !json.Valid(current) {
		return nil, fmt.Errorf("invalid JSON: %w", errors.New("syntax error"))
	}
	for _, token := range p.tokens {
		var next json.RawMessage
		if token.isKey {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(current, &object); err != nil {
				return nil, err
			}
			next = object[token.key]
		} else {
			var array []json.RawMessage
			if err := json.Unmarshal(current, &array); err != nil {
				return nil, err
			}
			if token.index >= len(array) {
				return nil, fmt.Errorf("JSON path index %d is out of range", token.index)
			}
			next = array[token.index]
		}
		if next == nil {
			return nil, fmt.Errorf("JSON path does not match")
		}
		current = next
	}
	return [][]byte{append([]byte(nil), current...)}, nil
}

// Unmarshal 提取表达式命中的值并解码到 v。
// Unmarshal extracts the matched value and decodes it into v.
func (p *JSONPath) Unmarshal(data []byte, v interface{}) error {
	if p == nil {
		return ErrNilJSONPath
	}
	values, err := p.Extract(data)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("JSON path does not match")
	}
	return json.Unmarshal(values[0], v)
}
