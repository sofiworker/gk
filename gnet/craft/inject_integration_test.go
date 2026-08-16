//go:build linux

package craft

import (
	"net"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/layers"
	"github.com/sofiworker/gk/gnet/rawcap"
)

// TestInjectCaptureLoopback 集成：AF_PACKET 注入 lo → rawcap 回捕解码验证
// （需 root/CAP_NET_RAW，无权限跳过）。
//
// TestInjectCaptureLoopback integrates: AF_PACKET injection into lo →
// rawcap recapture + decode verification (needs root/CAP_NET_RAW; skips
// otherwise).
func TestInjectCaptureLoopback(t *testing.T) {
	h, err := rawcap.OpenLive("lo", rawcap.Config{SnapLen: 65535})
	if err != nil {
		t.Skipf("no CAP_NET_RAW: %v", err)
	}
	defer h.Close()

	eth := &layers.Ethernet{
		DstMAC:    net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		SrcMAC:    net.HardwareAddr{0x02, 0, 0, 0, 0, 0x01},
		EtherType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{Version: 4, ID: 0xbeef, TTL: 64, Protocol: layers.ProtocolUDP,
		SrcIP: net.IPv4(127, 0, 0, 1), DstIP: net.IPv4(127, 0, 0, 1)}
	udp := &layers.UDP{SrcPort: 45677, DstPort: 45678}
	frame, err := Serialize(layers.SerializeOptions{FixLengths: true, ComputeChecksums: true},
		eth, ip, udp, layers.Payload("inject-integration"))
	if err != nil {
		t.Fatal(err)
	}

	inj, err := NewAFPacketInjector("lo")
	if err != nil {
		t.Skipf("no injection permission: %v", err)
	}
	t.Cleanup(func() { _ = inj.(interface{ Close() error }).Close() })

	if err := inj.Inject(frame); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pkt, err := h.ReadPacket()
		if err != nil {
			continue
		}
		ls, err := layers.Decode(pkt.Data)
		if err != nil {
			continue
		}
		for _, l := range ls {
			if u, ok := l.(*layers.UDP); ok && u.SrcPort == 45677 && u.DstPort == 45678 {
				if string(u.Payload()) != "inject-integration" {
					t.Fatalf("payload = %q", u.Payload())
				}
				return
			}
		}
	}
	t.Fatal("injected frame not captured on lo")
}
