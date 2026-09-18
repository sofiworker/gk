package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/sandbox/memory"
	"github.com/sofiworker/gk/gai/tool"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func definition(name, version string) core.ToolDefinition {
	return core.ToolDefinition{Name: name, Version: version, InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)}
}

type args struct {
	Path string `json:"path"`
}

func function(t *testing.T, name, version string, calls *atomic.Int32) *tool.Function[args] {
	t.Helper()
	f, err := tool.NewFunction(definition(name, version), func(_ context.Context, a args) error {
		if a.Path == "" {
			return errors.New("path is required")
		}
		return nil
	}, func(_ context.Context, _ core.ToolContext, a args) (core.ToolOutput, error) {
		calls.Add(1)
		return core.ToolOutput{Content: []core.Content{{Kind: core.ContentText, Text: a.Path}}}, nil
	})
	must(t, err)
	return f
}
func allow(context.Context, tool.Authorization) error { return nil }
func TestSetFrozenDefinitions(t *testing.T) {
	var calls atomic.Int32
	one := function(t, "path", "1", &calls)
	two := function(t, "path", "2", &calls)
	agent := core.Agent{Tools: []core.Tool{one}}
	set, err := tool.New(agent.Tools, tool.WithAuthorizer(allow))
	must(t, err)
	agent.Tools[0] = two
	defs := set.Definitions()
	defs[0].InputSchema[0] = 'X'
	if set.Definitions()[0].InputSchema[0] != '{' || set.Definitions()[0].Version != "1" {
		t.Fatal("metadata changed")
	}
	result, err := set.Execute(ctx, core.ToolContext{}, core.ToolCall{ID: "call", Name: "path", Arguments: json.RawMessage(`{"path":"value"}`)})
	must(t, err)
	if !result.Started || result.Tool != (core.Binding{ID: "path", Version: "1"}) || calls.Load() != 1 {
		t.Fatal(result)
	}
	for _, tools := range [][]core.Tool{{one, two}, {one, one}} {
		if _, err := tool.New(tools); !errors.Is(err, tool.ErrDuplicate) {
			t.Fatal(err)
		}
	}
	empty, err := tool.New(nil)
	must(t, err)
	if len(empty.Definitions()) != 0 {
		t.Fatal("empty set")
	}
	_, err = empty.Execute(ctx, core.ToolContext{}, core.ToolCall{ID: "c", Name: "path"})
	if !errors.Is(err, tool.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestValidationAndAuthorizationBeforeExecution(t *testing.T) {
	var calls atomic.Int32
	ref := function(t, "path", "1", &calls)
	denied, err := tool.New([]core.Tool{ref})
	must(t, err)
	call := core.ToolCall{ID: "c", Name: "path", Arguments: json.RawMessage(`{"path":"original"}`)}
	execution, err := denied.Execute(ctx, core.ToolContext{}, call)
	if !errors.Is(err, tool.ErrDenied) || execution.Started {
		t.Fatal(execution, err)
	}
	authorized := 0
	set, err := tool.New([]core.Tool{ref}, tool.WithAuthorizer(func(_ context.Context, a tool.Authorization) error {
		authorized++
		a.Call.Arguments[9] = 'X'
		a.Definition.InputSchema[0] = 'X'
		return nil
	}))
	must(t, err)
	for _, raw := range []string{`null`, `[]`, `{}`, `{"path":2}`, `{"path":"a","extra":1}`, `{"path":"a","path":"b"}`, `{"path":"a"} {}`, `{"path":`} {
		execution, err = set.Execute(ctx, core.ToolContext{}, core.ToolCall{ID: "bad", Name: "path", Arguments: json.RawMessage(raw)})
		if !errors.Is(err, tool.ErrArguments) || execution.Started {
			t.Fatal(raw, execution, err)
		}
	}
	if authorized != 0 || calls.Load() != 0 {
		t.Fatal("invalid arguments executed policy or tool")
	}
	execution, err = set.Execute(ctx, core.ToolContext{}, call)
	must(t, err)
	if execution.Output.Content[0].Text != "original" || string(call.Arguments) != `{"path":"original"}` {
		t.Fatal(execution)
	}
	_, err = set.Execute(ctx, core.ToolContext{Call: core.Call{ToolCallID: "other"}}, call)
	if !errors.Is(err, tool.ErrArguments) {
		t.Fatal(err)
	}
	denyReason := errors.New("policy says no")
	blocked, err := tool.New([]core.Tool{ref}, tool.WithAuthorizer(func(context.Context, tool.Authorization) error { return denyReason }))
	must(t, err)
	_, err = blocked.Execute(ctx, core.ToolContext{}, call)
	if !errors.Is(err, tool.ErrDenied) || !errors.Is(err, denyReason) {
		t.Fatal(err)
	}
}
func TestSandboxPerCallInjection(t *testing.T) {
	f, err := tool.NewFunction(definition("write", "1"), func(_ context.Context, a args) error {
		if a.Path == "" {
			return tool.ErrArguments
		}
		return nil
	}, func(c context.Context, tc core.ToolContext, a args) (core.ToolOutput, error) {
		if tc.Files == nil {
			return core.ToolOutput{}, core.ErrDenied
		}
		_, err := tc.Files.WriteFile(c, tc.Call, a.Path, []byte(tc.Call.SessionID))
		return core.ToolOutput{}, err
	})
	must(t, err)
	ref := f
	set, err := tool.New([]core.Tool{ref}, tool.WithAuthorizer(allow))
	must(t, err)
	var wg sync.WaitGroup
	for _, id := range []string{"one", "two"} {
		b, err := memory.New()
		must(t, err)
		wg.Add(1)
		go func() {
			defer wg.Done()
			call := core.Call{SessionID: id, ToolCallID: "call", ID: "operation"}
			tc := core.ToolContext{Call: call, Files: b.View(call)}
			execution, err := set.Execute(ctx, tc, core.ToolCall{ID: "call", Name: "write", Arguments: json.RawMessage(`{"path":"/file"}`)})
			if err != nil || !execution.Started {
				t.Error(execution, err)
				return
			}
			data, err := b.ReadFile(ctx, core.Call{}, "/file")
			if err != nil || string(data) != id {
				t.Error(string(data), err)
			}
		}()
	}
	wg.Wait()
}

type custom struct {
	def      core.ToolDefinition
	validate func(context.Context, json.RawMessage) error
	run      func(context.Context) (core.ToolOutput, error)
}

func (c *custom) Definition() core.ToolDefinition { return c.def }
func (c *custom) Validate(ctx context.Context, raw json.RawMessage) error {
	if c.validate != nil {
		return c.validate(ctx, raw)
	}
	return nil
}
func (c *custom) Execute(ctx context.Context, _ core.ToolContext, _ core.ToolCall) (core.ToolOutput, error) {
	return c.run(ctx)
}
func TestCancellationPanicAndNoAutomaticRetry(t *testing.T) {
	for _, kind := range []string{"error", "panic", "timeout", "business"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			impl := &custom{def: definition("run", "1"), run: func(c context.Context) (core.ToolOutput, error) {
				calls++
				switch kind {
				case "error":
					return core.ToolOutput{}, errors.New("external failure")
				case "panic":
					panic("private data")
				case "timeout":
					<-c.Done()
					return core.ToolOutput{}, c.Err()
				default:
					return core.ToolOutput{Error: &core.Failure{Code: "failed", Message: "failed"}}, nil
				}
			}}
			ref := impl
			set, err := tool.New([]core.Tool{ref}, tool.WithAuthorizer(allow), tool.WithLimits(64, time.Millisecond))
			must(t, err)
			call := core.ToolCall{ID: "c", Name: "run", Arguments: json.RawMessage(`{}`)}
			execution, err := set.Execute(ctx, core.ToolContext{}, call)
			if !execution.Started || calls != 1 || execution.StartedAt == nil || execution.EndedAt == 0 {
				t.Fatal(execution, err)
			}
			if kind == "business" {
				if err != nil || execution.Status != core.CallFailed {
					t.Fatal(execution, err)
				}
			} else if err == nil || execution.Status != core.CallUnknown {
				t.Fatal(execution, err)
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			execution, err = set.Execute(cancelled, core.ToolContext{}, call)
			if !errors.Is(err, context.Canceled) || execution.Started || calls != 1 {
				t.Fatal(execution, err)
			}
		})
	}
}
func TestInvalidDefinitionsLimitsAndNil(t *testing.T) {
	var typedNil *custom
	if _, err := tool.New([]core.Tool{typedNil}); !errors.Is(err, tool.ErrDefinition) {
		t.Fatal(err)
	}
	for _, def := range []core.ToolDefinition{{}, definition("with.dot", "1"), definition("valid", "")} {
		if _, err := tool.New([]core.Tool{&custom{def: def}}); !errors.Is(err, tool.ErrDefinition) {
			t.Fatal(err)
		}
	}
	var calls atomic.Int32
	ref := function(t, "ok", "1", &calls)
	for _, o := range []tool.Option{nil, tool.WithAuthorizer(nil), tool.WithLimits(0, time.Second)} {
		if _, err := tool.New(nil, o); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
	set, err := tool.New([]core.Tool{ref}, tool.WithLimits(2, time.Second), tool.WithAuthorizer(allow))
	must(t, err)
	execution, err := set.Execute(ctx, core.ToolContext{}, core.ToolCall{ID: "c", Name: "ok", Arguments: json.RawMessage(`{"path":"large"}`)})
	if !errors.Is(err, tool.ErrLimit) || execution.Started {
		t.Fatal(execution, err)
	}
}
