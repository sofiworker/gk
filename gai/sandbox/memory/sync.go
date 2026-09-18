package memory

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"

	s "github.com/sofiworker/gk/gai/sandbox"
)

func (b *Sandbox) materialize(n node) *s.Node {
	return &s.Node{Dir: n.dir, Data: append([]byte(nil), b.blobs[n.content]...)}
}
func (b *Sandbox) changes(before, after map[string]node) []s.Change {
	removes := []s.Change{}
	adds := []s.Change{}
	for p, n := range before {
		if p == "." {
			continue
		}
		v, ok := after[p]
		if !ok || v.dir != n.dir {
			removes = append(removes, s.Change{Path: p, Before: b.materialize(n)})
		}
	}
	for p, n := range after {
		if p == "." {
			continue
		}
		v, ok := before[p]
		if !ok || v != n {
			c := s.Change{Path: p, After: b.materialize(n)}
			if ok && v.dir == n.dir {
				c.Before = b.materialize(v)
			}
			adds = append(adds, c)
		}
	}
	sort.Slice(removes, func(i, j int) bool {
		a, c := removes[i].Path, removes[j].Path
		if strings.Count(a, "/") != strings.Count(c, "/") {
			return strings.Count(a, "/") > strings.Count(c, "/")
		}
		return a < c
	})
	sort.Slice(adds, func(i, j int) bool {
		a, c := adds[i].Path, adds[j].Path
		if strings.Count(a, "/") != strings.Count(c, "/") {
			return strings.Count(a, "/") < strings.Count(c, "/")
		}
		return a < c
	})
	return append(removes, adds...)
}
func (b *Sandbox) Sync(ctx context.Context, id string) (s.SyncStatus, error) {
	if err := b.lock(ctx); err != nil {
		return s.SyncStatus{}, err
	}
	defer b.unlock()
	return b.syncLocked(ctx, id)
}
func (b *Sandbox) syncLocked(ctx context.Context, id string, snapshotID ...string) (s.SyncStatus, error) {
	r, ok := b.resources[id]
	if !ok {
		return s.SyncStatus{}, s.ErrNotExist
	}
	if r.binding.Target == nil {
		return r.status, s.ErrUnsupported
	}
	if err := b.eventSpace(2); err != nil {
		return r.status, err
	}
	targetTree := b.tree
	revision := b.revision
	if len(snapshotID) > 0 {
		cp, ok := b.snapshots[snapshotID[0]]
		if !ok {
			return r.status, s.ErrNotExist
		}
		old, ok := cp.bindings[id]
		if !ok || cp.epochs[id] != r.epoch || old.Access != r.binding.Access {
			return r.status, s.ErrDenied
		}
		targetTree = cp.tree
		revision = cp.info.Revision
		if r.pending != nil && r.pending.batch.Revision != revision {
			return r.status, s.ErrBusy
		}
	}
	if r.pending != nil && r.pending.unknown {
		return r.status, s.ErrUnknown
	}
	if r.pending == nil {
		target := subtreeOf(targetTree, r.binding.Path)
		changes := b.changes(r.baseline, target)
		if len(changes) == 0 {
			r.status = s.SyncStatus{ResourceID: id, Revision: revision, State: s.SyncClean}
			if !sameTree(target, b.subtree(r.binding.Path)) {
				r.status.State = s.SyncPending
			}
			return r.status, nil
		}
		pendingBytes := int64(1024)
		for _, c := range changes {
			pendingBytes += int64(len(c.Path) + 128)
			if c.Before != nil {
				pendingBytes += int64(len(c.Before.Data))
			}
			if c.After != nil {
				pendingBytes += int64(len(c.After.Data))
			}
		}
		if b.usage(b.tree, nil)+pendingBytes > b.cfg.limits.Bytes {
			return r.status, s.ErrQuota
		}
		r.pending = &pending{batch: s.Batch{ID: b.id("batch"), SandboxID: b.cfg.id, ResourceID: id, Revision: revision, Changes: changes}, target: target}
	}
	p := r.pending
	r.status = s.SyncStatus{ResourceID: id, Revision: p.batch.Revision, State: s.SyncPending, BatchID: p.batch.ID, Applied: p.applied, Total: len(p.batch.Changes)}
	// 传递副本，防止外部实现修改重试所依赖的固定批次。
	// Pass a copy so external implementations cannot mutate the frozen retry batch.
	batch := p.batch
	batch.Changes = cloneChanges(p.batch.Changes[p.applied:])
	b.addEvent(s.Event{OperationID: p.batch.ID, Kind: "sync_start", Path: r.binding.Path, After: b.revision})
	child, token := b.beginExternal(ctx)
	b.mu.Unlock()
	n, err := safeApply(child, r.binding.Target, batch)
	b.mu.Lock()
	b.endExternal(token)
	if n < 0 || n > len(batch.Changes) {
		n = 0
		err = s.ErrUnknown
	}
	p.applied += n
	r.status.Applied = p.applied
	if err == nil && p.applied != len(p.batch.Changes) {
		err = s.ErrUnknown
	}
	if err != nil {
		p.unknown = errors.Is(err, s.ErrUnknown)
		r.status.State = s.SyncFailed
		if errors.Is(err, s.ErrSyncConflict) {
			r.status.State = s.SyncConflict
		}
		r.status.Error = "synchronization failed"
		if errors.Is(err, s.ErrSyncConflict) {
			r.status.Error = s.ErrSyncConflict.Error()
		}
	} else {
		r.baseline = p.target
		r.pending = nil
		r.status.State = s.SyncClean
		r.status.Error = ""
		if !sameTree(r.baseline, b.subtree(r.binding.Path)) {
			r.status.State = s.SyncPending
		}
	}
	b.addEvent(s.Event{OperationID: p.batch.ID, Kind: "sync_end", Path: r.binding.Path, After: b.revision, Error: r.status.Error})
	return r.status, err
}

// SyncTo 固定为指定快照的文件版本，不恢复或修改环境当前状态。
// SyncTo targets a snapshot revision without restoring or changing current environment state.
func (b *Sandbox) SyncTo(ctx context.Context, id, snapshotID string) (s.SyncStatus, error) {
	if err := b.lock(ctx); err != nil {
		return s.SyncStatus{}, err
	}
	defer b.unlock()
	return b.syncLocked(ctx, id, snapshotID)
}
func cloneChanges(in []s.Change) []s.Change {
	out := make([]s.Change, len(in))
	for i, c := range in {
		out[i] = c
		if c.Before != nil {
			v := *c.Before
			v.Data = append([]byte(nil), v.Data...)
			out[i].Before = &v
		}
		if c.After != nil {
			v := *c.After
			v.Data = append([]byte(nil), v.Data...)
			out[i].After = &v
		}
	}
	return out
}
func safeApply(ctx context.Context, t s.Target, b s.Batch) (n int, err error) {
	defer func() {
		if recover() != nil {
			err = s.ErrUnknown
		}
	}()
	return t.Apply(ctx, b)
}
func (b *Sandbox) SyncStatus(ctx context.Context, id string) (s.SyncStatus, error) {
	if err := ctx.Err(); err != nil {
		return s.SyncStatus{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.resources[id]
	if !ok {
		return s.SyncStatus{}, s.ErrNotExist
	}
	return r.status, nil
}

// Rebase 由可信调用方提供已核对的宿主基线，显式解除失败批次阻塞。
// Rebase accepts a host baseline verified by the trusted caller and explicitly unblocks a failed batch.
func (b *Sandbox) Rebase(ctx context.Context, id string, tree s.Tree) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	r, ok := b.resources[id]
	if !ok {
		return s.ErrNotExist
	}
	if err := b.eventSpace(1); err != nil {
		return err
	}
	base := map[string]node{}
	if len(tree) > b.cfg.limits.Entries {
		return s.ErrQuota
	}
	extra := map[string][]byte{}
	for p, n := range tree {
		if n.Dir && len(n.Data) > 0 {
			return s.ErrUnsupported
		}
		if p != "." {
			clean, err := resolve("/", p)
			if err != nil || strings.TrimPrefix(clean, "/") != p {
				return s.ErrPath
			}
		}
		v := node{dir: n.Dir}
		if int64(len(n.Data)) > b.cfg.limits.FileBytes {
			return s.ErrQuota
		}
		if !n.Dir {
			v.content = digest(n.Data)
			extra[v.content] = n.Data
		}
		base[p] = v
	}
	if n, ok := base["."]; !ok || !n.dir {
		return s.ErrPath
	}
	metadata := int64(32768)
	for p := range base {
		metadata += int64(len(p) + 64)
		if p != "." {
			parent, ok := base[path.Dir(p)]
			if !ok || !parent.dir {
				return s.ErrPath
			}
		}
	}
	if b.usage(b.tree, extra)+metadata > b.cfg.limits.Bytes {
		return s.ErrQuota
	}
	if err := b.validate(b.tree, extra); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for k, v := range extra {
		b.blobs[k] = append([]byte(nil), v...)
	}
	r.baseline = base
	r.pending = nil
	r.status = s.SyncStatus{ResourceID: id, State: s.SyncPending}
	b.addEvent(s.Event{Kind: "rebase", Path: r.binding.Path, After: b.revision})
	return nil
}
