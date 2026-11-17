package layers

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestParseEthernetIPv4TCP(t *testing.T) {
	frame := buildEthernetIPv4TCP([]byte{0x61, 0x62, 0x63}) // "abc"

	p, err := Parse(frame)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if p.Ether == nil || p.IPv4 == nil || p.TCP == nil {
		t.Fatalf("missing layers: %+v", p)
	}
	payload, err := p.Payload()
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if string(payload) != "abc" {
		t.Fatalf("unexpected payload %q", string(payload))
	}
	if p.IPv4.TotalLength == 0 {
		t.Fatalf("ipv4 total length not set")
	}
}

func buildEthernetIPv4TCP(payload []byte) []byte {
	dst := []byte{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb}
	src := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}

	ipHeaderLen := 20
	tcpHeaderLen := 20
	totalLen := uint16(ipHeaderLen + tcpHeaderLen + len(payload))

	buf := make([]byte, 14+ipHeaderLen+tcpHeaderLen+len(payload))
	offset := 0
	copy(buf[offset:], dst)
	offset += 6
	copy(buf[offset:], src)
	offset += 6
	binary.BigEndian.PutUint16(buf[offset:], uint16(EthernetTypeIPv4))
	offset += 2

	// IPv4 header
	buf[offset] = (4 << 4) | 5 // version=4, IHL=5
	buf[offset+1] = 0          // TOS
	binary.BigEndian.PutUint16(buf[offset+2:], totalLen)
	binary.BigEndian.PutUint16(buf[offset+4:], 0x1234) // ID
	binary.BigEndian.PutUint16(buf[offset+6:], 0)      // flags+frag
	buf[offset+8] = 64                                 // TTL
	buf[offset+9] = ProtocolTCP
	binary.BigEndian.PutUint16(buf[offset+10:], 0) // checksum (ignored)
	copy(buf[offset+12:], net.IPv4(192, 168, 1, 1).To4())
	copy(buf[offset+16:], net.IPv4(192, 168, 1, 2).To4())
	offset += ipHeaderLen

	// TCP header
	binary.BigEndian.PutUint16(buf[offset:], 1234)    // src port
	binary.BigEndian.PutUint16(buf[offset+2:], 80)    // dst port
	binary.BigEndian.PutUint32(buf[offset+4:], 1)     // seq
	binary.BigEndian.PutUint32(buf[offset+8:], 0)     // ack
	buf[offset+12] = (5 << 4)                         // data offset
	buf[offset+13] = 0x18                             // flags (PSH,ACK)
	binary.BigEndian.PutUint16(buf[offset+14:], 1024) // window
	binary.BigEndian.PutUint16(buf[offset+16:], 0)    // checksum
	binary.BigEndian.PutUint16(buf[offset+18:], 0)    // urgent
	offset += tcpHeaderLen

	copy(buf[offset:], payload)
	return buf
}
