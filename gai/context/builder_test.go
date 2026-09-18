package context_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	gcontext "github.com/sofiworker/gk/gai/context"
	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/model"
)

func request() model.Request {
	return model.Request{Model: core.ModelSelection{ID: "m"}, Messages: []model.Message{{Role: core.RoleUser, Content: []core.Content{{Kind: core.ContentText, Text: "hello"}}}}, Tools: []model.Tool{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}, Parameters: model.Parameters{OutputFormat: &model.OutputFormat{Kind: model.FormatJSON}}}
}
func count(n int64) gcontext.Counter {
	return gcontext.CounterFunc(func(context.Context, model.Request) (core.TokenMeasurement, error) {
		return core.TokenMeasurement{Tokens: n, Kind: core.MeasurementExact, Counter: core.Binding{ID: "test", Version: "1"}}, nil
	})
}

func TestBudgetAndFullRequest(t *testing.T) {
	ctx := context.Background()
	r := request()
	builder, err := gcontext.New(gcontext.CounterFunc(func(_ context.Context, input model.Request) (core.TokenMeasurement, error) {
		if len(input.Tools) != 1 || input.Parameters.OutputFormat.Kind != model.FormatJSON || input.Parameters.MaxOutputTokens == nil || *input.Parameters.MaxOutputTokens != 20 {
			t.Fatal("incomplete measurement envelope", input)
		}
		input.Messages[0].Content[0].Text = "mutated"
		return core.TokenMeasurement{Tokens: 60, Kind: core.MeasurementEstimated, Counter: core.Binding{ID: "test", Version: "1"}}, nil
	}), gcontext.FixedBudget(gcontext.Limits{ModelID: "m", SharedWindow: 100, InputTokens: 90, OutputReserve: 20, SafetyMargin: 10}))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Build(ctx, gcontext.Input{Request: r, Policy: core.ContextPolicy{SoftInputTokens: 60}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.InputLimit != 70 || !plan.NeedsCompaction || plan.OutputReserve != 20 || plan.Request.Messages[0].Content[0].Text != "hello" || r.Parameters.MaxOutputTokens != nil {
		t.Fatal(plan)
	}
	inspected, err := gcontext.Inspect(ctx, plan.Request)
	if err != nil || inspected.Digest != plan.Digest {
		t.Fatal("digest mismatch", err)
	}
	plan.Request.Messages[0].Content[0].Text = "changed"
	if r.Messages[0].Content[0].Text != "hello" {
		t.Fatal("input aliased")
	}
}

func TestBudgetFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		limits gcontext.Limits
		tokens int64
		policy core.ContextPolicy
		want   error
	}{
		{"overflow", gcontext.Limits{ModelID: "m", InputTokens: 10, OutputReserve: 2}, 11, core.ContextPolicy{}, gcontext.ErrOverflow},
		{"unknown model", gcontext.Limits{ModelID: "other", InputTokens: 10, OutputReserve: 2}, 1, core.ContextPolicy{}, gcontext.ErrBudgetUnknown},
		{"unknown reserve", gcontext.Limits{ModelID: "m", InputTokens: 10}, 1, core.ContextPolicy{}, gcontext.ErrBudgetUnknown},
		{"no space", gcontext.Limits{ModelID: "m", SharedWindow: 10, OutputReserve: 10}, 1, core.ContextPolicy{}, gcontext.ErrOverflow},
		{"policy cap", gcontext.Limits{ModelID: "m", InputTokens: 100, OutputReserve: 2}, 21, core.ContextPolicy{MaxInputTokens: 20}, gcontext.ErrOverflow},
		{"soft above hard", gcontext.Limits{ModelID: "m", InputTokens: 10, OutputReserve: 2}, 1, core.ContextPolicy{SoftInputTokens: 20}, gcontext.ErrConfig},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, err := gcontext.New(count(tt.tokens), gcontext.FixedBudget(tt.limits))
			if err != nil {
				t.Fatal(err)
			}
			_, err = b.Build(context.Background(), gcontext.Input{Request: request(), Policy: tt.policy})
			if !errors.Is(err, tt.want) {
				t.Fatal(err)
			}
		})
	}
	for _, kind := range []core.MeasurementKind{core.MeasurementUnknown, "bad"} {
		b, err := gcontext.New(gcontext.CounterFunc(func(context.Context, model.Request) (core.TokenMeasurement, error) {
			return core.TokenMeasurement{Kind: kind}, nil
		}), gcontext.FixedBudget(gcontext.Limits{ModelID: "m", InputTokens: 10, OutputReserve: 2}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = b.Build(context.Background(), gcontext.Input{Request: request()}); err == nil {
			t.Fatal("invalid measurement accepted")
		}
	}
	if _, err := gcontext.New(nil, nil); !errors.Is(err, gcontext.ErrConfig) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gcontext.Inspect(ctx, request()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestToolGroups(t *testing.T) {
	r := request()
	r.Messages = append(r.Messages,
		model.Message{Role: core.RoleAssistant, Content: []core.Content{{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "a", Name: "read", Arguments: json.RawMessage(`{}`)}}, {Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "b", Name: "read", Arguments: json.RawMessage(`{}`)}}}},
		model.Message{Role: core.RoleTool, Content: []core.Content{{Kind: core.ContentToolResult, ToolResult: &core.ToolResult{CallID: "a"}}}},
		model.Message{Role: core.RoleTool, Content: []core.Content{{Kind: core.ContentToolResult, ToolResult: &core.ToolResult{CallID: "b"}}}},
	)
	plan, err := gcontext.Inspect(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups) != 2 || plan.Groups[1].Start != 1 || plan.Groups[1].End != 4 || !plan.Groups[0].Required || !plan.Groups[1].Required {
		t.Fatal(plan.Groups)
	}
	r.Messages = r.Messages[:3]
	if _, err = gcontext.Inspect(context.Background(), r); !errors.Is(err, model.ErrRequest) {
		t.Fatal("unbalanced group accepted", err)
	}
}
