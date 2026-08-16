//go:build linux

package gpoller

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// epollEngine 是 Linux epoll 后端：边沿触发 + EPOLLRDHUP，
// 跨 goroutine 唤醒用 eventfd 等价物（pipe2）。
//
// epollEngine is the Linux epoll backend: edge-triggered with EPOLLRDHUP,
// cross-goroutine wakeups use a pipe2 self-pipe.
type epollEngine struct {
	epfd  int
	wakeR int
	wakeW int

	pollInterval time.Duration

	mu      sync.Mutex
	entries map[int]*entry
	tasks   []func()
	closed  bool

	polling atomic.Bool
}

// NewEngine 创建 Linux epoll 引擎。
// NewEngine creates the Linux epoll engine.
func NewEngine(opts ...EngineOption) (EventEngine, error) {
	cfg := &engineConfig{pollInterval: -1}
	for _, o := range opts {
		o(cfg)
	}
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	var fds [2]int
	if err := unix.Pipe2(fds[:], unix.O_NONBLOCK|unix.O_CLOEXEC); err != nil {
		unix.Close(epfd)
		return nil, err
	}
	e := &epollEngine{epfd: epfd, wakeR: fds[0], wakeW: fds[1], pollInterval: cfg.pollInterval, entries: make(map[int]*entry)}
	ent := &entry{fd: e.wakeR, read: true, readH: e.onWake}
	e.entries[e.wakeR] = ent
	evt := unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(e.wakeR)}
	if err := unix.EpollCtl(e.epfd, unix.EPOLL_CTL_ADD, e.wakeR, &evt); err != nil {
		unix.Close(epfd)
		unix.Close(e.wakeR)
		unix.Close(e.wakeW)
		return nil, err
	}
	return e, nil
}

// AddRead 注册 fd 读兴趣。
// AddRead registers read interest for fd.
func (e *epollEngine) AddRead(fd int, h Handler) error {
	return e.add(fd, true, false, h)
}

// AddWrite 注册 fd 写兴趣。
// AddWrite registers write interest for fd.
func (e *epollEngine) AddWrite(fd int, h Handler) error {
	return e.add(fd, false, true, h)
}

// add 注册或追加 fd 的兴趣与处理器。
// add registers or extends interest and handlers for fd.
func (e *epollEngine) add(fd int, read, write bool, h Handler) error {
	if err := unix.SetNonblock(fd, true); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	ent, ok := e.entries[fd]
	op := unix.EPOLL_CTL_ADD
	if !ok {
		ent = &entry{fd: fd}
		e.entries[fd] = ent
	} else {
		op = unix.EPOLL_CTL_MOD
	}
	if read {
		ent.read, ent.readH = true, h
	}
	if write {
		ent.write, ent.writeH = true, h
	}
	return e.ctl(op, ent)
}

// Mod 调整 fd 的兴趣集合。
// Mod adjusts the interest set of fd.
func (e *epollEngine) Mod(fd int, read, write bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	ent, ok := e.entries[fd]
	if !ok {
		return ErrNotRegistered
	}
	ent.read, ent.write = read, write
	return e.ctl(unix.EPOLL_CTL_MOD, ent)
}

// Delete 注销 fd 的全部兴趣。
// Delete unregisters all interests of fd.
func (e *epollEngine) Delete(fd int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.entries[fd]; !ok {
		return nil
	}
	// DEL 需要目标 fd，不能用 ctl（ctl 从 ent 取 fd，此处 ent 仍有效亦可，
	// 但 EPOLL_CTL_DEL 的事件参数被忽略，直接传 fd 最直观）。
	if err := unix.EpollCtl(e.epfd, unix.EPOLL_CTL_DEL, fd, &unix.EpollEvent{}); err != nil &&
		err != unix.ENOENT && err != unix.EBADF {
		return err
	}
	delete(e.entries, fd)
	return nil
}

// ctl 执行 epoll_ctl；ent 为 nil 表示删除。
// ctl issues epoll_ctl; a nil ent means deletion.
func (e *epollEngine) ctl(op int, ent *entry) error {
	var evt unix.EpollEvent
	if ent != nil {
		evt = unix.EpollEvent{Events: ent.events(), Fd: int32(ent.fd)}
	}
	return unix.EpollCtl(e.epfd, op, int(evt.Fd), &evt)
}

// events 计算 epoll 事件位。水平触发（LT）：每事件单次读，
// 避免 ET 模式的 EAGAIN 探测 syscall；高吞吐场景后续可切 ET。
//
// events computes the epoll event bits. Level-triggered (LT): one read per
// event, avoiding ET's EAGAIN probe syscall; ET can be revisited for
// high-throughput paths.
func (ent *entry) events() uint32 {
	ev := uint32(unix.EPOLLRDHUP)
	if ent.read {
		ev |= unix.EPOLLIN
	}
	if ent.write {
		ev |= unix.EPOLLOUT
	}
	return ev
}

// Poll 阻塞循环分发事件，Close 后退出。
// Poll blocks dispatching events until Close.
func (e *epollEngine) Poll() error {
	e.polling.Store(true)
	defer e.polling.Store(false)
	defer e.closeFds()

	events := make([]unix.EpollEvent, 128)
	for {
		n, err := unix.EpollWait(e.epfd, events, e.waitMsec())
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			e.mu.Lock()
			closed := e.closed
			e.mu.Unlock()
			if closed {
				return nil
			}
			return err
		}
		if n == 0 {
			// busy-poll 模式：空闲时让出，避免独占 CPU。
			if e.pollInterval == 0 {
				runtime.Gosched()
			}
			continue
		}
		stop := false
		for i := 0; i < n; i++ {
			if e.dispatchOne(&events[i]) {
				stop = true
			}
		}
		if stop {
			return nil
		}
	}
}

// waitMsec 换算 EpollWait 超时：-1 阻塞，0 非阻塞，>0 毫秒。
// waitMsec converts the wait mode to an EpollWait timeout: -1 blocking,
// 0 nonblocking, >0 milliseconds.
func (e *epollEngine) waitMsec() int {
	switch {
	case e.pollInterval < 0:
		return -1
	case e.pollInterval == 0:
		return 0
	default:
		ms := int(e.pollInterval / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
		return ms
	}
}

// dispatchOne 分发单个 epoll 事件；返回 true 表示引擎已关闭、Poll 应退出。
// dispatchOne dispatches one epoll event; true means the engine closed and Poll
// should return.
func (e *epollEngine) dispatchOne(evt *unix.EpollEvent) bool {
	fd := int(evt.Fd)
	if fd == e.wakeR {
		e.drainWake()
		e.mu.Lock()
		closed := e.closed
		tasks := e.tasks
		e.tasks = nil
		e.mu.Unlock()
		for _, fn := range tasks {
			fn()
		}
		return closed
	}
	e.mu.Lock()
	ent := e.entries[fd]
	e.mu.Unlock()
	if ent == nil {
		return false
	}
	flags := evt.Events
	ev := Event{
		FD:       fd,
		Readable: flags&(unix.EPOLLIN|unix.EPOLLHUP|unix.EPOLLERR|unix.EPOLLRDHUP) != 0,
		Writable: flags&unix.EPOLLOUT != 0,
		Hup:      flags&(unix.EPOLLHUP|unix.EPOLLERR|unix.EPOLLRDHUP) != 0,
	}
	ent.dispatch(ev)
	return false
}

// onWake 是自管道读端的事件处理器：由 dispatchOne 内联处理，
// 此回调仅用于注册占位，防止 entry 为空。
//
// onWake is a placeholder handler for the self-pipe; the wake fd is handled
// inline in dispatchOne, this keeps the entry non-nil.
func (e *epollEngine) onWake(ev Event) {}

// drainWake 排空自管道中待处理的唤醒字节。
// drainWake drains pending wake bytes from the self-pipe.
func (e *epollEngine) drainWake() {
	buf := make([]byte, 64)
	for {
		if _, err := unix.Read(e.wakeR, buf); err == unix.EAGAIN {
			return
		}
	}
}

// Wake 投递任务到 Poll goroutine。
// Wake posts a task to the Poll goroutine.
func (e *epollEngine) Wake(fn func()) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	e.tasks = append(e.tasks, fn)
	e.mu.Unlock()
	// 唤醒字节写满时 EAGAIN 即可：已有待处理字节会触发 Poll。
	// EAGAIN on a full pipe is fine: a pending byte already triggers Poll.
	_, err := unix.Write(e.wakeW, []byte{1})
	if err != nil && err != unix.EAGAIN {
		return err
	}
	return nil
}

// Close 请求停止 Poll；fd 资源在 Poll 退出后释放。
// Close requests Poll to stop; fds are released after Poll exits.
func (e *epollEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()
	_, _ = unix.Write(e.wakeW, []byte{1})
	if !e.polling.Load() {
		e.closeFds()
	}
	return nil
}

// closeFds 释放 epoll fd 与自管道；须在 Poll 退出后调用。
// closeFds releases the epoll fd and self-pipe; must be called after Poll exits.
func (e *epollEngine) closeFds() {
	unix.Close(e.epfd)
	unix.Close(e.wakeR)
	unix.Close(e.wakeW)
}
