package capture

import (
	"net"
	"os/exec"
	"testing"

	"github.com/sofiworker/gk/gnet/layers"
	"golang.org/x/net/bpf"
)

// buildPackets 构造语义测试报文语料：IPv4 TCP/UDP/ICMP、ARP、IPv6 TCP。
// buildPackets builds the semantic test corpus: IPv4 TCP/UDP/ICMP, ARP, IPv6
// TCP.
func buildPackets(t *testing.T) map[string][]byte {
	t.Helper()
	corpus := make(map[string][]byte)

	mkEth := func(etherType layers.EthernetType) *layers.Ethernet {
		return &layers.Ethernet{
			DstMAC:    net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
			SrcMAC:    net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
			EtherType: etherType,
		}
	}
	mkIPv4 := func(proto uint8, src, dst string) *layers.IPv4 {
		return &layers.IPv4{Version: 4, ID: 1, TTL: 64, Protocol: proto,
			SrcIP: net.ParseIP(src), DstIP: net.ParseIP(dst)}
	}

	build := func(name string, ls ...layers.Layer) {
		buf, err := layers.SerializeLayers(layers.SerializeOptions{FixLengths: true, ComputeChecksums: true}, ls...)
		if err != nil {
			t.Fatalf("build %s: %v", name, err)
		}
		corpus[name] = buf
	}

	build("tcp80", mkEth(layers.EthernetTypeIPv4),
		mkIPv4(layers.ProtocolTCP, "192.168.1.1", "10.0.0.1"),
		&layers.TCP{SrcPort: 12345, DstPort: 80, Seq: 1, Window: 65535},
		layers.Payload("GET /"))
	build("tcp443", mkEth(layers.EthernetTypeIPv4),
		mkIPv4(layers.ProtocolTCP, "192.168.1.1", "10.0.0.1"),
		&layers.TCP{SrcPort: 12345, DstPort: 443, Seq: 1, Window: 65535},
		layers.Payload("x"))
	build("udp53", mkEth(layers.EthernetTypeIPv4),
		mkIPv4(layers.ProtocolUDP, "192.168.1.2", "8.8.8.8"),
		&layers.UDP{SrcPort: 53000, DstPort: 53}, layers.Payload("dns"))
	build("icmp", mkEth(layers.EthernetTypeIPv4),
		mkIPv4(layers.ProtocolICMP, "192.168.1.1", "10.0.0.2"),
		&layers.ICMP{Type: 8, Code: 0})
	build("arp", mkEth(layers.EthernetTypeARP),
		&layers.ARP{HwType: 1, ProtoType: uint16(layers.EthernetTypeIPv4), Operation: 1,
			SrcMAC: net.HardwareAddr{1, 2, 3, 4, 5, 6}, SrcIP: net.IPv4(192, 168, 1, 1),
			DstMAC: make(net.HardwareAddr, 6), DstIP: net.IPv4(192, 168, 1, 2)})
	build("ipv6tcp", mkEth(layers.EthernetTypeIPv6),
		&layers.IPv6{Version: 6, NextHeader: layers.ProtocolTCP, HopLimit: 64,
			SrcIP: net.ParseIP("2001:db8::1"), DstIP: net.ParseIP("2001:db8::2")},
		&layers.TCP{SrcPort: 1000, DstPort: 22, Seq: 1, Window: 65535},
		layers.Payload("ssh"))
	return corpus
}

// runFilter 在报文上执行过滤程序，返回 accept 与否。
// runFilter runs the program on a packet, reporting accept/reject.
func runFilter(t *testing.T, insns []bpf.Instruction, pkt []byte) bool {
	t.Helper()
	vm, err := bpf.NewVM(insns)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	n, err := vm.Run(pkt)
	if err != nil {
		t.Fatalf("vm.Run: %v", err)
	}
	return n > 0
}

// TestCompileExprSemantics 组合子语义与 tcpdump 同报文对照。
// TestCompileExprSemantics compares combinator semantics with tcpdump on the
// same packets.
func TestCompileExprSemantics(t *testing.T) {
	if !tcpdumpAvailable() {
		t.Skip("tcpdump not available")
	}
	corpus := buildPackets(t)
	exprs := []string{
		"tcp or udp",
		"tcp and port 80",
		"not tcp",
		"tcp and not port 80",
		"(tcp or udp) and port 53",
		"host 192.168.1.1 and tcp",
		"src host 192.168.1.2 and udp",
		"net 10.0.0.0/24 or host 8.8.8.8",
		"icmp or arp",
		"not (tcp or udp)",
		"port 80 or port 53",
		"tcp and (port 80 or port 443)",
		"ip6 and tcp",
		"dst port 53 or src port 53",
		"portrange 1-100 and tcp",
	}
	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			got, err := CompileExpr(expr, LinkTypeEN10MB)
			if err != nil {
				t.Fatalf("CompileExpr: %v", err)
			}
			out, err := exec.Command("tcpdump", "-dd", expr).Output()
			if err != nil {
				t.Fatalf("tcpdump: %v", err)
			}
			want := parseTcpdumpDD(t, string(out))
			for name, pkt := range corpus {
				g := runFilter(t, got, pkt)
				w := runFilter(t, want, pkt)
				if g != w {
					t.Errorf("packet %s: got accept=%v, tcpdump accept=%v", name, g, w)
				}
			}
		})
	}
}
