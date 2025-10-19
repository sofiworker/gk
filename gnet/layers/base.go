package layers

import (
	"errors"
	"fmt"
	"sync"
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

var (
	layerDecoders   = map[LayerType]Decoder{}
	layerDecodersMu sync.RWMutex

	ErrDecoderNotFound = errors.New("layers: decoder not registered")
)

func RegisterLayerDecoder(layerType LayerType, decoder Decoder) {
	layerDecodersMu.Lock()
	layerDecoders[layerType] = decoder
	layerDecodersMu.Unlock()
}

func DecodeLayer(layerType LayerType, data []byte) (Layer, error) {
	layerDecodersMu.RLock()
	decoder, ok := layerDecoders[layerType]
	layerDecodersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrDecoderNotFound, layerType)
	}
	return decoder.Decode(data)
}
