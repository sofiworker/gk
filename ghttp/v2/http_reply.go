package v2

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// FileReply 通过标准库提供可定位内容的 Range、条件请求和 HEAD 语义。
// FileReply serves seekable content with standard-library Range, conditional request and HEAD semantics.
// Request 必须是当前请求；需要关闭 Content 时将其同时设置为 Closer，handler 不应 defer Close。
// Request must be the current request; set Content as Closer when it needs closing, without deferring Close in the handler.
type FileReply struct {
	Request      *http.Request
	Name         string
	ModTime      time.Time
	Content      io.ReadSeeker
	Closer       io.Closer
	DownloadName string
}

func (r FileReply) encodeReply(resp *Response) (err error) {
	defer closeHTTPReply(r.Closer, &err)
	if r.Request == nil || r.Content == nil {
		return errors.New("ghttp/v2: file reply requires request and content")
	}
	if r.DownloadName != "" {
		value := mime.FormatMediaType("attachment", map[string]string{"filename": r.DownloadName})
		if value == "" {
			return errors.New("ghttp/v2: invalid download filename")
		}
		resp.Header().Set("Content-Disposition", value)
	}
	http.ServeContent(resp, r.Request, r.Name, r.ModTime, r.Content)
	return nil
}

func closeHTTPReply(closer io.Closer, err *error) {
	if closer != nil {
		if closeErr := closer.Close(); closeErr != nil {
			*err = errors.Join(*err, fmt.Errorf("ghttp/v2: close response content: %w", closeErr))
		}
	}
}

// RedirectReply 返回显式 Location 和重定向状态码，Status 为零时使用 302。
// RedirectReply returns an explicit Location and redirect status, defaulting to 302.
type RedirectReply struct {
	Location string
	Status   int
}

func (r RedirectReply) encodeReply(resp *Response) error {
	if r.Location == "" || strings.ContainsAny(r.Location, "\r\n") {
		return errors.New("ghttp/v2: invalid redirect location")
	}
	status := r.Status
	if status == 0 {
		status = http.StatusFound
	}
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return fmt.Errorf("ghttp/v2: invalid redirect status %d", status)
	}
	resp.Header().Set("Location", r.Location)
	resp.WriteHeader(status)
	return nil
}

// StreamReply 按需复制普通 HTTP 响应体；需要关闭 Reader 时同时设置 Closer，handler 不应 defer Close。
// StreamReply copies a regular HTTP body on demand; set Reader as Closer when it needs closing, without deferring Close in the handler.
// 设置 Request 可在 HEAD 请求中避免读取 Reader。
// Set Request to avoid consuming Reader on HEAD requests.
type StreamReply struct {
	Request     *http.Request
	Reader      io.Reader
	Closer      io.Closer
	ContentType string
	Status      int
}

func (r StreamReply) encodeReply(resp *Response) (err error) {
	defer closeHTTPReply(r.Closer, &err)
	if r.Reader == nil {
		return errors.New("ghttp/v2: stream reply requires reader")
	}
	if r.Status != 0 && (r.Status < 200 || r.Status > 599) {
		return errors.New("ghttp/v2: invalid stream status")
	}
	if r.ContentType != "" {
		resp.Header().Set("Content-Type", r.ContentType)
	}
	if r.Status != 0 {
		resp.WriteHeader(r.Status)
	}
	if r.Status == http.StatusNoContent || r.Status == http.StatusNotModified || r.Request != nil && r.Request.Method == http.MethodHead {
		return nil
	}
	_, err = io.Copy(resp, r.Reader)
	if err != nil {
		return fmt.Errorf("ghttp/v2: copy response stream: %w", err)
	}
	return nil
}
