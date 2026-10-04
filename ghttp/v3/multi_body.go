package v3

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"sync"

	root "github.com/sofiworker/gk/ghttp"
)

// ContentType 表示请求体的内容格式。
// ContentType represents the content format of the request body.
type ContentType string

const (
	// JSON 格式 / JSON format
	JSON ContentType = "json"

	// Form 格式（application/x-www-form-urlencoded）
	// Form format (application/x-www-form-urlencoded)
	Form ContentType = "form"

	// Multipart 格式（multipart/form-data）
	// Multipart format (multipart/form-data)
	Multipart ContentType = "multipart"

	// XML 格式 / XML format
	XML ContentType = "xml"

	// RawBytes 原始字节流 / Raw bytes
	RawBytes ContentType = "raw"
)

// MultiBodyAccessor 提供多种格式的 Body 访问。
// MultiBodyAccessor provides multi-format body access.
type MultiBodyAccessor[T any] struct {
	ctx     context.Context
	req     *Request
	cached  map[ContentType]*T
	errors  map[ContentType]error
	mu      sync.RWMutex
	once    map[ContentType]*sync.Once
}

// newMultiBodyAccessor 创建多格式 Body 访问器。
// newMultiBodyAccessor creates a multi-format body accessor.
func newMultiBodyAccessor[T any](ctx context.Context, req *Request) *MultiBodyAccessor[T] {
	return &MultiBodyAccessor[T]{
		ctx:    ctx,
		req:    req,
		cached: make(map[ContentType]*T),
		errors: make(map[ContentType]error),
		once:   make(map[ContentType]*sync.Once),
	}
}

// Get 按指定格式获取 Body 数据（延迟解码）。
// Get retrieves body data in the specified format (lazy decoding).
func (m *MultiBodyAccessor[T]) Get(format ContentType) (T, error) {
	m.mu.Lock()
	if m.once[format] == nil {
		m.once[format] = &sync.Once{}
	}
	once := m.once[format]
	m.mu.Unlock()

	once.Do(func() {
		val, err := m.decode(format)
		m.mu.Lock()
		if err != nil {
			m.errors[format] = err
		} else {
			m.cached[format] = &val
		}
		m.mu.Unlock()
	})

	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, ok := m.errors[format]; ok {
		var zero T
		return zero, err
	}

	if cached, ok := m.cached[format]; ok {
		return *cached, nil
	}

	var zero T
	return zero, fmt.Errorf("body not decoded yet")
}

// MustGet 获取 Body 数据，失败时 panic。
// MustGet retrieves body data, panics on error.
func (m *MultiBodyAccessor[T]) MustGet(format ContentType) T {
	val, err := m.Get(format)
	if err != nil {
		panic(fmt.Sprintf("MultiBodyAccessor.MustGet failed: %v", err))
	}
	return val
}

// decode 根据格式解码 Body。
// decode decodes the body according to the format.
func (m *MultiBodyAccessor[T]) decode(format ContentType) (T, error) {
	var zero T

	if m.req == nil || m.req.Request == nil {
		return zero, ErrMissingBody
	}

	switch format {
	case JSON:
		return m.decodeJSON()
	case Form:
		return m.decodeForm()
	case Multipart:
		return m.decodeMultipart()
	case XML:
		return m.decodeXML()
	case RawBytes:
		return m.decodeRaw()
	default:
		return zero, fmt.Errorf("unsupported content type: %s", format)
	}
}

// decodeJSON 解码 JSON 格式。
// decodeJSON decodes JSON format.
func (m *MultiBodyAccessor[T]) decodeJSON() (T, error) {
	var val T

	if m.req.Body == nil {
		return val, ErrMissingBody
	}

	// 检查 Content-Type
	// Check Content-Type
	ct := m.req.Header.Get("Content-Type")
	if ct != "" {
		mediaType, _, _ := mime.ParseMediaType(ct)
		if !strings.Contains(mediaType, "json") {
			return val, fmt.Errorf("%w: expected application/json, got %s", root.ErrUnsupportedMediaType, mediaType)
		}
	}

	if err := json.NewDecoder(m.req.Body).Decode(&val); err != nil {
		return val, fmt.Errorf("%w: %v", root.ErrInvalidInput, err)
	}

	return val, nil
}

// decodeForm 解码 Form 格式。
// decodeForm decodes form format.
func (m *MultiBodyAccessor[T]) decodeForm() (T, error) {
	var val T

	if err := m.req.ParseForm(); err != nil {
		return val, fmt.Errorf("%w: %v", root.ErrInvalidInput, err)
	}

	// 检查 Content-Type
	// Check Content-Type
	ct := m.req.Header.Get("Content-Type")
	if ct != "" {
		mediaType, _, _ := mime.ParseMediaType(ct)
		if !strings.Contains(mediaType, "x-www-form-urlencoded") {
			return val, fmt.Errorf("%w: expected application/x-www-form-urlencoded, got %s", root.ErrUnsupportedMediaType, mediaType)
		}
	}

	// 使用 BindInput 解码 Form
	// Use BindInput to decode form
	codec := BindInput[T]()
	if codec.err != nil {
		return val, codec.err
	}

	if codec.decoder != nil {
		if err := codec.decoder.Decode(m.req, &val); err != nil {
			return val, fmt.Errorf("%w: %v", root.ErrInvalidInput, err)
		}
	}

	return val, nil
}

// decodeMultipart 解码 Multipart 格式。
// decodeMultipart decodes multipart format.
func (m *MultiBodyAccessor[T]) decodeMultipart() (T, error) {
	var val T

	// 检查 Content-Type
	// Check Content-Type
	ct := m.req.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return val, fmt.Errorf("%w: expected multipart/form-data", root.ErrUnsupportedMediaType)
	}

	boundary := params["boundary"]
	if boundary == "" {
		return val, fmt.Errorf("%w: missing boundary in multipart", root.ErrInvalidInput)
	}

	reader := multipart.NewReader(m.req.Body, boundary)
	form, err := reader.ReadForm(32 << 20) // 32 MB 默认限制
	if err != nil {
		return val, fmt.Errorf("%w: %v", root.ErrInvalidInput, err)
	}
	defer form.RemoveAll()

	m.req.MultipartForm = form

	// 使用 BindInput 解码 Multipart
	// Use BindInput to decode multipart
	codec := BindInput[T]()
	if codec.err != nil {
		return val, codec.err
	}

	if codec.decoder != nil {
		if err := codec.decoder.Decode(m.req, &val); err != nil {
			return val, fmt.Errorf("%w: %v", root.ErrInvalidInput, err)
		}
	}

	return val, nil
}

// decodeXML 解码 XML 格式。
// decodeXML decodes XML format.
func (m *MultiBodyAccessor[T]) decodeXML() (T, error) {
	var val T

	if m.req.Body == nil {
		return val, ErrMissingBody
	}

	// 检查 Content-Type
	// Check Content-Type
	ct := m.req.Header.Get("Content-Type")
	if ct != "" {
		mediaType, _, _ := mime.ParseMediaType(ct)
		if !strings.Contains(mediaType, "xml") {
			return val, fmt.Errorf("%w: expected application/xml, got %s", root.ErrUnsupportedMediaType, mediaType)
		}
	}

	if err := xml.NewDecoder(m.req.Body).Decode(&val); err != nil {
		return val, fmt.Errorf("%w: %v", root.ErrInvalidInput, err)
	}

	return val, nil
}

// decodeRaw 读取原始字节流。
// decodeRaw reads raw bytes.
func (m *MultiBodyAccessor[T]) decodeRaw() (T, error) {
	var val T

	if m.req.Body == nil {
		return val, ErrMissingBody
	}

	data, err := io.ReadAll(m.req.Body)
	if err != nil {
		return val, fmt.Errorf("%w: %v", root.ErrInvalidInput, err)
	}

	// 尝试将 []byte 赋值给 T
	// Try to assign []byte to T
	if bytes, ok := any(&val).(*[]byte); ok {
		*bytes = data
		return val, nil
	}

	return val, fmt.Errorf("raw format only supports []byte type")
}

// WithMultiBody 为 RequestOf 添加多格式 Body 访问器。
// WithMultiBody adds a multi-format body accessor to RequestOf.
//
// 用法 / Usage:
//
//	func UpdateProfile(ctx context.Context, req v3.RequestOf[ProfileForm]) (User, error) {
//	    bodyAccessor := v3.WithMultiBody(ctx, &req)
//
//	    // 根据实际情况选择格式
//	    // Choose format based on actual situation
//	    form, err := bodyAccessor.Get(v3.Form)
//	    // 或 / or
//	    json, err := bodyAccessor.Get(v3.JSON)
//
//	    return updateUser(ctx, form)
//	}
func WithMultiBody[T any](ctx context.Context, req *RequestOf[T]) *MultiBodyAccessor[T] {
	return newMultiBodyAccessor[T](ctx, req.Request)
}
