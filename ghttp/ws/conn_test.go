package ws_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/ghttp/ws"
)

// TestEcho 验证文本/二进制回显，覆盖三种长度编码。
// TestEcho verifies text/binary echo across all three length encodings.
func TestEcho(t *testing.T) {
	srv, _ := newEchoServer(t)
	c := mustDial(t, srv)
	for _, n := range []int{0, 1, 125, 126, 65535, 65536, 200000} {
		for _, op := range []byte{opText, opBinary} {
			payload := bytes.Repeat([]byte("a"), n)
			c.send(true, op, payload)
			if got := c.mustRead(op); !bytes.Equal(got, payload) {
				t.Fatalf("op %d len %d: echo len %d", op, n, len(got))
			}
		}
	}
}

// TestBufferedAfterHandshake 验证握手后紧跟的帧（已被 net/http 缓冲）不会丢失。
// TestBufferedAfterHandshake verifies frames sent right after the handshake (already
// buffered by net/http) are not lost.
func TestBufferedAfterHandshake(t *testing.T) {
	srv, _ := newEchoServer(t, ws.WithReadBufferSize(64))
	trailing := append(encodeFrame(bitFin|opText, []byte("early"), true),
		encodeFrame(bitFin|opBinary, bytes.Repeat([]byte{7}, 300), true)...)
	c := dial(t, srv, "/", handshake{trailing: trailing})
	if c.resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d", c.resp.StatusCode)
	}
	if got := c.mustRead(opText); string(got) != "early" {
		t.Fatalf("got %q", got)
	}
	if got := c.mustRead(opBinary); len(got) != 300 {
		t.Fatalf("got len %d", len(got))
	}
}

// TestFragmentation 验证分片重组、控制帧插入分片中间以及跨分片的 UTF-8 字符。
// TestFragmentation verifies reassembly, control frames between fragments and UTF-8
// characters split across fragments.
func TestFragmentation(t *testing.T) {
	srv, _ := newEchoServer(t)
	c := mustDial(t, srv)
	euro := []byte("€") // E2 82 AC
	c.send(false, opText, append([]byte("price "), euro[0]))
	c.send(true, opPing, []byte("mid"))
	c.send(false, opCont, euro[1:2])
	c.send(false, opCont, nil)
	c.send(true, opCont, append(euro[2:], " 5"...))
	if got := c.mustRead(opPong); string(got) != "mid" {
		t.Fatalf("pong = %q", got)
	}
	if got := c.mustRead(opText); string(got) != "price € 5" {
		t.Fatalf("message = %q", got)
	}
}

// TestPingPong 验证自动 pong、自定义 ping/pong 处理器以及服务端 Ping。
// TestPingPong verifies automatic pong, custom ping/pong handlers and server Ping.
func TestPingPong(t *testing.T) {
	t.Run("auto pong", func(t *testing.T) {
		srv, _ := newEchoServer(t)
		c := mustDial(t, srv)
		c.send(true, opPing, []byte("hello"))
		if got := c.mustRead(opPong); string(got) != "hello" {
			t.Fatalf("pong = %q", got)
		}
	})
	t.Run("custom handlers", func(t *testing.T) {
		pongs := make(chan string, 1)
		srv, errs := newEchoServer(t,
			ws.WithPingHandler(func(c *ws.Conn, p []byte) error {
				return c.WriteControl(ws.PongMessage, append([]byte("re:"), p...), time.Now().Add(time.Second))
			}),
			ws.WithPongHandler(func(_ *ws.Conn, p []byte) error {
				pongs <- string(p)
				if string(p) == "stop" {
					return io.ErrClosedPipe
				}
				return nil
			}),
		)
		c := mustDial(t, srv)
		c.send(true, opPing, []byte("x"))
		if got := c.mustRead(opPong); string(got) != "re:x" {
			t.Fatalf("pong = %q", got)
		}
		c.send(true, opPong, []byte("unsolicited"))
		if got := <-pongs; got != "unsolicited" {
			t.Fatalf("pong handler got %q", got)
		}
		c.send(true, opPong, []byte("stop"))
		<-pongs
		if err := recvErr(t, errs); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("server err = %v", err)
		}
	})
	t.Run("server ping", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := ws.Upgrade(w, r)
			if err != nil {
				return
			}
			defer func() { _ = c.Close(ws.CloseNormalClosure, "") }()
			_ = c.Ping([]byte("srv"))
			_, _, _ = c.ReadMessage()
		}))
		defer srv.Close()
		c := mustDial(t, srv)
		if got := c.mustRead(opPing); string(got) != "srv" {
			t.Fatalf("ping = %q", got)
		}
	})
}

// TestCloseHandshake 验证对端发起与服务端发起的关闭握手。
// TestCloseHandshake verifies peer-initiated and server-initiated close handshakes.
func TestCloseHandshake(t *testing.T) {
	t.Run("peer initiated", func(t *testing.T) {
		srv, errs := newEchoServer(t)
		c := mustDial(t, srv)
		c.send(true, opClose, closePayload(ws.CloseGoingAway, "bye"))
		c.expectClose(ws.CloseGoingAway)
		err := recvErr(t, errs)
		var ce *ws.CloseError
		if !errors.As(err, &ce) || ce.Code != ws.CloseGoingAway || ce.Text != "bye" {
			t.Fatalf("err = %v", err)
		}
		if !ws.IsCloseError(err, ws.CloseNormalClosure, ws.CloseGoingAway) || ws.IsCloseError(err, ws.CloseProtocolError) {
			t.Fatal("IsCloseError mismatch")
		}
	})
	t.Run("peer without status", func(t *testing.T) {
		srv, errs := newEchoServer(t)
		c := mustDial(t, srv)
		c.send(true, opClose, nil)
		c.expectClose(0)
		if err := recvErr(t, errs); !ws.IsCloseError(err, ws.CloseNoStatusReceived) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("server initiated", func(t *testing.T) {
		done := make(chan error, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := ws.Upgrade(w, r)
			if err != nil {
				return
			}
			err = c.Close(ws.CloseGoingAway, strings.Repeat("é", 100))
			if err2 := c.Close(ws.CloseNormalClosure, ""); err2 != nil {
				err = fmt.Errorf("second close: %w", err2)
			}
			if werr := c.WriteMessage(ws.TextMessage, []byte("x")); !errors.Is(werr, ws.ErrCloseSent) {
				err = fmt.Errorf("write after close = %v", werr)
			}
			done <- err
		}))
		defer srv.Close()
		c := mustDial(t, srv)
		reason := c.expectClose(ws.CloseGoingAway)
		if len(reason) > 123 || !strings.HasPrefix(reason, "éé") {
			t.Fatalf("reason len = %d", len(reason))
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("abnormal", func(t *testing.T) {
		srv, errs := newEchoServer(t)
		c := mustDial(t, srv)
		c.writeRaw(encodeFrame(bitFin|opText, []byte("partial"), true)[:5])
		_ = c.conn.Close()
		err := recvErr(t, errs)
		if !ws.IsCloseError(err, ws.CloseAbnormalClosure) || !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestProtocolViolations 验证各类协议违规以正确的关闭码关闭并返回对应哨兵错误。
// TestProtocolViolations verifies protocol violations close with the right code and
// return the matching sentinel error.
func TestProtocolViolations(t *testing.T) {
	invalidUTF8 := []byte{'a', 0xff, 'b'}
	tests := []struct {
		name  string
		opts  []ws.Option
		write func(c *testClient)
		code  int
		kind  error
	}{
		{"unmasked frame", nil, func(c *testClient) {
			c.writeRaw(encodeFrame(bitFin|opText, []byte("hi"), false))
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"rsv bits", nil, func(c *testClient) {
			c.writeRaw(encodeFrame(bitFin|0x40|opText, []byte("hi"), true))
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"reserved opcode", nil, func(c *testClient) {
			c.send(true, 0x3, []byte("hi"))
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"reserved control opcode", nil, func(c *testClient) {
			c.send(true, 0xB, nil)
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"fragmented control", nil, func(c *testClient) {
			c.send(false, opPing, []byte("x"))
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"control too large", nil, func(c *testClient) {
			c.send(true, opPing, bytes.Repeat([]byte("x"), 126))
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"continuation without start", nil, func(c *testClient) {
			c.send(true, opCont, []byte("x"))
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"new message inside fragmented", nil, func(c *testClient) {
			c.send(false, opText, []byte("a"))
			c.send(true, opBinary, []byte("b"))
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"close payload one byte", nil, func(c *testClient) {
			c.send(true, opClose, []byte{0x03})
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"invalid close code", nil, func(c *testClient) {
			c.send(true, opClose, closePayload(1005, ""))
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"length msb set", nil, func(c *testClient) {
			c.writeRaw([]byte{bitFin | opBinary, 0x80 | 127, 0x80, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4})
		}, ws.CloseProtocolError, ws.ErrProtocol},
		{"over read limit", []ws.Option{ws.WithReadLimit(10)}, func(c *testClient) {
			c.send(true, opBinary, bytes.Repeat([]byte("x"), 11))
		}, ws.CloseMessageTooBig, ws.ErrReadLimit},
		{"fragments over read limit", []ws.Option{ws.WithReadLimit(10)}, func(c *testClient) {
			c.send(false, opBinary, bytes.Repeat([]byte("x"), 6))
			c.send(true, opCont, bytes.Repeat([]byte("x"), 6))
		}, ws.CloseMessageTooBig, ws.ErrReadLimit},
		{"invalid utf8", nil, func(c *testClient) {
			c.send(true, opText, invalidUTF8)
		}, ws.CloseInvalidFramePayloadData, ws.ErrInvalidUTF8},
		{"truncated utf8 at end", nil, func(c *testClient) {
			c.send(false, opText, []byte("ok"))
			c.send(true, opCont, []byte{0xE2, 0x82})
		}, ws.CloseInvalidFramePayloadData, ws.ErrInvalidUTF8},
		{"invalid utf8 close reason", nil, func(c *testClient) {
			c.send(true, opClose, closePayload(ws.CloseNormalClosure, string(invalidUTF8)))
		}, ws.CloseInvalidFramePayloadData, ws.ErrInvalidUTF8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, errs := newEchoServer(t, tt.opts...)
			c := mustDial(t, srv)
			tt.write(c)
			c.expectClose(tt.code)
			err := recvErr(t, errs)
			if !errors.Is(err, tt.kind) || err.Error() == "" {
				t.Fatalf("err = %v, want %v", err, tt.kind)
			}
		})
	}
}

// TestReadLimitExact 验证恰好等于上限的消息被接受。
// TestReadLimitExact verifies a message exactly at the limit is accepted.
func TestReadLimitExact(t *testing.T) {
	srv, _ := newEchoServer(t, ws.WithReadLimit(10))
	c := mustDial(t, srv)
	c.send(false, opBinary, []byte("12345"))
	c.send(true, opCont, []byte("67890"))
	if got := c.mustRead(opBinary); string(got) != "1234567890" {
		t.Fatalf("got %q", got)
	}
}

// TestNextReader 验证流式读取、丢弃未读完的消息与失效 reader。
// TestNextReader verifies streaming reads, discarding an unfinished message and stale
// readers.
func TestNextReader(t *testing.T) {
	result := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Upgrade(w, r)
		if err != nil {
			result <- err
			return
		}
		defer func() { _ = c.Close(ws.CloseNormalClosure, "") }()
		result <- func() error {
			mt, r1, err := c.NextReader()
			if err != nil || mt != ws.BinaryMessage {
				return fmt.Errorf("first: %v %v", mt, err)
			}
			buf := make([]byte, 3)
			if n, err := io.ReadFull(r1, buf); err != nil || string(buf[:n]) != "abc" {
				return fmt.Errorf("partial read: %q %v", buf[:n], err)
			}
			if n, err := r1.Read(nil); n != 0 || err != nil {
				return fmt.Errorf("empty read: %d %v", n, err)
			}
			mt, r2, err := c.NextReader()
			if err != nil || mt != ws.TextMessage {
				return fmt.Errorf("second: %v %v", mt, err)
			}
			if _, err := r1.Read(buf); !errors.Is(err, ws.ErrStaleReader) {
				return fmt.Errorf("stale read: %v", err)
			}
			data, err := io.ReadAll(r2)
			if err != nil || string(data) != "next" {
				return fmt.Errorf("second data: %q %v", data, err)
			}
			if _, err := r2.Read(buf); err != io.EOF {
				return fmt.Errorf("read after EOF: %v", err)
			}
			return nil
		}()
	}))
	defer srv.Close()
	c := mustDial(t, srv)
	c.send(false, opBinary, []byte("abcdef"))
	c.send(true, opCont, []byte("ghi"))
	c.send(true, opText, []byte("next"))
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

// TestWriteValidation 验证写方法的参数校验。
// TestWriteValidation verifies argument checks of the write methods.
func TestWriteValidation(t *testing.T) {
	result := make(chan []error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Upgrade(w, r)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(ws.CloseNormalClosure, "") }()
		result <- []error{
			c.WriteMessage(ws.PingMessage, nil),
			c.WriteControl(ws.TextMessage, nil, time.Time{}),
			c.WriteControl(ws.PingMessage, make([]byte, 126), time.Time{}),
			c.WriteControl(ws.PongMessage, []byte("p"), time.Now().Add(-time.Second)),
		}
	}))
	defer srv.Close()
	mustDial(t, srv)
	errs := <-result
	want := []error{ws.ErrInvalidMessageType, ws.ErrInvalidMessageType, ws.ErrControlTooLarge, os.ErrDeadlineExceeded}
	for i, err := range errs {
		if !errors.Is(err, want[i]) {
			t.Errorf("case %d: err = %v, want %v", i, err, want[i])
		}
	}
}

// TestDeadlines 验证读写超时与地址访问器。
// TestDeadlines verifies read/write deadlines and address accessors.
func TestDeadlines(t *testing.T) {
	result := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Upgrade(w, r)
		if err != nil {
			result <- err
			return
		}
		defer func() { _ = c.Close(ws.CloseNormalClosure, "") }()
		result <- func() error {
			if c.RemoteAddr() == nil || c.LocalAddr() == nil || c.NetConn() == nil {
				return errors.New("nil address")
			}
			if err := c.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
				return err
			}
			if _, _, err := c.ReadMessage(); !errors.Is(err, os.ErrDeadlineExceeded) {
				return fmt.Errorf("read err = %v", err)
			}
			if _, _, err := c.ReadMessage(); !errors.Is(err, os.ErrDeadlineExceeded) {
				return fmt.Errorf("sticky read err = %v", err)
			}
			if err := c.SetWriteDeadline(time.Now().Add(-time.Second)); err != nil {
				return err
			}
			if err := c.WriteMessage(ws.TextMessage, []byte("x")); !errors.Is(err, os.ErrDeadlineExceeded) {
				return fmt.Errorf("write err = %v", err)
			}
			if err := c.WriteMessage(ws.TextMessage, []byte("x")); !errors.Is(err, os.ErrDeadlineExceeded) {
				return fmt.Errorf("sticky write err = %v", err)
			}
			return c.SetWriteDeadline(time.Time{})
		}()
	}))
	defer srv.Close()
	mustDial(t, srv)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

// TestConcurrentWrites 验证多个 goroutine 并发写数据帧与控制帧时帧不交错（配合 -race）。
// TestConcurrentWrites verifies frames never interleave when several goroutines write
// data and control frames concurrently (run with -race).
func TestConcurrentWrites(t *testing.T) {
	const writers, perWriter = 8, 50
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pinged := make(chan struct{})
		c, err := ws.Upgrade(w, r, ws.WithWriteBufferSize(512), ws.WithPingHandler(func(c *ws.Conn, p []byte) error {
			defer close(pinged)
			return c.WriteControl(ws.PongMessage, p, time.Now().Add(5*time.Second))
		}))
		if err != nil {
			return
		}
		defer func() { _ = c.Close(ws.CloseNormalClosure, "done") }()
		var wg sync.WaitGroup
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				msg := bytes.Repeat([]byte{byte('a' + i)}, 1000+i)
				for j := range perWriter {
					if err := c.WriteMessage(ws.BinaryMessage, msg); err != nil {
						t.Errorf("write: %v", err)
						return
					}
					if j%10 == 0 {
						if err := c.Ping([]byte{byte(i)}); err != nil {
							t.Errorf("ping: %v", err)
							return
						}
					}
				}
			}()
		}
		// 同时运行读 goroutine，处理客户端 ping。
		// Run the reader concurrently to handle client pings.
		go func() { _, _, _ = c.ReadMessage() }()
		wg.Wait()
		<-pinged
	}))
	defer srv.Close()
	c := mustDial(t, srv)
	c.send(true, opPing, []byte("client"))

	var data, pings, pongs int
	for {
		b0, p, err := c.readFrame()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		switch b0 {
		case bitFin | opBinary:
			if len(p) < 1000 || !bytes.Equal(p, bytes.Repeat(p[:1], len(p))) || len(p) != 1000+int(p[0]-'a') {
				t.Fatalf("corrupted message len %d", len(p))
			}
			data++
		case bitFin | opPing:
			pings++
		case bitFin | opPong:
			pongs++
		case bitFin | opClose:
			if data != writers*perWriter || pings != writers*perWriter/10 || pongs != 1 {
				t.Fatalf("data=%d pings=%d pongs=%d", data, pings, pongs)
			}
			return
		default:
			t.Fatalf("unexpected frame %#x", b0)
		}
	}
}
