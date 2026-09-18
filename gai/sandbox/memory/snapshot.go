package memory

import (
	"context"
	"sort"

	s "github.com/sofiworker/gk/gai/sandbox"
)

func (b *Sandbox) Snapshot(ctx context.Context) (s.Snapshot, error) {
	if err := b.lock(ctx); err != nil {
		return s.Snapshot{}, err
	}
	defer b.unlock()
	if len(b.snapshots) >= b.cfg.limits.Snapshots {
		return s.Snapshot{}, s.ErrQuota
	}
	if err := b.eventSpace(1); err != nil {
		return s.Snapshot{}, err
	}
	bytes := int64(512)
	for p := range b.tree {
		bytes += int64(len(p) + 64)
	}
	if b.usage(b.tree, nil)+bytes > b.cfg.limits.Bytes {
		return s.Snapshot{}, s.ErrQuota
	}
	if err := ctx.Err(); err != nil {
		return s.Snapshot{}, err
	}
	info := s.Snapshot{ID: b.id("snapshot"), Revision: b.revision, PolicyVersion: b.policy}
	bindings := map[string]s.Binding{}
	epochs := map[string]uint64{}
	for id, r := range b.resources {
		v := r.binding
		v.Target = nil
		bindings[id] = v
		epochs[id] = r.epoch
	}
	b.snapshots[info.ID] = checkpoint{info: info, tree: cloneTree(b.tree), bindings: bindings, epochs: epochs}
	b.addEvent(s.Event{OperationID: info.ID, Kind: "snapshot", After: b.revision})
	return info, nil
}
func (b *Sandbox) Snapshots(ctx context.Context) ([]s.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []s.Snapshot{}
	for _, v := range b.snapshots {
		out = append(out, v.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (b *Sandbox) Restore(ctx context.Context, id string) (s.Result, error) {
	if err := b.lock(ctx); err != nil {
		return s.Result{}, err
	}
	defer b.unlock()
	if b.active > 0 {
		return s.Result{}, s.ErrBusy
	}
	cp, ok := b.snapshots[id]
	if !ok {
		return s.Result{}, s.ErrNotExist
	}
	if len(cp.bindings) != len(b.resources) {
		return s.Result{}, s.ErrConflict
	}
	for key, old := range cp.bindings {
		r, ok := b.resources[key]
		if !ok || r.binding.Path != old.Path || r.binding.Access != old.Access || r.epoch != cp.epochs[key] {
			return s.Result{}, s.ErrDenied
		}
		if r.pending != nil {
			return s.Result{}, s.ErrBusy
		}
	}
	if err := b.eventSpace(1); err != nil {
		return s.Result{}, err
	}
	if err := b.validate(cp.tree, nil); err != nil {
		return s.Result{}, err
	}
	if n, ok := cp.tree[b.cwd]; !ok || !n.dir {
		return s.Result{}, s.ErrConflict
	}
	if b.usage(cp.tree, nil)+diffCost(b.tree, cp.tree)+32768 > b.cfg.limits.Bytes {
		return s.Result{}, s.ErrQuota
	}
	if err := ctx.Err(); err != nil {
		return s.Result{}, err
	}
	changes := b.fileChanges(b.tree, cp.tree)
	before := b.revision
	b.tree = cloneTree(cp.tree)
	b.revision++
	b.paused = true
	state := s.SyncClean
	for _, r := range b.resources {
		r.status.State = s.SyncClean
		if r.binding.Mode != s.SyncNone && !sameTree(r.baseline, b.subtree(r.binding.Path)) {
			r.status.State = s.SyncPending
			state = s.SyncPending
		}
	}
	op := b.id("restore")
	b.addEvent(s.Event{OperationID: op, Kind: "restore", Path: id, Before: before, After: b.revision, Changes: changes})
	return s.Result{OperationID: op, Revision: b.revision, LocalApplied: true, SyncState: state}, nil
}
func (b *Sandbox) Release(ctx context.Context, id string) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	if _, ok := b.snapshots[id]; !ok {
		return s.ErrNotExist
	}
	delete(b.snapshots, id)
	b.gc()
	return nil
}
func (b *Sandbox) ResumeSync(ctx context.Context) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	b.paused = false
	return nil
}

// Diff 比较两个保留快照；after 为空时比较当前环境。
// Diff compares retained snapshots; an empty after selects current state.
func (b *Sandbox) Diff(ctx context.Context, before, after string) (s.ChangeSet, error) {
	if err := ctx.Err(); err != nil {
		return s.ChangeSet{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	first, ok := b.snapshots[before]
	if !ok {
		return s.ChangeSet{}, s.ErrNotExist
	}
	tree := b.tree
	rev := b.revision
	if after != "" {
		last, ok := b.snapshots[after]
		if !ok {
			return s.ChangeSet{}, s.ErrNotExist
		}
		tree = last.tree
		rev = last.info.Revision
	}
	return s.ChangeSet{From: first.info.Revision, To: rev, Changes: b.fileChanges(first.tree, tree)}, nil
}
