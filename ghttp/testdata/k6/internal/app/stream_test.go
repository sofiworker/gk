package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSSEOnceWireFormat(t *testing.T) {
	server := newStreamTestServer(t)
	request, err := http.NewRequestWithContext(testContext(t), http.MethodGet, server.URL+"/sse/once", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("stream headers = %v", response.Header)
	}
	want := "retry: 1000\n\nid: 1\nevent: message\ndata: hello\ndata: world\n\n"
	if string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestSSEMultiAndHeartbeat(t *testing.T) {
	server := newStreamTestServer(t)
	for path, fragments := range map[string][]string{
		"/sse/multi?count=3":     {"id: 1", "data: message-1", "id: 3", "data: message-3"},
		"/sse/heartbeat?count=2": {": heartbeat-1", "data: heartbeat-1", ": heartbeat-2", "data: heartbeat-2"},
	} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			t.Fatalf("ReadAll(%s) error = %v", path, readErr)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(body), fragment) {
				t.Fatalf("GET %s body = %q, want fragment %q", path, body, fragment)
			}
		}
	}
}

func TestSSESlowFlushesBeforeCompletionAndCancelClosesMetric(t *testing.T) {
	server := newStreamTestServer(t)
	ctx, cancel := context.WithCancel(testContext(t))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/sse/slow?interval_ms=1000&count=5", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("first ReadString() error = %v", err)
	}
	if first != "id: 1\n" {
		t.Fatalf("first flushed line = %q, want id line", first)
	}
	waitForConnections(t, server, 1, 0)
	cancel()
	_ = response.Body.Close()
	waitForConnections(t, server, 0, 0)
}

func TestSSEConcurrentConnectionsReachPeakAndReturnToZero(t *testing.T) {
	server := newStreamTestServer(t)
	const clients = 4
	cancels := make([]context.CancelFunc, clients)
	responses := make([]*http.Response, clients)
	for index := 0; index < clients; index++ {
		ctx, cancel := context.WithCancel(testContext(t))
		cancels[index] = cancel
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/sse/slow?interval_ms=1000&count=5", nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext() error = %v", err)
		}
		responses[index], err = server.Client().Do(request)
		if err != nil {
			t.Fatalf("Do() error = %v", err)
		}
	}
	waitForConnections(t, server, clients, 0)
	for index := range cancels {
		cancels[index]()
		_ = responses[index].Body.Close()
	}
	waitForConnections(t, server, 0, 0)
}

func TestWebSocketEchoBinaryMultiAndSlow(t *testing.T) {
	server := newStreamTestServer(t)
	tests := []struct {
		path        string
		messageType int
		messages    []string
	}{
		{path: "/ws/echo", messageType: websocket.TextMessage, messages: []string{"alpha"}},
		{path: "/ws/binary", messageType: websocket.BinaryMessage, messages: []string{"binary"}},
		{path: "/ws/multi", messageType: websocket.TextMessage, messages: []string{"one", "two", "three"}},
		{path: "/ws/slow?delay_ms=1", messageType: websocket.TextMessage, messages: []string{"slow"}},
	}
	for _, test := range tests {
		conn := dialWebSocket(t, server.URL+test.path, nil)
		for _, message := range test.messages {
			if err := conn.WriteMessage(test.messageType, []byte(message)); err != nil {
				t.Fatalf("WriteMessage(%s) error = %v", test.path, err)
			}
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("ReadMessage(%s) error = %v", test.path, err)
			}
			if messageType != test.messageType || string(data) != message {
				t.Fatalf("echo(%s) = (%d, %q)", test.path, messageType, data)
			}
		}
		_ = conn.Close()
	}
}

func TestWebSocketOriginClosePostReadWireRejectionAndMetrics(t *testing.T) {
	server := newStreamTestServer(t)
	_, response, err := websocket.DefaultDialer.Dial(httpToWebSocketURL(server.URL+"/ws/echo"), http.Header{"Origin": {"https://denied.test"}})
	if err == nil {
		t.Fatal("denied origin dial succeeded")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("denied origin response = %#v", response)
	}

	allowed := dialWebSocket(t, server.URL+"/ws/echo", http.Header{"Origin": {server.URL}})
	waitForConnections(t, server, 0, 1)
	pong := make(chan string, 1)
	allowed.SetPongHandler(func(payload string) error {
		pong <- payload
		return nil
	})
	readDone := make(chan error, 1)
	go func() {
		_, _, err := allowed.ReadMessage()
		readDone <- err
	}()
	if err := allowed.WriteControl(websocket.PingMessage, []byte("probe"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("WriteControl(Ping) error = %v", err)
	}
	if err := allowed.WriteMessage(websocket.TextMessage, []byte("ok")); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	select {
	case payload := <-pong:
		if payload != "probe" {
			t.Fatalf("pong payload = %q, want probe", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pong payload not received")
	}
	if err := <-readDone; err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	_ = allowed.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client done"))
	_ = allowed.Close()
	waitForConnections(t, server, 0, 0)

	closed := dialWebSocket(t, server.URL+"/ws/close?code=4001", nil)
	_, _, err = closed.ReadMessage()
	if !websocket.IsCloseError(err, 4001) || !strings.Contains(err.Error(), "server close") {
		t.Fatalf("server close error = %v, want code 4001 and reason", err)
	}
	_ = closed.Close()

	limited := dialWebSocket(t, server.URL+"/ws/echo", nil)
	if err := limited.WriteMessage(websocket.TextMessage, make([]byte, maxStreamMessageBytes+1)); err != nil {
		t.Fatalf("large WriteMessage() error = %v", err)
	}
	_, _, err = limited.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("large message close error = %v, want 1009", err)
	}
	_ = limited.Close()
}

func TestWebSocketConcurrentConnectionsReturnToZero(t *testing.T) {
	server := newStreamTestServer(t)
	const clients = 4
	connections := make([]*websocket.Conn, clients)
	for index := range connections {
		connections[index] = dialWebSocket(t, server.URL+"/ws/echo", nil)
	}
	waitForConnections(t, server, 0, clients)
	for _, conn := range connections {
		_ = conn.Close()
	}
	waitForConnections(t, server, 0, 0)
}

func newStreamTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler, cleanup := New(Config{})
	t.Cleanup(cleanup)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func dialWebSocket(t *testing.T, rawURL string, header http.Header) *websocket.Conn {
	t.Helper()
	conn, response, err := websocket.DefaultDialer.Dial(httpToWebSocketURL(rawURL), header)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		t.Fatalf("Dial(%s) error = %v", rawURL, err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("Dial(%s) status = %d, want 101", rawURL, response.StatusCode)
	}
	return conn
}

func httpToWebSocketURL(rawURL string) string {
	return "ws" + strings.TrimPrefix(rawURL, "http")
}

func waitForConnections(t *testing.T, server *httptest.Server, wantSSE, wantWS int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, err := server.Client().Get(server.URL + "/__test/metrics")
		if err == nil {
			var snapshot MetricsSnapshot
			decodeErr := json.NewDecoder(response.Body).Decode(&snapshot)
			_ = response.Body.Close()
			if decodeErr == nil && snapshot.SSEConnections == uint64(wantSSE) && snapshot.WSConnections == uint64(wantWS) {
				return
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("connections did not reach SSE=%d WS=%d", wantSSE, wantWS)
		case <-ticker.C:
		}
	}
}
