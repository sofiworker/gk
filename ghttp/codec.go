package ghttp

import (
	"fmt"
	"io"
	"reflect"
)

// Codec 处理 HTTP 内容编码/解码。
// Codec handles HTTP content encoding/decoding.
// 每个 Codec 关联一个或多个 Content-Type。
// Each Codec is associated with one or more Content-Types.
type Codec interface {
	ContentTypes() []string
	Marshal(w io.Writer, v interface{}) error
	Unmarshal(r io.Reader, v interface{}) error
}

func indirectDecodeValue(target interface{}) (reflect.Value, error) {
	value := reflect.ValueOf(target)
	if !value.IsValid() || value.Kind() != reflect.Ptr || value.IsNil() {
		return reflect.Value{}, fmt.Errorf("decode target must be a non-nil pointer, got %T", target)
	}
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			if !value.CanSet() {
				return reflect.Value{}, fmt.Errorf("decode target %T is not settable", target)
			}
			value.Set(reflect.New(value.Type().Elem()))
		}
		value = value.Elem()
	}
	return value, nil
}
