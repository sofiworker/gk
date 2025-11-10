package gcodec

import (
	"bytes"
	"io"

	"gopkg.in/yaml.v3"
)

type YAMLCodec struct{}

func NewYAMLCodec() *YAMLCodec {
	return &YAMLCodec{}
}

func (y *YAMLCodec) Encode(w io.Writer, v interface{}) error {
	return yaml.NewEncoder(w).Encode(v)
}

func (y *YAMLCodec) Decode(r io.Reader, v interface{}) error {
	return yaml.NewDecoder(r).Decode(v)
}

func (y *YAMLCodec) EncodeBytes(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := y.Encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (y *YAMLCodec) DecodeBytes(data []byte, v interface{}) error {
	return y.Decode(bytes.NewReader(data), v)
}
