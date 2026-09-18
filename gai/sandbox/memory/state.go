// Package memory 提供无磁盘依赖的进程内 Sandbox。
// Package memory provides an in-process Sandbox without disk dependencies.
package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	s "github.com/sofiworker/gk/gai/sandbox"
)

type Limits struct {
	Bytes      int64
	FileBytes  int64
	Entries    int
	Events     int
	Snapshots  int
	Operations int
	Requests   int
}
type config struct {
	id      string
	access  s.Access
	limits  Limits
	network s.HTTPSender
}
type Option func(*config) error

func WithID(id string) Option {
	return func(c *config) error {
		if id == "" || len(id) > 128 {
			return s.ErrPath
		}
		c.id = id
		return nil
	}
}
func WithAccess(a s.Access) Option {
	return func(c *config) error {
		if a & ^s.ReadWrite != 0 {
			return s.ErrDenied
		}
		c.access = a
		return nil
	}
}
func WithLimits(l Limits) Option {
	return func(c *config) error {
		if l.Bytes <= 0 || l.FileBytes <= 0 || l.Entries < 1 || l.Events < 1 || l.Snapshots < 1 || l.Operations < 1 || l.Requests < 1 {
			return s.ErrQuota
		}
		c.limits = l
		return nil
	}
}
func WithNetwork(n s.HTTPSender) Option {
	return func(c *config) error {
		if n == nil {
			return s.ErrUnsupported
		}
		c.network = n
		return nil
	}
}

type node struct {
	dir     bool
	content string
}
type record struct {
	fingerprint string
	result      s.Result
	err         error
}
type checkpoint struct {
	info     s.Snapshot
	tree     map[string]node
	bindings map[string]s.Binding
	epochs   map[string]uint64
}
type resource struct {
	binding  s.Binding
	baseline map[string]node
	pending  *pending
	status   s.SyncStatus
	epoch    uint64
}
type pending struct {
	batch   s.Batch
	applied int
	target  map[string]node
	unknown bool
}

// Sandbox 的管理方法只应交给可信控制层；工具使用 View。
// Sandbox management methods are for trusted controllers; tools use View.
type Sandbox struct {
	inflightBytes               int64
	mu                          sync.Mutex
	gate                        chan struct{}
	cfg                         config
	tree                        map[string]node
	blobs                       map[string][]byte
	resources                   map[string]*resource
	snapshots                   map[string]checkpoint
	records                     map[string]record
	requests                    map[string]s.ResourceRequest
	events                      []s.Event
	revision, policy, seq, next uint64
	cwd                         string
	paused                      bool
	closing, closed, destroyed  bool
	active                      int
	idle                        chan struct{}
	cancels                     map[uint64]context.CancelFunc
	pruned                      uint64
}

func New(options ...Option) (*Sandbox, error) {
	c := config{access: s.ReadWrite, limits: Limits{Bytes: 64 << 20, FileBytes: 8 << 20, Entries: 10000, Events: 10000, Snapshots: 128, Operations: 10000, Requests: 1000}}
	for _, o := range options {
		if o == nil {
			return nil, s.ErrUnsupported
		}
		if err := o(&c); err != nil {
			return nil, err
		}
	}
	if c.id == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		c.id = "memory-" + hex.EncodeToString(id[:])
	}
	b := &Sandbox{cfg: c, gate: make(chan struct{}, 1), tree: map[string]node{"/": {dir: true}}, blobs: map[string][]byte{}, resources: map[string]*resource{}, snapshots: map[string]checkpoint{}, records: map[string]record{}, requests: map[string]s.ResourceRequest{}, cwd: "/", cancels: map[uint64]context.CancelFunc{}, idle: make(chan struct{})}
	close(b.idle)
	return b, nil
}
func (b *Sandbox) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case b.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	b.mu.Lock()
	if err := ctx.Err(); err != nil {
		b.mu.Unlock()
		<-b.gate
		return err
	}
	if b.closing || b.destroyed {
		b.mu.Unlock()
		<-b.gate
		return s.ErrClosed
	}
	return nil
}
func (b *Sandbox) unlock() { b.mu.Unlock(); <-b.gate }
func (b *Sandbox) id(prefix string) string {
	b.next++
	return fmt.Sprintf("%s-%s-%d", b.cfg.id, prefix, b.next)
}
func cloneTree(t map[string]node) map[string]node {
	out := make(map[string]node, len(t))
	for p, n := range t {
		out[p] = n
	}
	return out
}
func within(p, root string) bool { return p == root || root == "/" || strings.HasPrefix(p, root+"/") }
func resolve(dir, p string) (string, error) {
	if p == "" || len(p) > 4096 || strings.ContainsRune(p, 0) || strings.Contains(p, "\\") {
		return "", s.ErrPath
	}
	if !strings.HasPrefix(p, "/") {
		p = dir + "/" + p
	}
	depth := 0
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return "", s.ErrPath
			}
		default:
			depth++
		}
	}
	clean := path.Clean(p)
	if len(clean) > 4096 {
		return "", s.ErrPath
	}
	return clean, nil
}
func (b *Sandbox) path(call s.Call, p string) (string, error) {
	if len(call.ID)+len(call.SessionID)+len(call.TurnID)+len(call.ToolCallID) > 8192 {
		return "", s.ErrQuota
	}
	if strings.HasPrefix(p, "/") {
		return resolve("/", p)
	}
	dir := call.Dir
	if dir == "" {
		dir = b.cwd
	}
	if !strings.HasPrefix(dir, "/") {
		return "", s.ErrPath
	}
	d, err := resolve("/", dir)
	if err != nil {
		return "", err
	}
	n, ok := b.tree[d]
	if !ok || !n.dir {
		return "", s.ErrPath
	}
	if err = b.allow(d, s.Read); err != nil {
		return "", err
	}
	return resolve(d, p)
}
func (b *Sandbox) allow(p string, a s.Access) error {
	access := b.cfg.access
	for _, r := range b.resources {
		if within(p, r.binding.Path) {
			access = r.binding.Access
			break
		}
	}
	if access&a != a {
		return s.ErrDenied
	}
	return nil
}
func digest(data []byte) string { d := sha256.Sum256(data); return hex.EncodeToString(d[:]) }
func (b *Sandbox) addEvent(e s.Event) {
	b.seq++
	e.Sequence = b.seq
	e.SandboxID = b.cfg.id
	e.PolicyVersion = b.policy
	e.EndedAt = time.Now().UnixMilli()
	if e.StartedAt == 0 {
		e.StartedAt = e.EndedAt
	}
	b.events = append(b.events, e)
}
func (b *Sandbox) eventSpace(n int) error {
	if len(b.events)+b.active*2+n > b.cfg.limits.Events || b.usage(b.tree, nil)+int64(b.active*2+n)*32768 > b.cfg.limits.Bytes {
		return s.ErrQuota
	}
	return nil
}
func (b *Sandbox) usage(tree map[string]node, extra map[string][]byte) int64 {
	size := b.inflightBytes
	for p := range tree {
		size += int64(len(p) + 64)
	}
	for _, t := range b.snapshots {
		for p := range t.tree {
			size += int64(len(p) + 64)
		}
	}
	for _, r := range b.resources {
		for p := range r.baseline {
			size += int64(len(p) + 64)
		}
		if r.pending != nil {
			for _, c := range r.pending.batch.Changes {
				size += int64(len(c.Path) + 128)
				if c.Before != nil {
					size += int64(len(c.Before.Data))
				}
				if c.After != nil {
					size += int64(len(c.After.Data))
				}
			}
		}
	}
	for _, v := range b.blobs {
		size += int64(len(v))
	}
	for k, v := range extra {
		if _, ok := b.blobs[k]; !ok {
			size += int64(len(v))
		}
	}
	for _, e := range b.events {
		size += int64(len(e.Path) + len(e.Destination) + len(e.Error) + len(e.OperationID) + len(e.Attribution.SessionID) + len(e.Attribution.TurnID) + len(e.Attribution.ToolCallID) + 256)
		for _, c := range e.Changes {
			size += int64(len(c.Path)*3 + 512)
		}
	}
	for k, r := range b.records {
		size += int64(len(k) + len(r.fingerprint) + 128)
	}
	for _, r := range b.requests {
		size += int64(len(r.ID) + len(r.Description) + len(r.Error) + 128)
	}
	return size
}
func (b *Sandbox) validate(tree map[string]node, extra map[string][]byte) error {
	if len(tree) > b.cfg.limits.Entries || b.usage(tree, extra) > b.cfg.limits.Bytes {
		return s.ErrQuota
	}
	return nil
}
func (b *Sandbox) entry(p string, n node) s.Entry {
	return s.Entry{Path: p, Dir: n.dir, ContentID: n.content, Size: int64(len(b.blobs[n.content]))}
}
func attribution(c s.Call) s.Attribution {
	return s.Attribution{SessionID: c.SessionID, TurnID: c.TurnID, ToolCallID: c.ToolCallID}
}
func (b *Sandbox) ID() string { return b.cfg.id }
func (b *Sandbox) SetDir(ctx context.Context, dir string) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	p, err := b.path(s.Call{Dir: "/"}, dir)
	if err != nil {
		return err
	}
	n, ok := b.tree[p]
	if !ok || !n.dir {
		return s.ErrPath
	}
	if err = b.allow(p, s.Read); err != nil {
		return err
	}
	b.cwd = p
	return nil
}
func (b *Sandbox) Events(ctx context.Context, after uint64, limit int) ([]s.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.destroyed {
		return nil, s.ErrClosed
	}
	if limit <= 0 {
		return nil, s.ErrPath
	}
	if after > b.seq {
		return nil, s.ErrConflict
	}
	if after < b.pruned {
		return nil, s.ErrCursorExpired
	}
	out := []s.Event{}
	for _, e := range b.events {
		if e.Sequence > after {
			out = append(out, cloneEvent(e))
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}
func (b *Sandbox) Content(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.destroyed {
		return nil, s.ErrClosed
	}
	v, ok := b.blobs[id]
	if !ok {
		return nil, s.ErrNotExist
	}
	return append([]byte(nil), v...), nil
}

// PruneEvents 显式丢弃历史事件，并回收不再被状态或历史引用的内容。
// PruneEvents explicitly drops history and collects content no longer referenced by state or history.
func (b *Sandbox) PruneEvents(ctx context.Context, through uint64) error {
	if err := b.lock(ctx); err != nil {
		return err
	}
	defer b.unlock()
	if b.active > 0 {
		return s.ErrBusy
	}
	i := sort.Search(len(b.events), func(i int) bool { return b.events[i].Sequence > through })
	if i > 0 {
		b.pruned = b.events[i-1].Sequence
	}
	b.events = append([]s.Event(nil), b.events[i:]...)
	b.gc()
	return nil
}
func (b *Sandbox) gc() {
	used := map[string]bool{}
	mark := func(t map[string]node) {
		for _, n := range t {
			used[n.content] = true
		}
	}
	mark(b.tree)
	for _, c := range b.snapshots {
		mark(c.tree)
	}
	for _, r := range b.resources {
		mark(r.baseline)
		if r.pending != nil {
			mark(r.pending.target)
		}
	}
	for _, e := range b.events {
		used[e.BeforeContent] = true
		used[e.AfterContent] = true
		for _, c := range e.Changes {
			if c.Before != nil {
				used[c.Before.ContentID] = true
			}
			if c.After != nil {
				used[c.After.ContentID] = true
			}
		}
	}
	for k := range b.blobs {
		if !used[k] {
			delete(b.blobs, k)
		}
	}
}

func cloneEvent(e s.Event) s.Event {
	e.Changes = append([]s.FileChange(nil), e.Changes...)
	for i, c := range e.Changes {
		if c.Before != nil {
			v := *c.Before
			e.Changes[i].Before = &v
		}
		if c.After != nil {
			v := *c.After
			e.Changes[i].After = &v
		}
	}
	return e
}
