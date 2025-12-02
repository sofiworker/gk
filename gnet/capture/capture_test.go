package capture

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/rawcap"
)

type stubHandle struct {
	packets chan *rawcap.Packet
}

func (s *stubHandle) ReadPacket() (*rawcap.Packet, error) {
	pkt, ok := <-s.packets
	if !ok {
		return nil, rawcap.ErrHandleClosed
	}
	return pkt, nil
}
func (*stubHandle) WritePacketData([]byte) error { return nil }
func (*stubHandle) SetFilter(string) error       { return nil }
func (*stubHandle) RawHandle() (interface{}, error) {
	return nil, nil
}
func (*stubHandle) Stats() *rawcap.Stats { return &rawcap.Stats{} }
func (*stubHandle) Close() error         { return nil }

func TestNewCaptureRejectsMultiPcap(t *testing.T) {
	cfg := Config{
		Interfaces: []string{"eth0", "eth1"},
		Format:     FormatPCAP,
		Writer:     &bytes.Buffer{},
	}
	if _, err := New(cfg); err == nil {
		t.Fatalf("expected error for multi-interface pcap")
	}
}

func TestCaptureRunWithStub(t *testing.T) {
	origOpen := openLiveFn
	origIface := ifaceByName
	defer func() {
		openLiveFn = origOpen
		ifaceByName = origIface
	}()

	openLiveFn = func(name string, cfg rawcap.Config) (rawcap.Handle, error) {
		h := &stubHandle{packets: make(chan *rawcap.Packet, 2)}
		h.packets <- &rawcap.Packet{Data: []byte("abc")}
		close(h.packets)
		return h, nil
	}
	ifaceByName = func(name string) (*net.Interface, error) {
		return &net.Interface{Name: name, Index: 1}, nil
	}

	var buf bytes.Buffer
	cap, err := New(Config{
		Interfaces: []string{"eth0"},
		Format:     FormatPCAP,
		Writer:     &buf,
	})
	if err != nil {
		t.Fatalf("new capture: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := cap.Run(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run capture: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("expected output data written")
	}
}
