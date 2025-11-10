package gcodec

import (
	"bytes"
	"encoding/json"
	"io"
)

type JSONCodec struct{}

func NewJSONCodec() *JSONCodec {
	return &JSONCodec{}
}

func (j *JSONCodec) Encode(w io.Writer, v interface{}) error {
	return json.NewEncoder(w).Encode(v)
}

func (j *JSONCodec) Decode(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}

func (j *JSONCodec) EncodeBytes(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := j.Encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (j *JSONCodec) DecodeBytes(data []byte, v interface{}) error {
	return j.Decode(bytes.NewReader(data), v)
}
