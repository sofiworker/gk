package example

import (
	"net/http"
	"reflect"
)

type Context struct {
}

func (c *Context) JSON(int, interface{}) error {
	return nil
}

// Handler 核心接口
type Handler interface {
	Handle(ctx *Context) error
}

// 子接口定义
type ParameterBinder interface {
	Bind(ctx *Context) ([]interface{}, error)
}

type BusinessLogic interface {
	Execute(params ...interface{}) (interface{}, error)
}

type ResponseWriter interface {
	Write(ctx *Context, result interface{}, err error) error
}

type ErrorHandler interface {
	HandleError(ctx *Context, err error) error
}

// 参数绑定实现
type ReflectionBinder struct {
	paramTypes []reflect.Type
}

func (b *ReflectionBinder) Bind(ctx *Context) ([]interface{}, error) {
	params := make([]interface{}, len(b.paramTypes))
	//for i, paramType := range b.paramTypes {
	//value, err := bindParam(ctx, paramType)
	//if err != nil {
	//	return nil, err
	//}
	//params[i] = value
	//}
	return params, nil
}

// 业务逻辑包装器
type FunctionLogic struct {
	fn interface{}
}

func (l *FunctionLogic) Execute(params ...interface{}) (interface{}, error) {
	//fnValue := reflect.ValueOf(l.fn)
	//args := make([]reflect.Value, len(params))
	//for i, param := range params {
	//	args[i] = reflect.ValueOf(param)
	//}

	//results := fnValue.Call(args)
	//return parseResults(results)
	return nil, nil
}

// JSON响应写入器
type JSONResponseWriter struct{}

func (w *JSONResponseWriter) Write(ctx *Context, result interface{}, err error) error {
	if err != nil {
		return w.writeError(ctx, err)
	}
	return w.writeSuccess(ctx, result)
}

func (w *JSONResponseWriter) writeSuccess(ctx *Context, data interface{}) error {
	response := map[string]interface{}{
		"success": true,
		"data":    data,
	}
	return ctx.JSON(http.StatusOK, response)
}

func (w *JSONResponseWriter) writeError(ctx *Context, err error) error {
	// 错误处理逻辑
	return ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
		"success": false,
		"error":   err.Error(),
	})
}

// CompositeHandler 组合式Handler
type CompositeHandler struct {
	Binder        ParameterBinder
	Logic         BusinessLogic
	Writer        ResponseWriter
	ErrorHandlers []ErrorHandler
}

func (h *CompositeHandler) Handle(ctx *Context) error {
	// 1. 参数绑定
	params, err := h.Binder.Bind(ctx)
	if err != nil {
		return h.handleError(ctx, err)
	}

	// 2. 执行业务逻辑
	result, err := h.Logic.Execute(params...)
	if err != nil {
		return h.handleError(ctx, err)
	}

	// 3. 写入响应
	return h.Writer.Write(ctx, result, nil)
}

func (h *CompositeHandler) handleError(ctx *Context, err error) error {
	for _, errorHandler := range h.ErrorHandlers {
		if handled := errorHandler.HandleError(ctx, err); handled {
			return nil
		}
	}
	return h.Writer.Write(ctx, nil, err)
}

// HandlerBuilder Handler构建器
type HandlerBuilder struct {
	binder        ParameterBinder
	writer        ResponseWriter
	errorHandlers []ErrorHandler
}

func NewHandlerBuilder() *HandlerBuilder {
	return &HandlerBuilder{
		writer:        &JSONResponseWriter{},
		errorHandlers: []ErrorHandler{&DefaultErrorHandler{}},
	}
}

// 基于函数创建组合Handler
func (b *HandlerBuilder) FromFunction(fn interface{}) Handler {
	fnType := reflect.TypeOf(fn)

	// 分析函数参数类型
	paramTypes := make([]reflect.Type, fnType.NumIn())
	for i := 0; i < fnType.NumIn(); i++ {
		paramTypes[i] = fnType.In(i)
	}

	binder := &ReflectionBinder{paramTypes: paramTypes}
	logic := &FunctionLogic{fn: fn}

	return &CompositeHandler{
		Binder:        binder,
		Logic:         logic,
		Writer:        b.writer,
		ErrorHandlers: b.errorHandlers,
	}
}

// 链式配置方法
func (b *HandlerBuilder) WithBinder(binder ParameterBinder) *HandlerBuilder {
	b.binder = binder
	return b
}

func (b *HandlerBuilder) WithWriter(writer ResponseWriter) *HandlerBuilder {
	b.writer = writer
	return b
}

func (b *HandlerBuilder) WithErrorHandler(handler ErrorHandler) *HandlerBuilder {
	b.errorHandlers = append(b.errorHandlers, handler)
	return b
}

// Router 路由器
type Router struct {
	builder *HandlerBuilder
}

func NewRouter() *Router {
	return &Router{
		builder: NewHandlerBuilder(),
	}
}

// 注册方法 - 直接传入函数
func (r *Router) GET(path string, handler interface{}) {
	r.addRoute("GET", path, handler)
}

func (r *Router) POST(path string, handler interface{}) {
	r.addRoute("POST", path, handler)
}

func (r *Router) PUT(path string, handler interface{}) {
	r.addRoute("PUT", path, handler)
}

func (r *Router) DELETE(path string, handler interface{}) {
	r.addRoute("DELETE", path, handler)
}

// 支持自定义配置的注册
func (r *Router) Handle(method, path string, handler interface{}, configs ...func(*HandlerBuilder)) {
	builder := r.builder
	for _, config := range configs {
		config(builder)
	}
	compositeHandler := builder.FromFunction(handler)
	r.addRoute(method, path, compositeHandler)
}

func (r *Router) addRoute(method, path string, handler interface{}) {
	var h Handler
	switch handler := handler.(type) {
	case Handler:
		h = handler
	case func(*Context) error:
		h = HandlerFunc(handler)
	default:
		h = r.builder.FromFunction(handler)
	}
	// 添加到路由表...
}

// 查询处理器接口
type QueryHandler interface {
	BusinessLogic
	Validation
}

// 命令处理器接口
type CommandHandler interface {
	BusinessLogic
	Authorization
	Transaction
}

// 文件处理器接口
type FileHandler interface {
	BusinessLogic
	ResponseWriter
	ContentType
	string
}

// 特定场景的组合
type RESTHandler struct {
	QueryHandler
	JSONResponseWriter
	DefaultErrorHandler
}

type GraphQLHandler struct {
	BusinessLogic
	GraphQLResponseWriter
	Validation
}

// 用户定义的业务函数（纯函数，无框架依赖）
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// 简单的查询函数
func GetUser(id string) (*User, error) {
	// 纯业务逻辑
	return &User{ID: id, Name: "John"}, nil
}

// 带结构体绑定的函数
func CreateUser(user *User) error {
	// 创建用户逻辑
	return nil
}

// 复杂函数签名
func UpdateUser(id string, user *User) (int, *User, error) {
	// 更新逻辑
	return http.StatusOK, user, nil
}

// 路由设置
func setupRoutes() {
	router := NewRouter()

	// 简单注册 - 自动适配
	router.GET("/users/:id", GetUser)
	router.POST("/users", CreateUser)
	router.PUT("/users/:id", UpdateUser)

	// 自定义配置注册
	router.Handle("GET", "/admin/users/:id", GetUser,
		func(b *HandlerBuilder) {
			b.WithWriter(&XMLResponseWriter{})
			b.WithErrorHandler(&AdminErrorHandler{})
		})

	// 使用闭包函数
	router.GET("/health", func() (string, error) {
		return "OK", nil
	})

	// 直接使用Handler接口
	customHandler := &CustomUserHandler{}
	router.GET("/custom/:id", customHandler)

	// 在路由中使用专用组合
	router.AddHandler("GET", "/download/:file",
		NewFileDownloadHandler(downloadLogic))
}

// 创建专用的Handler组合
func NewRESTHandler(logic BusinessLogic) Handler {
	return &CompositeHandler{
		Binder: &JSONBinder{},
		Logic:  logic,
		Writer: &JSONResponseWriter{},
		ErrorHandlers: []ErrorHandler{
			&ValidationErrorHandler{},
			&BusinessErrorHandler{},
			&DefaultErrorHandler{},
		},
	}
}

func NewFileDownloadHandler(logic BusinessLogic) Handler {
	return &CompositeHandler{
		Binder: &PathParamBinder{},
		Logic:  logic,
		Writer: &FileResponseWriter{},
		ErrorHandlers: []ErrorHandler{
			&NotFoundErrorHandler{},
			&DefaultErrorHandler{},
		},
	}
}
