//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package gnet

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// echoHandler 是回显处理器；同时把新连接与关闭通知导出给测试。
type echoHandler struct {
	opened chan Conn
	closed atomic.Int32
	ticks  atomic.Int32
}

func (h *echoHandler) OnBoot(_ *Server) error { return nil }
func (h *echoHandler) OnTick() (time.Duration, Action) {
	h.ticks.Add(1)
	return time.Second, ActionNone
}
func (h *echoHandler) OnOpen(c Conn) ([]byte, Action) {
	if h.opened != nil {
		h.opened <- c
	}
	return nil, ActionNone
}
func (h *echoHandler) OnTraffic(c Conn) Action {
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return ActionClose
	}
	if _, err := c.Write(buf[:n]); err != nil {
		return ActionClose
	}
	return ActionNone
}
func (h *echoHandler) OnClose(_ Conn, _ error) { h.closed.Add(1) }

// startEchoServer 启动回显服务器并等待监听就绪。
func startEchoServer(t *testing.T, h EventHandler) (*Server, error) {
	t.Helper()
	s := New(h)
	errc := make(chan error, 1)
	go func() { errc <- s.Serve("127.0.0.1:0") }()
	deadline := time.Now().Add(5 * time.Second)
	for s.Addr() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.Addr() == nil {
		t.Fatal("server did not start listening")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
		select {
		case err := <-errc:
			if err != nil {
				t.Logf("Serve returned: %v", err)
			}
		case <-time.After(4 * time.Second):
			t.Error("Serve did not return after Stop")
		}
	})
	return s, nil
}

// TestEchoServer 覆盖单连接回显。
func TestEchoServer(t *testing.T) {
	h := &echoHandler{}
	s, _ := startEchoServer(t, h)

	conn, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	msg := "hello gnet"
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != msg {
		t.Fatalf("echo = %q, want %q", got, msg)
	}
	if n := s.CountConnections(); n != 1 {
		t.Fatalf("CountConnections = %d, want 1", n)
	}
}

// TestEchoConcurrent 覆盖并发连接与多次往返。
func TestEchoConcurrent(t *testing.T) {
	h := &echoHandler{}
	s, _ := startEchoServer(t, h)

	var wg sync.WaitGroup
	errs := make(chan error, 10*10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := net.Dial("tcp", s.Addr().String())
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			for j := 0; j < 10; j++ {
				msg := fmt.Sprintf("conn-%d-msg-%d|", id, j)
				if _, err := conn.Write([]byte(msg)); err != nil {
					errs <- err
					return
				}
				got := make([]byte, len(msg))
				if _, err := io.ReadFull(conn, got); err != nil {
					errs <- err
					return
				}
				if string(got) != msg {
					errs <- fmt.Errorf("echo mismatch: got %q want %q", got, msg)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestOnOpenOnClose 覆盖回调次数与优雅关闭。
func TestOnOpenOnClose(t *testing.T) {
	h := &echoHandler{opened: make(chan Conn, 8)}
	s, _ := startEchoServer(t, h)

	conn, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	select {
	case <-h.opened:
	case <-time.After(3 * time.Second):
		t.Fatal("OnOpen not called")
	}
	// 客户端关闭 → 服务端应收到 OnClose。
	_ = conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for h.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if h.closed.Load() == 0 {
		t.Fatal("OnClose not called after client close")
	}
}

// TestReadDeadline 覆盖连接读截止。
func TestReadDeadline(t *testing.T) {
	h := &echoHandler{opened: make(chan Conn, 1)}
	s, _ := startEchoServer(t, h)

	netConn, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer netConn.Close()

	var c Conn
	select {
	case c = <-h.opened:
	case <-time.After(3 * time.Second):
		t.Fatal("OnOpen not called")
	}
	_ = c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	start := time.Now()
	buf := make([]byte, 16)
	_, err = c.Read(buf)
	if err == nil || !isTimeout(err) {
		t.Fatalf("Read = %v, want deadline error", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline took %v, want ~50ms", elapsed)
	}
}

// isTimeout 判断是否为超时错误。
func isTimeout(err error) bool {
	if ne, ok := err.(net.Error); ok {
		return ne.Timeout()
	}
	return false
}
