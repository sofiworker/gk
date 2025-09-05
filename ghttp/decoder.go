package ghttp

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"sync"

	"gopkg.in/yaml.v3"
)

var decoders *DecoderInstance

type DecoderInstance struct {
	mutex    sync.RWMutex
	decoders map[string]Decoder
}

func init() {
	decoders = &DecoderInstance{
		decoders: make(map[string]Decoder),
	}

	_ = RegisterDecoder("application/json", NewJsonDecoder())
	_ = RegisterDecoder("application/xml", NewXmlDecoder())
	_ = RegisterDecoder("application/yaml", NewYamlDecoder())
}

func RegisterDecoder(name string, d Decoder) error {
	decoders.mutex.Lock()
	defer decoders.mutex.Unlock()
	decoders.decoders[name] = d
	return nil
}

func (d *DecoderInstance) Exist(name string) (Decoder, bool) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	decoder, ok := d.decoders[name]
	return decoder, ok
}

type Decoder interface {
	Decode(reader io.Reader, v interface{}) error
}

func NewJsonDecoder() Decoder {
	return &jsonDecoder{}
}

type jsonDecoder struct{}

func (d *jsonDecoder) Decode(reader io.Reader, v interface{}) error {
	return json.NewDecoder(reader).Decode(v)
}

type xmlDecoder struct{}

func NewXmlDecoder() Decoder {
	return &xmlDecoder{}
}

func (d *xmlDecoder) Decode(reader io.Reader, v interface{}) error {
	return xml.NewDecoder(reader).Decode(v)
}

type yamlDecoder struct {
}

func NewYamlDecoder() Decoder {
	return &yamlDecoder{}
}

func (d *yamlDecoder) Decode(reader io.Reader, v interface{}) error {
	return yaml.NewDecoder(reader).Decode(v)
}

type contentDecoder struct{}

func NewContentDecoder() Decoder {
	return &contentDecoder{}
}

func (c *contentDecoder) Decode(reader io.Reader, v interface{}) error {
	//return Decode(reader, v)
	return nil
}
