//go:build linux

package addr

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestFromNetlinkAddr(t *testing.T) {
	ip, ipnet, _ := net.ParseCIDR("10.0.0.1/24")
	ipnet.IP = ip
	peerIP, peerNet, _ := net.ParseCIDR("10.0.0.2/24")
	peerNet.IP = peerIP

	attrs := &netlink.LinkAttrs{
		Index: 1,
		Name:  "eth0",
	}
	na := netlink.Addr{
		IPNet: ipnet,
		Peer:  peerNet,
		Label: "eth0",
		Scope: 0,
		Flags: 0,
	}
	a := fromNetlinkAddr(attrs, na)
	if a.IfIndex != 1 || a.IfName != "eth0" {
		t.Fatalf("unexpected interface fields: %+v", a)
	}
	if a.IPNet.String() != ipnet.String() {
		t.Fatalf("unexpected IPNet: %s", a.IPNet)
	}
	if a.Peer.String() != peerNet.String() {
		t.Fatalf("unexpected Peer: %s", a.Peer)
	}
}

func TestToNetlinkAddr(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("fd00::1/64")
	na := toNetlinkAddr(Address{IPNet: ipnet, Label: "lo"})
	if na.Label != "lo" || na.IPNet.String() != ipnet.String() {
		t.Fatalf("unexpected netlink addr: %+v", na)
	}
}
