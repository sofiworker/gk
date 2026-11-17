package forward

import (
	"net"
	"testing"
	"time"
)

// TestUDPBridgeBidirectional 验证双向桥接：client→remote 转发 +
// remote 回包经会话专属 upstream 送回 client（回归单向缺陷修复）。
//
// TestUDPBridgeBidirectional verifies bidirectional bridging: client→remote
// forwarding plus remote replies relayed back to the client via the
// session's dedicated upstream (regression for the one-way defect fix).
func TestUDPBridgeBidirectional(t *testing.T) {
	// remote：UDP echo 服务器。
	remote, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := remote.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = remote.WriteToUDP(buf[:n], addr) // echo
		}
	}()

	// 桥：本地监听端口。
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	bridge, err := NewUDPBridge(local, remote.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- bridge.Start() }()
	t.Cleanup(func() {
		_ = bridge.Close()
		<-done
	})

	// 两个客户端并发往返。
	for i := 0; i < 2; i++ {
		client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		msg := []byte("ping-from-client")
		if _, err := client.WriteToUDP(msg, local.LocalAddr().(*net.UDPAddr)); err != nil {
			t.Fatal(err)
		}
		_ = client.SetDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 2048)
		n, _, err := client.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("client %d read reply: %v", i, err)
		}
		if string(buf[:n]) != string(msg) {
			t.Fatalf("client %d reply = %q, want %q", i, buf[:n], msg)
		}
	}
	if n := bridge.GetActiveSessions(); n != 2 {
		t.Fatalf("active sessions = %d, want 2", n)
	}
}
