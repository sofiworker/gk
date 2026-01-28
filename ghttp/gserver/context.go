package gserver

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

var renderBufPool = sync.Pool{New: func() interface{} { return make([]byte, 8*1024) }}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Context struct {
	noCopy noCopy

	Writer ResponseWriter

	fastCtx    *fasthttp.RequestCtx
	pathParams map[string]string
	fullPath   string

	queryCache url.Values
	logger     Logger

	reqCtx context.Context
	values map[interface{}]interface{}

	handlers     []HandlerFunc
	handlerIndex int

	codec *CodecFactory

	render Render
}

func (c *Context) Reset() {
	c.Writer = nil
	c.fastCtx = nil

	c.handlers = c.handlers[:0]
	c.handlerIndex = -1
	c.queryCache = nil
	c.reqCtx = context.Background()
	c.logger = nil
	c.codec = nil
	c.render = nil
	c.fullPath = ""

	// Optimize pathParams reset for better performance
	// Instead of recreating the map, clear it to reduce allocations
	// This follows fasthttp best practices of reusing objects
	if c.pathParams != nil {
		for k := range c.pathParams {
			delete(c.pathParams, k)
		}
	} else {
		c.pathParams = make(map[string]string)
	}

	if c.values != nil {
		for k := range c.values {
			delete(c.values, k)
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

func (c *Context) AbortWithStatus(code int) {
	c.Status(code)
	c.Abort()
}

func (c *Context) AbortWithStatusJSON(code int, obj interface{}) {
	c.JSON(code, obj)
	c.Abort()
}

func (c *Context) IsAborted() bool {
	return c.handlerIndex >= len(c.handlers)
}

func (c *Context) HandlerCount() int {
	return len(c.handlers)
}

func (c *Context) AddParam(k, v string) {
	c.pathParams[k] = v
}

func (c *Context) Logger() Logger {
	return c.logger
}

func (c *Context) Param(key string) string {
	return c.pathParams[key]
}

func (c *Context) Params() map[string]string {
	return c.pathParams
}

func (c *Context) FullPath() string {
	return c.fullPath
}

func (c *Context) Query(key string) string {
	return string(c.fastCtx.QueryArgs().Peek(key))
}

func (c *Context) GetQuery(key string) (string, bool) {
	b, ok := c.GetQueryBytes(key)
	if !ok {
		return "", false
	}
	return string(b), true
}

func (c *Context) GetQueryBytes(key string) ([]byte, bool) {
	b := c.fastCtx.QueryArgs().Peek(key)
	return b, b != nil
}

func (c *Context) QueryDefault(key, defaultValue string) string {
	query := c.Query(key)
	if query == "" {
		return defaultValue
	}
	return query
}

// QueryArray returns a slice of strings for a given query key
func (c *Context) QueryArray(key string) []string {
	values := c.fastCtx.QueryArgs().PeekMulti(key)
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = string(v)
	}
	return result
}

// QueryMap returns a map for a given query key
func (c *Context) QueryMap(key string) map[string]string {
	// Query maps are typically in the form of key[subkey]=value
	// We'll parse all query parameters and extract those that match the pattern
	queryArgs := c.fastCtx.QueryArgs()
	result := make(map[string]string)

	queryArgs.VisitAll(func(k, v []byte) {
		keyStr := string(k)
		if len(keyStr) > len(key)+2 && keyStr[:len(key)] == key && keyStr[len(key)] == '[' {
			// Extract subkey from brackets
			end := len(keyStr) - 1
			if keyStr[end] == ']' {
				subKey := keyStr[len(key)+1 : end]
				result[subKey] = string(v)
			}
		}
	})

	return result
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
	if c.values == nil {
		c.values = make(map[interface{}]interface{}, 8)
	}
	c.values[key] = value
}

func (c *Context) Set(key, value interface{}) {
	c.SetValue(key, value)
}

func (c *Context) Value(key interface{}) interface{} {
	if c.values != nil {
		if v, ok := c.values[key]; ok {
			return v
		}
	}
	if c.reqCtx != nil {
		return c.reqCtx.Value(key)
	}
	return nil
}

func (c *Context) GetValue(key interface{}) (interface{}, bool) {
	if c.values == nil {
		return nil, false
	}
	v, ok := c.values[key]
	return v, ok
}

// Context returns the request context for cancellation/deadlines.
func (c *Context) Context() context.Context {
	if c.reqCtx == nil {
		return context.Background()
	}
	return c.reqCtx
}

// SetContext sets the request context. A nil ctx resets to Background.
func (c *Context) SetContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.reqCtx = ctx
}

func (c *Context) Deadline() (time.Time, bool) {
	return c.Context().Deadline()
}

func (c *Context) Done() <-chan struct{} {
	return c.Context().Done()
}

func (c *Context) Err() error {
	return c.Context().Err()
}

func (c *Context) SetCookie(cookie *fasthttp.Cookie) {
	c.fastCtx.Response.Header.SetCookie(cookie)
}

func (c *Context) Cookie(key string) string {
	return string(c.fastCtx.Request.Header.Cookie(key))
}

// PostForm returns the specified key from a POST form request
func (c *Context) PostForm(key string) string {
	return string(c.fastCtx.PostArgs().Peek(key))
}

func (c *Context) GetPostForm(key string) (string, bool) {
	b, ok := c.GetPostFormBytes(key)
	if !ok {
		return "", false
	}
	return string(b), true
}

func (c *Context) GetPostFormBytes(key string) ([]byte, bool) {
	b := c.fastCtx.PostArgs().Peek(key)
	return b, b != nil
}

// PostFormDefault returns the specified key from a POST form request or a default value
func (c *Context) PostFormDefault(key, defaultValue string) string {
	value := c.PostForm(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// PostFormArray returns a slice of strings for a given form key
func (c *Context) PostFormArray(key string) []string {
	values := c.fastCtx.PostArgs().PeekMulti(key)
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = string(v)
	}
	return result
}

// PostFormMap returns a map for a given form key
func (c *Context) PostFormMap(key string) map[string]string {
	postArgs := c.fastCtx.PostArgs()
	result := make(map[string]string)

	postArgs.VisitAll(func(k, v []byte) {
		keyStr := string(k)
		if len(keyStr) > len(key)+2 && keyStr[:len(key)] == key && keyStr[len(key)] == '[' {
			// Extract subkey from brackets
			end := len(keyStr) - 1
			if keyStr[end] == ']' {
				subKey := keyStr[len(key)+1 : end]
				result[subKey] = string(v)
			}
		}
	})

	return result
}

// FormFile returns the first file for the provided form key
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	return c.fastCtx.FormFile(name)
}

// MultipartForm returns the parsed multipart form, including file uploads
func (c *Context) MultipartForm() (*multipart.Form, error) {
	return c.fastCtx.MultipartForm()
}

// BindJSON binds the request body to the specified object using JSON
func (c *Context) BindJSON(obj interface{}) error {
	return json.Unmarshal(c.fastCtx.Request.Body(), obj)
}

// BindXML binds the request body to the specified object using XML
func (c *Context) BindXML(obj interface{}) error {
	return xml.Unmarshal(c.fastCtx.Request.Body(), obj)
}

// ShouldBindJSON binds the request body to the specified object using JSON
// It's an alias for BindJSON for API compatibility
func (c *Context) ShouldBindJSON(obj interface{}) error {
	return c.BindJSON(obj)
}

// ShouldBindXML binds the request body to the specified object using XML
// It's an alias for BindXML for API compatibility
func (c *Context) ShouldBindXML(obj interface{}) error {
	return c.BindXML(obj)
}

// RespAuto automatically encodes and writes data based on the Accept header
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

// JSON serializes the given struct as JSON into the response body
// It also sets the Content-Type as "application/json"
func (c *Context) JSON(code int, obj interface{}) {
	c.Header("Content-Type", "application/json")
	c.Status(code)
	if err := json.NewEncoder(c.Writer).Encode(obj); err != nil {
		panic(err)
	}
}

// XML serializes the given struct as XML into the response body
// It also sets the Content-Type as "application/xml"
func (c *Context) XML(code int, obj interface{}) {
	c.Header("Content-Type", "application/xml")
	c.Status(code)
	if err := xml.NewEncoder(c.Writer).Encode(obj); err != nil {
		panic(err)
	}
}

// String writes the given string into the response body
func (c *Context) String(code int, format string, values ...interface{}) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(code)
	if len(values) > 0 {
		_, _ = c.Writer.WriteString(fmt.Sprintf(format, values...))
	} else {
		_, _ = c.Writer.WriteString(format)
	}
}

// Data writes some data into the response body with specific content type
func (c *Context) Data(code int, contentType string, data []byte) {
	c.Header("Content-Type", contentType)
	c.Status(code)
	_, _ = c.Writer.Write(data)
}

// HTML renders the HTTP template specified by its file name
func (c *Context) HTML(code int, name string, obj interface{}) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(code)
	if c.render == nil || obj == nil {
		_, _ = c.Writer.WriteString(name)
		return
	}
	if hr, ok := c.render.(interface {
		RenderHTML(name string, data interface{}) (io.Reader, error)
	}); ok {
		reader, err := hr.RenderHTML(name, obj)
		if err != nil {
			panic(err)
		}
		c.writeFromReader(reader)
		return
	}
	_, _ = c.Writer.WriteString(name)
}

// Request returns the underlying fasthttp request context
func (c *Context) Request() *fasthttp.Request {
	return &c.fastCtx.Request
}

// Response returns the underlying fasthttp response context
func (c *Context) Response() *fasthttp.Response {
	return &c.fastCtx.Response
}

// ClientIP returns the real client IP
func (c *Context) ClientIP() string {
	return c.fastCtx.RemoteIP().String()
}

// ContentType returns the Content-Type header of the request
func (c *Context) ContentType() string {
	return string(c.fastCtx.Request.Header.ContentType())
}

// IsWebsocket returns true if the request headers indicate it's a websocket connection
func (c *Context) IsWebsocket() bool {
	upgrade := c.requestHeader("Upgrade")
	return strings.EqualFold(upgrade, "websocket")
}

// StatusCode returns the response status code
func (c *Context) StatusCode() int {
	return c.Writer.Status()
}

func (c *Context) Render(data interface{}) {
	if c.render == nil {
		panic("render is nil")
	}
	reader, err := c.render.Render(data)
	if err != nil {
		panic(err)
	}
	c.writeFromReader(reader)
}

func (c *Context) writeFromReader(r io.Reader) {
	if r == nil {
		panic("render returned nil reader")
	}
	buf := renderBufPool.Get().([]byte)
	defer renderBufPool.Put(buf)
	if _, err := io.CopyBuffer(c.Writer, r, buf); err != nil {
		panic(err)
	}
}

func (c *Context) QueryBytes(key string) []byte {
	return c.fastCtx.QueryArgs().Peek(key)
}

func (c *Context) PostFormBytes(key string) []byte {
	return c.fastCtx.PostArgs().Peek(key)
}

func (c *Context) HeaderBytes(key string) []byte {
	return c.fastCtx.Request.Header.Peek(key)
}

func (c *Context) CookieBytes(key string) []byte {
	return c.fastCtx.Request.Header.Cookie(key)
}

func (c *Context) BodyBytes() []byte {
	return c.fastCtx.Request.Body()
}
