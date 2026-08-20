package ghttp

import (
	"net/http"
)

// 响应错误写入状态追踪:原 responseWriteState 已合并进 Ctx
// (committed/errorHandled/suppressBody)。以下函数保留签名,内部改从
// ctxFromRequest 读取 Ctx。
// Response error-write state tracking: the old responseWriteState has been
// merged into Ctx (committed/errorHandled/suppressBody). The functions below
// keep their signatures and read the Ctx via ctxFromRequest.

func responseErrorWriteBlocked(r *http.Request) bool {
	if r == nil {
		return false
	}
	c := ctxFromRequest(r)
	if c == nil {
		return false
	}
	return c.committed
}

func responseErrorHandlerStarted(r *http.Request) bool {
	if r == nil {
		return false
	}
	c := ctxFromRequest(r)
	if c == nil {
		return false
	}
	return c.errorHandled
}

func beginResponseErrorHandler(r *http.Request) bool {
	if r == nil {
		return true
	}
	c := ctxFromRequest(r)
	if c == nil {
		return true
	}
	if c.errorHandled {
		return false
	}
	c.errorHandled = true
	return true
}
