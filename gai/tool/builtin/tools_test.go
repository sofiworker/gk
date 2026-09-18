package builtin_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/sandbox/memory"
	"github.com/sofiworker/gk/gai/tool"
	"github.com/sofiworker/gk/gai/tool/builtin"
)

func TestFileTools(t *testing.T) {
	ctx := context.Background()
	box, err := memory.New()
	if err != nil {
		t.Fatal(err)
	}
	constructors := []func() (core.Tool, error){builtin.NewListDir, builtin.NewReadFile, builtin.NewFindFiles, builtin.NewSearchText, builtin.NewWriteFile, builtin.NewEditFile}
	tools := []core.Tool{}
	for _, newTool := range constructors {
		v, err := newTool()
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, v)
	}
	set, err := tool.New(tools, tool.WithAuthorizer(func(context.Context, tool.Authorization) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	tc := core.ToolContext{Files: box.View(core.Call{})}
	cases := []struct{ name, raw string }{
		{"write_file", `{"path":"/a.txt","content":"hello\nworld"}`},
		{"list_dir", `{"path":"/"}`},
		{"read_file", `{"path":"/a.txt","start_line":2,"max_lines":1}`},
		{"find_files", `{"path":"/","pattern":"*.txt"}`},
		{"search_text", `{"path":"/","pattern":"WORLD","ignore_case":true,"context_lines":1}`},
		{"edit_file", `{"path":"/a.txt","old_text":"hello","new_text":"changed"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := set.Execute(ctx, tc, core.ToolCall{ID: c.name, Name: c.name, Arguments: json.RawMessage(c.raw)})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Output.Content) != 1 || !json.Valid([]byte(result.Output.Content[0].Text)) {
				t.Fatal(result)
			}
			var out map[string]any
			if err = json.Unmarshal([]byte(result.Output.Content[0].Text), &out); err != nil {
				t.Fatal(err)
			}
			switch c.name {
			case "read_file":
				if out["lines"].([]any)[0] != "world" {
					t.Fatal(out)
				}
			case "find_files":
				if out["paths"].([]any)[0] != "/a.txt" {
					t.Fatal(out)
				}
			case "search_text":
				if len(out["matches"].([]any)) != 1 {
					t.Fatal(out)
				}
			}
		})
	}
	data, err := box.ReadFile(ctx, core.Call{}, "/a.txt")
	if err != nil || string(data) != "changed\nworld" {
		t.Fatal(string(data), err)
	}
	entry, err := box.Stat(ctx, core.Call{}, "/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = box.WriteFile(ctx, core.Call{}, "/a.txt", []byte("concurrent")); err != nil {
		t.Fatal(err)
	}
	_, err = box.View(core.Call{}).CompareAndWrite(ctx, core.Call{}, "/a.txt", entry.ContentID, []byte("lost"))
	if !errors.Is(err, core.ErrConflict) {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"path":"/a.txt","old_text":"missing","new_text":"x"}`, `{"path":"/a.txt","old_text":"concurrent","new_text":"x","expected_content_id":"stale"}`} {
		_, err = set.Execute(ctx, tc, core.ToolCall{ID: "bad", Name: "edit_file", Arguments: json.RawMessage(raw)})
		if err == nil {
			t.Fatal("invalid edit accepted")
		}
	}
	_, err = set.Execute(ctx, core.ToolContext{}, core.ToolCall{ID: "no-files", Name: "read_file", Arguments: json.RawMessage(`{"path":"/a.txt"}`)})
	if !errors.Is(err, core.ErrDenied) {
		t.Fatal(err)
	}
}
