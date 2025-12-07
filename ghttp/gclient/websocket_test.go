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

var upgrader = websocket.Upgrader{}

func TestWebSocketRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	req := client.R().SetURL(wsURL)
	wsReq := NewWebSocketRequest(req).SetWebSocketHandler(func(msg WebSocketMessage) error {
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
}
