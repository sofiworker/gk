package tool_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/sandbox/memory"
	"github.com/sofiworker/gk/gai/tool"
)

func ExampleNew() {
	ctx := context.Background()
	type input struct {
		Path string `json:"path"`
	}
	reader, err := tool.NewFunction(
		core.ToolDefinition{Name: "read_file", Version: "1", Description: "Read a sandbox file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		func(_ context.Context, in input) error {
			if in.Path == "" {
				return tool.ErrArguments
			}
			return nil
		},
		func(ctx context.Context, tc core.ToolContext, in input) (core.ToolOutput, error) {
			if tc.Files == nil {
				return core.ToolOutput{}, core.ErrDenied
			}
			data, err := tc.Files.ReadFile(ctx, tc.Call, in.Path)
			if err != nil {
				return core.ToolOutput{}, err
			}
			return core.ToolOutput{Content: []core.Content{{Kind: core.ContentText, Text: string(data)}}}, nil
		},
	)
	if err != nil {
		panic(err)
	}
	agent := core.Agent{ID: "reader", Tools: []core.Tool{reader}}
	set, err := tool.New(agent.Tools, tool.WithAuthorizer(func(_ context.Context, a tool.Authorization) error {
		if a.Tool != (core.Binding{ID: "read_file", Version: "1"}) {
			return tool.ErrDenied
		}
		return nil
	}))
	if err != nil {
		panic(err)
	}
	box, err := memory.New()
	if err != nil {
		panic(err)
	}
	if _, err = box.WriteFile(ctx, core.Call{}, "/note", []byte("hello")); err != nil {
		panic(err)
	}
	call := core.ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"/note"}`)}
	attribution := core.Call{SessionID: "session", TurnID: "turn", ToolCallID: call.ID}
	execution, err := set.Execute(ctx, core.ToolContext{Call: attribution, Files: box.View(attribution)}, call)
	if err != nil {
		panic(err)
	}
	fmt.Println(execution.Tool.ID, execution.Tool.Version, execution.Output.Content[0].Text)
	// Output: read_file 1 hello
}
