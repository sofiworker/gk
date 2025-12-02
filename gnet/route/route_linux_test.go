//go:build linux

package route

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type stubRoute struct {
	netlink.Route
}

func TestConvertRoutes(t *testing.T) {
	dst := parseCIDR("10.0.0.0/24")
	gw := net.ParseIP("10.0.0.1")
	nlr := netlink.Route{
		Dst:       dst,
		Gw:        gw,
		Src:       net.ParseIP("10.0.0.2"),
		LinkIndex: 2,
		Table:     254,
		Priority:  100,
		Protocol:  4,
		Scope:     netlink.SCOPE_UNIVERSE,
		Type:      unix.RTN_UNICAST,
	}
	rs := convertRoutes([]netlink.Route{nlr})
	if len(rs) != 1 {
		t.Fatalf("expected 1 route, got %d", len(rs))
	}
	r := rs[0]
	if r.IfIndex != 2 || r.Gw.String() != gw.String() || r.Dst.String() != dst.String() {
		t.Fatalf("unexpected route fields: %+v", r)
	}
}

func TestToNetlinkRoute(t *testing.T) {
	dst := parseCIDR("192.168.1.0/24")
	gw := net.ParseIP("192.168.1.1")
	r := Route{
		Dst:      dst,
		Gw:       gw,
		Src:      net.ParseIP("192.168.1.10"),
		IfIndex:  3,
		Table:    254,
		Priority: 10,
		Protocol: 2,
		Scope:    int(netlink.SCOPE_UNIVERSE),
		Type:     unix.RTN_UNICAST,
	}
	nlr := toNetlinkRoute(r)
	if nlr.LinkIndex != 3 || nlr.Gw.String() != gw.String() || nlr.Dst.String() != dst.String() {
		t.Fatalf("unexpected netlink route: %+v", nlr)
	}
}
