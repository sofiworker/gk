package layers

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func TestDecodeEthernetIPv4TCP(t *testing.T) {
	payload := []byte("hello")

	etherHeader := []byte{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66,
		0x08, 0x00,
	}

	ipHeader := make([]byte, 20)
	ipHeader[0] = (4 << 4) | 5
	ipHeader[1] = 0
	totalLen := 20 + 20 + len(payload)
	binary.BigEndian.PutUint16(ipHeader[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(ipHeader[4:6], 0x1234)
	binary.BigEndian.PutUint16(ipHeader[6:8], 0x4000)
	ipHeader[8] = 64
	ipHeader[9] = ProtocolTCP
	copy(ipHeader[12:16], net.ParseIP("1.2.3.4").To4())
	copy(ipHeader[16:20], net.ParseIP("5.6.7.8").To4())

	tcpHeader := make([]byte, 20)
	binary.BigEndian.PutUint16(tcpHeader[0:2], 12345)
	binary.BigEndian.PutUint16(tcpHeader[2:4], 80)
	binary.BigEndian.PutUint32(tcpHeader[4:8], 0x11223344)
	binary.BigEndian.PutUint32(tcpHeader[8:12], 0x55667788)
	tcpHeader[12] = (5 << 4)
	tcpHeader[13] = byte(TCPFlagACK | TCPFlagPSH)
	binary.BigEndian.PutUint16(tcpHeader[14:16], 1024)

	frame := append(etherHeader, ipHeader...)
	frame = append(frame, tcpHeader...)
	frame = append(frame, payload...)

	decoded, err := Decode(frame)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(decoded))
	}

	eth, ok := decoded[0].(*Ethernet)
	if !ok {
		t.Fatalf("expected Ethernet layer, got %T", decoded[0])
	}
	if eth.EtherType != EthernetTypeIPv4 {
		t.Fatalf("unexpected ether type: %#04x", uint16(eth.EtherType))
	}

	ip, ok := decoded[1].(*IPv4)
	if !ok {
		t.Fatalf("expected IPv4 layer, got %T", decoded[1])
	}
	if !ip.SrcIP.Equal(net.ParseIP("1.2.3.4").To4()) || !ip.DstIP.Equal(net.ParseIP("5.6.7.8").To4()) {
		t.Fatalf("unexpected ip addresses: %v -> %v", ip.SrcIP, ip.DstIP)
	}
	if ip.Protocol != ProtocolTCP {
		t.Fatalf("unexpected protocol: %d", ip.Protocol)
	}

	tcp, ok := decoded[2].(*TCP)
	if !ok {
		t.Fatalf("expected TCP layer, got %T", decoded[2])
	}
	if tcp.SrcPort != 12345 || tcp.DstPort != 80 {
		t.Fatalf("unexpected ports: %d -> %d", tcp.SrcPort, tcp.DstPort)
	}
	if !tcp.HasFlag(TCPFlagACK) || !tcp.HasFlag(TCPFlagPSH) {
		t.Fatalf("expected ACK and PSH flags, got %#x", tcp.Flags)
	}
	if !bytes.Equal(tcp.Payload(), payload) {
		t.Fatalf("payload mismatch: %x", tcp.Payload())
	}
}

func TestDecodeEthernetIPv6UDP(t *testing.T) {
	payload := []byte{0xde, 0xad, 0xbe, 0xef}

	etherHeader := []byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x86, 0xdd,
	}

	ipHeader := make([]byte, IPv6HeaderLen)
	ipHeader[0] = (6 << 4)
	binary.BigEndian.PutUint16(ipHeader[4:6], uint16(8+len(payload)))
	ipHeader[6] = ProtocolUDP
	ipHeader[7] = 64
	copy(ipHeader[8:24], net.ParseIP("2001:db8::1").To16())
	copy(ipHeader[24:40], net.ParseIP("2001:db8::2").To16())

	udpHeader := make([]byte, 8)
	binary.BigEndian.PutUint16(udpHeader[0:2], 5353)
	binary.BigEndian.PutUint16(udpHeader[2:4], 8080)
	binary.BigEndian.PutUint16(udpHeader[4:6], uint16(8+len(payload)))

	frame := append(etherHeader, ipHeader...)
	frame = append(frame, udpHeader...)
	frame = append(frame, payload...)

	decoded, err := Decode(frame)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(decoded))
	}

	ip, ok := decoded[1].(*IPv6)
	if !ok {
		t.Fatalf("expected IPv6 layer, got %T", decoded[1])
	}
	if ip.NextHeader != ProtocolUDP {
		t.Fatalf("unexpected next header: %d", ip.NextHeader)
	}

	udp, ok := decoded[2].(*UDP)
	if !ok {
		t.Fatalf("expected UDP layer, got %T", decoded[2])
	}
	if udp.SrcPort != 5353 || udp.DstPort != 8080 {
		t.Fatalf("unexpected udp ports: %d -> %d", udp.SrcPort, udp.DstPort)
	}
	if !bytes.Equal(udp.Payload(), payload) {
		t.Fatalf("unexpected payload: %x", udp.Payload())
	}
}
