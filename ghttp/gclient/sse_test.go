package gclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSSERequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

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
	
	req := client.R().SetURL(ts.URL)
	sseReq := NewSSERequest(req).SetSSEHandler(func(event SSEEvent) error {
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
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event 1")
	}

	select {
	case e := <-received:
		if e.Data != "world" {
			t.Errorf("event 2 mismatch: %v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event 2")
	}
}
