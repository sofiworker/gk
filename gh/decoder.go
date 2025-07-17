package ghttp

import (
	"encoding/json"
	"io"
	"sync"
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

	_ = RegisterDecoder("application/json", &jsonDecoder{})
	_ = RegisterDecoder("application/xml", &xmlDecoder{})
	_ = RegisterDecoder("application/yaml", &yamlDecoder{})
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

func (d *xmlDecoder) Decode(reader io.Reader, v interface{}) error {
	return nil
}

type yamlDecoder struct {
}

func (d *yamlDecoder) Decode(reader io.Reader, v interface{}) error {
	return nil
}
