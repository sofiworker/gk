package wire

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrInvalidFormat 标记"字节流无法按预期格式解出目标值"：语法错误、截断、尾随内容、
// 未知字段（启用 DisallowUnknownFields 时）、空体（启用 RequireBody 时）。
//
// 它不覆盖"体量超限"：那由 *http.MaxBytesError 表达并保留在错误链中，调用方可用
// errors.As 精确分类（server 侧映射 413）。两者必须可区分，否则 413 会退化成 400。
// ErrInvalidFormat marks "the byte stream cannot be decoded into the target value":
// syntax errors, truncation, trailing content, unknown fields (with
// DisallowUnknownFields) and empty bodies (with RequireBody).
//
// It does NOT cover size overrun: that is expressed by *http.MaxBytesError, kept in the
// error chain so callers can classify it with errors.As (413 on the server). The two
// must stay distinguishable, otherwise 413 degrades into 400.
var ErrInvalidFormat = errors.New("wire: invalid format")

// decodeConfig 是解码选项的集合。
// decodeConfig collects decode options.
type decodeConfig struct {
	// disallowUnknown 拒绝目标结构体中不存在的 JSON 字段
	// disallowUnknown rejects JSON fields absent from the target struct
	disallowUnknown bool

	// requireBody 把空体视为格式错误
	// requireBody treats an empty body as a format error
	requireBody bool
}

// DecodeOption 配置 DecodeJSON / DecodeXML 的严格程度。
// DecodeOption configures the strictness of DecodeJSON / DecodeXML.
type DecodeOption func(*decodeConfig)

// DisallowUnknownFields 拒绝目标结构体中不存在的字段（仅 JSON 生效）。
// server 端解析请求体时通常开启；client 端解析响应体时通常不开，
// 以免服务端新增字段导致客户端解码失败。
// DisallowUnknownFields rejects fields absent from the target struct (JSON only).
// Servers usually enable it for request bodies; clients usually do not for response
// bodies, so a server adding a field does not break client decoding.
func DisallowUnknownFields() DecodeOption {
	return func(c *decodeConfig) { c.disallowUnknown = true }
}

// RequireBody 把空体视为格式错误，而不是让目标保持零值。
// RequireBody treats an empty body as a format error instead of leaving the target zero.
func RequireBody() DecodeOption {
	return func(c *decodeConfig) { c.requireBody = true }
}

// newDecodeConfig 应用选项。
// newDecodeConfig applies options.
func newDecodeConfig(opts []DecodeOption) decodeConfig {
	var c decodeConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	return c
}

// emptyBody 处理"读到 EOF 而一个值都没有"的情况。contentLength 区分两种 EOF：
// 声明了正长度 = 传输被截断（报错）；长度未知或为 0 = 空体（RequireBody 时报错，否则放行）。
// emptyBody handles "hit EOF before any value". contentLength distinguishes two EOFs:
// a declared positive length means truncation (error); an unknown or zero length is an
// empty body (an error with RequireBody, otherwise accepted).
func emptyBody(cfg decodeConfig, contentLength int64, kind string) error {
	if contentLength > 0 {
		return fmt.Errorf("%w: body of %d bytes ended before any %s value", ErrInvalidFormat, contentLength, kind)
	}
	if cfg.requireBody {
		return fmt.Errorf("%w: empty body", ErrInvalidFormat)
	}
	return nil
}

// DecodeJSON 把 r 严格解码进 v：只接受恰好一个 JSON 值，拒绝其后的任何非空白内容。
//
// 返回的错误要么包装 ErrInvalidFormat（同时保留 *json.SyntaxError 等底层错误），要么在链中
// 保留 *http.MaxBytesError；调用方用 errors.Is / errors.As 分类，不应解析错误文本。
// DecodeJSON strictly decodes r into v: exactly one JSON value is accepted and any
// non-whitespace content after it is rejected.
//
// A returned error either wraps ErrInvalidFormat (still exposing *json.SyntaxError and
// friends) or keeps *http.MaxBytesError in its chain; callers classify with
// errors.Is / errors.As rather than parsing error text.
func DecodeJSON(r io.Reader, v any, contentLength int64, opts ...DecodeOption) error {
	cfg := newDecodeConfig(opts)
	if r == nil {
		return emptyBody(cfg, contentLength, "JSON")
	}
	dec := json.NewDecoder(r)
	if cfg.disallowUnknown {
		dec.DisallowUnknownFields()
	}
	// 用 errors.Is 比较 io.EOF：中间层（自定义 Reader 等）可能包装 EOF，== 会漏判。
	// Compare io.EOF with errors.Is: intermediate readers may wrap EOF and == would miss it.
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return emptyBody(cfg, contentLength, "JSON")
		}
		return wrapFormat(err)
	}
	// 拒绝首个值之后的内容：`{"a":1}{"a":2}` 若被静默接受，链路上另一个宽松解析器可能取到
	// 第二个值，造成两端理解不一致（走私类混淆）。用 Token() 探测而非 Decode(&extra)：
	// 前者只读一个令牌，后者会把巨型第二值完整物化后才报错；More() 对顶层尾随的 `]`/`}`
	// 返回 false，不能使用。
	// Reject content after the first value: silently accepting `{"a":1}{"a":2}` lets
	// another lenient parser take the second value (a smuggling-class confusion). Probe
	// with Token() rather than Decode(&extra): the former reads one token, the latter
	// fully materializes a huge second value before failing; More() returns false for a
	// trailing top-level `]`/`}` and cannot be used.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if isMaxBytes(err) {
			return err
		}
		return fmt.Errorf("%w: unexpected trailing content after the JSON value", ErrInvalidFormat)
	}
	return nil
}

// DecodeXML 是 DecodeJSON 的 XML 对应物，严格性策略一致；DisallowUnknownFields 对 XML 无效。
// DecodeXML is the XML counterpart of DecodeJSON with identical strictness;
// DisallowUnknownFields has no effect on XML.
func DecodeXML(r io.Reader, v any, contentLength int64, opts ...DecodeOption) error {
	cfg := newDecodeConfig(opts)
	if r == nil {
		return emptyBody(cfg, contentLength, "XML")
	}
	dec := xml.NewDecoder(r)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return emptyBody(cfg, contentLength, "XML")
		}
		return wrapFormat(err)
	}
	// XML 没有 More()，需手动推进令牌：仅纯空白 CharData 可跳过（元素间的换行缩进合法），
	// 其他内容一律视为尾随垃圾。
	// XML has no More(), so advance tokens manually: only whitespace-only CharData is
	// skipped (indentation between elements is legal); anything else is trailing garbage.
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if isMaxBytes(err) {
				return err
			}
			return wrapFormat(err)
		}
		if cd, ok := tok.(xml.CharData); ok && len(bytes.TrimSpace(cd)) == 0 {
			continue
		}
		return fmt.Errorf("%w: unexpected trailing content after the XML document", ErrInvalidFormat)
	}
}

// wrapFormat 给底层错误加上 ErrInvalidFormat 标记并用 %w 保留原错误；体量超限错误原样返回，
// 不归入格式错误。
// wrapFormat tags a low-level error with ErrInvalidFormat while keeping it via %w; size
// overrun errors are returned as-is and never classified as format errors.
func wrapFormat(err error) error {
	if isMaxBytes(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrInvalidFormat, err)
}

// isMaxBytes 报告 err 链中是否含 *http.MaxBytesError（请求体超出 http.MaxBytesReader 限额）。
// isMaxBytes reports whether err's chain holds *http.MaxBytesError (body over the
// http.MaxBytesReader limit).
func isMaxBytes(err error) bool {
	var mbe *http.MaxBytesError
	return err != nil && errors.As(err, &mbe)
}
