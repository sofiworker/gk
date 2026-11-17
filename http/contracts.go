package ghttp

import (
	"context"
)

// BodyConstraint 表示可用作 HTTP Body 的类型。
// Body constraint represents types that can be used as HTTP Body.
//
// 虽然定义为 any，但实际支持的类型为：
// Although defined as any, the actual supported types are:
//   - 结构体：User / Struct: User
//   - 结构体指针：*User / Struct pointer: *User
//   - NoData（表示无 Body） / NoData (indicates no Body)
//
// 类型合法性在路由注册期通过反射检查。
// Type validity is checked via reflection during route registration.
//
// 注：Go 泛型约束无法表达"结构体或结构体指针"，因此使用 any，
// Note: Go generic constraints cannot express "struct or struct pointer", so we use any,
// 但保留接口名称以表达设计意图。
// but keep the interface name to express design intent.
type BodyConstraint = any

// ResponseConstraint 表示可作为 HTTP 响应的类型。
// Response constraint represents types that can be used as HTTP response.
//
// 虽然定义为 any，但实际支持的类型为：
// Although defined as any, the actual supported types are:
//   - 基本类型：string, int, int64, bool, float64 等 / Basic types: string, int, int64, bool, float64, etc.
//   - 结构体：User / Struct: User
//   - 结构体指针：*User / Struct pointer: *User
//   - 切片：[]User, []*User / Slice: []User, []*User
//   - Map：map[string]any / Map: map[string]any
//
// 类型必须可 JSON 序列化。
// The type must be JSON serializable.
type ResponseConstraint = any

// Endpoint 描述有输入和输出的业务端点。
// Endpoint describes a business endpoint with input and output.
//
// T 为 Body 类型（默认 JSON 解码），O 为返回值类型（默认 JSON 序列化）。
// T is the Body type (default JSON decode), O is the return type (default JSON serialize).
type Endpoint[T BodyConstraint, O ResponseConstraint] func(context.Context, RequestOf[T]) (O, error)

// Action 描述有输入但无返回值的端点（返回空或状态码）。
// Action describes an endpoint with input but no return value (returns empty or status code).
type Action[T BodyConstraint] func(context.Context, RequestOf[T]) error

// Procedure 描述无输入无返回值的端点。
// Procedure describes an endpoint with no input and no return value.
type Procedure func(context.Context) error

// Handler 是统一的 HTTP 处理器签名，接收原始请求和响应。
// Handler is the unified HTTP handler signature, receiving raw request and response.
//
// 所有 typed handler（Endpoint、Action）最终编译为此签名。
// All typed handlers (Endpoint, Action) are eventually compiled to this signature.
type Handler func(context.Context, *Request, *Response) error

// Middleware 是中间件函数，接收下一个 handler 并返回包装后的 handler。
// Middleware is a middleware function that receives the next handler and returns a wrapped handler.
type Middleware func(next Handler) Handler

// RawHandlerFunc 是完全接管响应的原始处理器形态。
// RawHandlerFunc is the raw handler form that completely takes over the response.
//
// 用于 WebSocket、SSE、文件流等场景。
// Used for WebSocket, SSE, file streaming, etc.
type RawHandlerFunc func(context.Context, *Request, *Response) error

// NoDataType 是 NoData 的底层类型。
// NoDataType is the underlying type of NoData.
type NoDataType struct{}

// NoData 明确表示端点不消费请求体。
// NoData explicitly indicates that the endpoint does not consume the request body.
//
// 用于 GET、DELETE 等不需要 Body 的请求。
// Used for GET, DELETE, and other requests that don't need a Body.
var NoData = NoDataType{}
