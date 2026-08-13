package ghttp

import (
	"bufio"
	"net"
	"net/http"
	"sync"
)

type responseWriteState struct {
	http.ResponseWriter
	mu           sync.Mutex
	committed    bool
	buffered     bool
	hijacked     bool
	suppressBody bool
	errorHandled bool
}

func responseErrorWriteBlocked(r *http.Request) bool {
	state := responseWriteStateFromRequest(r)
	return state != nil && state.errorWriteBlocked()
}

func responseWriteStateFromRequest(r *http.Request) *responseWriteState {
	if r == nil {
		return nil
	}
	reqState := requestStateFromRequest(r)
	if reqState == nil {
		return nil
	}
	return reqState.responseState
}

func responseErrorHandlerStarted(r *http.Request) bool {
	if r == nil {
		return false
	}
	state := responseWriteStateFromRequest(r)
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.errorHandled
}

func beginResponseErrorHandler(r *http.Request) bool {
	if r == nil {
		return true
	}
	state := responseWriteStateFromRequest(r)
	if state == nil {
		return true
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.errorHandled {
		return false
	}
	state.errorHandled = true
	return true
}

func newResponseWriteState(w http.ResponseWriter, suppressBody bool) (*responseWriteState, http.ResponseWriter) {
	state := &responseWriteState{ResponseWriter: w, suppressBody: suppressBody}
	return state, state
}

func (w *responseWriteState) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed || w.hijacked {
		return
	}
	w.committed = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriteState) Write(data []byte) (int, error) {
	w.mu.Lock()
	writeHeader := !w.committed
	if writeHeader {
		w.committed = true
	}
	suppressBody := w.suppressBody || w.hijacked
	w.mu.Unlock()
	if writeHeader {
		w.ResponseWriter.WriteHeader(http.StatusOK)
	}
	if suppressBody {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseWriteState) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriteState) errorWriteBlocked() bool {
	if w == nil {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.committed || w.buffered || w.hijacked
}

func (w *responseWriteState) markBuffered() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.buffered = true
	w.mu.Unlock()
}

func (w *responseWriteState) clearBuffered() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.buffered = false
	w.mu.Unlock()
}

func (w *responseWriteState) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriteState) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, rw, err := h.Hijack()
	if err == nil {
		w.mu.Lock()
		w.hijacked = true
		w.mu.Unlock()
	}
	return conn, rw, err
}

func (w *responseWriteState) Push(target string, options *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, options)
	}
	return http.ErrNotSupported
}

// ResponseWriter 包装 http.ResponseWriter 并提供便捷方法。
// ResponseWriter wraps http.ResponseWriter with convenience methods.
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
	size       int
}

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (w *ResponseWriter) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.written = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *ResponseWriter) Write(data []byte) (int, error) {
	if !w.written {
		w.WriteHeader(w.statusCode)
	}
	n, err := w.ResponseWriter.Write(data)
	w.size += n
	return n, err
}

func (w *ResponseWriter) Status() int {
	return w.statusCode
}

func (w *ResponseWriter) Written() bool {
	return w.written
}

func (w *ResponseWriter) Size() int {
	return w.size
}

func (w *ResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// Unwrap 返回底层 writer 供 http.ResponseController 使用。
// Unwrap returns the underlying writer for http.ResponseController.
func (w *ResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
