//go:build !go1.27

package ghttp

import "context"

// Handle 将 endpoint、输入、输出和业务逻辑编译为一等 Operation。
// Handle compiles an endpoint, input, output, and business logic into a first-class Operation.
func Handle[I, O any](builder *EndpointBuilder, input Input[I], output Output[O], handler func(context.Context, I) (O, error)) *Operation {
	return compileOperation(builder, input, output, handler)
}
