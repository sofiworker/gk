package gclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSERequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("unexpected auth header %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("lang") != "zh" {
			t.Fatalf("unexpected query %q", r.URL.RawQuery)
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "close")

		// Send event
		fmt.Fprintf(w, "id: 1\n")
		fmt.Fprintf(w, "event: message\n")
		fmt.Fprintf(w, "data: hello\n\n")
		flusher.Flush()

		// Send another
		fmt.Fprintf(w, "data: world\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	client := NewClient()
	received := make(chan SSEEvent, 2)

	req := client.R().
		SetURL(ts.URL).
		SetBearerToken("token-1").
		SetQueryParam("lang", "zh")
	sseReq := req.NewSSERequest().SetHandler(func(event SSEEvent) error {
		received <- event
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = sseReq.Stream(ctx)
	}()

	// Verify events
	select {
	case e := <-received:
		if e.ID != "1" || e.Data != "hello" {
			t.Errorf("event 1 mismatch: %v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event 1")
	}
}

func TestSSEHandleStreamParsesMultipleEvents(t *testing.T) {
	received := make([]SSEEvent, 0, 2)
	req := NewClient().R().SetURL("http://example.com")
	sseReq := req.NewSSERequest().SetHandler(func(event SSEEvent) error {
		received = append(received, event)
		return nil
	})

	data := "id: 1\nevent: message\ndata: hello\n\ndata: world\n\n"
	if err := sseReq.handleSSEStream(context.Background(), strings.NewReader(data)); err != nil {
		t.Fatalf("handle sse stream failed: %v", err)
	}
	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %+v", received)
	}
	if received[0].ID != "1" || received[0].Data != "hello" {
		t.Fatalf("unexpected first event %+v", received[0])
	}
	if received[1].Data != "world" {
		t.Fatalf("unexpected second event %+v", received[1])
	}
}

func TestSSEStreamChannels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: hello\n\n")
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stream := NewClient().R().
		SetURL(ts.URL).
		NewSSERequest().
		StreamChannels(ctx, 1)

	select {
	case event, ok := <-stream.Events:
		if !ok {
			t.Fatal("events channel closed unexpectedly")
		}
		if event.Data != "hello" {
			t.Fatalf("unexpected event %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting sse event")
	}

	select {
	case err := <-stream.Errors:
		if err != nil {
			t.Fatalf("unexpected stream error %v", err)
		}
	default:
	}
}

func TestSSEFilterAndDecodeJSON(t *testing.T) {
	var received struct {
		Name string `json:"name"`
	}
	req := NewClient().R().SetURL("http://example.com")
	sseReq := req.NewSSERequest().
		SetHandler(func(SSEEvent) error { return nil }).
		OnJSON("user", &received, func(interface{}) error { return nil })

	data := "event: ignore\ndata: {\"name\":\"skip\"}\n\nevent: user\ndata: {\"name\":\"gk\"}\n\n"
	if err := sseReq.handleSSEStream(context.Background(), strings.NewReader(data)); err != nil {
		t.Fatalf("handle sse stream failed: %v", err)
	}
	if received.Name != "gk" {
		t.Fatalf("unexpected decoded payload %+v", received)
	}
}

func TestSSEStreamJSONChannels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: user\ndata: {\"name\":\"gk\"}\n\n")
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stream := NewClient().R().
		SetURL(ts.URL).
		NewSSERequest().
		StreamJSONChannels(ctx, "user", 1, func() interface{} {
			return &struct {
				Name string `json:"name"`
			}{}
		})

	select {
	case v, ok := <-stream.Events:
		if !ok {
			t.Fatal("json events channel closed unexpectedly")
		}
		payload, ok := v.(*struct {
			Name string `json:"name"`
		})
		if !ok || payload.Name != "gk" {
			t.Fatalf("unexpected payload %#v", v)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting json sse event")
	}
}

func TestSSEHooks(t *testing.T) {
	connected := false
	retried := false
	capturedErr := ""

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := NewClient().R().
		SetURL(ts.URL).
		NewSSERequest().
		SetHandler(func(SSEEvent) error { return nil }).
		SetReconnect(false).
		OnConnect(func(*http.Response) error {
			connected = true
			return nil
		}).
		OnRetry(func(int, error, time.Duration) {
			retried = true
		}).
		OnError(func(err error) {
			capturedErr = err.Error()
		}).
		Stream(ctx)
	if err == nil {
		t.Fatal("expected sse error")
	}
	if connected {
		t.Fatal("onConnect should not be called on non-200")
	}
	if retried {
		t.Fatal("retry should not happen when reconnect disabled")
	}
	if capturedErr == "" {
		t.Fatal("expected error hook called")
	}
}

func TestSSEObserver(t *testing.T) {
	connected := false
	closed := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: ok\n\n")
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := NewClient().R().
		SetURL(ts.URL).
		NewSSERequest().
		SetHandler(func(SSEEvent) error { return nil }).
		SetObserver(StreamObserverFuncs{
			Connect: func(info StreamConnectInfo) error {
				connected = info.Protocol == StreamProtocolSSE
				return nil
			},
			Close: func(info StreamCloseInfo) {
				closed = info.Protocol == StreamProtocolSSE
			},
		}).
		Stream(ctx)
	if err != nil {
		t.Fatalf("unexpected sse error %v", err)
	}
	if !connected || !closed {
		t.Fatalf("expected observer connect/close called, connected=%v closed=%v", connected, closed)
	}
}
