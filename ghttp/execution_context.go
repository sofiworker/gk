package ghttp

import (
	"mime/multipart"
	"net/http"
	"net/url"
	"sync"
)

// executionContext stores all mutable per-request state and is recycled via a
// pool. Handlers must not retain references after ServeHTTP returns.
// executionContext 保存请求期间的可变状态并通过池复用；处理结束后禁止保留引用。
type executionContext struct {
	server        *Server
	responseState *responseWriteState
	req           *http.Request
	matched       *lazyPathParams
	body          *memoBody

	formMu     sync.Mutex
	formParsed bool
	form       url.Values
	postForm   url.Values
	formErr    error

	multipartMu     sync.Mutex
	multipartParsed bool
	multipart       *multipart.Form
	multipartErr    error

	paramsMu    sync.Mutex
	paramsBuilt bool
	params      paramsState
}

func (c *executionContext) reset(server *Server, responseState *responseWriteState) {
	c.server = server
	c.responseState = responseState
	c.req = nil
	c.matched = nil
	c.body = nil
	c.formMu = sync.Mutex{}
	c.formParsed = false
	c.form = nil
	c.postForm = nil
	c.formErr = nil
	c.multipartMu = sync.Mutex{}
	c.multipartParsed = false
	c.multipart = nil
	c.multipartErr = nil
	c.paramsMu = sync.Mutex{}
	c.paramsBuilt = false
	c.params = paramsState{}
}

func (c *executionContext) clear() {
	c.server = nil
	c.responseState = nil
	c.req = nil
	c.matched = nil
	c.body = nil
	c.form = nil
	c.postForm = nil
	c.formErr = nil
	c.multipart = nil
	c.multipartErr = nil
	c.params = paramsState{}
}

var executionContextPool = sync.Pool{New: func() any { return new(executionContext) }}

func acquireExecutionContext(server *Server, responseState *responseWriteState) *executionContext {
	c := executionContextPool.Get().(*executionContext)
	c.reset(server, responseState)
	return c
}

func releaseExecutionContext(c *executionContext) {
	if c == nil {
		return
	}
	if c.responseState != nil && c.responseState.hijacked {
		return
	}
	rs := c.responseState
	c.clear()
	releaseResponseWriteState(rs)
	executionContextPool.Put(c)
}
