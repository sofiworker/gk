package v2

import (
	root "github.com/sofiworker/gk/ghttp"
	"net/http"
)

// Handler 是 middleware 包裹的端点执行契约；响应仍由框架统一处理。
// Handler is the endpoint execution contract wrapped by middleware; responses remain framework-managed.
type Handler = root.Handler

// HTTPError 为业务错误声明 HTTP 状态码，同时保留可供 errors.Is/As 检查的原因。
// HTTPError declares an HTTP status for a business error and preserves its cause for errors.Is/As.
type HTTPError struct {
	Status int
	Cause  error
}

func (e HTTPError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return http.StatusText(e.HTTPStatus())
}

// HTTPStatus 将无效错误状态映射为 500，不允许错误返回成功状态。
// HTTPStatus maps invalid error statuses to 500 and disallows success statuses for errors.
func (e HTTPError) HTTPStatus() int {
	if e.Status < 400 || e.Status > 599 {
		return 500
	}
	return e.Status
}

// Unwrap 保留业务错误原因。
// Unwrap preserves the underlying business error.
func (e HTTPError) Unwrap() error { return e.Cause }

// Recovery 将 panic 转交统一错误处理；已提交响应无法撤回。
// Recovery sends panics to the error pipeline; committed responses cannot be rolled back.
func Recovery() Middleware { return root.Recovery() }
