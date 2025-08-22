package layers

import (
	"fmt"
)

type LayerType int

const (
	LayerTypeEthernet LayerType = iota
	LayerTypeIPv4
	LayerTypeIPv6
	LayerTypeARP
	LayerTypeTCP
	LayerTypeUDP
	LayerTypeICMP
	LayerTypeDNS
	LayerTypeHTTP
	LayerTypeTLS
)

type Layer interface {
	LayerType() LayerType
	Length() int
	Payload() []byte
	String() string
}

type BaseLayer struct {
	Contents    []byte
	PayloadData []byte
}

func (b *BaseLayer) Length() int {
	return len(b.Contents)
}

func (b *BaseLayer) Payload() []byte {
	return b.PayloadData
}

type Decoder interface {
	Decode(data []byte) (Layer, error)
}

var layerDecoders = map[LayerType]Decoder{}

func RegisterLayerDecoder(layerType LayerType, decoder Decoder) {
	layerDecoders[layerType] = decoder
}

func DecodeLayer(layerType LayerType, data []byte) (Layer, error) {
	decoder, ok := layerDecoders[layerType]
	if !ok {
		return nil, fmt.Errorf("no decoder for layer type %d", layerType)
	}
	return decoder.Decode(data)
}
