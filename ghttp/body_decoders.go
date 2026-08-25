package ghttp

import (
	"fmt"
	"io"
	"mime/multipart"
	"reflect"
	"strconv"
	"strings"
)

// defaultMaxMultipartMemory 是解析 multipart 表单时驻留内存的上限,超出部分写入临时文件
// (与 net/http 默认 32MiB 一致)。
// defaultMaxMultipartMemory caps the in-memory portion when parsing a multipart
// form; the rest spills to temp files (matches net/http's 32 MiB default).
const defaultMaxMultipartMemory = 32 << 20

// uploadType / sliceUploadType 是 Upload 与 []Upload 的反射类型,供 form 解码识别文件字段。
// 包级缓存,避免每次解码重复 reflect.TypeOf。
// uploadType / sliceUploadType are the reflect types of Upload and []Upload, used
// by form decoding to recognize file fields. Package-level cache to avoid
// repeating reflect.TypeOf on every decode.
var (
	uploadType      = reflect.TypeOf(Upload{})
	sliceUploadType = reflect.TypeOf([]Upload(nil))
)

// ——— FormBody ——— //

// formCodec 解码 application/x-www-form-urlencoded 与 multipart/form-data 到 struct:
// 文本字段(form tag)绑标量,Upload / []Upload 字段(form tag)绑上传文件。urlencoded 只有
// 文本字段;multipart 同时含文本(MultipartForm.Value)与文件(MultipartForm.File)。
// formCodec decodes application/x-www-form-urlencoded and multipart/form-data
// into a struct: text fields (form tag) bind scalars, and Upload / []Upload
// fields (form tag) bind uploaded files. urlencoded carries text only; multipart
// carries both text (MultipartForm.Value) and files (MultipartForm.File).
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
		if err := req.ParseMultipartForm(defaultMaxMultipartMemory); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		var files map[string][]*multipart.FileHeader
		if req.MultipartForm != nil {
			files = req.MultipartForm.File
		}
		return decodeFormStruct(req.MultipartForm.Value, files, v)
	}
	if err := req.ParseForm(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return decodeFormStruct(req.PostForm, nil, v)
}

// decodeFormStruct 把表单文本值与上传文件映射到 struct 的 form tag 字段上:标量字段走
// setScalarForm,Upload / []Upload 字段从 files 绑定。支持 string/bool/int/uint/float
// 标量;不递归内嵌结构体。文件缺失时保留零值(必填与否由后续校验层决定)。
// decodeFormStruct maps form text values and uploaded files onto a struct's
// form-tagged fields: scalar fields go through setScalarForm, and Upload /
// []Upload fields bind from files. Supports string/bool/int/uint/float scalars
// and does not recurse into embedded structs. A missing file leaves the zero
// value (whether it is required is decided by a later validation layer).
func decodeFormStruct(values map[string][]string, files map[string][]*multipart.FileHeader, dst any) error {
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
		fv := sv.Field(i)
		// Upload / []Upload 字段从 files 绑定;其余按标量从 values 绑定。
		// Upload / []Upload fields bind from files; others bind as scalars from values.
		switch ft.Type {
		case uploadType:
			if hs := files[name]; len(hs) > 0 {
				fv.Set(reflect.ValueOf(uploadFromHeader(hs[0])))
			}
			continue
		case sliceUploadType:
			if hs := files[name]; len(hs) > 0 {
				ups := make([]Upload, len(hs))
				for j, h := range hs {
					ups[j] = uploadFromHeader(h)
				}
				fv.Set(reflect.ValueOf(ups))
			}
			continue
		}
		vv, ok := values[name]
		if !ok || len(vv) == 0 {
			continue
		}
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
