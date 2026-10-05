package ws

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestMaskBytes 对比按字节掩码的朴素实现，覆盖各种偏移与长度。
// TestMaskBytes compares with a naive byte-wise implementation across offsets and lengths.
func TestMaskBytes(t *testing.T) {
	key := [4]byte{0x12, 0x34, 0x56, 0x78}
	for pos := 0; pos < 4; pos++ {
		for n := 0; n < 70; n++ {
			b := make([]byte, n)
			for i := range b {
				b[i] = byte(i * 7)
			}
			want := bytes.Clone(b)
			for i := range want {
				want[i] ^= key[(pos+i)&3]
			}
			next := maskBytes(key, pos, b)
			if !bytes.Equal(b, want) || next != (pos+n)&3 {
				t.Fatalf("pos %d n %d: mismatch (next %d)", pos, n, next)
			}
		}
	}
}

// TestFrameHeaderRoundTrip 验证帧头编码后可被解析回来。
// TestFrameHeaderRoundTrip verifies an encoded header parses back.
func TestFrameHeaderRoundTrip(t *testing.T) {
	key := [4]byte{1, 2, 3, 4}
	for _, length := range []int{0, 125, 126, math.MaxUint16, math.MaxUint16 + 1, 1 << 40} {
		for _, masked := range []bool{false, true} {
			for _, fin := range []bool{false, true} {
				hdr := appendFrameHeader(nil, fin, opBinary, length, masked, key)
				h, err := readFrameHeader(bufio.NewReader(bytes.NewReader(hdr)))
				if err != nil {
					t.Fatalf("len %d: %v", length, err)
				}
				if h.fin != fin || h.opcode != opBinary || h.masked != masked || h.length != int64(length) || h.rsv != 0 {
					t.Fatalf("len %d masked %v: got %+v", length, masked, h)
				}
				if masked && h.key != key {
					t.Fatalf("key = %v", h.key)
				}
			}
		}
	}
}

// TestReadFrameHeaderErrors 验证截断与非法长度。
// TestReadFrameHeaderErrors verifies truncation and invalid lengths.
func TestReadFrameHeaderErrors(t *testing.T) {
	full := appendFrameHeader(nil, true, opBinary, 1<<20, true, [4]byte{1, 2, 3, 4})
	for i := 0; i < len(full); i++ {
		if _, err := readFrameHeader(bufio.NewReader(bytes.NewReader(full[:i]))); err == nil {
			t.Fatalf("truncated at %d: no error", i)
		}
	}
	short := appendFrameHeader(nil, true, opBinary, 300, false, [4]byte{})
	if _, err := readFrameHeader(bufio.NewReader(bytes.NewReader(short[:3]))); err == nil {
		t.Fatal("truncated 16-bit length: no error")
	}
	bad := []byte{0x82, 127, 0x80, 0, 0, 0, 0, 0, 0, 0}
	if _, err := readFrameHeader(bufio.NewReader(bytes.NewReader(bad))); !errors.Is(err, ErrProtocol) {
		t.Fatalf("msb length err = %v", err)
	}
}

// TestUTF8Validator 验证增量 UTF-8 校验在任意切分下与 utf8.Valid 一致。
// TestUTF8Validator verifies incremental validation agrees with utf8.Valid under any split.
func TestUTF8Validator(t *testing.T) {
	inputs := [][]byte{
		[]byte("hello"),
		[]byte("héllo €𝄞 世界"),
		{0xff},
		{0xC3},
		{0xE2, 0x82},
		{0xE2, 0x28, 0xA1},
		{0xED, 0xA0, 0x80},              // 代理项 / surrogate
		{0xF4, 0x90, 0x80, 0x80},        // 超出范围 / out of range
		{0xC0, 0xAF},                    // 过长编码 / overlong
		{'a', 0xF0, 0x9D, 0x84, 0x9E},   // 𝄞
		{'a', 0xF0, 0x9D, 0x84},         // 截断 / truncated
		{0x80, 'a'},                     // 孤立续字节 / lone continuation
		[]byte(strings.Repeat("€", 20)), // 长输入 / long input
	}
	for _, in := range inputs {
		want := utf8.Valid(in)
		for chunk := 1; chunk <= len(in)+1; chunk++ {
			var v utf8Validator
			ok := true
			for i := 0; i < len(in) && ok; i += chunk {
				ok = v.write(in[i:min(i+chunk, len(in))])
			}
			ok = ok && v.done()
			if ok != want {
				t.Fatalf("input %x chunk %d: got %v, want %v", in, chunk, ok, want)
			}
		}
	}
}

// TestCloseCodes 验证关闭负载的构造与解析。
// TestCloseCodes verifies building and parsing close payloads.
func TestCloseCodes(t *testing.T) {
	if p := FormatCloseMessage(CloseNoStatusReceived, "x"); len(p) != 0 {
		t.Errorf("1005 payload = %v", p)
	}
	if p := FormatCloseMessage(0, "x"); len(p) != 0 {
		t.Errorf("0 payload = %v", p)
	}
	p := FormatCloseMessage(CloseNormalClosure, strings.Repeat("a", 200))
	if len(p) != MaxControlPayload {
		t.Errorf("len = %d", len(p))
	}
	code, text, ferr := parseClosePayload(FormatCloseMessage(4000, "bye"))
	if ferr != nil || code != 4000 || text != "bye" {
		t.Errorf("parse = %d %q %v", code, text, ferr)
	}
	for _, code := range []int{999, 1004, 1005, 1006, 1015, 2999, 5000} {
		if validReceivedCloseCode(code) {
			t.Errorf("code %d accepted", code)
		}
	}
	for _, code := range []int{1000, 1003, 1007, 1011, 1014, 3000, 4999} {
		if !validReceivedCloseCode(code) {
			t.Errorf("code %d rejected", code)
		}
	}
	if got := truncateUTF8("aé", 2); got != "a" {
		t.Errorf("truncateUTF8 = %q", got)
	}
}

// TestErrorStrings 验证错误与类型的字符串形式。
// TestErrorStrings verifies string forms of errors and types.
func TestErrorStrings(t *testing.T) {
	names := map[MessageType]string{TextMessage: "text", BinaryMessage: "binary", CloseMessage: "close",
		PingMessage: "ping", PongMessage: "pong", 3: "MessageType(3)"}
	for mt, want := range names {
		if mt.String() != want {
			t.Errorf("%d.String() = %q", int(mt), mt.String())
		}
	}
	ce := &CloseError{Code: 1006, Text: "x", err: io.ErrUnexpectedEOF}
	if !strings.Contains(ce.Error(), "1006 x: unexpected EOF") || !errors.Is(ce, io.ErrUnexpectedEOF) {
		t.Errorf("CloseError = %q", ce.Error())
	}
	if IsCloseError(io.EOF) || !IsCloseError(ce) {
		t.Error("IsCloseError type check")
	}
}

// TestOptions 验证选项默认值与非法值被忽略。
// TestOptions verifies option defaults and that invalid values are ignored.
func TestOptions(t *testing.T) {
	cfg := newConfig([]Option{nil, WithReadLimit(0), WithReadBufferSize(-1), WithWriteBufferSize(0),
		WithControlTimeout(0), WithHandshakeTimeout(-1), WithErrorHandler(nil)})
	if cfg.readLimit != DefaultReadLimit || cfg.readBufferSize != DefaultReadBufferSize ||
		cfg.writeBufferSize != DefaultWriteBufferSize || cfg.controlTimeout != DefaultControlTimeout ||
		cfg.handshakeTimeout != 0 || cfg.errorHandler == nil {
		t.Fatalf("cfg = %+v", cfg)
	}
	cfg = newConfig([]Option{WithReadLimit(5), WithReadBufferSize(10), WithWriteBufferSize(20),
		WithControlTimeout(time.Second), WithHandshakeTimeout(time.Second), WithSubprotocols("a")})
	if cfg.readLimit != 5 || cfg.readBufferSize != 10 || cfg.writeBufferSize != 20 ||
		cfg.controlTimeout != time.Second || cfg.handshakeTimeout != time.Second || cfg.subprotocols[0] != "a" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// pipeConns 用 net.Pipe 构造一对 client/server Conn。
// pipeConns builds a client/server Conn pair over net.Pipe.
func pipeConns(opts ...Option) (client, server *Conn) {
	a, b := net.Pipe()
	cfg := newConfig(opts)
	client = newConn(a, bufio.NewReader(a), bufio.NewWriterSize(a, 64), false, "", cfg)
	server = newConn(b, bufio.NewReader(b), bufio.NewWriter(b), true, "", cfg)
	return client, server
}

// TestClientModeConn 验证客户端方向（写掩码帧、读未掩码帧）与服务端互通。
// TestClientModeConn verifies the client direction (masked writes, unmasked reads)
// interoperates with the server.
func TestClientModeConn(t *testing.T) {
	client, server := pipeConns()
	defer func() { _ = client.Close(CloseNormalClosure, "") }()
	go func() {
		for {
			mt, data, err := server.ReadMessage()
			if err != nil {
				return
			}
			if err := server.WriteMessage(mt, data); err != nil {
				return
			}
		}
	}()
	payload := bytes.Repeat([]byte("xyz"), 100) // 大于写缓冲 / larger than the write buffer
	orig := bytes.Clone(payload)
	if err := client.WriteMessage(BinaryMessage, payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, orig) {
		t.Fatal("WriteMessage modified caller data")
	}
	mt, got, err := client.ReadMessage()
	if err != nil || mt != BinaryMessage || !bytes.Equal(got, orig) {
		t.Fatalf("echo = %v %d %v", mt, len(got), err)
	}
}

// TestClientRejectsMaskedFrame 验证客户端方向收到掩码帧时以 1002 失败。
// TestClientRejectsMaskedFrame verifies the client direction fails with 1002 on a masked
// frame.
func TestClientRejectsMaskedFrame(t *testing.T) {
	a, b := net.Pipe()
	cfg := newConfig(nil)
	reader := newConn(a, bufio.NewReader(a), bufio.NewWriter(a), false, "", cfg)
	writer := newConn(b, bufio.NewReader(b), bufio.NewWriter(b), false, "", cfg) // 同为客户端：写掩码帧 / also a client: writes masked frames
	go func() {
		_ = writer.WriteMessage(TextMessage, []byte("masked"))
		_, _, _ = writer.ReadMessage() // 接收 reader 发出的关闭帧 / drain the close frame
	}()
	if _, _, err := reader.ReadMessage(); !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v", err)
	}
	_ = writer.Close(CloseNormalClosure, "")
}

// TestWriteLockTimeout 验证写锁被占用时 WriteControl 按 deadline 超时。
// TestWriteLockTimeout verifies WriteControl times out on its deadline while the write
// lock is held.
func TestWriteLockTimeout(t *testing.T) {
	client, server := pipeConns(WithControlTimeout(20 * time.Millisecond))
	defer func() { _ = client.Close(0, ""); _ = server.Close(0, "") }()
	server.wlock <- struct{}{}
	err := server.WriteControl(PingMessage, nil, time.Now().Add(20*time.Millisecond))
	<-server.wlock
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

// BenchmarkFrameEncode 衡量服务端帧编码（帧头 + 负载写入缓冲）。
// BenchmarkFrameEncode measures server frame encoding (header + payload into a buffer).
func BenchmarkFrameEncode(b *testing.B) {
	for _, n := range []int{64, 4096, 65536} {
		b.Run(byteSize(n), func(b *testing.B) {
			payload := make([]byte, n)
			bw := bufio.NewWriterSize(io.Discard, DefaultWriteBufferSize)
			var hdr [maxFrameHeaderSize]byte
			b.SetBytes(int64(n))
			b.ReportAllocs()
			for b.Loop() {
				_, _ = bw.Write(appendFrameHeader(hdr[:0], true, opBinary, n, false, [4]byte{}))
				_, _ = bw.Write(payload)
				_ = bw.Flush()
			}
		})
	}
}

// BenchmarkFrameDecode 衡量服务端读取客户端掩码帧（帧头解析 + 解掩码 + 读取消息）。
// BenchmarkFrameDecode measures reading masked client frames on the server (header
// parsing + unmasking + message read).
func BenchmarkFrameDecode(b *testing.B) {
	for _, n := range []int{64, 4096, 65536} {
		b.Run(byteSize(n), func(b *testing.B) {
			payload := make([]byte, n)
			frame := appendFrameHeader(nil, true, opBinary, n, true, [4]byte{1, 2, 3, 4})
			frame = append(frame, payload...)
			src := bytes.NewReader(frame)
			c := &Conn{server: true, cfg: newConfig(nil), br: bufio.NewReaderSize(src, DefaultReadBufferSize)}
			buf := make([]byte, n)
			b.SetBytes(int64(n))
			b.ReportAllocs()
			for b.Loop() {
				src.Reset(frame)
				c.br.Reset(src)
				_, r, err := c.NextReader()
				if err != nil {
					b.Fatal(err)
				}
				if _, err := io.ReadFull(r, buf); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMaskBytes 衡量掩码吞吐。
// BenchmarkMaskBytes measures masking throughput.
func BenchmarkMaskBytes(b *testing.B) {
	buf := make([]byte, 4096)
	b.SetBytes(int64(len(buf)))
	for b.Loop() {
		maskBytes([4]byte{1, 2, 3, 4}, 1, buf)
	}
}

// byteSize 返回基准子测试名。
// byteSize returns a benchmark sub-test name.
func byteSize(n int) string { return strconv.Itoa(n) + "B" }
