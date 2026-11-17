//go:build linux

package forward

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// TestSpliceCopy 验证 splice 转发：客户端 → 中继（splice）→ 后端，
// 字节一致且 EOF 传播。
// TestSpliceCopy verifies splice relay: client → relay (splice) → backend,
// identical bytes with EOF propagation.
func TestSpliceCopy(t *testing.T) {
	// 客户端监听：接受客户端连接作为 splice 的源。
	srcLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srcLn.Close()
	// 后端监听：splice 的目的端。
	dstLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dstLn.Close()

	const total = 1 << 20
	payload := bytes.Repeat([]byte("splice!"), total/7+1)[:total]

	// 客户端：写数据 + FIN。
	client, err := net.DialTCP("tcp", nil, srcLn.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	go func() {
		_, _ = client.Write(payload)
		_ = client.CloseWrite()
	}()

	// 后端：读全量数据并验证 EOF。
	// 先拨 dst 再等 accept，避免 accept 等待与拨号互相死锁。
	dstConn, err := net.DialTCP("tcp", nil, dstLn.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer dstConn.Close()

	backendAccCh := make(chan *net.TCPConn, 1)
	go func() {
		c, err := dstLn.Accept()
		if err == nil {
			backendAccCh <- c.(*net.TCPConn)
		}
	}()
	backend := <-backendAccCh
	defer backend.Close()
	_ = backend.SetDeadline(time.Now().Add(10 * time.Second))
	backendErr := make(chan error, 1)
	got := make([]byte, total)
	go func() {
		if _, err := io.ReadFull(backend, got); err != nil {
			backendErr <- err
			return
		}
		buf := make([]byte, 1)
		if _, err := backend.Read(buf); err != io.EOF {
			backendErr <- err
			return
		}
		backendErr <- nil
	}()

	srcConn, err := srcLn.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer srcConn.Close()

	n, err := SpliceCopy(dstConn, srcConn.(*net.TCPConn), 0)
	if err != nil {
		t.Fatalf("SpliceCopy: %v", err)
	}
	if n != int64(total) {
		t.Fatalf("copied = %d, want %d", n, total)
	}
	// splice 不传 FIN：转发完手动半关闭目的端。
	// splice does not propagate FIN: half-close the dst after the relay.
	if err := dstConn.CloseWrite(); err != nil {
		t.Fatalf("dst CloseWrite: %v", err)
	}
	if err := <-backendErr; err != nil {
		t.Fatalf("backend: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("spliced bytes mismatch")
	}
}
