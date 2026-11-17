//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package gnet

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestListenStdlibHTTPServer 覆盖核心目标场景：标准库 http.Server 直接
// 消费引擎驱动的 Listener/Conn（ghttp 经 Serve(ln) 同理，零改动）。
//
// TestListenStdlibHTTPServer covers the key scenario: a stdlib http.Server
// consuming the engine-driven Listener/Conn directly (ghttp works the same
// via Serve(ln) with zero changes).
func TestListenStdlibHTTPServer(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello-"+r.URL.Path)
	})}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-done
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/engine")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "hello-/engine" {
		t.Fatalf("body = %q", body)
	}
}

// TestListenRawConn 覆盖裸 net.Conn 语义：读写、截止时间、半关闭。
func TestListenRawConn(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

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
	defer client.Close()
	accepted := <-accCh
	defer accepted.Close()

	// 客户端 → 服务端。
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(accepted, buf); err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got %q", buf)
	}

	// 服务端 → 客户端。
	if _, err := accepted.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	buf = make([]byte, 4)
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("got %q", buf)
	}

	// 截止时间语义。
	_ = accepted.SetReadDeadline(time.Now().Add(40 * time.Millisecond))
	start := time.Now()
	if _, err := accepted.Read(make([]byte, 1)); err == nil || !isTimeout(err) {
		t.Fatalf("Read = %v, want timeout", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("deadline took %v", time.Since(start))
	}

	// 半关闭：客户端 CloseWrite，服务端应读到 EOF。
	// 刷新读截止（前面设的 40ms 截止已过期，否则会先于 FIN 命中）。
	_ = accepted.SetReadDeadline(time.Now().Add(3 * time.Second))
	tc := client.(*net.TCPConn)
	if err := tc.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	buf = make([]byte, 1)
	if _, err := accepted.Read(buf); err != io.EOF {
		t.Fatalf("after half-close Read = %v, want io.EOF", err)
	}
}

// TestListenCloseSemantics 覆盖 Close 后 Accept 报错与幂等。
func TestListenCloseSemantics(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("Accept after Close = nil error")
	}
}

// TestListenBadAddr 覆盖非法地址即刻失败。
func TestListenBadAddr(t *testing.T) {
	if _, err := Listen("999.999.999.999:1"); err == nil {
		t.Fatal("Listen with invalid IP = nil error")
	}
	if _, err := Listen("not-a-host:1"); err == nil {
		t.Fatal("Listen with bad host = nil error")
	}
}

// TestListenConcurrent 覆盖并发连接。
func TestListenConcurrent(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
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
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
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
			msg := strings.Repeat("x", 4096)
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
