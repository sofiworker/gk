package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sofiworker/gk/gai/adapters/modelhttp"
	"github.com/sofiworker/gk/gai/agent"
	gcontext "github.com/sofiworker/gk/gai/context"
	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/model"
	runtime "github.com/sofiworker/gk/gai/runtime"
	"github.com/sofiworker/gk/gai/session"
	"github.com/sofiworker/gk/gai/session/memory"
	"github.com/sofiworker/gk/gai/tool"
	"github.com/sofiworker/gk/gai/tool/builtin"
	"github.com/sofiworker/gk/gai/transport/httptransport"
)

type modelFunc func(context.Context, model.Request) (model.Response, error)

func (f modelFunc) Generate(ctx context.Context, r model.Request) (model.Response, error) {
	return f(ctx, r)
}
func input() runtime.Input {
	return runtime.Input{Content: []core.Content{{Kind: core.ContentText, Text: "write a file"}}}
}
func answer() model.Response {
	return model.Response{ActualModel: core.Binding{ID: "logical", Version: "1"}, FinishReason: model.FinishStop, Message: model.Message{Role: core.RoleAssistant, Content: []core.Content{{Kind: core.ContentText, Text: "done"}}}}
}
func definition() core.Agent { return core.Agent{ID: "assistant", Version: "1"} }
func metadata() core.SessionMetadata {
	return core.SessionMetadata{Model: core.ModelSelection{ID: "logical"}}
}

type protocol struct{ url string }

func (p protocol) Encode(_ context.Context, r model.Request) (httptransport.Request, error) {
	b, e := json.Marshal(r)
	return httptransport.Request{URL: p.url, Method: "POST", Body: b}, e
}
func (protocol) Decode(_ context.Context, r httptransport.Response) (model.Response, error) {
	var out model.Response
	e := json.Unmarshal(r.Body, &out)
	return out, e
}

func TestSessionHTTPToolsAndHistory(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req model.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		n := requests.Add(1)
		out := answer()
		switch n {
		case 1:
			if len(req.Messages) != 1 {
				t.Errorf("first history: %d", len(req.Messages))
			}
			out.FinishReason = model.FinishTools
			out.Message.Content = []core.Content{{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "write", Name: "write_file", Arguments: json.RawMessage(`{"path":"note","content":"hello"}`)}}}
		case 2:
			if len(req.Messages) != 3 || req.Messages[2].Role != core.RoleTool {
				t.Error("tool history missing")
			}
		case 3:
			if len(req.Messages) != 5 {
				t.Errorf("second turn history: %d", len(req.Messages))
			}
		}
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	transport, err := httptransport.New()
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	client, err := modelhttp.New(protocol{server.URL}, transport)
	if err != nil {
		t.Fatal(err)
	}
	write, err := builtin.NewWriteFile()
	if err != nil {
		t.Fatal(err)
	}
	def := definition()
	def.Tools = []core.Tool{write}
	rt, err := runtime.New(def, client, store, runtime.WithAgentOptions(agent.WithToolOptions(tool.WithAuthorizer(func(context.Context, tool.Authorization) error { return nil }))))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.CreateSession(ctx, metadata())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := rt.Run(ctx, sess.ID, input())
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != core.TurnCompleted || len(turn.Messages) != 4 || len(turn.ModelCalls) != 2 || len(turn.ToolCalls) != 1 {
		t.Fatalf("unexpected turn: %+v", turn)
	}
	if turn.ModelCalls[0].OutputMessageIDs[0] != turn.Messages[1].ID || turn.ToolCalls[0].ResultMessageID != turn.Messages[2].ID || turn.Messages[2].Content[0].ToolResult.ExecutionID != turn.ToolCalls[0].ID {
		t.Fatal("broken record links")
	}
	box, err := rt.Sandbox(sess.Metadata.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := box.ReadFile(ctx, core.Call{}, "/note")
	if err != nil || string(data) != "hello" {
		t.Fatal(string(data), err)
	}
	events, err := box.Events(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "write" {
			found = true
			if e.Attribution.SessionID != sess.ID || e.Attribution.TurnID != turn.ID || e.Attribution.ToolCallID != "write" {
				t.Fatal(e)
			}
		}
	}
	if !found {
		t.Fatal("missing write audit")
	}
	log, err := store.History(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	modelStarted, toolStarted := false, false
	for _, c := range log {
		for _, m := range c.Turn.ModelCalls {
			modelStarted = modelStarted || m.Status == core.CallRunning
		}
		for _, e := range c.Turn.ToolCalls {
			toolStarted = toolStarted || e.Status == core.CallRunning
		}
	}
	if !modelStarted || !toolStarted {
		t.Fatal("missing intent records")
	}
	if _, err = rt.Run(ctx, sess.ID, input()); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(ctx, sess.ID)
	if err != nil || len(loaded.Turns) != 2 {
		t.Fatal(loaded, err)
	}
	other, err := rt.CreateSession(ctx, metadata())
	if err != nil {
		t.Fatal(err)
	}
	otherBox, err := rt.Sandbox(other.Metadata.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = otherBox.ReadFile(ctx, core.Call{}, "/note"); !errors.Is(err, core.ErrNotExist) {
		t.Fatal("sessions share files", err)
	}
}

func TestCancellationAndBusySession(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	entered := make(chan struct{})
	var calls atomic.Int32
	client := modelFunc(func(ctx context.Context, _ model.Request) (model.Response, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return model.Response{}, ctx.Err()
	})
	rt, err := runtime.New(definition(), client, store)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.CreateSession(ctx, metadata())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := rt.Run(ctx, sess.ID, input()); done <- e }()
	<-entered
	loaded, err := store.Load(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	turnID := loaded.Turns[0].ID
	if _, err = rt.Run(ctx, sess.ID, input()); !errors.Is(err, session.ErrBusy) {
		t.Fatal(err)
	}
	second, err := runtime.New(definition(), client, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.Run(ctx, sess.ID, input()); !errors.Is(err, session.ErrBusy) {
		t.Fatal("other runtime bypassed reservation", err)
	}
	if err = rt.Cancel(sess.ID, turnID); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	loaded, err = store.Load(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn := loaded.Turns[0]
	if turn.Status != core.TurnCancelled || turn.ModelCalls[0].Status != core.CallCancelled || turn.EndedAt == nil || calls.Load() != 1 {
		t.Fatal(turn)
	}
	if err = rt.Cancel(sess.ID, turnID); !errors.Is(err, runtime.ErrNotRunning) {
		t.Fatal(err)
	}
}

type failingStore struct {
	*memory.Store
	commits int
	failAt  int
}

func (s *failingStore) Commit(ctx context.Context, key string, c session.Commit) (uint64, error) {
	s.commits++
	if s.commits == s.failAt {
		return 0, errors.New("storage unavailable")
	}
	return s.Store.Commit(ctx, key, c)
}
func TestCheckpointFailureStopsExecution(t *testing.T) {
	for _, failAt := range []int{2, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			store := &failingStore{Store: memory.New(), failAt: failAt}
			calls := 0
			rt, err := runtime.New(definition(), modelFunc(func(context.Context, model.Request) (model.Response, error) { calls++; return answer(), nil }), store)
			if err != nil {
				t.Fatal(err)
			}
			sess, err := rt.CreateSession(context.Background(), metadata())
			if err != nil {
				t.Fatal(err)
			}
			_, err = rt.Run(context.Background(), sess.ID, input())
			if !errors.Is(err, agent.ErrCheckpoint) {
				t.Fatal(err)
			}
			if calls != failAt-2 {
				t.Fatal("unexpected model calls", calls)
			}
			loaded, err := store.Load(context.Background(), sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Turns[0].Status != core.TurnRunning {
				t.Fatal("lost ownership")
			}
			if _, err = rt.Run(context.Background(), sess.ID, input()); !errors.Is(err, session.ErrBusy) {
				t.Fatal("unsafe automatic replay", err)
			}
		})
	}
}

func TestFailedModelRecorded(t *testing.T) {
	store := memory.New()
	sentinel := errors.New("private provider diagnostic")
	rt, err := runtime.New(definition(), modelFunc(func(context.Context, model.Request) (model.Response, error) { return model.Response{}, sentinel }), store)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.CreateSession(context.Background(), metadata())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := rt.Run(context.Background(), sess.ID, input())
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if turn.Status != core.TurnFailed || len(turn.ModelCalls) != 1 || turn.ModelCalls[0].Status != core.CallFailed || turn.ModelCalls[0].Error.Message == sentinel.Error() {
		t.Fatal(turn)
	}
	loaded, err := store.Load(context.Background(), sess.ID)
	if err != nil || loaded.Turns[0].Status != core.TurnFailed {
		t.Fatal(loaded, err)
	}
}

func TestToolDenialAndIncompleteHistory(t *testing.T) {
	for _, batch := range []bool{false, true} {
		store := memory.New()
		write, err := builtin.NewWriteFile()
		if err != nil {
			t.Fatal(err)
		}
		def := definition()
		def.Tools = []core.Tool{write}
		calls := 0
		client := modelFunc(func(context.Context, model.Request) (model.Response, error) {
			calls++
			out := answer()
			out.FinishReason = model.FinishTools
			out.Message.Content = []core.Content{{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "one", Name: "write_file", Arguments: json.RawMessage(`{"path":"denied","content":"no"}`)}}}
			if batch {
				out.Message.Content = append(out.Message.Content, core.Content{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "two", Name: "write_file", Arguments: json.RawMessage(`{"path":"other","content":"no"}`)}})
			}
			return out, nil
		})
		rt, err := runtime.New(def, client, store)
		if err != nil {
			t.Fatal(err)
		}
		sess, err := rt.CreateSession(context.Background(), metadata())
		if err != nil {
			t.Fatal(err)
		}
		turn, err := rt.Run(context.Background(), sess.ID, input())
		if !errors.Is(err, tool.ErrDenied) || len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Status != core.CallFailed || turn.ToolCalls[0].StartedAt != nil || turn.Messages[2].Content[0].ToolResult.Error == nil {
			t.Fatal(turn, err)
		}
		box, err := rt.Sandbox(sess.Metadata.Environment.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = box.ReadFile(context.Background(), core.Call{}, "/denied"); !errors.Is(err, core.ErrNotExist) {
			t.Fatal(err)
		}
		if batch {
			if _, err = rt.Run(context.Background(), sess.ID, input()); !errors.Is(err, runtime.ErrHistory) || calls != 1 {
				t.Fatal("incomplete history was replayed", err, calls)
			}
		}
	}
}

func TestExternalEnvironment(t *testing.T) {
	store := memory.New()
	resolved := false
	rt, err := runtime.New(definition(), modelFunc(func(context.Context, model.Request) (model.Response, error) { return answer(), nil }), store, runtime.WithEnvironment(func(_ context.Context, b core.EnvironmentBinding) (core.EnvironmentBinding, runtime.Capabilities, error) {
		resolved = true
		b.Revision = 9
		b.PolicyVersion = 3
		return b, func(context.Context, core.Call) (core.ToolContext, error) { return core.ToolContext{}, nil }, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	m := metadata()
	m.Environment = core.EnvironmentBinding{ID: "external", Dir: "/custom"}
	sess, err := rt.CreateSession(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := rt.Run(context.Background(), sess.ID, input())
	if err != nil || !resolved || turn.Metadata.Environment.Revision != 9 || turn.Metadata.Environment.Dir != "/custom" {
		t.Fatal(turn, err)
	}
	if _, err = rt.Sandbox("external"); !errors.Is(err, runtime.ErrEnvironment) {
		t.Fatal(err)
	}
}

func TestSelectionAndNotificationHistory(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	rt, err := runtime.New(definition(), modelFunc(func(_ context.Context, r model.Request) (model.Response, error) {
		if r.Model.ID != "override" || len(r.Messages) != 1 {
			t.Fatalf("unexpected model request: %+v", r)
		}
		return answer(), nil
	}), store)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.CreateSession(ctx, metadata())
	if err != nil {
		t.Fatal(err)
	}
	notice := core.Turn{ID: "notice", Kind: core.TurnNotification, Status: core.TurnRunning, Messages: []core.Message{{ID: "notice-message", Role: core.RoleSystem, Content: []core.Content{{Kind: core.ContentText, Text: "presentation only"}}}}}
	v, err := store.Commit(ctx, sess.ID, session.Commit{OperationID: "notice-start", ExpectedVersion: sess.Version, Turn: notice})
	if err != nil {
		t.Fatal(err)
	}
	end := int64(1)
	notice.EndedAt = &end
	notice.Status = core.TurnCompleted
	if _, err = store.Commit(ctx, sess.ID, session.Commit{OperationID: "notice-end", ExpectedVersion: v, Turn: notice}); err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Model = core.ModelSelection{ID: "override"}
	turn, err := rt.Run(ctx, sess.ID, in)
	if err != nil || turn.Metadata.Model.ID != "override" {
		t.Fatal(turn, err)
	}
	saved, err := store.Load(ctx, sess.ID)
	if err != nil || saved.Metadata.Model.ID != "logical" {
		t.Fatal(saved, err)
	}
}

func TestContextBudgetStopsBeforeModel(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	calls := 0
	builder, err := gcontext.New(gcontext.CounterFunc(func(context.Context, model.Request) (core.TokenMeasurement, error) {
		return core.TokenMeasurement{Tokens: 11, Kind: core.MeasurementExact, Counter: core.Binding{ID: "test", Version: "1"}}, nil
	}), gcontext.FixedBudget(gcontext.Limits{ModelID: "logical", InputTokens: 10, OutputReserve: 2}))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New(definition(), modelFunc(func(context.Context, model.Request) (model.Response, error) { calls++; return answer(), nil }), store, runtime.WithAgentOptions(agent.WithContextBuilder(builder)))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.CreateSession(ctx, metadata())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := rt.Run(ctx, sess.ID, input())
	if !errors.Is(err, gcontext.ErrOverflow) || calls != 0 || len(turn.ModelCalls) != 0 || turn.Status != core.TurnFailed {
		t.Fatal(turn, err, calls)
	}
}

func TestContextMeasuredEachToolRound(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	measures, calls := 0, 0
	builder, err := gcontext.New(gcontext.CounterFunc(func(_ context.Context, r model.Request) (core.TokenMeasurement, error) {
		measures++
		return core.TokenMeasurement{Tokens: int64(len(r.Messages)), Kind: core.MeasurementExact, Counter: core.Binding{ID: "test", Version: "1"}}, nil
	}), gcontext.FixedBudget(gcontext.Limits{ModelID: "logical", InputTokens: 2, OutputReserve: 10}))
	if err != nil {
		t.Fatal(err)
	}
	write, err := builtin.NewWriteFile()
	if err != nil {
		t.Fatal(err)
	}
	def := definition()
	def.Tools = []core.Tool{write}
	rt, err := runtime.New(def, modelFunc(func(context.Context, model.Request) (model.Response, error) {
		calls++
		out := answer()
		out.FinishReason = model.FinishTools
		out.Message.Content = []core.Content{{Kind: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "c", Name: "write_file", Arguments: json.RawMessage(`{"path":"/note","content":"done"}`)}}}
		return out, nil
	}), store, runtime.WithAgentOptions(agent.WithContextBuilder(builder), agent.WithToolOptions(tool.WithAuthorizer(func(context.Context, tool.Authorization) error { return nil }))))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.CreateSession(ctx, metadata())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := rt.Run(ctx, sess.ID, input())
	if !errors.Is(err, gcontext.ErrOverflow) || measures != 2 || calls != 1 || len(turn.Contexts) != 1 || turn.ModelCalls[0].ContextSnapshotID != turn.Contexts[0].ID || turn.Contexts[0].SourceMessageIDs[0] != turn.Messages[0].ID {
		t.Fatal(turn, err, calls, measures)
	}
	if turn.ToolCalls[0].Status != core.CallCompleted {
		t.Fatal("completed effect lost")
	}
	loaded, err := store.Load(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Turns[0].Contexts) != 1 || loaded.Turns[0].Status != core.TurnFailed {
		t.Fatal(loaded)
	}
	journal, err := store.History(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range journal {
		for _, call := range entry.Turn.ModelCalls {
			if call.ContextSnapshotID == "" || len(entry.Turn.Contexts) == 0 {
				t.Fatal("call without atomic context")
			}
		}
	}
}
