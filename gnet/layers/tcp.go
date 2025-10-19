package layers

import (
	"encoding/binary"
	"fmt"
)

const (
	TCPFlagFIN uint16 = 1 << 0
	TCPFlagSYN uint16 = 1 << 1
	TCPFlagRST uint16 = 1 << 2
	TCPFlagPSH uint16 = 1 << 3
	TCPFlagACK uint16 = 1 << 4
	TCPFlagURG uint16 = 1 << 5
	TCPFlagECE uint16 = 1 << 6
	TCPFlagCWR uint16 = 1 << 7
	TCPFlagNS  uint16 = 1 << 8
)

type TCP struct {
	BaseLayer
	SrcPort    uint16
	DstPort    uint16
	Seq        uint32
	Ack        uint32
	DataOffset uint8
	Flags      uint16
	Window     uint16
	Checksum   uint16
	Urgent     uint16
	Options    []byte
}

func (t *TCP) LayerType() LayerType {
	return LayerTypeTCP
}

func (t *TCP) HeaderLength() int {
	return int(t.DataOffset) * 4
}

func (t *TCP) HasFlag(flag uint16) bool {
	return t.Flags&flag != 0
}

func (t *TCP) String() string {
	return fmt.Sprintf("TCP %d -> %d seq=%d ack=%d flags=%#x", t.SrcPort, t.DstPort, t.Seq, t.Ack, t.Flags)
}

type tcpDecoder struct{}

func (tcpDecoder) Decode(data []byte) (Layer, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("layers: tcp segment too short: %d", len(data))
	}

	dataOffset := data[12] >> 4
	headerLen := int(dataOffset) * 4
	if headerLen < 20 || len(data) < headerLen {
		return nil, fmt.Errorf("layers: invalid tcp header length %d", headerLen)
	}

	nsFlag := uint16(data[12] & 0x01)
	flags := (nsFlag << 8) | uint16(data[13])

	optionsLen := headerLen - 20
	var options []byte
	if optionsLen > 0 {
		options = append([]byte(nil), data[20:headerLen]...)
	}

	payload := append([]byte(nil), data[headerLen:]...)

	return &TCP{
		BaseLayer: BaseLayer{
			Contents:    append([]byte(nil), data[:headerLen]...),
			PayloadData: payload,
		},
		SrcPort:    binary.BigEndian.Uint16(data[0:2]),
		DstPort:    binary.BigEndian.Uint16(data[2:4]),
		Seq:        binary.BigEndian.Uint32(data[4:8]),
		Ack:        binary.BigEndian.Uint32(data[8:12]),
		DataOffset: dataOffset,
		Flags:      flags,
		Window:     binary.BigEndian.Uint16(data[14:16]),
		Checksum:   binary.BigEndian.Uint16(data[16:18]),
		Urgent:     binary.BigEndian.Uint16(data[18:20]),
		Options:    options,
	}, nil
}

func init() {
	RegisterLayerDecoder(LayerTypeTCP, tcpDecoder{})
}
