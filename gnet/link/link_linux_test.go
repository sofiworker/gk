//go:build linux

package link

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

type stubLink struct{ attrs *netlink.LinkAttrs }

func (s stubLink) Attrs() *netlink.LinkAttrs { return s.attrs }
func (stubLink) Type() string                { return "stub" }

func TestFromNetlink(t *testing.T) {
	flags := net.FlagUp | net.FlagBroadcast
	hw := net.HardwareAddr{0, 1, 2, 3, 4, 5}
	attrs := &netlink.LinkAttrs{
		Index:        1,
		Name:         "eth0",
		MTU:          1500,
		HardwareAddr: hw,
		Flags:        flags,
		OperState:    netlink.OperUp,
	}

	l := fromNetlink(stubLink{attrs: attrs})
	if l.Name != "eth0" || l.Index != 1 || l.MTU != 1500 {
		t.Fatalf("unexpected basic fields: %+v", l)
	}
	if l.Up != true || l.LinkDetected != true {
		t.Fatalf("expected up and link detected: %+v", l)
	}
	if l.OperState != "up" {
		t.Fatalf("unexpected oper state: %s", l.OperState)
	}
	if l.HardwareAddr.String() != hw.String() {
		t.Fatalf("unexpected hardware addr: %s", l.HardwareAddr)
	}
}

func TestFromNetlinkDown(t *testing.T) {
	attrs := &netlink.LinkAttrs{
		Index:     2,
		Name:      "eth1",
		Flags:     0,
		OperState: netlink.OperDown,
	}
	l := fromNetlink(stubLink{attrs: attrs})
	if l.Up || l.LinkDetected {
		t.Fatalf("expected down link: %+v", l)
	}
	if l.HardwareAddr != nil {
		t.Fatalf("expected nil hardware addr when empty: %+v", l)
	}
}
