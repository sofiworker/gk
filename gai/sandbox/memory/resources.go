package memory

import (
	"context"
	"path"
	"strings"

	s "github.com/sofiworker/gk/gai/sandbox"
)

func (b *Sandbox) Provide(ctx context.Context, r s.Resource) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	return b.provide(ctx, r)
}
func (b *Sandbox) provide(ctx context.Context, r s.Resource) error {
	binding := r.Binding
	if binding.ID == "" || len(binding.ID) > 128 || binding.Access & ^s.ReadWrite != 0 {
		return s.ErrPath
	}
	p, err := resolve("/", binding.Path)
	if err != nil || !strings.HasPrefix(binding.Path, "/") || p == "/" {
		return s.ErrPath
	}
	binding.Path = p
	if binding.Mode == "" {
		binding.Mode = s.SyncNone
	}
	switch binding.Mode {
	case s.SyncNone:
		if binding.Target != nil {
			return s.ErrConflict
		}
	case s.SyncExplicit, s.SyncImmediate:
		if binding.Target == nil {
			return s.ErrUnsupported
		}
	default:
		return s.ErrUnsupported
	}
	if _, ok := b.resources[binding.ID]; ok {
		return s.ErrConflict
	}
	for _, existing := range b.resources {
		if within(p, existing.binding.Path) || within(existing.binding.Path, p) {
			return s.ErrConflict
		}
	}
	if _, ok := b.tree[p]; ok {
		return s.ErrExist
	}
	parent, ok := b.tree[path.Dir(p)]
	if !ok || !parent.dir {
		return s.ErrNotExist
	}
	if err = b.eventSpace(1); err != nil {
		return err
	}
	tree := cloneTree(b.tree)
	extra := map[string][]byte{}
	baseline := map[string]node{}
	if root, ok := r.Tree["."]; !ok || !root.Dir {
		return s.ErrPath
	}
	for rel, n := range r.Tree {
		if len(rel) > 4096 {
			return s.ErrPath
		}
		if rel != "." && (path.Clean(rel) != rel || strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") || strings.ContainsAny(rel, "\\\x00")) {
			return s.ErrPath
		}
		if n.Dir && len(n.Data) > 0 {
			return s.ErrUnsupported
		}
		if int64(len(n.Data)) > b.cfg.limits.FileBytes {
			return s.ErrQuota
		}
		v := node{dir: n.Dir}
		if !n.Dir {
			v.content = digest(n.Data)
			extra[v.content] = n.Data
		}
		k := p
		if rel != "." {
			k = p + "/" + rel
		}
		if len(k) > 4096 {
			return s.ErrPath
		}
		tree[k] = v
		baseline[rel] = v
	}
	for k := range tree {
		if k != "/" {
			n, ok := tree[path.Dir(k)]
			if !ok || !n.dir {
				return s.ErrPath
			}
		}
	}
	if err = b.validate(tree, extra); err != nil {
		return err
	}
	baselineBytes := int64(32768) + diffCost(b.tree, tree)
	for k := range baseline {
		baselineBytes += int64(len(k) + 64)
	}
	if b.usage(tree, extra)+baselineBytes > b.cfg.limits.Bytes {
		return s.ErrQuota
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for k, v := range extra {
		b.blobs[k] = append([]byte(nil), v...)
	}
	changes := b.fileChanges(b.tree, tree)
	b.tree = tree
	b.revision++
	b.policy++
	b.resources[binding.ID] = &resource{epoch: b.policy, binding: binding, baseline: baseline, status: s.SyncStatus{ResourceID: binding.ID, Revision: b.revision, State: s.SyncClean}}
	b.addEvent(s.Event{Kind: "provide", Path: p, After: b.revision, Changes: changes})
	return nil
}
func (b *Sandbox) Bindings(ctx context.Context) ([]s.Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []s.Binding{}
	for _, r := range b.resources {
		v := r.binding
		v.Target = nil
		out = append(out, v)
	}
	return out, nil
}
func (b *Sandbox) SetAccess(ctx context.Context, id string, a s.Access) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	if a & ^s.ReadWrite != 0 {
		return s.ErrDenied
	}
	r, ok := b.resources[id]
	if !ok {
		return s.ErrNotExist
	}
	if err := b.eventSpace(1); err != nil {
		return err
	}
	r.binding.Access = a
	b.policy++
	b.addEvent(s.Event{Kind: "access", Path: r.binding.Path, After: b.revision})
	return nil
}
func (b *Sandbox) Revoke(ctx context.Context, id string, discard bool) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	r, ok := b.resources[id]
	if !ok {
		return s.ErrNotExist
	}
	if !discard && (r.pending != nil || !sameTree(r.baseline, b.subtree(r.binding.Path))) {
		return s.ErrConflict
	}
	if err := b.eventSpace(1); err != nil {
		return err
	}
	tree := cloneTree(b.tree)
	for p := range tree {
		if within(p, r.binding.Path) {
			delete(tree, p)
		}
	}
	if b.usage(tree, nil)+diffCost(b.tree, tree)+32768 > b.cfg.limits.Bytes {
		return s.ErrQuota
	}
	changes := b.fileChanges(b.tree, tree)
	b.tree = tree
	delete(b.resources, id)
	b.revision++
	b.policy++
	b.addEvent(s.Event{Kind: "revoke", Path: r.binding.Path, After: b.revision, Changes: changes})
	return nil
}
func sameTree(a, c map[string]node) bool {
	if len(a) != len(c) {
		return false
	}
	for p, n := range a {
		if other, ok := c[p]; !ok || n != other {
			return false
		}
	}
	return true
}
func (b *Sandbox) subtree(root string) map[string]node {
	return subtreeOf(b.tree, root)
}
func subtreeOf(tree map[string]node, root string) map[string]node {
	out := map[string]node{}
	for p, n := range tree {
		if within(p, root) {
			rel := strings.TrimPrefix(p, root)
			if rel == "" {
				rel = "."
			} else {
				rel = strings.TrimPrefix(rel, "/")
			}
			out[rel] = n
		}
	}
	return out
}
func (b *Sandbox) RequestResource(ctx context.Context, id, description string, a s.Access) (s.ResourceRequest, error) {
	if err := b.lock(ctx); err != nil {
		return s.ResourceRequest{}, err
	}
	defer b.unlock()
	if id == "" || len(id)+len(description) > 8192 || a & ^s.ReadWrite != 0 {
		return s.ResourceRequest{}, s.ErrPath
	}
	if r, ok := b.requests[id]; ok {
		if r.Description != description || r.Access != a {
			return r, s.ErrConflict
		}
		return r, nil
	}
	if len(b.requests) >= b.cfg.limits.Requests || b.usage(b.tree, nil)+int64(len(description)+len(id)+1024) > b.cfg.limits.Bytes {
		return s.ResourceRequest{}, s.ErrQuota
	}
	if err := b.eventSpace(1); err != nil {
		return s.ResourceRequest{}, err
	}
	r := s.ResourceRequest{ID: id, Description: description, Access: a, State: s.Requested}
	b.requests[id] = r
	b.addEvent(s.Event{OperationID: id, Kind: "resource_requested", After: b.revision})
	return r, nil
}
func (b *Sandbox) DecideResource(ctx context.Context, id string, approve bool) (s.ResourceRequest, error) {
	if err := b.lock(ctx); err != nil {
		return s.ResourceRequest{}, err
	}
	defer b.unlock()
	r, ok := b.requests[id]
	if !ok {
		return r, s.ErrNotExist
	}
	if r.State != s.Requested {
		return r, s.ErrConflict
	}
	if err := b.eventSpace(1); err != nil {
		return r, err
	}
	r.State = s.Denied
	if approve {
		r.State = s.Approved
	}
	b.requests[id] = r
	b.addEvent(s.Event{OperationID: id, Kind: string(r.State), After: b.revision})
	return r, nil
}
func (b *Sandbox) FulfillResource(ctx context.Context, id string, res s.Resource) (s.ResourceRequest, error) {
	if err := b.lock(ctx); err != nil {
		return s.ResourceRequest{}, err
	}
	defer b.unlock()
	r, ok := b.requests[id]
	if !ok {
		return r, s.ErrNotExist
	}
	if r.State != s.Approved && r.State != s.ProvideFailed {
		return r, s.ErrConflict
	}
	if res.Binding.Access&r.Access != r.Access {
		return r, s.ErrDenied
	}
	if err := b.eventSpace(3); err != nil {
		return r, err
	}
	r.State = s.Providing
	b.requests[id] = r
	b.addEvent(s.Event{OperationID: id, Kind: "providing", After: b.revision})
	err := b.provide(ctx, res)
	if err != nil {
		r.State = s.ProvideFailed
		r.Error = err.Error()
	} else {
		r.State = s.Available
		r.Error = ""
		r.ResourceID = res.Binding.ID
		r.Path = b.resources[res.Binding.ID].binding.Path
	}
	b.requests[id] = r
	b.addEvent(s.Event{OperationID: id, Kind: string(r.State), Path: r.Path, Error: r.Error, After: b.revision})
	return r, err
}
func (b *Sandbox) ResourceRequest(ctx context.Context, id string) (s.ResourceRequest, error) {
	if err := ctx.Err(); err != nil {
		return s.ResourceRequest{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.requests[id]
	if !ok {
		return r, s.ErrNotExist
	}
	return r, nil
}
