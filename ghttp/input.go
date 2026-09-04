package ghttp

// InputSpec 是请求体输入契约:注册期固定解码器与期望 Content-Type,decode 在请求期把
// 请求体解进 *T。它是 OutputSpec[O] 的输入侧对称物——把"用哪个解码器 + 解成哪个类型"
// 绑成一个类型安全的值,供 PostBody/PostParamsBody 等入口的 body 参数使用。
// InputSpec is the request-body input contract: the decoder and expected
// Content-Type are fixed at registration, and decode parses the body into *T at
// request time. It is the input-side mirror of OutputSpec[O], binding "which
// decoder + which target type" into one type-safe value for the body parameter
// of entries like PostBody/PostParamsBody.
type InputSpec[T any] interface {
	// decode 把请求体解码进 v(v 为 *T)。
	// decode parses the request body into v (v is a *T).
	decode(req *Request, v *T) error
	// contentType 返回期望的请求 Content-Type,供 OpenAPI 文档使用;空串表示未声明。
	// contentType returns the expected request Content-Type for OpenAPI docs; empty
	// means undeclared.
	contentType() string
	// contentTypes 返回严格模式下可接受的 Content-Type 集合(已归一化为 media-type);
	// 空集表示不校验。它而非 contentType 才是 415 判定的依据——表单等契约接受多个类型,
	// 单值无法表达,而单值为空曾被当作"放行"从而绕过校验。
	// contentTypes returns the Content-Type set accepted under strict mode (normalized
	// to media-types); an empty set means no check. This, not contentType, drives the
	// 415 decision: contracts such as forms accept several types that a single value
	// cannot express, and an empty single value used to be read as "pass", bypassing
	// the check entirely.
	contentTypes() []string
	// codecMissing 报告本契约是否缺少解码器。Body[T](nil) 造出的是一个【非 nil 的接口
	// 值】(内部 decoder 为 nil),注册期的 in == nil 判不出它,随后取 Content-Type 便
	// nil 解引用 panic,ErrMissingCodec 形同虚设。注册期改问这个方法,让缺失以错误返回。
	// codecMissing reports whether this contract lacks a decoder. Body[T](nil) yields a
	// NON-nil interface value holding a nil decoder, which a registration-time
	// in == nil check cannot detect; reading its Content-Type then panicked on a nil
	// dereference, leaving ErrMissingCodec useless. Registration now asks this method
	// so the omission surfaces as an error.
	codecMissing() bool
}

// bodyInput 是把任意 RequestDecoder 适配为 InputSpec[T] 的通用实现:注册期持有解码器,
// 请求期直接下沉到它。所有内置糖(JSONBody/FormBody/…)都构造它。
// bodyInput is the generic adapter turning any RequestDecoder into an
// InputSpec[T]: it holds the decoder at registration and delegates to it at
// request time. All built-in sugar (JSONBody/FormBody/…) constructs it.
type bodyInput[T any] struct {
	decoder RequestDecoder
}

func (b bodyInput[T]) decode(req *Request, v *T) error { return b.decoder.Decode(req, v) }
func (b bodyInput[T]) codecMissing() bool              { return b.decoder == nil }

// contentType 与 contentTypes 都必须容忍 nil decoder:注册期虽会先经 codecMissing 拒掉
// 缺失解码器的契约,但这两个只读访问器不该依赖调用顺序才安全(此前 contentType 直接
// 解引用,Body[T](nil) 便在注册期 panic 而非返回 ErrMissingCodec)。
// Both contentType and contentTypes must tolerate a nil decoder: registration does
// reject a decoder-less contract via codecMissing first, but these read-only accessors
// must not depend on call order for safety (contentType previously dereferenced
// directly, so Body[T](nil) panicked at registration instead of returning
// ErrMissingCodec).
func (b bodyInput[T]) contentType() string {
	if b.decoder == nil {
		return ""
	}
	return b.decoder.ContentType()
}

func (b bodyInput[T]) contentTypes() []string {
	if b.decoder == nil {
		return nil
	}
	return acceptedContentTypes(b.decoder)
}

// Body 是请求体输入契约的底层构造器:用给定的 RequestDecoder 把请求体解成 T。需要 XML
// 或自定义解码器时用它;JSON/form/text 等常用场景直接用同名快捷糖(JSONBody[T] 等)。
//
// codec 为 nil 时本函数不 panic,而是造出一个"缺解码器"的契约,由注册入口返回
// ErrMissingCodec——注册期报错可被处理,请求期或注册期 panic 不能。
// Body is the low-level constructor for the request-body input contract: it
// decodes the body into T with the given RequestDecoder. Use it for XML or custom
// decoders; for JSON/form/text use the matching sugar (JSONBody[T], etc.).
//
// A nil codec does not panic here; it produces a "decoder missing" contract that the
// registration entries reject with ErrMissingCodec — a registration error can be
// handled, a panic cannot.
func Body[T any](codec RequestDecoder) InputSpec[T] { return bodyInput[T]{decoder: codec} }

// JSONBody 返回一个把请求体按 JSON 解码为 T 的输入契约。等价于 Body[T](JSONCodec())。
// JSONBody returns an input contract decoding the body as JSON into T. Equivalent
// to Body[T](JSONCodec()).
func JSONBody[T any]() InputSpec[T] { return bodyInput[T]{decoder: jsonCodec{}} }

// XMLBody 返回一个把请求体按 XML 解码为 T 的输入契约。等价于 Body[T](XMLCodec())。
// XMLBody returns an input contract decoding the body as XML into T. Equivalent
// to Body[T](XMLCodec()).
func XMLBody[T any]() InputSpec[T] { return bodyInput[T]{decoder: xmlCodec{}} }

// FormBody 返回一个表单输入契约:把 application/x-www-form-urlencoded 或
// multipart/form-data 的【文本字段与上传文件】一并解进 T。文本字段用 form: tag 绑定,
// 文件用 Upload / []Upload 字段(form: tag 指定字段名)绑定——文件由此归属于请求体,
// 而非 path/query/header 参数。
// FormBody returns a form input contract: it decodes BOTH text fields and
// uploaded files of application/x-www-form-urlencoded or multipart/form-data into
// T. Text fields bind via form: tags; files bind via Upload / []Upload fields
// (form: tag names the field) — so files belong to the request body, not to
// path/query/header params.
func FormBody[T any]() InputSpec[T] { return bodyInput[T]{decoder: formCodec{}} }

// TextBody 返回一个把请求体作为纯文本读入 T 的输入契约,T 须为 string 或 []byte。
// 等价于 Body[T](textCodec)。
// TextBody returns an input contract reading the body as plain text into T, where
// T must be string or []byte. Equivalent to Body[T](textCodec).
func TextBody[T any]() InputSpec[T] { return bodyInput[T]{decoder: textCodec{}} }
