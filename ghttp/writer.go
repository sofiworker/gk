package ghttp

import "net/http"

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
