package mux

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// TestFrameHeaderEncoding 用黄金字节验证帧头与 yamux 规格一致
// （12 字节大端：version/type/flags/streamID/length）。
func TestFrameHeaderEncoding(t *testing.T) {
	h := header{Version: 0, Type: typePing, Flags: flagSYN | flagFIN, StreamID: 0x01020304, Length: 0x05060708}
	buf := make([]byte, headerSize)
	h.encode(buf)
	want := []byte{0, typePing, 0x00, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if !bytes.Equal(buf, want) {
		t.Fatalf("encoded = %x, want %x", buf, want)
	}
	got := decodeHeader(buf)
	if got != h {
		t.Fatalf("decoded = %+v, want %+v", got, h)
	}
}

// sessionPair 建立一对真实 TCP 承载的会话（FIN/RST 语义可靠）。
// sessionPair creates a session pair over real TCP (reliable FIN/RST).
func sessionPair(t *testing.T, opts ...Option) (client, server *Session) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accCh <- c
		}
	}()
	cc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	sc := <-accCh

	client, err = New(cc, opts...)
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	server, err = New(sc, opts...)
	if err != nil {
		t.Fatalf("New server: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client, server
}

// TestOpenDataClose 覆盖开流、双向数据与关闭。
func TestOpenDataClose(t *testing.T) {
	client, server := sessionPair(t)

	st, err := client.OpenStream(context.Background())
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	accepted, err := server.AcceptStream(context.Background())
	if err != nil {
		t.Fatalf("AcceptStream: %v", err)
	}
	if accepted.StreamID() != st.StreamID() {
		t.Fatalf("stream ids mismatch: %d != %d", accepted.StreamID(), st.StreamID())
	}
	if client.NumStreams() != 1 || server.NumStreams() != 1 {
		t.Fatalf("NumStreams = %d/%d, want 1/1", client.NumStreams(), server.NumStreams())
	}

	if _, err := st.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(accepted, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got %q, want ping", buf)
	}
	if _, err := accepted.Write([]byte("pong")); err != nil {
		t.Fatalf("write back: %v", err)
	}
	buf = make([]byte, 4)
	if _, err := io.ReadFull(st, buf); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("got %q, want pong", buf)
	}

	_ = st.Close()
	_ = accepted.Close()
	waitNumStreams(t, client, 0)
	waitNumStreams(t, server, 0)
}

// TestMultipleStreams 覆盖多流交错传输。
func TestMultipleStreams(t *testing.T) {
	client, server := sessionPair(t)

	const n = 5
	go func() {
		for i := 0; i < n; i++ {
			st, err := server.AcceptStream(context.Background())
			if err != nil {
				return
			}
			go func(st Stream) {
				defer st.Close()
				_, _ = io.Copy(st, st) // echo
			}(st)
		}
	}()

	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			st, err := client.OpenStream(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			defer st.Close()
			msg := bytes.Repeat([]byte{byte('a' + i)}, 1024)
			if _, err := st.Write(msg); err != nil {
				errCh <- err
				return
			}
			got := make([]byte, len(msg))
			if _, err := io.ReadFull(st, got); err != nil {
				errCh <- err
				return
			}
			if !bytes.Equal(got, msg) {
				errCh <- errors.New("echo mismatch")
				return
			}
			errCh <- nil
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

// TestHalfClose 覆盖 FIN 半关闭：CloseWrite 后仍可读回对端响应。
func TestHalfClose(t *testing.T) {
	client, server := sessionPair(t)

	st, err := client.OpenStream(context.Background())
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	accepted, err := server.AcceptStream(context.Background())
	if err != nil {
		t.Fatalf("AcceptStream: %v", err)
	}

	if _, err := st.Write([]byte("request")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}

	// 服务端：读到 request 后 EOF，仍可写回响应。
	got, err := io.ReadAll(accepted)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(got) != "request" {
		t.Fatalf("server got %q", got)
	}
	if _, err := accepted.Write([]byte("response")); err != nil {
		t.Fatalf("server write after peer FIN: %v", err)
	}
	if err := accepted.Close(); err != nil {
		t.Fatalf("server close: %v", err)
	}

	// 客户端：FIN 后仍读到响应，随后 EOF。
	resp, err := io.ReadAll(st)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(resp) != "response" {
		t.Fatalf("client got %q", resp)
	}
	waitNumStreams(t, client, 0)
}

// TestSendWindowFlowControl 白盒验证窗口机制：yamux 窗口是“在途数据上界”
// （预算随 WindowUpdate 回补，与应用读取无关；应用级背压由 TCP 反压承担）。
func TestSendWindowFlowControl(t *testing.T) {
	client, raw := rawClient(t, WithStreamWindowSize(512))

	// net.Pipe 的写阻塞到对端读：并发开流，主测程读 SYN 解除阻塞。
	// net.Pipe writes block until the peer reads: open concurrently while the
	// test reads the SYN.
	type result struct {
		st  Stream
		err error
	}
	res := make(chan result, 1)
	go func() {
		st, err := client.OpenStream(context.Background())
		res <- result{st, err}
	}()

	// 1) 读客户端 SYN（WindowUpdate+SYN，Length=初始窗口 512）。
	h, body := readFrame(t, raw)
	if h.Type != typeWindowUpdate || h.Flags&flagSYN == 0 || h.Length != 512 || len(body) != 0 {
		t.Fatalf("SYN frame = %+v, want windowupdate+SYN len=512", h)
	}
	r := <-res
	if r.err != nil {
		t.Fatalf("OpenStream: %v", r.err)
	}
	st := r.st

	// 2) 回 ACK：通告初始窗口 512。等待客户端处理完毕（白盒观察
	// windowInit），否则 ACK 可能晚于后续写到达，造成窗口被重置。
	writeFrame(t, raw, header{Type: typeWindowUpdate, Flags: flagACK, StreamID: st.StreamID(), Length: 512}, nil)
	waitFor(t, func() bool {
		s2 := st.(*stream)
		s2.writeMu.Lock()
		defer s2.writeMu.Unlock()
		return s2.windowInit
	})

	// 3) 写满整个窗口后，下一次写必须阻塞等待 WindowUpdate。
	// net.Pipe 写阻塞到对端读：并发写，主测程读数据帧解除阻塞。
	wrote := make(chan error, 1)
	go func() {
		_, err := st.Write(make([]byte, 512))
		wrote <- err
	}()
	h, body = readFrame(t, raw)
	if h.Type != typeData || len(body) != 512 {
		t.Fatalf("data frame = %+v len(body)=%d, want 512B data", h, len(body))
	}
	if err := <-wrote; err != nil {
		t.Fatalf("first write: %v", err)
	}
	blocked := make(chan error, 1)
	go func() {
		_, err := st.Write(make([]byte, 64))
		blocked <- err
	}()
	select {
	case err := <-blocked:
		t.Fatalf("Write completed before window update: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// 4) 补 64 字节预算 → 写放行，帧到达。
	writeFrame(t, raw, header{Type: typeWindowUpdate, StreamID: st.StreamID(), Length: 64}, nil)
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Write still blocked after window update")
	}
	h, body = readFrame(t, raw)
	if h.Type != typeData || len(body) != 64 {
		t.Fatalf("second data frame = %+v len(body)=%d, want 64B", h, len(body))
	}
}

// rawClient 建立一方会话 + 由测试直接控制的原始对端（TCP 回环，
// 规避 net.Pipe 的同步写语义干扰）。
// rawClient creates a session with a test-controlled raw peer over TCP
// loopback (avoiding net.Pipe's synchronous write semantics).
func rawClient(t *testing.T, opts ...Option) (*Session, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accCh <- c
		}
	}()
	clientSide, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	rawSide := <-accCh
	t.Cleanup(func() {
		_ = clientSide.Close()
		_ = rawSide.Close()
	})
	s, err := New(clientSide, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, rawSide
}

// readFrame 从原始对端读取一帧。
// readFrame reads one frame from the raw peer.
func readFrame(t *testing.T, conn net.Conn) (header, []byte) {
	t.Helper()
	var hbuf [headerSize]byte
	if _, err := io.ReadFull(conn, hbuf[:]); err != nil {
		t.Fatalf("read header: %v", err)
	}
	h := decodeHeader(hbuf[:])
	var body []byte
	// 与 readLoop 同规则：仅 Data 帧有 body。
	// Same rule as readLoop: only Data frames carry a body.
	if h.Type == typeData && h.Length > 0 {
		body = make([]byte, h.Length)
		if _, err := io.ReadFull(conn, body); err != nil {
			t.Fatalf("read body: %v", err)
		}
	}
	return h, body
}

// writeFrame 向原始对端写入一帧。
// writeFrame writes one frame to the raw peer.
func writeFrame(t *testing.T, conn net.Conn, h header, body []byte) {
	t.Helper()
	frame := make([]byte, headerSize+len(body))
	h.encode(frame)
	copy(frame[headerSize:], body)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// TestPing 覆盖 RTT 测量。
func TestPing(t *testing.T) {
	client, server := sessionPair(t)
	_ = server
	rtt, err := client.Ping()
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if rtt < 0 || rtt > 3*time.Second {
		t.Fatalf("rtt = %v, want [0, 3s)", rtt)
	}
}

// TestKeepAliveTimeout 覆盖 keepalive 无响应时按超时关闭会话。
func TestKeepAliveTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accCh <- c
		}
	}()
	cc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	raw := <-accCh
	defer raw.Close()
	// 对端只读不回：ping 永远得不到响应。
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := raw.Read(buf); err != nil {
				return
			}
		}
	}()

	s, err := New(cc, WithKeepAlive(30*time.Millisecond), WithConnectionWriteTimeout(150*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case <-s.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("session not closed after keepalive timeout")
	}
	if !errors.Is(s.shutdownErr(), ErrKeepAliveTimeout) {
		t.Fatalf("shutdownErr = %v, want ErrKeepAliveTimeout", s.shutdownErr())
	}
}

// TestGoAway 覆盖 go-away 拒绝新流。
func TestGoAway(t *testing.T) {
	client, server := sessionPair(t)

	if err := client.GoAway(); err != nil {
		t.Fatalf("GoAway: %v", err)
	}
	// 本端立即拒绝。
	if _, err := client.OpenStream(context.Background()); !errors.Is(err, ErrGoAway) {
		t.Fatalf("OpenStream after GoAway = %v, want ErrGoAway", err)
	}
	// 对端收到 go-away 后同样拒绝开流。
	waitFor(t, func() bool {
		_, err := server.OpenStream(context.Background())
		return errors.Is(err, ErrGoAway)
	})
}

// TestDeadline 覆盖读截止。
func TestDeadline(t *testing.T) {
	client, _ := sessionPair(t)
	st, err := client.OpenStream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReadDeadline(time.Now().Add(60 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	buf := make([]byte, 8)
	_, err = st.Read(buf)
	if !isTimeout(err) {
		t.Fatalf("Read = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline took %v", elapsed)
	}
}

// TestCloseUnblocksRead 覆盖对端 Close 后 Read 返回 EOF。
func TestCloseUnblocksRead(t *testing.T) {
	client, server := sessionPair(t)
	st, err := client.OpenStream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := server.AcceptStream(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = accepted.Close()
	buf := make([]byte, 8)
	if _, err := st.Read(buf); err != io.EOF {
		t.Fatalf("Read after peer close = %v, want io.EOF", err)
	}
}

// waitNumStreams 轮询等待流数量达到期望。
func waitNumStreams(t *testing.T, s *Session, want int) {
	t.Helper()
	waitFor(t, func() bool { return s.NumStreams() == want })
}

// waitFor 轮询等待条件成立。
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
