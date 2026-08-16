//go:build linux

package rawcap

import (
	"net"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/layers"
)

// TestTPacketV3Live 实测 TPACKET_V3 模式：抓 lo 真实流量并解码验证
// 包内容正确（回归 P0 包地址错位修复）。
//
// TestTPacketV3Live exercises TPACKET_V3 mode: capture real lo traffic and
// decode to verify packet content (regression for the P0 packet-offset fix).
func TestTPacketV3Live(t *testing.T) {
	h, err := OpenLive("lo", Config{SnapLen: 65535, TPacketV3: true, Timeout: 2 * time.Second})
	if err != nil {
		t.Skipf("no CAP_NET_RAW: %v", err)
	}
	defer h.Close()

	// 产生真实 TCP 流量。
	go func() {
		for i := 0; i < 3; i++ {
			c, err := net.DialTimeout("tcp", "127.0.0.1:9", time.Second)
			if err == nil {
				_ = c.Close()
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	decoded := 0
	for time.Now().Before(deadline) {
		pkt, err := h.ReadPacket()
		if err != nil {
			continue
		}
		ls, err := layers.Decode(pkt.Data)
		if err != nil {
			t.Fatalf("TPACKET packet failed decode (offset bug?): %v (len=%d head=%x)", err, len(pkt.Data), pkt.Data[:16])
		}
		for _, l := range ls {
			if tcp, ok := l.(*layers.TCP); ok {
				if tcp.SrcPort == 0 && tcp.DstPort == 0 {
					t.Fatalf("TCP ports both zero — data offset wrong")
				}
				decoded++
			}
		}
		if decoded >= 2 {
			break
		}
	}
	if decoded == 0 {
		t.Fatal("no decodable TCP packets captured via TPACKET_V3")
	}
	t.Logf("decoded %d TCP packets via TPACKET_V3", decoded)
}
