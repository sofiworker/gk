package ghttp

import (
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// setValueFromString 将字符串值按字段类型写入 reflect.Value。
// setValueFromString writes a string value into a reflect.Value by field kind.
// 支持 string/bool/整数/浮点与指针字段;不支持的类型返回错误。
// it supports string/bool/int/float and pointer fields; unsupported kinds error.
func setValueFromString(field reflect.Value, value string) error {
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return setValueFromString(field.Elem(), value)
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
		return nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(parsed)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(parsed)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(value, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(parsed)
		return nil
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(parsed)
		return nil
	default:
		return fmt.Errorf("unsupported kind %s", field.Kind())
	}
}

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
	target, err := indirectDecodeValue(v)
	if err != nil {
		return fmt.Errorf("form codec: %w", err)
	}
	switch {
	case target.Type() == reflect.TypeOf(url.Values{}):
		target.Set(reflect.ValueOf(values))
		return nil
	case target.Type() == reflect.TypeOf(map[string]string{}):
		out := make(map[string]string, len(values))
		for k, vs := range values {
			out[k] = vs[0]
		}
		target.Set(reflect.ValueOf(out))
		return nil
	case target.Kind() == reflect.String:
		target.SetString(string(data))
		return nil
	case target.Kind() == reflect.Slice && target.Type().Elem().Kind() == reflect.Uint8:
		target.SetBytes(data)
		return nil
	case target.Kind() == reflect.Struct:
		return fillFormStruct(values, target)
	default:
		return fmt.Errorf("form codec: unsupported target %T", v)
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
