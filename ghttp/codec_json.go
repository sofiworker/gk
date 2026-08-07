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
	return json.NewEncoder(w).Encode(v)
}

func (c *JSONCodec) Unmarshal(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
