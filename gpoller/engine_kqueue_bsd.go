//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package gpoller

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// kqueueEngine 是 BSD/macOS kqueue 后端：EV_CLEAR 边沿语义，
// 跨 goroutine 唤醒用自管道（syscall.Pipe）。
//
// kqueueEngine is the BSD/macOS kqueue backend with EV_CLEAR edge semantics;
// cross-goroutine wakeups use a self-pipe (syscall.Pipe).
type kqueueEngine struct {
	kq    int
	wakeR int
	wakeW int

	pollInterval time.Duration

	mu      sync.Mutex
	entries map[int]*entry
	tasks   []func()
	closed  bool

	polling atomic.Bool
}

// NewEngine 创建 kqueue 引擎。
// NewEngine creates the kqueue engine.
func NewEngine(opts ...EngineOption) (EventEngine, error) {
	cfg := &engineConfig{pollInterval: -1}
	for _, o := range opts {
		o(cfg)
	}
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		unix.Close(kq)
		return nil, err
	}
	unix.SetNonblock(fds[0], true)
	unix.SetNonblock(fds[1], true)
	e := &kqueueEngine{kq: kq, wakeR: fds[0], wakeW: fds[1], pollInterval: cfg.pollInterval, entries: make(map[int]*entry)}
	ent := &entry{fd: e.wakeR, read: true, readH: e.onWake}
	e.entries[e.wakeR] = ent
	if err := e.change(e.wakeR, unix.EVFILT_READ, unix.EV_ADD|unix.EV_ENABLE|unix.EV_CLEAR); err != nil {
		unix.Close(kq)
		unix.Close(e.wakeR)
		unix.Close(e.wakeW)
		return nil, err
	}
	return e, nil
}

// AddRead 注册 fd 读兴趣。
// AddRead registers read interest for fd.
func (e *kqueueEngine) AddRead(fd int, h Handler) error {
	return e.add(fd, true, false, h)
}

// AddWrite 注册 fd 写兴趣。
// AddWrite registers write interest for fd.
func (e *kqueueEngine) AddWrite(fd int, h Handler) error {
	return e.add(fd, false, true, h)
}

// add 注册或追加 fd 的兴趣与处理器。
// add registers or extends interest and handlers for fd.
func (e *kqueueEngine) add(fd int, read, write bool, h Handler) error {
	if err := unix.SetNonblock(fd, true); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	ent, ok := e.entries[fd]
	if !ok {
		ent = &entry{fd: fd}
		e.entries[fd] = ent
	}
	flags := unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR
	if read && !ent.read {
		if err := e.change(fd, unix.EVFILT_READ, flags); err != nil {
			return err
		}
	}
	if write && !ent.write {
		if err := e.change(fd, unix.EVFILT_WRITE, flags); err != nil {
			return err
		}
	}
	if read {
		ent.read, ent.readH = true, h
	}
	if write {
		ent.write, ent.writeH = true, h
	}
	return nil
}

// Mod 调整 fd 的兴趣集合。
// Mod adjusts the interest set of fd.
func (e *kqueueEngine) Mod(fd int, read, write bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	ent, ok := e.entries[fd]
	if !ok {
		return ErrNotRegistered
	}
	flags := unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR
	if ent.read && !read {
		if err := e.change(fd, unix.EVFILT_READ, unix.EV_DELETE); err != nil {
			return err
		}
	}
	if read && !ent.read {
		if err := e.change(fd, unix.EVFILT_READ, flags); err != nil {
			return err
		}
	}
	if ent.write && !write {
		if err := e.change(fd, unix.EVFILT_WRITE, unix.EV_DELETE); err != nil {
			return err
		}
	}
	if write && !ent.write {
		if err := e.change(fd, unix.EVFILT_WRITE, flags); err != nil {
			return err
		}
	}
	ent.read, ent.write = read, write
	return nil
}

// Delete 注销 fd 的全部兴趣。
// Delete unregisters all interests of fd.
func (e *kqueueEngine) Delete(fd int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ent, ok := e.entries[fd]
	if !ok {
		return nil
	}
	if ent.read {
		_ = e.change(fd, unix.EVFILT_READ, unix.EV_DELETE)
	}
	if ent.write {
		_ = e.change(fd, unix.EVFILT_WRITE, unix.EV_DELETE)
	}
	delete(e.entries, fd)
	return nil
}

// change 提交一条 kevent 变更。
// change submits one kevent change.
func (e *kqueueEngine) change(fd int, filter, flags int) error {
	ev := unix.Kevent_t{}
	unix.SetKevent(&ev, fd, filter, flags)
	_, err := unix.Kevent(e.kq, []unix.Kevent_t{ev}, nil, nil)
	return err
}

// Poll 阻塞循环分发事件，Close 后退出。
// Poll blocks dispatching events until Close.
func (e *kqueueEngine) Poll() error {
	e.polling.Store(true)
	defer e.polling.Store(false)
	defer e.closeFds()

	events := make([]unix.Kevent_t, 128)
	for {
		n, err := unix.Kevent(e.kq, nil, events, e.waitTimeout())
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

// waitTimeout 换算 Kevent 超时：nil 阻塞，零值非阻塞，>0 定时。
// waitTimeout converts the wait mode to a Kevent timeout: nil blocking, zero
// nonblocking, >0 timed.
func (e *kqueueEngine) waitTimeout() *unix.Timespec {
	switch {
	case e.pollInterval < 0:
		return nil
	case e.pollInterval == 0:
		ts := unix.Timespec{}
		return &ts
	default:
		ts := unix.NsecToTimespec(e.pollInterval.Nanoseconds())
		return &ts
	}
}

// dispatchOne 分发单个 kevent；返回 true 表示引擎已关闭、Poll 应退出。
// dispatchOne dispatches one kevent; true means the engine closed and Poll
// should return.
func (e *kqueueEngine) dispatchOne(ev *unix.Kevent_t) bool {
	fd := int(ev.Ident)
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
	readable := ev.Filter == unix.EVFILT_READ
	writable := ev.Filter == unix.EVFILT_WRITE
	hup := ev.Flags&(unix.EV_EOF|unix.EV_ERROR) != 0
	ent.dispatch(Event{FD: fd, Readable: readable, Writable: writable, Hup: hup})
	return false
}

// onWake 是自管道读端占位处理器，实际处理内联在 dispatchOne。
// onWake is a placeholder handler for the self-pipe, handled inline in
// dispatchOne.
func (e *kqueueEngine) onWake(ev Event) {}

// drainWake 排空自管道中待处理的唤醒字节。
// drainWake drains pending wake bytes from the self-pipe.
func (e *kqueueEngine) drainWake() {
	buf := make([]byte, 64)
	for {
		if _, err := unix.Read(e.wakeR, buf); err == unix.EAGAIN {
			return
		}
	}
}

// Wake 投递任务到 Poll goroutine。
// Wake posts a task to the Poll goroutine.
func (e *kqueueEngine) Wake(fn func()) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	e.tasks = append(e.tasks, fn)
	e.mu.Unlock()
	_, err := unix.Write(e.wakeW, []byte{1})
	if err != nil && err != unix.EAGAIN {
		return err
	}
	return nil
}

// Close 请求停止 Poll；fd 资源在 Poll 退出后释放。
// Close requests Poll to stop; fds are released after Poll exits.
func (e *kqueueEngine) Close() error {
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

// closeFds 释放 kqueue fd 与自管道；须在 Poll 退出后调用。
// closeFds releases the kqueue fd and self-pipe; must be called after Poll
// exits.
func (e *kqueueEngine) closeFds() {
	unix.Close(e.kq)
	unix.Close(e.wakeR)
	unix.Close(e.wakeW)
}
