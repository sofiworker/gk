package gcodec

import (
	"bytes"
	"encoding/xml"
	"io"
)

type XMLCodec struct{}

func NewXMLCodec() *XMLCodec {
	return &XMLCodec{}
}

func (x *XMLCodec) Encode(w io.Writer, v interface{}) error {
	return xml.NewEncoder(w).Encode(v)
}

func (x *XMLCodec) Decode(r io.Reader, v interface{}) error {
	return xml.NewDecoder(r).Decode(v)
}

func (x *XMLCodec) EncodeBytes(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := x.Encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (x *XMLCodec) DecodeBytes(data []byte, v interface{}) error {
	return x.Decode(bytes.NewReader(data), v)
}
