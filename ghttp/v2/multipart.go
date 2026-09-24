package v2

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
)

// MultipartLimits 分别限制整个请求体与文件驻留内存，超出内存部分写入临时文件。
// MultipartLimits bounds the entire body and file memory; excess file data spills to disk.
type MultipartLimits struct {
	MaxBytes    int64
	MemoryBytes int64
}

// MultipartInput 绑定 form 标签及文件字段；省略限制时使用 32 MiB 请求上限与 1 MiB 内存。
// MultipartInput binds form tags and files, defaulting to a 32 MiB body and 1 MiB file memory.
func MultipartInput[T any](limits ...MultipartLimits) Input[T] {
	l := MultipartLimits{MaxBytes: 32 << 20, MemoryBytes: 1 << 20}
	if len(limits) > 0 {
		l = limits[0]
	}
	plan, err := compileMultipartPlan(reflect.TypeFor[T]())
	return CustomInput[T](multipartDecoder[T]{limits: l, plan: plan, err: err})
}

type multipartFileField struct {
	index    []int
	name     string
	multiple bool
}

type multipartPlan struct {
	form  formPlan
	files []multipartFileField
}

func compileMultipartPlan(typ reflect.Type) (multipartPlan, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return multipartPlan{}, fmt.Errorf("ghttp/v2: multipart input requires a struct, got %s", typ)
	}
	var plan multipartPlan
	fileType := reflect.TypeFor[*multipart.FileHeader]()
	filesType := reflect.TypeFor[[]*multipart.FileHeader]()
	selectFile := func(f reflect.StructField, index []int, name string) bool {
		if f.Type != fileType && f.Type != filesType {
			return false
		}
		plan.files = append(plan.files, multipartFileField{index: index, name: name, multiple: f.Type == filesType})
		return true
	}
	if err := compileFormFields(typ, nil, map[reflect.Type]bool{}, &plan.form, selectFile); err != nil {
		return multipartPlan{}, err
	}
	return plan, nil
}

type multipartDecoder[T any] struct {
	limits MultipartLimits
	plan   multipartPlan
	err    error
}

func (multipartDecoder[T]) ContentType() string { return "multipart/form-data" }

func (d multipartDecoder[T]) registrationError() error {
	if d.err != nil {
		return d.err
	}
	if d.limits.MaxBytes <= 0 || d.limits.MemoryBytes < 0 {
		return errors.New("ghttp/v2: invalid multipart limits")
	}
	return nil
}

func (d multipartDecoder[T]) Decode(req *Request, dst *T) error {
	if err := d.registrationError(); err != nil {
		return err
	}
	if req == nil || req.Request == nil || req.Body == nil {
		return errors.New("ghttp/v2: multipart decoder requires a request body")
	}
	req.Body = http.MaxBytesReader(nil, req.Body, d.limits.MaxBytes)
	if err := req.ParseMultipartForm(d.limits.MemoryBytes); err != nil {
		return fmt.Errorf("ghttp/v2: parse multipart: %w", err)
	}
	v := reflect.ValueOf(dst).Elem()
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if err := bindFormPlan(v, req.MultipartForm.Value, d.plan.form); err != nil {
		return err
	}
	for _, f := range d.plan.files {
		files := req.MultipartForm.File[f.name]
		if !f.multiple && len(files) > 1 {
			return fmt.Errorf("ghttp/v2: multiple files for field %s", f.name)
		}
		if f.multiple {
			fieldValue(v, f.index).Set(reflect.ValueOf(files))
		} else if len(files) == 1 {
			fieldValue(v, f.index).Set(reflect.ValueOf(files[0]))
		}
	}
	return nil
}
