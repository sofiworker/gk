// middleware.go - 增强中间件系统

package ghttp

import (
	"time"
)

// MiddlewareFunc 中间件函数类型
type MiddlewareFunc func(Handler) Handler

// LoggingMiddleware 日志中间件
func LoggingMiddleware() MiddlewareFunc {
	return func(next Handler) Handler {
		return func(req *Request, resp *Response) error {
			// start := time.Now()

			err := next(req, resp)

			// duration := time.Since(start)
			// 记录日志
			if err != nil {
				// 记录错误日志
			} else {
				// 记录成功日志
			}

			return err
		}
	}
}

// RetryMiddleware 重试中间件
func RetryMiddleware(config RetryConfig) MiddlewareFunc {
	return func(next Handler) Handler {
		return func(req *Request, resp *Response) error {
			var lastErr error

			for i := 0; i <= config.MaxRetries; i++ {
				err := next(req, resp)
				lastErr = err

				if err == nil {
					return nil
				}

				// 检查重试条件
				shouldRetry := false
				for _, condition := range config.RetryConditions {
					if condition(resp, err) {
						shouldRetry = true
						break
					}
				}

				if !shouldRetry || i >= config.MaxRetries {
					break
				}

				// 延迟重试
				if config.Backoff != nil {
					time.Sleep(config.Backoff(i))
				}
			}

			return lastErr
		}
	}
}

// TimeoutMiddleware 超时中间件
func TimeoutMiddleware(timeout time.Duration) MiddlewareFunc {
	return func(next Handler) Handler {
		return func(req *Request, resp *Response) error {
			// 设置请求超时
			if req.timeout == 0 {
				req.timeout = timeout
			}
			return next(req, resp)
		}
	}
}
