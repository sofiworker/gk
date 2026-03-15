package gclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{Subprotocols: []string{"chat"}}

func TestWebSocketRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lang") != "zh" {
			t.Fatalf("unexpected query %q", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("unexpected auth header %q", r.Header.Get("Authorization"))
		}
		if got := websocket.Subprotocols(r); len(got) == 0 || got[0] != "chat" {
			t.Fatalf("unexpected subprotocols %+v", got)
		}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			mt, message, err := c.ReadMessage()
			if err != nil {
				break
			}
			// Echo back
			err = c.WriteMessage(mt, message)
			if err != nil {
				break
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	client := NewClient()
	received := make(chan WebSocketMessage, 1)

	req := client.R().
		SetURL(wsURL).
		SetBearerToken("token-1").
		SetQueryParam("lang", "zh")
	wsReq := req.NewWebSocketRequest().
		SetSubprotocols([]string{"chat"}).
		SetHandler(func(msg WebSocketMessage) error {
			received <- msg
			return nil
		})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start connection in background
	go func() {
		_ = wsReq.Connect(ctx)
	}()

	// Give it time to connect
	time.Sleep(100 * time.Millisecond)

	// Send message
	if err := wsReq.SendMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Verify echo
	select {
	case msg := <-received:
		if string(msg.Data) != "hello" {
			t.Errorf("expected hello, got %s", msg.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	if err := wsReq.WriteText("bye"); err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg.Data) != "bye" {
			t.Errorf("expected bye, got %s", msg.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for bye")
	}

	if err := wsReq.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestWebSocketHooksAndPing(t *testing.T) {
	connected := false
	closed := false
	beforeWriteCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.SetPingHandler(func(appData string) error {
			return c.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
		})
		for {
			mt, message, err := c.ReadMessage()
			if err != nil {
				break
			}
			if err := c.WriteMessage(mt, message); err != nil {
				break
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	req := NewClient().R().SetURL(wsURL)
	wsReq := req.NewWebSocketRequest().
		SetHandler(func(WebSocketMessage) error { return nil }).
		OnConnect(func(_ *websocket.Conn, _ *http.Response) error {
			connected = true
			return nil
		}).
		OnClose(func(error) {
			closed = true
		}).
		BeforeWrite(func(messageType int, data []byte) error {
			beforeWriteCalled = true
			if messageType != websocket.TextMessage || string(data) != "hook" {
				t.Fatalf("unexpected write hook payload type=%d data=%q", messageType, string(data))
			}
			return nil
		})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = wsReq.Connect(ctx)
	}()

	deadline := time.Now().Add(time.Second)
	for wsReq.Conn() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if wsReq.Conn() == nil {
		t.Fatalf("websocket connection not established in time")
	}

	if err := wsReq.WriteText("hook"); err != nil {
		t.Fatalf("WriteText failed: %v", err)
	}
	if err := wsReq.Ping(); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	if err := wsReq.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if !connected {
		t.Fatalf("expected onConnect called")
	}
	if !beforeWriteCalled {
		t.Fatalf("expected beforeWrite called")
	}
	if !closed {
		t.Fatalf("expected onClose called")
	}
}

func TestWebSocketReadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteJSON(map[string]string{"name": "gk"})
		time.Sleep(50 * time.Millisecond)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	wsReq := NewClient().R().SetURL(wsURL).NewWebSocketRequest()
	conn, _, err := wsReq.Dial(context.Background())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	var out struct {
		Name string `json:"name"`
	}
	if err := wsReq.ReadJSON(&out); err != nil {
		t.Fatalf("read json failed: %v", err)
	}
	if out.Name != "gk" {
		t.Fatalf("unexpected payload %+v", out)
	}
}

func TestWebSocketJSONHandlerAndMessageDecode(t *testing.T) {
	received := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteJSON(map[string]string{"name": "gk"})
		time.Sleep(50 * time.Millisecond)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	wsReq := NewClient().R().SetURL(wsURL).NewWebSocketRequest().
		SetJSONHandler(func() interface{} {
			return &struct {
				Name string `json:"name"`
			}{}
		}, func(v interface{}) error {
			received <- v.(*struct {
				Name string `json:"name"`
			}).Name
			return nil
		})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		_ = wsReq.Connect(ctx)
	}()

	select {
	case name := <-received:
		if name != "gk" {
			t.Fatalf("unexpected name %q", name)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting json handler")
	}

	msg := WebSocketMessage{Type: websocket.TextMessage, Data: []byte(`{"name":"manual"}`)}
	var out struct {
		Name string `json:"name"`
	}
	if err := msg.DecodeJSON(&out); err != nil {
		t.Fatalf("decode json message failed: %v", err)
	}
	if out.Name != "manual" {
		t.Fatalf("unexpected manual payload %+v", out)
	}
}

func TestWebSocketCloseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"), time.Now().Add(time.Second))
		time.Sleep(50 * time.Millisecond)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	wsReq := NewClient().R().SetURL(wsURL).NewWebSocketRequest()
	conn, _, err := wsReq.Dial(context.Background())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	_, err = wsReq.ReadMessage()
	if err == nil {
		t.Fatal("expected websocket close error")
	}
	closeErr, ok := err.(*WebSocketCloseError)
	if !ok {
		t.Fatalf("expected WebSocketCloseError, got %T", err)
	}
	if closeErr.Code != websocket.CloseNormalClosure || closeErr.Reason != "bye" {
		t.Fatalf("unexpected close error %+v", closeErr)
	}
	if !closeErr.IsCode(websocket.CloseNormalClosure) {
		t.Fatalf("expected close code helper true")
	}
	if closeErr.CloseCode() != websocket.CloseNormalClosure || closeErr.CloseReason() != "bye" {
		t.Fatalf("unexpected close helpers code=%d reason=%q", closeErr.CloseCode(), closeErr.CloseReason())
	}
}

func TestWebSocketRetryAndErrorHooks(t *testing.T) {
	retried := false
	capturedErr := ""

	wsReq := NewClient().R().SetURL("ws://127.0.0.1:1").NewWebSocketRequest().
		SetHandler(func(WebSocketMessage) error { return nil }).
		SetMaxRetries(1).
		SetRetryDelay(0).
		OnRetry(func(attempt int, err error, delay time.Duration) {
			retried = attempt == 1 && err != nil && delay == 0
		}).
		OnError(func(err error) {
			capturedErr = err.Error()
		})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := wsReq.Connect(ctx); err == nil {
		t.Fatal("expected connect error")
	}
	if !retried {
		t.Fatal("expected retry hook called")
	}
	if capturedErr == "" {
		t.Fatal("expected error hook called")
	}
}

func TestWebSocketObserver(t *testing.T) {
	connected := false
	closed := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, []byte("ok"))
		time.Sleep(50 * time.Millisecond)
	}))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	wsReq := NewClient().R().SetURL(wsURL).NewWebSocketRequest().
		SetHandler(func(WebSocketMessage) error { return nil }).
		SetObserver(StreamObserverFuncs{
			Connect: func(info StreamConnectInfo) error {
				connected = info.Protocol == StreamProtocolWebSocket
				return nil
			},
			Close: func(info StreamCloseInfo) {
				closed = info.Protocol == StreamProtocolWebSocket
			},
		})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = wsReq.Connect(ctx)

	if !connected || !closed {
		t.Fatalf("expected observer connect/close called, connected=%v closed=%v", connected, closed)
	}
}
