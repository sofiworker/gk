// Package v2 提供 ghttp 下一代门面的类型化公开契约。
// Package v2 contains the typed public contracts for the next ghttp facade.
package v2

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	root "github.com/sofiworker/gk/ghttp"
)

// Request 是 v1 请求上下文的别名，便于自定义解码器逐步接入 v2。
// Request aliases the v1 request context so custom decoders can adopt v2 incrementally.
type Request = root.Request

// RequestInput 是面向 handler 的增强请求视图，直接持有当前请求，不复制来源数据。
// RequestInput is an enhanced handler request view that holds the current request without copying source data.
type RequestInput struct{ *Request }

// Sources returns a zero-copy source view over the request.
// Sources 返回不复制请求数据的来源视图。
func (in RequestInput) Sources() Sources { return Sources{req: in.Request} }

// Sources exposes path, query, header and cookie values without materializing a DTO.
// Sources 按需暴露 path、query、header、cookie，不物化 DTO。
type Sources struct{ req *Request }

func (s Sources) Path(name string) string               { return s.req.Params.Get(name) }
func (s Sources) QueryFirst(name string) (string, bool) { return s.req.QueryFirst(name) }
func (s Sources) QueryValues(name string) []string      { return s.req.QueryValues(name) }
func (s Sources) Header(name string) string             { return s.req.Header.Get(name) }
func (s Sources) Cookie(name string) string {
	c, err := s.req.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func (in RequestInput) Path(name string) string               { return in.Params.Get(name) }
func (in RequestInput) QueryFirst(name string) (string, bool) { return in.Request.QueryFirst(name) }
func (in RequestInput) QueryValues(name string) []string      { return in.Request.QueryValues(name) }
func (in RequestInput) HeaderValue(name string) string        { return in.Header.Get(name) }

func (in RequestInput) QueryFirstValue(key string) string {
	value, _ := in.QueryFirst(key)
	return value
}
func (in RequestInput) CookieValue(key string) string {
	c, err := in.Cookie(key)
	if err != nil {
		return ""
	}
	return c.Value
}

// Response 是 v1 响应封装的别名，便于自定义编码器写出响应。
// Response aliases the v1 response wrapper for custom encoders.
type Response = root.Response

// Endpoint 描述一个有输入和输出的业务端点。
// Endpoint describes a business endpoint with typed input and output.
//
// I 和 O 可独立选择值或指针，四种组合共用此契约。
// I and O may independently be values or pointers. All four combinations are
// valid and are intentionally represented by this single contract.
type Endpoint[I, O any] func(context.Context, I) (O, error)

// Action 描述一个只产生错误结果的业务端点。
// Action describes a business endpoint with no response body.
type Action[I any] func(context.Context, I) error

// Procedure 描述一个既没有输入也没有响应体的业务端点。
// Procedure describes a business endpoint with neither input nor response body.
type Procedure func(context.Context) error

// Reply 携带响应体及 HTTP 响应元数据。
// Reply carries a response body and HTTP response metadata.
type Reply[T any] struct {
	Body    T
	Status  int
	Headers http.Header
	Cookies []*http.Cookie
}

// Decoder 是请求输入解码器。dst 始终指向可写入的 T。
// Decoder decodes a request into dst, which always points to writable T.
type Decoder[T any] interface {
	Decode(*Request, *T) error
}

// Encoder 是响应输出编码器。
// Encoder encodes a response value into the HTTP response.
type Encoder[T any] interface {
	Encode(*Response, T) error
}

// ContentTyper 是可选的 codec 元数据接口。
// ContentTyper is an optional codec metadata interface.
type ContentTyper interface {
	ContentType() string
}

// MultiContentTyper 是可选的多 Content-Type 元数据接口。
// MultiContentTyper optionally declares all accepted media types.
type MultiContentTyper interface {
	ContentTypes() []string
}

// ErrNilDecoder 表示输入契约没有解码器。
// ErrNilDecoder reports an input contract without a decoder.
var ErrNilDecoder = errors.New("ghttp/v2: nil decoder")

// ErrNilEncoder 表示输出契约没有编码器。
// ErrNilEncoder reports an output contract without an encoder.
var ErrNilEncoder = errors.New("ghttp/v2: nil encoder")

// Input 是注册期固定的输入契约。
// Input is an input contract fixed at registration time.
type Input[T any] struct {
	decoder      Decoder[T]
	read         func(context.Context, *Request) (T, error)
	contentType  string
	contentTypes []string
}

// CustomInput 使用用户提供的解码器创建输入契约。
// CustomInput creates an input contract from a user-provided decoder.
func CustomInput[T any](decoder Decoder[T]) Input[T] {
	if nilCodec(decoder) {
		return Input[T]{}
	}
	in := Input[T]{decoder: decoder}
	if typer, ok := any(decoder).(ContentTyper); ok {
		in.contentType = typer.ContentType()
	}
	if typer, ok := any(decoder).(MultiContentTyper); ok {
		in.contentTypes = append([]string(nil), typer.ContentTypes()...)
	}
	return in
}

// Decode 将请求解码到 dst。
// Decode decodes the request into dst.
func (in Input[T]) Decode(req *Request, dst *T) error {
	if in.read != nil {
		value, err := in.read(req.Context(), req)
		if err == nil {
			*dst = value
		}
		return err
	}
	if in.decoder == nil {
		return ErrNilDecoder
	}
	return in.decoder.Decode(req, dst)
}

// ContentType 返回输入契约声明的主 Content-Type。
// ContentType returns the primary media type declared by the input contract.
func (in Input[T]) ContentType() string { return in.contentType }

// ContentTypes 返回输入契约声明的可接受 Content-Type 集合。
// ContentTypes returns the accepted media types declared by the input contract.
func (in Input[T]) ContentTypes() []string {
	return append([]string(nil), in.contentTypes...)
}

// HasDecoder 报告输入契约是否包含解码器。
// HasDecoder reports whether the input contract contains a decoder.
func (in Input[T]) HasDecoder() bool { return in.decoder != nil || in.read != nil }

// Output 是注册期固定的输出契约。
// Output is an output contract fixed at registration time.
type Output[T any] struct {
	encoder     Encoder[T]
	contentType string
	status      int
	hasBody     bool
	configured  bool
}

// CustomOutput 使用用户提供的编码器创建输出契约。
// CustomOutput creates an output contract from a user-provided encoder.
func CustomOutput[T any](encoder Encoder[T]) Output[T] {
	if nilCodec(encoder) {
		return Output[T]{}
	}
	out := Output[T]{encoder: encoder, hasBody: true, configured: true}
	if typer, ok := any(encoder).(ContentTyper); ok {
		out.contentType = typer.ContentType()
	}
	return out
}

// Encode 将值编码到响应。
// Encode encodes a value into the response.
func (out Output[T]) Encode(resp *Response, value T) error {
	if out.status != 0 && (out.status < 200 || out.status > 599) {
		return errors.New("ghttp/v2: invalid output status")
	}
	if !out.configured {
		return ErrNilEncoder
	}
	if !out.hasBody || out.status == http.StatusNoContent || out.status == http.StatusNotModified {
		if out.status != 0 {
			resp.WriteHeader(out.status)
		}
		return nil
	}
	if out.encoder == nil {
		return ErrNilEncoder
	}
	if out.contentType != "" && resp.Header().Get("Content-Type") == "" {
		resp.Header().Set("Content-Type", out.contentType)
	}
	if out.status != 0 {
		resp.WriteHeader(out.status)
	}
	return out.encoder.Encode(resp, value)
}

// compileEncoder 将注册期已校验的输出契约固化为请求期执行函数。
// compileEncoder freezes a validated output contract into a request-time function.
func (out Output[T]) compileEncoder() func(*Response, T) error {
	encoder := out.encoder
	contentType, status := out.contentType, out.status
	if !out.hasBody || status == http.StatusNoContent || status == http.StatusNotModified {
		return func(resp *Response, _ T) error {
			if status != 0 {
				resp.WriteHeader(status)
			}
			return nil
		}
	}
	return func(resp *Response, value T) error {
		if contentType != "" && resp.Header().Get("Content-Type") == "" {
			resp.Header().Set("Content-Type", contentType)
		}
		if status != 0 {
			resp.WriteHeader(status)
		}
		return encoder.Encode(resp, value)
	}
}

// ContentType 返回输出契约声明的 Content-Type。
// ContentType returns the media type declared by the output contract.
func (out Output[T]) ContentType() string { return out.contentType }

// Status 返回输出契约声明的状态码，未声明时为零。
// Status returns the declared status code, or zero when unspecified.
func (out Output[T]) Status() int { return out.status }

// HasBody 报告输出契约是否包含响应体。
// HasBody reports whether the output contract contains a response body.
func (out Output[T]) HasBody() bool { return out.hasBody }

// WithStatus 返回使用指定状态码的输出契约副本。
// WithStatus returns a copy of the output contract with the given status code.
func (out Output[T]) WithStatus(code int) Output[T] {
	out.status = code
	return out
}

type builtinEncoder[T any] struct {
	contentType string
	encode      func(*Response, T) error
}

func (e builtinEncoder[T]) ContentType() string { return e.contentType }
func (e builtinEncoder[T]) Encode(resp *Response, value T) error {
	return e.encode(resp, value)
}

func newBuiltinOutput[T any](contentType string, encode func(*Response, T) error) Output[T] {
	return CustomOutput[T](builtinEncoder[T]{contentType: contentType, encode: encode})
}

func encodeJSON[T any](resp *Response, value T) error {
	if resp.Header().Get("Content-Type") == "" {
		resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	return json.NewEncoder(resp).Encode(value)
}

func encodeXML[T any](resp *Response, value T) error {
	if resp.Header().Get("Content-Type") == "" {
		resp.Header().Set("Content-Type", "application/xml; charset=utf-8")
	}
	return xml.NewEncoder(resp).Encode(value)
}

func encodeText[T any](resp *Response, value T) error {
	if resp.Header().Get("Content-Type") == "" {
		resp.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	switch v := any(value).(type) {
	case string:
		_, err := resp.Write([]byte(v))
		return err
	case []byte:
		_, err := resp.Write(v)
		return err
	default:
		_, err := fmt.Fprint(resp, value)
		return err
	}
}

// JSONInput 创建 JSON 请求输入契约。
// JSONInput creates a JSON request input contract.
func JSONInput[T any]() Input[T] {
	return CustomInput[T](jsonDecoder[T]{})
}

// StrictJSONInput 拒绝未知字段及多个 JSON 值；不改变标准库重复键语义。
// StrictJSONInput rejects unknown fields and multiple JSON values, retaining standard duplicate-key semantics.
func StrictJSONInput[T any]() Input[T] {
	return CustomInput[T](jsonDecoder[T]{strict: true})
}

// FormInput 创建表单请求输入契约。实际字段绑定由后续绑定层实现。
// FormInput creates a form request input contract; field binding is implemented by the binding layer.
func FormInput[T any]() Input[T] {
	plan, err := compileFormPlan(reflect.TypeFor[T]())
	return CustomInput[T](formDecoder[T]{plan: plan, err: err})
}

// XMLInput 创建 XML 请求输入契约。
// XMLInput creates an XML request input contract.
func XMLInput[T any]() Input[T] {
	return CustomInput[T](xmlDecoder[T]{})
}

// TextInput 创建纯文本请求输入契约。
// TextInput creates a plain-text request input contract.
func TextInput[T any]() Input[T] {
	return CustomInput[T](textDecoder[T]{})
}

// JSONOutput 创建 JSON 响应输出契约。
// JSONOutput creates a JSON response output contract.
func JSONOutput[T any]() Output[T] {
	if reflect.TypeFor[T]().Implements(reflect.TypeFor[interface{ encodeReply(*Response) error }]()) {
		return newBuiltinOutput[T]("", func(resp *Response, value T) error {
			return any(value).(interface{ encodeReply(*Response) error }).encodeReply(resp)
		})
	}
	return newBuiltinOutput[T]("application/json; charset=utf-8", encodeJSON[T])
}

// XMLOutput 创建 XML 响应输出契约。
// XMLOutput creates an XML response output contract.
func XMLOutput[T any]() Output[T] {
	return newBuiltinOutput[T]("application/xml; charset=utf-8", encodeXML[T])
}

// TextOutput 创建纯文本响应输出契约。
// TextOutput creates a plain-text response output contract.
func TextOutput[T any]() Output[T] {
	return newBuiltinOutput[T]("text/plain; charset=utf-8", encodeText[T])
}

// HTMLOutput 创建 HTML 响应输出契约。
// HTMLOutput creates an HTML response output contract.
func HTMLOutput[T any]() Output[T] {
	return newBuiltinOutput[T]("text/html; charset=utf-8", encodeText[T])
}

// EmptyOutput 创建无响应体的输出契约。
// EmptyOutput creates an output contract without a response body.
func EmptyOutput() Output[struct{}] {
	return Output[struct{}]{status: http.StatusNoContent, hasBody: false, configured: true}
}

type jsonDecoder[T any] struct{ strict bool }

func (jsonDecoder[T]) ContentType() string { return "application/json" }
func (codec jsonDecoder[T]) Decode(req *Request, dst *T) error {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	d := json.NewDecoder(req.Body)
	if codec.strict {
		d.DisallowUnknownFields()
	}
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("ghttp/v2: multiple JSON values")
	}
	return nil
}

type xmlDecoder[T any] struct{}

func (xmlDecoder[T]) ContentType() string               { return "application/xml" }
func (xmlDecoder[T]) Decode(req *Request, dst *T) error { return xml.NewDecoder(req.Body).Decode(dst) }

type formDecoder[T any] struct {
	plan formPlan
	err  error
}

func (formDecoder[T]) ContentType() string { return "application/x-www-form-urlencoded" }
func (d formDecoder[T]) Decode(req *Request, dst *T) error {
	if d.err != nil {
		return d.err
	}
	if req == nil || req.Request == nil {
		return errors.New("ghttp/v2: form decoder requires a request")
	}
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return fmt.Errorf("ghttp/v2: parse form: %w", err)
	}
	value := reflect.ValueOf(dst)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return errors.New("ghttp/v2: form decoder requires a non-nil destination")
	}
	value = value.Elem()
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return fmt.Errorf("ghttp/v2: form input requires a struct, got %s", value.Type())
	}
	if err := bindFormPlan(value, values, d.plan); err != nil {
		return err
	}
	return nil
}

func setFormValue(dst reflect.Value, raw []string) error {
	value := raw[0]
	switch dst.Kind() {
	case reflect.String:
		dst.SetString(value)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		dst.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, dst.Type().Bits())
		if err != nil {
			return err
		}
		dst.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(value, 10, dst.Type().Bits())
		if err != nil {
			return err
		}
		dst.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, dst.Type().Bits())
		if err != nil {
			return err
		}
		dst.SetFloat(parsed)
	case reflect.Slice:
		if dst.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice type %s", dst.Type())
		}
		result := reflect.MakeSlice(dst.Type(), len(raw), len(raw))
		for i, item := range raw {
			result.Index(i).SetString(item)
		}
		dst.Set(result)
	default:
		return fmt.Errorf("unsupported field type %s", dst.Type())
	}
	return nil
}

type textDecoder[T any] struct{}

func (textDecoder[T]) ContentType() string { return "text/plain" }
func (textDecoder[T]) Decode(req *Request, dst *T) error {
	var value any = dst
	switch target := value.(type) {
	case *string:
		b, err := io.ReadAll(req.Body)
		if err == nil {
			*target = string(b)
		}
		return err
	case *[]byte:
		b, err := io.ReadAll(req.Body)
		if err == nil {
			*target = b
		}
		return err
	default:
		return fmt.Errorf("ghttp/v2: text input requires string or []byte, got %T", dst)
	}
}

func nilCodec(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}
