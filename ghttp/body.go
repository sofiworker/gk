package ghttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
)

// ErrInvalidBody 表示请求体无法读取或解码。
// ErrInvalidBody indicates the request body could not be read or decoded.
// Body[T].Raw/Decode 的读取与解码错误都包装此哨兵。
// read and decode errors from Body[T].Raw/Decode wrap this sentinel.
var ErrInvalidBody = errors.New("invalid request body")

// ErrMultipleBodyFields 表示输入结构体声明了多个 body 字段。
// ErrMultipleBodyFields indicates the input struct declares multiple body fields.
var ErrMultipleBodyFields = errors.New("multiple body fields are not allowed")

// ErrBodyFieldMustBeValue 表示 Body[T] 必须以值字段使用,不能是指针。
// ErrBodyFieldMustBeValue indicates Body[T] must be a value field, not a pointer.
var ErrBodyFieldMustBeValue = errors.New("ghttp.Body[T] must be used as a value field, not a pointer")

// bodyFieldMarker 供反射识别“这是一个 Body[T] 值类型”。
// bodyFieldMarker lets reflection identify a Body[T] value type.
// 值接收者使 Body[T] 与 *Body[T] 都可识别,具体值/指针由 Kind 区分。
// the value receiver marks both; value vs pointer is distinguished by Kind.
type bodyFieldMarker interface{ lazyBodyField() }

var bodyFieldMarkerType = reflect.TypeOf((*bodyFieldMarker)(nil)).Elem()

func (Body[T]) lazyBodyField() {}

// bodySourceSetter 在绑定期给 Body[T] 安装请求句柄。
// bodySourceSetter installs the request handle into Body[T] during binding.
type bodySourceSetter interface{ setBodySource(*bodySource) }

func (b *Body[T]) setBodySource(src *bodySource) { b.src = src }

// bodySource 是 Body[T] 的类型擦除共享状态:原始字节、读取错误与解码结果
// 都缓存于此,字段拷贝与 validator/handler 的多次调用共享同一份。
// bodySource is the type-erased shared state of Body[T]: raw bytes, read errors
// and the decoded value are cached here so field copies and repeated
// validator/handler calls share one result.
type bodySource struct {
	req      *http.Request
	cfg      *Config
	codecMgr *CodecManager

	readOnce sync.Once
	raw      []byte
	readErr  error

	decodeOnce sync.Once
	decoded    any
	decodeErr  error
}

func newBodySource(r *http.Request, c *Config, codecMgr *CodecManager) *bodySource {
	return &bodySource{req: r, cfg: c, codecMgr: codecMgr}
}

func (s *bodySource) rawBytes() ([]byte, error) {
	s.readOnce.Do(func() {
		s.raw, s.readErr = RawBody(s.req)
	})
	return s.raw, s.readErr
}

func (s *bodySource) contentType() string {
	if s.req == nil {
		return ""
	}
	return s.req.Header.Get("Content-Type")
}

// Body 是惰性请求体视图:绑定期只装句柄,不读流、不反序列化。
// Body is a lazy request body view: binding only installs a handle; the stream
// is neither read nor decoded until first access.
// 值语义、仅在 handler 期间有效、单 goroutine 使用;Raw/Decode 首次访问时
// 读流并缓存,后续调用零成本复用。
// it has value semantics, is valid for the handler lifetime and single-goroutine
// use; Raw/Decode read and cache on first access, later calls reuse the result.
// 零值安全:Raw/Decode 返回 ErrInvalidBody。
// the zero value is safe: Raw/Decode return ErrInvalidBody.
type Body[T any] struct {
	src *bodySource
	// typ 让 T 可被反射还原(OpenAPI/绑定识别);永不由框架填充。
	// typ reifies T for reflection (OpenAPI/binding detection); never populated.
	typ *T
}

// Raw 返回请求体原始字节,不做反序列化;与 RawBody(r) 及 Decode 共享同一份。
// Raw returns the raw body bytes without decoding; it shares bytes with
// RawBody(r) and Decode.
func (b Body[T]) Raw() ([]byte, error) {
	if b.src == nil {
		return nil, fmt.Errorf("%w: body is not bound to a request", ErrInvalidBody)
	}
	raw, err := b.src.rawBytes()
	if err != nil {
		return raw, fmt.Errorf("%w: %w", ErrInvalidBody, err)
	}
	return raw, nil
}

// ContentType 返回原始 Content-Type 头值。
// ContentType returns the raw Content-Type header value.
func (b Body[T]) ContentType() string {
	if b.src == nil {
		return ""
	}
	return b.src.contentType()
}

// bodyDecodeKind 表示 Body[T] 的解码格式策略。
// bodyDecodeKind is the decode-format strategy of Body[T].
type bodyDecodeKind uint8

const (
	// bodyDecodeAuto 按 Content-Type 自动派发(便捷层)。
	// bodyDecodeAuto dispatches by Content-Type (the convenience path).
	bodyDecodeAuto bodyDecodeKind = iota
	// bodyDecodeJSON 强制标准库 JSON,无视 Content-Type。
	// bodyDecodeJSON forces the standard-library JSON decoder, ignoring Content-Type.
	bodyDecodeJSON
	// bodyDecodeXML 强制 XML,无视 Content-Type。
	// bodyDecodeXML forces XML, ignoring Content-Type.
	bodyDecodeXML
	// bodyDecodeForm 强制表单:multipart 按 multipart 解析,否则按 urlencoded。
	// bodyDecodeForm forces a form: multipart parses as multipart, otherwise urlencoded.
	bodyDecodeForm
)

// Decode 按 Content-Type 自动派发解码请求体;首次调用执行解码并缓存结果。
// Decode decodes the body by Content-Type; the first call decodes and caches.
// 这是便捷层:JSON 走标准库,其余类型经 CodecManager 派发,缺失回退 JSON。
// This is the convenience path: JSON uses the standard library, other types dispatch through
// CodecManager, and a missing Content-Type falls back to JSON.
// 显式声明格式请用 DecodeJSON/DecodeXML/DecodeForm。所有方法共享同一份缓存,
// 首次调用(无论哪个方法)决定解码格式与结果。
// Prefer DecodeJSON/DecodeXML/DecodeForm for an explicit format. All methods
// share one cache; the first call (whichever) fixes the format and result.
func (b Body[T]) Decode() (T, error) { return b.decodeWith(bodyDecodeAuto) }

// DecodeJSON 强制按标准库 JSON 解码,无视 Content-Type;首次调用缓存结果。
// DecodeJSON forces standard-library JSON decoding, ignoring Content-Type; the first call caches.
func (b Body[T]) DecodeJSON() (T, error) { return b.decodeWith(bodyDecodeJSON) }

// DecodeXML 强制按 XML 解码,无视 Content-Type;首次调用缓存结果。
// DecodeXML forces XML decoding, ignoring Content-Type; the first call caches.
func (b Body[T]) DecodeXML() (T, error) { return b.decodeWith(bodyDecodeXML) }

// DecodeForm 强制按表单解码:multipart 按 multipart 解析,否则按 urlencoded;
// 首次调用缓存结果。
// DecodeForm forces form decoding: multipart parses as multipart, otherwise
// urlencoded; the first call caches.
func (b Body[T]) DecodeForm() (T, error) { return b.decodeWith(bodyDecodeForm) }

func (b Body[T]) decodeWith(kind bodyDecodeKind) (T, error) {
	var zero T
	if b.src == nil {
		return zero, fmt.Errorf("%w: body is not bound to a request", ErrInvalidBody)
	}
	b.src.decodeOnce.Do(func() {
		var v T
		if err := b.src.decode(&v, kind); err != nil {
			b.src.decodeErr = err
			return
		}
		b.src.decoded = any(v)
	})
	if b.src.decodeErr != nil {
		return zero, b.src.decodeErr
	}
	return b.src.decoded.(T), nil
}

func (s *bodySource) decode(v any, kind bodyDecodeKind) error {
	if s.cfg != nil && s.cfg.bodyDecoder != nil {
		raw, err := s.rawBytes()
		if err != nil {
			return wrapBodyError(err)
		}
		return wrapBodyError(s.cfg.bodyDecoder(bytes.NewReader(raw), s.contentType(), v))
	}
	switch kind {
	case bodyDecodeJSON:
		raw, err := s.rawBytes()
		if err != nil {
			return wrapBodyError(err)
		}
		return wrapBodyError(json.Unmarshal(raw, v))
	case bodyDecodeXML:
		raw, err := s.rawBytes()
		if err != nil {
			return wrapBodyError(err)
		}
		return wrapBodyError((&XMLCodec{}).Unmarshal(bytes.NewReader(raw), v))
	case bodyDecodeForm:
		if normalizeContentType(s.contentType()) == MIMEMultipartPOSTForm {
			return s.decodeMultipart(v)
		}
		return s.decodeURLEncodedForce(v)
	default:
		return s.decodeAuto(v)
	}
}

func (s *bodySource) decodeAuto(v any) error {
	ct := normalizeContentType(s.contentType())
	switch ct {
	case MIMEPOSTForm:
		return s.decodeURLEncoded(v)
	case MIMEMultipartPOSTForm:
		return s.decodeMultipart(v)
	}
	if ct == "" || ct == MIMEJSON {
		raw, err := s.rawBytes()
		if err != nil {
			return wrapBodyError(err)
		}
		return wrapBodyError(json.Unmarshal(raw, v))
	}
	raw, err := s.rawBytes()
	if err != nil {
		return wrapBodyError(err)
	}
	mgr := s.codecMgr
	if mgr == nil {
		mgr = NewCodecManager()
	}
	codec, ok := mgr.Resolve(ct)
	if !ok {
		return fmt.Errorf("%w: unsupported media type %q", ErrInvalidBody, s.contentType())
	}
	return wrapBodyError(codec.Unmarshal(bytes.NewReader(raw), v))
}

// decodeURLEncoded 从共享 postForm 缓存填充结构体;非结构体目标沿用
// FormCodec 语义。
// decodeURLEncoded fills the struct from the shared postForm cache; non-struct
// targets keep FormCodec semantics.
func (s *bodySource) decodeURLEncoded(v any) error {
	_, post, err := formValuesFromRequest(s.req)
	if err != nil {
		return wrapBodyError(err)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr && !rv.IsNil() && rv.Elem().Kind() == reflect.Struct {
		return wrapBodyError(fillFormStruct(post, rv.Elem()))
	}
	raw, err := s.rawBytes()
	if err != nil {
		return wrapBodyError(err)
	}
	return wrapBodyError((&FormCodec{}).Unmarshal(bytes.NewReader(raw), v))
}

// decodeURLEncodedForce 无条件把 body 字节按 urlencoded 解析,不依赖 Content-Type。
// decodeURLEncodedForce parses body bytes as urlencoded unconditionally,
// ignoring Content-Type.
func (s *bodySource) decodeURLEncodedForce(v any) error {
	raw, err := s.rawBytes()
	if err != nil {
		return wrapBodyError(err)
	}
	return wrapBodyError((&FormCodec{}).Unmarshal(bytes.NewReader(raw), v))
}

// decodeMultipart 从共享 multipart 表单填充结构体(支持 FileHeader)。
// decodeMultipart fills the struct from the shared multipart form (FileHeader supported).
func (s *bodySource) decodeMultipart(v any) error {
	form, err := MultipartForm(s.req, defaultMaxMemory)
	if err != nil {
		return wrapBodyError(err)
	}
	target, err := indirectDecodeValue(v)
	if err != nil || target.Kind() != reflect.Struct {
		return fmt.Errorf("%w: multipart target must be a struct pointer", ErrInvalidBody)
	}
	err = fillMultipartBody(target, form)
	return wrapBodyError(err)
}

func wrapBodyError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalidBody, err)
}

func isLazyBodyFieldType(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && t.Implements(bodyFieldMarkerType)
}

func isLazyBodyPointerType(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr && t.Implements(bodyFieldMarkerType)
}
