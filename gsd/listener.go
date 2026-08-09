package gsd

import (
	"sort"
	"strings"
	"sync"
)

// UpdateListener 接收服务快照更新。
// UpdateListener receives service snapshot updates.
// 模式源自 linkerd2 的 destination listener（fallback/dedup）：监听器是可组合的装饰器，
// 每层在转发前可记忆状态、去重或合并。
// Pattern borrowed from linkerd2's destination listeners: listeners are composable
// decorators that may remember state, deduplicate, or merge before forwarding.
type UpdateListener interface {
	// Update 推送服务快照；实现必须可被并发调用。
	// Update pushes a service snapshot; implementations must be concurrency-safe.
	Update(instances []ServiceInfo)
}

// UpdateFunc 把函数适配为 UpdateListener。
// UpdateFunc adapts a function to an UpdateListener.
type UpdateFunc func([]ServiceInfo)

// Update 实现 UpdateListener 接口。
// Update implements the UpdateListener interface.
func (f UpdateFunc) Update(instances []ServiceInfo) {
	f(instances)
}

// snapshotSignature 生成快照的稳定签名（按实例地址排序拼接）。
// snapshotSignature builds a stable signature for a snapshot (sorted addresses joined).
// 相同地址集合的不同顺序产生相同签名，用于快照去重。
// different orderings of the same address set yield the same signature, used for dedup.
func snapshotSignature(instances []ServiceInfo) string {
	if len(instances) == 0 {
		return ""
	}
	addrs := make([]string, 0, len(instances))
	for _, in := range instances {
		addrs = append(addrs, in.Address)
	}
	sort.Strings(addrs)
	return strings.Join(addrs, "|")
}

// DedupListener 跳过与前一次相同的快照。
// DedupListener skips snapshots identical to the previous one.
// 服务端 watch 抖动或事件风暴会触发全量 Discover 并重复回调（见 etcd Watch），
// 本层在此过滤冗余通知。
// watch jitter or event storms trigger full Discover calls and duplicate callbacks
// (see etcd Watch); this layer filters redundant notifications.
type DedupListener struct {
	next        UpdateListener
	initialized bool
	last        string
	mu          sync.Mutex
}

// NewDedupListener 创建去重监听器。
// NewDedupListener creates a deduplicating listener.
func NewDedupListener(next UpdateListener) *DedupListener {
	return &DedupListener{next: next}
}

// Update 实现 UpdateListener 接口；与前一次签名相同的快照被丢弃。
// Update implements UpdateListener; snapshots with the same signature are dropped.
func (d *DedupListener) Update(instances []ServiceInfo) {
	sig := snapshotSignature(instances)
	d.mu.Lock()
	if d.initialized && sig == d.last {
		d.mu.Unlock()
		return
	}
	d.initialized = true
	d.last = sig
	d.mu.Unlock()
	d.next.Update(instances)
}

// FallbackListener 合并主备两个快照源：主源有可用实例时用主源，否则回退备源。
// FallbackListener merges primary and backup snapshot sources: primary wins while it
// yields available instances, otherwise the backup is used.
// 语义与 linkerd2 的 fallbackProfileListener 一致：
// - 两个源都完成首次更新后才向父监听器发布；
// - 主源快照非空时发布主源；
// - 主源为空而备源非空时发布备源；
// - 两源都为空才发布空快照。
// Semantics match linkerd2's fallbackProfileListener:
// - publish only after both sources initialized;
// - publish primary when its snapshot is non-empty;
// - publish backup when primary is empty but backup is not;
// - publish an empty snapshot only when both are empty.
type FallbackListener struct {
	next    UpdateListener
	primary *fallbackChild
	backup  *fallbackChild
	mu      sync.Mutex
}

type fallbackChild struct {
	parent      *FallbackListener
	initialized bool
	instances   []ServiceInfo
}

// NewFallbackListener 创建主备双源监听器，返回 primary 与 backup 两个入口。
// NewFallbackListener creates a primary/backup listener; returns the two entry points.
// 两个入口分别接入不同发现源（如主/备 etcd 或 etcd/本地缓存）。
// wire each entry to a different discovery source (e.g. primary/backup etcd or etcd/cache).
func NewFallbackListener(next UpdateListener) (primary, backup UpdateListener) {
	f := &FallbackListener{next: next}
	f.primary = &fallbackChild{parent: f}
	f.backup = &fallbackChild{parent: f}
	return f.primary, f.backup
}

// Update 实现 UpdateListener 接口；任一源更新后重新选择发布。
// Update implements UpdateListener; republishes after either source updates.
func (c *fallbackChild) Update(instances []ServiceInfo) {
	c.parent.mu.Lock()
	defer c.parent.mu.Unlock()
	c.instances = append(c.instances[:0], instances...)
	c.initialized = true
	c.parent.publishLocked()
}

func (f *FallbackListener) publishLocked() {
	// 两源都完成首次更新前不发布，避免备源尚未就绪时的过早空快照。
	// do not publish before both sources initialized to avoid premature empty snapshots.
	if !f.primary.initialized || !f.backup.initialized {
		return
	}
	if len(f.primary.instances) == 0 && len(f.backup.instances) > 0 {
		f.next.Update(f.backup.instances)
		return
	}
	f.next.Update(f.primary.instances)
}
