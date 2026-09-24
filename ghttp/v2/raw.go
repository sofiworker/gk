package v2

import (
	"context"
	"errors"
)

// Raw 注册自行解析请求和写出响应的 handler；返回错误交给统一错误管线。
// Raw registers a handler that parses requests and writes responses, returning errors to the common pipeline.
// 保留组/路由 middleware、Body 限制及上传清理；不进行自动绑定或输出编码。
// Group/route middleware, body limits and upload cleanup remain active without automatic binding or encoding.
// 组级 codec/validator/协商默认值不适用；路由显式配置这些选项会报注册错误。
// Group codec/validator/negotiation defaults do not apply; explicit route configuration of them fails registration.
func Raw(method, path string, handler func(context.Context, *Request, *Response) error, opts ...Option) Route {
	snapshot := append([]Option(nil), opts...)
	build := func(fullPath string, inherited []Option) Route {
		if handler == nil {
			return Route{Method: method, Path: fullPath, err: errors.New("ghttp/v2: nil raw handler")}
		}
		local := routeOptions{}
		for _, opt := range snapshot {
			if opt != nil {
				opt(&local)
			}
		}
		if local.inputSet || local.outputSet || local.validator != nil || local.negotiation != nil {
			return Route{Method: method, Path: fullPath, err: errors.New("ghttp/v2: raw routes do not accept codecs, validators or negotiation")}
		}
		all := append(append([]Option(nil), inherited...), snapshot...)
		all = append(all, func(c *routeOptions) {
			c.input = DecodeWith(func(context.Context, *Request) (struct{}, error) { return struct{}{}, nil })
			c.output = Output[struct{}]{configured: true}
			c.validator, c.negotiation = nil, nil
			c.rawHandler = handler
		})
		r := compileMethod(method, fullPath, func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }, all...)
		r.outputType = nil
		r.outputHasBody = true
		return r
	}
	r := build(path, nil)
	r.compile = build
	return r
}
