package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sofiworker/gk/gai/adapters/modelhttp"
	"github.com/sofiworker/gk/gai/agent"
	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/model"
	"github.com/sofiworker/gk/gai/sandbox/memory"
	"github.com/sofiworker/gk/gai/tool"
	"github.com/sofiworker/gk/gai/tool/builtin"
	"github.com/sofiworker/gk/gai/transport/httptransport"
)

func TestHTTPToolLoop(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test" {
			t.Error("request configuration")
		}
		var req model.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.Model.ID != "test" || len(req.Tools) != 1 || req.Messages[0].Role != core.RoleSystem {
			t.Error(req)
		}
		response := model.Response{ActualModel: core.Binding{ID: "test"}, FinishReason: model.FinishStop, Message: model.Message{Role: core.RoleAssistant}}
		if requests == 1 {
			response.FinishReason = model.FinishTools
			response.Message.Content = []core.Content{{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "write-1", Name: "write_file", Arguments: json.RawMessage(`{"path":"/test","content":"hello"}`)}}}
		} else {
			if req.Messages[len(req.Messages)-1].Role != core.RoleTool {
				t.Error("missing tool result")
			}
			response.Message.Content = []core.Content{{Kind: core.ContentText, Text: "done"}}
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	transport, err := httptransport.New()
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	client, err := modelhttp.New(testProtocol{url: server.URL}, transport)
	if err != nil {
		t.Fatal(err)
	}

	write, err := builtin.NewWriteFile()
	if err != nil {
		t.Fatal(err)
	}
	runner, err := agent.New(core.Agent{ID: "agent", Prompt: core.Prompt{System: "help"}, Tools: []core.Tool{write}}, client, agent.WithToolOptions(tool.WithAuthorizer(func(context.Context, tool.Authorization) error { return nil })))
	if err != nil {
		t.Fatal(err)
	}
	box, err := memory.New()
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), agent.Input{Model: core.ModelSelection{ID: "test"}, Messages: []core.Message{{Role: core.RoleUser, Content: []core.Content{{Kind: core.ContentText, Text: "write"}}}}, Tools: core.ToolContext{Files: box.View(core.Call{})}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ModelCalls) != 2 || len(result.ToolCalls) != 1 || len(result.Messages) != 3 {
		t.Fatal(result)
	}
	data, err := box.ReadFile(context.Background(), core.Call{}, "/test")
	if err != nil || string(data) != "hello" {
		t.Fatal(string(data), err)
	}
}

type fake struct{}

func (fake) Generate(context.Context, model.Request) (model.Response, error) {
	return model.Response{FinishReason: model.FinishTools, Message: model.Message{Role: core.RoleAssistant, Content: []core.Content{{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "call", Name: "write_file", Arguments: json.RawMessage(`{}`)}}}}}, nil
}

type invalidModel struct{}

func (invalidModel) Generate(context.Context, model.Request) (model.Response, error) {
	call := core.Content{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "duplicate", Name: "write_file", Arguments: json.RawMessage(`{}`)}}
	return model.Response{FinishReason: model.FinishTools, Message: model.Message{Role: core.RoleAssistant, Content: []core.Content{call, call}}}, nil
}
func TestInvalidResponseIsRecordedWithoutExecution(t *testing.T) {
	r, err := agent.New(core.Agent{ID: "a"}, invalidModel{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Run(context.Background(), agent.Input{Model: core.ModelSelection{ID: "logical"}, Messages: []core.Message{{Role: core.RoleUser, Content: []core.Content{{Kind: core.ContentText, Text: "hi"}}}}})
	if !errors.Is(err, agent.ErrResponse) || len(result.Messages) != 0 || len(result.ToolRecords) != 0 || len(result.ModelRecords) != 1 || result.ModelRecords[0].Status != core.CallFailed {
		t.Fatal(result, err)
	}
}
func TestLimitsAndCancellation(t *testing.T) {
	write, err := builtin.NewWriteFile()
	if err != nil {
		t.Fatal(err)
	}
	r, err := agent.New(core.Agent{ID: "a", Tools: []core.Tool{write}}, fake{}, agent.WithLimits(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	in := agent.Input{Model: core.ModelSelection{ID: "m"}, Messages: []core.Message{{Role: core.RoleUser, Content: []core.Content{{Kind: core.ContentText, Text: "hi"}}}}}
	_, err = r.Run(context.Background(), in)
	if !errors.Is(err, agent.ErrBudget) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Run(ctx, in)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, err = r.Run(context.Background(), agent.Input{})
	if !errors.Is(err, agent.ErrConfig) {
		t.Fatal(err)
	}
}

type testProtocol struct{ url string }

func (p testProtocol) Encode(_ context.Context, r model.Request) (httptransport.Request, error) {
	data, err := json.Marshal(r)
	return httptransport.Request{Method: "POST", URL: p.url, Header: http.Header{"Authorization": []string{"Bearer test"}, "Content-Type": []string{"application/json"}}, Body: data}, err
}
func (testProtocol) Decode(_ context.Context, r httptransport.Response) (model.Response, error) {
	var out model.Response
	if r.StatusCode != 200 {
		return out, errors.New("test gateway failed")
	}
	err := json.Unmarshal(r.Body, &out)
	return out, err
}
