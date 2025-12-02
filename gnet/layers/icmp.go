package layers

import (
	"encoding/binary"
	"fmt"
)

type ICMP struct {
	BaseLayer
	Type     uint8
	Code     uint8
	Checksum uint16
	Rest     []byte
}

func (i *ICMP) LayerType() LayerType {
	return LayerTypeICMP
}

func (i *ICMP) String() string {
	return fmt.Sprintf("ICMP type=%d code=%d", i.Type, i.Code)
}

type icmpDecoder struct{}

func (icmpDecoder) Decode(data []byte) (Layer, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("layers: icmp packet too short: %d", len(data))
	}
	rest := append([]byte(nil), data[4:]...)
	return &ICMP{
		BaseLayer: BaseLayer{
			Contents:    append([]byte(nil), data[:4]...),
			PayloadData: rest,
		},
		Type:     data[0],
		Code:     data[1],
		Checksum: binary.BigEndian.Uint16(data[2:4]),
		Rest:     rest,
	}, nil
}

func init() {
	RegisterLayerDecoder(LayerTypeICMP, icmpDecoder{})
}
