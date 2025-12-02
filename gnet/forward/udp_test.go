package forward

import (
	"net"
	"testing"
	"time"
)

func TestUDPBridgeForward(t *testing.T) {
	localConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Skipf("listen udp not permitted: %v", err)
	}
	defer localConn.Close()

	remoteConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Skipf("listen remote udp not permitted: %v", err)
	}
	defer remoteConn.Close()

	bridge, err := NewUDPBridge(localConn, remoteConn.LocalAddr().String())
	if err != nil {
		t.Fatalf("new udp bridge: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_ = bridge.Start()
		close(done)
	}()

	clientConn, err := net.DialUDP("udp", nil, localConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer clientConn.Close()

	msg := []byte("hello")
	if _, err := clientConn.Write(msg); err != nil {
		t.Fatalf("client write: %v", err)
	}

	_ = remoteConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, addr, err := remoteConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("remote read: %v", err)
	}
	if addr == nil || n != len(msg) || string(buf[:n]) != string(msg) {
		t.Fatalf("unexpected forwarded data: %q from %v", string(buf[:n]), addr)
	}

	_ = bridge.Close()
	<-done
}
