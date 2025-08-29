package ghttp

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/valyala/fasthttp"
)

// NewContext 创建新的 Context 实例
func NewContext(ctx *fasthttp.RequestCtx) *Context {
	return &Context{ctx: ctx}
}

// ---------- Request 相关方法 ----------

// Method 返回请求方法
func (c *Context) Method() string {
	return string(c.ctx.Method())
}

// Path 返回请求路径
func (c *Context) Path() string {
	return string(c.ctx.Path())
}

// Query 获取查询参数
func (c *Context) Query(key string) string {
	return string(c.ctx.QueryArgs().Peek(key))
}

// QueryDefault 获取查询参数，如果不存在则返回默认值
func (c *Context) QueryDefault(key, defaultValue string) string {
	value := c.ctx.QueryArgs().Peek(key)
	if value == nil {
		return defaultValue
	}
	return string(value)
}

// Param 获取路径参数
func (c *Context) Param(key string) string {
	value, _ := c.ctx.UserValue(key).(string)
	return value
}

// ParamInt 获取 int 类型的路径参数
func (c *Context) ParamInt(key string) (int, error) {
	value := c.Param(key)
	return strconv.Atoi(value)
}

// Header 获取请求头
func (c *Context) Header(key string) string {
	return string(c.ctx.Request.Header.Peek(key))
}

// ContentType 返回 Content-Type 头部
func (c *Context) ContentType() string {
	return string(c.ctx.Request.Header.ContentType())
}

// Body 返回原始请求体
func (c *Context) Body() []byte {
	return c.ctx.PostBody()
}

// Bind 根据 Content-Type 自动解析请求体到结构体
func (c *Context) Bind(v interface{}) error {
	contentType := c.ContentType()

	// 获取 content type 主体部分 (去除 charset 等参数)
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = contentType[:idx]
	}

	// 去除空格
	contentType = strings.TrimSpace(contentType)

	// 查找对应的解码器
	decoder, ok := decoders.Exist(contentType)
	if !ok {
		// 默认使用 JSON 解解码器
		decoder = NewJsonDecoder()
	}

	return decoder.Decode(strings.NewReader(string(c.Body())), v)
}

// FormValue 获取表单值
func (c *Context) FormValue(key string) string {
	return string(c.ctx.FormValue(key))
}

// FormFile 获取上传的文件
func (c *Context) FormFile(key string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(key)
}

// MultipartForm 获取 multipart 表单
func (c *Context) MultipartForm() (*multipart.Form, error) {
	return c.ctx.MultipartForm()
}

// RemoteIP 获取客户端 IP
func (c *Context) RemoteIP() net.IP {
	return c.ctx.RemoteIP()
}

// URI 返回完整的请求 URI
func (c *Context) URI() string {
	return string(c.ctx.RequestURI())
}

// UserAgent 返回 User-Agent 头部
func (c *Context) UserAgent() string {
	return string(c.ctx.Request.Header.UserAgent())
}

// Referer 返回 Referer 头部
func (c *Context) Referer() string {
	return string(c.ctx.Request.Header.Referer())
}

// Request 返回原始请求
func (c *Context) Request() *fasthttp.Request {
	return &c.ctx.Request
}

// Response 返回原始响应
func (c *Context) Response() *fasthttp.Response {
	return &c.ctx.Response
}

// ---------- Response 相关方法 ----------

// SetStatusCode 设置状态码
func (c *Context) SetStatusCode(code int) {
	c.ctx.SetStatusCode(code)
}

// SetHeader 设置响应头
func (c *Context) SetHeader(key, value string) {
	c.ctx.Response.Header.Set(key, value)
}

// String 返回文本响应
func (c *Context) String(code int, format string, values ...interface{}) {
	c.ctx.SetStatusCode(code)
	c.ctx.SetContentType("text/plain; charset=utf-8")
	if len(values) > 0 {
		c.ctx.SetBodyString(fmt.Sprintf(format, values...))
	} else {
		c.ctx.SetBodyString(format)
	}
}

// JSON 返回 JSON 响应
func (c *Context) JSON(code int, obj interface{}) {
	c.ctx.SetStatusCode(code)
	c.ctx.SetContentType("application/json; charset=utf-8")
	if obj == nil {
		c.ctx.SetBody([]byte{})
		return
	}

	if data, err := json.Marshal(obj); err != nil {
		c.ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		c.ctx.SetBodyString("Internal Server Error")
	} else {
		c.ctx.SetBody(data)
	}
}

// XML 返回 XML 响应
func (c *Context) XML(code int, obj interface{}) {
	c.ctx.SetStatusCode(code)
	c.ctx.SetContentType("application/xml; charset=utf-8")
	if obj == nil {
		c.ctx.SetBody([]byte{})
		return
	}

	if data, err := json.Marshal(obj); err != nil { // 简化处理，实际应使用 xml.Marshal
		c.ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		c.ctx.SetBodyString("Internal Server Error")
	} else {
		c.ctx.SetBody(data)
	}
}

// File 发送文件内容
func (c *Context) File(filepath string) {
	c.ctx.SendFile(filepath)
}

// FileAttachment 发送文件下载响应
func (c *Context) FileAttachment(filepath, filename string) {
	c.ctx.SendFile(filepath)
	c.ctx.Response.Header.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
}

// Stream 流式传输数据
func (c *Context) Stream(code int, contentType string, r io.Reader) error {
	c.ctx.SetStatusCode(code)
	c.ctx.SetContentType(contentType)
	_, err := io.Copy(c.ctx, r)
	return err
}

// Redirect 重定向
func (c *Context) Redirect(code int, location string) {
	c.ctx.Redirect(location, code)
}

// NoContent 返回无内容响应
func (c *Context) NoContent(code int) {
	c.ctx.SetStatusCode(code)
	c.ctx.SetBody([]byte{})
}

// ---------- 原始上下文访问 ----------

// RequestCtx 返回原始的 fasthttp.RequestCtx（谨慎使用）
func (c *Context) RequestCtx() *fasthttp.RequestCtx {
	return c.ctx
}

// ---------- 统一响应结构体 ----------

// APIResponse 统一响应结构体 (避免与 Response 重复定义)
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Success 返回成功响应
func (c *Context) Success(data interface{}) {
	c.JSON(fasthttp.StatusOK, &APIResponse{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// Error 返回错误响应
func (c *Context) Error(code int, message string) {
	c.JSON(code, &APIResponse{
		Code:    -1,
		Message: message,
	})
}

// CustomResponse 返回自定义响应
func (c *Context) CustomResponse(code int, message string, data interface{}) {
	c.JSON(fasthttp.StatusOK, &APIResponse{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

// ---------- 工具方法 ----------

// IsAjax 判断是否为 AJAX 请求
func (c *Context) IsAjax() bool {
	return c.Header("X-Requested-With") == "XMLHttpRequest"
}

// IsJson 判断是否为 JSON 请求
func (c *Context) IsJson() bool {
	return strings.Contains(c.ContentType(), "application/json")
}

// Time 获取请求时间
func (c *Context) Time() time.Time {
	return c.ctx.Time()
}

// ---------- Context 相关方法 ----------

// Set 在 context 中存储值
func (c *Context) Set(key string, value interface{}) {
	c.ctx.SetUserValue(key, value)
}

// Get 从 context 中获取值
func (c *Context) Get(key string) (interface{}, bool) {
	value := c.ctx.UserValue(key)
	return value, value != nil
}

// MustGet 从 context 中获取值，不存在则 panic
func (c *Context) MustGet(key string) interface{} {
	value := c.ctx.UserValue(key)
	if value == nil {
		panic("key \"" + key + "\" does not exist")
	}
	return value
}

// ---------- 扩展响应方法 ----------

// Status 设置 HTTP 状态码并返回 Context 以支持链式调用
func (c *Context) Status(code int) *Context {
	c.ctx.SetStatusCode(code)
	return c
}

// SendString 发送字符串响应
func (c *Context) SendString(s string) {
	c.ctx.SetBodyString(s)
}

// Send 发送字节响应
func (c *Context) Send(b []byte) {
	c.ctx.SetBody(b)
}

// SendJSON 发送 JSON 响应
func (c *Context) SendJSON(data interface{}) {
	c.JSON(fasthttp.StatusOK, data)
}

// SendError 发送错误响应
func (c *Context) SendError(code int, message string) {
	c.Error(code, message)
}

// ---------- 路由相关 ----------

// HandlerFunc 定义处理函数类型
type HandlerFunc func(*Context)

// Validator 定义验证器接口，兼容常见的验证器库
type Validator interface {
	Validate() error
}

// Router 路由器结构
type Router struct {
	// 路由树，按照 HTTP 方法分组
	trees map[string]*node

	// 全局中间件
	middlewares []HandlerFunc

	// 统一响应体模板
	unifiedResponseTemplate interface{}

	// 验证器缓存
	validatorCache sync.Map // map[reflect.Type]Validator

	// validator/v10 验证器实例
	validate *validator.Validate
}

// New 创建新的路由器实例
func New() *Router {
	r := &Router{
		trees:          make(map[string]*node),
		validatorCache: sync.Map{},
		validate:       validator.New(),
	}

	// 设置默认统一响应体模板
	r.unifiedResponseTemplate = &APIResponse{}

	return r
}

// Use 添加全局中间件
func (r *Router) Use(middleware ...HandlerFunc) {
	r.middlewares = append(r.middlewares, middleware...)
}

// SetUnifiedResponseTemplate 设置统一响应体模板
func (r *Router) SetUnifiedResponseTemplate(template interface{}) {
	r.unifiedResponseTemplate = template
}

// GET 注册 GET 路由
func (r *Router) GET(path string, fn interface{}) {
	r.handle(http.MethodGet, path, fn)
}

// POST 注册 POST 路由
func (r *Router) POST(path string, fn interface{}) {
	r.handle(http.MethodPost, path, fn)
}

// PUT 注册 PUT 路由
func (r *Router) PUT(path string, fn interface{}) {
	r.handle(http.MethodPut, path, fn)
}

// DELETE 注册 DELETE 路由
func (r *Router) DELETE(path string, fn interface{}) {
	r.handle(http.MethodDelete, path, fn)
}

// PATCH 注册 PATCH 路由
func (r *Router) PATCH(path string, fn interface{}) {
	r.handle(http.MethodPatch, path, fn)
}

// HEAD 注册 HEAD 路由
func (r *Router) HEAD(path string, fn interface{}) {
	r.handle(http.MethodHead, path, fn)
}

// OPTIONS 注册 OPTIONS 路由
func (r *Router) OPTIONS(path string, fn interface{}) {
	r.handle(http.MethodOptions, path, fn)
}

// ServeHTTP 实现 http.Handler 接口，兼容标准库
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// 创建 fasthttp.RequestCtx
	ctx := &fasthttp.RequestCtx{}

	// 转换请求
	ctx.Request.SetRequestURI(req.URL.String())
	ctx.Request.Header.SetMethod(req.Method)
	ctx.Request.SetBodyStream(req.Body, int(req.ContentLength))

	// 复制请求头
	for key, values := range req.Header {
		for _, value := range values {
			ctx.Request.Header.Add(key, value)
		}
	}

	// 处理请求
	r.Serve()(ctx)

	// 转换响应
	w.WriteHeader(ctx.Response.StatusCode())
	w.Write(ctx.Response.Body())
}

// Serve 返回 fasthttp 统一入口
func (r *Router) Serve() fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		method := string(ctx.Method())
		path := string(ctx.Path())

		// 查找路由树
		root := r.trees[method]
		if root == nil {
			ctx.Error("404 Not Found", fasthttp.StatusNotFound)
			return
		}

		// 查找处理器
		handler, _, _ := root.getValue(path, ctx)

		if handler != nil {
			handler(ctx)
			return
		}

		ctx.Error("404 Not Found", fasthttp.StatusNotFound)
	}
}

// handlerInfo 存储处理器信息，用于减少反射
type handlerInfo struct {
	handler    reflect.Value
	typ        reflect.Type
	paramNames []string
}

// handle 内部处理函数注册
func (r *Router) handle(method, path string, fn interface{}) {
	val := reflect.ValueOf(fn)
	typ := val.Type()

	if typ.Kind() != reflect.Func {
		panic("handler must be func")
	}

	// 验证函数签名是否符合 HTTP 服务器要求
	if err := r.validateHandlerSignature(typ); err != nil {
		panic(fmt.Sprintf("invalid handler signature: %v", err))
	}

	// 预计算路径参数位置
	paramNames := parsePathParams(path)

	// 创建处理器信息
	handlerInfo := &handlerInfo{
		handler:    val,
		typ:        typ,
		paramNames: paramNames,
	}

	// 生成处理函数
	handler := func(ctx *fasthttp.RequestCtx) {
		c := NewContext(ctx)
		args, err := r.buildHandlerArgs(handlerInfo, c)
		if err != nil {
			c.Error(fasthttp.StatusBadRequest, err.Error())
			return
		}

		rets := handlerInfo.handler.Call(args)
		r.writeHandlerResponse(c, rets)
	}

	// 注册到路由树
	if r.trees[method] == nil {
		r.trees[method] = new(node)
	}
	r.trees[method].addRoute(path, handler)
}

// validateHandlerSignature 验证处理函数签名是否符合要求
func (r *Router) validateHandlerSignature(typ reflect.Type) error {
	// 检查返回值数量（最多支持4个返回值）
	numOut := typ.NumOut()
	if numOut > 4 {
		return fmt.Errorf("handler function can have at most 4 return values")
	}

	// 检查参数类型是否支持
	for i := 0; i < typ.NumIn(); i++ {
		argType := typ.In(i)

		// 支持的参数类型
		supported := false

		// 1. Context 类型
		if argType == reflect.TypeOf(&Context{}) {
			supported = true
		}

		// 2. 基本类型 (用于路径参数)
		switch argType.Kind() {
		case reflect.String, reflect.Int, reflect.Int64, reflect.Bool, reflect.Float64:
			supported = true
		}

		// 3. 指针类型 (用于请求体绑定)
		if argType.Kind() == reflect.Ptr {
			supported = true
		}

		// 4. []byte 和 string 类型 (用于原始请求体)
		if argType == reflect.TypeOf([]byte(nil)) || argType.Kind() == reflect.String {
			supported = true
		}

		if !supported {
			return fmt.Errorf("unsupported parameter type: %v", argType)
		}
	}

	return nil
}

// buildHandlerArgs 构造处理函数参数
func (r *Router) buildHandlerArgs(info *handlerInfo, c *Context) ([]reflect.Value, error) {
	fnType := info.typ
	numIn := fnType.NumIn()
	args := make([]reflect.Value, numIn)
	idx := 0 // 已处理的路径变量计数

	for i := 0; i < numIn; i++ {
		argT := fnType.In(i)

		// 1) *Context 直接注入
		if argT == reflect.TypeOf(&Context{}) {
			args[i] = reflect.ValueOf(c)
			continue
		}

		// 2) 路径变量
		if idx < len(info.paramNames) {
			seg := c.Param(info.paramNames[idx])
			v, err := convert(argT, seg)
			if err != nil {
				return nil, err
			}
			args[i] = v
			idx++
			continue
		}

		// 3) 请求体绑定
		if argT.Kind() == reflect.Ptr {
			ptr := reflect.New(argT.Elem()).Interface()
			if err := c.Bind(ptr); err != nil {
				return nil, err
			}

			// 验证结构体字段 (如果实现了 Validator 接口)
			if validator, ok := ptr.(Validator); ok {
				if err := validator.Validate(); err != nil {
					return nil, err
				}
			}

			// 使用 validator/v10 验证
			if err := r.validate.Struct(ptr); err != nil {
				return nil, err
			}

			args[i] = reflect.ValueOf(ptr)
			continue
		}

		// 4) 原始 []byte
		if argT == reflect.TypeOf([]byte(nil)) {
			args[i] = reflect.ValueOf(c.Body())
			continue
		}

		// 5) 字符串
		if argT.Kind() == reflect.String {
			args[i] = reflect.ValueOf(string(c.Body()))
			continue
		}

		return nil, fmt.Errorf("unsupported parameter type: %v", argT)
	}
	return args, nil
}

// handlerReturnValues 解析处理函数的返回值
type handlerReturnValues struct {
	code       int
	message    string
	data       interface{}
	err        error
	hasCode    bool
	hasMessage bool
	hasData    bool
	hasError   bool
}

// parseReturnValues 解析处理函数的返回值
func (r *Router) parseReturnValues(rets []reflect.Value) *handlerReturnValues {
	result := &handlerReturnValues{
		code:    fasthttp.StatusOK, // 默认状态码
		message: "success",         // 默认消息
	}

	// 遍历所有返回值并根据类型进行分类
	for _, retVal := range rets {
		// 检查是否为 error 类型
		if retVal.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !retVal.IsNil() {
				result.err = retVal.Interface().(error)
			}
			result.hasError = true
			continue
		}

		// 检查是否为 int 类型（状态码）
		if retVal.Kind() == reflect.Int || retVal.Kind() == reflect.Int64 {
			result.code = int(retVal.Int())
			result.hasCode = true
			continue
		}

		// 检查是否为 string 类型（消息）
		if retVal.Kind() == reflect.String {
			// 如果还没有数据，则认为是数据
			// 如果已经有数据了，则认为是消息
			if !result.hasData && !result.hasMessage {
				result.data = retVal.String()
				result.hasData = true
			} else if !result.hasMessage {
				result.message = retVal.String()
				result.hasMessage = true
			}
			continue
		}

		// 其他类型视为数据
		result.data = retVal.Interface()
		result.hasData = true
	}

	return result
}

// writeHandlerResponse 处理函数返回值并写入响应
func (r *Router) writeHandlerResponse(c *Context, rets []reflect.Value) {
	// 如果没有返回值，直接返回
	if len(rets) == 0 {
		return
	}

	// 解析返回值
	parsed := r.parseReturnValues(rets)

	// 如果有错误且错误不为 nil，优先处理错误
	if parsed.hasError && parsed.err != nil {
		if parsed.hasCode {
			c.Error(parsed.code, parsed.err.Error())
		} else {
			c.Error(fasthttp.StatusBadRequest, parsed.err.Error())
		}
		return
	}

	// 根据返回值组合设置响应
	c.Status(parsed.code)

	// 如果只有数据且是字符串类型，直接作为文本响应
	if parsed.hasData && !parsed.hasMessage && !parsed.hasError && parsed.hasCode &&
		reflect.TypeOf(parsed.data).Kind() == reflect.String && parsed.code != 0 {
		c.String(parsed.code, parsed.data.(string))
		return
	}

	// 如果只有数据且是字符串类型，但没有明确的状态码，则使用默认成功响应
	if parsed.hasData && !parsed.hasMessage && !parsed.hasError && !parsed.hasCode &&
		reflect.TypeOf(parsed.data).Kind() == reflect.String {
		c.String(fasthttp.StatusOK, parsed.data.(string))
		return
	}

	// 如果有自定义消息和数据
	if parsed.hasMessage && parsed.hasData {
		c.CustomResponse(parsed.code, parsed.message, parsed.data)
		return
	}

	// 如果只有数据
	if parsed.hasData && !parsed.hasMessage {
		switch v := parsed.data.(type) {
		case nil:
			c.NoContent(fasthttp.StatusNoContent)
		case string:
			c.String(parsed.code, v)
		case []byte:
			c.Send(v)
		default:
			// 使用统一响应格式
			c.Success(v)
		}
		return
	}

	// 如果只有消息
	if parsed.hasMessage && !parsed.hasData {
		c.String(parsed.code, parsed.message)
		return
	}

	// 默认情况
	c.NoContent(parsed.code)
}

// node 路由树节点
type node struct {
	path      string
	wildChild bool
	nType     nodeType
	maxParams uint8
	indices   string
	children  []*node
	handlers  []fasthttp.RequestHandler
	priority  uint32
}

type nodeType uint8

const (
	static nodeType = iota // default
	root
	param
	catchAll
)

// addRoute 添加路由
func (n *node) addRoute(path string, handlers ...fasthttp.RequestHandler) {
	fullPath := path
	n.priority++
	numParams := countParams(path)

	// 空树特殊情况
	if len(n.path) == 0 && len(n.children) == 0 {
		n.insertChild(numParams, path, fullPath, handlers)
		n.nType = root
		return
	}

walk:
	for {
		// 更新最大参数数
		if numParams > n.maxParams {
			n.maxParams = numParams
		}

		// 查找最长公共前缀
		i := 0
		max := min(len(path), len(n.path))
		for i < max && path[i] == n.path[i] {
			i++
		}

		if i < len(n.path) {
			// 分裂节点
			child := node{
				path:      n.path[i:],
				wildChild: n.wildChild,
				indices:   n.indices,
				children:  n.children,
				handlers:  n.handlers,
				priority:  n.priority - 1,
			}

			// 更新当前节点
			n.children = []*node{&child}
			n.indices = string([]byte{n.path[i]})
			n.path = path[:i]
			n.handlers = nil
			n.wildChild = false
		}

		if i < len(path) {
			path = path[i:]

			if n.wildChild {
				// 处理通配符子节点
				n = n.children[0]
				n.priority++

				// 检查通配符匹配
				if len(path) >= len(n.path) && n.path == path[:len(n.path)] &&
					// 检查更长的通配符段
					(len(n.path) >= len(path) || path[len(n.path)] == '/') {
					continue walk
				} else {
					// 找不到匹配，需要插入通配符节点
					pathSeg := path
					if n.nType != catchAll {
						pathSeg = strings.SplitN(path, "/", 2)[0]
					}
					prefix := fullPath[:strings.Index(fullPath, pathSeg)] + n.path

					panic("'" + pathSeg +
						"' in new path '" + fullPath +
						"' conflicts with existing wildcard '" + n.path +
						"' in existing prefix '" + prefix +
						"'")
				}
			}

			c := path[0]

			// 检查路径参数
			if n.nType == param && c == '/' && len(n.children) == 1 {
				n = n.children[0]
				n.priority++
				continue walk
			}

			// 检查子节点
			for i := 0; i < len(n.indices); i++ {
				if c == n.indices[i] {
					i = n.incrementChildPrio(i)
					n = n.children[i]
					continue walk
				}
			}

			// 如果是路径参数
			if c != ':' && c != '*' {
				// 创建子节点
				n.indices += string([]byte{c})
				child := &node{}
				n.children = append(n.children, child)
				n.incrementChildPrio(len(n.indices) - 1)
				n = child
			}
			n.insertChild(numParams, path, fullPath, handlers)
			return

		} else if i == len(path) {
			// 路径完全匹配
			if n.handlers != nil {
				panic("handlers are already registered for path '" + fullPath + "'")
			}
			n.handlers = handlers
			return
		}
	}
}

// insertChild 插入子节点
func (n *node) insertChild(numParams uint8, path, fullPath string, handlers []fasthttp.RequestHandler) {
	var offset int // 已经处理的字节数

	for numParams > 0 {
		// 查找参数边界
		var wildcard string
		if path[offset] == ':' { // 参数
			wildcard, offset = getNextParam(path, offset)
			numParams--

			child := &node{
				nType:     param,
				path:      wildcard,
				maxParams: numParams,
			}
			n.children = []*node{child}
			n.wildChild = true
			n = child
			n.priority++
		} else { // catchAll
			wildcard = path[offset+1:]
			offset = len(path)

			if len(wildcard) < 1 {
				panic("catch-all routes must have non-empty path")
			}

			child := &node{
				nType:     catchAll,
				path:      wildcard,
				maxParams: numParams,
			}
			n.children = []*node{child}
			n.wildChild = true
			n = child
			n.priority++
		}
	}

	n.path = path[offset:]
	n.handlers = handlers
}

// getNextParam 获取下一个参数
func getNextParam(path string, start int) (string, int) {
	end := start + 1
	for end < len(path) && path[end] != '/' {
		end++
	}
	return path[start:end], end
}

// countParams 计算参数数量
func countParams(path string) uint8 {
	var n uint
	for i := 0; i < len(path); i++ {
		if path[i] != ':' && path[i] != '*' {
			continue
		}
		n++
	}
	if n >= 255 {
		return 255
	}
	return uint8(n)
}

// min 返回较小值
func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

// incrementChildPrio 增加子节点优先级
func (n *node) incrementChildPrio(pos int) int {
	n.children[pos].priority++
	prio := n.children[pos].priority

	// 调整位置以保持排序
	newPos := pos
	for newPos > 0 && n.children[newPos-1].priority < prio {
		// 交换节点
		n.children[newPos-1], n.children[newPos] = n.children[newPos], n.children[newPos-1]
		newPos--
	}

	if newPos != pos {
		// 交换索引
		n.indices = n.indices[:newPos] +
			string(n.indices[pos]) +
			n.indices[newPos:pos] +
			n.indices[pos+1:]
	}

	return newPos
}

// getValue 获取路由值
func (n *node) getValue(path string, ctx *fasthttp.RequestCtx) (fasthttp.RequestHandler, map[string]string, bool) {
walk: // 外部(根)循环
	for {
		// 如果路径完全匹配
		if path == n.path {
			// 我们应该已经到达包含处理器的节点。
			// 检查这个节点是否确实有处理器。如果没有找到，
			// 意味着我们遇到了一个不匹配项（例如，用户请求了/b/，
			// 但只有/b注册了处理器）
			if handlers := n.handlers; handlers != nil {
				return handlers[0], nil, false
			}

			// 没有找到处理器，检查是否有带斜杠的路径（但不是在根路径上）
			tsr := (path == "/" && n.wildChild && n.nType != root) ||
				(len(n.path) == len(path)+1 && n.path[len(path)] == '/' &&
					path == n.path[:len(n.path)-1] && n.handlers != nil)
			return nil, nil, tsr
		}

		if len(path) > len(n.path) && path[:len(n.path)] == n.path {
			path = path[len(n.path):]

			// 如果这个节点没有通配符子节点，
			// 我们可以只在子节点中查找下一个路径部分
			if !n.wildChild {
				c := path[0]
				indices := n.indices

				for i, max := 0, len(indices); i < max; i++ {
					if c == indices[i] {
						n = n.children[i]
						continue walk
					}
				}

				// 没有找到，如果存在以'/'结尾的相同路径，则建议添加尾部斜杠
				tsr := (path == "/" && n.handlers != nil) ||
					(len(n.path) == len(path)+1 && n.path[len(path)] == '/' &&
						path == n.path[:len(n.path)-1] && n.handlers != nil)
				return nil, nil, tsr
			}

			// 处理通配符子节点
			n = n.children[0]
			switch n.nType {
			case param:
				// 在路径段中查找下一个'/'
				end := 0
				for end < len(path) && path[end] != '/' {
					end++
				}

				// 保存参数值
				if ctx != nil {
					ctx.SetUserValue(n.path[1:], path[:end])
				}

				// 继续向下搜索
				if end < len(path) {
					if len(n.children) > 0 {
						path = path[end:]
						n = n.children[0]
						continue walk
					}

					// 没有找到处理器，检查是否有带斜杠的路径
					tsr := (len(path) == end+1 && path[end] == '/' && n.handlers != nil)
					return nil, nil, tsr
				}

				if handlers := n.handlers; handlers != nil {
					return handlers[0], nil, false
				} else if len(n.children) == 1 {
					// 没有处理器，但有子节点，检查是否有带斜杠的路径
					n = n.children[0]
					tsr := (n.path == "/" && n.handlers != nil) ||
						(len(n.path) == 1 && n.path[0] == '/' && n.handlers != nil)
					return nil, nil, tsr
				}

				return nil, nil, false

			case catchAll:
				// 保存参数值
				if ctx != nil {
					ctx.SetUserValue(n.path, path)
				}

				handlers := n.handlers
				if handlers == nil {
					return nil, nil, false
				}
				return handlers[0], nil, false

			default:
				panic("invalid node type")
			}
		}

		// 没有找到处理器，检查是否有带斜杠的路径
		tsr := (path == "/" && n.handlers != nil) ||
			(len(n.path) == len(path)+1 && n.path[len(path)] == '/' &&
				path == n.path[:len(n.path)-1] && n.handlers != nil)
		return nil, nil, tsr
	}
}

// 把单个字符串转换成目标类型
func convert(t reflect.Type, s string) (reflect.Value, error) {
	switch t.Kind() {
	case reflect.Int, reflect.Int64:
		v, err := strconv.ParseInt(s, 10, 64)
		return reflect.ValueOf(v).Convert(t), err
	case reflect.String:
		return reflect.ValueOf(s), nil
	case reflect.Bool:
		v, err := strconv.ParseBool(s)
		return reflect.ValueOf(v), err
	case reflect.Float64:
		v, err := strconv.ParseFloat(s, 64)
		return reflect.ValueOf(v), err
	}
	return reflect.Zero(t), fmt.Errorf("unsupported path var type: %v", t.Kind())
}

// 解析路径变量名，如 /user/:id/file/*path -> ["id","path"]
func parsePathParams(pattern string) []string {
	var names []string
	parts := strings.Split(pattern, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, ":") {
			names = append(names, p[1:])
		}
		if strings.HasPrefix(p, "*") {
			names = append(names, p[1:])
		}
	}
	return names
}
