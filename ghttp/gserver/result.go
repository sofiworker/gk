package gserver

import (
	"fmt"
	"net/http"
)

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

type EmptyResult struct {
	Code int
}

func (r *JsonResult) Execute(c *Context) {
	if c == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}
	//c.JSON(code, r.Data)
}

func (r *StringResult) Execute(c *Context) {
	if c == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}
	//c.String(code, r.Format, r.Data...)
}

func (r *HtmlResult) Execute(c *Context) {
	if c == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusOK
	}
	switch v := r.Data.(type) {
	case string:
		//c.HTML(code, v)
	case []byte:
		//c.Data(code, MIMEHTML+"; charset=utf-8", v)
	default:
		content := r.Template
		if content == "" && v != nil {
			content = fmt.Sprint(v)
		}
		//c.HTML(code, content)
	}
}

func (r *ErrorResult) Execute(c *Context) {
	if c == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusInternalServerError
	}
	msg := r.Msg
	if msg == "" && r.Err != nil {
		msg = r.Err.Error()
	}
	if msg == "" {
		msg = http.StatusText(code)
	}
	//c.AbortWithStatusJSON(code, map[string]interface{}{
	//	"error": msg,
	//})
}

func (r *RedirectResult) Execute(c *Context) {
	if c == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusFound
	}
	//c.Redirect(code, r.URL)
}

func (r *EmptyResult) Execute(c *Context) {
	if c == nil {
		return
	}
	code := r.Code
	if code == 0 {
		code = http.StatusNoContent
	}
	if c.Writer != nil && !c.Writer.Written() {
		//c.Status(code)
	}
}

func JSON(data interface{}) Result {
	return &JsonResult{Data: data, Code: http.StatusOK}
}

func JSONCode(data interface{}, code int) Result {
	return &JsonResult{Data: data, Code: code}
}

func String(format string, data ...interface{}) Result {
	return &StringResult{Format: format, Data: data, Code: http.StatusOK}
}

func HTML(template string, data interface{}) Result {
	return &HtmlResult{Template: template, Data: data, Code: http.StatusOK}
}

func Error(err error) Result {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &ErrorResult{Err: err, Code: http.StatusInternalServerError, Msg: msg}
}

func ErrorMsg(msg string) Result {
	return &ErrorResult{Msg: msg, Code: http.StatusInternalServerError}
}

func ErrorCode(err error, code int) Result {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &ErrorResult{Err: err, Code: code, Msg: msg}
}

func Redirect(url string) Result {
	return &RedirectResult{URL: url, Code: http.StatusFound}
}
