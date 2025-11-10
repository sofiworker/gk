package gcodec

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
)

type PlainCodec struct{}

func NewPlainCodec() *PlainCodec {
	return &PlainCodec{}
}

func (p *PlainCodec) Encode(w io.Writer, v interface{}) error {
	var data []byte
	switch val := v.(type) {
	case []byte:
		data = val
	case string:
		data = []byte(val)
	case fmt.Stringer:
		data = []byte(val.String())
	case error:
		data = []byte(val.Error())
	default:
		data = []byte(fmt.Sprint(v))
	}
	_, err := w.Write(data)
	return err
}

func (p *PlainCodec) Decode(r io.Reader, v interface{}) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

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

func (p *PlainCodec) EncodeBytes(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := p.Encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (p *PlainCodec) DecodeBytes(data []byte, v interface{}) error {
	return p.Decode(bytes.NewReader(data), v)
}
