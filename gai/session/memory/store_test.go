package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/session"
	"github.com/sofiworker/gk/gai/session/memory"
)

func TestAtomicCommitAndCopies(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	if err := s.Create(ctx, core.Session{ID: "s"}); err != nil {
		t.Fatal(err)
	}
	start := session.Commit{OperationID: "start", ExpectedVersion: 1, Turn: core.Turn{ID: "t", Status: core.TurnRunning, Messages: []core.Message{{ID: "m", Role: core.RoleUser}}}}
	if v, err := s.Commit(ctx, "s", start); err != nil || v != 2 {
		t.Fatal(v, err)
	}
	if v, err := s.Commit(ctx, "s", start); err != nil || v != 2 {
		t.Fatal("idempotent retry", v, err)
	}
	changed := start
	changed.Turn.Status = core.TurnWaitingApproval
	if _, err := s.Commit(ctx, "s", changed); !errors.Is(err, session.ErrConflict) {
		t.Fatal(err)
	}
	next := session.Commit{OperationID: "new", ExpectedVersion: 2, Turn: core.Turn{ID: "other", Status: core.TurnRunning}}
	if _, err := s.Commit(ctx, "s", next); !errors.Is(err, session.ErrBusy) {
		t.Fatal(err)
	}
	loaded, err := s.Load(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	loaded.Turns[0].Messages[0].Role = core.RoleAssistant
	again, _ := s.Load(ctx, "s")
	if again.Turns[0].Messages[0].Role != core.RoleUser {
		t.Fatal("load aliases state")
	}
	changed = start
	changed.OperationID = "rewrite"
	changed.ExpectedVersion = 2
	changed.Turn = loaded.Turns[0]
	if _, err := s.Commit(ctx, "s", changed); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
	ended := int64(1)
	done := start
	done.OperationID = "done"
	done.ExpectedVersion = 2
	done.Turn.Status = core.TurnCompleted
	done.Turn.EndedAt = &ended
	if _, err := s.Commit(ctx, "s", done); err != nil {
		t.Fatal(err)
	}
	journal, err := s.History(ctx, "s")
	if err != nil || len(journal) != 2 {
		t.Fatal(journal, err)
	}
	journal[0].Turn.Messages[0].ID = "mutated"
	journal, _ = s.History(ctx, "s")
	if journal[0].Turn.Messages[0].ID != "m" {
		t.Fatal("journal aliases state")
	}
	next.ExpectedVersion = 3
	if _, err := s.Commit(ctx, "s", next); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(ctx, "missing"); !errors.Is(err, session.ErrNotFound) {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Load(cancelCtx, "s"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConcurrentTurnReservation(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	if err := s.Create(ctx, core.Session{ID: "s"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, key := range []string{"a", "b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Commit(ctx, "s", session.Commit{OperationID: key, ExpectedVersion: 1, Turn: core.Turn{ID: key, Status: core.TurnRunning}})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, session.ErrConflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
}

func TestContextSnapshotsAreAppendOnly(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	if err := s.Create(ctx, core.Session{ID: "s"}); err != nil {
		t.Fatal(err)
	}
	turn := core.Turn{ID: "t", Status: core.TurnRunning, Contexts: []core.ContextSnapshot{{ID: "context", RequestDigest: "original"}}, ModelCalls: []core.ModelCall{{ID: "call", ContextSnapshotID: "context", Status: core.CallRunning}}}
	v, err := s.Commit(ctx, "s", session.Commit{OperationID: "start", ExpectedVersion: 1, Turn: turn})
	if err != nil {
		t.Fatal(err)
	}
	turn.Contexts[0].RequestDigest = "rewritten"
	if _, err = s.Commit(ctx, "s", session.Commit{OperationID: "rewrite", ExpectedVersion: v, Turn: turn}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
	turn.Contexts[0].RequestDigest = "original"
	turn.ModelCalls[0].ContextSnapshotID = "other"
	if _, err = s.Commit(ctx, "s", session.Commit{OperationID: "relink", ExpectedVersion: v, Turn: turn}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
}
