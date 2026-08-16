package craft

import (
	"io"
	"net"
	"testing"

	"github.com/sofiworker/gk/gnet/layers"
)

// buildTCPFrame 构造完整 L2 帧。
func buildTCPFrame(t *testing.T, payload string) []byte {
	t.Helper()
	eth := &layers.Ethernet{
		DstMAC:    net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		SrcMAC:    net.HardwareAddr{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb},
		EtherType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{Version: 4, ID: 1, TTL: 64, Protocol: layers.ProtocolTCP,
		SrcIP: net.IPv4(192, 168, 1, 1), DstIP: net.IPv4(10, 0, 0, 1)}
	tcp := &layers.TCP{SrcPort: 12345, DstPort: 80, Seq: 1, Flags: layers.TCPFlagSYN, Window: 65535}
	buf, err := Serialize(layers.SerializeOptions{FixLengths: true, ComputeChecksums: true},
		eth, ip, tcp, layers.Payload(payload))
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	return buf
}

// TestConnInjector 覆盖连接注入：帧经 net.Pipe 可完整读出并解码。
func TestConnInjector(t *testing.T) {
	frame := buildTCPFrame(t, "hello")
	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()

	injector := NewConnInjector(srv)
	// net.Pipe 写阻塞到对端读：读端先就位。
	gotCh := make(chan []byte, 1)
	go func() {
		buf := make([]byte, len(frame))
		if _, err := io.ReadFull(cli, buf); err != nil {
			t.Errorf("read: %v", err)
			gotCh <- nil
			return
		}
		gotCh <- buf
	}()
	if err := injector.Inject(frame); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	got := <-gotCh
	if got == nil {
		t.Fatal("reader failed")
	}
	for i := range frame {
		if got[i] != frame[i] {
			t.Fatalf("frame mismatch at %d", i)
		}
	}
	// 解码验证完整性。
	ls, err := layers.Decode(got)
	if err != nil || len(ls) != 3 {
		t.Fatalf("Decode = %v (%d layers)", err, len(ls))
	}
}

// TestSerializeErrors 覆盖非法层组合错误。
func TestSerializeErrors(t *testing.T) {
	// 缺网络层的 TCP（校验和需要 IPv4/IPv6，但 FixLengths 不强制——
	// 用错误 MAC 长度触发错误）。
	badEth := &layers.Ethernet{DstMAC: net.HardwareAddr{0x01}, SrcMAC: make(net.HardwareAddr, 6)}
	if _, err := Serialize(layers.SerializeOptions{}, badEth, layers.Payload("x")); err == nil {
		t.Fatal("Serialize with 1-byte MAC = nil error")
	}
}

// TestAFPacketInjectorSkip 覆盖无特权环境下的错误处理（非 root 跳过）。
func TestAFPacketInjectorSkip(t *testing.T) {
	inj, err := NewAFPacketInjector("lo")
	if err != nil {
		t.Skipf("no permission for raw socket (expected as non-root): %v", err)
	}
	t.Cleanup(func() {
		if c, ok := inj.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})
}
