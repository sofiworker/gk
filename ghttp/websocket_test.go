package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Response.Hijack 在底层不支持 http.Hijacker 时返回 ErrNotHijackable。
// httptest.ResponseRecorder 不实现 Hijacker,正好触发该分支。
func TestResponseHijackUnsupported(t *testing.T) {
	resp := &Response{ResponseWriter: httptest.NewRecorder()}
	_, _, err := resp.Hijack()
	if !errors.Is(err, ErrNotHijackable) {
		t.Fatalf("Hijack err = %v, want ErrNotHijackable", err)
	}
}

// ServeWS 端到端:升级成功后回声,客户端收到相同消息;连接关闭后 handler 退出。
func TestServeWSEcho(t *testing.T) {
	s := New()
	if err := ServeWS(s, "/ws", nil, func(ctx context.Context, req *Request, conn *websocket.Conn) error {
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return err // 客户端关闭 → 正常退出
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return err
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := dialWS(t, srv.URL, "/ws", nil)
	defer c.Close()

	if err := c.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_, got, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ping" {
		t.Errorf("echo = %q, want ping", got)
	}
}

// Upgrade 语法糖:在 RawHandle 内手动升级也可用,拿到 *websocket.Conn。
func TestWSUpgradeInRawHandle(t *testing.T) {
	s := New()
	up := NewWSUpgrader(WithWSReadBufferSize(2048), WithWSWriteBufferSize(2048))
	if err := s.RawHandle(http.MethodGet, "/raw-ws", func(ctx context.Context, req *Request, resp *Response) error {
		conn, err := up.Upgrade(resp, req, nil)
		if err != nil {
			return err
		}
		defer conn.Close()
		return conn.WriteMessage(websocket.TextMessage, []byte("hi from server"))
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := dialWS(t, srv.URL, "/raw-ws", nil)
	defer c.Close()
	_, got, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi from server" {
		t.Errorf("got %q", got)
	}
}

// CheckOrigin 拒绝:自定义 CheckOrigin 返回 false 时握手失败(HTTP 403),不建立连接。
func TestWSCheckOriginRejects(t *testing.T) {
	s := New()
	up := NewWSUpgrader(WithWSCheckOrigin(func(r *http.Request) bool { return false }))
	if err := ServeWS(s, "/ws", up, func(ctx context.Context, req *Request, conn *websocket.Conn) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": {"http://evil.example"}})
	if err == nil {
		t.Fatal("expected handshake rejection, got success")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Errorf("reject status = %d, want 403", code)
	}
}

// Subprotocol 协商:服务端声明支持的子协议,握手后 conn.Subprotocol() 反映所选。
func TestWSSubprotocol(t *testing.T) {
	s := New()
	up := NewWSUpgrader(WithWSSubprotocols("chat.v1", "chat.v2"))
	if err := ServeWS(s, "/ws", up, func(ctx context.Context, req *Request, conn *websocket.Conn) error {
		return conn.WriteMessage(websocket.TextMessage, []byte(conn.Subprotocol()))
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	dialer := websocket.Dialer{Subprotocols: []string{"chat.v2"}, HandshakeTimeout: 2 * time.Second}
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := c.Subprotocol(); got != "chat.v2" {
		t.Errorf("client subprotocol = %q, want chat.v2", got)
	}
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "chat.v2" {
		t.Errorf("server-selected subprotocol = %q, want chat.v2", msg)
	}
}

// dialWS 拨号建立到 srvURL+path 的 WebSocket 连接。
func dialWS(t *testing.T, srvURL, path string, header http.Header) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srvURL, "http") + path
	c, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	return c
}
