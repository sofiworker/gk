package codec

import (
	"fmt"
	"io"
	"sync"
)

var decoders *DecoderInstance

type Decoder interface {
    ContentTypes() []string
    Decode(reader io.Reader, v interface{}) error
}

type DecoderInstance struct {
	mutex    sync.RWMutex
	decoders map[string]Decoder
}

func init() {
    decoders = &DecoderInstance{
        decoders: make(map[string]Decoder),
    }

    RegisterDecoder(NewCodecDecoder(NewJSONCodec()))
    RegisterDecoder(NewCodecDecoder(NewXMLCodec()))
    RegisterDecoder(NewCodecDecoder(NewYAMLCodec()))
    RegisterDecoder(NewCodecDecoder(NewPlainCodec()))
}

func NewJsonDecoder() Decoder {
    return NewCodecDecoder(NewJSONCodec())
}

func NewXmlDecoder() Decoder {
    return NewCodecDecoder(NewXMLCodec())
}

func NewYamlDecoder() Decoder {
    return NewCodecDecoder(NewYAMLCodec())
}

// RegisterDecoder 按照 decoder 支持的 Content-Type 注册
func RegisterDecoder(d Decoder) {
	if d == nil {
		return
	}
	decoders.mutex.Lock()
	defer decoders.mutex.Unlock()
	for _, ct := range d.ContentTypes() {
		decoders.decoders[normalizeContentType(ct)] = d
	}
}

// DecoderFor 根据 Content-Type 查找 decoder
func DecoderFor(contentType string) (Decoder, bool) {
	decoders.mutex.RLock()
	defer decoders.mutex.RUnlock()
	decoder, ok := decoders.decoders[normalizeContentType(contentType)]
	return decoder, ok
}

// Decode 根据 Content-Type 读取数据并解码
func DecodeBody(reader io.Reader, contentType string, v interface{}) error {
	if reader == nil {
		return fmt.Errorf("nil reader")
	}
	if v == nil {
		return fmt.Errorf("nil target")
	}

	dec, ok := DecoderFor(contentType)
	if !ok {
		// 使用默认编解码器
		if mgr := DefaultManager(); mgr != nil && mgr.DefaultCodec() != nil {
			all, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			return mgr.DefaultCodec().Decode(all, v)
		}
		return fmt.Errorf("unsupported content type: %s", contentType)
	}

    return dec.Decode(reader, v)
}

// codecDecoder 适配 Codec 到 Decoder
type codecDecoder struct {
	codec Codec
}

func NewCodecDecoder(c Codec) Decoder {
	return &codecDecoder{
		codec: c,
	}
}

func (c *codecDecoder) ContentTypes() []string {
	if c.codec == nil {
		return nil
	}
	contentType := c.codec.ContentType()
	switch normalized := normalizeContentType(contentType); normalized {
	case normalizeContentType("application/json"):
		return jsonContentTypes
	case normalizeContentType("application/xml"):
		return xmlContentTypes
	case normalizeContentType("application/x-yaml"):
		return yamlContentTypes
	case normalizeContentType("text/plain"):
		return plainContentTypes
	default:
		return []string{contentType}
	}
}

func (c *codecDecoder) Decode(reader io.Reader, v interface{}) error {
	if c.codec == nil {
		return fmt.Errorf("codec not configured")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return c.codec.Decode(data, v)
}
