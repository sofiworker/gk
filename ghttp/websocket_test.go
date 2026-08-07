package ghttp

import (
	"context"
	"crypto/tls"
	"errors"
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

func TestWebSocketServerUnderTimeoutMiddlewareUpgrades(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Timeout(5 * time.Second))

	type message struct {
		Text string `json:"text"`
	}
	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
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

func TestWebSocketServerRawMessages(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		return conn.WriteMessage(messageType, append([]byte("echo:"), data...))
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	conn := dialTestWebSocket(t, ts.URL+"/ws")
	defer conn.Close()
	if err := conn.WriteMessage(TextMessage, []byte("hi")); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}
	if messageType != TextMessage {
		t.Fatalf("message type = %d, want %d", messageType, TextMessage)
	}
	if string(data) != "echo:hi" {
		t.Fatalf("data = %q, want echo:hi", data)
	}
}

func TestWebSocketServerNegotiatedSubprotocol(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithServerWebSocketSubprotocols([]string{"chat", "json"}))

	type output struct {
		Protocol string `json:"protocol"`
	}
	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
		return conn.WriteJSON(output{Protocol: conn.Subprotocol()})
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
		t.Fatalf("protocol = %q, want json", out.Protocol)
	}
}

func TestWebSocketServerRouteOriginOverride(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).
		GET("/open").
		WebSocketCheckOrigin(func(r *http.Request) bool {
			return r.Header.Get("Origin") == "https://allowed.test"
		}).
		ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
			return conn.WriteJSON(map[string]any{"ok": true})
		})

	ts := httptest.NewServer(app)
	defer ts.Close()

	allowed := dialTestWebSocketWithHeader(t, ts.URL+"/open", http.Header{
		"Origin": {"https://allowed.test"},
	})
	defer allowed.Close()
	var out map[string]any
	if err := allowed.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("out = %#v, want ok=true", out)
	}

	_, _, err := gwebsocket.DefaultDialer.Dial(httpToWSURL(ts.URL+"/open"), http.Header{
		"Origin": {"https://denied.test"},
	})
	if err == nil {
		t.Fatal("dial with denied origin should fail")
	}
}

func TestWebSocketServerReadJSONContextCanceled(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	errCh := make(chan error, 1)
	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
		ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		var msg struct {
			Text string `json:"text"`
		}
		errCh <- conn.ReadJSONContext(ctx, &msg)
		return nil
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	conn := dialTestWebSocket(t, ts.URL+"/ws")
	defer conn.Close()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ReadJSONContext error = %v, want DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadJSONContext did not return after context cancel")
	}
}

func TestWebSocketServerKeepAlivePing(t *testing.T) {
	app := New(
		WithProduces(MIMEJSON),
		WithServerWebSocketPingPeriod(20*time.Millisecond),
		WithServerWebSocketPongWait(60*time.Millisecond),
	)

	type message struct {
		Text string `json:"text"`
	}
	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
		var in message
		if err := conn.ReadJSONContext(ctx, &in); err != nil {
			return err
		}
		return conn.WriteJSON(message{Text: "alive:" + in.Text})
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	conn := dialTestWebSocket(t, ts.URL+"/ws")
	defer conn.Close()

	replyCh := make(chan message, 1)
	go func() {
		for {
			var m message
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			if strings.HasPrefix(m.Text, "alive:") {
				replyCh <- m
				return
			}
		}
	}()

	time.Sleep(150 * time.Millisecond) // survive several ping/pong rounds
	if err := conn.WriteJSON(message{Text: "check"}); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}
	select {
	case out := <-replyCh:
		if out.Text != "alive:check" {
			t.Fatalf("Text = %q, want alive:check", out.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for keepalive echo")
	}
}

func dialTestWebSocket(t *testing.T, rawURL string) *WebSocketConn {
	t.Helper()
	return dialTestWebSocketWithHeader(t, rawURL, nil)
}

func dialTestWebSocketWithHeader(t *testing.T, rawURL string, header http.Header) *WebSocketConn {
	t.Helper()

	conn, _, err := gwebsocket.DefaultDialer.Dial(httpToWSURL(rawURL), header)
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

func TestServerWebSocketOptionsConfigure(t *testing.T) {
	cfg := &Config{}
	WithServerWebSocketSubprotocols([]string{"chat", "json"})(cfg)
	WithServerWebSocketReadBufferSize(1024)(cfg)
	WithServerWebSocketWriteBufferSize(2048)(cfg)
	WithServerWebSocketPingPeriod(time.Second)(cfg)
	WithServerWebSocketPongWait(5 * time.Second)(cfg)

	if len(cfg.webSocketSubprotocols) != 2 || cfg.webSocketSubprotocols[1] != "json" {
		t.Fatalf("subprotocols = %#v, want [chat json]", cfg.webSocketSubprotocols)
	}
	if cfg.webSocketReadBufferSize != 1024 {
		t.Fatalf("read buffer = %d, want 1024", cfg.webSocketReadBufferSize)
	}
	if cfg.webSocketWriteBufferSize != 2048 {
		t.Fatalf("write buffer = %d, want 2048", cfg.webSocketWriteBufferSize)
	}
	if cfg.webSocketPingPeriod != time.Second {
		t.Fatalf("ping period = %s, want 1s", cfg.webSocketPingPeriod)
	}
	if cfg.webSocketPongWait != 5*time.Second {
		t.Fatalf("pong wait = %s, want 5s", cfg.webSocketPongWait)
	}
}

func TestWebSocketRejectsCrossOriginByDefault(t *testing.T) {
	app := New()
	Route[Params, struct{}](app).GET("/ws").ToWebSocket(func(context.Context, Params, *WebSocketConn) error {
		return nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "https://evil.example")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}
