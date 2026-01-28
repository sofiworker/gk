package gserver

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// MarshalFunc：自定义marshal函数类型
type MarshalFunc func(data interface{}) ([]byte, string, error)

// ==================== AutoResult ====================

// AutoResult：自动marshal返回值
// 根据Accept header自动选择编码格式（JSON/XML等）
type AutoResult struct {
	data    interface{}
	code    int
	headers map[string]string
	marshal MarshalFunc // 可选的自定义marshal函数
}

// NewAutoResult：创建AutoResult
func NewAutoResult(data interface{}) *AutoResult {
	return &AutoResult{
		data:    data,
		code:    http.StatusOK,
		headers: make(map[string]string),
	}
}

// Execute：实现Result接口
func (r *AutoResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}

	// 设置状态码
	code := r.code
	if code == 0 {
		code = http.StatusOK
	}

	// 设置自定义headers
	for k, v := range r.headers {
		ctx.Header(k, v)
	}

	// 执行marshal
	if r.marshal != nil {
		// 使用自定义marshal函数
		body, contentType, err := r.marshal(r.data)
		if err != nil {
			panic(err)
		}
		ctx.Header("Content-Type", contentType)
		ctx.Status(code)
		ctx.Writer.Write(body)
		return
	}

	// 自动marshal：根据Accept header选择编码
	r.autoMarshal(ctx, code)
}

// autoMarshal：根据Accept header自动选择编码格式
func (r *AutoResult) autoMarshal(ctx *Context, code int) {
	accept := ctx.GetHeader("Accept")
	if accept == "" {
		accept = "*/*"
	}

	// 根据Accept header的优先级选择编码
	// 支持的格式：application/json, application/xml, text/plain, application/octet-stream
	switch {
	case contains(accept, "application/json") || accept == "*/*":
		ctx.JSON(code, r.data)
	case contains(accept, "application/xml") || contains(accept, "text/xml"):
		ctx.XML(code, r.data)
	case contains(accept, "text/plain"):
		ctx.String(code, "%v", r.data)
	case contains(accept, "application/octet-stream"):
		// 对于二进制数据，需要调用者提供[]byte或io.Reader
		if data, ok := r.data.([]byte); ok {
			ctx.Data(code, "application/octet-stream", data)
		} else if reader, ok := r.data.(io.Reader); ok {
			ctx.Header("Content-Type", "application/octet-stream")
			ctx.Status(code)
			_, _ = io.Copy(ctx.Writer, reader)
		} else {
			panic("AutoResult: binary data must be []byte or io.Reader")
		}
	default:
		// 默认JSON
		ctx.JSON(code, r.data)
	}
}

// WithCode：设置状态码（链式调用）
func (r *AutoResult) WithCode(code int) *AutoResult {
	r.code = code
	return r
}

// WithHeader：设置header（链式调用）
func (r *AutoResult) WithHeader(key, value string) *AutoResult {
	if r.headers == nil {
		r.headers = make(map[string]string)
	}
	r.headers[key] = value
	return r
}

// WithHeaders：设置多个headers（链式调用）
func (r *AutoResult) WithHeaders(headers map[string]string) *AutoResult {
	if r.headers == nil {
		r.headers = make(map[string]string)
	}
	for k, v := range headers {
		r.headers[k] = v
	}
	return r
}

// WithMarshal：设置自定义marshal函数（链式调用）
func (r *AutoResult) WithMarshal(marshal MarshalFunc) *AutoResult {
	r.marshal = marshal
	return r
}

// contains：辅助函数，检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// Auto：自动marshal返回值（默认状态码200）
func Auto(data interface{}) Result {
	return NewAutoResult(data)
}

// AutoCode：自动marshal返回值（指定状态码）
func AutoCode(data interface{}, code int) Result {
	return NewAutoResult(data).WithCode(code)
}

// AutoWithHeaders：自动marshal返回值（指定headers）
func AutoWithHeaders(data interface{}, headers map[string]string) Result {
	return NewAutoResult(data).WithHeaders(headers)
}

// AutoCustom：自动marshal返回值（自定义marshal函数）
func AutoCustom(data interface{}, marshal MarshalFunc) Result {
	return NewAutoResult(data).WithMarshal(marshal)
}

// ==================== JSON Result ====================

// JSONResult：固定使用JSON编码
type JSONResult struct {
	Data interface{}
	Code int
}

func (r *JSONResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}
	ctx.JSON(code, r.Data)
}

func JSON(data interface{}) Result {
	return &JSONResult{Data: data, Code: http.StatusOK}
}

func JSONCode(data interface{}, code int) Result {
	return &JSONResult{Data: data, Code: code}
}

// ==================== XML Result ====================

// XMLResult：固定使用XML编码
type XMLResult struct {
	Data interface{}
	Code int
}

func (r *XMLResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}
	ctx.XML(code, r.Data)
}

func XML(data interface{}) Result {
	return &XMLResult{Data: data, Code: http.StatusOK}
}

func XMLCode(data interface{}, code int) Result {
	return &XMLResult{Data: data, Code: code}
}

// ==================== HTML Result ====================

// HTMLResult：渲染HTML模板
type HTMLResult struct {
	Template string
	Data     interface{}
	Code     int
}

func (r *HTMLResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}
	ctx.HTML(code, r.Template, r.Data)
}

func HTML(template string, data interface{}) Result {
	return &HTMLResult{Template: template, Data: data, Code: http.StatusOK}
}

func HTMLCode(template string, data interface{}, code int) Result {
	return &HTMLResult{Template: template, Data: data, Code: code}
}

// ==================== String Result ====================

// StringResult：返回纯文本
type StringResult struct {
	Format string
	Data   []interface{}
	Code   int
}

func (r *StringResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}

	ctx.Header("Content-Type", "text/plain; charset=utf-8")
	ctx.Status(code)

	if len(r.Data) > 0 {
		_, _ = ctx.Writer.WriteString(fmt.Sprintf(r.Format, r.Data...))
	} else {
		_, _ = ctx.Writer.WriteString(r.Format)
	}
}

func String(format string, data ...interface{}) Result {
	return &StringResult{Format: format, Data: data, Code: http.StatusOK}
}

func StringCode(format string, code int, data ...interface{}) Result {
	return &StringResult{Format: format, Data: data, Code: code}
}

// ==================== Data Result ====================

// DataResult：返回二进制数据
type DataResult struct {
	Data        []byte
	ContentType string
	Code        int
}

func (r *DataResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}
	ctx.Data(code, r.ContentType, r.Data)
}

func Data(contentType string, data []byte) Result {
	return &DataResult{Data: data, ContentType: contentType, Code: http.StatusOK}
}

func DataCode(contentType string, data []byte, code int) Result {
	return &DataResult{Data: data, ContentType: contentType, Code: code}
}

// ==================== Redirect Result ====================

// RedirectResult：重定向
type RedirectResult struct {
	URL  string
	Code int
}

func (r *RedirectResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusFound
	}
	ctx.Writer.Header().Set("Location", r.URL)
	ctx.Status(code)
}

func Redirect(url string) Result {
	return &RedirectResult{URL: url, Code: http.StatusFound}
}

func RedirectCode(url string, code int) Result {
	return &RedirectResult{URL: url, Code: code}
}

// ==================== Error Result ====================

// ErrorResult：返回错误
type ErrorResult struct {
	Err  error
	Code int
	Msg  string
}

func (r *ErrorResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusInternalServerError
	}
	msg := r.Msg
	if msg == "" && r.Err != nil {
		msg = r.Err.Error()
	}
	if msg == "" {
		msg = http.StatusText(code)
	}
	ctx.String(code, "%s", msg)
}

func Error(err error) Result {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &ErrorResult{Err: err, Code: http.StatusInternalServerError, Msg: msg}
}

func ErrorMsg(msg string) Result {
	return &ErrorResult{Msg: msg, Code: http.StatusInternalServerError}
}

func ErrorCode(err error, code int) Result {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &ErrorResult{Err: err, Code: code, Msg: msg}
}

func ErrorStatusCode(code int, msg string) Result {
	return &ErrorResult{Code: code, Msg: msg}
}

// ==================== NoContent Result ====================

// NoContentResult：返回204 No Content
type NoContentResult struct{}

func (r *NoContentResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	if ctx.Writer != nil && !ctx.Writer.Written() {
		ctx.Status(http.StatusNoContent)
	}
}

func NoContent() Result {
	return &NoContentResult{}
}

// ==================== Stream Result ====================

// StreamResult：流式响应
type StreamResult struct {
	Reader      io.Reader
	ContentType string
	Code        int
}

func (r *StreamResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}
	if r.ContentType == "" {
		r.ContentType = "application/octet-stream"
	}
	ctx.Header("Content-Type", r.ContentType)
	ctx.Status(code)
	_, _ = io.Copy(ctx.Writer, r.Reader)
}

func Stream(reader io.Reader) Result {
	return &StreamResult{Reader: reader, Code: http.StatusOK}
}

func StreamWithContentType(reader io.Reader, contentType string) Result {
	return &StreamResult{Reader: reader, ContentType: contentType, Code: http.StatusOK}
}

// ==================== File Result ====================

// FileResult：返回文件
type FileResult struct {
	Path string
}

func (r *FileResult) Execute(ctx *Context) {
	if ctx == nil {
		return
	}

	// 检查文件是否存在
	_, err := os.Stat(r.Path)
	if err != nil {
		if os.IsNotExist(err) {
			ctx.Status(http.StatusNotFound)
		} else {
			ctx.Status(http.StatusInternalServerError)
		}
		return
	}

	// 根据扩展名设置Content-Type
	ext := filepath.Ext(r.Path)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	ctx.Header("Content-Type", contentType)
	ctx.Status(http.StatusOK)

	// 读取文件并写入响应
	file, err := os.ReadFile(r.Path)
	if err != nil {
		ctx.Status(http.StatusInternalServerError)
		return
	}

	ctx.Writer.Write(file)
}

func File(filepath string) Result {
	return &FileResult{Path: filepath}
}

// ==================== 兼容旧API（已废弃） ====================

// 已废弃的Result类型，保持向后兼容
type JsonResult = JSONResult
type HtmlResult = HTMLResult
type EmptyResult = NoContentResult
