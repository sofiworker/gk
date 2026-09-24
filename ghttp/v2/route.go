package v2

import (
	"context"
	"errors"
	"fmt"
	root "github.com/sofiworker/gk/ghttp"
	"mime"
	"net/http"
	"reflect"
	"strings"
)

// Route 保存端点描述；调用方负责路径匹配。
// Route describes an endpoint; callers own path matching.
type Route struct {
	Method        string
	Path          string
	err           error
	serve         func(context.Context, *Request, *Response) error
	compile       func(string, []Option) Route
	inputType     reflect.Type
	outputType    reflect.Type
	inputMedia    string
	outputMedia   string
	outputStatus  int
	negotiated    bool
	outputHasBody bool
	sourceBinding bool
}

// Serve 执行端点，返回错误交由外层统一处理。
// Serve executes the endpoint and leaves error rendering to the caller.
func (r Route) Serve(ctx context.Context, req *Request, resp *Response) error {
	if r.err != nil {
		return r.err
	}
	if r.serve == nil {
		return errors.New("ghttp/v2: uninitialized route")
	}
	return r.serve(ctx, req, resp)
}

type routeOptions struct {
	input          any
	output         any
	middleware     []root.Middleware
	maxBodyBytes   int64
	inputSet       bool
	outputSet      bool
	skipBodyLimit  bool
	skipMediaCheck bool
	skipCleanup    bool
	validator      any
	negotiation    any
	rawHandler     Handler
}

// Option 在注册时配置输入与输出，类型错误可通过 Route.Err 检查。
// Option configures codecs; type errors are available through Route.Err.
type Option func(*routeOptions)

// WithUnsafeFastPath 关闭框架侧 body 限制、媒体类型和 multipart 清理检查；使用者必须自行负责这些约束。本选项不使用 Go unsafe 包。
// WithUnsafeFastPath transfers body limits, media checks and multipart cleanup to callers without using Go unsafe.
func WithUnsafeFastPath() Option {
	return func(c *routeOptions) { c.skipBodyLimit = true; c.skipMediaCheck = true; c.skipCleanup = true }
}

// WithInput 选择请求解码器。
// WithInput selects a request decoder.
func WithInput[I any](in Input[I]) Option {
	return func(c *routeOptions) { c.input = in; c.inputSet = true }
}

// WithOutput 选择响应编码器。
// WithOutput selects a response encoder.
func WithOutput[O any](out Output[O]) Option {
	return func(c *routeOptions) { c.output = out; c.outputSet = true }
}

// Method 创建端点；默认 JSON 输入输出，不进行路由匹配。
// Method creates an endpoint with JSON defaults and no path matching.
func Method[I, O any](method, path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	snapshot := append([]Option(nil), opts...)
	build := func(fullPath string, inherited []Option) Route {
		all := append(append([]Option(nil), inherited...), snapshot...)
		return compileMethod(method, fullPath, h, all...)
	}
	r := build(path, nil)
	r.compile = build
	return r
}

func compileMethod[I, O any](method, path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	c := routeOptions{output: JSONOutput[O](), maxBodyBytes: 32 << 20}
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	if c.input == nil {
		in, err := defaultInput[I]()
		if err != nil {
			return Route{Method: method, Path: path, err: err}
		}
		c.input = in
	}
	input, inOK := c.input.(Input[I])
	output, outOK := c.output.(Output[O])
	route := Route{Method: method, Path: path}
	route.inputType, route.outputType = reflect.TypeFor[I](), reflect.TypeFor[O]()
	route.inputMedia, route.outputMedia = input.ContentType(), output.ContentType()
	route.outputStatus, route.negotiated = output.status, c.negotiation != nil
	route.outputHasBody = output.hasBody
	_, route.sourceBinding = input.decoder.(bindingDecoder[I])
	if input.read != nil {
		route.inputType = nil
	}
	if !inOK || !outOK || !input.HasDecoder() || !output.configured {
		route.err = errors.New("ghttp/v2: codec type mismatch")
		return route
	}
	if path == "" || !strings.HasPrefix(path, "/") || method == "" || strings.IndexFunc(method, func(r rune) bool {
		return !strings.ContainsRune("!#$%&'*+-.^_`|~0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", r)
	}) >= 0 {
		route.err = errors.New("ghttp/v2: invalid method or path")
		return route
	}
	if h == nil {
		route.err = errors.New("ghttp/v2: nil handler")
		return route
	}
	if c.maxBodyBytes <= 0 {
		route.err = errors.New("ghttp/v2: body limit must be positive")
		return route
	}
	for _, mw := range c.middleware {
		if mw == nil {
			route.err = errors.New("ghttp/v2: nil middleware")
			return route
		}
	}
	if output.status != 0 && (output.status < 200 || output.status > 599) {
		route.err = errors.New("ghttp/v2: invalid output status")
		return route
	}
	decode, err := compileInput(input)
	if err != nil {
		route.err = err
		return route
	}
	var validate func(context.Context, I) error
	if c.validator != nil {
		var ok bool
		validate, ok = c.validator.(func(context.Context, I) error)
		if !ok || validate == nil {
			route.err = errors.New("ghttp/v2: validator type mismatch or nil validator")
			return route
		}
	}
	encode := output.compileEncoder()
	choose, err := compileNegotiation[O](c.negotiation)
	if err != nil {
		route.err = err
		return route
	}
	accepted := input.ContentTypes()
	if len(accepted) == 0 && input.ContentType() != "" {
		accepted = []string{input.ContentType()}
	}
	// 自定义 decoder 和 handler 也可以解析 multipart，不能按声明的媒体类型推断清理需求。
	// Custom decoders and handlers may parse multipart regardless of declared media types.
	needsCleanup := !c.skipCleanup
	route.serve = func(ctx context.Context, req *Request, resp *Response) error {
		encodeResponse := encode
		if choose != nil {
			resp.Header().Add("Vary", "Accept")
			var err error
			encodeResponse, err = choose(req.Header.Values("Accept"))
			if err != nil {
				return err
			}
		}
		if needsCleanup {
			defer func() {
				if req.MultipartForm != nil {
					_ = req.MultipartForm.RemoveAll()
				}
			}()
		}
		if !c.skipBodyLimit && req.Body != nil && req.Body != http.NoBody {
			req.Body = http.MaxBytesReader(resp, req.Body, c.maxBodyBytes)
		}
		if !c.skipMediaCheck && req.Body != nil && req.Body != http.NoBody && req.Header.Get("Content-Type") != "" && input.ContentType() != "" {
			media, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
			match := false
			for _, ct := range accepted {
				if strings.EqualFold(ct, media) {
					match = true
				}
			}
			if err != nil || !match {
				return root.ErrUnsupportedMediaType
			}
		}
		if c.rawHandler != nil {
			if req.Method == http.MethodHead {
				original := resp.ResponseWriter
				resp.ResponseWriter = headWriter{original}
				defer func() { resp.ResponseWriter = original }()
			}
			return c.rawHandler(ctx, req, resp)
		}
		in, err := decode(ctx, req)
		if err != nil {
			var size *http.MaxBytesError
			if errors.As(err, &size) {
				return fmt.Errorf("%w: %w", root.ErrRequestEntityTooLarge, err)
			}
			return fmt.Errorf("%w: %w", root.ErrInvalidInput, err)
		}
		if validate != nil {
			if err := validate(ctx, in); err != nil {
				return fmt.Errorf("%w: %w", root.ErrInvalidInput, err)
			}
		}
		out, err := h(ctx, in)
		if err != nil {
			var size *http.MaxBytesError
			if errors.As(err, &size) {
				return fmt.Errorf("%w: %w", root.ErrRequestEntityTooLarge, err)
			}
			return err
		}
		if method == http.MethodHead {
			original := resp.ResponseWriter
			resp.ResponseWriter = headWriter{original}
			defer func() { resp.ResponseWriter = original }()
		}
		return encodeResponse(resp, out)
	}
	for i := len(c.middleware) - 1; i >= 0; i-- {
		route.serve = c.middleware[i](root.Handler(route.serve))
	}
	return route
}

// WithMiddleware 将中间件绑定到该端点。
// WithMiddleware attaches middleware to this endpoint.
func WithMiddleware(middleware ...root.Middleware) Option {
	snapshot := append([]root.Middleware(nil), middleware...)
	return func(c *routeOptions) { c.middleware = append(c.middleware, snapshot...) }
}

// Get 创建 GET 端点。
// Get creates a GET endpoint.
func Get[I, O any](path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	return Method(http.MethodGet, path, h, opts...)
}

// Post 创建 POST 端点。
// Post creates a POST endpoint.
func Post[I, O any](path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	return Method(http.MethodPost, path, h, opts...)
}

// Put 创建 PUT 端点。
// Put creates a PUT endpoint.
func Put[I, O any](path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	return Method(http.MethodPut, path, h, opts...)
}

// Patch 创建 PATCH 端点。
// Patch creates a PATCH endpoint.
func Patch[I, O any](path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	return Method(http.MethodPatch, path, h, opts...)
}

// Delete 创建 DELETE 端点。
// Delete creates a DELETE endpoint.
func Delete[I, O any](path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	return Method(http.MethodDelete, path, h, opts...)
}

// Head 创建 HEAD 端点。
// Head creates a HEAD endpoint.
func Head[I, O any](path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	return Method(http.MethodHead, path, h, opts...)
}

// Options 创建 OPTIONS 端点。
// Options creates a OPTIONS endpoint.
func Options[I, O any](path string, h func(context.Context, I) (O, error), opts ...Option) Route {
	return Method(http.MethodOptions, path, h, opts...)
}

// Err 返回注册期间发现的配置错误。
// Err returns configuration errors discovered during registration.
func (r Route) Err() error { return r.err }

type headWriter struct{ http.ResponseWriter }

func (w headWriter) Write(p []byte) (int, error) { return len(p), nil }

// WithBodyLimit 设置端点请求体字节上限，必须为正数。
// WithBodyLimit sets a positive endpoint request body limit in bytes.
func WithBodyLimit(bytes int64) Option {
	return func(c *routeOptions) { c.maxBodyBytes = bytes; c.skipBodyLimit = false }
}

// WithoutBodyLimit 将端点大小限制责任移交调用方；codec 自身限制仍生效。
// WithoutBodyLimit transfers endpoint size enforcement to the caller; codec limits still apply.
func WithoutBodyLimit() Option { return func(c *routeOptions) { c.skipBodyLimit = true } }

// WithoutContentTypeCheck 关闭框架媒体类型检查，不关闭 decoder 的格式检查。
// WithoutContentTypeCheck skips framework media checks, retaining decoder format checks.
func WithoutContentTypeCheck() Option { return func(c *routeOptions) { c.skipMediaCheck = true } }

// WithoutMultipartCleanup 将上传临时文件清理责任移交调用方。
// WithoutMultipartCleanup transfers multipart temporary-file cleanup to the caller.
func WithoutMultipartCleanup() Option { return func(c *routeOptions) { c.skipCleanup = true } }
