package builtin

import (
	"context"
	"encoding/json"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/tool"
)

const (
	maxResults = 1000
	maxFiles   = 10000
	maxBytes   = 8 << 20
	maxOutput  = 256 << 10
)

func output(value any) (core.ToolOutput, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if len(data) > maxOutput {
		return core.ToolOutput{}, core.ErrQuota
	}
	return core.ToolOutput{Content: []core.Content{{Kind: core.ContentText, Text: string(data)}}}, nil
}
func mutationOutput(result core.Result, err error) (core.ToolOutput, error) {
	out, e := output(result)
	if e != nil {
		return out, e
	}
	return out, err
}
func limit(n int) int {
	if n == 0 {
		return 200
	}
	return n
}
func valid(path string, n int) error {
	if path == "" || n < 0 || n > maxResults {
		return tool.ErrArguments
	}
	return nil
}
func build[T any](name, description string, required []string, validate func(T) error, run func(context.Context, core.ToolContext, T) (core.ToolOutput, error)) (core.Tool, error) {
	properties := map[string]any{}
	typ := reflect.TypeOf(*new(T))
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		kind := "string"
		switch field.Type.Kind() {
		case reflect.Bool:
			kind = "boolean"
		case reflect.Int:
			kind = "integer"
		}
		properties[strings.Split(field.Tag.Get("json"), ",")[0]] = map[string]string{"type": kind}
	}
	schema, err := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false})
	if err != nil {
		return nil, err
	}
	return tool.NewFunction(core.ToolDefinition{Name: name, Version: "1", Description: description, InputSchema: schema}, func(_ context.Context, a T) error { return validate(a) }, func(ctx context.Context, tc core.ToolContext, a T) (core.ToolOutput, error) {
		if err := validate(a); err != nil {
			return core.ToolOutput{}, err
		}
		if tc.Files == nil {
			return core.ToolOutput{}, core.ErrDenied
		}
		return run(ctx, tc, a)
	})
}
func NewListDir() (core.Tool, error) {
	return build(ListDirName, "List direct children of a sandbox directory", []string{"path"}, func(a ListDirArgs) error { return valid(a.Path, a.Limit) }, func(ctx context.Context, tc core.ToolContext, a ListDirArgs) (core.ToolOutput, error) {
		entries, err := tc.Files.ReadDir(ctx, tc.Call, a.Path)
		if err != nil {
			return core.ToolOutput{}, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		truncated := len(entries) > limit(a.Limit)
		if truncated {
			entries = entries[:limit(a.Limit)]
		}
		return output(map[string]any{"entries": entries, "truncated": truncated})
	})
}
func NewReadFile() (core.Tool, error) {
	return build(ReadFileName, "Read sandbox text with one-based line numbers", []string{"path"}, func(a ReadFileArgs) error {
		if a.StartLine < 0 {
			return tool.ErrArguments
		}
		return valid(a.Path, a.MaxLines)
	}, func(ctx context.Context, tc core.ToolContext, a ReadFileArgs) (core.ToolOutput, error) {
		data, err := read(ctx, tc, a.Path)
		if err != nil {
			return core.ToolOutput{}, err
		}
		lines := strings.Split(string(data), "\n")
		start := a.StartLine
		if start == 0 {
			start = 1
		}
		if start > len(lines) {
			return output(map[string]any{"lines": []string{}, "truncated": false})
		}
		end := min(len(lines), start-1+limit(a.MaxLines))
		return output(map[string]any{"start_line": start, "lines": lines[start-1 : end], "truncated": end < len(lines)})
	})
}
func NewWriteFile() (core.Tool, error) {
	return build(WriteFileName, "Create or overwrite a sandbox text file", []string{"path", "content"}, func(a WriteFileArgs) error {
		if len(a.Content) > maxBytes {
			return core.ErrQuota
		}
		return valid(a.Path, 0)
	}, func(ctx context.Context, tc core.ToolContext, a WriteFileArgs) (core.ToolOutput, error) {
		r, err := tc.Files.WriteFile(ctx, tc.Call, a.Path, []byte(a.Content))
		return mutationOutput(r, err)
	})
}
func NewEditFile() (core.Tool, error) {
	return build(EditFileName, "Replace exact text with atomic content checks", []string{"path", "old_text", "new_text"}, func(a EditFileArgs) error {
		if a.OldText == "" {
			return tool.ErrArguments
		}
		if len(a.OldText)+len(a.NewText) > maxBytes {
			return core.ErrQuota
		}
		return valid(a.Path, 0)
	}, edit)
}
func read(ctx context.Context, tc core.ToolContext, p string) ([]byte, error) {
	n, err := tc.Files.Stat(ctx, tc.Call, p)
	if err != nil {
		return nil, err
	}
	if n.Size > maxBytes {
		return nil, core.ErrQuota
	}
	data, err := tc.Files.ReadFile(ctx, tc.Call, p)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, core.ErrQuota
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return nil, core.ErrUnsupported
	}
	return data, nil
}
func walk(ctx context.Context, tc core.ToolContext, p string) ([]string, error) {
	stack := []string{p}
	files := []string{}
	seen := map[string]bool{}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p = stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[p] {
			continue
		}
		seen[p] = true
		if len(seen) > maxFiles {
			return nil, core.ErrQuota
		}
		n, err := tc.Files.Stat(ctx, tc.Call, p)
		if err != nil {
			return nil, err
		}
		if !n.Dir {
			files = append(files, n.Path)
			continue
		}
		entries, err := tc.Files.ReadDir(ctx, tc.Call, p)
		if err != nil {
			return nil, err
		}
		if len(stack)+len(entries) > maxFiles {
			return nil, core.ErrQuota
		}
		for _, e := range entries {
			stack = append(stack, e.Path)
		}
	}
	sort.Strings(files)
	return files, nil
}
func matches(pattern, p string) bool {
	if !strings.Contains(pattern, "/") {
		p = path.Base(p)
	}
	ok, _ := path.Match(pattern, p)
	return ok
}
func NewFindFiles() (core.Tool, error) {
	return build(FindFilesName, "Recursively find files using Go path.Match patterns; no double-star syntax", []string{"path", "pattern"}, func(a FindFilesArgs) error {
		if a.Pattern == "" {
			return tool.ErrArguments
		}
		if _, err := path.Match(a.Pattern, ""); err != nil {
			return tool.ErrArguments
		}
		return valid(a.Path, a.MaxResults)
	}, func(ctx context.Context, tc core.ToolContext, a FindFilesArgs) (core.ToolOutput, error) {
		files, err := walk(ctx, tc, a.Path)
		if err != nil {
			return core.ToolOutput{}, err
		}
		out := []string{}
		truncated := false
		for _, p := range files {
			if matches(a.Pattern, p) {
				if len(out) == limit(a.MaxResults) {
					truncated = true
					break
				}
				out = append(out, p)
			}
		}
		return output(map[string]any{"paths": out, "truncated": truncated})
	})
}
func pattern(a SearchTextArgs) (*regexp.Regexp, error) {
	p := a.Pattern
	if !a.Regex {
		p = regexp.QuoteMeta(p)
	}
	if a.IgnoreCase {
		p = "(?i)" + p
	}
	return regexp.Compile(p)
}
func NewSearchText() (core.Tool, error) {
	return build(SearchTextName, "Search sandbox text using literal text or Go regular expressions", []string{"path", "pattern"}, func(a SearchTextArgs) error {
		if a.Pattern == "" || a.ContextLines < 0 || a.ContextLines > 20 {
			return tool.ErrArguments
		}
		if _, err := pattern(a); err != nil {
			return tool.ErrArguments
		}
		if _, err := path.Match(a.Glob, ""); err != nil {
			return tool.ErrArguments
		}
		return valid(a.Path, a.MaxResults)
	}, func(ctx context.Context, tc core.ToolContext, a SearchTextArgs) (core.ToolOutput, error) {
		files, err := walk(ctx, tc, a.Path)
		if err != nil {
			return core.ToolOutput{}, err
		}
		re, err := pattern(a)
		if err != nil {
			return core.ToolOutput{}, err
		}
		results := []any{}
		total := 0
		truncated := false
	outer:
		for _, p := range files {
			if a.Glob != "" && !matches(a.Glob, p) {
				continue
			}
			data, err := read(ctx, tc, p)
			if err == core.ErrUnsupported {
				continue
			}
			if err != nil {
				return core.ToolOutput{}, err
			}
			total += len(data)
			if total > maxBytes {
				return core.ToolOutput{}, core.ErrQuota
			}
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if err := ctx.Err(); err != nil {
					return core.ToolOutput{}, err
				}
				if re.MatchString(line) {
					if len(results) == limit(a.MaxResults) {
						truncated = true
						break outer
					}
					results = append(results, map[string]any{"path": p, "line": i + 1, "text": line, "context_start": max(0, i-a.ContextLines) + 1, "context": lines[max(0, i-a.ContextLines):min(len(lines), i+a.ContextLines+1)]})
				}
			}
		}
		return output(map[string]any{"matches": results, "truncated": truncated})
	})
}
