package ghttp

import (
	"net/http"
	"strings"
	"testing"

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
