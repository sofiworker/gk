package forward

import (
	"net"
	"strings"
	"testing"
	"time"
)

type mockHandler struct{ called int }

func (m *mockHandler) ShouldProcess(data []byte) bool { return true }

func (m *mockHandler) ProcessData(data []byte, direction Direction) []byte {
	m.called++
	out := make([]byte, len(data))
	copy(out, data)
	for i := range out {
		out[i] = byte(strings.ToUpper(string(out[i]))[0])
	}
	return out
}

// helper to create two connected pairs so bridge directions can be validated independently.
func pipePair(t *testing.T) (localPeer net.Conn, bridgeEnd net.Conn, remotePeer net.Conn, bridgeEndRemote net.Conn) {
	t.Helper()
	localPeer, bridgeEnd = net.Pipe()
	remotePeer, bridgeEndRemote = net.Pipe()
	return
}

func TestProtocolAwareBridgeForward(t *testing.T) {
	localPeer, localEnd, remotePeer, remoteEnd := pipePair(t)
	defer localPeer.Close()
	defer remotePeer.Close()

	handler := &mockHandler{}
	bridge := NewProtocolAwareBridge(localEnd, remoteEnd, TCPConfig{BufferSize: 0, Timeout: 50 * time.Millisecond}, &ByteOrderConfig{
		ProtocolHandler: handler,
	})

	done := make(chan struct{})
	go func() {
		_ = bridge.Start()
		close(done)
	}()

	msg := []byte("ping")
	if _, err := localPeer.Write(msg); err != nil {
		t.Fatalf("write local: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := remotePeer.Read(buf); err != nil {
		t.Fatalf("read remote: %v", err)
	}
	if string(buf) != "PING" {
		t.Fatalf("expected protocol handler uppercased, got %q", string(buf))
	}

	// reverse direction
	reply := []byte("pong")
	if _, err := remotePeer.Write(reply); err != nil {
		t.Fatalf("write remote: %v", err)
	}
	if _, err := localPeer.Read(buf[:len(reply)]); err != nil {
		t.Fatalf("read local: %v", err)
	}

	if handler.called < 2 {
		t.Fatalf("protocol handler should be called in both directions")
	}

	_ = bridge.Close()
	<-done
}
