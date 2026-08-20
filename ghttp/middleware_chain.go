package ghttp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Middleware 是执行链成员:与 HandlerFunc 同型(gin 风格),最后一个不调用
// Next 的即为终端。
// Middleware is an execution chain member: same type as HandlerFunc (gin
// style); the last one that never calls Next is the terminal.
type Middleware = HandlerFunc

// Chain 编译期固化:中间件在前、终端殿后,注册期执行一次,请求期零分配。
// Chain freezes at registration time: middleware first, terminal last; runs
// once at registration with zero per-request cost.
func Chain(terminal HandlerFunc, mws ...Middleware) []HandlerFunc {
	if len(mws) == 0 {
		return []HandlerFunc{terminal}
	}
	handlers := make([]HandlerFunc, len(mws)+1)
	copy(handlers, mws)
	handlers[len(mws)] = terminal
	return handlers
}

// --- RequestID ---

// DefaultMaxRequestIDLength 是回显请求 ID 的默认长度上限。
// DefaultMaxRequestIDLength is the default length cap for echoing a request ID.
const DefaultMaxRequestIDLength = 64

type requestIDConfig struct {
	maxLength int
}

// RequestIDOption 调整 RequestID 行为。
// RequestIDOption tunes RequestID behavior.
type RequestIDOption func(*requestIDConfig)

// WithRequestIDMaxLength 设置回显 X-Request-ID 的长度上限。
// WithRequestIDMaxLength caps the echoed X-Request-ID length.
func WithRequestIDMaxLength(n int) RequestIDOption {
	return func(cfg *requestIDConfig) { cfg.maxLength = n }
}

// GetRequestID 返回当前请求的请求 ID(仅 RequestID 中间件之后可用)。
// GetRequestID returns the current request ID (available only after the
// RequestID middleware ran).
func GetRequestID(c *Ctx) string {
	id, _ := GetValue[string](c, requestIDStoreKey)
	return id
}

const requestIDStoreKey = "ghttp.requestID"

// RequestID 为每个响应添加唯一 X-Request-ID。
// RequestID adds a unique X-Request-ID header to every response.
// 传入的 X-Request-ID 仅在其非空且不超过上限时回显。
// an incoming X-Request-ID is echoed only when non-empty and within the limit.
// 上限默认 DefaultMaxRequestIDLength,可用 WithRequestIDMaxLength 调整。
// the limit defaults to DefaultMaxRequestIDLength, configurable via
// WithRequestIDMaxLength.
func RequestID(opts ...RequestIDOption) HandlerFunc {
	cfg := requestIDConfig{maxLength: DefaultMaxRequestIDLength}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return func(c *Ctx) {
		id := c.R.Header.Get("X-Request-ID")
		if id == "" || len(id) > cfg.maxLength {
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		c.W.Header().Set("X-Request-ID", id)
		c.Set(requestIDStoreKey, id)
		c.Next()
	}
}

// --- CORS ---

// CORSConfig 配置 CORS 中间件。
// CORSConfig configures CORS middleware.
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	MaxAge           int
}

// CORS 返回 CORS 中间件。语义与旧 http.Handler 版本逐项一致。
// CORS returns a CORS middleware with semantics matching the legacy
// http.Handler version item by item.
func CORS(cfg CORSConfig) HandlerFunc {
	allowMethods := strings.Join(cfg.AllowMethods, ", ")
	allowHeaders := strings.Join(cfg.AllowHeaders, ", ")
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strconv.Itoa(cfg.MaxAge)
	}

	return func(c *Ctx) {
		origin := c.R.Header.Get("Origin")
		allowedOrigin, allowed := corsAllowedOrigin(origin, cfg.AllowOrigins, cfg.AllowCredentials)
		if origin != "" {
			addVary(c.W.Header(), "Origin")
		}

		if allowed {
			c.W.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			if allowMethods != "" {
				c.W.Header().Set("Access-Control-Allow-Methods", allowMethods)
			}
			if allowHeaders != "" {
				c.W.Header().Set("Access-Control-Allow-Headers", allowHeaders)
			}
			if cfg.AllowCredentials {
				c.W.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if maxAge != "" {
				c.W.Header().Set("Access-Control-Max-Age", maxAge)
			}
		}

		if c.R.Method == http.MethodOptions && origin != "" && c.R.Header.Get("Access-Control-Request-Method") != "" {
			c.WriteHeader(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func corsAllowedOrigin(origin string, allowedOrigins []string, allowCredentials bool) (string, bool) {
	if origin == "" {
		return "", false
	}
	if len(allowedOrigins) == 0 {
		return "", false
	}
	for _, allowed := range allowedOrigins {
		if allowed == "*" && !allowCredentials {
			return "*", true
		}
		if allowed == origin {
			return origin, true
		}
	}
	return "", false
}

func addVary(header http.Header, value string) {
	for _, existing := range header["Vary"] {
		for _, part := range strings.Split(existing, ", ") {
			if part == value {
				return
			}
		}
	}
	header.Add("Vary", value)
}

// --- RequestLogger ---

// loggingResponseWriter 拦截 Write 以记录状态码与字节数。
// loggingResponseWriter intercepts Write to record status and bytes.
type loggingResponseWriter interface {
	http.ResponseWriter
	Status() int
	Bytes() int
}

type baseLoggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *baseLoggingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (w *baseLoggingResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *baseLoggingResponseWriter) Status() int { return w.status }
func (w *baseLoggingResponseWriter) Bytes() int  { return w.bytes }
func (w *baseLoggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying writer does not support hijacking")
}

// flushLoggingResponseWriter 仅在下游支持 http.Flusher 时实现 Flush,
// 避免包装器凭空"发明"能力。
// flushLoggingResponseWriter implements Flush only when the underlying writer
// supports http.Flusher, so the wrapper never invents the capability.
type flushLoggingResponseWriter struct {
	*baseLoggingResponseWriter
}

func (w *flushLoggingResponseWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func newLoggingResponseWriter(w http.ResponseWriter) loggingResponseWriter {
	base := &baseLoggingResponseWriter{ResponseWriter: w}
	if _, ok := w.(http.Flusher); ok {
		return &flushLoggingResponseWriter{baseLoggingResponseWriter: base}
	}
	return base
}

// RequestLogger 记录请求方法、路径、状态码与字节数。
// RequestLogger logs the request method, path, status code, and bytes.
func RequestLogger() HandlerFunc {
	return func(c *Ctx) {
		start := time.Now()
		rec := newLoggingResponseWriter(c.W)
		c.W = rec
		c.Next()
		if c.server != nil && c.server.logger != nil {
			c.server.logger.InfoContext(c.R.Context(), "http request",
				"method", c.R.Method, "path", c.R.URL.Path,
				"status", rec.Status(), "size", rec.Bytes(),
				"duration", time.Since(start))
		} else {
			log.Printf("http request: method=%s path=%s status=%d size=%d duration=%s",
				c.R.Method, c.R.URL.Path, rec.Status(), rec.Bytes(), time.Since(start))
		}
	}
}

// --- Recoverer ---

// Recoverer 捕获中间件链 panic:记录堆栈日志,经 writeError 写 500,
// 不 re-panic(避免 ServeHTTP 兜底 defer 二次写响应)。
// Recoverer catches panics in the middleware chain: logs the stack, writes
// 500 via writeError, and never re-panics (avoids the ServeHTTP escape-hatch
// defer writing the response a second time).
func Recoverer() HandlerFunc {
	return func(c *Ctx) {
		defer func() {
			if err := recover(); err != nil {
				if c.server != nil {
					c.server.logger.ErrorContext(c.R.Context(), "panic recovered",
						"panic", err, "method", c.R.Method, "path", c.R.URL.Path)
				} else {
					log.Printf("panic recovered: %v method=%s path=%s", err, c.R.Method, c.R.URL.Path)
				}
				writeError(c, c.R, c.server, http.StatusInternalServerError,
					Err(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)))
			}
		}()
		c.Next()
	}
}

// --- Timeout ---

// Timeout 限制剩余链的执行时间;超时写 504 Gateway Timeout。
// 注意:504 写出后、子链排空前,自定义 ErrorHandler 不应再读请求输入
// (form/params 缓存在子链排空完成前仍可能被子 goroutine 写入)。
// Timeout bounds the remaining chain's execution; on expiry it writes 504
// Gateway Timeout. Note: after the 504 is written and before the child chain
// drains, a custom ErrorHandler must not read request inputs again — the
// form/params caches may still be written by the child goroutine until the
// drain completes.
func Timeout(d time.Duration) HandlerFunc {
	return func(c *Ctx) {
		rest := c.handlers[c.index:]
		c.index = len(c.handlers)

		child := *c
		child.index = 0
		child.handlers = rest
		rec := newTimeoutResponseWriter(c)
		child.W = rec
		ctx, cancel := context.WithCancel(c.R.Context())
		defer cancel()
		// 把子 Ctx 挂到子链请求 context:子链的错误抑制检查读子 Ctx
		// 自己的 committed 字段,不跨 goroutine 访问父 Ctx。
		// attach the child Ctx to the child chain's request context:
		// the child chain's error-suppression check reads the child
		// Ctx's own committed field, never crossing goroutines.
		child.R = c.R.WithContext(context.WithValue(ctx, ctxKey{}, &child))

		done := make(chan struct{})
		panicCh := make(chan any, 1)
		go func() {
			defer close(done)
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()
			child.Next()
		}()

		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-done:
			select {
			case r := <-panicCh:
				panic(r)
			default:
			}
			// writeTo 经父 Ctx 写入,父 committed 由父 goroutine 设置,零竞争。
			// writeTo writes through the parent Ctx; parent.committed is set
			// on the parent goroutine, race-free.
			rec.writeTo(c)
		case r := <-panicCh:
			panic(r)
		case <-timer.C:
			cancel() // 立即取消子链 context,解除其阻塞等待。
			// cancel immediately unblocks any child handler waiting on the
			// request context.
			rec.stop()
			writeError(c.W, c.R, c.server, http.StatusGatewayTimeout, Err(http.StatusGatewayTimeout, http.StatusText(http.StatusGatewayTimeout)))
			select {
			case <-done:
			case <-panicCh: // 超时后 panic 已被 504 覆盖,直接吞掉。
				// a post-timeout panic is superseded by the 504; swallow it.
			}
		}
	}
}

// timeoutResponseWriter 缓冲子链输出,超时可丢弃;支持 Flush 流式直通
// (SSE/chunked)与 Hijack 委托(WebSocket)。
// timeoutResponseWriter buffers the child chain output, discardable on expiry;
// it supports Flush streaming pass-through (SSE/chunked) and Hijack
// delegation (WebSocket).
type timeoutResponseWriter struct {
	mu        sync.Mutex
	stopped   bool
	streaming bool
	header    http.Header
	body      bytes.Buffer
	status    int
	wrote     bool
	parent    http.ResponseWriter
}

func newTimeoutResponseWriter(parent http.ResponseWriter) *timeoutResponseWriter {
	return &timeoutResponseWriter{header: make(http.Header), parent: parent}
}

func (w *timeoutResponseWriter) Header() http.Header { return w.header }

func (w *timeoutResponseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return 0, http.ErrHandlerTimeout
	}
	if w.streaming {
		return w.parent.Write(data)
	}
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.body.Write(data)
}

func (w *timeoutResponseWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.wrote = true
	if w.status == 0 {
		w.status = status
	}
}

func (w *timeoutResponseWriter) stop() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
}

// Flush 将缓冲的 header/body 提交到父 writer 并进入流式直通。提交期间
// 持有锁:父 goroutine 的 stop() 会等待提交完成后再写 504,避免并发写
// 底层 writer。
// Flush commits the buffered header/body to the parent writer and switches
// into streaming pass-through. It holds the lock during the commit: the
// parent goroutine's stop() waits for the commit before writing 504,
// preventing concurrent writes to the underlying writer.
func (w *timeoutResponseWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	for key, values := range w.header {
		w.parent.Header()[key] = append([]string(nil), values...)
	}
	w.parent.WriteHeader(w.status)
	if w.body.Len() > 0 {
		_, _ = w.parent.Write(w.body.Bytes())
	}
	w.body.Reset()
	w.streaming = true
	if flusher, ok := w.parent.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack 委托 WebSocket 升级,使 Timeout 内的连接可用。
// Hijack delegates the WebSocket upgrade so connections work inside Timeout.
func (w *timeoutResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return nil, nil, fmt.Errorf("response timeout")
	}
	if h, ok := w.parent.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying writer does not support hijacking")
}

// writeTo 在子链正常完成时把剩余缓冲写回父 writer;已流式直通则跳过。
// writeTo writes the remaining buffer back to the parent writer when the
// child chain completes normally; it is a no-op after streaming started.
func (w *timeoutResponseWriter) writeTo(writer http.ResponseWriter) {
	w.mu.Lock()
	header := w.header.Clone()
	status := w.status
	body := append([]byte(nil), w.body.Bytes()...)
	streaming := w.streaming
	w.mu.Unlock()
	if streaming {
		return
	}
	for key, values := range header {
		writer.Header()[key] = append([]string(nil), values...)
	}
	if status == 0 {
		status = http.StatusOK
	}
	writer.WriteHeader(status)
	if len(body) > 0 {
		_, _ = writer.Write(body)
	}
}

// --- 辅助 ---
