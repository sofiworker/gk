package v2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	root "github.com/sofiworker/gk/ghttp"
)

// FromFunc 将无输入函数适配为 Route；输出可为普通值或 Reply。
// FromFunc adapts a no-input function into a Route; its output may be a value or Reply.
func FromFunc[O any](method, path string, fn func(context.Context) (O, error), opts ...Option) Route {
	if fn == nil {
		return Route{Method: method, Path: path, err: errors.New("ghttp/v2: nil handler")}
	}
	options := append([]Option{}, opts...)
	options = append(options, WithInput(CustomInput[struct{}](noInputDecoder{})))
	return Method(method, path, func(ctx context.Context, _ struct{}) (O, error) { return fn(ctx) }, options...)
}

// FromAction 将带输入且仅返回 error 的函数适配为无响应体 Route。
// FromAction adapts an input action that only returns error into a no-body Route.
func FromAction[I any](method, path string, fn func(context.Context, I) error, opts ...Option) Route {
	if fn == nil {
		return Route{Method: method, Path: path, err: errors.New("ghttp/v2: nil handler")}
	}
	options := append([]Option{WithOutput(EmptyOutput())}, opts...)
	return Method(method, path, func(ctx context.Context, in I) (struct{}, error) { return struct{}{}, fn(ctx, in) }, options...)
}

// FromProcedure 将无输入且仅返回 error 的函数适配为无响应体 Route。
// FromProcedure adapts a no-input error-only function into a no-body Route.
func FromProcedure(method, path string, fn func(context.Context) error, opts ...Option) Route {
	if fn == nil {
		return Route{Method: method, Path: path, err: errors.New("ghttp/v2: nil handler")}
	}
	options := append([]Option{WithOutput(EmptyOutput())}, opts...)
	return FromFunc(method, path, func(ctx context.Context) (struct{}, error) { return struct{}{}, fn(ctx) }, options...)
}

// Mount 将 Route 挂载到 root ghttp.Server 或 Group 的 RawHandle。
// Mount attaches a Route to a root ghttp Server or Group through RawHandle.
func (r Route) Mount(target any) error {
	if r.err != nil {
		return r.err
	}
	if registrar, ok := target.(interface{ registerRoute(Route) error }); ok {
		return registrar.registerRoute(r)
	}
	registrar, ok := target.(interface {
		RawHandle(string, string, root.RawHandlerFunc) error
	})
	if !ok {
		return errors.New("ghttp/v2: mount target does not implement RawHandle")
	}
	return registrar.RawHandle(r.Method, r.Path, root.RawHandlerFunc(func(ctx context.Context, req *Request, resp *Response) error { return r.Serve(ctx, req, resp) }))
}

type noInputDecoder struct{}

func (noInputDecoder) Decode(_ *Request, _ *struct{}) error { return nil }

// ReplyOutput 用 typed body 输出编码器创建 Reply 输出契约。
// ReplyOutput creates a Reply output contract backed by a typed body encoder.
func ReplyOutput[T any](body Output[T]) Output[Reply[T]] {
	return CustomOutput[Reply[T]](replyEncoder[T]{body: body})
}

type replyEncoder[T any] struct{ body Output[T] }

func (e replyEncoder[T]) ContentType() string { return e.body.ContentType() }
func (e replyEncoder[T]) Encode(resp *Response, reply Reply[T]) error {
	if err := reply.apply(resp); err != nil {
		return err
	}
	if reply.Status == http.StatusNoContent || reply.Status == http.StatusNotModified {
		return nil
	}
	return e.body.Encode(resp, reply.Body)
}

// encodeReply 展开 Reply 元数据并编码默认 JSON body。
// encodeReply applies Reply metadata and encodes the body as default JSON.
func (r Reply[T]) encodeReply(resp *Response) error {
	if resp.Header().Get("Content-Type") == "" {
		resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	if err := r.apply(resp); err != nil {
		return err
	}
	if r.Status == http.StatusNoContent || r.Status == http.StatusNotModified {
		return nil
	}
	return json.NewEncoder(resp).Encode(r.Body)
}
func (r Reply[T]) apply(resp *Response) error {
	for key, values := range r.Headers {
		resp.Header()[key] = append([]string(nil), values...)
	}
	for _, cookie := range r.Cookies {
		if cookie != nil {
			http.SetCookie(resp, cookie)
		}
	}
	if r.Status != 0 && (r.Status < 200 || r.Status > 599) {
		return errors.New("ghttp/v2: invalid reply status")
	}
	if r.Status != 0 {
		resp.WriteHeader(r.Status)
	}
	return nil
}
