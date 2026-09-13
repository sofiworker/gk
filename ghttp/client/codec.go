package client

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"

	internalcodec "github.com/sofiworker/gk/ghttp/internal/codec"
)

// 本文件定义请求体编码与响应体解码的扩展点，以及内置的 JSON/XML/Text/Form 实现。
// This file defines the extension points for encoding request bodies and decoding
// response bodies, plus the built-in JSON/XML/Text/Form implementations.

// Encoder 把 Go 值编码进 w。ContentType 返回写出的 Content-Type，用于设置请求头与
// 选择响应解码器。
// Encoder encodes a Go value into w. ContentType returns the Content-Type written,
// used for the request header and for selecting a response decoder.
type Encoder interface {
	Encode(w io.Writer, v any) error
	ContentType() string
}

// Decoder 把 r 解码进 v。contentLength 供实现区分"合法空体"与"声明了长度却截断"
// （-1 或 0 表示长度未知）；实现应把它透传给 ghttp/internal/codec 的严格解码内核，
// 以保持与 server 侧一致的严格性。
// Decoder decodes r into v. contentLength lets an implementation distinguish a
// legitimate empty body from truncation after a declared length (-1 or 0 means
// unknown); implementations should pass it through to the strict decoding kernel in
// ghttp/internal/codec so strictness matches the server side.
type Decoder interface {
	Decode(r io.Reader, v any, contentLength int64) error
	ContentType() string
}

// Codec 同时具备编码与解码能力。
// Codec provides both encoding and decoding.
type Codec interface {
	Encoder
	Decoder
}

// ——— JSON ——— //

type jsonCodec struct{}

// JSONCodec 返回内置的 JSON 编解码器。
// JSONCodec returns the built-in JSON codec.
func JSONCodec() Codec { return jsonCodec{} }

func (jsonCodec) ContentType() string { return internalcodec.ContentTypeJSON }

func (jsonCodec) Encode(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

func (jsonCodec) Decode(r io.Reader, v any, contentLength int64) error {
	return internalcodec.DecodeJSON(r, v, contentLength)
}

// ——— XML ——— //

type xmlCodec struct{}

// XMLCodec 返回内置的 XML 编解码器。
// XMLCodec returns the built-in XML codec.
func XMLCodec() Codec { return xmlCodec{} }

func (xmlCodec) ContentType() string { return internalcodec.ContentTypeXML }

func (xmlCodec) Encode(w io.Writer, v any) error {
	return xml.NewEncoder(w).Encode(v)
}

func (xmlCodec) Decode(r io.Reader, v any, contentLength int64) error {
	return internalcodec.DecodeXML(r, v, contentLength)
}

// ——— Text ——— //

type textCodec struct{}

// TextCodec 返回内置的 text/plain 编解码器：编码接受 string、[]byte、fmt.Stringer
// 与实现 encoding.TextMarshaler 的值；解码接受 *string、*[]byte 与 *any。
// TextCodec returns the built-in text/plain codec: encoding accepts string, []byte,
// fmt.Stringer and encoding.TextMarshaler values; decoding accepts *string, *[]byte
// and *any.
func TextCodec() Codec { return textCodec{} }

func (textCodec) ContentType() string { return internalcodec.ContentTypeText }

func (textCodec) Encode(w io.Writer, v any) error {
	switch s := v.(type) {
	case nil:
		return nil
	case string:
		_, err := io.WriteString(w, s)
		return err
	case []byte:
		_, err := w.Write(s)
		return err
	case fmt.Stringer:
		_, err := io.WriteString(w, s.String())
		return err
	default:
		_, err := fmt.Fprint(w, v)
		return err
	}
}

func (textCodec) Decode(r io.Reader, v any, _ int64) error {
	// 调用方（Response）已在读取阶段施加内存上限，此处可以整体读出。
	// The caller (Response) already capped memory at the read stage, so reading it all
	// here is safe.
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	switch dst := v.(type) {
	case *string:
		*dst = string(data)
	case *[]byte:
		*dst = data
	case *any:
		*dst = string(data)
	default:
		return fmt.Errorf("%w: text codec needs *string, *[]byte or *any, got %T", ErrDecode, v)
	}
	return nil
}

// ——— Form ——— //

type formCodec struct{}

// FormCodec 返回内置的 application/x-www-form-urlencoded 编解码器：编码接受
// url.Values、map[string][]string 与 map[string]string；解码写入 *url.Values 或
// *map[string][]string。
// FormCodec returns the built-in application/x-www-form-urlencoded codec: encoding
// accepts url.Values, map[string][]string and map[string]string; decoding writes into
// *url.Values or *map[string][]string.
func FormCodec() Codec { return formCodec{} }

func (formCodec) ContentType() string { return internalcodec.ContentTypeForm }

func (formCodec) Encode(w io.Writer, v any) error {
	values, err := formValues(v)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, values.Encode())
	return err
}

func (formCodec) Decode(r io.Reader, v any, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDecode, err)
	}
	switch dst := v.(type) {
	case *url.Values:
		*dst = values
	case *map[string][]string:
		*dst = map[string][]string(values)
	default:
		return fmt.Errorf("%w: form codec needs *url.Values or *map[string][]string, got %T", ErrDecode, v)
	}
	return nil
}

// formValues 把几种常见的表单载体归一化为 url.Values。
// formValues normalizes the common form carriers into url.Values.
func formValues(v any) (url.Values, error) {
	switch src := v.(type) {
	case url.Values:
		return src, nil
	case map[string][]string:
		return url.Values(src), nil
	case map[string]string:
		out := make(url.Values, len(src))
		for k, val := range src {
			out.Set(k, val)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: form codec needs url.Values or a string map, got %T", ErrNoCodec, v)
	}
}

// defaultCodecs 返回内置编解码器注册表（键为规范化后的 media-type）。
// defaultCodecs returns the built-in codec registry, keyed by normalized media-type.
func defaultCodecs() map[string]Codec {
	return map[string]Codec{
		internalcodec.ContentTypeJSON: JSONCodec(),
		internalcodec.ContentTypeXML:  XMLCodec(),
		internalcodec.ContentTypeText: TextCodec(),
		internalcodec.ContentTypeForm: FormCodec(),
	}
}

// lookupCodec 按响应的 Content-Type 查找解码器；未注册时返回 nil 与 false。
// lookupCodec finds a decoder for the response Content-Type; nil and false when none
// is registered.
func lookupCodec(registry map[string]Codec, contentType string) (Codec, bool) {
	if contentType == "" {
		return nil, false
	}
	ct := internalcodec.MediaType(contentType)
	if ct == "" {
		return nil, false
	}
	c, ok := registry[ct]
	return c, ok
}

// registryKey 归一化注册键，避免 "application/json" 与 "Application/JSON; charset=utf-8"
// 被当成两个条目。
// registryKey normalizes a registration key so "application/json" and
// "Application/JSON; charset=utf-8" are not treated as two entries.
func registryKey(contentType string) string {
	return strings.ToLower(internalcodec.MediaType(contentType))
}
