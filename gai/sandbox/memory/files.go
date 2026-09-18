package memory

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	s "github.com/sofiworker/gk/gai/sandbox"
)

func (b *Sandbox) read(ctx context.Context, call s.Call, name, kind string) (node, string, error) {
	if len(call.ID)+len(call.SessionID)+len(call.TurnID)+len(call.ToolCallID) > 8192 {
		return node{}, "", s.ErrQuota
	}
	p, err := b.path(call, name)
	if err == nil {
		err = b.allow(p, s.Read)
	}
	n, ok := b.tree[p]
	if err == nil && !ok {
		err = s.ErrNotExist
	}
	if kind == "read" && err == nil && n.dir {
		err = s.ErrUnsupported
	}
	if kind == "list" && err == nil && !n.dir {
		err = s.ErrUnsupported
	}
	e := s.Event{OperationID: call.ID, Attribution: attribution(call), Kind: kind, Path: p, Before: b.revision, After: b.revision}
	if err != nil {
		e.Error = err.Error()
	} else {
		e.AfterContent = n.content
	}
	b.addEvent(e)
	return n, p, err
}
func (b *Sandbox) ReadFile(ctx context.Context, c s.Call, p string) ([]byte, error) {
	if err := b.lock(ctx); err != nil {
		return nil, err
	}
	defer b.unlock()
	if err := b.eventSpace(1); err != nil {
		return nil, err
	}
	n, _, err := b.read(ctx, c, p, "read")
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), b.blobs[n.content]...), nil
}
func (b *Sandbox) Stat(ctx context.Context, c s.Call, p string) (s.Entry, error) {
	if err := b.lock(ctx); err != nil {
		return s.Entry{}, err
	}
	defer b.unlock()
	if err := b.eventSpace(1); err != nil {
		return s.Entry{}, err
	}
	n, p, err := b.read(ctx, c, p, "stat")
	if err != nil {
		return s.Entry{}, err
	}
	return b.entry(p, n), nil
}
func (b *Sandbox) ReadDir(ctx context.Context, c s.Call, p string) ([]s.Entry, error) {
	if err := b.lock(ctx); err != nil {
		return nil, err
	}
	defer b.unlock()
	if err := b.eventSpace(1); err != nil {
		return nil, err
	}
	_, p, err := b.read(ctx, c, p, "list")
	if err != nil {
		return nil, err
	}
	out := []s.Entry{}
	for k, n := range b.tree {
		if k != p && path.Dir(k) == p && b.allow(k, s.Read) == nil {
			out = append(out, b.entry(k, n))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
func (b *Sandbox) WriteFile(ctx context.Context, c s.Call, p string, data []byte) (s.Result, error) {
	return b.mutate(ctx, c, "write", p, "", data)
}
func (b *Sandbox) Mkdir(ctx context.Context, c s.Call, p string) (s.Result, error) {
	return b.mutate(ctx, c, "mkdir", p, "", nil)
}

// Remove 递归删除目录，但拒绝删除资源绑定根或其祖先。
// Remove recursively deletes directories but rejects binding roots and their ancestors.
func (b *Sandbox) Remove(ctx context.Context, c s.Call, p string) (s.Result, error) {
	return b.mutate(ctx, c, "remove", p, "", nil)
}
func (b *Sandbox) Rename(ctx context.Context, c s.Call, p, to string) (s.Result, error) {
	return b.mutate(ctx, c, "rename", p, to, nil)
}
func (b *Sandbox) resourceAt(p string) string {
	for id, r := range b.resources {
		if within(p, r.binding.Path) {
			return id
		}
	}
	return ""
}
func (b *Sandbox) mutate(ctx context.Context, c s.Call, kind, name, dest string, data []byte, expected ...string) (s.Result, error) {
	if err := b.lock(ctx); err != nil {
		return s.Result{}, err
	}
	defer b.unlock()
	if len(name)+len(dest)+len(c.ID)+len(c.Dir)+len(c.SessionID)+len(c.TurnID)+len(c.ToolCallID) > 16384 {
		return s.Result{}, s.ErrQuota
	}
	if int64(len(data)) > b.cfg.limits.FileBytes {
		return s.Result{}, s.ErrQuota
	}
	p, err := b.path(c, name)
	to := ""
	if err == nil && dest != "" {
		to, err = b.path(c, dest)
	}
	dir := c.Dir
	if dir == "" {
		dir = b.cwd
	}
	fp := digest([]byte(fmt.Sprintf("%q|%q|%q|%q|%q|%q|%q|%q", kind, name, dest, dir, digest(data), c.SessionID, c.TurnID, c.ToolCallID)))
	if len(expected) > 0 {
		fp = digest([]byte(fp + "|" + expected[0]))
	}
	if c.ID != "" {
		if r, ok := b.records[c.ID]; ok {
			if r.fingerprint != fp {
				return s.Result{}, s.ErrConflict
			}
			return r.result, r.err
		}
	}
	if c.ID == "" {
		c.ID = b.id("op")
	}
	result := s.Result{OperationID: c.ID, Revision: b.revision, SyncState: s.SyncClean}
	if len(b.records) >= b.cfg.limits.Operations {
		return result, s.ErrQuota
	}
	if e := b.eventSpace(2); e != nil {
		return result, e
	}
	if err == nil {
		err = b.allow(p, s.Write)
	}
	if err == nil && to != "" {
		err = b.allow(to, s.Write)
	}
	old := b.tree[p]
	if err == nil && len(expected) > 0 && (old.dir || old.content != expected[0]) {
		err = s.ErrConflict
	}
	tree := cloneTree(b.tree)
	extra := map[string][]byte{}
	if err == nil {
		err = b.edit(tree, kind, p, to, data, extra)
	}
	if err == nil {
		err = b.validate(tree, extra)
	}
	if err == nil && b.usage(tree, extra)+int64(len(p)+len(to)+len(c.ID)+len(c.SessionID)+len(c.TurnID)+len(c.ToolCallID)+1024) > b.cfg.limits.Bytes {
		err = s.ErrQuota
	}
	if err == nil && b.usage(tree, extra)+diffCost(b.tree, tree)+32768 > b.cfg.limits.Bytes {
		err = s.ErrQuota
	}
	if err == nil {
		err = ctx.Err()
	}
	e := s.Event{OperationID: c.ID, Attribution: attribution(c), Kind: kind, Path: p, Destination: to, Before: b.revision, After: b.revision, BeforeContent: old.content}
	if err == nil {
		previous := b.tree
		b.tree = tree
		for k, v := range extra {
			b.blobs[k] = append([]byte(nil), v...)
		}
		e.Changes = b.fileChanges(previous, tree)
		b.revision++
		result.Revision = b.revision
		result.LocalApplied = true
		e.After = b.revision
		e.AfterContent = tree[p].content
		if kind == "rename" {
			e.AfterContent = tree[to].content
		}
	} else {
		e.Error = err.Error()
	}
	b.addEvent(e)
	if err == nil {
		id := b.resourceAt(p)
		if id != "" {
			r := b.resources[id]
			if r.binding.Mode != s.SyncNone {
				if r.pending == nil {
					r.status.State = s.SyncPending
				}
				result.SyncState = r.status.State
				if r.binding.Mode == s.SyncImmediate && !b.paused {
					for {
						_, err = b.syncLocked(ctx, id)
						if err != nil || r.status.State != s.SyncPending {
							break
						}
					}
					result.SyncState = r.status.State
				}
			}
		}
	}
	b.records[c.ID] = record{fingerprint: fp, result: result, err: err}
	return result, err
}
func (b *Sandbox) edit(tree map[string]node, kind, p, to string, data []byte, extra map[string][]byte) error {
	if p == "/" {
		return s.ErrDenied
	}
	if kind == "remove" || kind == "rename" {
		for _, r := range b.resources {
			if within(r.binding.Path, p) {
				return s.ErrDenied
			}
		}
	}
	old, exists := tree[p]
	switch kind {
	case "write", "mkdir":
		parent, ok := tree[path.Dir(p)]
		if !ok || !parent.dir {
			return s.ErrNotExist
		}
		if kind == "mkdir" {
			if exists {
				return s.ErrExist
			}
			tree[p] = node{dir: true}
		} else {
			if exists && old.dir {
				return s.ErrConflict
			}
			h := digest(data)
			extra[h] = data
			tree[p] = node{content: h}
		}
	case "remove":
		if !exists {
			return s.ErrNotExist
		}
		for k := range tree {
			if within(k, p) {
				delete(tree, k)
			}
		}
	case "rename":
		if !exists {
			return s.ErrNotExist
		}
		if _, ok := tree[to]; ok {
			return s.ErrExist
		}
		if within(to, p) {
			return s.ErrPath
		}
		if b.resourceAt(p) != b.resourceAt(to) {
			return s.ErrDenied
		}
		parent, ok := tree[path.Dir(to)]
		if !ok || !parent.dir {
			return s.ErrNotExist
		}
		for k, n := range b.tree {
			if within(k, p) {
				delete(tree, k)
				next := to + strings.TrimPrefix(k, p)
				if len(next) > 4096 {
					return s.ErrPath
				}
				tree[next] = n
			}
		}
	default:
		return s.ErrUnsupported
	}
	return nil
}

// View 固定执行归属和工作目录，只暴露工具能力。
// View fixes execution attribution and working directory and exposes only tool capabilities.
type View struct {
	box  *Sandbox
	call s.Call
}

func (b *Sandbox) View(call s.Call) *View { return &View{box: b, call: call} }
func (v *View) callFor(c s.Call) s.Call {
	c.Dir = v.call.Dir
	c.SessionID = v.call.SessionID
	c.TurnID = v.call.TurnID
	c.ToolCallID = v.call.ToolCallID
	return c
}
func (v *View) ReadFile(ctx context.Context, c s.Call, p string) ([]byte, error) {
	return v.box.ReadFile(ctx, v.callFor(c), p)
}
func (v *View) ReadDir(ctx context.Context, c s.Call, p string) ([]s.Entry, error) {
	return v.box.ReadDir(ctx, v.callFor(c), p)
}
func (v *View) Stat(ctx context.Context, c s.Call, p string) (s.Entry, error) {
	return v.box.Stat(ctx, v.callFor(c), p)
}
func (v *View) WriteFile(ctx context.Context, c s.Call, p string, d []byte) (s.Result, error) {
	return v.box.WriteFile(ctx, v.callFor(c), p, d)
}
func (v *View) Mkdir(ctx context.Context, c s.Call, p string) (s.Result, error) {
	return v.box.Mkdir(ctx, v.callFor(c), p)
}
func (v *View) Remove(ctx context.Context, c s.Call, p string) (s.Result, error) {
	return v.box.Remove(ctx, v.callFor(c), p)
}
func (v *View) Rename(ctx context.Context, c s.Call, p, to string) (s.Result, error) {
	return v.box.Rename(ctx, v.callFor(c), p, to)
}
func (v *View) Request(ctx context.Context, c s.Call, r s.HTTPRequest) (s.HTTPResponse, error) {
	return v.box.Request(ctx, v.callFor(c), r)
}
func (v *View) RequestResource(ctx context.Context, id, description string, a s.Access) (s.ResourceRequest, error) {
	return v.box.RequestResource(ctx, id, description, a)
}

var _ s.Files = (*View)(nil)

func diffCost(before, after map[string]node) int64 {
	cost := int64(0)
	for p, n := range before {
		if v, ok := after[p]; !ok || v != n {
			cost += int64(len(p)*3 + 512)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			cost += int64(len(p)*3 + 512)
		}
	}
	return cost
}
func (b *Sandbox) fileChanges(before, after map[string]node) []s.FileChange {
	out := []s.FileChange{}
	for p, n := range before {
		v, ok := after[p]
		if !ok || v != n {
			old := b.entry(p, n)
			c := s.FileChange{Path: p, Before: &old}
			if ok {
				next := b.entry(p, v)
				c.After = &next
			}
			out = append(out, c)
		}
	}
	for p, n := range after {
		if _, ok := before[p]; !ok {
			next := b.entry(p, n)
			out = append(out, s.FileChange{Path: p, After: &next})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// CompareAndWrite 在同一修改临界区验证现有内容，防止读后覆盖其他修改。
// CompareAndWrite checks existing content within the mutation critical section to prevent lost updates.
func (b *Sandbox) CompareAndWrite(ctx context.Context, c s.Call, p, expected string, data []byte) (s.Result, error) {
	if expected == "" {
		return s.Result{}, s.ErrConflict
	}
	return b.mutate(ctx, c, "write", p, "", data, expected)
}
func (v *View) CompareAndWrite(ctx context.Context, c s.Call, p, expected string, data []byte) (s.Result, error) {
	return v.box.CompareAndWrite(ctx, v.callFor(c), p, expected, data)
}
