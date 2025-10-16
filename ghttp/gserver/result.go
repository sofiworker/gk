package gserver

type JsonResult struct {
	Data interface{}
	Code int
}

type StringResult struct {
	Format string
	Data   []interface{}
	Code   int
}

type HtmlResult struct {
	Template string
	Data     interface{}
	Code     int
}

type ErrorResult struct {
	Err  error
	Code int
	Msg  string
}

type RedirectResult struct {
	URL  string
	Code int
}

type EmptyResult struct{}

func (r *JsonResult) Execute(c *Context) {
	// 这里简化实现，实际需要完整的JSON序列化
	//c.Writer.Header().Set("Content-Type", "application/json")
	//c.Writer.WriteHeader(r.Code)
	// 序列化r.Data并写入...
}

func (r *StringResult) Execute(c *Context) {
	//c.Writer.Header().Set("Content-Type", "text/plain")
	//c.Writer.WriteHeader(r.Code)
	// 格式化字符串并写入...
}

func (r *HtmlResult) Execute(c *Context) {
	//c.Writer.Header().Set("Content-Type", "text/html")
	//c.Writer.WriteHeader(r.Code)
	// 渲染模板...
}

func (r *ErrorResult) Execute(c *Context) {
	//c.Writer.Header().Set("Content-Type", "application/json")
	//c.Writer.WriteHeader(r.Code)
	// 错误信息序列化...
}

func (r *RedirectResult) Execute(c *Context) {
	//http.Redirect(c.Writer, c.Request, r.URL, r.Code)
}

func (r *EmptyResult) Execute(c *Context) {
	// 无操作
}

func JSON(data interface{}) Result {
	return &JsonResult{Data: data, Code: 200}
}

func JSONCode(data interface{}, code int) Result {
	return &JsonResult{Data: data, Code: code}
}

func String(format string, data ...interface{}) Result {
	return &StringResult{Format: format, Data: data, Code: 200}
}

func HTML(template string, data interface{}) Result {
	return &HtmlResult{Template: template, Data: data, Code: 200}
}

func Error(err error) Result {
	return &ErrorResult{Err: err, Code: 500, Msg: err.Error()}
}

func ErrorMsg(msg string) Result {
	return &ErrorResult{Msg: msg, Code: 500}
}

func ErrorCode(err error, code int) Result {
	return &ErrorResult{Err: err, Code: code, Msg: err.Error()}
}

func Redirect(url string) Result {
	return &RedirectResult{URL: url, Code: 302}
}
