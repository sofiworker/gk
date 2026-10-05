package ws_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sofiworker/gk/ghttp/ws"
)

// TestAcceptKey 使用 RFC 6455 示例验证 Accept 计算。
// TestAcceptKey verifies the Accept computation with the RFC 6455 example.
func TestAcceptKey(t *testing.T) {
	if got, want := ws.AcceptKey(testKey), "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="; got != want {
		t.Fatalf("AcceptKey = %q, want %q", got, want)
	}
}

// TestUpgradeSuccess 验证成功握手的响应头。
// TestUpgradeSuccess verifies the response headers of a successful handshake.
func TestUpgradeSuccess(t *testing.T) {
	srv, _ := newEchoServer(t, ws.WithResponseHeader(http.Header{
		"X-Test":     {"1"},
		"Connection": {"close"},  // 握手头被忽略 / handshake header ignored
		"Bad:Name":   {"x"},      // 非法名被忽略 / invalid name ignored
		"X-Inject":   {"a\r\nb"}, // 含换行的值被忽略 / value with CRLF ignored
	}))
	c := mustDial(t, srv)
	h := c.resp.Header
	if got := h.Get("Sec-WebSocket-Accept"); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("Accept = %q", got)
	}
	if !strings.EqualFold(h.Get("Upgrade"), "websocket") || !strings.EqualFold(h.Get("Connection"), "upgrade") {
		t.Errorf("Upgrade/Connection = %q/%q", h.Get("Upgrade"), h.Get("Connection"))
	}
	if h.Get("X-Test") != "1" || h.Get("X-Inject") != "" || h.Get("Sec-WebSocket-Protocol") != "" {
		t.Errorf("headers = %v", h)
	}
}

// TestUpgradeFailures 验证各类握手失败的状态码与错误。
// TestUpgradeFailures verifies status codes and errors of failed handshakes.
func TestUpgradeFailures(t *testing.T) {
	tests := []struct {
		name        string
		hs          handshake
		status      int
		kind        error
		wantVersion bool
	}{
		{"method", handshake{method: http.MethodPost}, http.StatusMethodNotAllowed, ws.ErrBadHandshake, false},
		{"http10", handshake{proto: "HTTP/1.0"}, http.StatusBadRequest, ws.ErrBadHandshake, true},
		{"no connection", handshake{headers: map[string]string{"Connection": ""}}, http.StatusBadRequest, ws.ErrBadHandshake, true},
		{"connection keep-alive", handshake{headers: map[string]string{"Connection": "keep-alive"}}, http.StatusBadRequest, ws.ErrBadHandshake, true},
		{"no upgrade", handshake{headers: map[string]string{"Upgrade": ""}}, http.StatusBadRequest, ws.ErrBadHandshake, true},
		{"bad version", handshake{headers: map[string]string{"Sec-WebSocket-Version": "8"}}, http.StatusUpgradeRequired, ws.ErrBadHandshake, true},
		{"no key", handshake{headers: map[string]string{"Sec-WebSocket-Key": ""}}, http.StatusBadRequest, ws.ErrBadHandshake, true},
		{"short key", handshake{headers: map[string]string{"Sec-WebSocket-Key": "c2hvcnQ="}}, http.StatusBadRequest, ws.ErrBadHandshake, true},
		{"non-base64 key", handshake{headers: map[string]string{"Sec-WebSocket-Key": "!!!!!!!!!!!!!!!!!!!!!!!!"}}, http.StatusBadRequest, ws.ErrBadHandshake, true},
		{"cross origin", handshake{headers: map[string]string{"Origin": "http://evil.example"}}, http.StatusForbidden, ws.ErrOriginNotAllowed, false},
		{"bad origin url", handshake{headers: map[string]string{"Origin": "http://[::1"}}, http.StatusForbidden, ws.ErrOriginNotAllowed, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, errs := newEchoServer(t)
			c := dial(t, srv, "/", tt.hs)
			if c.resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", c.resp.StatusCode, tt.status)
			}
			if got := c.resp.Header.Get("Sec-WebSocket-Version"); (got == "13") != tt.wantVersion {
				t.Errorf("Sec-WebSocket-Version = %q", got)
			}
			if tt.status == http.StatusMethodNotAllowed && c.resp.Header.Get("Allow") != http.MethodGet {
				t.Errorf("Allow = %q", c.resp.Header.Get("Allow"))
			}
			err := recvErr(t, errs)
			if !errors.Is(err, tt.kind) {
				t.Fatalf("err = %v, want %v", err, tt.kind)
			}
			var he *ws.HandshakeError
			if !errors.As(err, &he) || he.Status != tt.status || he.Reason == "" || he.Error() == "" {
				t.Fatalf("HandshakeError = %+v", he)
			}
		})
	}
}

// TestUpgradeOrigin 验证默认同源策略与自定义校验。
// TestUpgradeOrigin verifies the default same-origin policy and custom checks.
func TestUpgradeOrigin(t *testing.T) {
	t.Run("same host", func(t *testing.T) {
		srv, _ := newEchoServer(t)
		host := strings.TrimPrefix(srv.URL, "http://")
		c := dial(t, srv, "/", handshake{headers: map[string]string{"Origin": "http://" + strings.ToUpper(host)}})
		if c.resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status = %d", c.resp.StatusCode)
		}
	})
	t.Run("custom allow", func(t *testing.T) {
		srv, _ := newEchoServer(t, ws.WithCheckOrigin(func(r *http.Request) bool {
			return r.Header.Get("Origin") == "https://app.example"
		}))
		c := dial(t, srv, "/", handshake{headers: map[string]string{"Origin": "https://app.example"}})
		if c.resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status = %d", c.resp.StatusCode)
		}
	})
	t.Run("nil restores default", func(t *testing.T) {
		srv, _ := newEchoServer(t, ws.WithCheckOrigin(nil))
		c := dial(t, srv, "/", handshake{headers: map[string]string{"Origin": "https://evil.example"}})
		if c.resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d", c.resp.StatusCode)
		}
	})
}

// TestUpgradeErrorHandler 验证自定义握手失败响应。
// TestUpgradeErrorHandler verifies a custom handshake failure response.
func TestUpgradeErrorHandler(t *testing.T) {
	var gotStatus int
	srv, _ := newEchoServer(t, ws.WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, status int, err error) {
		gotStatus = status
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(err.Error()))
	}))
	c := dial(t, srv, "/", handshake{headers: map[string]string{"Sec-WebSocket-Version": "7"}})
	if c.resp.StatusCode != http.StatusTeapot || gotStatus != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, handler got %d", c.resp.StatusCode, gotStatus)
	}
	if !strings.Contains(string(c.body), "bad handshake") {
		t.Fatalf("body = %q", c.body)
	}
}

// TestUpgradeSubprotocol 验证子协议协商（服务端偏好优先）。
// TestUpgradeSubprotocol verifies subprotocol negotiation (server preference first).
func TestUpgradeSubprotocol(t *testing.T) {
	tests := []struct {
		name    string
		server  []string
		offered string
		want    string
	}{
		{"server preference", []string{"c", "b", "a"}, "a, b", "b"},
		{"no match", []string{"x"}, "a, b", ""},
		{"none offered", []string{"x"}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := ws.Upgrade(w, r, ws.WithSubprotocols(tt.server...))
				if err != nil {
					got <- "error: " + err.Error()
					return
				}
				got <- c.Subprotocol()
				_ = c.Close(ws.CloseNormalClosure, "")
			}))
			defer srv.Close()
			c := dial(t, srv, "/", handshake{headers: map[string]string{"Sec-WebSocket-Protocol": tt.offered}})
			if c.resp.StatusCode != http.StatusSwitchingProtocols {
				t.Fatalf("status = %d", c.resp.StatusCode)
			}
			if h := c.resp.Header.Get("Sec-WebSocket-Protocol"); h != tt.want {
				t.Errorf("header = %q, want %q", h, tt.want)
			}
			select {
			case s := <-got:
				if s != tt.want {
					t.Errorf("Subprotocol() = %q, want %q", s, tt.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timeout")
			}
		})
	}
}

// TestUpgradeNotHijackable 验证无法接管连接时返回 500 与错误。
// TestUpgradeNotHijackable verifies 500 and an error when the connection cannot be hijacked.
func TestUpgradeNotHijackable(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", testKey)
	rec := httptest.NewRecorder()
	c, err := ws.Upgrade(rec, r)
	if err == nil || c != nil {
		t.Fatalf("Upgrade = %v, %v", c, err)
	}
	if !errors.Is(err, http.ErrNotSupported) || rec.Code != http.StatusInternalServerError {
		t.Fatalf("err = %v, code = %d", err, rec.Code)
	}
}

// TestHelpers 验证 IsWebSocketUpgrade、Subprotocols 与 SameOrigin。
// TestHelpers verifies IsWebSocketUpgrade, Subprotocols and SameOrigin.
func TestHelpers(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	if ws.IsWebSocketUpgrade(r) {
		t.Error("plain request reported as upgrade")
	}
	r.Header.Set("Connection", "keep-alive, Upgrade")
	r.Header.Set("Upgrade", "WebSocket")
	if !ws.IsWebSocketUpgrade(r) {
		t.Error("upgrade request not detected")
	}

	r.Header.Add("Sec-WebSocket-Protocol", "a, b")
	r.Header.Add("Sec-WebSocket-Protocol", " ,c")
	if got := ws.Subprotocols(r); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("Subprotocols = %v", got)
	}

	origins := map[string]bool{"": true, "http://example.com": true, "https://EXAMPLE.com": true, "http://other.com": false, "%zz": false}
	for origin, want := range origins {
		r.Header.Set("Origin", origin)
		if got := ws.SameOrigin(r); got != want {
			t.Errorf("SameOrigin(%q) = %v, want %v", origin, got, want)
		}
	}
}
