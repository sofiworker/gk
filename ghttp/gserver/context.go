package gserver

import (
	"net/url"
	"sync"

	"github.com/valyala/fasthttp"
)

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Context struct {
	noCopy noCopy

	Writer ResponseWriter

	fastCtx *fasthttp.RequestCtx

	pathMutex  sync.RWMutex
	PathParams map[string]string

	queryCache url.Values

	handlers     []HandlerFunc
	handlerIndex int
}

func (c *Context) Reset() {
	c.Writer = nil
	c.fastCtx = nil

	c.handlers = c.handlers[:0]
	c.handlerIndex = -1
	c.queryCache = nil
	if c.PathParams != nil {
		for k := range c.PathParams {
			delete(c.PathParams, k)
		}
	} else {
		c.PathParams = make(map[string]string)
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

func (c *Context) FastContext() *fasthttp.RequestCtx {
	return c.fastCtx
}
