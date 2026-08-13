package ghttp

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"

	jsonx "github.com/goccy/go-json"
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

// Decode 按 Content-Type 解码请求体;首次调用执行解码并缓存结果。
// Decode decodes the body by Content-Type; the first call decodes and caches.
// JSON 走 goccy,其余类型经 CodecManager 派发;见设计文档“非 JSON body 处理”。
// JSON uses goccy, other types dispatch through CodecManager; see the design doc.
func (b Body[T]) Decode() (T, error) {
	var zero T
	if b.src == nil {
		return zero, fmt.Errorf("%w: body is not bound to a request", ErrInvalidBody)
	}
	b.src.decodeOnce.Do(func() {
		var v T
		if err := b.src.decode(&v); err != nil {
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

func (s *bodySource) decode(v any) error {
	if s.cfg != nil && s.cfg.bodyDecoder != nil {
		raw, err := s.rawBytes()
		if err != nil {
			return wrapBodyError(err)
		}
		return wrapBodyError(s.cfg.bodyDecoder(bytes.NewReader(raw), s.contentType(), v))
	}
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
		return wrapBodyError(jsonx.Unmarshal(raw, v))
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

// decodeMultipart 从共享 multipart 表单填充结构体(支持 FileHeader)。
// decodeMultipart fills the struct from the shared multipart form (FileHeader supported).
func (s *bodySource) decodeMultipart(v any) error {
	form, err := MultipartForm(s.req, defaultMaxMemory)
	if err != nil {
		return wrapBodyError(err)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("%w: multipart target must be a struct pointer", ErrInvalidBody)
	}
	err = fillMultipartBody(rv.Elem(), form)
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
