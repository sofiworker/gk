package v4

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	ghttp "github.com/sofiworker/gk/ghttp"
)

// Route 表示一个待注册的路由。
// Route represents a route to be registered.
type Route[In, Out any] struct {
	method  string
	path    string
	handler Handler[In, Out]
	wrapper func(*Request) In
}

// MustRegister 注册路由到服务器，失败时 panic。
// MustRegister registers the route to the server, panics on error.
func (r Route[In, Out]) MustRegister(server interface {
	RawHandle(method, path string, handler ghttp.RawHandlerFunc) error
}) {
	if err := r.Register(server); err != nil {
		panic(fmt.Sprintf("failed to register route %s %s: %v", r.method, r.path, err))
	}
}

// Register 注册路由到服务器。
// Register registers the route to the server.
func (r Route[In, Out]) Register(server interface {
	RawHandle(method, path string, handler ghttp.RawHandlerFunc) error
}) error {
	// 编译为 ghttp.RawHandlerFunc
	// Compile to ghttp.RawHandlerFunc
	compiledHandler := r.compile()

	return server.RawHandle(r.method, r.path, compiledHandler)
}

// compile 将 v4 Handler 编译为 ghttp.RawHandlerFunc。
// compile compiles a v4 Handler to ghttp.RawHandlerFunc.
func (r Route[In, Out]) compile() ghttp.RawHandlerFunc {
	return func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		// 1. 包装请求
		// 1. Wrap request
		input := r.wrapper(req)

		// 2. 调用业务 handler
		// 2. Call business handler
		output, err := r.handler(ctx, input)
		if err != nil {
			// 处理错误
			// Handle error
			if httpErr, ok := err.(HTTPError); ok {
				return writeJSON(resp, httpErr.Status, map[string]string{
					"error": httpErr.Message,
				})
			}
			return writeJSON(resp, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
		}

		// 3. 序列化响应
		// 3. Serialize response
		return writeJSON(resp, http.StatusOK, output)
	}
}

// writeJSON 写入 JSON 响应。
// writeJSON writes a JSON response.
func writeJSON(resp *ghttp.Response, status int, data any) error {
	resp.Header().Set("Content-Type", "application/json")
	resp.WriteHeader(status)
	return json.NewEncoder(resp).Encode(data)
}
