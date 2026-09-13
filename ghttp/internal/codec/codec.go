// Package codec 提供 HTTP 报文体的严格解码内核与 Content-Type 规范化，供
// ghttp(server)与 ghttp/client 共用。
// Package codec provides the strict message-body decoding kernel and Content-Type
// normalization shared by ghttp (server) and ghttp/client.
//
// 方向说明：本包只做"字节 → Go 值"（解码）。server 用它解析请求体，client 用它解析响应体；
// 两者对"严格"的要求一致——拒绝首个值之后的尾随内容、把"声明了正长度却读到 EOF"判为截断
// 而非空体。反方向（Go 值 → 字节）只有一行 json/xml Encoder，不值得共享，留在各包。
// Direction: this package only decodes bytes into Go values. The server uses it for
// request bodies, the client for response bodies, and both need the same strictness —
// rejecting trailing content after the first value, and treating "declared a positive
// length but hit EOF" as truncation rather than an empty body. The other direction
// (Go value to bytes) is a one-line json/xml Encoder and is not worth sharing, so it
// stays in each package.
package codec

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// 规范化的 media-type 常量。两端共用同一份字面量，避免 415 判定与解码分派各写一份而漂移。
// Canonical media-type constants. Both sides share one set of literals so the 415
// decision and decoder dispatch cannot drift apart.
const (
	ContentTypeJSON      = "application/json"
	ContentTypeXML       = "application/xml"
	ContentTypeForm      = "application/x-www-form-urlencoded"
	ContentTypeMultipart = "multipart/form-data"
	ContentTypeText      = "text/plain"
)

// ErrInvalidFormat 标记"字节流无法按预期格式解出目标值"：语法错误、截断、尾随内容、
// 空体等。调用方据此把它归类为自己的"输入非法"错误（server 侧映射 400，client 侧映射
// 响应解码失败）。
//
// 它不覆盖"体量超限"：那由 *http.MaxBytesError 表达，并保持在错误链里，使调用方能以
// errors.As 精确分类（server 侧映射 413）。两者必须可区分——把超限也算作"格式非法"会让
// 413 退化成 400，调用方无法据状态码判断该收紧限额还是修正报文。
// ErrInvalidFormat marks "the byte stream cannot be decoded into the target value":
// syntax errors, truncation, trailing content, empty body. Callers classify it as
// their own invalid-input error (400 on the server, a response-decode failure on the
// client).
//
// It deliberately does NOT cover size overrun: that is expressed by
// *http.MaxBytesError, which stays in the error chain so callers can classify it
// precisely with errors.As (413 on the server). The two must remain distinguishable —
// lumping overrun into "invalid format" would degrade 413 into 400, leaving callers
// unable to tell whether to tighten a limit or fix the payload.
var ErrInvalidFormat = errors.New("codec: invalid format")

// MediaType 取 Content-Type 头的 media-type 部分（到第一个 ';' 为止，去空白并转小写）。
// 不做完整 RFC 解析——415 判定只需比对主类型，参数（charset/boundary）无关。
// MediaType extracts the media-type part of a Content-Type header (up to the first
// ';', trimmed and lowercased). It does no full RFC parse — a 415 decision only needs
// the base type; parameters (charset/boundary) are irrelevant.
func MediaType(contentType string) string {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	return strings.ToLower(strings.TrimSpace(contentType))
}

// ContentTypeIn 报告实际收到的 Content-Type 是否属于 want 集合（按 media-type 比对）。
// 请求无 Content-Type 时放行（交给解码器处理空体/宽松场景，避免对无体请求误判），
// want 为空集时不校验。
//
// want 应在注册期已归一化为 media-type 小写形式，故此处只归一化收到的值一次，不在每次
// 调用上重复处理端点声明。集合最多两三项，线性比对比 map 查找更快且零分配。
// ContentTypeIn reports whether the received Content-Type belongs to the want set
// (compared by media-type). A missing Content-Type passes (deferring
// empty-body/lenient cases to the decoder) so body-less requests are not wrongly
// rejected, and an empty want set means no check.
//
// want is expected to be already normalized to lowercase media-type form (e.g. at
// registration), so only the received value is normalized here. The set holds at most
// a few entries, so a linear compare beats a map lookup and allocates nothing.
func ContentTypeIn(got string, want []string) bool {
	if got == "" || len(want) == 0 {
		return true
	}
	mt := MediaType(got)
	for _, w := range want {
		if mt == w {
			return true
		}
	}
	return false
}

// DecodeJSON 把 r 严格解码进 v。contentLength 用于区分两种"读到 EOF"：
// 声明了正长度却一个值都没有 = 传输被截断（报错），长度未知或为 0 = 合法空体（放行）。
//
// 返回的错误要么包装 ErrInvalidFormat，要么在链中保留 *http.MaxBytesError（体量超限）；
// 调用方用 errors.As/errors.Is 分类，不应自行解析错误文本。
// DecodeJSON strictly decodes r into v. contentLength distinguishes two kinds of EOF:
// a declared positive length with no value at all means a truncated transfer (error),
// while an unknown or zero length is a legitimate empty body (pass).
//
// A returned error either wraps ErrInvalidFormat or keeps *http.MaxBytesError in its
// chain (size overrun); callers classify with errors.As/errors.Is rather than parsing
// error text.
func DecodeJSON(r io.Reader, v any, contentLength int64) error {
	if r == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidFormat)
	}
	// 用 errors.Is 而非 == 比较 io.EOF：标准库允许把 EOF 包装返回（自定义 io.Reader、
	// http.MaxBytesReader 等中间层都可能这么做），== 会漏判而把"空体"当成解码失败。
	// Compare io.EOF with errors.Is rather than ==: the standard library permits a
	// wrapped EOF (custom io.Readers and intermediate layers do wrap it), and == would
	// miss it and report an empty body as a decode failure.
	dec := json.NewDecoder(r)
	err := dec.Decode(v)
	if err != nil {
		if errors.Is(err, io.EOF) {
			if contentLength > 0 {
				return fmt.Errorf("%w: body of %d bytes ended before any JSON value", ErrInvalidFormat, contentLength)
			}
			// 空请求体：v 保持零值。这通常【不是】错误——框架不内置校验，是否必填由业务
			// 判断；而"声明了 body 却一个字节都没到"是传输截断，已在上面单独报错。
			// An empty body leaves v at its zero value, which usually is NOT an error:
			// requiredness is the business's call. "Declared a body but not one byte
			// arrived" is truncation and is reported separately above.
			return nil
		}
		return wrap(err)
	}
	// 拒绝首个 JSON 值之后的任何内容。Decoder 是流式的，只读掉第一个值就返回，于是
	// `{"a":1} GARBAGE` 与 `{"a":1}{"a":2}` 都会静默成功。后者尤其危险：报文里有两个
	// 对象，本端按第一个处理，而链路上另一个按同样宽松规则解析的组件可能取到第二个，
	// 造成两端对"这次请求/响应是什么"理解不一致（请求走私一类的混淆）。一个 body 只应
	// 表示一个值，多出来的内容是错误而非可忽略的噪声。
	// Reject anything after the first JSON value. The Decoder streams and returns as
	// soon as it has read one value, so `{"a":1} GARBAGE` and `{"a":1}{"a":2}` both
	// succeed silently. The latter is especially dangerous: the body holds two objects,
	// this end acts on the first, and another component parsing just as loosely may take
	// the second, so the two disagree about what the payload was (a smuggling-class
	// confusion). One body should denote one value; trailing content is an error.
	//
	// 用 Token() 而非 Decode(&extra) 探测：Token 只读【一个】令牌（越过空白后的首个
	// 字节即可判定，`]`/`}` 这类顶层垃圾直接成为语法错），而 Decode 会把第二个值
	// 【完整物化】进 any 后才报错——未挂 LimitBody 时，巨型第二值成了免费的 CPU/内存
	// 放大点。（More() 不行：它专为数组/对象内部设计，对顶层尾随的 `]`/`}` 返回 false。）
	// Probe with Token() rather than Decode(&extra): Token reads ONE token (the first
	// post-whitespace byte decides, and top-level garbage like `]`/`}` is an immediate
	// syntax error), whereas Decode fully MATERIALIZED the second value into an any
	// before erroring — without a body limit, a huge second value was free CPU/memory
	// amplification. (More() does not work: it is for inside arrays/objects and returns
	// false for a trailing top-level `]`/`}`.)
	if _, terr := dec.Token(); terr != io.EOF {
		return fmt.Errorf("%w: unexpected trailing content after the JSON value", ErrInvalidFormat)
	}
	return nil
}

// DecodeXML 是 DecodeXML 的 XML 对应物，严格性策略与 JSON 侧一致。
// DecodeXML is the XML counterpart of DecodeJSON with identical strictness.
func DecodeXML(r io.Reader, v any, contentLength int64) error {
	if r == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidFormat)
	}
	dec := xml.NewDecoder(r)
	err := dec.Decode(v)
	if err != nil {
		if errors.Is(err, io.EOF) {
			if contentLength > 0 {
				return fmt.Errorf("%w: body of %d bytes ended before any XML element", ErrInvalidFormat, contentLength)
			}
			return nil
		}
		return wrap(err)
	}
	// 拒绝首个元素之后的内容：Decode 只读一个元素就返回，于是
	// `<User>…</User><User>…</User>` 会静默按第一个处理，与链路上另一个解析器可能取到的
	// 第二个不一致（请求混淆一类）。XML 没有 json.Decoder.More，需自行推进令牌：仅
	// 【纯空白】CharData 可跳过（元素间换行缩进是合法格式），非空白字符数据
	// （`<a/>trailing`）与 JSON 侧一样属尾随垃圾。
	// Reject content after the first element: Decode returns after one element, so
	// `<User>…</User><User>…</User>` is silently handled as the first while another
	// parser may take the second (a confusion class). XML lacks json.Decoder.More, so
	// advance tokens manually: only WHITESPACE-ONLY CharData is skipped (newlines and
	// indentation between elements are legal formatting); non-blank character data
	// (`<a/>trailing`) is trailing garbage, same stance as the JSON side.
	for {
		tok, terr := dec.Token()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			return wrap(terr)
		}
		if cd, isChar := tok.(xml.CharData); isChar {
			if len(bytes.TrimSpace(cd)) == 0 {
				continue
			}
		}
		return fmt.Errorf("%w: unexpected trailing content after the XML document", ErrInvalidFormat)
	}
	return nil
}

// wrap 给底层错误加上 ErrInvalidFormat 标记，同时用 %w 保留原错误，使调用方既能
// errors.Is 出格式错误，也能 errors.As 出 *json.SyntaxError / *http.MaxBytesError 等具体类型。
// wrap tags a low-level error with ErrInvalidFormat while preserving it via %w, so
// callers can errors.Is the format marker and still errors.As concrete types such as
// *json.SyntaxError or *http.MaxBytesError.
func wrap(err error) error {
	return fmt.Errorf("%w: %w", ErrInvalidFormat, err)
}
