package ghttp

import (
	"encoding/json"
	"io"
)

// JSONCodec 使用标准库 encoding/json。
// JSONCodec uses encoding/json (standard library).
type JSONCodec struct{}

func (c *JSONCodec) ContentTypes() []string {
	return []string{"application/json"}
}

func (c *JSONCodec) Marshal(w io.Writer, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// 保持与 json.Encoder 相同的结尾换行,避免改变外部可见响应体。
	// keep the trailing newline of json.Encoder so the wire format is unchanged.
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func (c *JSONCodec) Unmarshal(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
