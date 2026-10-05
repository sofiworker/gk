package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sofiworker/gk/ghttp"
	"github.com/sofiworker/gk/ghttp/ws"
)

// TestGHTTPRawRoute 端到端验证通过 ghttp.Raw 路由升级：*ghttp.Response 可直接作为
// http.ResponseWriter 交给 Upgrade，路径参数在升级后仍可使用。
// TestGHTTPRawRoute verifies upgrading through a ghttp.Raw route end to end:
// *ghttp.Response is passed to Upgrade as an http.ResponseWriter and path parameters
// remain usable after the upgrade.
func TestGHTTPRawRoute(t *testing.T) {
	s := ghttp.NewServer()
	err := s.Register(ghttp.Raw(http.MethodGet, "/ws/:room", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		conn, err := ws.Upgrade(resp, req.Raw, ws.WithSubprotocols("chat"))
		if err != nil {
			return nil // Upgrade 已写出 4xx / Upgrade already wrote the 4xx response
		}
		defer func() { _ = conn.Close(ws.CloseNormalClosure, "") }()
		room := req.Params.Get("room")
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return nil
			}
			if err := conn.WriteMessage(mt, append([]byte(room+":"), data...)); err != nil {
				return nil
			}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := dial(t, srv, "/ws/lobby", handshake{headers: map[string]string{"Sec-WebSocket-Protocol": "chat"}})
	if c.resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, body = %q", c.resp.StatusCode, c.body)
	}
	if got := c.resp.Header.Get("Sec-WebSocket-Protocol"); got != "chat" {
		t.Fatalf("subprotocol = %q", got)
	}
	c.send(true, opText, []byte("hi"))
	if got := c.mustRead(opText); string(got) != "lobby:hi" {
		t.Fatalf("echo = %q", got)
	}
	c.send(true, opClose, closePayload(ws.CloseNormalClosure, ""))
	c.expectClose(ws.CloseNormalClosure)

	// 非升级请求经由同一路由得到 400。
	// A non-upgrade request through the same route gets 400.
	bad := dial(t, srv, "/ws/lobby", handshake{headers: map[string]string{"Upgrade": ""}})
	if bad.resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", bad.resp.StatusCode)
	}
}
