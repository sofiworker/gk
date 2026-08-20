package ghttp

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
)

// ErrInvalidBody 表示请求体无法读取或解码。
// ErrInvalidBody indicates the request body could not be read or decoded.
var ErrInvalidBody = errors.New("invalid request body")

const defaultMaxMemory = 32 << 20 // 32 MB

// fillMultipartBody 从已解析的 multipart 表单填充结构体,只依赖解析结果而不
// 依赖具体 *http.Request,保证 eager 与惰性两条路径、以及被 MaxBodyBytes
// 替换过请求拷贝的场景语义一致。
// fillMultipartBody fills a struct from a parsed multipart form using only the
// parse result, keeping eager and lazy paths consistent even when the request
// was replaced by the MaxBodyBytes wrapper.
func fillMultipartBody(bodyVal reflect.Value, form *multipart.Form) error {
	if !bodyVal.IsValid() {
		return fmt.Errorf("%w: multipart target must be a struct pointer", ErrInvalidBody)
	}
	targetType := bodyVal.Type()
	for targetType.Kind() == reflect.Ptr {
		targetType = targetType.Elem()
	}
	if targetType.Kind() != reflect.Struct {
		return fmt.Errorf("%w: multipart target must be a struct pointer", ErrInvalidBody)
	}
	for bodyVal.Kind() == reflect.Ptr {
		if bodyVal.IsNil() {
			if !bodyVal.CanSet() {
				return fmt.Errorf("multipart target %s is not settable", bodyVal.Type())
			}
			bodyVal.Set(reflect.New(bodyVal.Type().Elem()))
		}
		bodyVal = bodyVal.Elem()
	}
	if form == nil {
		return nil
	}

	for i := 0; i < bodyVal.NumField(); i++ {
		field := bodyVal.Type().Field(i)
		tag := field.Tag.Get("form")
		if tag == "" {
			continue
		}

		fv := bodyVal.Field(i)

		if fv.Type() == reflect.TypeOf(&FileHeader{}) {
			files := form.File[tag]
			if len(files) == 0 {
				continue
			}
			fv.Set(reflect.ValueOf(&FileHeader{FileHeader: files[0]}))
			continue
		}

		if fv.Type() == reflect.TypeOf([]*FileHeader{}) {
			files := form.File[tag]
			fhs := make([]*FileHeader, 0, len(files))
			for _, f := range files {
				fhs = append(fhs, &FileHeader{FileHeader: f})
			}
			fv.Set(reflect.ValueOf(fhs))
			continue
		}

		vals := form.Value[tag]
		if len(vals) == 0 {
			continue
		}
		val := vals[0]
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(val)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, _ := strconv.ParseInt(val, 10, 64)
			fv.SetInt(n)
		case reflect.Slice:
			if fv.Type().Elem().Kind() == reflect.String {
				fv.Set(reflect.ValueOf(vals))
			}
		}
	}
	return nil
}

// FormValues 返回合并 query 与表单体的参数,解析一次并缓存到 requestState;
// FormValues returns query-merged form values, parsed once and cached on
// requestState.
func FormValues(r *http.Request) (url.Values, error) {
	form, _, err := formValuesFromRequest(r)
	return form, err
}

// PostFormValues 返回仅来自请求体的表单参数(不含 query)。
// PostFormValues returns form values from the body only (excluding query).
func PostFormValues(r *http.Request) (url.Values, error) {
	_, post, err := formValuesFromRequest(r)
	return post, err
}

// MultipartForm 解析 multipart 表单,按 maxMemory 阈值落盘,结果缓存于
// requestState。同一请求首个 maxMemory 生效;必须在 RawBody 整读之前调用。
// MultipartForm parses a multipart form (spilling over maxMemory), cached on
// requestState. The first maxMemory wins; call it before RawBody drains the body.
func MultipartForm(r *http.Request, maxMemory int64) (*multipart.Form, error) {
	if r == nil {
		return nil, nil
	}
	if c := ctxFromRequest(r); c != nil {
		return c.multipartForm(maxMemory)
	}
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		return nil, err
	}
	return r.MultipartForm, nil
}

// formValuesFromRequest 返回 (merged, post) 两套缓存视图。
// formValuesFromRequest returns both cached views: (merged, post).
func formValuesFromRequest(r *http.Request) (url.Values, url.Values, error) {
	if r == nil {
		return nil, nil, nil
	}
	if c := ctxFromRequest(r); c != nil {
		return c.formValues()
	}
	if err := r.ParseForm(); err != nil {
		return nil, nil, err
	}
	return r.Form, r.PostForm, nil
}

// mergeURLValues 先复制 query,再追加表单体值(与 net/http Form 语义一致)。
// mergeURLValues copies query then appends body values (net/http Form semantics).
func mergeURLValues(query, post url.Values) url.Values {
	out := make(url.Values, len(query)+len(post))
	for k, vs := range query {
		out[k] = append([]string(nil), vs...)
	}
	for k, vs := range post {
		out[k] = append(out[k], vs...)
	}
	return out
}
