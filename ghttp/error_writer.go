package ghttp

import "net/http"

// ErrorWriter 写 HTTP 错误响应；返回 true 表示已处理。
// ErrorWriter writes an HTTP error response; true means handled.
// 返回 false 时框架继续尝试下一个 writer 或内置错误 writer。
// false lets the framework fall through to the next writer.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, status int, err error) bool

// ChainErrorWriters 从外到内组合 writer，直到某个 writer 处理响应。
// ChainErrorWriters composes writers outermost-first until one handles it.
// 若全部未处理则返回 false，框架回退到默认错误 writer。
// if none handles it, the composed writer returns false.
func ChainErrorWriters(writers ...ErrorWriter) ErrorWriter {
	return func(w http.ResponseWriter, r *http.Request, status int, err error) bool {
		for _, writer := range writers {
			if writer == nil {
				continue
			}
			if writer(w, r, status, err) {
				return true
			}
		}
		return false
	}
}

func problemErrorWriter(s *Server) ErrorWriter {
	return func(w http.ResponseWriter, r *http.Request, status int, err error) bool {
		writeProblemDetails(w, r, s, status, err)
		return true
	}
}
