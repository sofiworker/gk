package v3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"sync"

	root "github.com/sofiworker/gk/ghttp"
)

// RequestOf 组合类型化数据与按需请求视图；数据格式由输入 codec 选择，视图仅在当前请求期间有效。
// RequestOf combines typed data and an on-demand request view; the input codec selects the format and the view is valid only during the current request.
//
// ⚠️ 破坏性变更：Data 字段改为 Data() 方法，实现 lazy 解码以提升提前返回场景的性能。
// ⚠️ Breaking change: Data field changed to Data() method for lazy decoding to improve early-return scenarios.
type RequestOf[T any] struct {
	RequestInput

	// 私有字段用于 lazy 解码
	// Private fields for lazy decoding
	lazyData *lazyData[T]
}

// lazyData 封装 lazy 解码逻辑，使用指针避免 sync.Once 的 copylocks 问题
// lazyData encapsulates lazy decoding logic, using pointer to avoid sync.Once copylocks issue
type lazyData[T any] struct {
	data     T
	dataOnce sync.Once
	dataErr  error
	decoder  func(context.Context, *Request) (T, error)
	req      *Request
}

// Data 返回解码后的请求体数据。首次调用时解码，后续调用返回缓存结果。
// Data returns the decoded request body. Decodes on first call, returns cached result on subsequent calls.
func (r *RequestOf[T]) Data(ctx context.Context) (T, error) {
	if r.lazyData == nil {
		var zero T
		return zero, errors.New("ghttp/v3: uninitialized RequestOf")
	}
	r.lazyData.dataOnce.Do(func() {
		if r.lazyData.decoder != nil {
			r.lazyData.data, r.lazyData.dataErr = r.lazyData.decoder(ctx, r.lazyData.req)
		}
	})
	return r.lazyData.data, r.lazyData.dataErr
}

// forceValidate 内部方法，用于在 required=true 时强制触发验证
// forceValidate is an internal method to force validation when required=true
func (r *RequestOf[T]) forceValidate(ctx context.Context) error {
	_, err := r.Data(ctx)
	return err
}

// lazyRequestValidator 内部接口，用于 route.go 强制触发验证
// lazyRequestValidator is an internal interface for route.go to force validation
type lazyRequestValidator interface {
	forceValidate(context.Context) error
}

type requestInputFactory interface {
	requestInputContract(bool) (any, error)
	dataValidation(any, bool) (any, error)
}

func requestInputFactoryFor(t reflect.Type) requestInputFactory {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if !t.Implements(reflect.TypeFor[requestInputFactory]()) {
		return nil
	}
	value := reflect.New(t).Elem().Interface()
	factory, _ := value.(requestInputFactory)
	return factory
}

func (RequestOf[T]) requestInputContract(pointer bool) (any, error) {
	in := DecodeRequest(JSONInput[T]())
	if !pointer {
		return in, in.err
	}
	return Input[*RequestOf[T]]{
		read: func(ctx context.Context, req *Request) (*RequestOf[T], error) {
			value, err := in.read(ctx, req)
			if err != nil {
				return nil, err
			}
			return &value, nil
		},
		contentType: in.contentType, contentTypes: in.contentTypes, schema: in.schema, err: in.err, required: in.required,
	}, in.err
}

func (RequestOf[T]) dataValidation(validate any, pointer bool) (any, error) {
	fn, ok := validate.(func(context.Context, T) error)
	if !ok || fn == nil {
		return nil, errors.New("ghttp/v3: data validator type mismatch or nil validator")
	}
	if pointer {
		return func(ctx context.Context, in *RequestOf[T]) error {
			data, err := in.Data(ctx)
			if err != nil {
				return err
			}
			return fn(ctx, data)
		}, nil
	}
	return func(ctx context.Context, in RequestOf[T]) error {
		data, err := in.Data(ctx)
		if err != nil {
			return err
		}
		return fn(ctx, data)
	}, nil
}

// DecodeRequest 将输入 codec 组合为统一 RequestOf 契约；包装不额外绑定 path/query/header/cookie 字段。
// DecodeRequest composes an input codec into a unified RequestOf contract without additional path/query/header/cookie binding by the wrapper.
func DecodeRequest[T any](codec Input[T]) Input[RequestOf[T]] {
	decode, err := compileBodyDecoder(codec)
	in := Input[RequestOf[T]]{
		contentType: codec.contentType, contentTypes: codec.ContentTypes(), schema: reflect.TypeFor[T](), err: err, required: codec.required,
		sourceBinding: codec.sourceBinding,
	}
	if err == nil {
		in.read = func(ctx context.Context, req *Request) (RequestOf[T], error) {
			if req == nil || req.Request == nil {
				return RequestOf[T]{}, errors.New("ghttp/v3: request input requires a request")
			}
			// 创建 lazy decoder
			return RequestOf[T]{
				RequestInput: RequestInput{Request: req},
				lazyData: &lazyData[T]{
					decoder: decode,
					req:     req,
				},
			}, nil
		}
	}
	return in
}

// ErrMissingBody 表示显式必需的请求体缺失或为空。
// ErrMissingBody indicates an explicitly required request body is absent or empty.
var ErrMissingBody = errors.New("ghttp/v3: missing request body")

// RequireBody 在解码前检查非空 body；不将 JSON null 视为缺失，也不缓存或重放请求体。
// RequireBody checks for a nonempty body before decoding; JSON null is not absence and bodies are not cached or replayed.
func RequireBody[T any](codec Input[T]) Input[T] {
	decode, err := compileBodyDecoder(codec)
	codec.err = err
	codec.required = true
	if codec.schema == nil && codec.read == nil {
		codec.schema = reflect.TypeFor[T]()
	}
	if err != nil {
		return codec
	}
	codec.read = func(ctx context.Context, req *Request) (T, error) {
		var zero T
		if req == nil || req.Request == nil || req.Body == nil || req.Body == http.NoBody {
			return zero, fmt.Errorf("%w: %w", root.ErrInvalidInput, ErrMissingBody)
		}
		// 检查一个字节后交还 decoder，避免读取或缓存整份 body。
		// Peek one byte and return it to the decoder without reading or buffering the entire body.
		var first [1]byte
		n, readErr := io.ReadFull(req.Body, first[:])
		if n == 0 && readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return zero, fmt.Errorf("%w: %w", root.ErrInvalidInput, ErrMissingBody)
			}
			return zero, readErr
		}
		req.Body = &prefixedBody{Reader: io.MultiReader(bytes.NewReader(first[:n]), req.Body), Closer: req.Body}
		return decode(ctx, req)
	}
	return codec
}

type prefixedBody struct {
	io.Reader
	io.Closer
}

// body 保留 codec 的 nil/null 语义，不使用根 DTO 输入的反射补分配路径。
// Bodies preserve codec nil/null semantics without the root DTO input's reflective allocation fallback.
func compileBodyDecoder[T any](codec Input[T]) (func(context.Context, *Request) (T, error), error) {
	if _, err := compileInput(codec); err != nil {
		return nil, err
	}
	if codec.read != nil {
		return codec.read, nil
	}
	return func(_ context.Context, req *Request) (T, error) {
		var value T
		err := codec.decoder.Decode(req, &value)
		return value, err
	}, nil
}

// ReadBody 显式解码当前请求体并归类输入错误；大小限制及 multipart 清理由端点执行器负责。
// ReadBody explicitly decodes the current body and classifies input errors; the endpoint owns size limits and multipart cleanup.
// 不缓存结果、不重放 body；同一请求应由一个所有者消费 body。
// Results are not cached and bodies are not replayed; one owner should consume each request body.
func ReadBody[T any](ctx context.Context, req RequestInput, codec Input[T]) (T, error) {
	var value T
	if req.Request == nil || req.Request.Request == nil {
		return value, fmt.Errorf("%w: missing request", root.ErrInvalidInput)
	}
	if codec.err != nil {
		return value, codec.err
	}
	if req.Body != nil && req.Body != http.NoBody && req.Header.Get("Content-Type") != "" && codec.ContentType() != "" {
		media, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		accepted := codec.ContentTypes()
		if len(accepted) == 0 {
			accepted = []string{codec.ContentType()}
		}
		match := false
		for _, ct := range accepted {
			match = match || strings.EqualFold(media, ct)
		}
		if err != nil || !match {
			return value, root.ErrUnsupportedMediaType
		}
	}
	var err error
	if codec.read != nil {
		value, err = codec.read(ctx, req.Request)
	} else if codec.decoder != nil {
		err = codec.decoder.Decode(req.Request, &value)
	} else {
		return value, ErrNilDecoder
	}
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			return value, fmt.Errorf("%w: %w", root.ErrRequestEntityTooLarge, err)
		}
		return value, fmt.Errorf("%w: %w", root.ErrInvalidInput, err)
	}
	return value, nil
}
