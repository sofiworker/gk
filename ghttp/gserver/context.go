package gserver

import (
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const abortIndex = math.MaxInt8 >> 1

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Context struct {
	noCopy noCopy

	Request *http.Request
	Writer  http.ResponseWriter

	Params     map[string]string
	Values     map[string]interface{}
	queryCache url.Values

	handlers []HandlerFunc
	index    int
}

func (c *Context) Reset() {
	c.Request = nil
	c.Writer = nil
	if c.Params != nil {
		for k := range c.Params {
			delete(c.Params, k)
		}
	}
	if c.Values != nil {
		for k := range c.Values {
			delete(c.Values, k)
		}
	}
	c.handlers = nil
	c.index = -1
}

func (c *Context) Next() {
	c.index++
	for c.index < len(c.handlers) {
		if c.handlers[c.index] != nil {
			c.handlers[c.index](c)
		}
		c.index++
	}
}

func (c *Context) Abort() {
	c.index = abortIndex
}

func (c *Context) IsAborted() bool {
	return c.index >= abortIndex
}

func (c *Context) Param(key string) string {
	//return c.Params.ByName(key)
	return ""
}

func (c *Context) AddParam(key, value string) {
	//c.Params = append(c.Params, Param{Key: key, Value: value})
}

func (c *Context) Query(key string) (value string) {
	value, _ = c.GetQuery(key)
	return
}

func (c *Context) DefaultQuery(key, defaultValue string) string {
	if value, ok := c.GetQuery(key); ok {
		return value
	}
	return defaultValue
}

func (c *Context) GetQuery(key string) (string, bool) {
	if values, ok := c.GetQueryArray(key); ok {
		return values[0], ok
	}
	return "", false
}

func (c *Context) QueryArray(key string) (values []string) {
	values, _ = c.GetQueryArray(key)
	return
}

func (c *Context) GetQueryArray(key string) (values []string, ok bool) {
	c.initQueryCache()
	values, ok = c.queryCache[key]
	return
}

func (c *Context) initQueryCache() {
	if c.queryCache == nil {
		if c.Request != nil && c.Request.URL != nil {
			c.queryCache = c.Request.URL.Query()
		} else {
			c.queryCache = url.Values{}
		}
	}
}

func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	return nil, nil
}

func (c *Context) MultipartForm() (*multipart.Form, error) {
	return nil, nil
}

func (c *Context) Bind(obj interface{}) error {
	return nil
}

func (c *Context) BindJSON(obj interface{}) error {
	return nil
}

func (c *Context) ClientIP() string {
	return ""
}

func (c *Context) RemoteIP() string {
	ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err != nil {
		return ""
	}
	return ip
}

func (c *Context) IsWebsocket() bool {
	if strings.Contains(strings.ToLower(c.requestHeader("Connection")), "upgrade") &&
		strings.EqualFold(c.requestHeader("Upgrade"), "websocket") {
		return true
	}
	return false
}

func (c *Context) requestHeader(key string) string {
	return c.Request.Header.Get(key)
}

func (c *Context) Cookie(name string) (string, error) {
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return "", err
	}
	val, _ := url.QueryUnescape(cookie.Value)
	return val, nil
}

func (c *Context) File(filepath string) {
	http.ServeFile(c.Writer, c.Request, filepath)
}

func (c *Context) FileFromFS(filepath string, fs http.FileSystem) {
	defer func(old string) {
		c.Request.URL.Path = old
	}(c.Request.URL.Path)

	c.Request.URL.Path = filepath

	http.FileServer(fs).ServeHTTP(c.Writer, c.Request)
}
