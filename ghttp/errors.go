package ghttp

import (
	"fmt"
	"net/http"

	"github.com/sofiworker/gk/ghttp/wire"
)

// HTTPError 表示 HTTP 错误，包含状态码、消息和底层原因。
// HTTPError represents an HTTP error with status code, message, and underlying cause.
type HTTPError struct {
	// Status 是 HTTP 状态码。
	// Status is the HTTP status code.
	Status int

	// Message 是面向客户端的错误消息。
	// Message is the client-facing error message.
	Message string

	// Cause 是底层错误原因（内部使用，不直接暴露给客户端）。
	// Cause is the underlying error cause (internal use, not directly exposed to client).
	Cause error
}

// Error 实现 error 接口。
// Error implements the error interface.
func (e HTTPError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("HTTP %d: %s (cause: %v)", e.Status, e.Message, e.Cause)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
}

// Unwrap 实现错误链，支持 errors.Is 和 errors.Unwrap。
// Unwrap implements error chaining, supporting errors.Is and errors.Unwrap.
func (e HTTPError) Unwrap() error {
	return e.Cause
}

// Is 让 HTTPError 按状态码参与 errors.Is：errors.Is(err, ErrNotFound) 对链上任意 404 HTTPError 成立，
// 与 Message、Cause 无关。
// Is makes HTTPError match by status in errors.Is: errors.Is(err, ErrNotFound) holds for any
// 404 HTTPError in the chain, regardless of Message and Cause.
func (e HTTPError) Is(target error) bool {
	t, ok := target.(HTTPError)
	return ok && t.Status == e.Status
}

// HTTPStatus 实现 StatusCoder，返回 HTTP 状态码。
// HTTPStatus implements StatusCoder and returns the HTTP status code.
func (e HTTPError) HTTPStatus() int {
	return e.Status
}

// StatusCoder 是携带 HTTP 状态码的错误契约，与 wire.StatusCoder 及 client.StatusCoder 是同一类型。
// StatusCoder is the contract for errors carrying an HTTP status; it is the same type as
// wire.StatusCoder and client.StatusCoder.
type StatusCoder = wire.StatusCoder

// 编译期断言 HTTPError 满足状态码契约。
// Compile-time assertion that HTTPError satisfies the status contract.
var _ StatusCoder = HTTPError{}

// 预定义的常见 HTTP 错误。
// Predefined common HTTP errors.
var (
	// ErrBadRequest 表示 400 Bad Request。
	// ErrBadRequest represents 400 Bad Request.
	ErrBadRequest = HTTPError{
		Status:  http.StatusBadRequest,
		Message: "bad request",
	}

	// ErrInvalidInput 表示输入非法（参数缺失、类型转换失败、请求体格式错误），映射为 400。
	// 业务代码可用 fmt.Errorf("%w: ...", ErrInvalidInput) 包装，响应中只返回本错误的 Message。
	// ErrInvalidInput marks invalid input (missing parameter, conversion failure, malformed
	// body) and maps to 400. Wrap it with fmt.Errorf("%w: ...", ErrInvalidInput); only this
	// error's Message is sent to the client.
	ErrInvalidInput = HTTPError{
		Status:  http.StatusBadRequest,
		Message: "invalid input",
	}

	// ErrUnauthorized 表示 401 Unauthorized。
	// ErrUnauthorized represents 401 Unauthorized.
	ErrUnauthorized = HTTPError{
		Status:  http.StatusUnauthorized,
		Message: "unauthorized",
	}

	// ErrForbidden 表示 403 Forbidden。
	// ErrForbidden represents 403 Forbidden.
	ErrForbidden = HTTPError{
		Status:  http.StatusForbidden,
		Message: "forbidden",
	}

	// ErrNotFound 表示 404 Not Found。
	// ErrNotFound represents 404 Not Found.
	ErrNotFound = HTTPError{
		Status:  http.StatusNotFound,
		Message: "not found",
	}

	// ErrMethodNotAllowed 表示 405 Method Not Allowed。
	// ErrMethodNotAllowed represents 405 Method Not Allowed.
	ErrMethodNotAllowed = HTTPError{
		Status:  http.StatusMethodNotAllowed,
		Message: "method not allowed",
	}

	// ErrRequestTimeout 表示 408 Request Timeout。
	// ErrRequestTimeout represents 408 Request Timeout.
	ErrRequestTimeout = HTTPError{
		Status:  http.StatusRequestTimeout,
		Message: "request timeout",
	}

	// ErrRequestEntityTooLarge 表示 413 Request Entity Too Large。
	// ErrRequestEntityTooLarge represents 413 Request Entity Too Large.
	ErrRequestEntityTooLarge = HTTPError{
		Status:  http.StatusRequestEntityTooLarge,
		Message: "request entity too large",
	}

	// ErrUnsupportedMediaType 表示 415 Unsupported Media Type。
	// ErrUnsupportedMediaType represents 415 Unsupported Media Type.
	ErrUnsupportedMediaType = HTTPError{
		Status:  http.StatusUnsupportedMediaType,
		Message: "unsupported media type",
	}

	// ErrInternalServerError 表示 500 Internal Server Error。
	// ErrInternalServerError represents 500 Internal Server Error.
	ErrInternalServerError = HTTPError{
		Status:  http.StatusInternalServerError,
		Message: "internal server error",
	}

	// ErrServiceUnavailable 表示 503 Service Unavailable。
	// ErrServiceUnavailable represents 503 Service Unavailable.
	ErrServiceUnavailable = HTTPError{
		Status:  http.StatusServiceUnavailable,
		Message: "service unavailable",
	}
)

// BadRequest 创建 400 Bad Request 错误。
// BadRequest creates a 400 Bad Request error.
func BadRequest(msg string) HTTPError {
	return HTTPError{
		Status:  http.StatusBadRequest,
		Message: msg,
	}
}

// Unauthorized 创建 401 Unauthorized 错误。
// Unauthorized creates a 401 Unauthorized error.
func Unauthorized(msg string) HTTPError {
	return HTTPError{
		Status:  http.StatusUnauthorized,
		Message: msg,
	}
}

// Forbidden 创建 403 Forbidden 错误。
// Forbidden creates a 403 Forbidden error.
func Forbidden(msg string) HTTPError {
	return HTTPError{
		Status:  http.StatusForbidden,
		Message: msg,
	}
}

// NotFound 创建 404 Not Found 错误。
// NotFound creates a 404 Not Found error.
func NotFound(msg string) HTTPError {
	return HTTPError{
		Status:  http.StatusNotFound,
		Message: msg,
	}
}

// InternalServerError 创建 500 Internal Server Error 错误。
// InternalServerError creates a 500 Internal Server Error error.
func InternalServerError(msg string) HTTPError {
	return HTTPError{
		Status:  http.StatusInternalServerError,
		Message: msg,
	}
}

// ErrorResponse 是错误的 JSON 响应格式。
// ErrorResponse is the JSON response format for errors.
type ErrorResponse struct {
	// Error 是错误消息。
	// Error is the error message.
	Error string `json:"error"`

	// Status 是 HTTP 状态码。
	// Status is the HTTP status code.
	Status int `json:"status,omitempty"`

	// Code 是业务错误码（可选）。
	// Code is the business error code (optional).
	Code string `json:"code,omitempty"`
}

// ToErrorResponse 将 HTTPError 转换为 ErrorResponse。
// ToErrorResponse converts HTTPError to ErrorResponse.
func (e HTTPError) ToErrorResponse() ErrorResponse {
	return ErrorResponse{
		Error:  e.Message,
		Status: e.Status,
	}
}

// WriteErrorJSON 将错误以 JSON 格式写入响应。
// WriteErrorJSON writes the error to the response in JSON format.
func WriteErrorJSON(resp *Response, err HTTPError) error {
	return WriteJSON(resp, err.Status, err.ToErrorResponse())
}
