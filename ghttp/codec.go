package ghttp

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	if req.Body == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidInput)
	}
	// 用 errors.Is 而非 == 比较 io.EOF:标准库允许把 EOF 包装返回(自定义 io.Reader、
	// http.MaxBytesReader 等中间层都可能这么做),== 会漏判而把"空体"当成解码失败。
	// Compare io.EOF with errors.Is rather than ==: the standard library permits a
	// wrapped EOF (custom io.Readers and intermediate layers do wrap it), and == would
	// miss it and report an empty body as a decode failure.
	dec := json.NewDecoder(req.Body)
	err := dec.Decode(v)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// 空请求体：v 保持零值。这通常【不是】错误——本框架不内置校验，是否必填由业务
			// 判断。但“客户端声明了 body 而实际一个字节都没到”是另一回事：那是传输被截断
			// （连接中断、半写请求），把它当成功会让 handler 拿着【全零结构体】继续执行——
			// 一次注册表单变成“用户名=空、密码=空”仍然 200。故仅在明确声明了正长度时报错，
			// 长度未知（chunked / -1）或为 0 时保持宽容。
			// An empty body leaves v at its zero value, which usually is NOT an error: this
			// framework has no built-in validation, so requiredness is the business's call.
			// But "the client declared a body while not one byte arrived" is different — that
			// is a truncated transfer (dropped connection, half-written request), and calling
			// it success lets the handler proceed with an ALL-ZERO struct: a signup request
			// becomes "empty username, empty password" and still returns 200. So report it
			// only when a positive length was declared, staying lenient when the length is
			// unknown (chunked / -1) or zero.
			if req.ContentLength > 0 {
				return fmt.Errorf("%w: body of %d bytes ended before any JSON value", ErrInvalidInput, req.ContentLength)
			}
			return nil
		}
		return decodeError(err)
	}
	// 拒绝首个 JSON 值之后的任何内容。Decoder 是流式的,只读掉第一个值就返回,于是
	// `{"a":1} GARBAGE` 与 `{"a":1}{"a":2}` 都会静默成功。后者尤其危险:请求体里有两个
	// 对象,本端按第一个处理,而链路上另一个按同样宽松规则解析的组件可能取到第二个,
	// 造成两端对"这次请求是什么"理解不一致(请求走私一类的混淆)。一个请求体只应表示
	// 一个值,多出来的内容是错误而非可忽略的噪声。
	// Reject anything after the first JSON value. The Decoder streams and returns as soon
	// as it has read one value, so `{"a":1} GARBAGE` and `{"a":1}{"a":2}` both succeed
	// silently. The latter is especially dangerous: the body holds two objects, this end
	// acts on the first, and another component along the path parsing just as loosely may
	// take the second, so the two disagree about what the request was (a smuggling-class
	// confusion). One body should denote one value; trailing content is an error, not
	// ignorable noise.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: unexpected trailing content after the JSON value", ErrInvalidInput)
		}
		return decodeError(err)
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
	dec := xml.NewDecoder(req.Body)
	err := dec.Decode(v)
	if err != nil {
		// 与 JSON 同一立场：声明了正长度却读到 EOF 属于截断，不能当空 body 成功。
		// Same stance as JSON: a declared positive length that yields EOF is truncation,
		// not a successful empty body.
		if errors.Is(err, io.EOF) {
			if req.ContentLength > 0 {
				return fmt.Errorf("%w: body of %d bytes ended before any XML element", ErrInvalidInput, req.ContentLength)
			}
			return nil
		}
		return decodeError(err)
	}
	// 拒绝首个元素之后的内容：Decode 只读一个元素就返回，于是 `<User>…</User><User>…</User>`
	// 会静默按第一个处理，与链路上另一个解析器可能取到的第二个不一致（请求混淆一类）。
	// XML 没有 json.Decoder.More，需自行推进到下一个非字符令牌。
	// Reject content after the first element: Decode returns after one element, so
	// `<User>…</User><User>…</User>` is silently handled as the first while another parser
	// on the path may take the second (a request-confusion class). XML lacks
	// json.Decoder.More, so advance to the next non-character token manually.
	for {
		tok, terr := dec.Token()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			return decodeError(terr)
		}
		if _, isChar := tok.(xml.CharData); isChar {
			continue
		}
		return fmt.Errorf("%w: unexpected trailing content after the XML document", ErrInvalidInput)
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

// mediaType 取 Content-Type 头的 media-type 部分(到第一个 ';' 为止,去空白并转小写)。
// 不做完整 RFC 解析——415 判定只需比对主类型,参数(charset/boundary)无关。
// mediaType extracts the media-type part of a Content-Type header (up to the
// first ';', trimmed and lowercased). It does no full RFC parse — a 415 decision
// only needs the base type; parameters (charset/boundary) are irrelevant.
func mediaType(contentType string) string {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	return strings.ToLower(strings.TrimSpace(contentType))
}

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
func contentTypeIn(got string, want []string) bool {
	if got == "" || len(want) == 0 {
		return true
	}
	mt := mediaType(got)
	for _, w := range want {
		if mt == w {
			return true
		}
	}
	return false
}

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
