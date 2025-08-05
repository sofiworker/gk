package ghttp

import (
	"encoding/json"
	"errors"
	"github.com/valyala/fasthttp"
	"reflect"
	"strconv"
	"strings"
)

type Router struct {
	tree map[string]fasthttp.RequestHandler
}

func New() *Router { return &Router{tree: make(map[string]fasthttp.RequestHandler)} }

// GET 注册 GET 路由
func (r *Router) GET(path string, fn interface{}) {
	r.handle("GET", path, fn)
}

// POST 注册 POST 路由
func (r *Router) POST(path string, fn interface{}) {
	r.handle("POST", path, fn)
}

// Serve 返回 fasthttp 统一入口
func (r *Router) Serve() fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		key := string(ctx.Method()) + ":" + string(ctx.Path())
		if h, ok := r.tree[key]; ok {
			h(ctx)
			return
		}
		ctx.Error("404 Not Found", fasthttp.StatusNotFound)
	}
}

// ---------------- 内部实现 ----------------
func (r *Router) handle(method, path string, fn interface{}) {
	val := reflect.ValueOf(fn)
	typ := val.Type()
	if typ.Kind() != reflect.Func {
		panic("handler must be func")
	}

	// 预计算路径参数位置
	paramNames := parsePathParams(path)

	// 生成零反射的 handler
	handler := func(ctx *fasthttp.RequestCtx) {
		args, err := buildArgs(typ, paramNames, ctx)
		if err != nil {
			ctx.Error(err.Error(), fasthttp.StatusBadRequest)
			return
		}

		rets := val.Call(args)
		writeResponse(ctx, rets)
	}

	// 保存到路由树
	r.tree[method+":"+path] = handler
}

// 根据函数签名 + 路径变量位置，构造实参列表
func buildArgs(fnType reflect.Type, paramNames []string, ctx *fasthttp.RequestCtx) ([]reflect.Value, error) {
	numIn := fnType.NumIn()
	args := make([]reflect.Value, numIn)
	idx := 0 // 已处理的路径变量计数

	for i := 0; i < numIn; i++ {
		argT := fnType.In(i)

		// 1) *fasthttp.RequestCtx 直接注入
		if argT == reflect.TypeOf((*fasthttp.RequestCtx)(nil)) {
			args[i] = reflect.ValueOf(ctx)
			continue
		}

		// 2) 路径变量
		if idx < len(paramNames) {
			seg := ctx.UserValue(paramNames[idx]).(string)
			v, err := convert(argT, seg)
			if err != nil {
				return nil, err
			}
			args[i] = v
			idx++
			continue
		}

		// 3) 请求体绑定（简单只支持 JSON）
		if argT.Kind() == reflect.Ptr {
			ptr := reflect.New(argT.Elem()).Interface()
			if err := json.Unmarshal(ctx.PostBody(), ptr); err != nil {
				return nil, err
			}
			args[i] = reflect.ValueOf(ptr).Elem().Addr()
			continue
		}

		// 4) 原始 []byte
		if argT == reflect.TypeOf([]byte(nil)) {
			args[i] = reflect.ValueOf(ctx.PostBody())
			continue
		}

		// 5) 字符串
		if argT.Kind() == reflect.String {
			args[i] = reflect.ValueOf(string(ctx.PostBody()))
			continue
		}

		return nil, errors.New("unsupported parameter type")
	}
	return args, nil
}

// 把单个字符串转换成目标类型
func convert(t reflect.Type, s string) (reflect.Value, error) {
	switch t.Kind() {
	case reflect.Int, reflect.Int64:
		v, err := strconv.ParseInt(s, 10, 64)
		return reflect.ValueOf(v).Convert(t), err
	case reflect.String:
		return reflect.ValueOf(s), nil
	}
	return reflect.Zero(t), errors.New("unsupported path var type")
}

// 写回响应 + 错误处理
func writeResponse(ctx *fasthttp.RequestCtx, rets []reflect.Value) {
	// 如果最后一个返回值是 error 且非 nil
	if len(rets) > 0 {
		last := rets[len(rets)-1]
		if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !last.IsNil() {
				ctx.SetStatusCode(fasthttp.StatusBadRequest)
				ctx.SetBodyString(last.Interface().(error).Error())
				return
			}
		}
	}

	// 正常写回
	if len(rets) > 0 {
		val := rets[0].Interface()
		switch v := val.(type) {
		case string:
			ctx.SetBodyString(v)
		case []byte:
			ctx.SetBody(v)
		default:
			b, _ := json.Marshal(v)
			ctx.SetBody(b)
		}
	}
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
