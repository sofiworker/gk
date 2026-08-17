package ghttp

import (
	"fmt"
	"io"
	"reflect"
)

// PlainCodec 处理 text/plain 内容。
// PlainCodec handles text/plain content.
type PlainCodec struct{}

func (c *PlainCodec) ContentTypes() []string {
	return []string{"text/plain"}
}

func (c *PlainCodec) Marshal(w io.Writer, v interface{}) error {
	switch val := v.(type) {
	case string:
		_, err := io.WriteString(w, val)
		return err
	case []byte:
		_, err := w.Write(val)
		return err
	default:
		_, err := io.WriteString(w, fmt.Sprintf("%v", v))
		return err
	}
}

func (c *PlainCodec) Unmarshal(r io.Reader, v interface{}) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	target, err := indirectDecodeValue(v)
	if err != nil {
		return fmt.Errorf("plain codec: %w", err)
	}
	if target.Kind() == reflect.String {
		target.SetString(string(data))
		return nil
	}
	if target.Kind() == reflect.Slice && target.Type().Elem().Kind() == reflect.Uint8 {
		target.SetBytes(data)
		return nil
	}
	return fmt.Errorf("plain codec: unsupported target %T", v)
}
