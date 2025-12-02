package layers

import (
	"encoding/binary"
	"fmt"
	"net"
)

const (
	ProtocolICMP uint8 = 1
	ProtocolTCP  uint8 = 6
	ProtocolUDP  uint8 = 17
)

type IPv4 struct {
	BaseLayer
	Version      uint8
	IHL          uint8
	TOS          uint8
	TotalLength  uint16
	ID           uint16
	Flags        uint8
	FragOffset   uint16
	TTL          uint8
	Protocol     uint8
	Checksum     uint16
	SrcIP, DstIP net.IP
	Options      []byte
}

func (ip *IPv4) LayerType() LayerType {
	return LayerTypeIPv4
}

func (ip *IPv4) String() string {
	return fmt.Sprintf("IPv4 %v -> %v proto=%d ttl=%d", ip.SrcIP, ip.DstIP, ip.Protocol, ip.TTL)
}

func (ip *IPv4) HeaderLength() int {
	return int(ip.IHL) * 4
}

type ipv4Decoder struct{}

func (ipv4Decoder) Decode(data []byte) (Layer, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("layers: ipv4 packet too short: %d", len(data))
	}

	versionIHL := data[0]
	version := versionIHL >> 4
	ihl := versionIHL & 0x0F
	if version != 4 {
		return nil, fmt.Errorf("layers: invalid ipv4 version %d", version)
	}

	headerLen := int(ihl) * 4
	if headerLen < 20 || len(data) < headerLen {
		return nil, fmt.Errorf("layers: invalid ipv4 header length %d", headerLen)
	}

	totalLen := int(binary.BigEndian.Uint16(data[2:4]))
	if totalLen > len(data) {
		totalLen = len(data)
	}
	if totalLen < headerLen {
		return nil, fmt.Errorf("layers: ipv4 total length smaller than header")
	}

	flagsFrag := binary.BigEndian.Uint16(data[6:8])
	flags := uint8(flagsFrag >> 13)
	fragOffset := flagsFrag & 0x1FFF

	optionsLen := headerLen - 20
	var options []byte
	if optionsLen > 0 {
		options = append([]byte(nil), data[20:headerLen]...)
	}

	payload := append([]byte(nil), data[headerLen:totalLen]...)

	src := make(net.IP, net.IPv4len)
	dst := make(net.IP, net.IPv4len)
	copy(src, data[12:16])
	copy(dst, data[16:20])

	packet := &IPv4{
		BaseLayer: BaseLayer{
			Contents:    append([]byte(nil), data[:headerLen]...),
			PayloadData: payload,
		},
		Version:     version,
		IHL:         ihl,
		TOS:         data[1],
		TotalLength: uint16(totalLen),
		ID:          binary.BigEndian.Uint16(data[4:6]),
		Flags:       flags,
		FragOffset:  fragOffset,
		TTL:         data[8],
		Protocol:    data[9],
		Checksum:    binary.BigEndian.Uint16(data[10:12]),
		SrcIP:       src,
		DstIP:       dst,
		Options:     options,
	}
	return packet, nil
}

func init() {
	RegisterLayerDecoder(LayerTypeIPv4, ipv4Decoder{})
}
