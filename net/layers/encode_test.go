package layers

import (
	"encoding/binary"
	"net"
	"testing"
)

// buildIPv4Packet 构造 Ethernet+IPv4+TCP 报文并返回原始字节。
func buildIPv4Packet(t *testing.T, payload []byte) []byte {
	t.Helper()
	eth := &Ethernet{
		SrcMAC:    net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		DstMAC:    net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EtherType: EthernetTypeIPv4,
	}
	ip := &IPv4{
		Version:  4,
		TOS:      0,
		ID:       0x1234,
		TTL:      64,
		Protocol: ProtocolTCP,
		SrcIP:    net.IPv4(192, 168, 1, 10),
		DstIP:    net.IPv4(10, 0, 0, 1),
	}
	tcp := &TCP{
		SrcPort: 12345,
		DstPort: 80,
		Seq:     1000,
		Ack:     0,
		Flags:   TCPFlagSYN,
		Window:  65535,
	}
	buf, err := SerializeLayers(SerializeOptions{FixLengths: true, ComputeChecksums: true},
		eth, ip, tcp, Payload(payload))
	if err != nil {
		t.Fatalf("SerializeLayers: %v", err)
	}
	return buf
}

// TestSerializeIPv4TCPRoundTrip 覆盖构造→解码回环与校验和有效性。
func TestSerializeIPv4TCPRoundTrip(t *testing.T) {
	buf := buildIPv4Packet(t, []byte("hello"))
	layers, err := Decode(buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(layers) != 3 { // Ethernet, IPv4, TCP
		t.Fatalf("decoded %d layers, want 3", len(layers))
	}
	ip := layers[1].(*IPv4)
	if !ip.SrcIP.Equal(net.IPv4(192, 168, 1, 10)) {
		t.Fatalf("src ip = %v", ip.SrcIP)
	}
	if ip.TotalLength != uint16(len(buf)-14) {
		t.Fatalf("TotalLength = %d, want %d", ip.TotalLength, len(buf)-14)
	}
	// 校验和合法：反码和应为 0。
	if InternetChecksum(ip.Contents) != 0 {
		t.Fatalf("IPv4 checksum invalid: %#04x", ip.Checksum)
	}
	tcp := layers[2].(*TCP)
	if tcp.SrcPort != 12345 || tcp.DstPort != 80 || !tcp.HasFlag(TCPFlagSYN) {
		t.Fatalf("tcp = %+v", tcp)
	}
	if string(tcp.Payload()) != "hello" {
		t.Fatalf("tcp payload = %q", tcp.Payload())
	}
	// TCP 伪头校验和合法。
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], ip.SrcIP.To4())
	copy(pseudo[4:8], ip.DstIP.To4())
	pseudo[9] = 6
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcp.Contents)+len(tcp.Payload())))
	full := append(pseudo, tcp.Contents...)
	full = append(full, tcp.Payload()...)
	if InternetChecksum(full) != 0 {
		t.Fatalf("TCP checksum invalid: %#04x", tcp.Checksum)
	}
}

// TestSerializeIPv4UDPAndDNS 覆盖 UDP+DNS 全栈构造与解码。
func TestSerializeIPv4UDPAndDNS(t *testing.T) {
	dns := &DNS{
		ID:    0xbeef,
		Flags: 0x0100,
		Questions: []DNSQuestion{{
			Name:  DNSName("example.com"),
			Type:  DNSTypeA,
			Class: DNSClassIN,
		}},
	}
	eth := &Ethernet{DstMAC: make(net.HardwareAddr, 6), SrcMAC: make(net.HardwareAddr, 6), EtherType: EthernetTypeIPv4}
	ip := &IPv4{Version: 4, ID: 1, TTL: 64, Protocol: ProtocolUDP, SrcIP: net.IPv4(1, 1, 1, 1), DstIP: net.IPv4(8, 8, 8, 8)}
	udp := &UDP{SrcPort: 53000, DstPort: 53}
	buf, err := SerializeLayers(SerializeOptions{FixLengths: true, ComputeChecksums: true},
		eth, ip, udp, dns)
	if err != nil {
		t.Fatalf("SerializeLayers: %v", err)
	}
	layers, err := Decode(buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// Ethernet, IPv4, UDP, DNS。
	if len(layers) != 4 {
		t.Fatalf("decoded %d layers, want 4: %v", len(layers), layerNames(layers))
	}
	got := layers[3].(*DNS)
	if got.ID != 0xbeef || len(got.Questions) != 1 {
		t.Fatalf("dns = %+v", got)
	}
	// 还原点分名。
	name, err := DNSNameToString(got.Questions[0].Name)
	if err != nil {
		t.Fatalf("name decode: %v", err)
	}
	if name != "example.com" {
		t.Fatalf("name = %q, want example.com", name)
	}
	// UDP 校验和合法。
	udpGot := layers[2].(*UDP)
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], ip.SrcIP.To4())
	copy(pseudo[4:8], ip.DstIP.To4())
	pseudo[9] = 17
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(udpGot.Contents)+len(udpGot.Payload())))
	full := append(pseudo, udpGot.Contents...)
	full = append(full, udpGot.Payload()...)
	if InternetChecksum(full) != 0 {
		t.Fatalf("UDP checksum invalid: %#04x", udpGot.Checksum)
	}
}

// TestSerializeARPRoundTrip 覆盖 ARP 构造与解码。
func TestSerializeARPRoundTrip(t *testing.T) {
	arp := &ARP{
		HwType:    1,
		ProtoType: uint16(EthernetTypeIPv4),
		Operation: ARPRequest,
		SrcMAC:    net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		SrcIP:     net.IPv4(192, 168, 0, 1),
		DstMAC:    make(net.HardwareAddr, 6),
		DstIP:     net.IPv4(192, 168, 0, 2),
	}
	eth := &Ethernet{
		DstMAC:    net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		SrcMAC:    arp.SrcMAC,
		EtherType: EthernetTypeARP,
	}
	buf, err := SerializeLayers(SerializeOptions{}, eth, arp)
	if err != nil {
		t.Fatalf("SerializeLayers: %v", err)
	}
	layers, err := Decode(buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(layers) != 2 {
		t.Fatalf("decoded %d layers, want 2", len(layers))
	}
	got := layers[1].(*ARP)
	if got.Operation != ARPRequest || !got.SrcIP.Equal(net.IPv4(192, 168, 0, 1)) || !got.DstIP.Equal(net.IPv4(192, 168, 0, 2)) {
		t.Fatalf("arp = %+v", got)
	}
}

// TestSerializeVLANRoundTrip 覆盖 VLAN 标签链构造。
func TestSerializeVLANRoundTrip(t *testing.T) {
	eth := &Ethernet{
		DstMAC:    make(net.HardwareAddr, 6),
		SrcMAC:    make(net.HardwareAddr, 6),
		EtherType: EthernetTypeIPv4,
		VLANIDs:   []uint16{100, 200},
	}
	ip := &IPv4{Version: 4, ID: 1, TTL: 64, Protocol: ProtocolICMP, SrcIP: net.IPv4(1, 1, 1, 1), DstIP: net.IPv4(2, 2, 2, 2)}
	icmp := &ICMP{Type: 8, Code: 0, Rest: []byte{0, 1, 2, 3}}
	buf, err := SerializeLayers(SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, ip, icmp)
	if err != nil {
		t.Fatalf("SerializeLayers: %v", err)
	}
	layers, err := Decode(buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := layers[0].(*Ethernet)
	if len(got.VLANIDs) != 2 || got.VLANIDs[0] != 100 || got.VLANIDs[1] != 200 {
		t.Fatalf("VLANIDs = %v, want [100 200]", got.VLANIDs)
	}
	if len(layers) != 3 {
		t.Fatalf("decoded %d layers, want 3", len(layers))
	}
	icmpGot := layers[2].(*ICMP)
	fullICMP := append(append([]byte(nil), icmpGot.Contents...), icmpGot.Rest...)
	if InternetChecksum(fullICMP) != 0 {
		t.Fatal("ICMP checksum invalid")
	}
}

// TestInternetChecksumGolden 覆盖 RFC 1071 黄金值。
func TestInternetChecksumGolden(t *testing.T) {
	// RFC 1071 4.1 示例：00 01 f2 03 f4 f5 f6 f7 → 0x220d。
	data := []byte{0x00, 0x01, 0xf2, 0x03, 0xf4, 0xf5, 0xf6, 0xf7}
	if got := InternetChecksum(data); got != 0x220d {
		t.Fatalf("checksum = %#04x, want 0x220d", got)
	}
}

// layerNames 辅助打印层序列。
func layerNames(ls []Layer) []LayerType {
	out := make([]LayerType, len(ls))
	for i, l := range ls {
		out[i] = l.LayerType()
	}
	return out
}
