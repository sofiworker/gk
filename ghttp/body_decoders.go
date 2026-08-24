package ghttp

import (
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// ——— FormBody ——— //

// formCodec 解码 application/x-www-form-urlencoded 与 multipart/form-data 的文本字段
// 到 struct 中(form tag)。urlencoded 走 ParseForm+PostForm;multipart 走
// ParseMultipartForm+MultipartForm.Value。
// formCodec decodes application/x-www-form-urlencoded and multipart/form-data
// text fields into a struct (form tag). urlencoded uses ParseForm+PostForm;
// multipart uses ParseMultipartForm+MultipartForm.Value.
type formCodec struct{}

// ContentType 返回空串:formCodec 同时接受 urlencoded 与 multipart 两种 Content-Type,
// 由 Decode 内部按类型分派,故不参与 415 严格校验。
// ContentType returns empty: formCodec accepts both urlencoded and multipart
// Content-Types, dispatched inside Decode, so it opts out of the strict 415
// check.
func (formCodec) ContentType() string { return "" }

func (formCodec) Decode(req *Request, v any) error {
	if req.Body == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidInput)
	}
	if mediaType(req.Header.Get("Content-Type")) == "multipart/form-data" {
		if err := req.ParseMultipartForm(32 << 20); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return decodeFormStruct(req.MultipartForm.Value, v)
	}
	if err := req.ParseForm(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return decodeFormStruct(req.PostForm, v)
}

// FormBody 返回一个表单请求体解码器，供 PostBody/PostParamsBody 等入口的 dec 参数使用。
// FormBody returns a form request-body decoder for the dec parameter of entries
// like PostBody/PostParamsBody. It calls req.ParseForm() at decode time, so it
// works for both urlencoded and multipart text fields.
func FormBody() RequestDecoder { return formCodec{} }

// decodeFormStruct 把 url.Values 映射到 struct 的 form tag 字段上。
// 支持 string/bool/int/uint/float 类型;不支持内嵌结构体与 slice。
// decodeFormStruct maps url.Values to a struct's form-tagged fields.
// It supports string/bool/int/uint/float and does not recurse into embedded
// structs or slices.
func decodeFormStruct(values map[string][]string, dst any) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("%w: decodeFormStruct target must be a pointer to struct, got %T", ErrInvalidInput, dst)
	}
	sv := rv.Elem()
	st := sv.Type()
	for i := 0; i < st.NumField(); i++ {
		ft := st.Field(i)
		tag := ft.Tag.Get("form")
		if tag == "" || !ft.IsExported() {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "-" {
			continue
		}
		vv, ok := values[name]
		if !ok || len(vv) == 0 {
			continue
		}
		fv := sv.Field(i)
		if err := setScalarForm(fv, ft.Type.Kind(), vv[0], name); err != nil {
			return err
		}
	}
	return nil
}

// setScalarForm 是 form 解码的标量赋值器,与 setScalar 共用解析逻辑但错误信息定位为 "form"。
// setScalarForm is the scalar setter for form decoding, sharing parse logic
// with setScalar but labelling errors as "form".
func setScalarForm(fv reflect.Value, kind reflect.Kind, raw, name string) error {
	switch kind {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: form %q: %v", ErrInvalidInput, name, err)
		}
		if fv.OverflowInt(n) {
			return fmt.Errorf("%w: form %q: value out of range", ErrInvalidInput, name)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: form %q: %v", ErrInvalidInput, name, err)
		}
		if fv.OverflowUint(n) {
			return fmt.Errorf("%w: form %q: value out of range", ErrInvalidInput, name)
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("%w: form %q: %v", ErrInvalidInput, name, err)
		}
		fv.SetFloat(f)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("%w: form %q: %v", ErrInvalidInput, name, err)
		}
		fv.SetBool(b)
	}
	return nil
}

// ——— TextBody ——— //

// textCodec 解码任意文本请求体为 string 或 []byte。
// 目标类型(*string 或 *[]byte)在请求期根据用户类型参数自动判定。
// textCodec decodes an arbitrary text request body into a string or []byte.
// The target type (*string or *[]byte) is determined at request time by the
// user's type parameter.
type textCodec struct{}

func (textCodec) ContentType() string { return "text/plain" }

func (textCodec) Decode(req *Request, v any) error {
	if req.Body == nil {
		return fmt.Errorf("%w: empty body", ErrInvalidInput)
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	switch dst := v.(type) {
	case *string:
		*dst = string(data)
		return nil
	case *[]byte:
		*dst = data
		return nil
	default:
		return fmt.Errorf("%w: TextBody target must be *string or *[]byte, got %T", ErrInvalidInput, v)
	}
}

// TextBody 返回一个纯文本请求体解码器，供 PostBody/PostParamsBody 等入口的 dec 参数使用。
// 目标类型为 string 或 []byte。
// TextBody returns a plain-text request-body decoder for the dec parameter of
// entries like PostBody/PostParamsBody. The target type must be string or []byte.
func TextBody() RequestDecoder { return textCodec{} }
