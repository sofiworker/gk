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
	// contentType 返回期望的请求 Content-Type,供严格模式下的 415 校验;空串表示不校验
	// (如 form 同时接受 urlencoded 与 multipart)。
	// contentType returns the expected request Content-Type for the strict 415
	// check; empty means no check (e.g. form accepts both urlencoded and
	// multipart).
	contentType() string
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
func (b bodyInput[T]) contentType() string             { return b.decoder.ContentType() }

// Body 是请求体输入契约的底层构造器:用给定的 RequestDecoder 把请求体解成 T。需要 XML
// 或自定义解码器时用它;JSON/form/text 等常用场景直接用同名快捷糖(JSONBody[T] 等)。
// Body is the low-level constructor for the request-body input contract: it
// decodes the body into T with the given RequestDecoder. Use it for XML or custom
// decoders; for JSON/form/text use the matching sugar (JSONBody[T], etc.).
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
