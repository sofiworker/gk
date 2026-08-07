package ghttp

import (
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"
)

// FormCodec 处理 application/x-www-form-urlencoded 表单。
// FormCodec handles application/x-www-form-urlencoded.
type FormCodec struct{}

func (c *FormCodec) ContentTypes() []string {
	return []string{"application/x-www-form-urlencoded"}
}

func (c *FormCodec) Marshal(w io.Writer, v interface{}) error {
	switch val := v.(type) {
	case url.Values:
		_, err := io.WriteString(w, val.Encode())
		return err
	case map[string]string:
		vals := url.Values{}
		for k, v := range val {
			vals.Set(k, v)
		}
		_, err := io.WriteString(w, vals.Encode())
		return err
	}
	return nil
}

func (c *FormCodec) Unmarshal(r io.Reader, v interface{}) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}
	switch target := v.(type) {
	case *url.Values:
		*target = values
		return nil
	case *map[string]string:
		out := make(map[string]string, len(values))
		for k, vs := range values {
			out[k] = vs[0]
		}
		*target = out
		return nil
	case *string:
		*target = string(data)
		return nil
	case *[]byte:
		*target = data
		return nil
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Ptr || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
			return fmt.Errorf("form codec: unsupported target %T", v)
		}
		return fillFormStruct(values, rv.Elem())
	}
}

func fillFormStruct(values url.Values, target reflect.Value) error {
	t := target.Type()
	hasTag := false
	hasBindable := false
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if isFormBindableField(f.Type) {
			hasBindable = true
		}
		name, ok := formFieldName(f)
		if !ok {
			continue
		}
		hasTag = true
		value := values.Get(name)
		if value == "" {
			value = f.Tag.Get("default")
		}
		if value == "" {
			continue
		}
		if err := setValueFromString(target.Field(i), value); err != nil {
			return fmt.Errorf("bind form %q to %s: %w", name, f.Name, err)
		}
	}
	if hasBindable && !hasTag {
		return fmt.Errorf("form codec: struct %s has bindable fields but no form tags", t.Name())
	}
	return nil
}

func isFormBindableField(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func formFieldName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("form")
	if tag == "" || tag == "-" {
		return "", false
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return "", false
	}
	return name, true
}
