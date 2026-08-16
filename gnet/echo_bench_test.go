//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package gnet

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// benchEchoHandler 是基准用回显处理器（不回读缓冲，直接按事件转发）。
type benchEchoHandler struct{}

func (benchEchoHandler) OnBoot(_ *Server) error          { return nil }
func (benchEchoHandler) OnOpen(Conn) ([]byte, Action)    { return nil, ActionNone }
func (benchEchoHandler) OnClose(Conn, error)             {}
func (benchEchoHandler) OnTick() (time.Duration, Action) { return time.Second, ActionNone }
func (benchEchoHandler) OnTraffic(c Conn) Action {
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

// startBenchEcho 启动 gnet 回显服务器并等待就绪。
func startBenchEcho(b *testing.B, opts ...ServerOption) (*Server, net.Conn) {
	b.Helper()
	s := New(benchEchoHandler{}, opts...)
	errc := make(chan error, 1)
	go func() { errc <- s.Serve("127.0.0.1:0") }()
	for s.Addr() == nil {
		time.Sleep(time.Millisecond)
	}
	conn, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		b.Fatalf("dial: %v", err)
	}
	b.Cleanup(func() {
		_ = conn.Close()
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Stop(stopCtx)
		<-errc
	})
	return s, conn
}

// echoRoundTrip 完成一次写读往返。
func echoRoundTrip(b *testing.B, conn net.Conn, payload []byte) {
	b.Helper()
	if _, err := conn.Write(payload); err != nil {
		b.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		b.Fatalf("read: %v", err)
	}
}

// BenchmarkGNetEcho 基准 gnet reactor 回显（默认阻塞引擎）。
// 注意：虚拟化环境下内核线程唤醒可达数十 µs，阻塞引擎的往返延迟会被
// 环境放大；真实硬件上唤醒为 µs 级。对照见 BenchmarkGNetEchoBusy。
//
// BenchmarkGNetEcho benchmarks gnet reactor echo with the default blocking
// engine. Note: under virtualization kernel thread wakeups can cost tens of
// µs and inflate round trips; real hardware wakes in µs. See
// BenchmarkGNetEchoBusy for comparison.
func BenchmarkGNetEcho(b *testing.B) {
	_, conn := startBenchEcho(b)
	payload := make([]byte, 128)
	for i := range payload {
		payload[i] = byte(i)
	}
	b.SetBytes(int64(len(payload) * 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		echoRoundTrip(b, conn, payload)
	}
}

// BenchmarkGNetEchoBusy 基准 busy-poll 引擎（空闲让出 CPU 换最低唤醒延迟）。
// BenchmarkGNetEchoBusy benchmarks the busy-poll engine (yield when idle for
// minimal wake latency).
func BenchmarkGNetEchoBusy(b *testing.B) {
	_, conn := startBenchEcho(b, WithPollInterval(0))
	payload := make([]byte, 128)
	for i := range payload {
		payload[i] = byte(i)
	}
	b.SetBytes(int64(len(payload) * 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		echoRoundTrip(b, conn, payload)
	}
}

// BenchmarkStdNetEcho 基准标准库 net 回显（goroutine-per-conn + io.Copy）。
func BenchmarkStdNetEcho(b *testing.B) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	b.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}()
		}
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		b.Fatalf("dial: %v", err)
	}
	b.Cleanup(func() { _ = conn.Close() })

	payload := make([]byte, 128)
	for i := range payload {
		payload[i] = byte(i)
	}
	b.SetBytes(int64(len(payload) * 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		echoRoundTrip(b, conn, payload)
	}
}
