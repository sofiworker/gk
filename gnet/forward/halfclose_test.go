package forward

import (
	"io"
	"net"
	"testing"
	"time"
)

// TestBridgeHalfClose 验证 FIN 传播：客户端半关闭后仍能收到对端响应。
// TestBridgeHalfClose verifies FIN propagation: the client still receives the
// peer response after half-closing.
func TestBridgeHalfClose(t *testing.T) {
	// 桥的本地端监听。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// 后端：读到 EOF 后回写 "bye" 再关闭。
	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backendLn.Close()
	backendDone := make(chan error, 1)
	go func() {
		c, err := backendLn.Accept()
		if err != nil {
			backendDone <- err
			return
		}
		defer c.Close()
		data, err := io.ReadAll(c) // 读至 FIN
		if err != nil {
			backendDone <- err
			return
		}
		if string(data) != "hello" {
			backendDone <- io.ErrUnexpectedEOF
			return
		}
		if _, err := c.Write([]byte("bye")); err != nil {
			backendDone <- err
			return
		}
		backendDone <- nil
	}()

	// 桥接：本地端 → 后端。
	go func() {
		local, err := ln.Accept()
		if err != nil {
			return
		}
		remote, err := net.Dial("tcp", backendLn.Addr().String())
		if err != nil {
			_ = local.Close()
			return
		}
		bridge := NewProtocolAwareBridge(local, remote, TCPConfig{}, nil)
		_ = bridge.Start()
	}()

	// 客户端：写数据、半关闭、读响应。
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	tc := client.(*net.TCPConn)
	_ = tc.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := tc.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := tc.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	resp, err := io.ReadAll(tc)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(resp) != "bye" {
		t.Fatalf("response = %q, want bye", resp)
	}
	if err := <-backendDone; err != nil {
		t.Fatalf("backend: %v", err)
	}
}
