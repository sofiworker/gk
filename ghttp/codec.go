package ghttp

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sofiworker/gk/ghttp/internal/codec"
)

// decodeError 把请求体解码期的底层错误收敛为框架错误:MaxBytesReader 触发的
// *http.MaxBytesError 归到 ErrRequestEntityTooLarge(413),其余归到 ErrInvalidInput(400)。
//
// 两条分支都必须用 %w 而非 %v 包装 err。此前统一写作 fmt.Errorf("%w: %v", ErrInvalidInput, err)
// 把底层错误打平成字符串,切断了错误链:error_chain 里按 errors.As 提取 *http.MaxBytesError
// 的 413 分支因此永不可达,挂了 LimitBody 又没有 Content-Length(chunked 传输)时,超限
// 请求体解码失败会退化成 400。保留错误链后状态码由哨兵单点决定,调用方也仍能 errors.As
// 出 *http.MaxBytesError / *json.SyntaxError 等具体类型做精细处理。
//
// 分类只认 *http.MaxBytesError 这一个类型而不按错误文本判断:文本随标准库版本变化,
// 类型是稳定契约。
// decodeError collapses a low-level request-body decoding error into a framework
// error: an *http.MaxBytesError raised by MaxBytesReader maps to
// ErrRequestEntityTooLarge (413), everything else to ErrInvalidInput (400).
//
// Both branches must wrap err with %w, not %v. The previous uniform
// fmt.Errorf("%w: %v", ErrInvalidInput, err) flattened the cause into a string and
// severed the error chain, making the error_chain branch that extracts
// *http.MaxBytesError via errors.As unreachable: with LimitBody installed and no
// Content-Length (chunked transfer), an oversized body degraded to 400. Keeping the
// chain lets a single sentinel decide the status, and callers can still errors.As
// out concrete types such as *http.MaxBytesError or *json.SyntaxError.
//
// Classification matches only the *http.MaxBytesError type, never error text: the
// text varies across standard-library versions, the type is a stable contract.
func decodeError(err error) error {
	if err == nil {
		return nil
	}
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return fmt.Errorf("%w: %w", ErrRequestEntityTooLarge, err)
	}
	return fmt.Errorf("%w: %w", ErrInvalidInput, err)
}

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

// MultiContentTypeDecoder 是 RequestDecoder 的可选补充:一个解码器若能接受多个请求
// Content-Type(如表单同时接受 urlencoded 与 multipart),实现它来声明完整集合,严格
// 校验会按该集合判定而非单值 ContentType()。
//
// 它单独成一个小接口而非并入 RequestDecoder:绝大多数解码器只对应一种 Content-Type,
// 让它们被迫实现一个返回单元素切片的方法既冗余、又会在每次注册时多分配一个切片。
//
// 为什么必须有这个声明能力:单值契约表达不了"接受两种",此前表单解码器只能返回空串,
// 而空串在严格校验里意味着【放行】——于是把 application/json 的请求体发到表单端点会被
// 放行,而表单解析根本不认识那个 Content-Type、压根不读请求体,最终静默解出一个零值
// 结构体并回 200。调用方数据被丢弃却毫无提示,这是最坏的失败形态(fail-open),故表单
// 契约改为显式声明两个可接受类型。
// MultiContentTypeDecoder is an optional supplement to RequestDecoder: a decoder
// that accepts several request Content-Types (e.g. a form accepting both
// urlencoded and multipart) implements it to declare the full set, and the strict
// check then decides against that set instead of the single-valued ContentType().
//
// It is a separate small interface rather than part of RequestDecoder because the
// vast majority of decoders map to exactly one Content-Type; forcing them to
// implement a method returning a one-element slice would be both redundant and an
// extra slice allocation per registration.
//
// Why this declaration is needed at all: a single-valued contract cannot express
// "accepts two", so the form decoder previously had to return the empty string —
// and in the strict check an empty string means PASS. A body sent to a form
// endpoint with Content-Type application/json was therefore admitted, while form
// parsing did not recognize that Content-Type and never read the body at all,
// silently yielding a zero-valued struct and a 200. The caller's data was dropped
// with no signal whatsoever — the worst possible failure mode (fail-open) — so the
// form contract now declares both acceptable types explicitly.
type MultiContentTypeDecoder interface {
	// ContentTypes 返回本解码器可接受的请求 Content-Type 列表;返回空表示回退到
	// RequestDecoder.ContentType() 的单值语义。
	// ContentTypes returns the request Content-Types this decoder accepts; an empty
	// result falls back to the single-valued RequestDecoder.ContentType() semantics.
	ContentTypes() []string
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
	// 严格解码内核（拒绝尾随内容、判截断、流式）与 client 侧共用，见 internal/codec。
	// The strict decoding kernel (trailing-content rejection, truncation detection,
	// streaming) is shared with the client; see internal/codec.
	return decodeError(codec.DecodeJSON(req.Body, v, req.ContentLength))
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
	return decodeError(codec.DecodeXML(req.Body, v, req.ContentLength))
}

func (xmlCodec) Encode(resp *Response, v any) error {
	return xml.NewEncoder(resp).Encode(v)
}

// XMLCodec 返回一个 XML 编解码器,同时实现 RequestDecoder 与 ResponseEncoder。
// XMLCodec returns an XML codec implementing both RequestDecoder and
// ResponseEncoder.
func XMLCodec() Codec { return xmlCodec{} }

// mediaType 取 Content-Type 头的 media-type 部分(到第一个 ';' 为止,去空白并转小写)。
// 不做完整 RFC 解析——415 判定只需比对主类型,参数(charset/boundary)无关。
// mediaType extracts the media-type part of a Content-Type header (up to the
// first ';', trimmed and lowercased). It does no full RFC parse — a 415 decision
// only needs the base type; parameters (charset/boundary) are irrelevant.
func mediaType(contentType string) string { return codec.MediaType(contentType) }

// contentTypeIn 报告请求 Content-Type 是否属于 want 集合(按 media-type 比对)。请求无
// Content-Type 时放行(交给解码器处理空体/宽松场景，避免对无体请求误判)，want 为空集时不校验。
//
// want 在注册期就已归一化为 media-type 小写形式(见 acceptedContentTypes)，故此处只
// 归一化请求侧一次，不在每请求上重复处理端点声明。集合最多两三项，线性比对比 map 查找
// 更快且零分配。
// contentTypeIn reports whether the request Content-Type belongs to the want set
// (compared by media-type). A missing request Content-Type passes (deferring
// empty-body/lenient cases to the decoder) so body-less requests are not wrongly
// rejected, and an empty want set means no check.
//
// want is already normalized to lowercase media-type form at registration (see
// acceptedContentTypes), so only the request side is normalized here rather than
// reprocessing the endpoint's declaration on every request. The set holds at most a
// few entries, so a linear compare beats a map lookup and allocates nothing.
func contentTypeIn(got string, want []string) bool { return codec.ContentTypeIn(got, want) }

// unsupportedMediaTypeError 构造一个 415 错误,报出实际收到的与端点可接受的 media-type。
// 它只在校验已判定失败的冷路径上调用,故此处的字符串拼接分配不落在命中热路径上。
// unsupportedMediaTypeError builds a 415 error reporting the media-type received and
// the ones the endpoint accepts. It is called only on the cold path after the check
// has already failed, so its string-building allocation never lands on the hit path.
func unsupportedMediaTypeError(got string, want []string) error {
	return fmt.Errorf("%w: got %q want %q", ErrUnsupportedMediaType, mediaType(got), strings.Join(want, ", "))
}

// acceptedContentTypes 返回一个解码器在严格模式下可接受的 media-type 集合,已归一化为
// 小写、去参数形式。优先取 MultiContentTypeDecoder 声明的集合,否则回退到单值
// ContentType();两者皆空则返回 nil,表示该解码器主动放弃 415 校验。
//
// 归一化只在注册期做一次:请求热路径拿到的是可直接比对的字符串,不必每请求重复解析
// 端点声明。
// acceptedContentTypes returns the media-type set a decoder accepts under strict
// mode, normalized to lowercase and stripped of parameters. It prefers the set
// declared by MultiContentTypeDecoder, else falls back to the single-valued
// ContentType(); when both are empty it returns nil, meaning the decoder opts out
// of the 415 check.
//
// Normalization happens once at registration: the request hot path receives
// directly comparable strings and never re-parses the endpoint's declaration.
func acceptedContentTypes(dec RequestDecoder) []string {
	if multi, ok := dec.(MultiContentTypeDecoder); ok {
		if cts := multi.ContentTypes(); len(cts) > 0 {
			out := make([]string, 0, len(cts))
			for _, ct := range cts {
				if mt := mediaType(ct); mt != "" {
					out = append(out, mt)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if mt := mediaType(dec.ContentType()); mt != "" {
		return []string{mt}
	}
	return nil
}
