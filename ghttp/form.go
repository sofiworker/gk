package ghttp

import (
	"net/http"
	"reflect"
	"strconv"
)

const defaultMaxMemory = 32 << 20 // 32 MB

func parseMultipartForm(r *http.Request, target interface{}) error {
	if err := r.ParseMultipartForm(defaultMaxMemory); err != nil {
		return err
	}

	v := reflect.ValueOf(target)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	return fillMultipartBodyFields(v, r)
}

func fillMultipartBodyFields(parent reflect.Value, r *http.Request) error {
	if parent.Kind() != reflect.Struct {
		return nil
	}

	for i := 0; i < parent.NumField(); i++ {
		field := parent.Type().Field(i)
		if field.Name != "Body" {
			continue
		}
		return fillMultipartBody(parent.Field(i), r)
	}
	return nil
}

func fillMultipartBody(bodyVal reflect.Value, r *http.Request) error {
	if bodyVal.Kind() != reflect.Struct {
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
			file, header, err := r.FormFile(tag)
			if err != nil {
				continue
			}
			file.Close()
			fv.Set(reflect.ValueOf(&FileHeader{FileHeader: header}))
			continue
		}

		if fv.Type() == reflect.TypeOf([]*FileHeader{}) {
			files := r.MultipartForm.File[tag]
			fhs := make([]*FileHeader, 0, len(files))
			for _, f := range files {
				fhs = append(fhs, &FileHeader{FileHeader: f})
			}
			fv.Set(reflect.ValueOf(fhs))
			continue
		}

		val := r.FormValue(tag)
		if val == "" {
			continue
		}
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(val)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, _ := strconv.ParseInt(val, 10, 64)
			fv.SetInt(n)
		case reflect.Slice:
			if fv.Type().Elem().Kind() == reflect.String {
				vals := r.Form[tag]
				fv.Set(reflect.ValueOf(vals))
			}
		}
	}
	return nil
}
