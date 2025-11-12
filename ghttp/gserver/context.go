package gserver

import (
	"context"
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
	pathParams map[string]string

	queryCache url.Values

	valueCtx context.Context

	handlers     []HandlerFunc
	handlerIndex int

	codec *CodecFactory
}

func (c *Context) Reset() {
	c.Writer = nil
	c.fastCtx = nil

	c.handlers = c.handlers[:0]
	c.handlerIndex = -1
	c.queryCache = nil
	c.valueCtx = context.Background()
	if c.pathParams != nil {
		for k := range c.pathParams {
			delete(c.pathParams, k)
		}
	} else {
		c.pathParams = make(map[string]string)
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
	c.pathParams[k] = v
}

func (c *Context) Param(key string) string {
	c.pathMutex.RLock()
	defer c.pathMutex.RUnlock()
	return c.pathParams[key]
}

func (c *Context) Params() map[string]string {
	c.pathMutex.RLock()
	defer c.pathMutex.RUnlock()
	return c.pathParams
}

func (c *Context) Query(key string) string {
	return string(c.fastCtx.QueryArgs().Peek(key))
}

func (c *Context) QueryDefault(key, defaultValue string) string {
	query := c.Query(key)
	if query == "" {
		return defaultValue
	}
	return query
}

func (c *Context) Status(code int) {
	c.Writer.WriteHeader(code)
}

func (c *Context) GetHeader(key string) string {
	return c.requestHeader(key)
}

func (c *Context) requestHeader(key string) string {
	return string(c.fastCtx.Request.Header.Peek(key))
}

func (c *Context) Header(key, value string) {
	if value == "" {
		c.Writer.Header().Del(key)
		return
	}
	c.Writer.Header().Set(key, value)
}

func (c *Context) FastContext() *fasthttp.RequestCtx {
	return c.fastCtx
}

func (c *Context) SetValue(key, value interface{}) {
	c.valueCtx = context.WithValue(c.valueCtx, key, value)
}

func (c *Context) Value(key interface{}) interface{} {
	return c.valueCtx.Value(key)
}

func (c *Context) SetCookie(cookie *fasthttp.Cookie) {
	c.fastCtx.Response.Header.SetCookie(cookie)
}

func (c *Context) Cookie(key string) string {
	return string(c.fastCtx.Request.Header.Cookie(key))
}

func (c *Context) RespAuto(data interface{}) {
	accept := c.requestHeader("Accept")
	if accept == "" {
		accept = "application/json"
	}
	bytes, err := c.codec.Get(accept).EncodeBytes(data)
	if err != nil {
		panic(err)
	}
	_, err = c.Writer.Write(bytes)
	if err != nil {
		panic(err)
	}
}
