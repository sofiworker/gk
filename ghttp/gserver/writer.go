package gserver

import (
	"bufio"
	"io"
	"net"
	"net/http"

	"github.com/valyala/fasthttp"
)

type ResponseWriter interface {
	http.ResponseWriter
	Status() int
	Written() bool
	Size() int
	WriteString(string) (int, error)
}

func wrapResponseWriter(w http.ResponseWriter) ResponseWriter {
	if rw, ok := w.(ResponseWriter); ok {
		return rw
	}
	return &stdResponseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

type stdResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	size        int
}

func (w *stdResponseWriter) WriteHeader(code int) {
	if code <= 0 {
		code = http.StatusOK
	}
	if !w.wroteHeader {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
		w.wroteHeader = true
		return
	}
	w.status = code
}

func (w *stdResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.size += n
	return n, err
}

func (w *stdResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *stdResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *stdResponseWriter) Written() bool {
	return w.wroteHeader || w.size > 0
}

func (w *stdResponseWriter) Size() int {
	return w.size
}

func (w *stdResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *stdResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrHijacked
}

func (w *stdResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *stdResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(r)
		if n > 0 {
			w.size += int(n)
		}
		return n, err
	}
	return io.Copy(w.ResponseWriter, r)
}

type respWriter struct {
	ctx         *fasthttp.RequestCtx
	header      http.Header
	wroteHeader bool
	statusCode  int
	size        int
}

func (r *respWriter) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *respWriter) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if len(data) == 0 {
		return 0, nil
	}
	r.size += len(data)
	return r.ctx.Write(data)
}

func (r *respWriter) WriteString(s string) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if len(s) == 0 {
		return 0, nil
	}
	r.size += len(s)
	return r.ctx.WriteString(s)
}

func (r *respWriter) WriteHeader(statusCode int) {
	if statusCode <= 0 {
		statusCode = http.StatusOK
	}
	r.statusCode = statusCode
	if r.header != nil {
		r.ctx.Response.Header.Reset()
		for k, values := range r.header {
			for _, v := range values {
				r.ctx.Response.Header.Add(k, v)
			}
		}
	}
	r.ctx.SetStatusCode(statusCode)
	r.wroteHeader = true
}

func (r *respWriter) Status() int {
	if r.statusCode == 0 {
		return http.StatusOK
	}
	return r.statusCode
}

func (r *respWriter) Written() bool {
	return r.wroteHeader || r.size > 0
}

func (r *respWriter) Size() int {
	return r.size
}

func (r *respWriter) Flush() {
	// fasthttp 在每次请求结束时会自动写回，显式 Flush 为空实现即可
}
