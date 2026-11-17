package ghttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"time"
)

// Reply 提供高级响应控制，允许同时返回 body、状态码、headers 和 cookies。
// Reply provides advanced response control, allowing simultaneous return of body, status code, headers, and cookies.
//
// 用于需要精细控制响应的场景。
// Used for scenarios requiring fine-grained response control.
type Reply[T ResponseConstraint] struct {
	// Body 是响应体（默认 JSON 序列化）。
	// Body is the response body (default JSON serialization).
	Body T

	// Status 是 HTTP 状态码（未指定时使用 200）。
	// Status is the HTTP status code (defaults to 200 if not specified).
	Status int

	// Headers 是自定义响应头。
	// Headers are custom response headers.
	Headers http.Header

	// Cookies 是要设置的 cookies。
	// Cookies are the cookies to be set.
	Cookies []*http.Cookie
}

// FileReply 表示文件响应，支持 Range、Last-Modified、条件请求。
// FileReply represents a file response, supporting Range, Last-Modified, and conditional requests.
type FileReply struct {
	// Path 是文件系统路径。
	// Path is the filesystem path.
	Path string

	// Reader 是文件内容读取器（优先级高于 Path）。
	// Reader is the file content reader (takes precedence over Path).
	Reader io.ReadSeeker

	// Name 是文件名（用于 Content-Disposition）。
	// Name is the filename (used for Content-Disposition).
	Name string

	// ContentType 是 MIME 类型（未指定时自动检测）。
	// ContentType is the MIME type (auto-detected if not specified).
	ContentType string

	// Size 是文件大小（用于 Content-Length）。
	// Size is the file size (used for Content-Length).
	Size int64

	// ModTime 是最后修改时间（用于 Last-Modified 和条件请求）。
	// ModTime is the last modification time (used for Last-Modified and conditional requests).
	ModTime time.Time

	// Attachment 为 true 时触发下载（Content-Disposition: attachment）。
	// Attachment triggers download when true (Content-Disposition: attachment).
	Attachment bool
}

// ServeFile 使用 http.ServeContent 提供文件响应。
// ServeFile provides file response using http.ServeContent.
//
// 支持 Range 请求、条件请求（If-Modified-Since、If-None-Match）。
// Supports Range requests and conditional requests (If-Modified-Since, If-None-Match).
func (f *FileReply) ServeFile(resp *Response, req *Request) error {
	var content io.ReadSeeker
	var modTime time.Time

	// 优先使用 Reader
	// Prioritize Reader
	if f.Reader != nil {
		content = f.Reader
		modTime = f.ModTime
	} else if f.Path != "" {
		// 从文件系统读取
		// Read from filesystem
		file, err := os.Open(f.Path)
		if err != nil {
			return NotFound("file not found")
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			return InternalServerError("failed to stat file")
		}

		content = file
		modTime = stat.ModTime()

		// 自动设置文件名
		// Auto-set filename
		if f.Name == "" {
			f.Name = stat.Name()
		}
	} else {
		return BadRequest("either Path or Reader must be specified")
	}

	// 设置 Content-Type
	// Set Content-Type
	if f.ContentType != "" {
		resp.Header().Set("Content-Type", f.ContentType)
	}

	// 设置 Content-Disposition
	// Set Content-Disposition
	if f.Name != "" {
		disposition := "inline"
		if f.Attachment {
			disposition = "attachment"
		}
		resp.Header().Set("Content-Disposition", disposition+`; filename="`+f.Name+`"`)
	}

	// 使用 http.ServeContent 处理 Range 和条件请求
	// Use http.ServeContent to handle Range and conditional requests
	http.ServeContent(resp, req.Raw, f.Name, modTime, content)
	return nil
}

// StreamReply 表示流式响应（如 SSE、chunked transfer）。
// StreamReply represents a streaming response (e.g., SSE, chunked transfer).
type StreamReply struct {
	// ContentType 是响应的 Content-Type（默认 text/plain）。
	// ContentType is the response Content-Type (defaults to text/plain).
	ContentType string

	// Writer 是流式写入函数。
	// Writer is the streaming write function.
	Writer func(w io.Writer) error
}

// Stream 执行流式写入。
// Stream performs streaming write.
func (s *StreamReply) Stream(resp *Response) error {
	if s.ContentType == "" {
		s.ContentType = "text/plain; charset=utf-8"
	}
	resp.Header().Set("Content-Type", s.ContentType)
	resp.WriteHeader(http.StatusOK)

	if s.Writer != nil {
		return s.Writer(resp)
	}
	return nil
}

// RedirectReply 表示重定向响应。
// RedirectReply represents a redirect response.
type RedirectReply struct {
	// URL 是重定向目标 URL。
	// URL is the redirect target URL.
	URL string

	// Status 是重定向状态码（301、302、303、307、308）。
	// Status is the redirect status code (301, 302, 303, 307, 308).
	//
	// 默认 302（Found）。
	// Defaults to 302 (Found).
	Status int
}

// Redirect 执行重定向。
// Redirect performs the redirect.
func (r *RedirectReply) Redirect(resp *Response, req *Request) {
	status := r.Status
	if status == 0 {
		status = http.StatusFound // 302
	}
	http.Redirect(resp, req.Raw, r.URL, status)
}

// NoContentReply 表示 204 No Content 响应（无响应体）。
// NoContentReply represents a 204 No Content response (no response body).
type NoContentReply struct{}

// WriteNoContent 写入 204 响应。
// WriteNoContent writes a 204 response.
func (n *NoContentReply) WriteNoContent(resp *Response) {
	resp.WriteHeader(http.StatusNoContent)
}

// responder 是能自行写出响应的返回值类型。typed handler 返回实现了它的值（或其指针）时，
// 框架调用它而不是把返回值当普通结构体 JSON 序列化。
// responder is a result type that writes its own response. When a typed handler returns
// such a value (or a pointer to one), the framework calls it instead of JSON-encoding the
// value as a plain struct.
type responder interface {
	respond(ctx context.Context, req *Request, resp *Response) error
}

// 编译期断言各响应类型满足 responder。
// Compile-time assertions that the reply types satisfy responder.
var (
	_ responder = Reply[any]{}
	_ responder = FileReply{}
	_ responder = StreamReply{}
	_ responder = RedirectReply{}
	_ responder = NoContentReply{}
)

// respond 依次写出 Headers、Cookies、状态码（未设置时为 200）与 JSON body；204/304 不写 body。
// respond writes Headers, Cookies, the status (200 when unset) and the JSON body; 204/304
// carry no body.
func (r Reply[T]) respond(_ context.Context, _ *Request, resp *Response) error {
	status := r.Status
	if status == 0 {
		status = http.StatusOK
	}
	if status < 100 || status > 999 {
		return fmt.Errorf("ghttp: invalid Reply status %d", status)
	}
	h := resp.Header()
	for k, vs := range r.Headers {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	for _, c := range r.Cookies {
		if c != nil {
			http.SetCookie(resp, c)
		}
	}
	return writeJSON(resp, status, r.Body)
}

// respond 实现 responder。
// respond implements responder.
func (f FileReply) respond(_ context.Context, req *Request, resp *Response) error {
	return f.ServeFile(resp, req)
}

// respond 实现 responder。
// respond implements responder.
func (s StreamReply) respond(_ context.Context, _ *Request, resp *Response) error {
	return s.Stream(resp)
}

// respond 实现 responder。
// respond implements responder.
func (r RedirectReply) respond(_ context.Context, req *Request, resp *Response) error {
	if r.URL == "" {
		return fmt.Errorf("ghttp: RedirectReply requires a URL")
	}
	r.Redirect(resp, req)
	return nil
}

// respond 实现 responder。
// respond implements responder.
func (n NoContentReply) respond(_ context.Context, _ *Request, resp *Response) error {
	n.WriteNoContent(resp)
	return nil
}

// asResponder 判断 handler 返回值是否为 responder；nil 指针不算，避免在 nil 上调用方法。
// asResponder reports whether a handler result is a responder; nil pointers are excluded
// so methods are never invoked on nil.
func asResponder(v any) (responder, bool) {
	r, ok := v.(responder)
	if !ok {
		return nil, false
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil, false
	}
	return r, true
}
