package ghttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResponseWriter(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)
	require.NotNil(t, rw)

	assert.Equal(t, 200, rw.Status())
	assert.False(t, rw.Written())
	assert.Equal(t, 0, rw.Size())
}

func TestResponseWriterWriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	rw.WriteHeader(201)
	assert.Equal(t, 201, rw.Status())
	assert.True(t, rw.Written())

	// Second call should be no-op
	rw.WriteHeader(500)
	assert.Equal(t, 201, rw.Status())
}

func TestResponseWriterWrite(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	n, err := rw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, 5, rw.Size())
	assert.True(t, rw.Written())
	assert.Equal(t, "hello", w.Body.String())
}

func TestResponseWriterWriteString(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	n, err := rw.WriteString("world")
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "world", w.Body.String())
}

func TestResponseWriterWriteAfterWriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	rw.WriteHeader(404)
	n, err := rw.Write([]byte("not found"))
	require.NoError(t, err)
	assert.Equal(t, 9, n)
	assert.Equal(t, 404, rw.Status())
}

func TestResponseWriterImplementsWriteResponse(t *testing.T) {
	var _ io.Writer = new(ResponseWriter)
	var _ http.ResponseWriter = new(ResponseWriter)
}

func TestResponseWriterMultipleWrites(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	_, _ = rw.Write([]byte("a"))
	_, _ = rw.Write([]byte("b"))
	_, _ = rw.Write([]byte("c"))

	assert.Equal(t, 3, rw.Size())
	assert.Equal(t, "abc", w.Body.String())
}

func TestResponseWriterUnwrap(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	assert.Same(t, w, rw.Unwrap())
}
