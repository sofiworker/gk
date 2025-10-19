package gserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/sofiworker/gk/ghttp/codec"
)

const (
	abortIndex              = math.MaxInt8 >> 1
	defaultMultipartMemory  = 32 << 20 // 32MB
	headerContentType       = "Content-Type"
	headerXForwardedFor     = "X-Forwarded-For"
	headerXRealIP           = "X-Real-IP"
	headerCFConnectingIP    = "CF-Connecting-IP"
	headerForwarded         = "Forwarded"
	headerForwardedForToken = "for="
)

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Context struct {
	noCopy noCopy

	engine *Server

	Request *http.Request
	Writer  ResponseWriter

	Params map[string]string
	Values map[string]interface{}

	queryCache url.Values
	formCache  url.Values

	handlers   []HandlerFunc
	index      int
	fullPath   string
	statusCode int
	bodyBytes  []byte
}

func (c *Context) setEngine(engine *Server) {
	c.engine = engine
}

func (c *Context) Engine() *Server {
	return c.engine
}

func (c *Context) Reset() {
	c.Request = nil
	c.Writer = nil
	c.handlers = c.handlers[:0]
	c.index = -1
	c.fullPath = ""
	c.statusCode = 0
	c.bodyBytes = nil
	c.queryCache = nil
	c.formCache = nil
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

func (c *Context) AbortWithStatus(code int) {
	c.Status(code)
	c.Abort()
}

func (c *Context) AbortWithStatusJSON(code int, obj interface{}) {
	c.Abort()
	c.JSON(code, obj)
}

func (c *Context) IsAborted() bool {
	return c.index >= abortIndex
}

func (c *Context) HandlerCount() int {
	return len(c.handlers)
}

func (c *Context) Param(key string) string {
	if c.Params == nil {
		return ""
	}
	return c.Params[key]
}

func (c *Context) AddParam(key, value string) {
	if c.Params == nil {
		c.Params = make(map[string]string)
	}
	c.Params[key] = value
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
	if values, ok := c.GetQueryArray(key); ok && len(values) > 0 {
		return values[0], ok
	}
	return "", false
}

func (c *Context) QueryArray(key string) []string {
	values, _ := c.GetQueryArray(key)
	return values
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

func (c *Context) initFormCache() {
	if c.formCache != nil {
		return
	}
	if c.Request == nil {
		c.formCache = url.Values{}
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		c.formCache = url.Values{}
		return
	}
	c.formCache = c.Request.Form
}

func (c *Context) FormValue(key string) string {
	c.initFormCache()
	return c.formCache.Get(key)
}

func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	if c.Request == nil {
		return nil, errors.New("request is nil")
	}
	if err := c.Request.ParseMultipartForm(defaultMultipartMemory); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return nil, err
	}
	file, header, err := c.Request.FormFile(name)
	if file != nil {
		_ = file.Close()
	}
	return header, err
}

func (c *Context) MultipartForm() (*multipart.Form, error) {
	if c.Request == nil {
		return nil, errors.New("request is nil")
	}
	if err := c.Request.ParseMultipartForm(defaultMultipartMemory); err != nil {
		return nil, err
	}
	return c.Request.MultipartForm, nil
}

func (c *Context) ShouldBind(obj interface{}) error {
	if c.Request == nil {
		return errors.New("request is nil")
	}
	if obj == nil {
		return errors.New("bind target is nil")
	}

	contentType := c.ContentType()

	switch {
	case strings.Contains(contentType, MIMEJSON):
		return c.bindWithCodec(MIMEJSON, obj)
	case strings.Contains(contentType, MIMEXML) || strings.Contains(contentType, MIMEXML2):
		return c.bindWithCodec(MIMEXML, obj)
	case strings.Contains(contentType, MIMEYAML) || strings.Contains(contentType, MIMEYAML2):
		return c.bindWithCodec(MIMEYAML, obj)
	case strings.Contains(contentType, MIMEPOSTForm):
		return c.bindForm(obj, false)
	case strings.Contains(contentType, MIMEMultipartPOSTForm):
		return c.bindForm(obj, true)
	default:
		return c.bindWithCodec(contentType, obj)
	}
}

func (c *Context) Bind(obj interface{}) error {
	if err := c.ShouldBind(obj); err != nil {
		if c.Writer != nil && !c.Writer.Written() {
			c.AbortWithStatusJSON(http.StatusBadRequest, map[string]interface{}{
				"error": err.Error(),
			})
		}
		return err
	}
	return nil
}

func (c *Context) ShouldBindJSON(obj interface{}) error {
	return c.bindWithCodec(MIMEJSON, obj)
}

func (c *Context) BindJSON(obj interface{}) error {
	if err := c.bindWithCodec(MIMEJSON, obj); err != nil {
		if c.Writer != nil && !c.Writer.Written() {
			c.AbortWithStatusJSON(http.StatusBadRequest, map[string]interface{}{
				"error": err.Error(),
			})
		}
		return err
	}
	return nil
}

func (c *Context) ShouldBindQuery(obj interface{}) error {
	c.initQueryCache()
	return bindFormLike(obj, c.queryCache, c.codecManager())
}

func (c *Context) BindQuery(obj interface{}) error {
	c.initQueryCache()
	if err := bindFormLike(obj, c.queryCache, c.codecManager()); err != nil {
		if c.Writer != nil && !c.Writer.Written() {
			c.AbortWithStatusJSON(http.StatusBadRequest, map[string]interface{}{
				"error": err.Error(),
			})
		}
		return err
	}
	return nil
}

func (c *Context) bindForm(obj interface{}, multipart bool) error {
	if multipart {
		if _, err := c.MultipartForm(); err != nil {
			return err
		}
	} else {
		if err := c.Request.ParseForm(); err != nil {
			return err
		}
	}
	return bindFormLike(obj, c.Request.Form, c.codecManager())
}

func (c *Context) bindWithCodec(contentType string, obj interface{}) error {
	body, err := c.BodyBytes()
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}

	manager := c.codecManager()
	if manager == nil {
		return errors.New("codec manager not configured")
	}
	cd := manager.GetCodec(contentType)
	if cd == nil {
		return fmt.Errorf("unsupported content type: %s", contentType)
	}
	return cd.Decode(body, obj)
}

func (c *Context) codecManager() *codec.CodecManager {
	if c.engine != nil && c.engine.codecManager != nil {
		return c.engine.codecManager
	}
	return defaultCodecManager()
}

func defaultCodecManager() *codec.CodecManager {
	return codec.DefaultManager()
}

func (c *Context) BodyBytes() ([]byte, error) {
	if c.bodyBytes != nil {
		return c.bodyBytes, nil
	}
	if c.Request == nil || c.Request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	_ = c.Request.Body.Close()
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.bodyBytes = body
	return body, nil
}

func (c *Context) BodyString() (string, error) {
	body, err := c.BodyBytes()
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *Context) Status(code int) {
	c.statusCode = code
	if c.Writer != nil {
		c.Writer.WriteHeader(code)
	}
}

func (c *Context) StatusCode() int {
	if c.statusCode != 0 {
		return c.statusCode
	}
	if c.Writer != nil {
		return c.Writer.Status()
	}
	return 0
}

func (c *Context) SetHeader(key, value string) {
	if c.Writer == nil {
		return
	}
	if value == "" {
		c.Writer.Header().Del(key)
		return
	}
	c.Writer.Header().Set(key, value)
}

func (c *Context) Header(key string) string {
	if c.Writer == nil {
		return ""
	}
	return c.Writer.Header().Get(key)
}

func (c *Context) Set(key string, value interface{}) {
	if c.Values == nil {
		c.Values = make(map[string]interface{})
	}
	c.Values[key] = value
}

func (c *Context) Get(key string) (interface{}, bool) {
	if c.Values == nil {
		return nil, false
	}
	val, ok := c.Values[key]
	return val, ok
}

func (c *Context) MustGet(key string) interface{} {
	val, ok := c.Get(key)
	if !ok {
		panic(fmt.Sprintf("key %s does not exist", key))
	}
	return val
}

func (c *Context) GetString(key string) string {
	if val, ok := c.Get(key); ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

func (c *Context) GetBool(key string) bool {
	if val, ok := c.Get(key); ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

func (c *Context) SetFullPath(path string) {
	c.fullPath = path
}

func (c *Context) FullPath() string {
	return c.fullPath
}

func (c *Context) ContentType() string {
	if c.Request == nil {
		return ""
	}
	return c.Request.Header.Get(headerContentType)
}

func (c *Context) JSON(code int, obj interface{}) {
	c.renderWithCodec(code, MIMEJSON, obj)
}

func (c *Context) XML(code int, obj interface{}) {
	c.renderWithCodec(code, MIMEXML, obj)
}

func (c *Context) YAML(code int, obj interface{}) {
	c.renderWithCodec(code, MIMEYAML, obj)
}

func (c *Context) String(code int, format string, values ...interface{}) {
	if c.Writer == nil {
		return
	}
	c.SetHeader(headerContentType, MIMEPlain+"; charset=utf-8")
	c.Status(code)
	if len(values) == 0 {
		_, _ = c.Writer.Write([]byte(format))
		return
	}
	_, _ = fmt.Fprintf(c.Writer, format, values...)
}

func (c *Context) Data(code int, contentType string, data []byte) {
	if c.Writer == nil {
		return
	}
	if contentType != "" {
		c.SetHeader(headerContentType, contentType)
	}
	c.Status(code)
	if len(data) > 0 {
		_, _ = c.Writer.Write(data)
	}
}

func (c *Context) HTML(code int, html string) {
	c.Data(code, MIMEHTML+"; charset=utf-8", []byte(html))
}

func (c *Context) Redirect(code int, location string) {
	if c.Writer == nil || c.Request == nil {
		return
	}
	http.Redirect(c.Writer, c.Request, location, code)
}

func (c *Context) Accepted() bool {
	if c.Values == nil {
		return false
	}
	if accepted, ok := c.Values["_accepted"].(bool); ok {
		return accepted
	}
	return false
}

func (c *Context) SetAccepted() {
	c.Set("_accepted", true)
}

func (c *Context) ClientIP() string {
	if c.Request == nil {
		return ""
	}

	if forwarded := c.Request.Header.Get(headerForwarded); forwarded != "" {
		if ip := parseForwarded(forwarded); ip != "" {
			return ip
		}
	}

	for _, header := range []string{headerXRealIP, headerCFConnectingIP} {
		if ip := strings.TrimSpace(c.Request.Header.Get(header)); ip != "" {
			return ip
		}
	}

	if xff := c.Request.Header.Get(headerXForwardedFor); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			ip := strings.TrimSpace(part)
			if ip != "" {
				return ip
			}
		}
	}

	return c.RemoteIP()
}

func (c *Context) RemoteIP() string {
	if c.Request == nil {
		return ""
	}
	ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(c.Request.RemoteAddr)
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
	if c.Request == nil {
		return ""
	}
	return c.Request.Header.Get(key)
}

func (c *Context) Cookie(name string) (string, error) {
	if c.Request == nil {
		return "", errors.New("request is nil")
	}
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return "", err
	}
	val, _ := url.QueryUnescape(cookie.Value)
	return val, nil
}

func (c *Context) File(filepath string) {
	if c.Writer == nil || c.Request == nil {
		return
	}
	http.ServeFile(c.Writer, c.Request, filepath)
}

func (c *Context) FileFromFS(path string, fs http.FileSystem) {
	if c.Writer == nil || c.Request == nil {
		return
	}
	defer func(old string) {
		c.Request.URL.Path = old
	}(c.Request.URL.Path)

	c.Request.URL.Path = path
	http.FileServer(fs).ServeHTTP(c.Writer, c.Request)
}

func (c *Context) renderWithCodec(code int, contentType string, obj interface{}) {
	if c.Writer == nil {
		return
	}
	manager := c.codecManager()
	if manager == nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	cd := manager.GetCodec(contentType)
	if cd == nil {
		c.Status(http.StatusUnsupportedMediaType)
		return
	}
	data, err := cd.Encode(obj)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_, _ = c.Writer.Write([]byte(err.Error()))
		return
	}
	c.SetHeader(headerContentType, cd.ContentType())
	c.Status(code)
	if len(data) > 0 {
		_, _ = c.Writer.Write(data)
	}
}

func parseForwarded(forwarded string) string {
	parts := strings.Split(forwarded, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		idx := strings.Index(strings.ToLower(part), headerForwardedForToken)
		if idx == -1 {
			continue
		}
		value := strings.TrimSpace(part[idx+len(headerForwardedForToken):])
		value = strings.Trim(value, "\"")
		value = strings.Trim(value, "[]")
		if value != "" {
			return value
		}
	}
	return ""
}

func bindFormLike(obj interface{}, values url.Values, manager *codec.CodecManager) error {
	if obj == nil {
		return errors.New("bind target is nil")
	}
	if len(values) == 0 {
		return nil
	}
	data := make(map[string]interface{}, len(values))
	for k, v := range values {
		switch len(v) {
		case 0:
			data[k] = ""
		case 1:
			data[k] = v[0]
		default:
			cp := make([]string, len(v))
			copy(cp, v)
			data[k] = cp
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if manager != nil {
		if cd := manager.GetCodec(MIMEJSON); cd != nil {
			return cd.Decode(raw, obj)
		}
	}
	return json.Unmarshal(raw, obj)
}
