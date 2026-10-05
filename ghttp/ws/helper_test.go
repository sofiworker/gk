package ws_test

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sofiworker/gk/ghttp/ws"
)

// testKey 是 RFC 6455 §1.3 的示例 key。
// testKey is the sample key from RFC 6455 §1.3.
const testKey = "dGhlIHNhbXBsZSBub25jZQ=="

// 测试用 opcode 与帧头位。
// Opcodes and header bits used by tests.
const (
	opCont   byte = 0x0
	opText   byte = 0x1
	opBinary byte = 0x2
	opClose  byte = 0x8
	opPing   byte = 0x9
	opPong   byte = 0xA
	bitFin   byte = 0x80
)

// handshake 描述测试客户端发出的握手请求；headers 中值为空串表示删除默认头。
// handshake describes the handshake sent by the test client; an empty value in headers
// deletes a default header.
type handshake struct {
	method   string
	proto    string
	headers  map[string]string
	trailing []byte
}

// testClient 是手写的最小 WebSocket 客户端：手写握手、掩码帧，不依赖被测包。
// testClient is a minimal hand-written WebSocket client: handwritten handshake and
// masked frames, independent of the package under test.
type testClient struct {
	t    testing.TB
	conn net.Conn
	br   *bufio.Reader
	resp *http.Response
	body []byte
}

// dial 连接 srv 并发送握手请求，读取响应但不断言状态码。
// dial connects to srv, sends the handshake and reads the response without asserting
// the status code.
func dial(t testing.TB, srv *httptest.Server, path string, h handshake) *testClient {
	t.Helper()
	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	method, proto := h.method, h.proto
	if method == "" {
		method = http.MethodGet
	}
	if proto == "" {
		proto = "HTTP/1.1"
	}
	hdr := map[string]string{
		"Host":                  addr,
		"Connection":            "Upgrade",
		"Upgrade":               "websocket",
		"Sec-WebSocket-Version": "13",
		"Sec-WebSocket-Key":     testKey,
	}
	for k, v := range h.headers {
		hdr[k] = v
	}
	keys := make([]string, 0, len(hdr))
	for k := range hdr {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var sb strings.Builder
	sb.WriteString(method + " " + path + " " + proto + "\r\n")
	for _, k := range keys {
		if hdr[k] != "" {
			sb.WriteString(k + ": " + hdr[k] + "\r\n")
		}
	}
	sb.WriteString("\r\n")
	req := append([]byte(sb.String()), h.trailing...)
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	c := &testClient{t: t, conn: conn, br: br, resp: resp}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		c.body, _ = io.ReadAll(resp.Body)
	}
	return c
}

// mustDial 发起默认握手并断言 101。
// mustDial performs the default handshake and asserts 101.
func mustDial(t testing.TB, srv *httptest.Server) *testClient {
	t.Helper()
	c := dial(t, srv, "/", handshake{})
	if c.resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, body = %q", c.resp.StatusCode, c.body)
	}
	return c
}

// encodeFrame 编码一个帧；b0 包含 FIN/RSV/opcode。
// encodeFrame encodes a frame; b0 carries FIN/RSV/opcode.
func encodeFrame(b0 byte, payload []byte, masked bool) []byte {
	var out []byte
	var mbit byte
	if masked {
		mbit = 0x80
	}
	switch n := len(payload); {
	case n <= 125:
		out = append(out, b0, mbit|byte(n))
	case n <= 0xFFFF:
		out = append(out, b0, mbit|126)
		out = binary.BigEndian.AppendUint16(out, uint16(n))
	default:
		out = append(out, b0, mbit|127)
		out = binary.BigEndian.AppendUint64(out, uint64(n))
	}
	if !masked {
		return append(out, payload...)
	}
	var key [4]byte
	_, _ = rand.Read(key[:])
	out = append(out, key[:]...)
	for i, b := range payload {
		out = append(out, b^key[i%4])
	}
	return out
}

// writeRaw 直接写出字节。
// writeRaw writes raw bytes.
func (c *testClient) writeRaw(b []byte) {
	c.t.Helper()
	if _, err := c.conn.Write(b); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// send 写出一个带掩码的帧。
// send writes one masked frame.
func (c *testClient) send(fin bool, op byte, payload []byte) {
	c.t.Helper()
	b0 := op
	if fin {
		b0 |= bitFin
	}
	c.writeRaw(encodeFrame(b0, payload, true))
}

// readFrame 读取一个服务端帧并断言其未掩码。
// readFrame reads one server frame and asserts it is unmasked.
func (c *testClient) readFrame() (b0 byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(c.br, hdr[:]); err != nil {
		return 0, nil, err
	}
	if hdr[1]&0x80 != 0 {
		c.t.Errorf("server frame is masked")
	}
	n := uint64(hdr[1] & 0x7F)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return 0, nil, err
	}
	return hdr[0], payload, nil
}

// mustRead 读取一个帧并断言 FIN 与 opcode。
// mustRead reads a frame and asserts FIN and the opcode.
func (c *testClient) mustRead(op byte) []byte {
	c.t.Helper()
	b0, p, err := c.readFrame()
	if err != nil {
		c.t.Fatalf("read frame: %v", err)
	}
	if b0 != bitFin|op {
		c.t.Fatalf("frame byte0 = %#x, want %#x (payload %q)", b0, bitFin|op, p)
	}
	return p
}

// expectClose 读取关闭帧并断言关闭码，随后断言服务端关闭了 TCP。
// expectClose reads a close frame, asserts the code and that the server closed TCP.
func (c *testClient) expectClose(code int) string {
	c.t.Helper()
	p := c.mustRead(opClose)
	if code == 0 {
		if len(p) != 0 {
			c.t.Fatalf("close payload = %q, want empty", p)
		}
		return ""
	}
	if len(p) < 2 {
		c.t.Fatalf("close payload = %q, want code %d", p, code)
	}
	if got := int(binary.BigEndian.Uint16(p)); got != code {
		c.t.Fatalf("close code = %d (%q), want %d", got, p[2:], code)
	}
	if _, _, err := c.readFrame(); !errors.Is(err, io.EOF) {
		c.t.Fatalf("after close: err = %v, want EOF", err)
	}
	return string(p[2:])
}

// closePayload 构造关闭帧负载。
// closePayload builds a close frame payload.
func closePayload(code int, text string) []byte {
	return append(binary.BigEndian.AppendUint16(nil, uint16(code)), text...)
}

// newEchoServer 启动回显服务器；服务端读循环结束的错误（或握手错误）发送到返回的 channel。
// newEchoServer starts an echo server; the error ending the server read loop (or the
// handshake error) is sent to the returned channel.
func newEchoServer(t testing.TB, opts ...ws.Option) (*httptest.Server, <-chan error) {
	t.Helper()
	errs := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Upgrade(w, r, opts...)
		if err != nil {
			errs <- err
			return
		}
		defer func() { _ = c.Close(ws.CloseNormalClosure, "") }()
		for {
			mt, data, err := c.ReadMessage()
			if err != nil {
				errs <- err
				return
			}
			if err := c.WriteMessage(mt, data); err != nil {
				errs <- err
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, errs
}

// recvErr 等待服务端错误。
// recvErr waits for the server error.
func recvErr(t testing.TB, errs <-chan error) error {
	t.Helper()
	select {
	case err := <-errs:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for server error")
		return nil
	}
}
