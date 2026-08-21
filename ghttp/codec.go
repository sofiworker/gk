package ghttp

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
)

// RequestDecoder 是请求体解码器:请求期把请求体解码进 v(v 为 *T)。仅需实现一侧时
// 直接实现本接口即可,无需实现完整 Codec。ContentType 返回期望的请求 Content-Type,
// 供 415 校验与 OpenAPI 使用。
// RequestDecoder decodes the request body into v (v is a *T) at request time.
// Implement this alone when only decoding is customized; the full Codec is not
// required. ContentType returns the expected request Content-Type, used for the
// 415 check and OpenAPI.
type RequestDecoder interface {
	Decode(req *Request, v any) error
	ContentType() string
}

// ResponseEncoder 是响应体编码器:请求期把 v 写入响应。仅需实现一侧时直接实现本接口
// 即可。ContentType 返回写出的响应 Content-Type。
// ResponseEncoder encodes v into the response at request time. Implement this
// alone when only encoding is customized. ContentType returns the response
// Content-Type written out.
type ResponseEncoder interface {
	Encode(resp *Response, v any) error
	ContentType() string
}

// Codec 组合请求解码与响应编码,供需要同时替换两侧的场景使用;它不是入口的强制要求,
// 入口分别只接受 RequestDecoder 或 ResponseEncoder。符合小接口组合原则。
// Codec composes request decoding and response encoding for callers that replace
// both sides at once; it is not required by entries, which accept RequestDecoder
// or ResponseEncoder separately. Follows the small-interface composition rule.
type Codec interface {
	RequestDecoder
	ResponseEncoder
}

// jsonCodec 用标准库 encoding/json 实现 JSON 的解码与编码。解码流式(json.NewDecoder
// 直接读 req.Body,不 io.ReadAll),内存不随请求体大小线性膨胀。
// jsonCodec implements JSON decoding and encoding with the standard
// encoding/json. Decoding streams (json.NewDecoder reads req.Body directly,
// never io.ReadAll), so memory does not grow linearly with body size.
type jsonCodec struct{}

func (jsonCodec) ContentType() string { return "application/json" }

func (jsonCodec) Decode(req *Request, v any) error {
	if req.Body == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidInput)
	}
	if err := json.NewDecoder(req.Body).Decode(v); err != nil && err != io.EOF {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return nil
}

func (jsonCodec) Encode(resp *Response, v any) error {
	return json.NewEncoder(resp).Encode(v)
}

// JSONCodec 返回一个 JSON 编解码器,同时实现 RequestDecoder 与 ResponseEncoder(即
// 完整 Codec)。可整体传入,也可只取其一侧。
// JSONCodec returns a JSON codec implementing both RequestDecoder and
// ResponseEncoder (a full Codec). Pass it whole, or use just one side.
func JSONCodec() Codec { return jsonCodec{} }

// xmlCodec 用标准库 encoding/xml 实现 XML 的解码与编码,解码同样流式。
// xmlCodec implements XML decoding and encoding with the standard encoding/xml;
// decoding likewise streams.
type xmlCodec struct{}

func (xmlCodec) ContentType() string { return "application/xml" }

func (xmlCodec) Decode(req *Request, v any) error {
	if req.Body == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidInput)
	}
	if err := xml.NewDecoder(req.Body).Decode(v); err != nil && err != io.EOF {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return nil
}

func (xmlCodec) Encode(resp *Response, v any) error {
	return xml.NewEncoder(resp).Encode(v)
}

// XMLCodec 返回一个 XML 编解码器,同时实现 RequestDecoder 与 ResponseEncoder。
// XMLCodec returns an XML codec implementing both RequestDecoder and
// ResponseEncoder.
func XMLCodec() Codec { return xmlCodec{} }
