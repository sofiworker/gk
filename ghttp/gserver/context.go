package gserver

import (
	"net/http"
	"net/url"
	"sync"
)

const (
	defaultMultipartMemory = 32 << 20 // 32MB
	headerContentType      = "Content-Type"
	headerXForwardedFor    = "X-Forwarded-For"
	headerXRealIP          = "X-Real-IP"
	headerCFConnectingIP   = "CF-Connecting-IP"
	headerForwarded        = "Forwarded"
)

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Context struct {
	noCopy noCopy

	Request *http.Request
	Writer  ResponseWriter

	pathMutex  sync.RWMutex
	PathParams map[string]string

	queryCache url.Values

	handlers     []HandlerFunc
	handlerIndex int
}

func (c *Context) Reset() {
	c.Request = nil
	c.Writer = nil
	c.handlers = c.handlers[:0]
	c.handlerIndex = -1
	c.queryCache = nil
	if c.PathParams != nil {
		for k := range c.PathParams {
			delete(c.PathParams, k)
		}
	}
}

func (c *Context) Next() {
	c.handlerIndex++
	for c.handlerIndex < len(c.handlers) {
		if c.handlers[c.handlerIndex] != nil {
			c.handlers[c.handlerIndex](c)
		}
		c.handlerIndex++
	}
}

func (c *Context) Abort() {
	c.handlerIndex = len(c.handlers)
}

func (c *Context) IsAborted() bool {
	return c.handlerIndex >= len(c.handlers)
}

func (c *Context) HandlerCount() int {
	return len(c.handlers)
}

func (c *Context) AddParam(k, v string) {
	c.pathMutex.Lock()
	defer c.pathMutex.Unlock()
	c.PathParams[k] = v
}

func (c *Context) Param(key string) string {
	c.pathMutex.RLock()
	defer c.pathMutex.RUnlock()
	return c.PathParams[key]
}

func (c *Context) Params() map[string]string {
	c.pathMutex.RLock()
	defer c.pathMutex.RUnlock()
	return c.PathParams
}
