// Package memory 实现带 CAS 和操作去重的进程内会话存储。
// Package memory implements in-process session storage with CAS and operation deduplication.
package memory

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/session"
)

type revision struct {
	data    []byte
	version uint64
}
type entry struct {
	state      core.Session
	operations map[string]revision
	journal    []session.Commit
}
type Store struct {
	mu      sync.Mutex
	entries map[string]*entry
}

func New() *Store { return &Store{entries: make(map[string]*entry)} }

func clone[T any](v T) (out T, err error) {
	b, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return
}
func (s *Store) Create(ctx context.Context, value core.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value.ID == "" || value.Version != 0 || len(value.Turns) != 0 {
		return session.ErrInvalid
	}
	copied, err := clone(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[value.ID]; ok {
		return session.ErrConflict
	}
	if s.entries == nil {
		s.entries = make(map[string]*entry)
	}
	now := time.Now().UnixMilli()
	copied.CreatedAt, copied.UpdatedAt, copied.Version = now, now, 1
	s.entries[value.ID] = &entry{state: copied, operations: make(map[string]revision)}
	return nil
}
func (s *Store) Load(ctx context.Context, key string) (core.Session, error) {
	if err := ctx.Err(); err != nil {
		return core.Session{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return core.Session{}, session.ErrNotFound
	}
	return clone(e.state)
}
func active(t core.Turn) bool {
	return t.Status == core.TurnRunning || t.Status == core.TurnWaitingApproval
}

// 校验已存在消息与终结调用不可改写；运行中调用可更新结果。
// Existing messages and terminal calls are immutable; running calls may acquire results.
func extends(old, next core.Turn) bool {
	if len(next.Compactions) < len(old.Compactions) {
		return false
	}
	for i, c := range old.Compactions {
		n := next.Compactions[i]
		if c.ID != n.ID || c.ModelCall.ID != n.ModelCall.ID || c.ModelCall.RequestedModel != n.ModelCall.RequestedModel || c.ModelCall.StartedAt != n.ModelCall.StartedAt || !reflect.DeepEqual(c.SourceIDs, n.SourceIDs) || c.RequestDigest != n.RequestDigest || (c.Status != core.CallRunning && !reflect.DeepEqual(c, n)) {
			return false
		}
	}
	if len(next.Contexts) < len(old.Contexts) {
		return false
	}
	for i, snapshot := range old.Contexts {
		if !reflect.DeepEqual(snapshot, next.Contexts[i]) {
			return false
		}
	}
	if old.ID != next.ID || old.Kind != next.Kind || old.StartedAt != next.StartedAt || !reflect.DeepEqual(old.Metadata, next.Metadata) || len(next.Messages) < len(old.Messages) || len(next.ModelCalls) < len(old.ModelCalls) || len(next.ToolCalls) < len(old.ToolCalls) {
		return false
	}
	for i, m := range old.Messages {
		if !reflect.DeepEqual(m, next.Messages[i]) {
			return false
		}
	}
	for i, c := range old.ModelCalls {
		n := next.ModelCalls[i]
		if c.ID != n.ID || c.ContextSnapshotID != n.ContextSnapshotID || c.StartedAt != n.StartedAt || c.RequestedModel != n.RequestedModel || c.RetryOf != n.RetryOf || (c.Status != core.CallRunning && c.Status != core.CallPending && !reflect.DeepEqual(c, n)) {
			return false
		}
	}
	for i, c := range old.ToolCalls {
		n := next.ToolCalls[i]
		if c.ID != n.ID || c.CallID != n.CallID || c.RetryOf != n.RetryOf || (c.Status != core.CallRunning && c.Status != core.CallPending && !reflect.DeepEqual(c, n)) {
			return false
		}
	}
	return true
}
func (s *Store) Commit(ctx context.Context, key string, change session.Commit) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if change.OperationID == "" || change.Turn.ID == "" {
		return 0, session.ErrInvalid
	}
	switch change.Turn.Status {
	case core.TurnRunning, core.TurnWaitingApproval, core.TurnCompleted, core.TurnFailed, core.TurnCancelled:
	default:
		return 0, session.ErrInvalid
	}
	if active(change.Turn) != (change.Turn.EndedAt == nil) {
		return 0, session.ErrInvalid
	}
	copied, err := clone(change)
	if err != nil {
		return 0, err
	}
	data, err := json.Marshal(copied)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return 0, session.ErrNotFound
	}
	if previous, ok := e.operations[change.OperationID]; ok {
		if string(previous.data) != string(data) {
			return 0, session.ErrConflict
		}
		return previous.version, nil
	}
	if e.state.Version != change.ExpectedVersion {
		return 0, session.ErrConflict
	}
	index := -1
	for i, t := range e.state.Turns {
		if t.ID == copied.Turn.ID {
			index = i
		}
		if active(t) && t.ID != copied.Turn.ID {
			return 0, session.ErrBusy
		}
	}
	if index >= 0 {
		old := e.state.Turns[index]
		if !active(old) || !extends(old, copied.Turn) {
			return 0, session.ErrInvalid
		}
		e.state.Turns[index] = copied.Turn
	} else {
		if copied.Turn.Status != core.TurnRunning {
			return 0, session.ErrInvalid
		}
		e.state.Turns = append(e.state.Turns, copied.Turn)
	}
	e.state.Version++
	e.state.UpdatedAt = time.Now().UnixMilli()
	e.journal = append(e.journal, copied)
	e.operations[change.OperationID] = revision{data: data, version: e.state.Version}
	return e.state.Version, nil
}

// History 返回原子提交日志副本，用于追溯状态变化；内存实现不提供崩溃持久性。
// History returns copies of atomic commits for auditing; this implementation provides no crash durability.
func (s *Store) History(ctx context.Context, key string) ([]session.Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return nil, session.ErrNotFound
	}
	return clone(e.journal)
}
