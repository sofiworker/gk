// Package gnet 是 gk 的事件驱动网络底座：基于 gpoller 的 reactor 服务器
// 与协议识别入口。M1 提供单引擎单循环的 fd 引擎模式（Linux epoll /
// BSD kqueue）；Windows 的 netpoller 接入路径见重构 spec §7（M5）。
//
// Package gnet is the gk event-driven networking foundation: a gpoller-based
// reactor server plus protocol detection entry points. M1 ships the fd-engine
// mode (Linux epoll / BSD kqueue, single engine single loop); the Windows
// netpoller path is planned in M5 (see the redesign spec, §7).
package gnet

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sofiworker/gk/gpoller"
)

// Action 是事件回调返回的连接处置动作。
// Action is the connection disposition returned from event callbacks.
type Action int

// 回调动作。
// Callback actions.
const (
	ActionNone Action = iota
	// ActionClose 立即关闭当前连接。
	ActionClose
	// ActionShutdown 停止整个服务器。
	ActionShutdown
)

// EventHandler 是 reactor 事件回调集。除 OnBoot 外均在引擎循环
// goroutine 上串行执行，回调中不得阻塞；OnClose 可能并发于其他回调。
//
// EventHandler is the reactor callback set. Except OnBoot, callbacks run
// serially on the engine loop goroutine and must not block; OnClose may run
// concurrently with other callbacks.
type EventHandler interface {
	OnBoot(s *Server) error
	OnOpen(c Conn) (out []byte, action Action)
	OnTraffic(c Conn) (action Action)
	OnClose(c Conn, err error)
	OnTick() (delay time.Duration, action Action)
}

// Conn 是 reactor 连接：满足 net.Conn，可直接被 mux/codec 与第三方
// 生态（http/quic/yamux）消费。
//
// Conn is a reactor connection: it satisfies net.Conn and can be consumed
// directly by mux/codec and third-party ecosystems (http/quic/yamux).
type Conn interface {
	net.Conn
	ID() int64
	Context() context.Context
}

// Logger 是 gnet 的最小日志接口；默认标准库实现，可用 WithLogger 注入
// glog 等适配。
//
// Logger is the minimal gnet logging interface; the stdlib default can be
// replaced via WithLogger with a glog adapter.
type Logger interface {
	Printf(format string, args ...any)
}

// Server 是 reactor 服务器。
// Server is the reactor server.
type Server struct {
	handler EventHandler
	logger  Logger
	engine  gpoller.EventEngine

	conns  sync.Map
	nextID atomic.Int64

	readBuf []byte

	pollInterval time.Duration // <0 阻塞；0 busy-poll；>0 定时轮询

	addrMu   sync.Mutex
	addr     net.Addr
	listenFD int
	closeLn  func()

	stopping chan struct{}
	stopOnce sync.Once
	done     chan struct{}
	serveErr error
}

// ServerOption 配置服务器。
// ServerOption configures the server.
type ServerOption func(*Server)

// WithEngine 指定事件引擎；缺省按平台取 gpoller.NewEngine。
// WithEngine sets the event engine; the default comes from gpoller.NewEngine.
func WithEngine(e gpoller.EventEngine) ServerOption {
	return func(s *Server) { s.engine = e }
}

// WithLogger 指定日志实现。
// WithLogger sets the logger implementation.
func WithLogger(l Logger) ServerOption {
	return func(s *Server) {
		if l != nil {
			s.logger = l
		}
	}
}

// WithReadBufferSize 设置每循环读取缓冲大小（默认 64KB）。
// WithReadBufferSize sets the per-loop read buffer size (default 64KB).
func WithReadBufferSize(n int) ServerOption {
	return func(s *Server) {
		if n > 0 {
			s.readBuf = make([]byte, n)
		}
	}
}

// WithPollInterval 设置引擎等待模式（仅引擎由 gnet 创建时生效）：
// <0 阻塞等待（默认，真实硬件上唤醒为 µs 级）；0 busy-poll（空闲让出
// CPU，换取最低唤醒延迟，适用于虚拟化等内核线程唤醒昂贵的环境）；
// >0 定时轮询。
//
// WithPollInterval sets the engine wait mode (only when gnet creates the
// engine): <0 blocking (default; µs wakeups on real hardware), 0 busy-poll
// (yield when idle, lowest wake latency on virtualization where kernel
// thread wakeups are expensive), >0 timed polling.
func WithPollInterval(d time.Duration) ServerOption {
	return func(s *Server) { s.pollInterval = d }
}

// New 创建服务器；Serve 开始监听。
// New creates a server; Serve starts listening.
func New(h EventHandler, opts ...ServerOption) *Server {
	s := &Server{
		handler:  h,
		logger:   log.Default(),
		readBuf:  make([]byte, 64*1024),
		stopping: make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Serve 在 addr 上监听并阻塞运行，直至 Stop 或引擎退出。
// 仅可调用一次；回调中调用 Stop 需在 goroutine 内执行。
//
// Serve listens on addr and blocks until Stop or engine exit. It may only be
// called once; calling Stop from a callback requires a goroutine.
func (s *Server) Serve(addr string) error {
	err := s.serveImpl(addr)
	s.serveErr = err
	close(s.done)
	return err
}

// Stop 优雅停止：停止接收、关闭全部连接并退出 Serve。
// Stop gracefully stops the server: no new accepts, all conns closed, Serve
// returns.
func (s *Server) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		close(s.stopping)
		if s.closeLn != nil {
			s.closeLn()
		}
		s.closeAllConns()
		if s.engine != nil {
			_ = s.engine.Close()
		}
	})
	select {
	case <-s.done:
		return s.serveErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Addr 返回监听地址；Serve 前为 nil。
// Addr returns the listen address; nil before Serve.
func (s *Server) Addr() net.Addr {
	s.addrMu.Lock()
	defer s.addrMu.Unlock()
	return s.addr
}

// CountConnections 返回当前连接数。
// CountConnections returns the current connection count.
func (s *Server) CountConnections() int {
	n := 0
	s.conns.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// closeAllConns 关闭全部连接。
// closeAllConns closes every connection.
func (s *Server) closeAllConns() {
	s.conns.Range(func(_, v any) bool {
		if c, ok := v.(Conn); ok {
			_ = c.Close()
		}
		return true
	})
}

// tickLoop 周期调用 OnTick；返回值决定下一次间隔。
// tickLoop calls OnTick periodically; the returned delay sets the next tick.
func (s *Server) tickLoop() {
	delay := time.Second
	for {
		select {
		case <-s.stopping:
			return
		case <-time.After(delay):
		}
		d, action := s.handler.OnTick()
		if d > 0 {
			delay = d
		}
		if action == ActionShutdown {
			go func() { _ = s.Stop(context.Background()) }()
			return
		}
	}
}
