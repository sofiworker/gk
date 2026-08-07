package ghttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
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

func TestSSEWriterWriteJSONWithID(t *testing.T) {
	w := &mockResponseWriter{header: make(http.Header)}
	sw := &SSEWriter{w: w, flusher: &mockFlusher{}}

	err := sw.WriteJSONWithID("update", "42", map[string]any{"ok": true})
	assert.NoError(t, err)

	output := w.buf.String()
	assert.Contains(t, output, "id: 42\n")
	assert.Contains(t, output, "event: update\n")
	assert.Contains(t, output, "data: {\"ok\":true}\n")
}

func TestSSEStreamingUnderTimeoutMiddleware(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(Timeout(5 * time.Second))

	Route[struct{}, struct{}](app).GET("/events").ToSSE(func(ctx context.Context, params Params, stream *SSEWriter) error {
		if err := stream.WriteJSON("tick", map[string]any{"n": 1}); err != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond)
		return stream.WriteJSON("tick", map[string]any{"n": 2})
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"n":1`) || !strings.Contains(text, `"n":2`) {
		t.Fatalf("SSE body = %q, want both events", text)
	}
}

func TestSSEHandlerErrorLogged(t *testing.T) {
	var buf bytes.Buffer
	app := New(
		WithProduces(MIMEJSON),
		WithLogger(NewSlogLogger(slog.New(slog.NewTextHandler(&buf, nil)))),
	)
	Route[struct{}, struct{}](app).GET("/events").ToSSE(func(ctx context.Context, params Params, stream *SSEWriter) error {
		return fmt.Errorf("sse boom")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/events", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(buf.String(), "sse handler error") || !strings.Contains(buf.String(), "sse boom") {
		t.Fatalf("logger output = %q, want sse handler error with sse boom", buf.String())
	}
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

func TestSSEWriterSplitsMultilineData(t *testing.T) {
	w := &mockResponseWriter{header: make(http.Header)}
	sw := &SSEWriter{w: w, flusher: &mockFlusher{}}

	err := sw.WriteEvent("message", "line1\nline2")
	assert.NoError(t, err)

	output := w.buf.String()
	assert.Equal(t, "event: message\ndata: line1\ndata: line2\n\n", output)
}

func TestSSEWriterRejectsNewlineInEventName(t *testing.T) {
	w := &mockResponseWriter{header: make(http.Header)}
	sw := &SSEWriter{w: w, flusher: &mockFlusher{}}

	assert.Error(t, sw.WriteEvent("bad\nevent", "x"))
}

func TestSSEWriterEventWithIDCommentRetry(t *testing.T) {
	w := &mockResponseWriter{header: make(http.Header)}
	sw := &SSEWriter{w: w, flusher: &mockFlusher{}}

	assert.NoError(t, sw.WriteEventWithID("update", "evt-1", "hello"))
	assert.NoError(t, sw.WriteComment("keepalive"))
	assert.NoError(t, sw.Retry(3000))

	output := w.buf.String()
	assert.Contains(t, output, "id: evt-1\n")
	assert.Contains(t, output, ": keepalive\n")
	assert.Contains(t, output, "retry: 3000\n")
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
