package ghttp

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"reflect"
	"strings"
)

type bytesOutput struct {
	contentType string
}

func (o bytesOutput) ContentType() string { return o.contentType }
func (bytesOutput) StatusCode() int       { return http.StatusOK }
func (bytesOutput) WriteBody(writer io.Writer, value []byte) error {
	_, err := writer.Write(value)
	return err
}
func (bytesOutput) responseSchema() any {
	return map[string]any{"type": "string", "format": "binary"}
}

// BytesOutput 创建原始字节输出契约。
// BytesOutput creates a raw byte output contract.
func BytesOutput(contentType string) Output[[]byte] {
	return bytesOutput{contentType: contentType}
}

type noContentOutput[T any] struct{}

func (noContentOutput[T]) ContentType() string          { return "" }
func (noContentOutput[T]) StatusCode() int              { return http.StatusNoContent }
func (noContentOutput[T]) WriteBody(io.Writer, T) error { return nil }
func (noContentOutput[T]) responseSchema() any          { return nil }

// NoContentOutput 创建 204 且无响应体的输出契约。
// NoContentOutput creates a 204 output contract with no response body.
func NoContentOutput[T any]() Output[T] { return noContentOutput[T]{} }

// RedirectResponse 是重定向输出的动态响应值。
// RedirectResponse is the dynamic response value for redirect outputs.
type RedirectResponse struct {
	Location string
}

type redirectOutput struct {
	status int
}

func (o redirectOutput) ContentType() string                       { return "" }
func (o redirectOutput) StatusCode() int                           { return o.status }
func (redirectOutput) WriteBody(io.Writer, RedirectResponse) error { return nil }
func (redirectOutput) responseSchema() any                         { return nil }
func (redirectOutput) responseHeaders() []responseHeader {
	return []responseHeader{{name: "Location", description: "Redirect target"}}
}
func (redirectOutput) writeValueHeaders(header http.Header, value RedirectResponse) error {
	if strings.TrimSpace(value.Location) == "" {
		return errors.New("redirect location is empty")
	}
	header.Set("Location", value.Location)
	return nil
}

// RedirectOutput 创建由 RedirectResponse 提供动态 Location 的重定向输出契约。
// RedirectOutput creates a redirect output whose dynamic Location comes from RedirectResponse.
func RedirectOutput(status int) Output[RedirectResponse] {
	return redirectOutput{status: status}
}

type operationCookieOutput[T any] struct {
	cookie *http.Cookie
	inner  Output[T]
}

func (o operationCookieOutput[T]) ContentType() string { return o.inner.ContentType() }
func (o operationCookieOutput[T]) StatusCode() int     { return o.inner.StatusCode() }
func (o operationCookieOutput[T]) WriteBody(writer io.Writer, value T) error {
	return o.inner.WriteBody(writer, value)
}
func (o operationCookieOutput[T]) prepare(value any) ([]byte, bool, error) {
	if preparer, ok := any(o.inner).(outputPreparer); ok {
		return preparer.prepare(value)
	}
	return nil, false, nil
}
func (o operationCookieOutput[T]) writePrepared(writer io.Writer, body []byte) error {
	if prepared, ok := any(o.inner).(preparedOutputWriter); ok {
		return prepared.writePrepared(writer, body)
	}
	return nil
}
func (o operationCookieOutput[T]) preflightValue(value T) error {
	if preflight, ok := any(o.inner).(outputValuePreflight[T]); ok {
		return preflight.preflightValue(value)
	}
	return nil
}
func (o operationCookieOutput[T]) preflightWriter(writer io.Writer) error {
	if preflight, ok := any(o.inner).(outputWriterPreflight); ok {
		return preflight.preflightWriter(writer)
	}
	return nil
}
func (o operationCookieOutput[T]) writeHeaders(header http.Header) {
	if headers, ok := any(o.inner).(outputHeaderWriter); ok {
		headers.writeHeaders(header)
	}
	if o.cookie != nil {
		header.Add("Set-Cookie", o.cookie.String())
	}
}
func (o operationCookieOutput[T]) writeValueHeaders(header http.Header, value T) error {
	if writer, ok := any(o.inner).(outputValueHeaderWriter[T]); ok {
		return writer.writeValueHeaders(header, value)
	}
	return nil
}
func (o operationCookieOutput[T]) responseHeaders() []responseHeader {
	var headers []responseHeader
	if provider, ok := any(o.inner).(outputHeaderMetadataProvider); ok {
		headers = append(headers, provider.responseHeaders()...)
	}
	if o.cookie != nil {
		headers = append(headers, responseHeader{name: "Set-Cookie", description: "Response cookie"})
	}
	return headers
}
func (o operationCookieOutput[T]) responseSchema() any {
	if provider, ok := any(o.inner).(outputSchemaProvider); ok {
		return provider.responseSchema()
	}
	return nil
}
func (o operationCookieOutput[T]) innerOutput() any { return o.inner }
func (o operationCookieOutput[T]) outputSetupError() error {
	return operationOutputSetupError(o.inner)
}

// WithResponseCookie 返回设置指定 Cookie 的输出契约。
// WithResponseCookie returns an output contract that sets the given cookie.
func WithResponseCookie[T any](cookie *http.Cookie, output Output[T]) Output[T] {
	var cloned *http.Cookie
	if cookie != nil {
		copy := *cookie
		cloned = &copy
	}
	return operationCookieOutput[T]{cookie: cloned, inner: output}
}

type xmlOutput[T any] struct{}

func (xmlOutput[T]) ContentType() string { return MIMEXML }
func (xmlOutput[T]) StatusCode() int     { return http.StatusOK }
func (xmlOutput[T]) WriteBody(writer io.Writer, value T) error {
	body, err := xml.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write(body)
	return err
}
func (xmlOutput[T]) prepare(value any) ([]byte, bool, error) {
	body, err := xml.Marshal(value)
	return body, true, err
}
func (xmlOutput[T]) writePrepared(writer io.Writer, body []byte) error {
	_, err := writer.Write(body)
	return err
}
func (xmlOutput[T]) responseSchema() any { return generateSchema(reflect.TypeFor[T]()) }

// XMLOutput 创建 XML 200 输出契约。
// XMLOutput creates an XML output contract with status 200.
func XMLOutput[T any]() Output[T] { return xmlOutput[T]{} }

type htmlOutput[T any] struct {
	template *template.Template
	name     string
}

func (o htmlOutput[T]) outputSetupError() error {
	if o.template == nil {
		return ErrOperationTemplateNil
	}
	return nil
}

func (htmlOutput[T]) ContentType() string { return "text/html; charset=utf-8" }
func (htmlOutput[T]) StatusCode() int     { return http.StatusOK }
func (o htmlOutput[T]) WriteBody(writer io.Writer, value T) error {
	return o.template.ExecuteTemplate(writer, o.name, value)
}
func (o htmlOutput[T]) prepare(value any) ([]byte, bool, error) {
	var buffer bytes.Buffer
	if err := o.template.ExecuteTemplate(&buffer, o.name, value); err != nil {
		return nil, false, err
	}
	return buffer.Bytes(), true, nil
}
func (htmlOutput[T]) writePrepared(writer io.Writer, body []byte) error {
	_, err := writer.Write(body)
	return err
}
func (o htmlOutput[T]) responseSchema() any { return map[string]any{"type": "string"} }

// HTMLOutput 创建基于 html/template 的 HTML 输出契约。
// HTMLOutput creates an HTML output contract backed by html/template.
func HTMLOutput[T any](source *template.Template, name string) Output[T] {
	return htmlOutput[T]{template: source, name: name}
}

type downloadOutput struct {
	filename string
}

func (downloadOutput) ContentType() string { return "application/octet-stream" }
func (downloadOutput) StatusCode() int     { return http.StatusOK }
func (downloadOutput) WriteBody(writer io.Writer, value []byte) error {
	_, err := writer.Write(value)
	return err
}
func (o downloadOutput) writeHeaders(header http.Header) {
	header.Set("Content-Disposition", `attachment; filename="`+o.filename+`"`)
}
func (o downloadOutput) responseHeaders() []responseHeader {
	return []responseHeader{{
		name: "Content-Disposition", value: `attachment; filename="` + o.filename + `"`,
		description: "Attachment filename",
	}}
}
func (downloadOutput) responseSchema() any {
	return map[string]any{"type": "string", "format": "binary"}
}

// DownloadOutput 创建带附件文件名的字节输出契约。
// DownloadOutput creates a byte output contract with an attachment filename.
func DownloadOutput(filename string) Output[[]byte] {
	return downloadOutput{filename: filename}
}

type streamOutput struct {
	contentType string
}

func (o streamOutput) ContentType() string { return o.contentType }
func (streamOutput) StatusCode() int       { return http.StatusOK }
func (streamOutput) WriteBody(writer io.Writer, stream func(io.Writer) error) error {
	if stream == nil {
		return nil
	}
	return stream(writer)
}
func (streamOutput) responseSchema() any { return map[string]any{"type": "string"} }

// StreamOutput 创建由 handler 主动写入的流式输出契约。
// StreamOutput creates a streaming output contract written by the handler.
func StreamOutput(contentType string) Output[func(io.Writer) error] {
	return streamOutput{contentType: contentType}
}

type sseOutput struct{}

func (sseOutput) ContentType() string { return "text/event-stream" }
func (sseOutput) StatusCode() int     { return http.StatusOK }
func (sseOutput) WriteBody(writer io.Writer, stream func(*SSEWriter) error) error {
	if stream == nil {
		return nil
	}
	responseWriter, ok := writer.(http.ResponseWriter)
	if !ok {
		return fmt.Errorf("sse output requires http.ResponseWriter")
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("sse output requires http.Flusher")
	}
	return stream(&SSEWriter{w: responseWriter, flusher: flusher})
}
func (sseOutput) preflightWriter(writer io.Writer) error {
	if _, ok := writer.(http.ResponseWriter); !ok {
		return fmt.Errorf("sse output requires http.ResponseWriter")
	}
	if _, ok := writer.(http.Flusher); !ok {
		return fmt.Errorf("sse output requires http.Flusher")
	}
	return nil
}
func (sseOutput) writeHeaders(header http.Header) {
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
}
func (sseOutput) responseHeaders() []responseHeader {
	return []responseHeader{
		{name: "Cache-Control", value: "no-cache"},
		{name: "Connection", value: "keep-alive"},
	}
}
func (sseOutput) responseSchema() any { return map[string]any{"type": "string"} }

// SSEOutput 创建复用 ghttp SSEWriter 的事件流输出契约。
// SSEOutput creates an event-stream output contract backed by ghttp SSEWriter.
func SSEOutput() Output[func(*SSEWriter) error] { return sseOutput{} }

type fileOutput struct {
	contentType string
}

func (o fileOutput) ContentType() string { return o.contentType }
func (fileOutput) StatusCode() int       { return http.StatusOK }
func (fileOutput) WriteBody(writer io.Writer, source io.ReadSeeker) error {
	if source == nil {
		return nil
	}
	_, err := io.Copy(writer, source)
	return err
}
func (fileOutput) preflightValue(source io.ReadSeeker) error {
	if source == nil {
		return nil
	}
	_, err := source.Seek(0, io.SeekStart)
	return err
}
func (fileOutput) responseSchema() any {
	return map[string]any{"type": "string", "format": "binary"}
}

// FileOutput 创建 io.ReadSeeker 文件流输出契约。
// FileOutput creates an io.ReadSeeker file-stream output contract.
func FileOutput(contentType string) Output[io.ReadSeeker] {
	return fileOutput{contentType: contentType}
}

type functionOutput[T any] struct {
	status      int
	contentType string
	write       func(io.Writer, T) error
}

func (o functionOutput[T]) outputSetupError() error {
	if o.write == nil {
		return ErrOperationOutputWriterNil
	}
	return nil
}

func (o functionOutput[T]) ContentType() string { return o.contentType }
func (o functionOutput[T]) StatusCode() int     { return o.status }
func (o functionOutput[T]) WriteBody(writer io.Writer, value T) error {
	if o.write == nil {
		return nil
	}
	return o.write(writer, value)
}

// OutputFunc 将显式写入函数适配为输出契约。
// OutputFunc adapts an explicit write function into an output contract.
func OutputFunc[T any](status int, contentType string, write func(io.Writer, T) error) Output[T] {
	return functionOutput[T]{status: status, contentType: contentType, write: write}
}
