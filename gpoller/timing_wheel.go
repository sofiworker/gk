package gpoller

import (
	"container/list"
	"sync"
	"time"
)

// 时间轮默认参数。
// Timing wheel defaults.
const (
	defaultTickDuration  = time.Millisecond
	defaultTicksPerWheel = 512
	// maxRounds 是单次可表达的延迟上限（轮转圈数），超限延迟按上限截断。
	maxRounds = 1 << 20
)

// timerNode 是时间轮中的一个定时节点。
// timerNode is one scheduled timer in the wheel.
type timerNode struct {
	id     uint64
	rounds int
	fn     func()
	cancel bool
	elem   *list.Element
}

// TimingWheel 是 Netty 风格的哈希时间轮：Add 与 Cancel 均 O(1)，
// 到期回调在轮内 goroutine 串行执行，fn 不得阻塞。
//
// TimingWheel is a Netty-style hashed wheel timer: Add and Cancel are O(1);
// callbacks run serially on the wheel goroutine and must not block.
type TimingWheel struct {
	mu      sync.Mutex
	tick    time.Duration
	ticks   int
	mask    int
	buckets []*list.List
	nodes   map[uint64]*timerNode
	nextID  uint64
	curTick uint64
	stopped bool

	ticker *time.Ticker
	stopCh chan struct{}
	once   sync.Once
}

// WheelOption 配置时间轮。
// WheelOption configures the timing wheel.
type WheelOption func(*TimingWheel)

// WithTickDuration 设置每格时长（默认 1ms）。
// WithTickDuration sets the tick duration (default 1ms).
func WithTickDuration(d time.Duration) WheelOption {
	return func(w *TimingWheel) {
		if d > 0 {
			w.tick = d
		}
	}
}

// WithTicksPerWheel 设置格数（默认 512，向上取整为 2 的幂）。
// WithTicksPerWheel sets the tick count (default 512, rounded up to a power of
// two).
func WithTicksPerWheel(n int) WheelOption {
	return func(w *TimingWheel) {
		if n > 0 {
			w.ticks = n
		}
	}
}

// NewTimingWheel 创建并启动时间轮。
// NewTimingWheel creates and starts a timing wheel.
func NewTimingWheel(opts ...WheelOption) *TimingWheel {
	w := &TimingWheel{tick: defaultTickDuration, ticks: defaultTicksPerWheel, nodes: make(map[uint64]*timerNode)}
	for _, o := range opts {
		o(w)
	}
	w.ticks = pow2(w.ticks)
	w.mask = w.ticks - 1
	w.buckets = make([]*list.List, w.ticks)
	for i := range w.buckets {
		w.buckets[i] = list.New()
	}
	w.ticker = time.NewTicker(w.tick)
	w.stopCh = make(chan struct{})
	go w.run()
	return w
}

// Add 在 delay 后执行 fn，返回定时器 id；停止后返回 0。
// Add schedules fn after delay and returns the timer id; returns 0 if stopped.
func (w *TimingWheel) Add(delay time.Duration, fn func()) uint64 {
	if fn == nil {
		return 0
	}
	if delay < w.tick {
		delay = w.tick
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return 0
	}
	ticks := uint64(delay / w.tick)
	rounds := int(ticks >> log2(w.ticks))
	if rounds > maxRounds {
		rounds = maxRounds
	}
	w.nextID++
	id := w.nextID
	idx := (w.curTick + ticks) & uint64(w.mask)
	node := &timerNode{id: id, rounds: rounds, fn: fn}
	node.elem = w.buckets[idx].PushBack(node)
	w.nodes[id] = node
	return id
}

// Cancel 取消定时器；返回是否取消成功（不存在或已取消返回 false）。
// Cancel cancels a timer; returns false if the id is unknown or already
// cancelled.
func (w *TimingWheel) Cancel(id uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	node, ok := w.nodes[id]
	if !ok || node.cancel {
		return false
	}
	node.cancel = true
	delete(w.nodes, id)
	return true
}

// Stop 停止时间轮并丢弃未触发的定时器。
// Stop halts the wheel and discards pending timers.
func (w *TimingWheel) Stop() {
	w.once.Do(func() {
		w.mu.Lock()
		w.stopped = true
		w.mu.Unlock()
		close(w.stopCh)
		w.ticker.Stop()
	})
}

// run 是时间轮推进循环。
// run advances the wheel.
func (w *TimingWheel) run() {
	for {
		select {
		case <-w.stopCh:
			return
		case <-w.ticker.C:
		}
		w.advance()
	}
}

// advance 前进一格并触发到期定时器。
// advance moves one tick and fires due timers.
func (w *TimingWheel) advance() {
	w.mu.Lock()
	w.curTick++
	bucket := w.buckets[w.curTick&uint64(w.mask)]
	var fire []func()
	for e := bucket.Front(); e != nil; {
		node := e.Value.(*timerNode)
		next := e.Next()
		switch {
		case node.cancel:
			bucket.Remove(e)
		case node.rounds > 0:
			node.rounds--
		default:
			bucket.Remove(e)
			delete(w.nodes, node.id)
			fire = append(fire, node.fn)
		}
		e = next
	}
	w.mu.Unlock()
	for _, fn := range fire {
		fn()
	}
}

// pow2 向上取整为 2 的幂。
// pow2 rounds n up to a power of two.
func pow2(n int) int {
	if n < 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// log2 返回 2 的幂 n 的对数；n 必须为 2 的幂。
// log2 returns the base-2 log of the power-of-two n.
func log2(n int) int {
	l := 0
	for n > 1 {
		n >>= 1
		l++
	}
	return l
}
