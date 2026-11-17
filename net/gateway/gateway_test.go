package gateway

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// echoBackend 启动回显后端并返回地址。
func echoBackend(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

// TestGatewayTCPEcho 覆盖 TCP 转发端到端回显。
func TestGatewayTCPEcho(t *testing.T) {
	backend := echoBackend(t)

	// 先占住一个固定本地端口。
	gln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := gln.Addr().String()
	_ = gln.Close()

	g := New([]Rule{{Listen: addr, Upstream: backend, Protocol: "tcp"}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Logf("Run: %v", err)
			}
		case <-time.After(3 * time.Second):
		}
	})

	// 轮询直到监听就绪。
	var conn net.Conn
	for i := 0; i < 100; i++ {
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	msg := "hello gateway"
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != msg {
		t.Fatalf("echo = %q, want %q", got, msg)
	}
}

// TestGatewayUnknownProtocol 覆盖非法协议错误。
func TestGatewayUnknownProtocol(t *testing.T) {
	g := New([]Rule{{Listen: "127.0.0.1:0", Upstream: "x", Protocol: "bogus"}})
	err := g.Run(context.Background())
	if err == nil {
		t.Fatal("Run with unknown protocol = nil error")
	}
}
