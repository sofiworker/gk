package model

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sofiworker/gk/gai/core"
)

func TestRequestValidation(t *testing.T) {
	good := func() Request {
		return Request{Model: core.ModelSelection{ID: "m"}, Messages: []Message{{Role: core.RoleUser, Content: []core.Content{{Kind: core.ContentText, Text: "hi"}}}}}
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Messages[0].Role = "invalid" },
		func(r *Request) { r.Messages[0].Content[0].Artifact = &core.ArtifactRef{ID: "ambiguous"} },
		func(r *Request) { r.Parameters.ToolChoice = &ToolChoice{Mode: ToolNamed, Name: "missing"} },
		func(r *Request) {
			r.Messages = []Message{{Role: core.RoleTool, Content: []core.Content{{Kind: core.ContentToolResult, ToolResult: &core.ToolResult{CallID: "orphan"}}}}}
		},
	} {
		r := good()
		mutate(&r)
		if !errors.Is(r.Validate(), ErrRequest) {
			t.Fatal("invalid request accepted", r)
		}
	}
}
func TestOutputValidation(t *testing.T) {
	request := Request{Parameters: Parameters{OutputFormat: &OutputFormat{Kind: FormatJSON}}}
	response := Response{FinishReason: FinishStop, Message: Message{Role: core.RoleAssistant, Content: []core.Content{{Kind: core.ContentText, Text: `{"ok":true}`}}}}
	ctx := context.Background()
	if err := response.Validate(ctx, request, nil); err != nil {
		t.Fatal(err)
	}
	request.Parameters.OutputFormat.Kind = FormatSchema
	if err := response.Validate(ctx, request, nil); !errors.Is(err, ErrValidator) {
		t.Fatal(err)
	}
	called := false
	if err := response.Validate(ctx, request, func(context.Context, json.RawMessage, json.RawMessage) error {
		called = true
		return errors.New("business mismatch")
	}); !errors.Is(err, ErrOutput) || !called {
		t.Fatal(err)
	}
	request.Parameters.OutputFormat.Kind = FormatJSON
	response.Message.Content[0].Text = "not json"
	if err := response.Validate(ctx, request, nil); !errors.Is(err, ErrOutput) {
		t.Fatal(err)
	}
	response.Message.Content = []core.Content{{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "call", Name: "unregistered", Arguments: json.RawMessage(`{}`)}}}
	response.FinishReason = FinishTools
	if err := response.Validate(ctx, request, nil); !errors.Is(err, ErrResponse) {
		t.Fatal(err)
	}
}
