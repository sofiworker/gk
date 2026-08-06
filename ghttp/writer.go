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
	reqState, _ := r.Context().Value(requestStateContextKey{}).(requestState)
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
	_, flushes := w.(http.Flusher)
	_, hijacks := w.(http.Hijacker)
	_, pushes := w.(http.Pusher)

	switch {
	case flushes && hijacks && pushes:
		return state, &responseWriteStateFlushHijackPush{responseWriteState: state}
	case flushes && hijacks:
		return state, &responseWriteStateFlushHijack{responseWriteState: state}
	case flushes && pushes:
		return state, &responseWriteStateFlushPush{responseWriteState: state}
	case hijacks && pushes:
		return state, &responseWriteStateHijackPush{responseWriteState: state}
	case flushes:
		return state, &responseWriteStateFlush{responseWriteState: state}
	case hijacks:
		return state, &responseWriteStateHijack{responseWriteState: state}
	case pushes:
		return state, &responseWriteStatePush{responseWriteState: state}
	default:
		return state, state
	}
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

func (w *responseWriteState) flush() {
	w.WriteHeader(http.StatusOK)
	w.ResponseWriter.(http.Flusher).Flush()
}

func (w *responseWriteState) hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, readWriter, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		w.mu.Lock()
		w.hijacked = true
		w.mu.Unlock()
	}
	return conn, readWriter, err
}

func (w *responseWriteState) push(target string, options *http.PushOptions) error {
	return w.ResponseWriter.(http.Pusher).Push(target, options)
}

type responseWriteStateFlush struct{ *responseWriteState }

func (w *responseWriteStateFlush) Flush() { w.responseWriteState.flush() }

type responseWriteStateHijack struct{ *responseWriteState }

func (w *responseWriteStateHijack) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.responseWriteState.hijack()
}

type responseWriteStatePush struct{ *responseWriteState }

func (w *responseWriteStatePush) Push(target string, options *http.PushOptions) error {
	return w.responseWriteState.push(target, options)
}

type responseWriteStateFlushHijack struct{ *responseWriteState }

func (w *responseWriteStateFlushHijack) Flush() { w.responseWriteState.flush() }

func (w *responseWriteStateFlushHijack) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.responseWriteState.hijack()
}

type responseWriteStateFlushPush struct{ *responseWriteState }

func (w *responseWriteStateFlushPush) Flush() { w.responseWriteState.flush() }

func (w *responseWriteStateFlushPush) Push(target string, options *http.PushOptions) error {
	return w.responseWriteState.push(target, options)
}

type responseWriteStateHijackPush struct{ *responseWriteState }

func (w *responseWriteStateHijackPush) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.responseWriteState.hijack()
}

func (w *responseWriteStateHijackPush) Push(target string, options *http.PushOptions) error {
	return w.responseWriteState.push(target, options)
}

type responseWriteStateFlushHijackPush struct{ *responseWriteState }

func (w *responseWriteStateFlushHijackPush) Flush() { w.responseWriteState.flush() }

func (w *responseWriteStateFlushHijackPush) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.responseWriteState.hijack()
}

func (w *responseWriteStateFlushHijackPush) Push(target string, options *http.PushOptions) error {
	return w.responseWriteState.push(target, options)
}

// ResponseWriter wraps http.ResponseWriter with additional convenience methods.
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

// Unwrap returns the underlying response writer for http.ResponseController.
func (w *ResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
