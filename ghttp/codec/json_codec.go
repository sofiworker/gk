package codec

import "encoding/json"

var jsonContentTypes = []string{
	"application/json",
	"text/json",
	"application/problem+json",
}

type JSONCodec struct{}

func NewJSONCodec() *JSONCodec {
	return &JSONCodec{}
}

func (j *JSONCodec) Encode(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (j *JSONCodec) Decode(data []byte, v interface{}) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

func (j *JSONCodec) ContentType() string {
	return jsonContentTypes[0]
}

func (j *JSONCodec) Supports(contentType string) bool {
	return matchContentType(contentType, jsonContentTypes)
}
