package layers

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

var (
	ErrHeaderTooShort = errors.New("layers: ipv6 header too short")
)

const (
	IPv6HeaderLen = 40
)

type IPv6 struct {
	BaseLayer
	Version      uint8
	TrafficClass uint8
	FlowLabel    uint32
	PayloadLen   uint16
	NextHeader   uint8
	HopLimit     uint8
	SrcIP        net.IP
	DstIP        net.IP
}

func (ip *IPv6) LayerType() LayerType {
	return LayerTypeIPv6
}

func (ip *IPv6) String() string {
	return fmt.Sprintf("IPv6 %v -> %v nh=%d hop=%d", ip.SrcIP, ip.DstIP, ip.NextHeader, ip.HopLimit)
}

type ipv6Decoder struct{}

func (ipv6Decoder) Decode(data []byte) (Layer, error) {
	if len(data) < IPv6HeaderLen {
		return nil, ErrHeaderTooShort
	}

	version := data[0] >> 4
	if version != 6 {
		return nil, fmt.Errorf("layers: invalid ipv6 version %d", version)
	}

	trafficClass := (data[0]&0x0F)<<4 | (data[1] >> 4)
	flowLabel := uint32(data[1]&0x0F)<<16 | uint32(data[2])<<8 | uint32(data[3])
	payloadLen := binary.BigEndian.Uint16(data[4:6])
	nextHeader := data[6]
	hopLimit := data[7]

	src := make(net.IP, net.IPv6len)
	dst := make(net.IP, net.IPv6len)
	copy(src, data[8:24])
	copy(dst, data[24:40])

	expectedTotal := IPv6HeaderLen + int(payloadLen)
	if expectedTotal > len(data) {
		expectedTotal = len(data)
	}

	ipv6 := &IPv6{
		BaseLayer: BaseLayer{
			Contents:    append([]byte(nil), data[:IPv6HeaderLen]...),
			PayloadData: append([]byte(nil), data[IPv6HeaderLen:expectedTotal]...),
		},
		Version:      version,
		TrafficClass: trafficClass,
		FlowLabel:    flowLabel,
		PayloadLen:   payloadLen,
		NextHeader:   nextHeader,
		HopLimit:     hopLimit,
		SrcIP:        src,
		DstIP:        dst,
	}

	return ipv6, nil
}

func init() {
	RegisterLayerDecoder(LayerTypeIPv6, ipv6Decoder{})
}
