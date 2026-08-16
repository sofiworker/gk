package layers

import (
	"encoding/binary"
	"fmt"
	"net"
)

// 以下为各层的序列化实现（构造报文用）。头部长度字段与校验和
// 由 SerializeBuffer.fixup 统一回填。
//
// Serialization for each layer (packet crafting). Header length fields and
// checksums are back-filled by SerializeBuffer.fixup.

// SerializeTo 序列化以太网头（含 VLAN 标签链）。
// SerializeTo serializes the Ethernet header (including VLAN tags).
func (e *Ethernet) SerializeTo(b *SerializeBuffer) error {
	if len(e.DstMAC) != 6 || len(e.SrcMAC) != 6 {
		return fmt.Errorf("layers: ethernet MAC must be 6 bytes")
	}
	size := 14 + 4*len(e.VLANIDs)
	hdr := b.PrependHeader(size, e)
	copy(hdr[0:6], e.DstMAC)
	copy(hdr[6:12], e.SrcMAC)
	off := 12
	for _, vid := range e.VLANIDs {
		binary.BigEndian.PutUint16(hdr[off:off+2], uint16(EthernetTypeVLAN))
		binary.BigEndian.PutUint16(hdr[off+2:off+4], vid&0x0FFF)
		off += 4
	}
	binary.BigEndian.PutUint16(hdr[off:off+2], uint16(e.EtherType))
	return nil
}

// SerializeTo 序列化 IPv4 头（长度与校验和由 fixup 回填）。
// SerializeTo serializes the IPv4 header (length/checksum back-filled).
func (ip *IPv4) SerializeTo(b *SerializeBuffer) error {
	if ip.SrcIP.To4() == nil || ip.DstIP.To4() == nil {
		return fmt.Errorf("layers: ipv4 requires 4-byte src/dst IPs")
	}
	ihl := 5
	opts := pad4(ip.Options)
	if len(opts) > 0 {
		ihl = 5 + len(opts)/4
	}
	ip.IHL = uint8(ihl) // 回填供 fixup 计算头部校验和
	hdr := b.PrependHeader(ihl*4, ip)
	hdr[0] = (4 << 4) | uint8(ihl) // 版本 + 实际 IHL（含选项）
	hdr[1] = ip.TOS
	// TotalLength 由 fixup 回填。
	binary.BigEndian.PutUint16(hdr[4:6], ip.ID)
	flagsFrag := uint16(ip.Flags)<<13 | (ip.FragOffset & 0x1FFF)
	binary.BigEndian.PutUint16(hdr[6:8], flagsFrag)
	hdr[8] = ip.TTL
	hdr[9] = ip.Protocol
	copy(hdr[12:16], ip.SrcIP.To4())
	copy(hdr[16:20], ip.DstIP.To4())
	if len(opts) > 0 {
		copy(hdr[20:], opts)
	}
	return nil
}

// SerializeTo 序列化 IPv6 头（负载长度由 fixup 回填）。
// SerializeTo serializes the IPv6 header (payload length back-filled).
func (ip *IPv6) SerializeTo(b *SerializeBuffer) error {
	if ip.SrcIP.To16() == nil || ip.DstIP.To16() == nil {
		return fmt.Errorf("layers: ipv6 requires 16-byte src/dst IPs")
	}
	hdr := b.PrependHeader(IPv6HeaderLen, ip)
	hdr[0] = 0x60
	hdr[0] |= ip.TrafficClass >> 4
	hdr[1] = ip.TrafficClass << 4
	binary.BigEndian.PutUint32(hdr[0:4], binary.BigEndian.Uint32(hdr[0:4])|ip.FlowLabel&0x0FFFFF)
	// PayloadLen 由 fixup 回填。
	hdr[6] = ip.NextHeader
	hdr[7] = ip.HopLimit
	copy(hdr[8:24], ip.SrcIP.To16())
	copy(hdr[24:40], ip.DstIP.To16())
	return nil
}

// SerializeTo 序列化 TCP 头（校验和由 fixup 回填）。
// SerializeTo serializes the TCP header (checksum back-filled).
func (t *TCP) SerializeTo(b *SerializeBuffer) error {
	opts := pad4(t.Options)
	do := t.DataOffset
	if len(opts) > 0 {
		need := uint8(5 + len(opts)/4)
		if do < need {
			do = need // 有选项时确保头长容纳选项
		}
	}
	if do == 0 {
		do = 5
	}
	t.DataOffset = do
	hdr := b.PrependHeader(int(do)*4, t)
	binary.BigEndian.PutUint16(hdr[0:2], t.SrcPort)
	binary.BigEndian.PutUint16(hdr[2:4], t.DstPort)
	binary.BigEndian.PutUint32(hdr[4:8], t.Seq)
	binary.BigEndian.PutUint32(hdr[8:12], t.Ack)
	hdr[12] = t.DataOffset<<4 | byte((t.Flags>>8)&0x01) // NS 位在 12 字节最低位
	hdr[13] = byte(t.Flags)
	binary.BigEndian.PutUint16(hdr[14:16], t.Window)
	// Checksum/Urgent 由 fixup 或保持 0。
	binary.BigEndian.PutUint16(hdr[18:20], t.Urgent)
	if len(opts) > 0 {
		copy(hdr[20:], opts)
	}
	return nil
}

// SerializeTo 序列化 UDP 头（长度与校验和由 fixup 回填）。
// SerializeTo serializes the UDP header (length/checksum back-filled).
func (u *UDP) SerializeTo(b *SerializeBuffer) error {
	hdr := b.PrependHeader(8, u)
	binary.BigEndian.PutUint16(hdr[0:2], u.SrcPort)
	binary.BigEndian.PutUint16(hdr[2:4], u.DstPort)
	return nil
}

// SerializeTo 序列化 ICMP 头（校验和由 fixup 回填）。
// SerializeTo serializes the ICMP header (checksum back-filled).
func (i *ICMP) SerializeTo(b *SerializeBuffer) error {
	hdr := b.PrependHeader(4+len(i.Rest), i)
	hdr[0] = i.Type
	hdr[1] = i.Code
	copy(hdr[4:], i.Rest)
	return nil
}

// pad4 把字节片补齐到 4 字节对齐（不足补零）。
// pad4 pads a byte slice to 4-byte alignment.
func pad4(p []byte) []byte {
	if len(p)%4 == 0 {
		return p
	}
	out := make([]byte, (len(p)/4+1)*4)
	copy(out, p)
	return out
}

// ARP 是地址解析协议层。
// ARP is the Address Resolution Protocol layer.
type ARP struct {
	BaseLayer
	HwType    uint16
	ProtoType uint16
	HwLen     uint8
	ProtoLen  uint8
	Operation uint16
	SrcMAC    net.HardwareAddr
	SrcIP     net.IP
	DstMAC    net.HardwareAddr
	DstIP     net.IP
}

// ARP 操作码。
// ARP operation codes.
const (
	ARPRequest uint16 = 1
	ARPReply   uint16 = 2
)

// LayerType 返回 LayerTypeARP。
// LayerType returns LayerTypeARP.
func (a *ARP) LayerType() LayerType { return LayerTypeARP }

// String 描述 ARP 报文。
// String describes the ARP packet.
func (a *ARP) String() string {
	return fmt.Sprintf("ARP op=%d %v(%s) -> %v(%s)", a.Operation, a.SrcIP, a.SrcMAC, a.DstIP, a.DstMAC)
}

type arpDecoder struct{}

// Decode 解析标准以太网 ARP（28 字节，IPv4 映射）。
// Decode parses standard Ethernet ARP (28 bytes, IPv4 mapping).
func (arpDecoder) Decode(data []byte) (Layer, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("layers: arp packet too short: %d", len(data))
	}
	hwLen, pLen := int(data[4]), int(data[5])
	if len(data) < 8+hwLen*2+pLen*2 {
		return nil, fmt.Errorf("layers: arp packet truncated")
	}
	off := 8
	srcMAC := make(net.HardwareAddr, hwLen)
	copy(srcMAC, data[off:off+hwLen])
	off += hwLen
	srcIP := append(net.IP(nil), data[off:off+pLen]...)
	off += pLen
	dstMAC := make(net.HardwareAddr, hwLen)
	copy(dstMAC, data[off:off+hwLen])
	off += hwLen
	dstIP := append(net.IP(nil), data[off:off+pLen]...)
	return &ARP{
		BaseLayer: BaseLayer{Contents: append([]byte(nil), data...)},
		HwType:    binary.BigEndian.Uint16(data[0:2]),
		ProtoType: binary.BigEndian.Uint16(data[2:4]),
		HwLen:     data[4],
		ProtoLen:  data[5],
		Operation: binary.BigEndian.Uint16(data[6:8]),
		SrcMAC:    srcMAC,
		SrcIP:     srcIP,
		DstMAC:    dstMAC,
		DstIP:     dstIP,
	}, nil
}

// SerializeTo 序列化 ARP 报文。
// SerializeTo serializes an ARP packet.
func (a *ARP) SerializeTo(b *SerializeBuffer) error {
	hwLen, pLen := len(a.SrcMAC), len(a.SrcIP)
	if hwLen != len(a.DstMAC) || pLen != len(a.DstIP) || hwLen == 0 || pLen == 0 {
		return fmt.Errorf("layers: arp inconsistent address lengths")
	}
	if a.HwLen == 0 {
		a.HwLen = uint8(hwLen)
	}
	if a.ProtoLen == 0 {
		a.ProtoLen = uint8(pLen)
	}
	buf := b.PrependHeader(8+hwLen*2+pLen*2, a)
	binary.BigEndian.PutUint16(buf[0:2], a.HwType)
	binary.BigEndian.PutUint16(buf[2:4], a.ProtoType)
	buf[4], buf[5] = a.HwLen, a.ProtoLen
	binary.BigEndian.PutUint16(buf[6:8], a.Operation)
	off := 8
	copy(buf[off:], a.SrcMAC)
	off += hwLen
	copy(buf[off:], a.SrcIP)
	off += pLen
	copy(buf[off:], a.DstMAC)
	off += hwLen
	copy(buf[off:], a.DstIP)
	return nil
}

func init() {
	RegisterLayerDecoder(LayerTypeARP, arpDecoder{})
}
