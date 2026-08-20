package ghttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ctx 实现 http.ResponseWriter;测试 Ctx 的写入行为。
// Ctx implements http.ResponseWriter; tests Ctx's write behavior.

func newCtxTestWriter(w http.ResponseWriter) *Ctx {
	c := acquireCtx(w, httptest.NewRequest("GET", "/", nil))
	return c
}

func TestCtxWriterWriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	c := newCtxTestWriter(w)

	c.WriteHeader(201)
	assert.Equal(t, 201, c.status)
	assert.True(t, c.committed)
	assert.Equal(t, 201, w.Code)
}

func TestCtxWriterWrite(t *testing.T) {
	w := httptest.NewRecorder()
	c := newCtxTestWriter(w)

	n, err := c.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", w.Body.String())
}

func TestCtxWriterImplementsResponseWriter(t *testing.T) {
	var _ http.ResponseWriter = new(Ctx)
}

func TestCtxWriterMultipleWrites(t *testing.T) {
	w := httptest.NewRecorder()
	c := newCtxTestWriter(w)

	_, _ = c.Write([]byte("a"))
	_, _ = c.Write([]byte("b"))
	_, _ = c.Write([]byte("c"))

	assert.Equal(t, "abc", w.Body.String())
}

func TestCtxWriterHeader(t *testing.T) {
	w := httptest.NewRecorder()
	c := newCtxTestWriter(w)

	c.Header().Set("X-Test", "1")
	assert.Equal(t, "1", w.Header().Get("X-Test"))
}
