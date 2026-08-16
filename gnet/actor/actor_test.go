//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package actor

import (
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet"
)

// echoHandler 是回显处理器，记录消息与关闭。
type echoHandler struct {
	mu     sync.Mutex
	got    []string
	closes int
}

func (h *echoHandler) OnMessage(data []byte) []byte {
	h.mu.Lock()
	h.got = append(h.got, string(data))
	h.mu.Unlock()
	return data
}

func (h *echoHandler) OnClose(err error) {
	h.mu.Lock()
	h.closes++
	h.mu.Unlock()
}

func (h *echoHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.got...)
}

func (h *echoHandler) closeCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closes
}

// spawnEchoWorker 启动回显 worker 并返回客户端连接。
func spawnEchoWorker(t *testing.T) (net.Conn, *echoHandler, *Worker) {
	t.Helper()
	ln, err := gnet.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accCh <- c
		}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	h := &echoHandler{}
	w := SpawnWorker((<-accCh).(gnet.Conn), h, 32)
	t.Cleanup(w.Stop)
	return client, h, w
}

// TestWorkerEcho 覆盖连接消息串行处理与回显。
func TestWorkerEcho(t *testing.T) {
	client, h, w := spawnEchoWorker(t)
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))

	for _, msg := range []string{"one", "two", "three"} {
		if _, err := client.Write([]byte(msg)); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(msg))
		if _, err := io.ReadFull(client, got); err != nil {
			t.Fatalf("echo %q: %v", msg, err)
		}
		if string(got) != msg {
			t.Fatalf("echo = %q, want %q", got, msg)
		}
	}
	// 等待处理完成。
	deadline := time.Now().Add(2 * time.Second)
	for len(h.messages()) < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := strings.Join(h.messages(), ","); got != "one,two,three" {
		t.Fatalf("messages = %q", got)
	}
	// 本地 Stop：OnClose 恰好一次。
	w.Stop()
	if n := h.closeCount(); n != 1 {
		t.Fatalf("OnClose count = %d, want 1", n)
	}
}

// TestWorkerSend 覆盖外部消息投递与邮箱满丢弃。
func TestWorkerSend(t *testing.T) {
	client, h, w := spawnEchoWorker(t)
	defer w.Stop()
	_ = client

	if !w.Send([]byte("external")) {
		t.Fatal("Send = false")
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(h.messages()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := h.messages(); len(got) != 1 || got[0] != "external" {
		t.Fatalf("messages = %v", got)
	}
	// 外部消息的响应写回连接：客户端应能读到。
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, len("external"))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("read external response: %v", err)
	}
	if string(buf) != "external" {
		t.Fatalf("response = %q", buf)
	}
}

// TestWorkerRemoteClose 覆盖对端断开触发 OnClose。
func TestWorkerRemoteClose(t *testing.T) {
	client, h, w := spawnEchoWorker(t)
	defer w.Stop()
	_ = client.Close()
	deadline := time.Now().Add(3 * time.Second)
	for h.closeCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if h.closeCount() != 1 {
		t.Fatalf("OnClose count = %d, want 1 after remote close", h.closeCount())
	}
	// 重复 Stop 幂等。
	w.Stop()
	w.Stop()
	if h.closeCount() != 1 {
		t.Fatalf("OnClose count = %d, want 1 (idempotent)", h.closeCount())
	}
}

// TestWorkerConcurrent 覆盖多个 worker 并发独立处理。
func TestWorkerConcurrent(t *testing.T) {
	ln, err := gnet.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			SpawnWorker(c.(gnet.Conn), &echoHandler{}, 32)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			msg := strings.Repeat(string(rune('a'+i)), 128)
			if _, err := conn.Write([]byte(msg)); err != nil {
				t.Error(err)
				return
			}
			got := make([]byte, len(msg))
			if _, err := io.ReadFull(conn, got); err != nil {
				t.Error(err)
				return
			}
			if string(got) != msg {
				t.Error("echo mismatch")
			}
		}(i)
	}
	wg.Wait()
}
