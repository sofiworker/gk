package v2

import "context"

// WithValidator 在解码后、handler 前校验输入；返回错误归类为无效输入并保留原始错误链。
// WithValidator validates decoded input before the handler, preserving the cause as an invalid-input error.
// 路由配置覆盖组默认值；输入类型必须完全一致，nil 校验器在注册时拒绝。
// Route configuration overrides group defaults; input types must match exactly and nil validators fail registration.
func WithValidator[I any](validate func(context.Context, I) error) Option {
	return func(c *routeOptions) { c.validator = validate }
}
