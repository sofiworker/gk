package layers

import (
	"encoding/binary"
	"fmt"
	"net"
)

type EthernetType uint16

const (
	EthernetTypeIPv4 EthernetType = 0x0800
	EthernetTypeARP  EthernetType = 0x0806
	EthernetTypeIPv6 EthernetType = 0x86DD
	EthernetTypeVLAN EthernetType = 0x8100
	EthernetTypeQinQ EthernetType = 0x88A8
)

type Ethernet struct {
	BaseLayer
	SrcMAC, DstMAC net.HardwareAddr
	EtherType      EthernetType
	VLANIDs        []uint16
}

func (e *Ethernet) LayerType() LayerType {
	return LayerTypeEthernet
}

func (e *Ethernet) String() string {
	if len(e.VLANIDs) == 0 {
		return fmt.Sprintf("Ethernet %s -> %s Type: %#04x",
			e.SrcMAC, e.DstMAC, uint16(e.EtherType))
	}
	return fmt.Sprintf("Ethernet %s -> %s VLAN %v Type: %#04x",
		e.SrcMAC, e.DstMAC, e.VLANIDs, uint16(e.EtherType))
}

type ethernetDecoder struct{}

func (ethernetDecoder) Decode(data []byte) (Layer, error) {
	if len(data) < 14 {
		return nil, fmt.Errorf("layers: ethernet frame too short: %d", len(data))
	}

	dst := make(net.HardwareAddr, 6)
	src := make(net.HardwareAddr, 6)
	copy(dst, data[0:6])
	copy(src, data[6:12])

	offset := 14
	ethType := EthernetType(binary.BigEndian.Uint16(data[12:14]))
	var vlanIDs []uint16

	for ethType == EthernetTypeVLAN || ethType == EthernetTypeQinQ {
		if len(data) < offset+4 {
			return nil, fmt.Errorf("layers: truncated vlan tag")
		}
		tci := binary.BigEndian.Uint16(data[offset : offset+2])
		vlanID := tci & 0x0FFF
		vlanIDs = append(vlanIDs, vlanID)
		ethType = EthernetType(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		offset += 4
	}

	if len(data) < offset {
		return nil, fmt.Errorf("layers: malformed ethernet payload")
	}

	contents := append([]byte(nil), data[:offset]...)
	payload := append([]byte(nil), data[offset:]...)

	return &Ethernet{
		BaseLayer: BaseLayer{
			Contents:    contents,
			PayloadData: payload,
		},
		SrcMAC:    src,
		DstMAC:    dst,
		EtherType: ethType,
		VLANIDs:   vlanIDs,
	}, nil
}

func init() {
	RegisterLayerDecoder(LayerTypeEthernet, ethernetDecoder{})
}
