package v3

import "context"

// WithValidator 在解码后、handler 前校验输入；返回错误归类为无效输入并保留原始错误链。
// WithValidator validates decoded input before the handler, preserving the cause as an invalid-input error.
// 路由配置覆盖组默认值；输入类型必须完全一致，nil 校验器在注册时拒绝。
// Route configuration overrides group defaults; input types must match exactly and nil validators fail registration.
func WithValidator[I any](validate func(context.Context, I) error) Option {
	return func(c *routeOptions) { c.validator = validate }
}

// WithDataValidator 校验 RequestOf 的 Data，先于整个输入的 WithValidator 执行；与编码格式无关。
// WithDataValidator validates RequestOf.Data before the whole-input WithValidator runs, independently of encoding format.
func WithDataValidator[T any](validate func(context.Context, T) error) Option {
	return func(c *routeOptions) { c.dataValidator = validate }
}
