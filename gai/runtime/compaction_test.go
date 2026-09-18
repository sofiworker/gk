package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sofiworker/gk/gai/agent"
	gcontext "github.com/sofiworker/gk/gai/context"
	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/model"
	runtime "github.com/sofiworker/gk/gai/runtime"
	"github.com/sofiworker/gk/gai/session"
	"github.com/sofiworker/gk/gai/session/memory"
)

func compactionRuntime(t *testing.T, store session.Store, summary model.Client, hard int64) (*runtime.Runtime, *int) {
	t.Helper()
	counter := gcontext.CounterFunc(func(_ context.Context, r model.Request) (core.TokenMeasurement, error) {
		n := int64(0)
		for _, m := range r.Messages {
			for _, c := range m.Content {
				n += int64(len(c.Text))
			}
		}
		return core.TokenMeasurement{Tokens: n, Kind: core.MeasurementEstimated, Counter: core.Binding{ID: "test-text", Version: "1"}}, nil
	})
	builder, err := gcontext.New(counter, gcontext.FixedBudget(gcontext.Limits{ModelID: "logical", InputTokens: hard, OutputReserve: 32}))
	if err != nil {
		t.Fatal(err)
	}
	summaryBuilder, err := gcontext.New(counter, gcontext.FixedBudget(gcontext.Limits{ModelID: "summary", InputTokens: 10000, OutputReserve: 32}))
	if err != nil {
		t.Fatal(err)
	}
	compactor, err := gcontext.NewCompactor(summary, summaryBuilder, core.ModelSelection{ID: "summary"}, 32, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	def := definition()
	def.ContextPolicy = core.ContextPolicy{SoftInputTokens: 150, MaxCompactions: 1}
	calls := new(int)
	rt, err := runtime.New(def, model.ClientFunc(func(_ context.Context, r model.Request) (model.Response, error) { *calls++; return answer(), nil }), store, runtime.WithAgentOptions(agent.WithContextBuilder(builder), agent.WithCompactor(compactor)))
	if err != nil {
		t.Fatal(err)
	}
	return rt, calls
}

func seedHistory(t *testing.T, rt *runtime.Runtime, store session.Store) core.Session {
	t.Helper()
	ctx := context.Background()
	s, err := rt.CreateSession(ctx, metadata())
	if err != nil {
		t.Fatal(err)
	}
	turn := core.Turn{ID: "old", Kind: core.TurnInteraction, Status: core.TurnRunning, Messages: []core.Message{{ID: "old-user", Role: core.RoleUser, Content: []core.Content{{Kind: core.ContentText, Text: strings.Repeat("x", 300)}}}, {ID: "old-answer", Role: core.RoleAssistant, Content: []core.Content{{Kind: core.ContentText, Text: "old answer"}}}}}
	v, err := store.Commit(ctx, s.ID, session.Commit{OperationID: "seed", ExpectedVersion: s.Version, Turn: turn})
	if err != nil {
		t.Fatal(err)
	}
	end := int64(1)
	turn.Status = core.TurnCompleted
	turn.EndedAt = &end
	if _, err = store.Commit(ctx, s.ID, session.Commit{OperationID: "seed-end", ExpectedVersion: v, Turn: turn}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAutomaticCompactionAndReuse(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	summaries := 0
	rt, calls := compactionRuntime(t, store, model.ClientFunc(func(_ context.Context, r model.Request) (model.Response, error) {
		summaries++
		if len(r.Tools) != 0 || r.Model.ID != "summary" || r.Parameters.MaxOutputTokens == nil {
			t.Fatal("unbounded summary", r)
		}
		out := answer()
		out.Message.Content[0].Text = "retained facts"
		return out, nil
	}), 200)
	s := seedHistory(t, rt, store)
	turn, err := rt.Run(ctx, s.ID, input())
	if err != nil {
		t.Fatal(err)
	}
	if summaries != 1 || *calls != 1 || len(turn.Compactions) != 1 || !turn.Compactions[0].Applied || turn.Compactions[0].ModelCall.Status != core.CallCompleted {
		t.Fatal(turn)
	}
	if turn.Compactions[0].Before.Tokens <= turn.Compactions[0].After.Tokens {
		t.Fatal("no reduction")
	}
	found := false
	for _, key := range turn.Contexts[0].InputSourceIDs {
		found = found || strings.HasPrefix(key, "summary:")
	}
	if !found {
		t.Fatal("missing summary provenance")
	}
	if _, err = rt.Run(ctx, s.ID, input()); err != nil {
		t.Fatal(err)
	}
	if summaries != 1 {
		t.Fatal("summary was not reused")
	}
	saved, err := store.Load(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Turns[0].Messages[0].Content[0].Text) != 300 {
		t.Fatal("history changed")
	}
	log, err := store.History(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	running := false
	for _, c := range log {
		for _, record := range c.Turn.Compactions {
			running = running || record.Status == core.CallRunning
		}
	}
	if !running {
		t.Fatal("summary intent missing")
	}
}

func TestCompactionFailureAndNoProgress(t *testing.T) {
	for _, hard := range []int64{200, 1000} {
		for _, mode := range []string{"error", "no-progress", "empty"} {
			t.Run(fmt.Sprintf("%s/%d", mode, hard), func(t *testing.T) {
				store := memory.New()
				rt, calls := compactionRuntime(t, store, model.ClientFunc(func(context.Context, model.Request) (model.Response, error) {
					if mode == "error" {
						return model.Response{}, errors.New("summary failed")
					}
					out := answer()
					out.Message.Content[0].Text = ""
					if mode == "no-progress" {
						out.Message.Content[0].Text = strings.Repeat("long", 400)
					}
					return out, nil
				}), hard)
				s := seedHistory(t, rt, store)
				turn, err := rt.Run(context.Background(), s.ID, input())
				if hard == 200 {
					if !errors.Is(err, gcontext.ErrOverflow) || *calls != 0 {
						t.Fatal(err, *calls)
					}
				} else if err != nil || *calls != 1 {
					t.Fatal(err, *calls)
				}
				if len(turn.Compactions) != 1 || turn.Compactions[0].Applied || turn.Compactions[0].Status != core.CallFailed {
					t.Fatal(turn)
				}
			})
		}
	}
}

func TestCompactionActivationFailureStopsGeneration(t *testing.T) {
	store := &failingStore{Store: memory.New()}
	rt, calls := compactionRuntime(t, store, model.ClientFunc(func(context.Context, model.Request) (model.Response, error) { return answer(), nil }), 200)
	s := seedHistory(t, rt, store)
	// 新 Turn、摘要意图之后，拒绝摘要激活提交。
	// Reject activation after the new Turn and summary intent commits.
	store.failAt = store.commits + 3
	_, err := rt.Run(context.Background(), s.ID, input())
	if !errors.Is(err, agent.ErrCheckpoint) || *calls != 0 {
		t.Fatal(err, *calls)
	}
	saved, err := store.Load(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	record := saved.Turns[1].Compactions[0]
	if record.Applied || record.Status != core.CallRunning {
		t.Fatal(record)
	}
}
