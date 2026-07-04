package ghttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSSEWriterWriteEvent(t *testing.T) {
	w := &mockResponseWriter{header: make(http.Header)}
	sw := &SSEWriter{w: w, flusher: &mockFlusher{}}

	err := sw.WriteEvent("update", `{"status":"ok"}`)
	assert.NoError(t, err)

	output := w.buf.String()
	assert.Contains(t, output, "event: update")
	assert.Contains(t, output, "data: {\"status\":\"ok\"}")
	assert.Contains(t, output, "\n\n")
}

func TestSSEWriterWriteJSON(t *testing.T) {
	w := &mockResponseWriter{header: make(http.Header)}
	sw := &SSEWriter{w: w, flusher: &mockFlusher{}}

	err := sw.WriteJSON("message", map[string]string{"key": "value"})
	assert.NoError(t, err)

	output := w.buf.String()
	assert.Contains(t, output, "event: message")
	assert.Contains(t, output, `"key":"value"`)
}

func TestSSEWriterMultipleEvents(t *testing.T) {
	w := &mockResponseWriter{header: make(http.Header)}
	sw := &SSEWriter{w: w, flusher: &mockFlusher{}}

	_ = sw.WriteEvent("e1", "data1")
	_ = sw.WriteEvent("e2", "data2")

	output := w.buf.String()
	assert.Contains(t, output, "event: e1")
	assert.Contains(t, output, "event: e2")
}

func TestSSEEventTypes(t *testing.T) {
	event := SSEEvent{ID: "1", Event: "update", Data: `{"x":1}`}
	assert.Equal(t, "1", event.ID)
	assert.Equal(t, "update", event.Event)
	assert.Equal(t, `{"x":1}`, event.Data)
}

func TestClientSSEReconnectSendsLastEventID(t *testing.T) {
	var calls int32
	lastEventID := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			_, _ = fmt.Fprint(w, "id: 42\nevent: first\ndata: one\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		lastEventID <- r.Header.Get("Last-Event-ID")
		_, _ = fmt.Fprint(w, "id: 43\nevent: second\ndata: two\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	client := NewClient(WithBaseURL(ts.URL))
	stream, err := client.SSE("/events", SSEConfig{
		Reconnect:     true,
		RetryInterval: time.Millisecond,
		MaxRetries:    1,
	})
	if err != nil {
		t.Fatalf("SSE failed: %v", err)
	}
	defer stream.Close()

	select {
	case event := <-stream.Events:
		if event.ID != "42" {
			t.Fatalf("first event ID = %q, want 42", event.ID)
		}
	case err := <-stream.Errors:
		t.Fatalf("unexpected SSE error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first SSE event")
	}

	select {
	case got := <-lastEventID:
		if got != "42" {
			t.Fatalf("Last-Event-ID = %q, want 42", got)
		}
	case err := <-stream.Errors:
		t.Fatalf("unexpected SSE error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reconnect")
	}
}

type mockResponseWriter struct {
	header http.Header
	buf    strings.Builder
	code   int
}

func (m *mockResponseWriter) Header() http.Header         { return m.header }
func (m *mockResponseWriter) Write(b []byte) (int, error) { return m.buf.Write(b) }
func (m *mockResponseWriter) WriteHeader(code int)        { m.code = code }

type mockFlusher struct{}

func (m *mockFlusher) Flush() {}
