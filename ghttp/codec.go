package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"sync"

	"github.com/sofiworker/gk/ghttp/wire"
)

// Input 把请求体解码为 T。通过 WithInput 为路由指定；未指定时使用 JSONInput。
// Input decodes the request body into T. Set it per route with WithInput; JSONInput is
// the default.
type Input[T BodyConstraint] interface {
	Decode(ctx context.Context, req *Request) (T, error)
}

// Output 把 handler 返回值写入响应。通过 WithOutput 为路由指定；未指定时使用 JSON 输出，
// 并识别 Reply、FileReply 等响应类型。
// Output writes a handler's result to the response. Set it per route with WithOutput;
// by default results are written as JSON and Reply, FileReply, etc. are honored.
type Output[O ResponseConstraint] interface {
	Encode(ctx context.Context, resp *Response, v O) error
}

// InputFunc 是函数形式的 Input。
// InputFunc is the function form of Input.
type InputFunc[T BodyConstraint] func(ctx context.Context, req *Request) (T, error)

// Decode 实现 Input。
// Decode implements Input.
func (f InputFunc[T]) Decode(ctx context.Context, req *Request) (T, error) { return f(ctx, req) }

// OutputFunc 是函数形式的 Output。
// OutputFunc is the function form of Output.
type OutputFunc[O ResponseConstraint] func(ctx context.Context, resp *Response, v O) error

// Encode 实现 Output。
// Encode implements Output.
func (f OutputFunc[O]) Encode(ctx context.Context, resp *Response, v O) error {
	return f(ctx, resp, v)
}

// JSONInput 返回严格 JSON 输入：媒体类型须为 application/json（缺省放行），拒绝空体、未知字段
// 与尾随内容。错误映射同 ReadJSON。
// JSONInput returns a strict JSON input: the media type must be application/json (a
// missing header passes); empty bodies, unknown fields and trailing content are rejected.
// Errors map like ReadJSON.
func JSONInput[T BodyConstraint]() Input[T] {
	return InputFunc[T](func(_ context.Context, req *Request) (T, error) {
		var v T
		err := ReadJSON(req, &v)
		return v, err
	})
}

// xmlMediaTypes 是 XMLInput 接受的 media-type 集合。
// xmlMediaTypes is the media-type set accepted by XMLInput.
var xmlMediaTypes = []string{wire.ContentTypeXML, "text/xml"}

// XMLInput 返回严格 XML 输入：媒体类型须为 application/xml 或 text/xml（缺省放行），拒绝空体
// 与尾随内容。415/413/400 的映射与 JSONInput 一致。
// XMLInput returns a strict XML input: the media type must be application/xml or text/xml
// (a missing header passes); empty bodies and trailing content are rejected. The
// 415/413/400 mapping matches JSONInput.
func XMLInput[T BodyConstraint]() Input[T] {
	return InputFunc[T](func(_ context.Context, req *Request) (T, error) {
		var v T
		ct := req.Raw.Header.Get("Content-Type")
		if !wire.ContentTypeIn(ct, xmlMediaTypes) {
			return v, unsupportedMediaType(wire.ContentTypeXML, ct)
		}
		err := wire.DecodeXML(req.Raw.Body, &v, req.Raw.ContentLength, wire.RequireBody())
		return v, bodyDecodeError(err)
	})
}

// JSONOutput 返回 JSON 输出：200，Content-Type 为 application/json; charset=utf-8。
// 序列化先于写出，失败时不会留下半个响应。
// JSONOutput returns a JSON output: 200 with application/json; charset=utf-8. Encoding
// happens before anything is written, so a failure never leaves a half-written response.
func JSONOutput[O ResponseConstraint]() Output[O] {
	return OutputFunc[O](func(_ context.Context, resp *Response, v O) error {
		return writeJSON(resp, http.StatusOK, v)
	})
}

// XMLOutput 返回 XML 输出：200，Content-Type 为 application/xml; charset=utf-8。
// XMLOutput returns an XML output: 200 with application/xml; charset=utf-8.
func XMLOutput[O ResponseConstraint]() Output[O] {
	return OutputFunc[O](func(_ context.Context, resp *Response, v O) error {
		data, err := xml.Marshal(v)
		if err != nil {
			return fmt.Errorf("ghttp: encode XML response: %w", err)
		}
		return writeBody(resp, http.StatusOK, "application/xml; charset=utf-8", data)
	})
}

// TextOutput 返回纯文本输出：200，Content-Type 为 text/plain; charset=utf-8。
// TextOutput returns a plain-text output: 200 with text/plain; charset=utf-8.
func TextOutput() Output[string] {
	return OutputFunc[string](func(_ context.Context, resp *Response, v string) error {
		return writeBody(resp, http.StatusOK, "text/plain; charset=utf-8", []byte(v))
	})
}

// writeJSON 序列化后再写出 JSON 响应。
// writeJSON encodes first and then writes a JSON response.
func writeJSON(resp *Response, status int, data any) error {
	buf := jsonOutputBuffers.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		// Do not retain unusually large responses in the pool.
		if buf.Cap() <= 64<<10 {
			buf.Reset()
			jsonOutputBuffers.Put(buf)
		}
	}()
	if err := json.NewEncoder(buf).Encode(data); err != nil {
		return fmt.Errorf("ghttp: encode JSON response: %w", err)
	}
	body := buf.Bytes()
	// Encoder adds one newline; Marshal (the previous implementation) does not.
	return writeBody(resp, status, "application/json; charset=utf-8", body[:len(body)-1])
}

var jsonOutputBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// writeBody 设置 Content-Type、写状态码与响应体。204/304 等不允许带体的状态只写状态码。
// writeBody sets Content-Type and writes the status and body. Statuses that forbid a body
// (204, 304, 1xx) only write the status.
func writeBody(resp *Response, status int, contentType string, body []byte) error {
	if !bodyAllowed(status) {
		resp.WriteHeader(status)
		return nil
	}
	resp.Header().Set("Content-Type", contentType)
	resp.WriteHeader(status)
	_, err := resp.Write(body)
	return err
}

// bodyAllowed 报告该状态码是否允许响应体。
// bodyAllowed reports whether the status permits a response body.
func bodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}
