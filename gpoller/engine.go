// Package gpoller 提供网络事件运行时原语：readiness 事件引擎、completion 引擎、
// 时间轮与环形缓冲。本包属于基础契约层·运行时原语组，仅依赖标准库与
// x/sys，不得 import 任何 gk 包。
//
// Package gpoller provides network event runtime primitives: a readiness event
// engine, a completion engine, a timing wheel, and a ring buffer. It belongs to
// the runtime-primitive group of the base contract layer: stdlib and x/sys only,
// no gk imports allowed.
package gpoller

import (
	"errors"
	"time"
)

// 引擎相关错误。
// Engine-related errors.
var (
	// ErrNotSupported 表示当前平台不提供该后端。
	ErrNotSupported = errors.New("gpoller: not supported on this platform")
	// ErrClosed 表示引擎已关闭，不再接受注册。
	ErrClosed = errors.New("gpoller: engine closed")
	// ErrNotRegistered 表示 fd 尚未注册到引擎。
	ErrNotRegistered = errors.New("gpoller: fd not registered")
)

// Event 描述一次 fd 就绪事件。
// Event describes one fd readiness event.
type Event struct {
	FD       int
	Readable bool
	Writable bool
	Hup      bool
	Err      error
}

// Handler 处理就绪事件。回调在 Poll 所在 goroutine 内串行执行，
// 必须快速返回；阻塞工作应通过 Wake 投递。
//
// Handler handles a readiness event. Callbacks run serially on the Poll
// goroutine and must return quickly; blocking work should be posted via Wake.
type Handler func(Event)

// EventEngine 是 readiness 事件引擎：注册 fd 的读写兴趣并分发就绪事件。
// 一个引擎对应一个 Poll 循环 goroutine。实现须保证各方法可跨 goroutine 调用。
//
// EventEngine is the readiness event engine: it registers read/write interests
// for fds and dispatches readiness events. One engine maps to one Poll loop
// goroutine. Implementations must be safe for cross-goroutine method calls.
type EventEngine interface {
	// AddRead 注册 fd 的读兴趣；fd 会被设为非阻塞。
	AddRead(fd int, h Handler) error
	// AddWrite 注册 fd 的写兴趣；fd 会被设为非阻塞。
	AddWrite(fd int, h Handler) error
	// Mod 调整 fd 的兴趣集合。
	Mod(fd int, read, write bool) error
	// Delete 注销 fd 的全部兴趣。
	Delete(fd int) error
	// Poll 阻塞循环分发事件，直至 Close 后退出并返回 nil。
	Poll() error
	// Wake 跨 goroutine 投递任务：fn 将在 Poll goroutine 上执行。
	Wake(fn func()) error
	// Close 请求停止 Poll 循环；实际 fd 资源在 Poll 退出后释放。
	Close() error
}

// OpKind 表示 completion 操作类型。
// OpKind is the kind of a completion operation.
type OpKind int

// completion 操作类型。
// Completion operation kinds.
const (
	OpRead OpKind = iota
	OpWrite
	OpAccept
	OpConnect
	OpTimeout
)

// Op 描述一次异步操作提交。
// Op describes one submitted async operation.
type Op struct {
	Kind OpKind
	FD   int
	Buf  []byte
}

// Result 是异步操作的完成结果。
// Result is the completion result of an async operation.
type Result struct {
	N   int
	Err error
}

// CompletionEngine 是 completion 引擎（io_uring 旁路，M5 落地）。
// 与 EventEngine 并行演进，互不依赖。
//
// CompletionEngine is the completion engine (io_uring sidecar, planned in M5).
// It evolves in parallel with EventEngine and neither depends on the other.
type CompletionEngine interface {
	Submit(op Op, cb func(Result)) (uint64, error)
	Cancel(id uint64) error
	Close() error
}

// EngineOption 配置事件引擎。
// EngineOption configures an event engine.
type EngineOption func(*engineConfig)

// engineConfig 是引擎共享配置。
// engineConfig holds shared engine configuration.
type engineConfig struct {
	// pollInterval 控制 Poll 的等待模式：<0 阻塞等待（默认，真实硬件上
	// 唤醒为 µs 级）；0 busy-poll（空闲时 Gosched，牺牲空闲 CPU 换取
	// 最低唤醒延迟，适用于虚拟化等内核线程唤醒昂贵的环境）；>0 定时轮询。
	pollInterval time.Duration
}

// WithPollInterval 设置 Poll 等待模式（见 engineConfig.pollInterval）。
// WithPollInterval sets the Poll wait mode (see engineConfig.pollInterval).
func WithPollInterval(d time.Duration) EngineOption {
	return func(c *engineConfig) { c.pollInterval = d }
}

// entry 是单个 fd 的注册项；读写兴趣与处理器独立。
// entry is one registered fd; read and write interests/handlers are independent.
type entry struct {
	fd     int
	read   bool
	write  bool
	readH  Handler
	writeH Handler
}

// dispatch 按事件类型分发到对应处理器。
// dispatch routes the event to the matching handlers.
func (e *entry) dispatch(ev Event) {
	if ev.Hup || ev.Err != nil {
		ev.Readable, ev.Writable = true, true
	}
	if ev.Readable && e.readH != nil {
		e.readH(ev)
	}
	if ev.Writable && e.writeH != nil {
		e.writeH(ev)
	}
}
