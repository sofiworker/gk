package ghttp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Middleware 是标准 net/http 中间件签名。
// Middleware is the standard net/http middleware signature.
type Middleware func(http.Handler) http.Handler

type requestIDContextKey struct{}

// DefaultMaxRequestIDLength 是传入 X-Request-ID 的最大接受长度。
// DefaultMaxRequestIDLength caps incoming X-Request-ID length.
// 超长值被忽略并替换为新 ID。
// longer values are replaced by a fresh ID.
const DefaultMaxRequestIDLength = 128

type requestIDConfig struct {
	maxLength int
}

// RequestIDOption 配置 RequestID 中间件。
// RequestIDOption configures the RequestID middleware.
type RequestIDOption func(*requestIDConfig)

// WithRequestIDMaxLength 限制传入 X-Request-ID 的长度上限。
// WithRequestIDMaxLength caps the accepted incoming X-Request-ID length.
// 超长值替换为新 ID。
// values longer than the cap are replaced by a fresh ID.
// 非正值保持默认。
// non-positive values keep the default.
func WithRequestIDMaxLength(n int) RequestIDOption {
	return func(cfg *requestIDConfig) {
		if n > 0 {
			cfg.maxLength = n
		}
	}
}

// GetRequestID 返回 RequestID 中间件安装的请求 ID。
// GetRequestID returns the request ID installed by RequestID middleware.
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

// Chain 按注册顺序组合中间件。
// Chain composes middlewares in registration order.
func Chain(mws ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		return wrap(next, mws...)
	}
}

// Wrap 按注册顺序用 mws 包装 handler。
// Wrap wraps handler with mws in registration order.
func Wrap(handler http.Handler, mws ...Middleware) http.Handler {
	return wrap(handler, mws...)
}

// WrapFunc 按注册顺序用 mws 包装 handler。
// WrapFunc wraps handler with mws in registration order.
func WrapFunc(handler http.HandlerFunc, mws ...Middleware) http.Handler {
	return Wrap(handler, mws...)
}

func wrap(handler http.Handler, mws ...Middleware) http.Handler {
	if handler == nil {
		handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] == nil {
			continue
		}
		handler = mws[i](handler)
	}
	return handler
}

// RequestID 为每个响应添加唯一 X-Request-ID。
// RequestID adds a unique X-Request-ID header to every response.
// 传入的 X-Request-ID 仅在其非空且不超过上限时回显。
// an incoming X-Request-ID is echoed only when within the limit.
// 上限默认 DefaultMaxRequestIDLength，可用 WithRequestIDMaxLength 调整。
// limit defaults to DefaultMaxRequestIDLength, configurable via WithRequestIDMaxLength.
func RequestID(opts ...RequestIDOption) Middleware {
	cfg := requestIDConfig{maxLength: DefaultMaxRequestIDLength}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" || len(id) > cfg.maxLength {
				b := make([]byte, 16)
				_, _ = rand.Read(b)
				id = hex.EncodeToString(b)
			}
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
		})
	}
}

// CORSConfig 配置 CORS 中间件。
// CORSConfig configures CORS middleware.
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	MaxAge           int
}

// CORS 返回 CORS 中间件。
// CORS returns a CORS middleware.
func CORS(cfg CORSConfig) Middleware {
	allowMethods := strings.Join(cfg.AllowMethods, ", ")
	allowHeaders := strings.Join(cfg.AllowHeaders, ", ")
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strconv.Itoa(cfg.MaxAge)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowedOrigin, allowed := corsAllowedOrigin(origin, cfg.AllowOrigins, cfg.AllowCredentials)
			if origin != "" {
				addVary(w.Header(), "Origin")
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
				if allowMethods != "" {
					w.Header().Set("Access-Control-Allow-Methods", allowMethods)
				}
				if allowHeaders != "" {
					w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
				}
				if cfg.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				if maxAge != "" {
					w.Header().Set("Access-Control-Max-Age", maxAge)
				}
			}

			if r.Method == http.MethodOptions && origin != "" && r.Header.Get("Access-Control-Request-Method") != "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func corsAllowedOrigin(origin string, allowedOrigins []string, allowCredentials bool) (string, bool) {
	if origin == "" {
		return "", false
	}
	for _, allowed := range allowedOrigins {
		switch {
		case allowed == "*" && !allowCredentials:
			return "*", true
		case allowed == origin:
			return origin, true
		}
	}
	return "", false
}

func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

// RequestLogger 记录每个请求。
// RequestLogger logs each request.
func RequestLogger() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := newLoggingResponseWriter(w)
			next.ServeHTTP(rw, r)
			if logger := loggerFromRequest(r); logger != nil {
				logger.InfoContext(r.Context(), "http request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", rw.Status(),
					"size", rw.Size(),
					"duration", time.Since(start),
				)
			}
		})
	}
}

type loggingResponseWriter interface {
	http.ResponseWriter
	Status() int
	Size() int
}

type flushLoggingResponseWriter struct {
	*ResponseWriter
}

func newLoggingResponseWriter(w http.ResponseWriter) loggingResponseWriter {
	rw := NewResponseWriter(w)
	if _, ok := w.(http.Flusher); ok {
		return &flushLoggingResponseWriter{ResponseWriter: rw}
	}
	return rw
}

func (w *flushLoggingResponseWriter) Flush() {
	if !w.written {
		w.WriteHeader(w.statusCode)
	}
	w.ResponseWriter.ResponseWriter.(http.Flusher).Flush()
}

// Recoverer 捕获 panic 并返回 500。
// Recoverer catches panics and returns 500.
func Recoverer() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if logger := loggerFromRequest(r); logger != nil {
						logger.ErrorContext(r.Context(), "panic recovered",
							"panic", rec,
							"stack", string(debug.Stack()),
							"method", r.Method,
							"path", r.URL.Path,
						)
					}
					if server := serverFromRequest(r); server != nil {
						writeError(w, r, server, http.StatusInternalServerError, Err(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)))
						return
					}
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func loggerFromRequest(r *http.Request) Logger {
	server := serverFromRequest(r)
	if server == nil {
		return nil
	}
	return server.logger
}

func serverFromRequest(r *http.Request) *Server {
	if r == nil {
		return nil
	}
	state, _ := r.Context().Value(requestStateContextKey{}).(requestState)
	return state.server
}

// Timeout 为请求 context 增加超时。
// Timeout adds a timeout to the request context.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			rec := newTimeoutResponseWriter(responseWriteStateFromRequest(r))
			done := make(chan struct{})
			panicCh := make(chan interface{}, 1)
			go func() {
				defer close(done)
				defer func() {
					if rec := recover(); rec != nil {
						panicCh <- rec
					}
				}()
				next.ServeHTTP(rec, r.WithContext(ctx))
			}()
			select {
			case <-done:
				select {
				case rec := <-panicCh:
					panic(rec)
				default:
				}
				rec.WriteTo(w)
			case rec := <-panicCh:
				panic(rec)
			case <-ctx.Done():
				rec.stop()
				rec.state.clearBuffered()
				if server := serverFromRequest(r); server != nil {
					writeError(w, r, server, http.StatusGatewayTimeout, Err(http.StatusGatewayTimeout, http.StatusText(http.StatusGatewayTimeout)))
					return
				}
				http.Error(w, http.StatusText(http.StatusGatewayTimeout), http.StatusGatewayTimeout)
			}
		})
	}
}

type timeoutResponseWriter struct {
	mu        sync.Mutex
	stopped   bool
	streaming bool
	header    http.Header
	body      bytes.Buffer
	status    int
	wrote     bool
	state     *responseWriteState
}

func newTimeoutResponseWriter(state *responseWriteState) *timeoutResponseWriter {
	return &timeoutResponseWriter{header: make(http.Header), state: state}
}

func (w *timeoutResponseWriter) Header() http.Header {
	return w.header
}

func (w *timeoutResponseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return 0, http.ErrHandlerTimeout
	}
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
		w.state.markBuffered()
	}
	return w.body.Write(data)
}

func (w *timeoutResponseWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.state.markBuffered()
}

// stop 让 writer 丢弃超时后 handler goroutine 的后续写入。
// stop discards writes from a handler goroutine that outlives the timeout.
// 避免迟到写入与已发送响应竞争或破坏。
// late writes cannot race with or corrupt the sent response.
func (w *timeoutResponseWriter) stop() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
}

func (w *timeoutResponseWriter) WriteTo(dst http.ResponseWriter) {
	w.mu.Lock()
	if w.streaming || w.stopped {
		w.mu.Unlock()
		w.state.clearBuffered()
		return
	}
	header := w.header.Clone()
	body := append([]byte(nil), w.body.Bytes()...)
	status := w.status
	wrote := w.wrote
	w.mu.Unlock()
	w.state.clearBuffered()

	for key, values := range header {
		dst.Header()[key] = append([]string(nil), values...)
	}
	if !wrote {
		status = http.StatusOK
	}
	dst.WriteHeader(status)
	_, _ = dst.Write(body)
}

// Unwrap 让 http.ResponseController 访问底层 writer。
// Unwrap lets http.ResponseController reach the underlying writer.
func (w *timeoutResponseWriter) Unwrap() http.ResponseWriter {
	return w.state
}

// Flush 将缓冲的 header/body 提交到底层 writer 并刷新。
// Flush commits buffered data and flushes the underlying writer.
// 流式响应（SSE、chunked）在 Timeout 内仍可工作。
// streaming responses (SSE, chunked) keep working inside Timeout.
func (w *timeoutResponseWriter) Flush() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
		w.state.markBuffered()
	}
	header := w.header.Clone()
	status := w.status
	body := append([]byte(nil), w.body.Bytes()...)
	w.body.Reset()
	w.streaming = true
	w.mu.Unlock()

	for key, values := range header {
		w.state.Header()[key] = append([]string(nil), values...)
	}
	w.state.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.state.Write(body)
	}
	if flusher, ok := w.state.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack 委托 WebSocket 升级，使 Timeout 内的连接可用。
// Hijack delegates the WebSocket upgrade inside Timeout.
func (w *timeoutResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return nil, nil, http.ErrNotSupported
	}
	w.stopped = true
	w.mu.Unlock()
	hijacker, ok := w.state.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}
