package codec

import "encoding/xml"

var xmlContentTypes = []string{
	"application/xml",
	"text/xml",
}

type XMLCodec struct{}

func NewXMLCodec() *XMLCodec {
	return &XMLCodec{}
}

func (x *XMLCodec) Encode(v interface{}) ([]byte, error) {
	return xml.Marshal(v)
}

func (x *XMLCodec) Decode(data []byte, v interface{}) error {
	if len(data) == 0 {
		return nil
	}
	return xml.Unmarshal(data, v)
}

func (x *XMLCodec) ContentType() string {
	return xmlContentTypes[0]
}

func (x *XMLCodec) Supports(contentType string) bool {
	return matchContentType(contentType, xmlContentTypes)
}
