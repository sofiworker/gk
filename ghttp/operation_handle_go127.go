//go:build go1.27

package ghttp

import (
	"context"
	"net/http"
)

// Handle 绑定输入、输出与业务逻辑，并返回不可变 Operation。
// Handle binds input, output, and business logic and returns an immutable Operation.
// 输入和输出类型由契约及 handler 推断。
// Input and output types are inferred from the contracts and handler.
func (b *EndpointBuilder) Handle[I, O any](input Input[I], output Output[O], handler func(context.Context, I) (O, error)) *Operation {
	return compileOperation(b, input, output, handler)
}

// Handle 保留 Go 1.27 前的包级入口，便于跨版本迁移。
// Handle keeps the pre-Go-1.27 package-level entry point for cross-version migration.
func Handle[I, O any](builder *EndpointBuilder, input Input[I], output Output[O], handler func(context.Context, I) (O, error)) *Operation {
	return compileOperation(builder, input, output, handler)
}

// HandleHTTP 绑定输入与自行写响应的 HTTP handler，并返回不可变 Operation。
// HandleHTTP binds input and a response-writing HTTP handler and returns an immutable Operation.
func (b *EndpointBuilder) HandleHTTP[I any](input Input[I], handler func(http.ResponseWriter, *http.Request, I) error) *Operation {
	return HandleHTTP(b, input, handler)
}
