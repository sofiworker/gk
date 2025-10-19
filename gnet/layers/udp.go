package layers

import (
	"encoding/binary"
	"fmt"
)

type UDP struct {
	BaseLayer
	SrcPort      uint16
	DstPort      uint16
	PacketLength uint16
	Checksum     uint16
}

func (u *UDP) LayerType() LayerType {
	return LayerTypeUDP
}

func (u *UDP) String() string {
	return fmt.Sprintf("UDP %d -> %d len=%d", u.SrcPort, u.DstPort, u.PacketLength)
}

type udpDecoder struct{}

func (udpDecoder) Decode(data []byte) (Layer, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("layers: udp datagram too short: %d", len(data))
	}

	length := binary.BigEndian.Uint16(data[4:6])
	payloadEnd := len(data)
	if length >= 8 && int(length) <= len(data) {
		payloadEnd = int(length)
	}
	if payloadEnd < 8 {
		payloadEnd = 8
	}

	payload := append([]byte(nil), data[8:payloadEnd]...)

	return &UDP{
		BaseLayer: BaseLayer{
			Contents:    append([]byte(nil), data[:8]...),
			PayloadData: payload,
		},
		SrcPort:      binary.BigEndian.Uint16(data[0:2]),
		DstPort:      binary.BigEndian.Uint16(data[2:4]),
		PacketLength: length,
		Checksum:     binary.BigEndian.Uint16(data[6:8]),
	}, nil
}

func init() {
	RegisterLayerDecoder(LayerTypeUDP, udpDecoder{})
}
