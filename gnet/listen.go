package gnet

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// Listen 创建引擎驱动的标准库兼容 Listener：accept 走 epoll/kqueue 引擎，
// Accept 返回的 net.Conn 由引擎分发读写（阻塞语义、截止时间、半关闭均
// 满足标准库约定）。任何基于标准库的 server（net/http、ghttp、grpc 等）
// 都可经 Serve(ln) 直接消费，ghttp 无需任何改动（2026-08-16 决策：
// 垫底走标准库接口，不碰 ghttp server）。
//
// Listen creates an engine-driven, stdlib-compatible Listener: accepts run
// on the epoll/kqueue engine and Accept returns net.Conns whose reads and
// writes are dispatched by the engine (blocking semantics, deadlines and
// half-close per stdlib conventions). Any stdlib-based server (net/http,
// ghttp, grpc, ...) can consume it via Serve(ln) with zero ghttp changes
// (2026-08-16 decision: back the transport at the stdlib interface level;
// never touch the ghttp server).
func Listen(addr string, opts ...ServerOption) (net.Listener, error) {
	l := &engineListener{ch: make(chan net.Conn, 128), closed: make(chan struct{})}
	l.srv = New(&passthroughHandler{ch: l.ch}, opts...)

	errc := make(chan error, 1)
	go func() { errc <- l.srv.Serve(addr) }()
	go func() {
		if err := <-errc; err != nil {
			l.mu.Lock()
			l.serveErr = err
			l.mu.Unlock()
			l.once.Do(func() { close(l.closed) })
		}
	}()

	// 等待监听就绪（bind 失败即刻返回错误，与 net.Listen 语义一致）。
	deadline := time.Now().Add(5 * time.Second)
	for l.srv.Addr() == nil {
		select {
		case <-l.closed:
			return nil, l.err()
		default:
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("gnet: listen %s: timeout", addr)
		}
		time.Sleep(time.Millisecond)
	}
	return l, nil
}

// engineListener 是引擎驱动的 net.Listener 实现。
// engineListener is the engine-driven net.Listener implementation.
type engineListener struct {
	srv      *Server
	ch       chan net.Conn
	once     sync.Once
	closed   chan struct{}
	mu       sync.Mutex
	serveErr error
}

// Accept 返回下一个引擎驱动连接；Close 后返回错误。
// Accept returns the next engine-driven conn; returns an error after Close.
func (l *engineListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		if c == nil {
			return nil, net.ErrClosed
		}
		return c, nil
	case <-l.closed:
		err := l.err()
		if err == nil {
			err = net.ErrClosed
		}
		return nil, err
	}
}

// Close 停止监听并关闭全部连接。
// Close stops the listener and closes every conn.
func (l *engineListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = l.srv.Stop(ctx)
	})
	return nil
}

// Addr 返回监听地址。
// Addr returns the listen address.
func (l *engineListener) Addr() net.Addr { return l.srv.Addr() }

// err 返回服务错误。
// err returns the serve error.
func (l *engineListener) err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.serveErr
}

// passthroughHandler 把接受的连接透传给 Accept 通道；读数据仅入缓冲，
// 不触发额外处理（数据由用户的 Read 消费）。
//
// passthroughHandler relays accepted conns to the Accept channel; read data
// only enters the inbound buffer with no extra processing (consumed by the
// user's Read).
type passthroughHandler struct {
	ch chan net.Conn
}

func (p *passthroughHandler) OnBoot(_ *Server) error          { return nil }
func (p *passthroughHandler) OnTraffic(_ Conn) Action         { return ActionNone }
func (p *passthroughHandler) OnClose(Conn, error)             {}
func (p *passthroughHandler) OnTick() (time.Duration, Action) { return time.Second, ActionNone }

// OnOpen 把连接交给 Accept；缓冲满时拒连。
// OnOpen hands the conn to Accept; refuses when the backlog is full.
func (p *passthroughHandler) OnOpen(c Conn) ([]byte, Action) {
	select {
	case p.ch <- c:
		return nil, ActionNone
	default:
		return nil, ActionClose
	}
}
