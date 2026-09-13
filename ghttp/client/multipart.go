package client

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"os"
	"strings"
)

// 本文件实现 multipart/form-data 上传。与 resty 的 map+魔法键（"@"+param）不同，这里用
// 显式类型表达部件，避免"字段名里藏语义"。
// This file implements multipart/form-data uploads. Unlike resty's map with magic keys
// ("@"+param), parts are expressed with explicit types, so no meaning hides inside a
// field name.

// MultipartField 描述一个 multipart 部件。三种载体按优先级取其一：
//
//   - Path 非空：每次尝试时重新打开该文件（天然可重放，适合重试）；
//   - Reader 非空：直接读取；若它不是 io.Seeker，则整请求不可重放（重试会被拒绝）；
//   - 两者皆空：写一个空的具名部件。
//
// MultipartField describes one multipart part. Exactly one carrier is used, by
// precedence:
//
//   - Path non-empty: the file is reopened on every attempt (replayable by
//     construction, safe with retries);
//   - Reader non-empty: read directly; unless it is an io.Seeker the whole request is
//     unreplayable (retries are refused);
//   - both empty: an empty named part is written.
type MultipartField struct {
	Field       string
	FileName    string
	ContentType string
	Path        string
	Reader      io.Reader
}

// SetMultipartFormData 让请求改以 multipart/form-data 发送，并写入文本字段。
// SetMultipartFormData switches the request to multipart/form-data and writes text
// fields.
func (r *Request) SetMultipartFormData(fields map[string]string) *Request {
	if r.formData == nil {
		r.formData = url.Values{}
	}
	for k, v := range fields {
		r.formData.Set(k, v)
	}
	r.isMultipart = true
	return r
}

// SetFile 让请求以 multipart/form-data 发送，并附上一个来自文件路径的部件。文件在每次
// 尝试时重新打开，因此配合重试是安全的。
// SetFile switches the request to multipart/form-data and attaches a part read from a
// file path. The file is reopened on every attempt, so it is safe with retries.
func (r *Request) SetFile(field, path string) *Request {
	return r.SetMultipartField(MultipartField{Field: field, Path: path})
}

// SetFileReader 让请求以 multipart/form-data 发送，并附上一个来自 reader 的部件。
// reader 不是 io.Seeker 时请求不可重放（重试会被拒绝，而不是静默发空部件）。
// SetFileReader switches the request to multipart/form-data and attaches a part read
// from a reader. A non-io.Seeker reader makes the request unreplayable (retries are
// refused rather than silently sending an empty part).
func (r *Request) SetFileReader(field, filename string, rc io.Reader) *Request {
	return r.SetMultipartField(MultipartField{Field: field, FileName: filename, Reader: rc})
}

// SetMultipartField 追加一个 multipart 部件（可多次调用）。
// SetMultipartField appends one multipart part (call it repeatedly for several parts).
func (r *Request) SetMultipartField(f MultipartField) *Request {
	if strings.TrimSpace(f.Field) == "" {
		r.record(fmt.Errorf("ghttp/client: multipart field name is empty"))
		return r
	}
	r.multipartFields = append(r.multipartFields, f)
	r.isMultipart = true
	return r
}

// SetMultipartFields 批量追加 multipart 部件。
// SetMultipartFields appends several multipart parts.
func (r *Request) SetMultipartFields(fields ...MultipartField) *Request {
	for _, f := range fields {
		r.SetMultipartField(f)
	}
	return r
}

// SetMultipartBoundary 覆盖自动生成的 multipart 边界串。
// SetMultipartBoundary overrides the auto-generated multipart boundary.
func (r *Request) SetMultipartBoundary(boundary string) *Request {
	r.multipartBoundary = boundary
	if boundary != "" {
		r.isMultipart = true
	}
	return r
}

// encodeMultipart 构建 multipart 请求体。它每次尝试都被重新调用，因此文件部件会被重新
// 打开、文本字段会被重新写出——这正是重试能安全重放 multipart 请求的原因。
// encodeMultipart builds the multipart body. It is called again on every attempt, so
// file parts are reopened and text fields rewritten — which is exactly why a multipart
// request can be safely replayed on retries.
func (r *Request) encodeMultipart() (io.Reader, string, error) {
	buf := new(bytes.Buffer)
	mw := multipart.NewWriter(buf)
	if r.multipartBoundary != "" {
		if err := mw.SetBoundary(r.multipartBoundary); err != nil {
			return nil, "", fmt.Errorf("ghttp/client: invalid multipart boundary: %w", err)
		}
	}

	for key, values := range r.formData {
		for _, value := range values {
			if err := mw.WriteField(key, value); err != nil {
				return nil, "", fmt.Errorf("ghttp/client: write multipart field %q: %w", key, err)
			}
		}
	}

	for _, f := range r.multipartFields {
		if err := writeMultipartPart(mw, f); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("ghttp/client: close multipart writer: %w", err)
	}
	return buf, mw.FormDataContentType(), nil
}

// writeMultipartPart 写出单个部件；路径型每次重新打开，reader 型直接读取。
// writeMultipartPart writes one part; a path carrier is reopened, a reader carrier is
// read as-is.
func writeMultipartPart(mw *multipart.Writer, f MultipartField) error {
	if f.Path != "" {
		file, err := os.Open(f.Path)
		if err != nil {
			return fmt.Errorf("ghttp/client: open multipart file %q: %w", f.Path, err)
		}
		defer func() { _ = file.Close() }()
		name := f.FileName
		if name == "" {
			name = baseName(f.Path)
		}
		part, err := createMultipartPart(mw, f.Field, name, f.ContentType)
		if err != nil {
			return err
		}
		if _, err := io.Copy(part, file); err != nil {
			return fmt.Errorf("ghttp/client: copy multipart file %q: %w", f.Path, err)
		}
		return nil
	}

	part, err := createMultipartPart(mw, f.Field, f.FileName, f.ContentType)
	if err != nil {
		return err
	}
	if f.Reader != nil {
		if _, err := io.Copy(part, f.Reader); err != nil {
			return fmt.Errorf("ghttp/client: copy multipart field %q: %w", f.Field, err)
		}
	}
	return nil
}

// createMultipartPart 创建一个部件：FileName 非空时按文件部件写 Content-Disposition，
// 否则写普通字段；ContentType 仅在显式给出时写入（不替调用方猜类型）。
// createMultipartPart creates a part: a non-empty FileName writes a file-style
// Content-Disposition, otherwise a plain field; ContentType is written only when given
// explicitly (this package never guesses a media type on the caller's behalf).
func createMultipartPart(mw *multipart.Writer, field, filename, contentType string) (io.Writer, error) {
	if filename == "" && contentType == "" {
		return mw.CreateFormField(field)
	}
	if filename != "" && contentType == "" {
		// 与标准库 multipart.CreateFormFile 的默认值一致：文件部件不带类型时用二进制流，
		// 否则对端拿不到任何类型信息。
		// Match the standard library's multipart.CreateFormFile default: a file part
		// without an explicit type is octet-stream, since the peer would otherwise get
		// no type information at all.
		contentType = "application/octet-stream"
	}
	header := make(textproto.MIMEHeader)
	disposition := fmt.Sprintf(`form-data; name="%s"`, escapeMultipartQuotes(field))
	if filename != "" {
		disposition += fmt.Sprintf(`; filename="%s"`, escapeMultipartQuotes(filename))
	}
	header.Set("Content-Disposition", disposition)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := mw.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("ghttp/client: create multipart part %q: %w", field, err)
	}
	return part, nil
}

// escapeMultipartQuotes 转义 Content-Disposition 中的引号与反斜杠，避免文件名里的引号
// 截断头部、注入额外的参数（与标准库 mime/multipart 的 escapeQuotes 同策略）。
// escapeMultipartQuotes escapes quotes and backslashes in Content-Disposition so a
// filename cannot terminate the header early or inject extra parameters (same policy as
// the standard library's mime/multipart escapeQuotes).
func escapeMultipartQuotes(s string) string {
	if !strings.ContainsAny(s, `"\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// baseName 取路径的最后一段（不依赖 filepath，避免为一行逻辑引入平台差异处理）。
// baseName returns the last path segment (without filepath, to avoid platform-specific
// handling for one line of logic).
func baseName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// multipartReplayable 报告 multipart 部件是否都能在重试时重放：路径型可以（重新打开），
// reader 型必须是 io.Seeker。
// multipartReplayable reports whether every multipart part can be replayed on a retry:
// path carriers can (reopened), while reader carriers must be io.Seeker.
func (r *Request) multipartReplayable() bool {
	for _, f := range r.multipartFields {
		if f.Path != "" {
			continue
		}
		if f.Reader == nil {
			continue
		}
		if _, ok := f.Reader.(io.Seeker); !ok {
			return false
		}
	}
	return true
}

// rewindMultipartReaders 把所有可 seek 的 multipart reader 归零。
// rewindMultipartReaders rewinds every seekable multipart reader.
func (r *Request) rewindMultipartReaders() error {
	for _, f := range r.multipartFields {
		if f.Path != "" || f.Reader == nil {
			continue
		}
		s, ok := f.Reader.(io.Seeker)
		if !ok {
			return ErrBodyNotReplayable
		}
		if _, err := s.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("ghttp/client: rewind multipart field %q: %w", f.Field, err)
		}
	}
	return nil
}
