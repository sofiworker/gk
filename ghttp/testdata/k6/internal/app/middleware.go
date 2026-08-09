package app

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/sofiworker/gk/ghttp"
)

var requestIDSequence atomic.Uint64

func observeRequests(metrics *RuntimeMetrics) ghttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = fmt.Sprintf("k6-%016x", requestIDSequence.Add(1))
			}
			w.Header().Set("X-Request-ID", requestID)
			recorder, wrapped := wrapMetricsResponseWriter(w)
			finish := metrics.BeginRequest()
			defer func() {
				requestBytes := r.ContentLength
				if requestBytes < 0 {
					requestBytes = 0
				}
				responseBytes := recorder.bytes
				if !responseCanHaveBody(r.Method, recorder.status) {
					responseBytes = 0
				}
				finish(recorder.status, uint64(requestBytes), responseBytes)
			}()
			next.ServeHTTP(wrapped, r)
		})
	}
}

type metricsResponseWriter struct {
	http.ResponseWriter
	status      int
	bytes       uint64
	wroteHeader bool
}

func newMetricsResponseWriter(w http.ResponseWriter) *metricsResponseWriter {
	return &metricsResponseWriter{ResponseWriter: w, status: http.StatusOK}
}

func wrapMetricsResponseWriter(w http.ResponseWriter) (*metricsResponseWriter, http.ResponseWriter) {
	core := newMetricsResponseWriter(w)
	_, flush := w.(http.Flusher)
	_, hijack := w.(http.Hijacker)
	_, push := w.(http.Pusher)
	switch {
	case flush && hijack && push:
		return core, &metricsFlushHijackPushWriter{metricsResponseWriter: core}
	case flush && hijack:
		return core, &metricsFlushHijackWriter{metricsResponseWriter: core}
	case flush && push:
		return core, &metricsFlushPushWriter{metricsResponseWriter: core}
	case hijack && push:
		return core, &metricsHijackPushWriter{metricsResponseWriter: core}
	case flush:
		return core, &metricsFlushWriter{metricsResponseWriter: core}
	case hijack:
		return core, &metricsHijackWriter{metricsResponseWriter: core}
	case push:
		return core, &metricsPushWriter{metricsResponseWriter: core}
	default:
		return core, core
	}
}

func (w *metricsResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricsResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += uint64(n)
	return n, err
}

func (w *metricsResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *metricsResponseWriter) flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *metricsResponseWriter) hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker := w.ResponseWriter.(http.Hijacker)
	return hijacker.Hijack()
}

func (w *metricsResponseWriter) push(target string, options *http.PushOptions) error {
	pusher := w.ResponseWriter.(http.Pusher)
	return pusher.Push(target, options)
}

type metricsFlushWriter struct{ *metricsResponseWriter }

func (w *metricsFlushWriter) Flush() { w.flush() }

type metricsHijackWriter struct{ *metricsResponseWriter }

func (w *metricsHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.hijack() }

type metricsPushWriter struct{ *metricsResponseWriter }

func (w *metricsPushWriter) Push(target string, options *http.PushOptions) error {
	return w.push(target, options)
}

type metricsFlushHijackWriter struct{ *metricsResponseWriter }

func (w *metricsFlushHijackWriter) Flush() { w.flush() }
func (w *metricsFlushHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

type metricsFlushPushWriter struct{ *metricsResponseWriter }

func (w *metricsFlushPushWriter) Flush() { w.flush() }
func (w *metricsFlushPushWriter) Push(target string, options *http.PushOptions) error {
	return w.push(target, options)
}

type metricsHijackPushWriter struct{ *metricsResponseWriter }

func (w *metricsHijackPushWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}
func (w *metricsHijackPushWriter) Push(target string, options *http.PushOptions) error {
	return w.push(target, options)
}

type metricsFlushHijackPushWriter struct{ *metricsResponseWriter }

func (w *metricsFlushHijackPushWriter) Flush() { w.flush() }
func (w *metricsFlushHijackPushWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}
func (w *metricsFlushHijackPushWriter) Push(target string, options *http.PushOptions) error {
	return w.push(target, options)
}

func responseCanHaveBody(method string, status int) bool {
	return method != http.MethodHead && status >= http.StatusOK && status != http.StatusNoContent && status != http.StatusNotModified
}
