//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package gnet

import (
	"context"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/sofiworker/gk/gpoller"
	"golang.org/x/sys/unix"
)

// listenBacklog 是 accept 队列深度。
// listenBacklog is the accept queue depth.
const listenBacklog = 1024

// serveImpl 在 fd 引擎上运行服务器。
// serveImpl runs the server on the fd engine.
func (s *Server) serveImpl(addr string) error {
	if s.engine == nil {
		eng, err := gpoller.NewEngine(gpoller.WithPollInterval(s.pollInterval))
		if err != nil {
			return err
		}
		s.engine = eng
	}
	fd, err := newListenerFD(addr)
	if err != nil {
		return err
	}
	s.listenFD = fd
	s.closeLn = func() { unix.Close(fd) }
	s.addrMu.Lock()
	s.addr = listenAddr(fd)
	s.addrMu.Unlock()

	if err := s.engine.AddRead(fd, s.onAccept); err != nil {
		unix.Close(fd)
		return err
	}
	if err := s.handler.OnBoot(s); err != nil {
		unix.Close(fd)
		return err
	}
	go s.tickLoop()

	err = s.engine.Poll()

	s.closeLn()
	s.closeAllConns()
	return err
}

// newListenerFD 创建、绑定并监听 TCP socket（IPv4 优先，":port" 绑定 0.0.0.0）。
// newListenerFD creates, binds and listens a TCP socket (IPv4-first; ":port"
// binds 0.0.0.0).
func newListenerFD(addr string) (int, error) {
	ta, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return -1, err
	}
	var (
		family int
		sa     unix.Sockaddr
	)
	if ip4 := ta.IP.To4(); ip4 != nil {
		family = unix.AF_INET
		sa4 := &unix.SockaddrInet4{Port: ta.Port}
		copy(sa4.Addr[:], ip4)
		sa = sa4
	} else {
		family = unix.AF_INET6
		sa6 := &unix.SockaddrInet6{Port: ta.Port}
		if ta.IP != nil {
			copy(sa6.Addr[:], ta.IP.To16())
		}
		sa = sa6
	}
	fd, err := unix.Socket(family, unix.SOCK_STREAM, 0)
	if err != nil {
		return -1, err
	}
	fail := func(err error) (int, error) {
		unix.Close(fd)
		return -1, err
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		return fail(err)
	}
	if family == unix.AF_INET6 {
		_ = unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		return fail(err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		return fail(err)
	}
	if err := unix.Bind(fd, sa); err != nil {
		return fail(err)
	}
	if err := unix.Listen(fd, listenBacklog); err != nil {
		return fail(err)
	}
	return fd, nil
}

// listenAddr 读取监听 socket 的本地地址。
// listenAddr reads the local address of the listen socket.
func listenAddr(fd int) net.Addr {
	sa, err := unix.Getsockname(fd)
	if err != nil {
		return nil
	}
	return sockaddrToTCPAddr(sa)
}

// sockaddrToTCPAddr 转换 sockaddr 为 net.TCPAddr。
// sockaddrToTCPAddr converts a sockaddr to net.TCPAddr.
func sockaddrToTCPAddr(sa unix.Sockaddr) net.Addr {
	switch a := sa.(type) {
	case *unix.SockaddrInet4:
		return &net.TCPAddr{IP: append(net.IP(nil), a.Addr[:]...), Port: a.Port}
	case *unix.SockaddrInet6:
		return &net.TCPAddr{IP: append(net.IP(nil), a.Addr[:]...), Port: a.Port}
	}
	return nil
}

// onAccept 处理监听 fd 的读就绪：循环 accept 直到 EAGAIN。
// onAccept handles listen-fd readiness: accept in a loop until EAGAIN.
func (s *Server) onAccept(ev gpoller.Event) {
	if ev.Hup || ev.Err != nil {
		return
	}
	for {
		fd, sa, err := acceptConn(s.listenFD)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EBADF || err == unix.EINVAL {
				return
			}
			s.logger.Printf("gnet: accept error: %v", err)
			return
		}
		c, cerr := s.newConn(fd, sa)
		if cerr != nil {
			unix.Close(fd)
			continue
		}
		s.conns.Store(c.ID(), c)
		out, action := s.handler.OnOpen(c)
		if len(out) > 0 {
			_, _ = c.Write(out)
		}
		if action == ActionClose {
			_ = c.Close()
			continue
		}
		if action == ActionShutdown {
			go func() { _ = s.Stop(context.Background()) }()
			return
		}
	}
}

// conn 是 fd 引擎模式下的连接实现。
// conn is the connection implementation in fd-engine mode.
type conn struct {
	server *Server
	id     int64
	fd     int

	ctx    context.Context
	cancel context.CancelFunc

	mu            sync.Mutex
	closed        bool
	closeErr      error
	inbound       *gpoller.RingBuffer
	outbound      []byte
	readWait      chan struct{}
	done          chan struct{}
	readDeadline  time.Time
	writeDeadline time.Time
	local, remote net.Addr
}

// newConn 包装已 accept 的 fd 并注册读兴趣。
// newConn wraps an accepted fd and registers read interest.
func (s *Server) newConn(fd int, sa unix.Sockaddr) (*conn, error) {
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	_ = unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{
		server:   s,
		id:       s.nextID.Add(1),
		fd:       fd,
		ctx:      ctx,
		cancel:   cancel,
		inbound:  gpoller.NewRingBuffer(64 * 1024),
		readWait: make(chan struct{}, 1),
		done:     make(chan struct{}),
		remote:   sockaddrToTCPAddr(sa),
	}
	if lsa, err := unix.Getsockname(fd); err == nil {
		c.local = sockaddrToTCPAddr(lsa)
	}
	if err := s.engine.AddRead(fd, c.onReadable); err != nil {
		cancel()
		return nil, err
	}
	// 注册写处理器但保持写兴趣关闭：Write 积压时才按需开启（Mod）。
	// Register the write handler with the interest off: queued writes enable
	// it on demand via Mod.
	if err := s.engine.AddWrite(fd, c.onWritable); err == nil {
		_ = s.engine.Mod(fd, true, false)
	} else {
		cancel()
		return nil, err
	}
	return c, nil
}

// onReadable 处理连接读就绪：单次读入缓冲、唤醒读者并触发 OnTraffic。
// LT 模式下剩余数据由下一次事件继续，不在此循环探测（省 syscall）。
//
// onReadable handles conn readability: one read into the buffer, reader
// wakeup, OnTraffic. LT leaves remaining data to the next event; no probe
// reads here (saves syscalls).
func (c *conn) onReadable(ev gpoller.Event) {
	buf := c.server.readBuf
	n, err := unix.Read(c.fd, buf)
	if n > 0 {
		c.mu.Lock()
		if !c.closed {
			c.inbound.Write(buf[:n])
		}
		c.mu.Unlock()
		select {
		case c.readWait <- struct{}{}:
		default:
		}
	}
	if err != nil && err != unix.EAGAIN && err != unix.EWOULDBLOCK {
		if err == io.EOF {
			c.closeWith(io.EOF)
		} else {
			c.closeWith(err)
		}
		return
	}
	if n == 0 && err == nil {
		// TCP EOF：对端关闭写侧。
		// TCP EOF: peer closed its write side.
		c.closeWith(io.EOF)
		return
	}
	if n == 0 && ev.Hup {
		// EAGAIN + RDHUP：无剩余数据且对端已关闭写侧。
		// EAGAIN + RDHUP: no data left and the peer half-closed.
		c.closeWith(io.EOF)
		return
	}
	action := c.server.handler.OnTraffic(c)
	if action == ActionClose {
		_ = c.Close()
	}
	if action == ActionShutdown {
		go func() { _ = c.server.Stop(context.Background()) }()
	}
}

// onWritable 处理连接写就绪：冲刷积压队列。
// onWritable handles conn writability: flushing the pending queue.
func (c *conn) onWritable(_ gpoller.Event) {
	c.mu.Lock()
	if c.closed || len(c.outbound) == 0 {
		if !c.closed {
			_ = c.server.engine.Mod(c.fd, true, false)
		}
		c.mu.Unlock()
		return
	}
	n, err := unix.Write(c.fd, c.outbound)
	if n > 0 {
		c.outbound = c.outbound[n:]
	}
	if len(c.outbound) == 0 {
		_ = c.server.engine.Mod(c.fd, true, false)
	} else if err != nil && err != unix.EAGAIN && err != unix.EWOULDBLOCK {
		c.mu.Unlock()
		c.closeWith(err)
		return
	}
	c.mu.Unlock()
}

// closeWith 幂等关闭：注销引擎、关 fd、唤醒读者并回调 OnClose。
// closeWith idempotently closes: engine unregister, fd close, reader wakeup,
// OnClose callback.
func (c *conn) closeWith(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.closeErr = err
	_ = c.server.engine.Delete(c.fd)
	_ = unix.Close(c.fd)
	c.cancel()
	close(c.done)
	c.mu.Unlock()

	c.server.conns.Delete(c.id)
	c.server.handler.OnClose(c, err)
}

// Read 实现 net.Conn：有数据即返回，否则等待数据/截止/关闭。
// Read implements net.Conn: returns buffered data or waits for data,
// deadline, or close.
func (c *conn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		if c.closed {
			err := c.closeErr
			c.mu.Unlock()
			if err == nil {
				err = net.ErrClosed
			}
			return 0, err
		}
		if c.inbound.Len() > 0 {
			n := c.inbound.Read(p)
			c.mu.Unlock()
			return n, nil
		}
		dl := c.readDeadline
		wait := c.readWait
		done := c.done
		c.mu.Unlock()

		if !dl.IsZero() && time.Now().After(dl) {
			return 0, os.ErrDeadlineExceeded
		}
		if dl.IsZero() {
			select {
			case <-wait:
			case <-done:
			}
			continue
		}
		timer := time.NewTimer(time.Until(dl))
		select {
		case <-wait:
			timer.Stop()
		case <-timer.C:
			c.mu.Lock()
			if c.inbound.Len() > 0 {
				n := c.inbound.Read(p)
				c.mu.Unlock()
				return n, nil
			}
			c.mu.Unlock()
			return 0, os.ErrDeadlineExceeded
		case <-done:
			timer.Stop()
		}
	}
}

// Write 实现 net.Conn：直写成功后返回；否则全量入队异步冲刷。
// Write implements net.Conn: direct write on success, otherwise the whole
// payload is queued for async flush.
func (c *conn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	if !c.writeDeadline.IsZero() && time.Now().After(c.writeDeadline) {
		c.mu.Unlock()
		return 0, os.ErrDeadlineExceeded
	}
	total := 0
	if len(c.outbound) == 0 {
		n, err := unix.Write(c.fd, p)
		if err == nil {
			c.mu.Unlock()
			return n, nil
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			c.mu.Unlock()
			return n, err
		}
		total += n
		p = p[n:]
	}
	c.outbound = append(c.outbound, p...)
	total += len(p)
	_ = c.server.engine.Mod(c.fd, true, true)
	c.mu.Unlock()
	return total, nil
}

// Close 关闭连接（本地关闭，OnClose 收到 nil）。
// Close closes the conn locally; OnClose receives nil.
func (c *conn) Close() error {
	c.closeWith(nil)
	return nil
}

// SetDeadline 同时设置读写截止。
// SetDeadline sets both read and write deadlines.
func (c *conn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline, c.writeDeadline = t, t
	c.mu.Unlock()
	return nil
}

// SetReadDeadline 设置读截止。
// SetReadDeadline sets the read deadline.
func (c *conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	return nil
}

// SetWriteDeadline 设置写截止。
// SetWriteDeadline sets the write deadline.
func (c *conn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.writeDeadline = t
	c.mu.Unlock()
	return nil
}

// ID 返回连接序号。
// ID returns the connection id.
func (c *conn) ID() int64 { return c.id }

// Context 返回连接上下文，关闭时取消。
// Context returns the conn context, cancelled on close.
func (c *conn) Context() context.Context { return c.ctx }

// LocalAddr 返回本地地址。
// LocalAddr returns the local address.
func (c *conn) LocalAddr() net.Addr { return c.local }

// RemoteAddr 返回对端地址。
// RemoteAddr returns the remote address.
func (c *conn) RemoteAddr() net.Addr { return c.remote }
