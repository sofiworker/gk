package codec

import (
	"fmt"
	"reflect"
)

type PlainCodec struct{}

var plainContentTypes = []string{
	"text/plain",
	"text/plain; charset=utf-8",
}

func NewPlainCodec() *PlainCodec {
	return &PlainCodec{}
}

func (p *PlainCodec) Encode(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		return val, nil
	case string:
		return []byte(val), nil
	case fmt.Stringer:
		return []byte(val.String()), nil
	case error:
		return []byte(val.Error()), nil
	default:
		return []byte(fmt.Sprint(v)), nil
	}
}

func (p *PlainCodec) Decode(data []byte, v interface{}) error {
	switch out := v.(type) {
	case *[]byte:
		*out = append((*out)[:0], data...)
	case *string:
		*out = string(data)
	case *interface{}:
		*out = string(data)
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Ptr && !rv.IsNil() {
			elem := rv.Elem()
			switch elem.Kind() {
			case reflect.String:
				elem.SetString(string(data))
			case reflect.Slice:
				if elem.Type().Elem().Kind() == reflect.Uint8 {
					elem.SetBytes(append([]byte(nil), data...))
				}
			}
		}
	}
	return nil
}

func (p *PlainCodec) ContentType() string {
	return plainContentTypes[0]
}

func (p *PlainCodec) Supports(contentType string) bool {
	return matchContentType(contentType, plainContentTypes)
}
