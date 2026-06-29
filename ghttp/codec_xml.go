package ghttp

import (
	"encoding/xml"
	"io"
)

// XMLCodec uses encoding/xml (standard library).
type XMLCodec struct{}

func (c *XMLCodec) ContentTypes() []string {
	return []string{"application/xml", "text/xml"}
}

func (c *XMLCodec) Marshal(w io.Writer, v interface{}) error {
	return xml.NewEncoder(w).Encode(v)
}

func (c *XMLCodec) Unmarshal(r io.Reader, v interface{}) error {
	return xml.NewDecoder(r).Decode(v)
}
