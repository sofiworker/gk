package ghttp

import (
	"context"
	"time"

	"github.com/sofiworker/gk/ghttp/wire"
)

// RequestInfo 是 Observe 回调收到的请求观测数据。
// RequestInfo is the observation data handed to the Observe callback.
type RequestInfo struct {
	// Method 是 HTTP 方法（已净化）。
	// Method is the HTTP method (sanitized).
	Method string
	// Route 是匹配到的路由模板，未匹配为空。指标应按 Route 聚合，不要按 Path，避免基数爆炸。
	// Route is the matched route template, empty if unmatched. Aggregate metrics by Route,
	// not Path, to avoid cardinality explosion.
	Route string
	// Path 是请求路径（已净化）。
	// Path is the request path (sanitized).
	Path string
	// Status 是最终响应状态码。
	// Status is the final response status.
	Status int
	// Latency 是处理耗时。
	// Latency is the processing latency.
	Latency time.Duration
	// BytesWritten 是已写出的响应体字节数。
	// BytesWritten is the number of response body bytes written.
	BytesWritten int64
	// Err 是 next 返回的错误。
	// Err is the error returned by next.
	Err error
}

// FinalStatus 计算中间件视角下请求的最终状态码：next 返回错误且响应未写出时，状态由外层
// 错误处理器写出，此时按 StatusFromError 推断；否则取已写出的状态码。日志、指标、追踪中间件
// 应使用它，而不是直接读 resp.StatusCode()（后者在出错时仍是 200）。
// FinalStatus computes a request's final status from a middleware's point of view: when
// next errored and nothing was written, the outer error handler will write it, so it is
// inferred via StatusFromError; otherwise the written status is used. Logging, metrics and
// tracing middleware should use it rather than resp.StatusCode() (still 200 on error).
func FinalStatus(resp *Response, err error) int {
	if err != nil && !resp.Written() {
		return StatusFromError(err)
	}
	return resp.StatusCode()
}

// Observe 返回在每个请求结束后调用 fn 的观测中间件；fn 为 nil 时透传。
// Observe returns a middleware calling fn after every request; a nil fn passes through.
func Observe(fn func(RequestInfo)) Middleware {
	return func(next Handler) Handler {
		if fn == nil {
			return next
		}
		return func(ctx context.Context, req *Request, resp *Response) error {
			start := time.Now()
			err := next(ctx, req, resp)
			fn(RequestInfo{
				Method:       wire.LogToken(req.Raw.Method),
				Route:        req.Route(),
				Path:         wire.LogToken(req.Raw.URL.Path),
				Status:       FinalStatus(resp, err),
				Latency:      time.Since(start),
				BytesWritten: resp.Size(),
				Err:          err,
			})
			return err
		}
	}
}
