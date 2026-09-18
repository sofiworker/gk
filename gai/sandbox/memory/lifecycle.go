package memory

import (
	"context"
	s "github.com/sofiworker/gk/gai/sandbox"
)

func (b *Sandbox) beginExternal(ctx context.Context) (context.Context, uint64) {
	child, cancel := context.WithCancel(ctx)
	b.next++
	token := b.next
	if b.active == 0 {
		b.idle = make(chan struct{})
	}
	b.active++
	b.cancels[token] = cancel
	return child, token
}
func (b *Sandbox) endExternal(token uint64) {
	b.cancels[token]()
	delete(b.cancels, token)
	b.active--
	if b.active == 0 {
		close(b.idle)
	}
}
func (b *Sandbox) Close(ctx context.Context) error {
	b.mu.Lock()
	b.closing = true
	for _, cancel := range b.cancels {
		cancel()
	}
	idle := b.idle
	b.mu.Unlock()
	select {
	case <-idle:
	case <-ctx.Done():
		return ctx.Err()
	}
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	return nil
}
func (b *Sandbox) Destroy(ctx context.Context, discard bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed || b.active > 0 {
		return s.ErrBusy
	}
	if !discard {
		for _, r := range b.resources {
			if r.binding.Target != nil && (r.pending != nil || !sameTree(r.baseline, b.subtree(r.binding.Path))) {
				return s.ErrConflict
			}
		}
	}
	b.destroyed = true
	b.tree = nil
	b.blobs = nil
	b.events = nil
	b.snapshots = nil
	b.records = nil
	b.requests = nil
	b.resources = nil
	return nil
}
func (b *Sandbox) Operation(ctx context.Context, id string) (s.Result, error) {
	if err := ctx.Err(); err != nil {
		return s.Result{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.records[id]
	if !ok {
		return s.Result{}, s.ErrNotExist
	}
	return r.result, r.err
}

func (b *Sandbox) State(ctx context.Context) (s.State, error) {
	if err := ctx.Err(); err != nil {
		return s.State{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.destroyed {
		return s.State{}, s.ErrClosed
	}
	return s.State{ID: b.cfg.id, Revision: b.revision, PolicyVersion: b.policy, Dir: b.cwd, Closing: b.closing && !b.closed, Closed: b.closed, SyncPaused: b.paused}, nil
}
