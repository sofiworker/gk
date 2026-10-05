package ghttp

import (
	"bufio"
	"net"
	"net/http"
)

// Response 封装 HTTP 响应写入。它本身实现 http.ResponseWriter，并通过 Unwrap 暴露底层写入器，
// 因此可以交给 http.NewResponseController 使用 Flush、Hijack、SetWriteDeadline 等能力。
// Response wraps HTTP response writing. It implements http.ResponseWriter and exposes the
// underlying writer via Unwrap, so http.NewResponseController can Flush, Hijack,
// SetWriteDeadline, etc.
type Response struct {
	// Writer 是底层的 http.ResponseWriter。中间件可以替换它（如压缩），替换后的写入器应实现
	// Unwrap 以保留 Flush/Hijack 能力。
	// Writer is the underlying http.ResponseWriter. Middleware may replace it (e.g. for
	// compression); a replacement should implement Unwrap to keep Flush/Hijack available.
	Writer http.ResponseWriter

	// statusCode 记录已写入的状态码（用于日志）
	// statusCode records the written status code (for logging)
	statusCode int

	// written 标记是否已写入响应（防止重复写入）
	// written marks whether the response has been written (to prevent duplicate writes)
	written bool

	// size 是已写入的响应体字节数
	// size is the number of body bytes written
	size int64

	// hijacked 标记连接已被接管
	// hijacked marks that the connection was hijacked
	hijacked bool
}

// 编译期断言 *Response 满足 http.ResponseWriter。
// Compile-time assertion that *Response satisfies http.ResponseWriter.
var (
	_ http.ResponseWriter = (*Response)(nil)
	_ http.Flusher        = (*Response)(nil)
	_ http.Hijacker       = (*Response)(nil)
)

// WriteHeader 写入 HTTP 状态码。重复调用被忽略。
// WriteHeader writes the HTTP status code. Repeated calls are ignored.
func (r *Response) WriteHeader(code int) {
	if r.written {
		return
	}
	r.statusCode = code
	r.Writer.WriteHeader(code)
	r.written = true
}

// Write 写入响应体。未显式写状态码时先写 200。
// Write writes the response body, writing 200 first if no status was written.
func (r *Response) Write(data []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.Writer.Write(data)
	r.size += int64(n)
	return n, err
}

// Header 返回响应头。
// Header returns the response headers.
func (r *Response) Header() http.Header {
	return r.Writer.Header()
}

// StatusCode 返回已写入的状态码；尚未写入时返回 200。
// StatusCode returns the written status code, or 200 when nothing was written yet.
func (r *Response) StatusCode() int {
	if r.statusCode == 0 {
		return http.StatusOK
	}
	return r.statusCode
}

// Written 报告状态码是否已写出。写出后不能再修改状态码和响应头。
// Written reports whether the status has been sent; headers and status are fixed afterwards.
func (r *Response) Written() bool {
	return r.written
}

// Size 返回已写入的响应体字节数。
// Size returns the number of body bytes written.
func (r *Response) Size() int64 {
	return r.size
}

// Unwrap 返回底层写入器，供 http.NewResponseController 使用。
// Unwrap returns the underlying writer for http.NewResponseController.
func (r *Response) Unwrap() http.ResponseWriter {
	return r.Writer
}

// FlushError 立即把已缓冲的数据发送给客户端；底层不支持时返回 http.ErrNotSupported。
// 未写状态码时先写 200。签名与 http.ResponseController 识别的 FlushError 一致。
// FlushError sends buffered data to the client immediately, returning
// http.ErrNotSupported when the underlying writer cannot flush. Writes 200 first if no
// status was written. The signature matches the FlushError recognized by
// http.ResponseController.
func (r *Response) FlushError() error {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(r.Writer).Flush()
}

// Flush 实现 http.Flusher；需要感知失败时使用 FlushError。
// Flush implements http.Flusher; use FlushError to observe failures.
func (r *Response) Flush() {
	_ = r.FlushError()
}

// Hijack 接管底层连接（WebSocket 等协议升级使用），并把响应标记为已提交，使之后的错误不会再
// 尝试写响应。底层不支持时返回 http.ErrNotSupported。
// Hijack takes over the underlying connection (used by WebSocket and other upgrades) and
// marks the response committed, so later errors never try to write to it. Returns
// http.ErrNotSupported when the underlying writer cannot hijack.
func (r *Response) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(r.Writer).Hijack()
	if err == nil {
		r.written = true
		r.hijacked = true
		if r.statusCode == 0 {
			r.statusCode = http.StatusSwitchingProtocols
		}
	}
	return conn, brw, err
}

// Hijacked 报告连接是否已被接管。
// Hijacked reports whether the connection has been hijacked.
func (r *Response) Hijacked() bool {
	return r.hijacked
}
