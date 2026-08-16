//go:build linux

package gpoller

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// 本文件是 io_uring CompletionEngine 的纯 Go 实现：raw syscall
// （setup/enter）+ 共享 mmap 环形（SQ/CQ/SQE），无 cgo、无 liburing。
// 参照 iceber/iouring-go 的内存模型实践（普通读写 + KeepAlive，
// x86 强序下安全）。内核 <5.6 或受限环境（seccomp）下
// NewCompletionEngine 返回错误，调用方回退 epoll。
//
// This file is a pure-Go io_uring CompletionEngine: raw syscalls
// (setup/enter) plus shared mmap rings (SQ/CQ/SQE), no cgo, no liburing.
// Memory-model practice follows iceber/iouring-go (plain accesses with
// KeepAlive; safe on strongly-ordered x86). On kernels <5.6 or restricted
// environments (seccomp) NewCompletionEngine returns an error and callers
// fall back to epoll.

// io_uring 操作码与参数常量（linux/io_uring.h）。
// io_uring opcodes and parameter constants (linux/io_uring.h).
const (
	uringOpNop         = 0
	uringOpAsyncCancel = 14
	uringOpRead        = 22
	uringOpWrite       = 23

	uringEnterGetEvents = 1 << 0

	uringOffSQRing = 0
	uringOffCQRing = 0x8000000
	uringOffSQEs   = 0x10000000
)

// uringParams 对应 io_uring_params（前缀 32 字节）。
// uringParams mirrors io_uring_params (first 32 bytes).
type uringParams struct {
	SqEntries    uint32
	CqEntries    uint32
	Flags        uint32
	SqThreadCPU  uint32
	SqThreadIdle uint32
	Features     uint32
	WqFd         uint32
	Resv         [3]uint32
	// SqOff/CqOff 按原始字节保留 io_sqring_offsets（40B）与
	// io_cqring_offsets（32B）的布局。
	SqOffRaw [40]byte
	CqOffRaw [32]byte
}

// uringSQE 对应 io_uring_sqe（64 字节，8 对齐）。
// uringSQE mirrors io_uring_sqe (64 bytes, 8-aligned).
type uringSQE struct {
	Opcode      uint8
	Flags       uint8
	Ioprio      uint16
	Fd          int32
	Off         uint64
	Addr        uint64
	Len         uint32
	RwFlags     uint32
	UserData    uint64
	BufIndex    uint16
	Personality uint16
	SpliceFdIn  int32
	Pad         [4]byte
}

// uringCQE 对应 io_uring_cqe（16 字节）。
// uringCQE mirrors io_uring_cqe (16 bytes).
type uringCQE struct {
	UserData uint64
	Res      int32
	Flags    uint32
}

// uringEngine 是 io_uring CompletionEngine 实现。
// uringEngine is the io_uring CompletionEngine implementation.
type uringEngine struct {
	fd int

	sqHead    *uint32
	sqTail    *uint32
	sqMask    *uint32
	sqArray   *uint32
	cqHead    *uint32
	cqTail    *uint32
	cqMask    *uint32
	cqEntries uint32
	cqes      *uringCQE
	sqes      *uringSQE
	sqEntries uint32

	sqRing []byte
	cqRing []byte
	sqesM  []byte

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]*uringPending

	closed chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

// uringPending 是一次未完成的操作。
// uringPending is one outstanding operation.
type uringPending struct {
	cb func(Result)
	fd int
}

// NewCompletionEngine 创建 io_uring 引擎；内核不支持时返回错误
// （调用方回退 epoll）。
//
// NewCompletionEngine creates the io_uring engine; returns an error when the
// kernel lacks support (callers fall back to epoll).
func NewCompletionEngine() (CompletionEngine, error) {
	params := &uringParams{}
	fd, _, errno := syscall.Syscall6(unix.SYS_IO_URING_SETUP, 128, uintptr(unsafe.Pointer(params)), 0, 0, 0, 0)
	if errno != 0 {
		return nil, fmt.Errorf("gpoller: io_uring_setup: %w", errno)
	}
	e := &uringEngine{fd: int(fd), pending: make(map[uint64]*uringPending), closed: make(chan struct{})}
	efd := int(fd)

	// 解析偏移。
	sqOff := func(i int) uint32 { return *(*uint32)(unsafe.Pointer(&params.SqOffRaw[i*4])) }
	cqOff := func(i int) uint32 { return *(*uint32)(unsafe.Pointer(&params.CqOffRaw[i*4])) }

	sqRingSize := sqOff(6) + params.SqEntries*4 // array 偏移 + 索引数组
	cqRingSize := cqOff(5) + params.CqEntries*16
	sqesSize := params.SqEntries * 64

	var err error
	if e.sqRing, err = mmapRing(efd, uringOffSQRing, int(sqRingSize)); err != nil {
		unix.Close(efd)
		return nil, err
	}
	if e.cqRing, err = mmapRing(efd, uringOffCQRing, int(cqRingSize)); err != nil {
		unix.Close(efd)
		return nil, err
	}
	if e.sqesM, err = mmapRing(efd, uringOffSQEs, int(sqesSize)); err != nil {
		unix.Close(efd)
		return nil, err
	}

	e.sqHead = (*uint32)(unsafe.Pointer(&e.sqRing[sqOff(0)]))
	e.sqTail = (*uint32)(unsafe.Pointer(&e.sqRing[sqOff(1)]))
	e.sqMask = (*uint32)(unsafe.Pointer(&e.sqRing[sqOff(2)]))
	e.sqArray = (*uint32)(unsafe.Pointer(&e.sqRing[sqOff(6)]))
	e.cqHead = (*uint32)(unsafe.Pointer(&e.cqRing[cqOff(0)]))
	e.cqTail = (*uint32)(unsafe.Pointer(&e.cqRing[cqOff(1)]))
	e.cqMask = (*uint32)(unsafe.Pointer(&e.cqRing[cqOff(2)]))
	e.cqEntries = params.CqEntries
	e.cqes = (*uringCQE)(unsafe.Pointer(&e.cqRing[cqOff(5)]))
	e.sqes = (*uringSQE)(unsafe.Pointer(&e.sqesM[0]))
	e.sqEntries = params.SqEntries

	e.wg.Add(1)
	go e.completionLoop()
	return e, nil
}

// mmapRing 映射 io_uring 环形区。
// mmapRing maps an io_uring ring region.
func mmapRing(fd, offset, length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("gpoller: zero-length ring")
	}
	b, err := unix.Mmap(fd, int64(offset), length, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		return nil, fmt.Errorf("gpoller: mmap io_uring ring: %w", err)
	}
	return b, nil
}

// Submit 提交一次异步操作；返回操作 id。Buf 在完成回调前必须保持有效。
// cb 在完成循环 goroutine 上串行执行，必须快速返回。
//
// Submit submits one async operation and returns its id. Buf must stay valid
// until the completion callback. cb runs serially on the completion loop
// goroutine and must return quickly.
func (e *uringEngine) Submit(op Op, cb func(Result)) (uint64, error) {
	select {
	case <-e.closed:
		return 0, ErrClosed
	default:
	}
	var (
		code uint8
		ok   bool
	)
	switch op.Kind {
	case OpRead:
		code, ok = uringOpRead, true
	case OpWrite:
		code, ok = uringOpWrite, true
	case OpTimeout:
		// 简化：NOP 立即完成（超时语义由调用方处理）。
		code, ok = uringOpNop, true
	default:
		return 0, fmt.Errorf("gpoller: uring op kind %d not supported", op.Kind)
	}
	if !ok {
		return 0, ErrNotSupported
	}

	e.mu.Lock()
	if e.pending == nil {
		e.mu.Unlock()
		return 0, ErrClosed
	}
	e.nextID++
	id := e.nextID
	if cb != nil {
		e.pending[id] = &uringPending{cb: cb, fd: op.FD}
	}
	tail := *e.sqTail
	idx := tail & *e.sqMask
	sqe := (*uringSQE)(unsafe.Pointer(uintptr(unsafe.Pointer(e.sqes)) + uintptr(idx)*64))
	sqe.Opcode = code
	sqe.Flags = 0
	sqe.Ioprio = 0
	sqe.Fd = int32(op.FD)
	sqe.Off = 0
	var addr uintptr
	if len(op.Buf) > 0 {
		addr = uintptr(unsafe.Pointer(&op.Buf[0]))
	}
	sqe.Addr = uint64(addr)
	sqe.Len = uint32(len(op.Buf))
	sqe.RwFlags = 0
	sqe.UserData = id
	*(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(e.sqArray)) + uintptr(idx)*4)) = idx
	// 写序：SQE → array → tail（发布）。
	*(*uint32)(unsafe.Pointer(e.sqTail)) = tail + 1
	runtime.KeepAlive(op.Buf)
	e.mu.Unlock()

	// 触发提交。
	_, _, errno := syscall.Syscall6(unix.SYS_IO_URING_ENTER, uintptr(e.fd), 1, 0, 0, 0, 0)
	if errno != 0 && errno != syscall.EAGAIN && errno != syscall.EBUSY {
		e.mu.Lock()
		delete(e.pending, id)
		e.mu.Unlock()
		return 0, fmt.Errorf("gpoller: io_uring_enter: %w", errno)
	}
	return id, nil
}

// Cancel 取消一次未完成的操作（cancel 本身也是异步的）。
// Cancel cancels an outstanding operation (the cancel itself is async).
func (e *uringEngine) Cancel(id uint64) error {
	e.mu.Lock()
	p, ok := e.pending[id]
	tail := *e.sqTail
	idx := tail & *e.sqMask
	sqe := (*uringSQE)(unsafe.Pointer(uintptr(unsafe.Pointer(e.sqes)) + uintptr(idx)*64))
	sqe.Opcode = uringOpAsyncCancel
	sqe.Fd = -1
	if ok {
		sqe.Fd = int32(p.fd)
	}
	sqe.Addr = id
	sqe.UserData = 0 // cancel 本身不等待回调
	*(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(e.sqArray)) + uintptr(idx)*4)) = idx
	*(*uint32)(unsafe.Pointer(e.sqTail)) = tail + 1
	e.mu.Unlock()

	_, _, errno := syscall.Syscall6(unix.SYS_IO_URING_ENTER, uintptr(e.fd), 1, 0, 0, 0, 0)
	if errno != 0 && errno != syscall.EAGAIN && errno != syscall.EBUSY {
		return fmt.Errorf("gpoller: io_uring_enter(cancel): %w", errno)
	}
	return nil
}

// Close 停止完成循环并释放映射。
// Close stops the completion loop and releases the mappings.
func (e *uringEngine) Close() error {
	e.once.Do(func() {
		close(e.closed)
		e.wakeNop()
	})
	e.wg.Wait()
	e.mu.Lock()
	e.pending = nil
	e.mu.Unlock()
	unix.Close(e.fd)
	_ = unix.Munmap(e.sqRing)
	_ = unix.Munmap(e.cqRing)
	_ = unix.Munmap(e.sqesM)
	return nil
}

// completionLoop 阻塞等待完成并分发回调。
// completionLoop blocks for completions and dispatches callbacks.
func (e *uringEngine) completionLoop() {
	defer e.wg.Done()
	for {
		select {
		case <-e.closed:
			return
		default:
		}
		_, _, errno := syscall.Syscall6(unix.SYS_IO_URING_ENTER, uintptr(e.fd), 0, 1, uringEnterGetEvents, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			select {
			case <-e.closed:
				return
			default:
			}
			continue
		}
		e.drainCompletions()
	}
}

// drainCompletions 消费 CQ 中的全部完成项。
// drainCompletions consumes all pending CQ completions.
func (e *uringEngine) drainCompletions() {
	head := *e.cqHead
	tail := *e.cqTail
	for head != tail {
		cqe := (*uringCQE)(unsafe.Pointer(uintptr(unsafe.Pointer(e.cqes)) + uintptr(head&*e.cqMask)*16))
		id := cqe.UserData
		res := cqe.Res

		e.mu.Lock()
		p := e.pending[id]
		if p != nil && id != 0 {
			delete(e.pending, id)
		}
		e.mu.Unlock()
		if p != nil && p.cb != nil {
			p.cb(Result{N: int(res), Err: uringErr(int(res))})
		}

		*(*uint32)(unsafe.Pointer(e.cqHead)) = head + 1
		head++
	}
}

// wakeNop 提交一个 NOP 唤醒阻塞中的完成循环。
// wakeNop submits a NOP to wake the blocked completion loop.
func (e *uringEngine) wakeNop() {
	e.mu.Lock()
	tail := *e.sqTail
	idx := tail & *e.sqMask
	sqe := (*uringSQE)(unsafe.Pointer(uintptr(unsafe.Pointer(e.sqes)) + uintptr(idx)*64))
	sqe.Opcode = uringOpNop
	sqe.UserData = 0
	*(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(e.sqArray)) + uintptr(idx)*4)) = idx
	*(*uint32)(unsafe.Pointer(e.sqTail)) = tail + 1
	e.mu.Unlock()
	_, _, _ = syscall.Syscall6(unix.SYS_IO_URING_ENTER, uintptr(e.fd), 1, 0, 0, 0, 0)
}

// uringErr 把完成码转换为错误（负数 errno）。
// uringErr converts a completion code into an error (negative errno).
func uringErr(res int) error {
	if res >= 0 {
		return nil
	}
	return syscall.Errno(-res)
}
