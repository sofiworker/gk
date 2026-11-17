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
	// LayerTypePayload 是构造报文时的原始负载层。
	LayerTypePayload
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

	// ErrDecoderNotFound 表示未注册的解码器。
	ErrDecoderNotFound = errors.New("layers: decoder not registered")
	// ErrTruncated 表示报文数据被截断（长度不足或头字段非法），
	// 供调用方 errors.Is 判断。
	ErrTruncated = errors.New("layers: truncated data")
)

// RegisterLayerDecoder 注册解码器；重复注册会覆盖并返回旧解码器
// （nil 表示首次注册）。
// RegisterLayerDecoder registers a decoder; re-registration overwrites and
// returns the old decoder (nil on first registration).
func RegisterLayerDecoder(layerType LayerType, decoder Decoder) Decoder {
	layerDecodersMu.Lock()
	old := layerDecoders[layerType]
	layerDecoders[layerType] = decoder
	layerDecodersMu.Unlock()
	return old
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
