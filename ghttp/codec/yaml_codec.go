package codec

import "gopkg.in/yaml.v3"

var yamlContentTypes = []string{
	"application/x-yaml",
	"application/yaml",
	"text/yaml",
}

type YAMLCodec struct{}

func NewYAMLCodec() *YAMLCodec {
	return &YAMLCodec{}
}

func (y *YAMLCodec) Encode(v interface{}) ([]byte, error) {
	return yaml.Marshal(v)
}

func (y *YAMLCodec) Decode(data []byte, v interface{}) error {
	if len(data) == 0 {
		return nil
	}
	return yaml.Unmarshal(data, v)
}

func (y *YAMLCodec) ContentType() string {
	return yamlContentTypes[0]
}

func (y *YAMLCodec) Supports(contentType string) bool {
	return matchContentType(contentType, yamlContentTypes)
}
