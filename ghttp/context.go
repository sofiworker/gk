package ghttp

import (
	"context"
	"net/http"
	"sync"
)

// Context 是 ghttp 的池化请求上下文；Context is ghttp's pooled request context.
// 仅允许在当前请求执行期间使用；it is valid only while the request is executing.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	Params  pathParamList
	Status  int
	Wrote   bool
}

// ContextHandler 处理一个池化请求上下文；ContextHandler handles a pooled request context.
type ContextHandler func(*Context) error

// ContextMiddleware 包装池化请求处理器；ContextMiddleware wraps a pooled request handler.
type ContextMiddleware func(ContextHandler) ContextHandler

var contextPool = sync.Pool{New: func() any { return new(Context) }}

func acquireContext(w http.ResponseWriter, r *http.Request, params pathParamList) *Context {
	c := contextPool.Get().(*Context)
	c.Writer = w
	c.Request = r
	c.Params = params
	c.Status = http.StatusOK
	c.Wrote = false
	return c
}

func releaseContext(c *Context) {
	if c == nil {
		return
	}
	c.Writer = nil
	c.Request = nil
	c.Params = pathParamList{}
	contextPool.Put(c)
}

// Context returns the request cancellation context.
func (c *Context) Context() context.Context { return c.Request.Context() }

// Header returns response headers.
func (c *Context) Header() http.Header { return c.Writer.Header() }

// WriteHeader commits the response status.
func (c *Context) WriteHeader(status int) {
	if c.Wrote {
		return
	}
	c.Wrote = true
	c.Status = status
	c.Writer.WriteHeader(status)
}

// Write writes response data and commits status 200 when needed.
func (c *Context) Write(data []byte) (int, error) {
	if !c.Wrote {
		c.WriteHeader(http.StatusOK)
	}
	return c.Writer.Write(data)
}

// Param returns a matched path parameter.
func (c *Context) Param(name string) string { return c.Params.Get(name) }

func chainContext(mws []ContextMiddleware, next ContextHandler) ContextHandler {
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] != nil {
			next = mws[i](next)
		}
	}
	return next
}
