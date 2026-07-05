package ghttp

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gwebsocket "github.com/gorilla/websocket"
)

func TestWebSocketServerEchoJSON(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type message struct {
		Text string `json:"text"`
	}

	Route[struct{}, struct{}](app).
		GET("/ws").
		ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
			var in message
			if err := conn.ReadJSON(&in); err != nil {
				return err
			}
			return conn.WriteJSON(message{Text: "echo:" + in.Text})
		})

	ts := httptest.NewServer(app)
	defer ts.Close()

	conn := dialTestWebSocket(t, ts.URL+"/ws")
	defer conn.Close()

	if err := conn.WriteJSON(message{Text: "hello"}); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}
	var out message
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out.Text != "echo:hello" {
		t.Fatalf("Text = %q, want echo:hello", out.Text)
	}
}

func TestWebSocketServerPassesParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type output struct {
		ID    string `json:"id"`
		Trace string `json:"trace"`
	}

	Route[struct{}, struct{}](app).
		GET("/ws/{id}").
		ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
			return conn.WriteJSON(output{
				ID:    params.Path("id"),
				Trace: params.Query("trace"),
			})
		})

	ts := httptest.NewServer(app)
	defer ts.Close()

	conn := dialTestWebSocket(t, ts.URL+"/ws/42?trace=abc")
	defer conn.Close()

	var out output
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out.ID != "42" || out.Trace != "abc" {
		t.Fatalf("out = %#v, want id and trace", out)
	}
}

func TestWebSocketServerRejectsPlainHTTP(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(context.Context, Params, *WebSocketConn) error {
		return nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestClientWebSocketEchoJSON(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type message struct {
		Text string `json:"text"`
	}

	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
		var in message
		if err := conn.ReadJSON(&in); err != nil {
			return err
		}
		return conn.WriteJSON(message{Text: "server:" + in.Text})
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	client := NewClient()
	conn, err := client.WebSocket(httpToWSURL(ts.URL + "/ws"))
	if err != nil {
		t.Fatalf("WebSocket failed: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(message{Text: "hello"}); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}
	var out message
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out.Text != "server:hello" {
		t.Fatalf("Text = %q, want server:hello", out.Text)
	}
}

func TestClientWebSocketSubprotocols(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type output struct {
		Protocol string `json:"protocol"`
	}

	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
		return conn.WriteJSON(output{Protocol: params.Header("Sec-WebSocket-Protocol")})
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	client := NewClient()
	conn, err := client.WebSocket(httpToWSURL(ts.URL+"/ws"), WithWebSocketSubprotocols([]string{"json"}))
	if err != nil {
		t.Fatalf("WebSocket failed: %v", err)
	}
	defer conn.Close()

	var out output
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out.Protocol != "json" {
		t.Fatalf("Protocol = %q, want json", out.Protocol)
	}
}

func TestClientWebSocketTLS(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type message struct {
		Text string `json:"text"`
	}

	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
		return conn.WriteJSON(message{Text: "secure"})
	})

	ts := httptest.NewTLSServer(app)
	defer ts.Close()

	transport, ok := ts.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("test client transport = %T, want *http.Transport", ts.Client().Transport)
	}

	client := NewClient()
	conn, err := client.WebSocket(httpToWSURL(ts.URL+"/ws"), WithWebSocketTLS(transport.TLSClientConfig))
	if err != nil {
		t.Fatalf("WebSocket failed: %v", err)
	}
	defer conn.Close()

	var out message
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out.Text != "secure" {
		t.Fatalf("Text = %q, want secure", out.Text)
	}
}

func dialTestWebSocket(t *testing.T, rawURL string) *WebSocketConn {
	t.Helper()

	conn, _, err := gwebsocket.DefaultDialer.Dial(httpToWSURL(rawURL), nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	return &WebSocketConn{conn: conn}
}

func httpToWSURL(rawURL string) string {
	return strings.NewReplacer("https://", "wss://", "http://", "ws://").Replace(rawURL)
}

func TestWebSocketOptionsConfigureClient(t *testing.T) {
	tlsConfig := &tls.Config{ServerName: "example.test"}
	cfg := &clientWebSocketConfig{}
	WithWebSocketTLS(tlsConfig)(cfg)
	WithWebSocketSubprotocols([]string{"json"})(cfg)
	WithWebSocketHandshakeTimeout(2 * time.Second)(cfg)
	WithWebSocketReadBufferSize(1024)(cfg)
	WithWebSocketWriteBufferSize(2048)(cfg)

	if cfg.tlsConfig != tlsConfig {
		t.Fatalf("tlsConfig was not set")
	}
	if len(cfg.subprotocols) != 1 || cfg.subprotocols[0] != "json" {
		t.Fatalf("subprotocols = %#v, want [json]", cfg.subprotocols)
	}
	if cfg.handshakeTimeout != 2*time.Second {
		t.Fatalf("handshakeTimeout = %s, want 2s", cfg.handshakeTimeout)
	}
	if cfg.readBufferSize != 1024 {
		t.Fatalf("readBufferSize = %d, want 1024", cfg.readBufferSize)
	}
	if cfg.writeBufferSize != 2048 {
		t.Fatalf("writeBufferSize = %d, want 2048", cfg.writeBufferSize)
	}
}
